package main

// Читалка PDF, часть 4: интерпретатор содержимого страницы.

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"
	"strings"
	"sync"
)

type PdfDoc struct {
	F       *PdfFile
	Pages   []*PdfPage
	Outline []PdfOutline
	fonts   map[string]*pdfFont
	fontMu  sync.Mutex
	images  map[int]*pdfImage
	imgMu   sync.Mutex
	imgLRU  []int              // порядок использования картинок (последняя — свежая)
	Text    map[int][]PdfGlyph // текст страниц (для поиска), в пунктах страницы
}

// PdfGlyph — символ на странице: координаты в пунктах (сверху вниз, с учётом поворота).
type PdfGlyph struct {
	S          string
	X, Y, W, H float64
}

func LoadPDF(data []byte) (*PdfDoc, error) {
	f, err := OpenPDF(data)
	if err != nil {
		return nil, err
	}
	d := &PdfDoc{F: f, fonts: map[string]*pdfFont{}, images: map[int]*pdfImage{}, Text: map[int][]PdfGlyph{}}
	d.Pages = f.Pages()
	if len(d.Pages) == 0 {
		return nil, ErrNoPages
	}
	func() {
		defer func() { recover() }()
		d.Outline = f.Outline(d.Pages)
	}()
	return d, nil
}

var ErrNoPages = errorString("в PDF нет страниц")

type errorString string

func (e errorString) Error() string { return string(e) }

type pdfColorSpace struct {
	kind   string // gray rgb cmyk lab indexed sep pattern
	n      int
	base   *pdfColorSpace
	lookup []byte
	hival  int
	fn     *pdfFunc
}

type pdfGState struct {
	ctm        pdfMat
	fillCS     *pdfColorSpace
	strokeCS   *pdfColorSpace
	fill       [3]uint8
	stroke     [3]uint8
	fillPat    any
	lw         float64
	cap, join  int
	dash       []float64
	dashPhase  float64
	clip       *pdfClip
	fa, sa     float32
	font       *pdfFont
	fs         float64
	tc, tw, th float64
	tl, rise   float64
	tmode      int
}

type pdfRenderer struct {
	f          *PdfFile
	doc        *PdfDoc
	cv         *pdfCanvas
	gs         pdfGState
	stack      []pdfGState
	path       pathBuilder
	pendClip   int // 1 nonzero, 2 even-odd
	tm, tlm    pdfMat
	res        []pdfDict
	depth      int
	text       []PdfGlyph
	base       pdfMat // из пунктов страницы в пиксели
	scale      float64
	textOnly   bool
	glyphCache map[glyphKeyP]*covMask
	ops        int
}

type glyphKeyP struct {
	f              *pdfFont
	code           int
	sx, sy, fx, fy int32
}

var devGray = &pdfColorSpace{kind: "gray", n: 1}
var devRGB = &pdfColorSpace{kind: "rgb", n: 3}
var devCMYK = &pdfColorSpace{kind: "cmyk", n: 4}

// RenderPage рисует страницу в масштабе scale (пикселей на пункт). textOnly — только собрать текст.
func (d *PdfDoc) RenderPage(i int, scale float64, textOnly bool) (img *image.RGBA, glyphs []PdfGlyph) {
	p := d.Pages[i]
	W := int(math.Ceil(p.W() * scale))
	H := int(math.Ceil(p.H() * scale))
	if textOnly {
		W, H = 1, 1
	}
	W, H = max(1, W), max(1, H)
	// огромный лист на большом масштабе: не просим у системы гигабайты, рисуем мельче
	const maxPix = 64 << 20
	if !textOnly && float64(W)*float64(H) > maxPix {
		k := math.Sqrt(maxPix / (float64(W) * float64(H)))
		scale *= k
		W, H = max(1, int(float64(W)*k)), max(1, int(float64(H)*k))
	}
	r := &pdfRenderer{f: d.F, doc: d, cv: newPdfCanvas(W, H), scale: scale, textOnly: textOnly, glyphCache: map[glyphKeyP]*covMask{}}
	// пункты страницы → пиксели: сдвиг на начало области, поворот, переворот по Y
	bx0, by0, bx1, by1 := p.Box[0], p.Box[1], p.Box[2], p.Box[3]
	var m pdfMat
	switch p.Rotate {
	case 90:
		m = pdfMat{0, 1, 1, 0, -by0, -bx0}
	case 180:
		m = pdfMat{-1, 0, 0, 1, bx1, -by0}
	case 270:
		m = pdfMat{0, -1, -1, 0, by1, bx1}
	default:
		m = pdfMat{1, 0, 0, -1, -bx0, by1}
	}
	r.base = m.mul(pdfMat{scale, 0, 0, scale, 0, 0})
	r.gs = pdfGState{ctm: r.base, fillCS: devGray, strokeCS: devGray, lw: 1, fa: 1, sa: 1, th: 1}
	r.res = []pdfDict{p.Res}
	func() {
		defer func() { recover() }()
		r.run(d.F.Content(p))
	}()
	if !textOnly {
		img = r.cv.toRGBA()
	}
	return img, r.text
}

func (r *pdfRenderer) resource(cat, name string) any {
	for i := len(r.res) - 1; i >= 0; i-- {
		if r.res[i] == nil {
			continue
		}
		if d := r.f.dict(r.res[i][cat]); d != nil {
			if v, ok := d[name]; ok {
				return v
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- цвет
func (r *pdfRenderer) colorSpace(v any, depth int) *pdfColorSpace {
	if depth > 5 {
		return devGray
	}
	v = r.f.Resolve(v)
	switch x := v.(type) {
	case pdfName:
		switch x {
		case "DeviceGray", "G", "CalGray":
			return devGray
		case "DeviceRGB", "RGB", "CalRGB":
			return devRGB
		case "DeviceCMYK", "CMYK":
			return devCMYK
		case "Pattern":
			return &pdfColorSpace{kind: "pattern", n: 1}
		default:
			if cs := r.resource("ColorSpace", string(x)); cs != nil {
				return r.colorSpace(cs, depth+1)
			}
		}
	case pdfArray:
		if len(x) == 0 {
			return devGray
		}
		switch r.f.name(x[0]) {
		case "ICCBased":
			if s := r.f.stream(x[1]); s != nil {
				switch int(r.f.num(s.Dict["N"])) {
				case 1:
					return devGray
				case 4:
					return devCMYK
				default:
					return devRGB
				}
			}
		case "CalRGB":
			return devRGB
		case "CalGray":
			return devGray
		case "Lab":
			return &pdfColorSpace{kind: "lab", n: 3}
		case "Indexed", "I":
			if len(x) >= 4 {
				cs := &pdfColorSpace{kind: "indexed", n: 1, base: r.colorSpace(x[1], depth+1), hival: int(r.f.num(x[2]))}
				switch l := r.f.Resolve(x[3]).(type) {
				case pdfString:
					cs.lookup = []byte(l)
				case *pdfStream:
					cs.lookup, _ = r.f.decodeStream(l, false)
				}
				return cs
			}
		case "Separation", "DeviceN":
			n := 1
			if r.f.name(x[0]) == "DeviceN" {
				n = len(r.f.arr(x[1]))
			}
			cs := &pdfColorSpace{kind: "sep", n: n}
			if len(x) >= 4 {
				cs.base = r.colorSpace(x[2], depth+1)
				cs.fn = r.f.loadFunc(x[3])
			}
			if r.f.name(x[1]) == "None" {
				cs.kind = "none"
			}
			return cs
		case "Pattern":
			return &pdfColorSpace{kind: "pattern", n: 1}
		default:
			return r.colorSpace(x[0], depth+1)
		}
	}
	return devGray
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(v*255 + 0.5)
}

func (cs *pdfColorSpace) rgb(c []float64) [3]uint8 {
	g := func(i int) float64 {
		if i < len(c) {
			return c[i]
		}
		return 0
	}
	switch cs.kind {
	case "gray":
		v := clamp8(g(0))
		return [3]uint8{v, v, v}
	case "rgb":
		return [3]uint8{clamp8(g(0)), clamp8(g(1)), clamp8(g(2))}
	case "cmyk":
		k := g(3)
		return [3]uint8{clamp8((1 - g(0)) * (1 - k)), clamp8((1 - g(1)) * (1 - k)), clamp8((1 - g(2)) * (1 - k))}
	case "lab":
		L, a, b := g(0), g(1), g(2)
		fy := (L + 16) / 116
		fx := fy + a/500
		fz := fy - b/200
		f := func(t float64) float64 {
			if t > 6.0/29 {
				return t * t * t
			}
			return 3 * (6.0 / 29) * (6.0 / 29) * (t - 4.0/29)
		}
		X, Y, Z := 0.9505*f(fx), f(fy), 1.089*f(fz)
		rl := 3.2406*X - 1.5372*Y - 0.4986*Z
		gl := -0.9689*X + 1.8758*Y + 0.0415*Z
		bl := 0.0557*X - 0.204*Y + 1.057*Z
		gam := func(v float64) float64 {
			if v <= 0.0031308 {
				return 12.92 * v
			}
			return 1.055*math.Pow(v, 1/2.4) - 0.055
		}
		return [3]uint8{clamp8(gam(rl)), clamp8(gam(gl)), clamp8(gam(bl))}
	case "indexed":
		i := int(g(0))
		if i < 0 {
			i = 0
		}
		if i > cs.hival {
			i = cs.hival
		}
		n := cs.base.n
		var bc []float64
		for k := 0; k < n; k++ {
			o := i*n + k
			v := 0.0
			if o < len(cs.lookup) {
				v = float64(cs.lookup[o]) / 255
			}
			bc = append(bc, v)
		}
		if cs.base.kind == "lab" && len(bc) == 3 {
			bc = []float64{bc[0] * 100, bc[1]*255 - 128, bc[2]*255 - 128}
		}
		return cs.base.rgb(bc)
	case "sep":
		if cs.fn != nil && cs.base != nil {
			return cs.base.rgb(cs.fn.eval(c))
		}
		v := clamp8(1 - g(0))
		return [3]uint8{v, v, v}
	case "none":
		return [3]uint8{255, 255, 255}
	}
	return [3]uint8{0, 0, 0}
}

// ---------------------------------------------------------------- функции
type pdfFunc struct {
	typ     int
	domain  []float64
	rng     []float64
	c0, c1  []float64
	n       float64
	fns     []*pdfFunc
	bounds  []float64
	encode  []float64
	size    []int
	bps     int
	samples []byte
	decode  []float64
}

func (f *PdfFile) loadFunc(v any) *pdfFunc {
	v = f.Resolve(v)
	if a, ok := v.(pdfArray); ok {
		// массив функций — по одной на компонент
		fn := &pdfFunc{typ: 100}
		for _, x := range a {
			if s := f.loadFunc(x); s != nil {
				fn.fns = append(fn.fns, s)
			}
		}
		return fn
	}
	d := f.dict(v)
	if d == nil {
		return nil
	}
	fn := &pdfFunc{typ: int(f.num(d["FunctionType"])), domain: f.numArray(d["Domain"]), rng: f.numArray(d["Range"])}
	switch fn.typ {
	case 2:
		fn.c0 = f.numArray(d["C0"])
		fn.c1 = f.numArray(d["C1"])
		if fn.c0 == nil {
			fn.c0 = []float64{0}
		}
		if fn.c1 == nil {
			fn.c1 = []float64{1}
		}
		fn.n = f.num(d["N"])
	case 3:
		for _, x := range f.arr(d["Functions"]) {
			fn.fns = append(fn.fns, f.loadFunc(x))
		}
		fn.bounds = f.numArray(d["Bounds"])
		fn.encode = f.numArray(d["Encode"])
	case 0:
		if s := f.stream(v); s != nil {
			fn.samples, _ = f.decodeStream(s, false)
		}
		for _, x := range f.numArray(d["Size"]) {
			fn.size = append(fn.size, int(x))
		}
		fn.bps = int(f.num(d["BitsPerSample"]))
		fn.encode = f.numArray(d["Encode"])
		fn.decode = f.numArray(d["Decode"])
	}
	return fn
}

func (fn *pdfFunc) eval(in []float64) []float64 {
	if fn == nil {
		return in
	}
	x := 0.0
	if len(in) > 0 {
		x = in[0]
	}
	if len(fn.domain) >= 2 {
		x = math.Max(fn.domain[0], math.Min(fn.domain[1], x))
	}
	switch fn.typ {
	case 100:
		var out []float64
		for _, s := range fn.fns {
			out = append(out, s.eval(in)...)
		}
		return out
	case 2:
		out := make([]float64, len(fn.c0))
		p := math.Pow(math.Max(x, 0), fn.n)
		if fn.n == 1 {
			p = x
		}
		for i := range out {
			c1 := 1.0
			if i < len(fn.c1) {
				c1 = fn.c1[i]
			}
			out[i] = fn.c0[i] + p*(c1-fn.c0[i])
		}
		return out
	case 3:
		k := 0
		for k < len(fn.bounds) && x >= fn.bounds[k] {
			k++
		}
		if k >= len(fn.fns) || fn.fns[k] == nil {
			return []float64{x}
		}
		lo, hi := 0.0, 1.0
		if len(fn.domain) >= 2 {
			lo, hi = fn.domain[0], fn.domain[1]
		}
		if k > 0 {
			lo = fn.bounds[k-1]
		}
		if k < len(fn.bounds) {
			hi = fn.bounds[k]
		}
		e0, e1 := 0.0, 1.0
		if 2*k+1 < len(fn.encode) {
			e0, e1 = fn.encode[2*k], fn.encode[2*k+1]
		}
		t := e0
		if hi > lo {
			t = e0 + (x-lo)/(hi-lo)*(e1-e0)
		}
		return fn.fns[k].eval([]float64{t})
	case 0:
		if len(fn.size) == 0 || fn.bps == 0 {
			return []float64{x}
		}
		n := len(fn.rng) / 2
		lo, hi := 0.0, 1.0
		if len(fn.domain) >= 2 {
			lo, hi = fn.domain[0], fn.domain[1]
		}
		e0, e1 := 0.0, float64(fn.size[0]-1)
		if len(fn.encode) >= 2 {
			e0, e1 = fn.encode[0], fn.encode[1]
		}
		idx := int(e0 + (x-lo)/math.Max(hi-lo, 1e-9)*(e1-e0) + 0.5)
		idx = max(0, min(fn.size[0]-1, idx))
		out := make([]float64, n)
		maxv := float64(int(1)<<uint(fn.bps) - 1)
		for k := 0; k < n; k++ {
			bit := (idx*n + k) * fn.bps
			v := 0
			for b := 0; b < fn.bps; b++ {
				byteI := (bit + b) / 8
				if byteI >= len(fn.samples) {
					break
				}
				v = v<<1 | int(fn.samples[byteI]>>(7-uint((bit+b)%8))&1)
			}
			d0, d1 := fn.rng[2*k], fn.rng[2*k+1]
			if 2*k+1 < len(fn.decode) {
				d0, d1 = fn.decode[2*k], fn.decode[2*k+1]
			}
			out[k] = d0 + float64(v)/maxv*(d1-d0)
		}
		return out
	}
	return []float64{1 - x}
}

// ---------------------------------------------------------------- интерпретатор
func (r *pdfRenderer) run(content []byte) {
	l := &pdfLexer{b: content}
	var ops []any
	for {
		r.ops++
		if r.ops > 3_000_000 {
			return
		}
		t, err := l.token()
		if err != nil {
			return
		}
		if kw, ok := t.(pdfKeyword); ok {
			switch kw {
			case "[", "<<":
				o, _ := l.finish(t)
				ops = append(ops, o)
				continue
			case "BI":
				r.inlineImage(l)
				ops = ops[:0]
				continue
			}
			r.op(string(kw), ops)
			ops = ops[:0]
			continue
		}
		ops = append(ops, t)
	}
}

func nums(ops []any) []float64 {
	out := make([]float64, 0, len(ops))
	for _, o := range ops {
		if v, ok := o.(float64); ok {
			out = append(out, v)
		}
	}
	return out
}

func (r *pdfRenderer) dev(x, y float64) (float64, float64) { return r.gs.ctm.apply(x, y) }

func (r *pdfRenderer) op(op string, ops []any) {
	n := nums(ops)
	a := func(i int) float64 {
		if i < len(n) {
			return n[i]
		}
		return 0
	}
	g := &r.gs
	switch op {
	case "q":
		if len(r.stack) < 256 {
			r.stack = append(r.stack, *g)
		}
	case "Q":
		if len(r.stack) > 0 {
			r.gs = r.stack[len(r.stack)-1]
			r.stack = r.stack[:len(r.stack)-1]
		}
	case "cm":
		if len(n) >= 6 {
			g.ctm = pdfMat{n[0], n[1], n[2], n[3], n[4], n[5]}.mul(g.ctm)
		}
	case "w":
		g.lw = a(0)
	case "J":
		g.cap = int(a(0))
	case "j":
		g.join = int(a(0))
	case "d":
		g.dash = nil
		if len(ops) > 0 {
			if arr, ok := ops[0].(pdfArray); ok {
				for _, x := range arr {
					if v, ok := x.(float64); ok {
						g.dash = append(g.dash, v)
					}
				}
			}
		}
		g.dashPhase = a(0)
	case "gs":
		if len(ops) > 0 {
			if nm, ok := ops[0].(pdfName); ok {
				r.extGState(r.f.dict(r.resource("ExtGState", string(nm))))
			}
		}
	case "m":
		r.path.tol = 0.25
		x, y := r.dev(a(0), a(1))
		r.path.moveTo(x, y)
	case "l":
		x, y := r.dev(a(0), a(1))
		r.path.lineTo(x, y)
	case "c":
		x1, y1 := r.dev(a(0), a(1))
		x2, y2 := r.dev(a(2), a(3))
		x3, y3 := r.dev(a(4), a(5))
		r.path.tol = 0.25
		r.path.curveTo(x1, y1, x2, y2, x3, y3)
	case "v":
		x2, y2 := r.dev(a(0), a(1))
		x3, y3 := r.dev(a(2), a(3))
		r.path.tol = 0.25
		r.path.curveTo(r.path.x, r.path.y, x2, y2, x3, y3)
	case "y":
		x1, y1 := r.dev(a(0), a(1))
		x3, y3 := r.dev(a(2), a(3))
		r.path.tol = 0.25
		r.path.curveTo(x1, y1, x3, y3, x3, y3)
	case "h":
		r.path.closePath()
	case "re":
		x, y, w, h := a(0), a(1), a(2), a(3)
		x0, y0 := r.dev(x, y)
		r.path.moveTo(x0, y0)
		x1, y1 := r.dev(x+w, y)
		r.path.lineTo(x1, y1)
		x2, y2 := r.dev(x+w, y+h)
		r.path.lineTo(x2, y2)
		x3, y3 := r.dev(x, y+h)
		r.path.lineTo(x3, y3)
		r.path.closePath()
	case "S", "s":
		if op == "s" {
			r.path.closePath()
		}
		r.strokeCur()
		r.endPath()
	case "f", "F", "f*":
		r.fillCur(op == "f*")
		r.endPath()
	case "B", "B*", "b", "b*":
		if op == "b" || op == "b*" {
			r.path.closePath()
		}
		r.fillCur(op == "B*" || op == "b*")
		r.strokeCur()
		r.endPath()
	case "n":
		r.endPath()
	case "W":
		r.pendClip = 1
	case "W*":
		r.pendClip = 2
	case "CS", "cs":
		cs := devGray
		if len(ops) > 0 {
			cs = r.colorSpace(ops[0], 0)
		}
		def := make([]float64, cs.n)
		if cs.kind == "cmyk" {
			def[3] = 1
		}
		if op == "CS" {
			g.strokeCS, g.stroke = cs, cs.rgb(def)
		} else {
			g.fillCS, g.fill, g.fillPat = cs, cs.rgb(def), nil
		}
	case "SC", "SCN":
		if g.strokeCS.kind != "pattern" {
			g.stroke = g.strokeCS.rgb(n)
		}
	case "sc", "scn":
		if g.fillCS.kind == "pattern" {
			if len(ops) > 0 {
				if nm, ok := ops[len(ops)-1].(pdfName); ok {
					g.fillPat = r.resource("Pattern", string(nm))
					g.fill = r.patternColor(g.fillPat)
				}
			}
		} else {
			g.fill = g.fillCS.rgb(n)
		}
	case "G":
		g.strokeCS, g.stroke = devGray, devGray.rgb(n)
	case "g":
		g.fillCS, g.fill, g.fillPat = devGray, devGray.rgb(n), nil
	case "RG":
		g.strokeCS, g.stroke = devRGB, devRGB.rgb(n)
	case "rg":
		g.fillCS, g.fill, g.fillPat = devRGB, devRGB.rgb(n), nil
	case "K":
		g.strokeCS, g.stroke = devCMYK, devCMYK.rgb(n)
	case "k":
		g.fillCS, g.fill, g.fillPat = devCMYK, devCMYK.rgb(n), nil
	case "sh":
		if len(ops) > 0 {
			if nm, ok := ops[0].(pdfName); ok {
				r.shading(r.resource("Shading", string(nm)), nil)
			}
		}
	case "Do":
		if len(ops) > 0 {
			if nm, ok := ops[0].(pdfName); ok {
				r.xobject(r.resource("XObject", string(nm)))
			}
		}
	// текст
	case "BT":
		r.tm, r.tlm = identMat, identMat
	case "ET":
	case "Tc":
		g.tc = a(0)
	case "Tw":
		g.tw = a(0)
	case "Tz":
		g.th = a(0) / 100
	case "TL":
		g.tl = a(0)
	case "Ts":
		g.rise = a(0)
	case "Tr":
		g.tmode = int(a(0))
	case "Tf":
		if len(ops) > 0 {
			if nm, ok := ops[0].(pdfName); ok {
				g.font = r.loadFont(r.resource("Font", string(nm)))
			}
		}
		g.fs = a(0)
	case "Td":
		r.tlm = pdfMat{1, 0, 0, 1, a(0), a(1)}.mul(r.tlm)
		r.tm = r.tlm
	case "TD":
		g.tl = -a(1)
		r.tlm = pdfMat{1, 0, 0, 1, a(0), a(1)}.mul(r.tlm)
		r.tm = r.tlm
	case "Tm":
		if len(n) >= 6 {
			r.tlm = pdfMat{n[0], n[1], n[2], n[3], n[4], n[5]}
			r.tm = r.tlm
		}
	case "T*":
		r.tlm = pdfMat{1, 0, 0, 1, 0, -g.tl}.mul(r.tlm)
		r.tm = r.tlm
	case "Tj":
		if len(ops) > 0 {
			if s, ok := ops[0].(pdfString); ok {
				r.showText(s)
			}
		}
	case "'":
		r.op("T*", nil)
		if len(ops) > 0 {
			if s, ok := ops[len(ops)-1].(pdfString); ok {
				r.showText(s)
			}
		}
	case "\"":
		if len(n) >= 2 {
			g.tw, g.tc = n[0], n[1]
		}
		r.op("T*", nil)
		if len(ops) > 0 {
			if s, ok := ops[len(ops)-1].(pdfString); ok {
				r.showText(s)
			}
		}
	case "TJ":
		if len(ops) > 0 {
			if arr, ok := ops[0].(pdfArray); ok {
				for _, x := range arr {
					switch v := x.(type) {
					case pdfString:
						r.showText(v)
					case float64:
						tx := -v / 1000 * g.fs * g.th
						r.tm = pdfMat{1, 0, 0, 1, tx, 0}.mul(r.tm)
					}
				}
			}
		}
	case "d0", "d1":
	}
}

func (r *pdfRenderer) extGState(d pdfDict) {
	if d == nil {
		return
	}
	g := &r.gs
	if v, ok := d["LW"]; ok {
		g.lw = r.f.num(v)
	}
	if v, ok := d["LC"]; ok {
		g.cap = int(r.f.num(v))
	}
	if v, ok := d["LJ"]; ok {
		g.join = int(r.f.num(v))
	}
	if v, ok := d["ca"]; ok {
		g.fa = float32(r.f.num(v))
	}
	if v, ok := d["CA"]; ok {
		g.sa = float32(r.f.num(v))
	}
	if fa := r.f.arr(d["Font"]); len(fa) == 2 {
		g.font = r.loadFont(fa[0])
		g.fs = r.f.num(fa[1])
	}
}

func (r *pdfRenderer) endPath() {
	p := r.path.done()
	if r.pendClip != 0 && !r.textOnly {
		W, H := r.cv.w, r.cv.h
		if x0, y0, x1, y1, ok := isRect(p); ok && (r.gs.clip == nil || r.gs.clip.mask == nil) {
			c := &pdfClip{x0: 0, y0: 0, x1: float64(W), y1: float64(H)}
			if r.gs.clip != nil {
				*c = *r.gs.clip
			}
			c.x0, c.y0 = math.Max(c.x0, x0), math.Max(c.y0, y0)
			c.x1, c.y1 = math.Min(c.x1, x1), math.Min(c.y1, y1)
			r.gs.clip = c
		} else {
			r.gs.clip = intersectClip(r.gs.clip, rasterize(p, W, H, r.pendClip == 2), W, H)
		}
	}
	r.pendClip = 0
	r.path = pathBuilder{}
}

func (r *pdfRenderer) fillCur(evenOdd bool) {
	if r.textOnly {
		return
	}
	p := r.path.p
	pp := r.path
	p = (&pp).done()
	if r.gs.fillPat != nil {
		if pd := r.f.dict(r.gs.fillPat); pd != nil && int(r.f.num(pd["PatternType"])) == 2 {
			m := rasterize(p, r.cv.w, r.cv.h, evenOdd)
			if m != nil {
				r.shading(pd["Shading"], &shadeTarget{mask: m, m: r.f.numArray(pd["Matrix"])})
			}
			return
		}
	}
	m := rasterize(p, r.cv.w, r.cv.h, evenOdd)
	c := r.gs.fill
	r.cv.fillMask(m, c[0], c[1], c[2], r.gs.fa, r.gs.clip)
}

func (r *pdfRenderer) strokeCur() {
	if r.textOnly {
		return
	}
	pp := r.path
	p := (&pp).done()
	sc := r.gs.ctm.scale()
	w := r.gs.lw * sc
	if w < 1 {
		w = 1
	}
	var dash []float64
	for _, d := range r.gs.dash {
		dash = append(dash, d*sc)
	}
	sp := strokePath(p, w, r.gs.cap, r.gs.join, dash, r.gs.dashPhase*sc)
	m := rasterize(sp, r.cv.w, r.cv.h, false)
	c := r.gs.stroke
	a := r.gs.sa
	if r.gs.lw*sc < 1 && r.gs.lw > 0 {
		a *= float32(math.Max(0.35, r.gs.lw*sc))
	}
	r.cv.fillMask(m, c[0], c[1], c[2], a, r.gs.clip)
}

func (r *pdfRenderer) patternColor(p any) [3]uint8 {
	d := r.f.dict(p)
	if d == nil {
		return [3]uint8{128, 128, 128}
	}
	if int(r.f.num(d["PatternType"])) == 2 {
		sh := r.f.dict(d["Shading"])
		if sh != nil {
			cs := r.colorSpace(sh["ColorSpace"], 0)
			if fn := r.f.loadFunc(sh["Function"]); fn != nil {
				return cs.rgb(fn.eval([]float64{0.5}))
			}
		}
	}
	return [3]uint8{200, 200, 200}
}

// ---------------------------------------------------------------- градиенты
type shadeTarget struct {
	mask *covMask
	m    []float64
}

func (r *pdfRenderer) shading(v any, tgt *shadeTarget) {
	if r.textOnly {
		return
	}
	sh := r.f.dict(v)
	if sh == nil {
		return
	}
	typ := int(r.f.num(sh["ShadingType"]))
	cs := r.colorSpace(sh["ColorSpace"], 0)
	fn := r.f.loadFunc(sh["Function"])
	coords := r.f.numArray(sh["Coords"])
	dom := r.f.numArray(sh["Domain"])
	if len(dom) < 2 {
		dom = []float64{0, 1}
	}
	ext := r.f.arr(sh["Extend"])
	e0, e1 := len(ext) > 0 && ext[0] == true, len(ext) > 1 && ext[1] == true
	mat := r.gs.ctm
	if tgt != nil && len(tgt.m) == 6 {
		mat = pdfMat{tgt.m[0], tgt.m[1], tgt.m[2], tgt.m[3], tgt.m[4], tgt.m[5]}.mul(r.base)
	}
	inv, ok := mat.inv()
	if !ok || fn == nil || (typ != 2 && typ != 3) {
		if fn != nil && tgt != nil {
			c := cs.rgb(fn.eval([]float64{0.5}))
			r.cv.fillMask(tgt.mask, c[0], c[1], c[2], r.gs.fa, r.gs.clip)
		}
		return
	}
	// таблица цветов
	var lut [256][3]uint8
	for i := range lut {
		t := dom[0] + (dom[1]-dom[0])*float64(i)/255
		lut[i] = cs.rgb(fn.eval([]float64{t}))
	}
	x0, y0, x1, y1 := 0, 0, r.cv.w, r.cv.h
	if tgt != nil {
		x0, y0, x1, y1 = tgt.mask.x0, tgt.mask.y0, tgt.mask.x0+tgt.mask.w, tgt.mask.y0+tgt.mask.h
	}
	if cl := r.gs.clip; cl != nil {
		x0, y0 = max(x0, int(cl.x0)), max(y0, int(cl.y0))
		x1, y1 = min(x1, int(math.Ceil(cl.x1))), min(y1, int(math.Ceil(cl.y1)))
	}
	x0, y0 = max(0, x0), max(0, y0)
	x1, y1 = min(r.cv.w, x1), min(r.cv.h, y1)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			a := r.gs.fa
			if tgt != nil {
				m := tgt.mask
				a *= m.a[(y-m.y0)*m.w+x-m.x0]
			}
			if r.gs.clip != nil {
				a *= r.gs.clip.at(x, y)
			}
			if a <= 0.002 {
				continue
			}
			ux, uy := inv.apply(float64(x)+0.5, float64(y)+0.5)
			t := 0.0
			if typ == 2 && len(coords) >= 4 {
				dx, dy := coords[2]-coords[0], coords[3]-coords[1]
				den := dx*dx + dy*dy
				if den > 0 {
					t = ((ux-coords[0])*dx + (uy-coords[1])*dy) / den
				}
			} else if len(coords) >= 6 {
				// радиальный: упрощённо — по расстоянию от центра конечной окружности
				cx, cy, r0, r1 := coords[3], coords[4], coords[2], coords[5]
				d := math.Hypot(ux-cx, uy-cy)
				if r1 != r0 {
					t = (d - r0) / (r1 - r0)
				}
			}
			if t < 0 {
				if !e0 {
					continue
				}
				t = 0
			}
			if t > 1 {
				if !e1 {
					continue
				}
				t = 1
			}
			c := lut[int(t*255+0.5)]
			i := (y*r.cv.w + x) * 3
			ia := 1 - a
			r.cv.pix[i] = uint8(float32(r.cv.pix[i])*ia + float32(c[0])*a)
			r.cv.pix[i+1] = uint8(float32(r.cv.pix[i+1])*ia + float32(c[1])*a)
			r.cv.pix[i+2] = uint8(float32(r.cv.pix[i+2])*ia + float32(c[2])*a)
		}
	}
}

// ---------------------------------------------------------------- XObject
func (r *pdfRenderer) xobject(v any) {
	s := r.f.stream(v)
	if s == nil || r.depth > 12 {
		return
	}
	switch r.f.name(s.Dict["Subtype"]) {
	case "Form":
		save := r.gs
		saveStack := len(r.stack)
		if m := r.f.numArray(s.Dict["Matrix"]); len(m) == 6 {
			r.gs.ctm = pdfMat{m[0], m[1], m[2], m[3], m[4], m[5]}.mul(r.gs.ctm)
		}
		if bb := r.f.numArray(s.Dict["BBox"]); len(bb) == 4 && !r.textOnly {
			pb := pathBuilder{}
			x0, y0 := r.dev(bb[0], bb[1])
			pb.moveTo(x0, y0)
			x1, y1 := r.dev(bb[2], bb[1])
			pb.lineTo(x1, y1)
			x2, y2 := r.dev(bb[2], bb[3])
			pb.lineTo(x2, y2)
			x3, y3 := r.dev(bb[0], bb[3])
			pb.lineTo(x3, y3)
			pb.closePath()
			r.path = pb
			r.pendClip = 1
			r.endPath()
		}
		res := r.f.dict(s.Dict["Resources"])
		r.res = append(r.res, res)
		r.depth++
		data, _ := r.f.decodeStream(s, false)
		r.run(data)
		r.depth--
		r.res = r.res[:len(r.res)-1]
		r.gs = save
		if len(r.stack) > saveStack {
			r.stack = r.stack[:saveStack]
		}
	case "Image":
		if r.textOnly {
			return
		}
		im := r.decodeImage(s)
		if im != nil {
			r.cv.drawImage(im, r.gs.ctm, r.gs.clip, r.gs.fill, r.gs.fa)
		}
	}
}

func (r *pdfRenderer) decodeImage(s *pdfStream) *pdfImage {
	key := s.ref.N
	d := r.doc
	touch := func() {
		for i, k := range d.imgLRU {
			if k == key {
				d.imgLRU = append(d.imgLRU[:i], d.imgLRU[i+1:]...)
				break
			}
		}
		d.imgLRU = append(d.imgLRU, key)
	}
	if key > 0 {
		d.imgMu.Lock()
		if im, ok := d.images[key]; ok {
			touch()
			d.imgMu.Unlock()
			return im
		}
		d.imgMu.Unlock()
	}
	im := r.imageFromDict(s.Dict, s)
	if im != nil && key > 0 {
		d.imgMu.Lock()
		d.images[key] = im
		touch()
		// держим не больше ~200 МБ картинок: у сканированных книг каждая страница — отдельная картинка
		total := 0
		for _, x := range d.images {
			total += pdfImageBytes(x)
		}
		for total > pdfImgCacheBytes && len(d.imgLRU) > 1 {
			old := d.imgLRU[0]
			d.imgLRU = d.imgLRU[1:]
			total -= pdfImageBytes(d.images[old])
			delete(d.images, old)
		}
		d.imgMu.Unlock()
	}
	return im
}

const pdfImgCacheBytes = 200 << 20

func (r *pdfRenderer) imageFromDict(d pdfDict, s *pdfStream) *pdfImage {
	f := r.f
	get := func(k, short string) any {
		if v, ok := d[k]; ok {
			return v
		}
		return d[short]
	}
	w := int(f.num(get("Width", "W")))
	h := int(f.num(get("Height", "H")))
	if w <= 0 || h <= 0 || w*h > 60_000_000 {
		return nil
	}
	isMask := false
	if v, ok := f.Resolve(get("ImageMask", "IM")).(bool); ok && v {
		isMask = true
	}
	bpc := int(f.num(get("BitsPerComponent", "BPC")))
	if bpc == 0 {
		bpc = 8
	}
	if isMask {
		bpc = 1
	}
	data, _ := f.decodeStream(s, false)
	filt := f.imageFilter(s)
	switch filt {
	case "CCITTFaxDecode", "CCF":
		pp := f.filterParms(s)
		cp := CCITTParams{K: int(f.num(pp["K"])), Columns: w, Rows: h}
		if v, ok := pp["Columns"]; ok {
			cp.Columns = int(f.num(v))
		}
		if v, ok := pp["Rows"]; ok && f.num(v) > 0 {
			cp.Rows = int(f.num(v))
		}
		cp.BlackIs1, _ = f.Resolve(pp["BlackIs1"]).(bool)
		cp.EncodedByteAlign, _ = f.Resolve(pp["EncodedByteAlign"]).(bool)
		data = ccittDecode(data, cp)
		filt, bpc = "", 1
	case "JBIG2Decode":
		pp := f.filterParms(s)
		var globals []byte
		if gs := f.stream(pp["JBIG2Globals"]); gs != nil {
			globals, _ = f.decodeStream(gs, false)
		}
		data = jbig2Decode(data, globals, w, h)
		filt, bpc = "", 1
	}
	// чёрно-белые сканы (факс, JBIG2, 1 бит): быстрый путь, огромные страницы сразу уменьшаем
	if filt == "" && bpc == 1 {
		cs := devGray
		if !isMask {
			cs = r.colorSpace(get("ColorSpace", "CS"), 0)
		}
		if cs.n == 1 && cs.kind != "indexed" {
			dec := f.numArray(get("Decode", "D"))
			inv := len(dec) >= 2 && dec[0] == 1
			im := bilevelImage(data, w, h, inv, isMask)
			r.attachMask(im, d)
			return im
		}
	}
	var im *pdfImage
	if filt != "DCTDecode" && filt != "DCT" {
		im = &pdfImage{w: w, h: h, rgb: make([]uint8, w*h*3), mask: isMask}
	}
	switch filt {
	case "DCTDecode", "DCT":
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		if fim := fastJPEG(img); fim != nil {
			fim = shrinkBig(fim)
			fim.mask = isMask
			r.attachMask(fim, d)
			return fim
		}
		im = &pdfImage{mask: isMask}
		b := img.Bounds()
		im.w, im.h = b.Dx(), b.Dy()
		im.rgb = make([]uint8, im.w*im.h*3)
		cmyk, isCMYK := img.(*image.CMYK)
		for y := 0; y < im.h; y++ {
			for x := 0; x < im.w; x++ {
				i := (y*im.w + x) * 3
				if isCMYK {
					c := cmyk.CMYKAt(b.Min.X+x, b.Min.Y+y)
					// у Adobe CMYK в JPEG значения инвертированы
					cc, mm, yy, kk := 255-c.C, 255-c.M, 255-c.Y, 255-c.K
					im.rgb[i] = uint8(int(255-cc) * int(255-kk) / 255)
					im.rgb[i+1] = uint8(int(255-mm) * int(255-kk) / 255)
					im.rgb[i+2] = uint8(int(255-yy) * int(255-kk) / 255)
					continue
				}
				rr, gg, bb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				im.rgb[i], im.rgb[i+1], im.rgb[i+2] = uint8(rr>>8), uint8(gg>>8), uint8(bb>>8)
			}
		}
	case "JPXDecode", "JBIG2Decode", "CCITTFaxDecode":
		// пока не умеем — светлая заглушка
		for i := range im.rgb {
			im.rgb[i] = 235
		}
		return im
	default:
		cs := devGray
		if !isMask {
			cs = r.colorSpace(get("ColorSpace", "CS"), 0)
		}
		nc := cs.n
		dec := f.numArray(get("Decode", "D"))
		maxv := float64(int(1)<<uint(bpc) - 1)
		stride := (w*nc*bpc + 7) / 8
		comp := make([]float64, nc)
		idx := cs.kind == "indexed"
		// быстрые пути
		fastRGB := cs.kind == "rgb" && bpc == 8 && dec == nil
		fastGray := cs.kind == "gray" && bpc == 8 && dec == nil
		for y := 0; y < h; y++ {
			row := y * stride
			for x := 0; x < w; x++ {
				o := (y*w + x) * 3
				if fastRGB {
					p := row + x*3
					if p+2 < len(data) {
						im.rgb[o], im.rgb[o+1], im.rgb[o+2] = data[p], data[p+1], data[p+2]
					}
					continue
				}
				if fastGray {
					p := row + x
					if p < len(data) {
						v := data[p]
						im.rgb[o], im.rgb[o+1], im.rgb[o+2] = v, v, v
					}
					continue
				}
				for k := 0; k < nc; k++ {
					bit := (x*nc + k) * bpc
					v := 0
					if bpc == 8 {
						p := row + bit/8
						if p < len(data) {
							v = int(data[p])
						}
					} else if bpc == 16 {
						p := row + bit/8
						if p+1 < len(data) {
							v = int(data[p])<<8 | int(data[p+1])
						}
					} else {
						p := row + bit/8
						if p < len(data) {
							v = int(data[p]>>uint(8-bpc-bit%8)) & (1<<uint(bpc) - 1)
						}
					}
					fv := float64(v)
					if idx {
						if len(dec) >= 2 {
							fv = dec[0] + fv*(dec[1]-dec[0])/maxv
						}
						comp[k] = fv
					} else {
						d0, d1 := 0.0, 1.0
						if 2*k+1 < len(dec) {
							d0, d1 = dec[2*k], dec[2*k+1]
						}
						comp[k] = d0 + fv/maxv*(d1-d0)
						if cs.kind == "lab" && k == 0 && dec == nil {
							comp[k] *= 100
						}
					}
				}
				if isMask {
					// 1 бит: 0 — рисовать (по умолчанию Decode [0 1])
					on := comp[0] < 0.5
					if len(dec) >= 2 && dec[0] == 1 {
						on = !on
					}
					v := uint8(255)
					if on {
						v = 0
					}
					im.rgb[o], im.rgb[o+1], im.rgb[o+2] = v, v, v
					continue
				}
				c := cs.rgb(comp)
				im.rgb[o], im.rgb[o+1], im.rgb[o+2] = c[0], c[1], c[2]
			}
		}
	}
	im = shrinkBig(im)
	r.attachMask(im, d)
	return im
}

// attachMask — мягкая маска (SMask) или маска (Mask) картинки.
func (r *pdfRenderer) attachMask(im *pdfImage, d pdfDict) {
	f := r.f
	if sm := f.stream(d["SMask"]); sm != nil {
		if mi := r.imageFromDict(sm.Dict, sm); mi != nil {
			im.a = make([]uint8, im.w*im.h)
			for y := 0; y < im.h; y++ {
				for x := 0; x < im.w; x++ {
					sx := x * mi.w / im.w
					sy := y * mi.h / im.h
					im.a[y*im.w+x] = mi.rgb[(sy*mi.w+sx)*3]
				}
			}
		}
	} else if mk := f.stream(d["Mask"]); mk != nil {
		if mi := r.imageFromDict(mk.Dict, mk); mi != nil {
			im.a = make([]uint8, im.w*im.h)
			for y := 0; y < im.h; y++ {
				for x := 0; x < im.w; x++ {
					sx := x * mi.w / im.w
					sy := y * mi.h / im.h
					im.a[y*im.w+x] = 255 - mi.rgb[(sy*mi.w+sx)*3]
				}
			}
		}
	}
}

func (r *pdfRenderer) inlineImage(l *pdfLexer) {
	d := pdfDict{}
	for {
		k, err := l.token()
		if err != nil {
			return
		}
		if kw, ok := k.(pdfKeyword); ok && kw == "ID" {
			break
		}
		name, ok := k.(pdfName)
		if !ok {
			continue
		}
		v, _ := l.object()
		d[string(name)] = v
	}
	start := l.p + 1
	end := bytes.Index(l.b[start:], []byte("EI"))
	for end >= 0 {
		e := start + end
		if (e == 0 || isWS(l.b[e-1])) && (e+2 >= len(l.b) || isWS(l.b[e+2])) {
			break
		}
		nx := bytes.Index(l.b[e+2:], []byte("EI"))
		if nx < 0 {
			end = -1
			break
		}
		end = e + 2 + nx - start
	}
	if end < 0 {
		l.p = len(l.b)
		return
	}
	raw := l.b[start : start+end]
	l.p = start + end + 2
	if r.textOnly {
		return
	}
	// краткие имена фильтров и пространств
	if v, ok := d["F"]; ok {
		d["Filter"] = v
	}
	if v, ok := d["DP"]; ok {
		d["DecodeParms"] = v
	}
	if cs, ok := d["CS"].(pdfName); ok {
		switch cs {
		case "G":
			d["CS"] = pdfName("DeviceGray")
		case "RGB":
			d["CS"] = pdfName("DeviceRGB")
		case "CMYK":
			d["CS"] = pdfName("DeviceCMYK")
		case "I":
			d["CS"] = pdfName("Indexed")
		}
	}
	s := &pdfStream{Dict: d, Raw: raw}
	if fn, ok := d["Filter"].(pdfName); ok {
		switch fn {
		case "AHx":
			d["Filter"] = pdfName("ASCIIHexDecode")
		case "A85":
			d["Filter"] = pdfName("ASCII85Decode")
		case "Fl":
			d["Filter"] = pdfName("FlateDecode")
		case "DCT":
			d["Filter"] = pdfName("DCTDecode")
		case "RL":
			d["Filter"] = pdfName("RunLengthDecode")
		}
	}
	im := r.imageFromDict(d, s)
	if im != nil {
		r.cv.drawImage(im, r.gs.ctm, r.gs.clip, r.gs.fill, r.gs.fa)
	}
}

// ---------------------------------------------------------------- текст
func (r *pdfRenderer) showText(s pdfString) {
	g := &r.gs
	ft := g.font
	if ft == nil {
		return
	}
	for _, code := range ft.codes(s) {
		w0 := ft.width(code) / 1000
		if ft.kind == "t3" {
			w0 = ft.width(code) * ft.t3matrix[0]
		}
		if w0 == 0 && ft.kind == "sys" {
			w0 = ft.sysAdvance(code)
		}
		trm := pdfMat{g.fs * g.th, 0, 0, g.fs, 0, g.rise}.mul(r.tm).mul(g.ctm)
		// текст для поиска
		uni := ft.unicode(code)
		if uni != "" {
			x0, y0 := trm.apply(0, 0)
			x1, y1 := trm.apply(w0, 0)
			xh, yh := trm.apply(0, 0.85)
			sc := r.scale
			gx := math.Min(x0, x1) / sc
			gw := math.Max(math.Abs(x1-x0), math.Abs(y1-y0)) / sc
			gh := math.Hypot(xh-x0, yh-y0) / sc
			gy := math.Min(y0, yh) / sc
			if math.Abs(x1-x0) < math.Abs(y1-y0) {
				gx = math.Min(x0, xh) / sc
				gy = math.Min(y0, y1) / sc
				gw, gh = gh, gw
			}
			r.text = append(r.text, PdfGlyph{S: uni, X: gx, Y: gy, W: gw, H: gh})
		}
		if !r.textOnly && g.tmode != 3 && g.tmode != 7 {
			r.drawGlyph(ft, code, trm)
		}
		tx := (w0*g.fs + g.tc) * g.th
		if code == 32 && len(ft.codeLens) > 0 && ft.codeLens[0].n == 1 {
			tx += g.tw * g.th
		}
		r.tm = pdfMat{1, 0, 0, 1, tx, 0}.mul(r.tm)
	}
}

func (r *pdfRenderer) drawGlyph(ft *pdfFont, code int, trm pdfMat) {
	g := &r.gs
	if ft.kind == "t3" {
		r.type3Glyph(ft, code, trm)
		return
	}
	gp := ft.glyphFor(code)
	if gp == nil || len(gp.ops) == 0 {
		return
	}
	col := g.fill
	alpha := g.fa
	if g.tmode == 1 {
		col, alpha = g.stroke, g.sa
	}
	// прямой (без поворота) глиф — кэшируем растровую маску
	if trm[1] == 0 && trm[2] == 0 && trm[0] > 0 && trm[3] < 0 && -trm[3] < 400 {
		fx := int32(math.Floor((trm[4] - math.Floor(trm[4])) * 4))
		fy := int32(math.Floor((trm[5] - math.Floor(trm[5])) * 4))
		key := glyphKeyP{ft, code, int32(trm[0] * 16), int32(-trm[3] * 16), fx, fy}
		m, ok := r.glyphCache[key]
		if !ok {
			ox := math.Floor(trm[4]) + float64(fx)/4
			oy := math.Floor(trm[5]) + float64(fy)/4
			lm := pdfMat{trm[0], 0, 0, trm[3], float64(fx) / 4, float64(fy) / 4}
			_ = ox
			_ = oy
			// рисуем относительно (0,0) со сдвигом, затем переносим
			m = r.glyphMask(gp, lm, 512)
			r.glyphCache[key] = m
		}
		if m == nil {
			return
		}
		sh := &covMask{x0: m.x0 + int(math.Floor(trm[4])), y0: m.y0 + int(math.Floor(trm[5])), w: m.w, h: m.h, a: m.a}
		r.cv.fillMask(sh, col[0], col[1], col[2], alpha, g.clip)
		return
	}
	m := r.glyphMask(gp, trm, 0)
	r.cv.fillMask(m, col[0], col[1], col[2], alpha, g.clip)
}

// glyphMask: off > 0 — растр в локальных координатах со сдвигом (для кэша).
func (r *pdfRenderer) glyphMask(gp *gpath, m pdfMat, off int) *covMask {
	pb := pathBuilder{tol: 0.15}
	k := 0
	o := float64(off)
	tr := func(x, y float64) (float64, float64) {
		a, b := m.apply(x, y)
		return a + o, b + o
	}
	for _, op := range gp.ops {
		switch op {
		case 'M':
			pb.moveTo(tr(gp.pts[k], gp.pts[k+1]))
			k += 2
		case 'L':
			pb.lineTo(tr(gp.pts[k], gp.pts[k+1]))
			k += 2
		case 'Q':
			x1, y1 := tr(gp.pts[k], gp.pts[k+1])
			x2, y2 := tr(gp.pts[k+2], gp.pts[k+3])
			pb.quadTo(x1, y1, x2, y2)
			k += 4
		case 'C':
			x1, y1 := tr(gp.pts[k], gp.pts[k+1])
			x2, y2 := tr(gp.pts[k+2], gp.pts[k+3])
			x3, y3 := tr(gp.pts[k+4], gp.pts[k+5])
			pb.curveTo(x1, y1, x2, y2, x3, y3)
			k += 6
		case 'Z':
			pb.closePath()
		}
	}
	W, H := r.cv.w, r.cv.h
	if off > 0 {
		W, H = 2*off, 2*off
	}
	cm := rasterize(pb.done(), W, H, false)
	if cm != nil && off > 0 {
		cm.x0 -= off
		cm.y0 -= off
	}
	return cm
}

func (r *pdfRenderer) type3Glyph(ft *pdfFont, code int, trm pdfMat) {
	if r.depth > 8 || ft.t3procs == nil || code > 255 {
		return
	}
	name := ft.enc[code]
	s := r.f.stream(ft.t3procs[name])
	if s == nil {
		return
	}
	save := r.gs
	saveStack := len(r.stack)
	saveTm, saveTlm := r.tm, r.tlm
	fm := pdfMat(ft.t3matrix)
	r.gs.ctm = fm.mul(trm)
	r.res = append(r.res, ft.t3res)
	r.depth++
	data, _ := r.f.decodeStream(s, false)
	r.run(data)
	r.depth--
	r.res = r.res[:len(r.res)-1]
	r.gs = save
	if len(r.stack) > saveStack {
		r.stack = r.stack[:saveStack]
	}
	r.tm, r.tlm = saveTm, saveTlm
}

// PageText — текст страницы одной строкой (для поиска) и соответствие символов глифам.
func PageText(gl []PdfGlyph) (string, []int) {
	var sb strings.Builder
	var idx []int
	for i, g := range gl {
		if i > 0 {
			p := gl[i-1]
			// новая строка или пробел между словами
			if math.Abs(g.Y-p.Y) > p.H*0.6 {
				sb.WriteString("\n")
				idx = append(idx, -1)
			} else if g.X-(p.X+p.W) > p.H*0.15 && !strings.HasSuffix(p.S, " ") && g.S != " " {
				sb.WriteString(" ")
				idx = append(idx, -1)
			}
		}
		for range []rune(g.S) {
			idx = append(idx, i)
		}
		sb.WriteString(g.S)
	}
	return sb.String(), idx
}
