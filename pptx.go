package main

// Просмотр презентаций PowerPoint (.pptx): слайды рисуются векторами в PDF и открываются в читалке
// (там уже есть миниатюры, масштаб, поиск, выделение текста и печать).
//
// Поддержано: размер слайда, фон, фигуры (прямоугольник, скруглённый, эллипс, линия, соединители) с заливкой
// и обводкой, текст в фигурах (перенос, выравнивание, отступы, маркеры, интервалы, якорь по вертикали),
// группы, картинки, таблицы, диаграммы (точечная, график, гистограмма, круговая — упрощённо),
// цвета темы с lumMod/lumOff/tint/shade, шрифты темы, наследование мест заполнителей от макета и образца.
// Не поддержано: анимации, эффекты (тени, градиенты — берётся первый цвет), поворот текста, SmartArt.

import (
	"encoding/xml"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

const emuPt = 12700.0 // EMU в пункте

type pptx struct {
	files    map[string][]byte
	theme    map[string]Color // dk1, lt1, accent1…
	clrMap   map[string]string
	major    string // шрифт заголовков темы
	minor    string // шрифт текста темы
	w, h     float64
	out      *pdfOut
	nodeOf   map[string]*Node
	txStyles *Node   // стили текста образца
	defStyle *Node   // defaultTextStyle презентации
	defTxt   float64 // размер текста по умолчанию
	titleSz  float64
	bodySz   float64
	otherSz  float64
}

func (x *pptx) xml(p string) *Node {
	if n, ok := x.nodeOf[p]; ok {
		return n
	}
	var n *Node
	if data, ok := x.files[p]; ok {
		if r, _ := ParseXML(data); r != nil {
			n = r.Root()
		}
	}
	x.nodeOf[p] = n
	return n
}

// relTarget — цель связи id у части part.
func (x *pptx) relTarget(part, id string) string {
	if r, ok := readRels(x.files, part)[id]; ok && !r.external {
		return r.target
	}
	return ""
}

func (x *pptx) relOfType(part, typ string) string {
	for _, r := range readRels(x.files, part) {
		if r.typ == typ && !r.external {
			return r.target
		}
	}
	return ""
}

func emu(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v / emuPt
}

func hexColor(s string) (Color, bool) {
	if len(s) != 6 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return Hex(uint32(v)), true
}

// ---------------------------------------------------------------- цвета

// color — цвет из узла-родителя (solidFill, ln, …): srgbClr, schemeClr, sysClr, prstClr с модификаторами.
func (x *pptx) color(parent *Node) (Color, bool) {
	if parent == nil {
		return 0, false
	}
	for _, c := range parent.Elems() {
		var base Color
		ok := false
		switch c.Name {
		case "srgbClr":
			base, ok = hexColor(c.Av("val"))
		case "sysClr":
			base, ok = hexColor(c.Av("lastClr"))
			if !ok {
				base, ok = map[string]Color{"windowText": Hex(0), "window": Hex(0xffffff)}[c.Av("val")]
			}
		case "schemeClr":
			name := c.Av("val")
			if m, okm := x.clrMap[name]; okm {
				name = m
			}
			base, ok = x.theme[name]
		case "prstClr":
			base, ok = map[string]Color{"black": Hex(0), "white": Hex(0xffffff), "red": Hex(0xff0000), "green": Hex(0x008000),
				"blue": Hex(0x0000ff), "gray": Hex(0x808080), "yellow": Hex(0xffff00)}[c.Av("val")]
		}
		if ok {
			return applyColorMods(base, c), true
		}
	}
	return 0, false
}

// applyColorMods — lumMod/lumOff (в HSL), tint/shade, alpha.
func applyColorMods(c Color, n *Node) Color {
	r, g, b := float64(uint32(c)>>16&255)/255, float64(uint32(c)>>8&255)/255, float64(uint32(c)&255)/255
	a := 1.0
	pct := func(m *Node) float64 { v, _ := strconv.ParseFloat(m.Av("val"), 64); return v / 100000 }
	for _, m := range n.Elems() {
		switch m.Name {
		case "lumMod", "lumOff":
			h, s, l := rgbToHSL(r, g, b)
			if m.Name == "lumMod" {
				l *= pct(m)
			} else {
				l += pct(m)
			}
			r, g, b = hslToRGB(h, s, math.Max(0, math.Min(1, l)))
		case "tint": // к белому
			t := pct(m)
			r, g, b = 1-(1-r)*t, 1-(1-g)*t, 1-(1-b)*t
		case "shade": // к чёрному
			t := pct(m)
			r, g, b = r*t, g*t, b*t
		case "alpha":
			a = pct(m)
		}
	}
	to := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255)) }
	return RGBA(to(r), to(g), to(b), to(a))
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

func hslToRGB(h, s, l float64) (float64, float64, float64) {
	if s == 0 {
		return l, l, l
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	f := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return f(h + 1.0/3), f(h), f(h - 1.0/3)
}

// fill — заливка из spPr (solidFill, gradFill — первый цвет, noFill).
func (x *pptx) fill(spPr *Node) (*Color, bool) {
	if spPr == nil {
		return nil, false
	}
	if spPr.Kid("noFill") != nil {
		return nil, true
	}
	if sf := spPr.Kid("solidFill"); sf != nil {
		if c, ok := x.color(sf); ok {
			return &c, true
		}
	}
	if gf := spPr.Kid("gradFill"); gf != nil {
		if gs := gf.First("gs"); gs != nil {
			if c, ok := x.color(gs); ok {
				return &c, true
			}
		}
	}
	return nil, false
}

// line — обводка из spPr/ln.
func (x *pptx) line(spPr *Node) (pdfStyle, bool) {
	ln := spPr.Kid("ln")
	if ln == nil {
		return pdfStyle{}, false
	}
	if ln.Kid("noFill") != nil {
		return pdfStyle{}, true
	}
	st := pdfStyle{W: 0.75}
	if w := ln.Av("w"); w != "" {
		st.W = emu(w)
	}
	if c, ok := x.color(ln.Kid("solidFill")); ok {
		st.Stroke = &c
	} else {
		return pdfStyle{}, false
	}
	if d := ln.Kid("prstDash"); d != nil {
		u := math.Max(1, st.W)
		switch d.Av("val") {
		case "dash", "sysDash", "lgDash":
			st.Dash = []float64{4 * u, 3 * u}
		case "dot", "sysDot":
			st.Dash = []float64{u, 2 * u}
		case "dashDot", "sysDashDot", "lgDashDot":
			st.Dash = []float64{4 * u, 2 * u, u, 2 * u}
		}
	}
	return st, true
}

// styleRef — цвета из p:style (fillRef, lnRef) — так рисуют фигуры, вставленные «как есть».
func (x *pptx) styleRef(sp *Node, kind string) (*Color, bool) {
	st := sp.Kid("style")
	if st == nil {
		return nil, false
	}
	ref := st.Kid(kind)
	if ref == nil || ref.Av("idx") == "0" {
		return nil, false
	}
	if c, ok := x.color(ref); ok {
		return &c, true
	}
	return nil, false
}

// ---------------------------------------------------------------- геометрия

type xform struct {
	off, ext     [2]float64
	chOff, chExt [2]float64
	flipH, flipV bool
	has          bool
}

func readXfrm(n *Node) xform {
	var t xform
	if n == nil {
		return t
	}
	if o := n.Kid("off"); o != nil {
		t.off = [2]float64{emu(o.Av("x")), emu(o.Av("y"))}
		t.has = true
	}
	if e := n.Kid("ext"); e != nil {
		t.ext = [2]float64{emu(e.Av("cx")), emu(e.Av("cy"))}
	}
	if o := n.Kid("chOff"); o != nil {
		t.chOff = [2]float64{emu(o.Av("x")), emu(o.Av("y"))}
	}
	if e := n.Kid("chExt"); e != nil {
		t.chExt = [2]float64{emu(e.Av("cx")), emu(e.Av("cy"))}
	}
	t.flipH, t.flipV = n.Av("flipH") == "1", n.Av("flipV") == "1"
	return t
}

// mapper — перевод координат группы в координаты слайда.
type mapper func(x, y float64) (float64, float64)

func identity(x, y float64) (float64, float64) { return x, y }

func (m mapper) rect(t xform) Rect {
	x0, y0 := m(t.off[0], t.off[1])
	x1, y1 := m(t.off[0]+t.ext[0], t.off[1]+t.ext[1])
	return Rect{math.Min(x0, x1), math.Min(y0, y1), math.Abs(x1 - x0), math.Abs(y1 - y0)}
}

// ---------------------------------------------------------------- слайды

func isPptx(p string) bool {
	e := strings.ToLower(filepath.Ext(p))
	return e == ".pptx" || e == ".ppsx" || e == ".potx"
}

// PptxToPDF — презентация как PDF (по листу на слайд).
func PptxToPDF(data []byte) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("презентация: %v", r)
		}
	}()
	files, err := unzipAll(data)
	if err != nil {
		return nil, err
	}
	x := &pptx{files: files, theme: map[string]Color{}, clrMap: map[string]string{}, nodeOf: map[string]*Node{},
		w: 720, h: 540, major: "Calibri", minor: "Calibri", defTxt: 18, titleSz: 44, bodySz: 28, otherSz: 18}
	pres := x.xml("ppt/presentation.xml")
	if pres == nil {
		return nil, fmt.Errorf("это не презентация PowerPoint")
	}
	if sz := pres.First("sldSz"); sz != nil {
		x.w, x.h = emu(sz.Av("cx")), emu(sz.Av("cy"))
	}
	x.defStyle = pres.Kid("defaultTextStyle")
	if d := x.defStyle; d != nil {
		if r := d.First("defRPr"); r != nil && r.Av("sz") != "" {
			v, _ := strconv.ParseFloat(r.Av("sz"), 64)
			x.defTxt = v / 100
		}
	}
	var slides []string
	for _, s := range pres.First("sldIdLst").KidsNamed("sldId") {
		if t := x.relTarget("ppt/presentation.xml", s.AttrNS(relNS, "id")); t != "" {
			slides = append(slides, t)
		}
	}
	if len(slides) == 0 {
		return nil, fmt.Errorf("в презентации нет слайдов")
	}
	x.out = newPdfOut()
	title := ""
	for _, s := range slides {
		layout := x.relOfType(s, "slideLayout")
		master := x.relOfType(layout, "slideMaster")
		x.loadTheme(master)
		p := x.out.newPage(x.w, x.h)
		x.drawSlide(p, s, layout, master)
		if title == "" {
			title = x.slideTitle(s)
		}
	}
	return x.out.finish(title), nil
}

func (x *pptx) loadTheme(master string) {
	m := x.xml(master)
	if m == nil {
		return
	}
	if cm := m.Kid("clrMap"); cm != nil {
		for _, a := range cm.Attr {
			x.clrMap[a.Name.Local] = a.Value
		}
	}
	// размеры текста по умолчанию из образца
	x.txStyles = m.Kid("txStyles")
	if ts := x.txStyles; ts != nil {
		get := func(name string, dst *float64) {
			if n := ts.Kid(name); n != nil {
				if r := n.First("defRPr"); r != nil && r.Av("sz") != "" {
					v, _ := strconv.ParseFloat(r.Av("sz"), 64)
					*dst = v / 100
				}
			}
		}
		get("titleStyle", &x.titleSz)
		get("bodyStyle", &x.bodySz)
		get("otherStyle", &x.otherSz)
	}
	th := x.xml(x.relOfType(master, "theme"))
	if th == nil {
		return
	}
	if cs := th.First("clrScheme"); cs != nil {
		for _, c := range cs.Elems() {
			if col, ok := x.color(c); ok {
				x.theme[c.Name] = col
			}
		}
	}
	if f := th.First("majorFont"); f != nil {
		if l := f.Kid("latin"); l != nil && l.Av("typeface") != "" {
			x.major = l.Av("typeface")
		}
	}
	if f := th.First("minorFont"); f != nil {
		if l := f.Kid("latin"); l != nil && l.Av("typeface") != "" {
			x.minor = l.Av("typeface")
		}
	}
	// в теме есть и dk1/lt1 без clrMap — заведём и «tx1/bg1», если образец их не отобразил
	for k, v := range map[string]string{"tx1": "dk1", "bg1": "lt1", "tx2": "dk2", "bg2": "lt2"} {
		if _, ok := x.clrMap[k]; !ok {
			x.clrMap[k] = v
		}
	}
}

func (x *pptx) slideTitle(s string) string {
	n := x.xml(s)
	for _, sp := range n.Find("sp") {
		if ph := sp.First("ph"); ph != nil && (ph.Av("type") == "title" || ph.Av("type") == "ctrTitle") {
			return strings.Join(strings.Fields(sp.Kid("txBody").TextContent()), " ")
		}
	}
	return ""
}

func (x *pptx) drawSlide(p *pdfPage, slide, layout, master string) {
	// фон: слайд → макет → образец
	bg := Hex(0xffffff)
	for _, part := range []string{slide, layout, master} {
		n := x.xml(part)
		if n == nil {
			continue
		}
		if b := n.First("bg"); b != nil {
			if pr := b.Kid("bgPr"); pr != nil {
				if c, ok := x.fill(pr); ok && c != nil {
					bg = *c
					break
				}
			}
			if ref := b.Kid("bgRef"); ref != nil {
				if c, ok := x.color(ref); ok {
					bg = c
					break
				}
			}
		}
	}
	p.fillRect(Rect{0, 0, x.w, x.h}, bg)
	sn := x.xml(slide)
	// декор образца и макета (не заполнители), если слайд их не прячет
	if sn.Av("showMasterSp") != "0" {
		for _, part := range []string{master, layout} {
			if n := x.xml(part); n != nil && n.Av("showMasterSp") != "0" {
				x.drawTree(p, part, n.First("spTree"), identity, true, layout, master)
			}
		}
	}
	x.drawTree(p, slide, sn.First("spTree"), identity, false, layout, master)
}

// drawTree рисует фигуры дерева; decorOnly — только не-заполнители (для образца и макета).
func (x *pptx) drawTree(p *pdfPage, part string, tree *Node, m mapper, decorOnly bool, layout, master string) {
	if tree == nil {
		return
	}
	for _, n := range tree.Elems() {
		isPh := n.First("ph") != nil
		if decorOnly && isPh {
			continue
		}
		switch n.Name {
		case "sp":
			x.drawShape(p, part, n, m, layout, master)
		case "cxnSp":
			x.drawShape(p, part, n, m, layout, master)
		case "pic":
			x.drawPic(p, part, n, m)
		case "grpSp":
			t := readXfrm(n.Kid("grpSpPr").Kid("xfrm"))
			sx, sy := 1.0, 1.0
			if t.chExt[0] > 0 {
				sx = t.ext[0] / t.chExt[0]
			}
			if t.chExt[1] > 0 {
				sy = t.ext[1] / t.chExt[1]
			}
			inner := mapper(func(cx, cy float64) (float64, float64) {
				return m(t.off[0]+(cx-t.chOff[0])*sx, t.off[1]+(cy-t.chOff[1])*sy)
			})
			x.drawTree(p, part, n, inner, decorOnly, layout, master)
		case "graphicFrame":
			x.drawFrame(p, part, n, m)
		case "AlternateContent":
			if f := n.Kid("Fallback"); f != nil {
				x.drawTree(p, part, f, m, decorOnly, layout, master)
			}
		}
	}
}

// placeholder — такой же заполнитель в макете или образце (оттуда берутся место и стиль текста).
func (x *pptx) placeholder(part string, ph *Node) *Node {
	if ph == nil {
		return nil
	}
	typ, idx := ph.Av("type"), ph.Av("idx")
	if typ == "" {
		typ = "body"
	}
	n := x.xml(part)
	if n == nil {
		return nil
	}
	var byType *Node
	for _, sp := range n.Find("sp") {
		q := sp.First("ph")
		if q == nil {
			continue
		}
		qt := q.Av("type")
		if qt == "" {
			qt = "body"
		}
		if idx != "" && q.Av("idx") == idx {
			return sp
		}
		if qt == typ || (typ == "ctrTitle" && qt == "title") || (typ == "subTitle" && qt == "body") {
			if byType == nil {
				byType = sp
			}
		}
	}
	return byType
}

func (x *pptx) drawShape(p *pdfPage, part string, sp *Node, m mapper, layout, master string) {
	spPr := sp.Kid("spPr")
	ph := sp.First("ph")
	// место: своё или от заполнителя макета/образца
	t := readXfrm(spPr.Kid("xfrm"))
	var inherit []*Node
	if ph != nil {
		for _, src := range []string{layout, master} {
			if q := x.placeholder(src, ph); q != nil {
				inherit = append(inherit, q)
				if !t.has {
					t = readXfrm(q.Kid("spPr").Kid("xfrm"))
				}
			}
		}
	}
	if !t.has {
		return
	}
	r := m.rect(t)
	geom := "rect"
	adj := 0.16667
	if g := spPr.Kid("prstGeom"); g != nil {
		geom = g.Av("prst")
		if gd := g.First("gd"); gd != nil {
			if f := strings.TrimPrefix(gd.Av("fmla"), "val "); f != gd.Av("fmla") {
				v, _ := strconv.ParseFloat(f, 64)
				adj = v / 100000
			}
		}
	} else if spPr.Kid("custGeom") != nil {
		geom = "cust"
	}
	// заливка и обводка: своя → из p:style
	fill, okF := x.fill(spPr)
	if !okF {
		fill, _ = x.styleRef(sp, "fillRef")
	}
	ln, okL := x.line(spPr)
	if !okL {
		if c, ok := x.styleRef(sp, "lnRef"); ok {
			ln = pdfStyle{Stroke: c, W: 0.75}
		}
	}
	isLine := sp.Name == "cxnSp" || geom == "line" || strings.Contains(geom, "Connector")
	if isLine {
		x0, y0 := m(t.off[0], t.off[1])
		x1, y1 := m(t.off[0]+t.ext[0], t.off[1]+t.ext[1])
		if t.flipH {
			x0, x1 = x1, x0
		}
		if t.flipV {
			y0, y1 = y1, y0
		}
		if ln.Stroke == nil {
			c := x.theme["dk1"]
			ln = pdfStyle{Stroke: &c, W: 0.75}
		}
		p.polyline([][2]float64{{x0, y0}, {x1, y1}}, false, ln)
		x.drawArrows(p, spPr, [2]float64{x0, y0}, [2]float64{x1, y1}, ln)
		return
	}
	st := ln
	st.Fill = fill
	switch geom {
	case "ellipse":
		p.shapePath("ellipse", r, 0, st)
	case "roundRect":
		p.shapePath("roundRect", r, adj*math.Min(r.W, r.H), st)
	case "triangle":
		p.polyline([][2]float64{{r.X + r.W/2, r.Y}, {r.X + r.W, r.Y + r.H}, {r.X, r.Y + r.H}}, true, st)
	case "rightArrow":
		hy := r.H * 0.25
		hx := r.X + r.W - math.Min(r.W/2, r.H/2)
		p.polyline([][2]float64{{r.X, r.Y + hy}, {hx, r.Y + hy}, {hx, r.Y}, {r.X + r.W, r.Y + r.H/2}, {hx, r.Y + r.H}, {hx, r.Y + r.H - hy}, {r.X, r.Y + r.H - hy}}, true, st)
	case "diamond":
		p.polyline([][2]float64{{r.X + r.W/2, r.Y}, {r.X + r.W, r.Y + r.H/2}, {r.X + r.W/2, r.Y + r.H}, {r.X, r.Y + r.H/2}}, true, st)
	case "cust":
		x.drawCustGeom(p, spPr.Kid("custGeom"), t, m, st)
	default:
		p.shapePath("rect", r, 0, st)
	}
	if tb := sp.Kid("txBody"); tb != nil {
		x.drawText(p, tb, r, ph, inherit)
	}
}

// drawArrows — стрелки на концах линии (headEnd/tailEnd).
func (x *pptx) drawArrows(p *pdfPage, spPr *Node, a, b [2]float64, st pdfStyle) {
	ln := spPr.Kid("ln")
	if ln == nil || st.Stroke == nil {
		return
	}
	head := func(tip, from [2]float64) {
		dx, dy := tip[0]-from[0], tip[1]-from[1]
		l := math.Hypot(dx, dy)
		if l < 0.1 {
			return
		}
		ux, uy := dx/l, dy/l
		s := math.Max(4, st.W*3)
		bx, by := tip[0]-ux*s, tip[1]-uy*s
		fs := pdfStyle{Fill: st.Stroke}
		p.polyline([][2]float64{tip, {bx - uy*s/2, by + ux*s/2}, {bx + uy*s/2, by - ux*s/2}}, true, fs)
	}
	if e := ln.Kid("tailEnd"); e != nil && e.Av("type") != "" && e.Av("type") != "none" {
		head(b, a)
	}
	if e := ln.Kid("headEnd"); e != nil && e.Av("type") != "" && e.Av("type") != "none" {
		head(a, b)
	}
}

func (x *pptx) drawPic(p *pdfPage, part string, pic *Node, m mapper) {
	t := readXfrm(pic.Kid("spPr").Kid("xfrm"))
	blip := pic.First("blip")
	if !t.has || blip == nil {
		return
	}
	target := x.relTarget(part, blip.Av("embed"))
	data, ok := x.files[target]
	if !ok {
		return
	}
	r := m.rect(t)
	p.image(&Media{Data: data}, r)
}

// ---------------------------------------------------------------- текст

type txRun struct {
	text string
	f    Font
	col  Color
	u    bool
	rise float64 // подъём над базовой линией (верхний/нижний индекс)
}

type txPara struct {
	runs                   []txRun
	align                  string
	marL, indent           float64
	bullet                 string
	bulletFont             Font
	before, after, lineMul float64
	lineExact              float64
	size                   float64 // размер для пустого абзаца
}

// rprOf — свойства текста: значения из цепочки (своё, стиль списка, заполнители), первое найденное.
func (x *pptx) runFont(chain []*Node, defSize float64, title bool) (Font, Color, bool) {
	f := Font{Px: defSize}
	fam := x.minor
	if title {
		fam = x.major
	}
	col := x.theme["dk1"]
	if m, ok := x.clrMap["tx1"]; ok {
		col = x.theme[m]
	}
	gotSz, gotB, gotI, gotC, gotF, u := false, false, false, false, false, false
	for _, r := range chain {
		if r == nil {
			continue
		}
		if !gotSz && r.Av("sz") != "" {
			v, _ := strconv.ParseFloat(r.Av("sz"), 64)
			f.Px, gotSz = v/100, true
		}
		if !gotB && r.Av("b") != "" {
			f.Bold, gotB = r.Av("b") == "1" || r.Av("b") == "true", true
		}
		if !gotI && r.Av("i") != "" {
			f.Italic, gotI = r.Av("i") == "1" || r.Av("i") == "true", true
		}
		if !u && r.Av("u") != "" && r.Av("u") != "none" {
			u = true
		}
		if !gotC {
			if c, ok := x.color(r.Kid("solidFill")); ok {
				col, gotC = c, true
			}
		}
		if !gotF {
			if l := r.Kid("latin"); l != nil && l.Av("typeface") != "" {
				tf := l.Av("typeface")
				switch tf {
				case "+mj-lt":
					tf = x.major
				case "+mn-lt":
					tf = x.minor
				}
				fam, gotF = tf, true
			}
		}
	}
	f.Family = fam
	return f, col, u
}

// lvlPPr — свойства абзаца уровня lvl из lstStyle (lvl1pPr…).
func lvlPPr(lst *Node, lvl int) *Node {
	if lst == nil {
		return nil
	}
	return lst.Kid(fmt.Sprintf("lvl%dpPr", lvl+1))
}

func (x *pptx) drawText(p *pdfPage, tb *Node, box Rect, ph *Node, inherit []*Node) {
	body := tb.Kid("bodyPr")
	// стили списка: свой → заполнители макета/образца → стиль образца по типу
	var lsts []*Node
	lsts = append(lsts, tb.Kid("lstStyle"))
	var bodies []*Node
	bodies = append(bodies, body)
	for _, q := range inherit {
		if qb := q.Kid("txBody"); qb != nil {
			lsts = append(lsts, qb.Kid("lstStyle"))
			bodies = append(bodies, qb.Kid("bodyPr"))
		}
	}
	title := false
	defSize := x.otherSz
	if ph != nil {
		switch ph.Av("type") {
		case "title", "ctrTitle":
			title, defSize = true, x.titleSz
		case "body", "subTitle", "", "obj":
			defSize = x.bodySz
		default:
			defSize = x.otherSz
		}
	} else {
		defSize = x.defTxt
	}
	// стили образца по типу текста, затем стиль презентации
	if ph != nil {
		name := "bodyStyle"
		switch ph.Av("type") {
		case "title", "ctrTitle":
			name = "titleStyle"
		case "body", "subTitle", "", "obj":
		default:
			name = "otherStyle"
		}
		lsts = append(lsts, x.txStyles.Kid(name))
	}
	lsts = append(lsts, x.defStyle)
	battr := func(name string) string {
		for _, b := range bodies {
			if v := b.Av(name); v != "" {
				return v
			}
		}
		return ""
	}
	ins := func(name string, def float64) float64 {
		if v := battr(name); v != "" {
			return emu(v)
		}
		return def
	}
	l, t, rr, bt := ins("lIns", 7.2), ins("tIns", 3.6), ins("rIns", 7.2), ins("bIns", 3.6)
	anchor := battr("anchor")
	if anchor == "" && title {
		anchor = "ctr"
	}
	wrap := battr("wrap") != "none"
	scale := 1.0
	for _, b := range bodies {
		if na := b.Kid("normAutofit"); na != nil && na.Av("fontScale") != "" {
			v, _ := strconv.ParseFloat(na.Av("fontScale"), 64)
			scale = v / 100000
			break
		}
	}
	inner := Rect{box.X + l, box.Y + t, box.W - l - rr, box.H - t - bt}
	// абзацы
	var paras []txPara
	for _, pn := range tb.KidsNamed("p") {
		pPr := pn.Kid("pPr")
		lvl, _ := strconv.Atoi(pPr.Av("lvl"))
		var pchain []*Node
		pchain = append(pchain, pPr)
		for _, ls := range lsts {
			pchain = append(pchain, lvlPPr(ls, lvl))
		}
		pa := func(name string) string {
			for _, n := range pchain {
				if v := n.Av(name); v != "" {
					return v
				}
			}
			return ""
		}
		kid := func(name string) *Node {
			for _, n := range pchain {
				if k := n.Kid(name); k != nil {
					return k
				}
			}
			return nil
		}
		tp := txPara{align: pa("algn"), lineMul: 1}
		if v := pa("marL"); v != "" {
			tp.marL = emu(v)
		}
		if v := pa("indent"); v != "" {
			tp.indent = emu(v)
		}
		sp := func(name string) (float64, bool) {
			if k := kid(name); k != nil {
				if pts := k.Kid("spcPts"); pts != nil {
					v, _ := strconv.ParseFloat(pts.Av("val"), 64)
					return v / 100, true
				}
				if pc := k.Kid("spcPct"); pc != nil {
					v, _ := strconv.ParseFloat(pc.Av("val"), 64)
					return -v / 100000, true // отрицательное — доля от высоты строки
				}
			}
			return 0, false
		}
		if v, ok := sp("lnSpc"); ok {
			if v < 0 {
				tp.lineMul = -v
			} else {
				tp.lineExact = v
			}
		}
		tp.before, _ = sp("spcBef")
		tp.after, _ = sp("spcAft")
		// свойства по умолчанию для текста абзаца
		var rdef []*Node
		for _, n := range pchain {
			if n != nil {
				rdef = append(rdef, n.Kid("defRPr"))
			}
		}
		if kid("buNone") == nil {
			if bc := kid("buChar"); bc != nil {
				tp.bullet = bc.Av("char")
			} else if ba := kid("buAutoNum"); ba != nil {
				tp.bullet = "•"
			}
		}
		for _, rn := range pn.Elems() {
			switch rn.Name {
			case "r", "fld":
				chain := append([]*Node{rn.Kid("rPr")}, rdef...)
				f, col, u := x.runFont(chain, defSize, title)
				f.Px *= scale
				rise := 0.0
				for _, c := range chain {
					if v := c.Av("baseline"); v != "" {
						b, _ := strconv.ParseFloat(v, 64)
						rise = b / 100000 * f.Px
						if b != 0 {
							f.Px *= 0.66
						}
						break
					}
				}
				tp.runs = append(tp.runs, txRun{text: rn.Kid("t").TextContent(), f: f, col: col, u: u, rise: rise})
			case "br":
				tp.runs = append(tp.runs, txRun{text: "\n"})
			}
		}
		f, _, _ := x.runFont(append([]*Node{pn.Kid("endParaRPr")}, rdef...), defSize, title)
		tp.size = f.Px * scale
		tp.bulletFont = f
		if len(tp.runs) > 0 && tp.runs[0].f.Px > 0 {
			tp.bulletFont = tp.runs[0].f
		}
		paras = append(paras, tp)
	}
	x.layoutText(p, inner, paras, anchor, wrap)
}

type txLine struct {
	items         []txRun
	xs            []float64
	w, asc, desc  float64
	h             float64
	x0            float64
	bullet        string
	bulletF       Font
	bulletCol     Color
	bx            float64 // место маркера от левого края
	align         string
	before, after float64
}

func (x *pptx) layoutText(p *pdfPage, r Rect, paras []txPara, anchor string, wrap bool) {
	o := x.out
	var lines []*txLine
	for _, tp := range paras {
		first := true
		avail := r.W - tp.marL
		cur := &txLine{x0: tp.marL + tp.indent, align: tp.align, before: tp.before}
		if tp.bullet != "" {
			// маркер — на marL+indent, текст первой строки — с marL (висячий отступ) или сразу за маркером
			cur.bullet, cur.bulletF = tp.bullet, tp.bulletFont
			if len(tp.runs) > 0 {
				cur.bulletCol = tp.runs[0].col
			}
			cur.bx = cur.x0
			bw := o.textWidth(tp.bullet+" ", tp.bulletFont)
			cur.x0 = math.Max(tp.marL, cur.bx+bw)
			if tp.indent >= 0 {
				cur.x0 = cur.bx + bw
			}
		}
		firstX := cur.x0
		width := func(l *txLine) float64 {
			if first {
				return r.W - firstX
			}
			return avail
		}
		push := func() {
			if cur.asc == 0 {
				f := Font{Family: x.minor, Px: math.Max(1, tp.size)}
				a, d, g := o.fontMetrics(f)
				cur.asc, cur.desc, cur.h = a, d, (a+d+g)*1.2/1.2
			}
			lh := cur.h * tp.lineMul
			if tp.lineExact > 0 {
				lh = tp.lineExact
			}
			cur.h = lh
			lines = append(lines, cur)
			first = false
			cur = &txLine{x0: tp.marL, align: tp.align}
		}
		addWord := func(w txRun) {
			ww := o.textWidth(w.text, w.f)
			if wrap && cur.w+ww > width(cur)+0.5 && cur.w > 0 && strings.TrimSpace(w.text) != "" {
				// пробелы в конце строки не считаем
				push()
			}
			if cur.w == 0 && strings.TrimSpace(w.text) == "" && len(cur.items) == 0 && !first {
				return
			}
			a, d, g := o.fontMetrics(w.f)
			cur.asc = math.Max(cur.asc, a)
			cur.desc = math.Max(cur.desc, d)
			cur.h = math.Max(cur.h, (a+d+g)*1.0)
			cur.xs = append(cur.xs, cur.w)
			cur.items = append(cur.items, w)
			cur.w += ww
		}
		for _, run := range tp.runs {
			if run.text == "\n" {
				push()
				continue
			}
			// по словам: слово + пробел за ним
			rs := []rune(run.text)
			for i := 0; i < len(rs); {
				j := i
				for j < len(rs) && rs[j] != ' ' {
					j++
				}
				for j < len(rs) && rs[j] == ' ' {
					j++
				}
				w := run
				w.text = string(rs[i:j])
				addWord(w)
				i = j
			}
		}
		push()
		lines[len(lines)-1].after = tp.after
	}
	total := 0.0
	for i, l := range lines {
		if i > 0 {
			total += l.before
		}
		total += l.h + l.after
	}
	y := r.Y
	switch anchor {
	case "ctr":
		y = r.Y + (r.H-total)/2
	case "b":
		y = r.Y + r.H - total
	}
	for i, l := range lines {
		if i > 0 {
			y += l.before
		}
		// пробелы в конце строки не влияют на выравнивание
		w := l.w
		if n := len(l.items); n > 0 {
			last := l.items[n-1]
			w -= o.textWidth(last.text, last.f) - o.textWidth(strings.TrimRight(last.text, " "), last.f)
		}
		x0 := r.X + l.x0
		switch l.align {
		case "ctr":
			x0 = r.X + l.x0 + (r.W-l.x0-w)/2
		case "r":
			x0 = r.X + r.W - w
		}
		base := y + l.asc + (l.h-(l.asc+l.desc))*0.15
		if l.bullet != "" {
			p.text(l.bullet, x0-l.x0+l.bx, base, l.bulletF, l.bulletCol)
		}
		// подряд идущие куски с одинаковым оформлением — одной строкой
		for k := 0; k < len(l.items); {
			m := k
			s := ""
			for m < len(l.items) && l.items[m].f == l.items[k].f && l.items[m].col == l.items[k].col && l.items[m].u == l.items[k].u && l.items[m].rise == l.items[k].rise {
				s += l.items[m].text
				m++
			}
			it := l.items[k]
			p.text(s, x0+l.xs[k], base-it.rise, it.f, it.col)
			if it.u {
				uw := o.textWidth(strings.TrimRight(s, " "), it.f)
				p.fillRect(Rect{x0 + l.xs[k], base + it.f.Px*0.1, uw, math.Max(0.5, it.f.Px/18)}, it.col)
			}
			k = m
		}
		y += l.h + l.after
	}
}

// ---------------------------------------------------------------- таблицы и диаграммы

func (x *pptx) drawFrame(p *pdfPage, part string, fr *Node, m mapper) {
	t := readXfrm(fr.Kid("xfrm"))
	if !t.has {
		return
	}
	r := m.rect(t)
	gd := fr.First("graphicData")
	if gd == nil {
		return
	}
	if tbl := gd.Kid("tbl"); tbl != nil {
		x.drawTable(p, tbl, r)
		return
	}
	if ch := gd.Kid("chart"); ch != nil {
		if target := x.relTarget(part, ch.AttrNS(relNS, "id")); target != "" {
			x.drawChart(p, target, r)
		}
		return
	}
	// прочее (SmartArt, OLE) — рамка-заглушка
	gray := Hex(0xbbbbbb)
	p.shapePath("rect", r, 0, pdfStyle{Stroke: &gray, W: 0.75, Dash: []float64{3, 3}})
}

func (x *pptx) drawTable(p *pdfPage, tbl *Node, r Rect) {
	var cols []float64
	for _, g := range tbl.Kid("tblGrid").KidsNamed("gridCol") {
		cols = append(cols, emu(g.Av("w")))
	}
	y := r.Y
	for _, tr := range tbl.KidsNamed("tr") {
		h := emu(tr.Av("h"))
		// высота строки — не меньше нужной тексту
		cx := r.X
		ci := 0
		type cell struct {
			r  Rect
			tc *Node
		}
		var cells []cell
		for _, tc := range tr.KidsNamed("tc") {
			if tc.Av("hMerge") == "1" || tc.Av("vMerge") == "1" {
				if ci < len(cols) {
					cx += cols[ci]
				}
				ci++
				continue
			}
			span, _ := strconv.Atoi(tc.Av("gridSpan"))
			w := 0.0
			for k := 0; k < max(1, span) && ci < len(cols); k++ {
				w += cols[ci]
				ci++
			}
			cells = append(cells, cell{Rect{cx, y, w, h}, tc})
			cx += w
		}
		for _, c := range cells {
			pr := c.tc.Kid("tcPr")
			if f, ok := x.fill(pr); ok && f != nil {
				p.fillRect(c.r, *f)
			}
			edge := func(name string, a, b [2]float64) {
				lnN := pr.Kid(name)
				st := pdfStyle{}
				if lnN != nil {
					if lnN.Kid("noFill") != nil {
						return
					}
					if col, ok := x.color(lnN.Kid("solidFill")); ok {
						st.Stroke = &col
						st.W = 0.75
						if w := lnN.Av("w"); w != "" {
							st.W = emu(w)
						}
					}
				}
				if st.Stroke == nil {
					col := Hex(0x000000)
					st = pdfStyle{Stroke: &col, W: 0.5}
				}
				p.polyline([][2]float64{a, b}, false, st)
			}
			rr := c.r
			edge("lnT", [2]float64{rr.X, rr.Y}, [2]float64{rr.X + rr.W, rr.Y})
			edge("lnB", [2]float64{rr.X, rr.Y + rr.H}, [2]float64{rr.X + rr.W, rr.Y + rr.H})
			edge("lnL", [2]float64{rr.X, rr.Y}, [2]float64{rr.X, rr.Y + rr.H})
			edge("lnR", [2]float64{rr.X + rr.W, rr.Y}, [2]float64{rr.X + rr.W, rr.Y + rr.H})
			if tb := c.tc.Kid("txBody"); tb != nil {
				mar := func(a string, def float64) float64 {
					if v := pr.Av(a); v != "" {
						return emu(v)
					}
					return def
				}
				in := Rect{rr.X + mar("marL", 7.2), rr.Y + mar("marT", 3.6), rr.W - mar("marL", 7.2) - mar("marR", 7.2), rr.H - mar("marT", 3.6) - mar("marB", 3.6)}
				anchor := pr.Av("anchor")
				x.drawTextInner(p, tb, in, anchor)
			}
		}
		y += h
	}
}

// drawTextInner — текст ячейки таблицы (без заполнителей и отступов фигуры).
func (x *pptx) drawTextInner(p *pdfPage, tb *Node, in Rect, anchor string) {
	box := Rect{in.X - 7.2, in.Y - 3.6, in.W + 14.4, in.H + 7.2}
	// подставляем якорь ячейки, если у текста своего нет
	if anchor != "" {
		if bp := tb.Kid("bodyPr"); bp != nil && bp.Av("anchor") == "" {
			bp.Attr = append(bp.Attr, xml.Attr{Name: xml.Name{Local: "anchor"}, Value: anchor})
		}
	}
	x.drawText(p, tb, box, nil, nil)
}

// ---------------------------------------------------------------- диаграммы

type chSeries struct {
	name   string
	xs, ys []float64
	cats   []string
	col    Color
	line   bool // соединять точки
	w      float64
	dash   []float64
	marker bool
}

func numCache(n *Node) []float64 {
	if n == nil {
		return nil
	}
	c := n.First("numCache")
	if c == nil {
		c = n.First("numLit")
	}
	if c == nil {
		return nil
	}
	cnt, _ := strconv.Atoi(c.Kid("ptCount").Av("val"))
	vals := make([]float64, cnt)
	for i := range vals {
		vals[i] = math.NaN()
	}
	for _, pt := range c.KidsNamed("pt") {
		i, _ := strconv.Atoi(pt.Av("idx"))
		v, err := strconv.ParseFloat(strings.TrimSpace(pt.Kid("v").TextContent()), 64)
		if err == nil {
			for i >= len(vals) {
				vals = append(vals, math.NaN())
			}
			vals[i] = v
		}
	}
	return vals
}

func strCache(n *Node) []string {
	if n == nil {
		return nil
	}
	c := n.First("strCache")
	if c == nil {
		c = n.First("strLit")
	}
	if c == nil {
		if v := numCache(n); v != nil {
			out := make([]string, len(v))
			for i, f := range v {
				out[i] = fmtTick(f)
			}
			return out
		}
		return nil
	}
	var out []string
	for _, pt := range c.KidsNamed("pt") {
		i, _ := strconv.Atoi(pt.Av("idx"))
		for i >= len(out) {
			out = append(out, "")
		}
		out[i] = pt.Kid("v").TextContent()
	}
	return out
}

func fmtTick(v float64) string {
	if math.Abs(v) < 1e-12 {
		return "0"
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if len(s) > 8 {
		s = strconv.FormatFloat(v, 'g', 4, 64)
	}
	return strings.Replace(s, ".", ",", 1)
}

// niceTicks — «круглые» деления оси от lo до hi.
func niceTicks(lo, hi float64, n int) (float64, float64, float64) {
	if hi <= lo {
		hi = lo + 1
	}
	raw := (hi - lo) / float64(n)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := mag
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if m*mag >= raw {
			step = m * mag
			break
		}
	}
	return math.Floor(lo/step) * step, math.Ceil(hi/step) * step, step
}

var chartPalette = []uint32{0x4472c4, 0xed7d31, 0xa5a5a5, 0xffc000, 0x5b9bd5, 0x70ad47, 0x264478, 0x9e480e}

type chAxis struct {
	pos           string // b, t, l, r
	del           bool
	lo, hi, unit  float64
	hasLo, hasHi  bool
	grid          *pdfStyle
	line          *pdfStyle
	title         string
	titleF, tickF Font
	titleC, tickC Color
	cat           bool
	lblNone       bool
}

// chFont — шрифт из txPr/rich (первый defRPr или rPr), иначе def.
func (x *pptx) chFont(n *Node, def Font, defC Color) (Font, Color) {
	if n == nil {
		return def, defC
	}
	var chain []*Node
	if r := n.First("rPr"); r != nil {
		chain = append(chain, r)
	}
	if r := n.First("defRPr"); r != nil {
		chain = append(chain, r)
	}
	if len(chain) == 0 {
		return def, defC
	}
	f, c, _ := x.runFont(chain, def.Px, false)
	if !hasAttr(chain, "sz") {
		f.Px = def.Px
	}
	if !hasAttr(chain, "b") {
		f.Bold = def.Bold
	}
	return f, c
}

func hasAttr(ns []*Node, a string) bool {
	for _, n := range ns {
		if n.Av(a) != "" {
			return true
		}
	}
	return false
}

func (x *pptx) readAxis(ax *Node, def Font, defC Color) chAxis {
	a := chAxis{pos: ax.Kid("axPos").Av("val"), del: ax.Kid("delete").Av("val") == "1", cat: ax.Name != "valAx"}
	sc := ax.Kid("scaling")
	if v := sc.Kid("min"); v != nil {
		a.lo, _ = strconv.ParseFloat(v.Av("val"), 64)
		a.hasLo = true
	}
	if v := sc.Kid("max"); v != nil {
		a.hi, _ = strconv.ParseFloat(v.Av("val"), 64)
		a.hasHi = true
	}
	if v := ax.Kid("majorUnit"); v != nil {
		a.unit, _ = strconv.ParseFloat(v.Av("val"), 64)
	}
	if g := ax.Kid("majorGridlines"); g != nil {
		st := pdfStyle{W: 0.5}
		c := Hex(0xd9d9d9)
		if ln, ok := x.line(g.Kid("spPr")); ok && ln.Stroke != nil {
			st = ln
		} else {
			st.Stroke = &c
		}
		a.grid = &st
	}
	if ln, ok := x.line(ax.Kid("spPr")); ok {
		if ln.Stroke != nil {
			a.line = &ln
		}
	} else {
		c := Hex(0x868686)
		a.line = &pdfStyle{Stroke: &c, W: 0.75}
	}
	a.tickF, a.tickC = x.chFont(ax.Kid("txPr"), def, defC)
	a.lblNone = ax.Kid("tickLblPos").Av("val") == "none"
	if t := ax.Kid("title"); t != nil {
		a.title = strings.Join(strings.Fields(t.Kid("tx").TextContent()), " ")
		tf := def
		tf.Bold = true
		a.titleF, a.titleC = x.chFont(t, tf, defC)
	}
	return a
}

// ticks — значения делений оси.
func (a *chAxis) ticks(dataLo, dataHi float64) []float64 {
	lo, hi, step := niceTicks(dataLo, dataHi, 5)
	if a.hasLo {
		lo = a.lo
	}
	if a.hasHi {
		hi = a.hi
	}
	if hi <= lo {
		hi = lo + 1
	}
	if a.unit > 0 && (hi-lo)/a.unit <= 100 {
		step = a.unit
	} else if (hi-lo)/step > 20 {
		_, _, step = niceTicks(lo, hi, 5)
	}
	a.lo, a.hi = lo, hi
	var out []float64
	for i := 0; ; i++ {
		v := lo + float64(i)*step
		if v > hi+step*1e-6 {
			break
		}
		out = append(out, math.Round(v/step*1e6)/1e6*step)
	}
	return out
}

func (x *pptx) drawChart(p *pdfPage, part string, r Rect) {
	cs := x.xml(part)
	if cs == nil {
		return
	}
	space := cs
	chart := space.Kid("chart")
	pa := chart.Kid("plotArea")
	if pa == nil {
		return
	}
	if f, ok := x.fill(space.Kid("spPr")); ok && f != nil {
		p.fillRect(r, *f)
	}
	defF, defC := x.chFont(space.Kid("txPr"), Font{Family: x.minor, Px: 10}, x.theme["dk1"])
	o := x.out
	lineH := func(f Font) float64 { a, d, _ := o.fontMetrics(f); return (a + d) * 1.2 }
	ascOf := func(f Font) float64 { a, _, _ := o.fontMetrics(f); return a }
	top := r.Y + 4
	// заголовок
	if t := chart.Kid("title"); t != nil && chart.Kid("autoTitleDeleted").Av("val") != "1" {
		s := strings.Join(strings.Fields(t.Kid("tx").TextContent()), " ")
		if s != "" {
			tf := defF
			tf.Px *= 1.4
			tf.Bold = true
			tf, tc := x.chFont(t, tf, defC)
			w := o.textWidth(s, tf)
			p.text(s, r.X+(r.W-w)/2, top+ascOf(tf), tf, tc)
			top += lineH(tf) * 1.2
		}
	}
	var series []chSeries
	kind := ""
	barDir := "col"
	for _, ct := range pa.Elems() {
		switch ct.Name {
		case "scatterChart", "lineChart", "barChart", "bar3DChart", "line3DChart", "pieChart", "pie3DChart", "doughnutChart", "areaChart":
		default:
			continue
		}
		if kind == "" {
			kind = ct.Name
		}
		if bd := ct.Kid("barDir"); bd != nil {
			barDir = bd.Av("val")
		}
		style := ct.Kid("scatterStyle").Av("val")
		for _, ser := range ct.KidsNamed("ser") {
			s := chSeries{name: strings.TrimSpace(ser.Kid("tx").TextContent()), w: 2.25}
			i := len(series)
			s.col = x.sliceColor(i)
			if sp := ser.Kid("spPr"); sp != nil {
				if c, ok := x.fill(sp); ok && c != nil {
					s.col = *c
				}
				if ln, ok := x.line(sp); ok && ln.Stroke != nil {
					s.col, s.w, s.dash = *ln.Stroke, ln.W, ln.Dash
				}
			}
			if ser.Kid("xVal") != nil {
				s.xs = numCache(ser.Kid("xVal"))
				s.cats = strCache(ser.Kid("xVal"))
				s.ys = numCache(ser.Kid("yVal"))
			} else {
				s.cats = strCache(ser.Kid("cat"))
				s.ys = numCache(ser.Kid("val"))
			}
			s.line = ct.Name == "lineChart" || ct.Name == "line3DChart" || (ct.Name == "scatterChart" && style != "marker")
			if ln := ser.Kid("spPr").Kid("ln"); ln != nil && ln.Kid("noFill") != nil {
				s.line = false
			}
			mk := ser.Kid("marker").Kid("symbol").Av("val")
			s.marker = (ct.Name == "scatterChart" || ct.Name == "lineChart") && mk != "none" && (mk != "" || !s.line)
			series = append(series, s)
		}
	}
	if len(series) == 0 {
		return
	}
	pie := strings.HasPrefix(kind, "pie") || kind == "doughnutChart"
	// легенда
	bottom := r.Y + r.H - 4
	if lg := chart.Kid("legend"); lg != nil {
		lf, lc := x.chFont(lg.Kid("txPr"), defF, defC)
		var items []string
		var cols []Color
		if pie {
			for i, c := range series[0].cats {
				items = append(items, c)
				cols = append(cols, x.sliceColor(i))
			}
		} else {
			for _, s := range series {
				items = append(items, s.name)
				cols = append(cols, s.col)
			}
		}
		// раскладка по строкам
		gap := lf.Px * 1.5
		key := lf.Px * 1.6
		var rows [][]int
		rowW := []float64{0}
		rows = append(rows, nil)
		for i, it := range items {
			w := key + o.textWidth(it, lf) + gap
			k := len(rows) - 1
			if rowW[k] > 0 && rowW[k]+w > r.W {
				rows = append(rows, nil)
				rowW = append(rowW, 0)
				k++
			}
			rows[k] = append(rows[k], i)
			rowW[k] += w
		}
		lh := lineH(lf)
		y := bottom - lh*float64(len(rows))
		bottom = y - lh*0.3
		for k, row := range rows {
			lx := r.X + (r.W-rowW[k]+gap)/2
			base := y + lh*float64(k) + ascOf(lf)
			for _, i := range row {
				c := cols[i]
				if pie || !series[i].line && !series[i].marker {
					p.fillRect(Rect{lx, base - lf.Px*0.7, lf.Px * 0.7, lf.Px * 0.7}, c)
				} else {
					s := series[i]
					p.polyline([][2]float64{{lx, base - lf.Px*0.35}, {lx + key*0.8, base - lf.Px*0.35}}, false, pdfStyle{Stroke: &c, W: math.Min(s.w, lf.Px/4), Dash: s.dash})
				}
				p.text(items[i], lx+key, base, lf, lc)
				lx += key + o.textWidth(items[i], lf) + gap
			}
		}
	}
	if pie {
		x.drawPie(p, series[0], Rect{r.X, top, r.W, bottom - top}, kind == "doughnutChart")
		return
	}
	// оси
	var xa, ya chAxis
	xa = chAxis{pos: "b", cat: kind != "scatterChart", tickF: defF, tickC: defC}
	ya = chAxis{pos: "l", tickF: defF, tickC: defC}
	horiz := barDir == "bar"
	for _, ax := range pa.Elems() {
		if ax.Name != "valAx" && ax.Name != "catAx" && ax.Name != "dateAx" {
			continue
		}
		a := x.readAxis(ax, defF, defC)
		vert := a.pos == "l" || a.pos == "r"
		if vert {
			ya = a
		} else {
			xa = a
		}
	}
	if horiz { // у линейчатой категории слева, значения снизу
		xa, ya = ya, xa
		xa.cat, ya.cat = true, false
	}
	numX := kind == "scatterChart"
	yLo, yHi := math.Inf(1), math.Inf(-1)
	xLo, xHi := math.Inf(1), math.Inf(-1)
	nCat := 0
	for _, s := range series {
		for _, v := range s.ys {
			if !math.IsNaN(v) {
				yLo, yHi = math.Min(yLo, v), math.Max(yHi, v)
			}
		}
		for _, v := range s.xs {
			if !math.IsNaN(v) {
				xLo, xHi = math.Min(xLo, v), math.Max(xHi, v)
			}
		}
		nCat = max(nCat, len(s.ys), len(s.cats))
		if numX && len(s.xs) == 0 {
			numX = false
		}
	}
	if math.IsInf(yLo, 0) {
		return
	}
	if kind == "barChart" || kind == "bar3DChart" || kind == "areaChart" {
		yLo, yHi = math.Min(0, yLo), math.Max(0, yHi)
	}
	// ось значений — та, что не категории
	valAx, catAx := &ya, &xa
	if horiz {
		valAx, catAx = &xa, &ya
	}
	vt := valAx.ticks(yLo, yHi)
	var xt []float64
	if numX {
		xt = catAx.ticks(xLo, xHi)
	}
	var cats []string
	for _, s := range series {
		if len(s.cats) > len(cats) {
			cats = s.cats
		}
	}
	// поле построения: место под подписи и названия осей
	labelsOf := func(a *chAxis, vals []float64, isCat bool) []string {
		if a.del || a.lblNone {
			return nil
		}
		if isCat {
			return cats
		}
		var out []string
		for _, v := range vals {
			out = append(out, fmtTick(v))
		}
		return out
	}
	ylabs := labelsOf(&ya, vt, horiz)
	if horiz {
		ylabs = labelsOf(&ya, nil, true)
	}
	xlabs := labelsOf(&xa, xt, !numX && !horiz)
	if horiz {
		xlabs = labelsOf(&xa, vt, false)
	}
	left := r.X + 4
	if ya.title != "" {
		left += lineH(ya.titleF) * 1.1
	}
	lw := 0.0
	for _, l := range ylabs {
		lw = math.Max(lw, o.textWidth(l, ya.tickF))
	}
	plot := Rect{X: left + lw + ya.tickF.Px*0.5, Y: top + lineH(ya.tickF)*0.5}
	plot.W = r.X + r.W - 4 - plot.X - o.textWidth(lastOr(xlabs), xa.tickF)/2
	bot := bottom
	if xa.title != "" {
		bot -= lineH(xa.titleF) * 1.2
	}
	if len(xlabs) > 0 {
		bot -= lineH(xa.tickF) * 1.1
	}
	plot.H = bot - plot.Y
	if plot.W < 10 || plot.H < 10 {
		return
	}
	// названия осей
	if xa.title != "" {
		w := o.textWidth(xa.title, xa.titleF)
		p.text(xa.title, plot.X+(plot.W-w)/2, bottom-lineH(xa.titleF)*0.3, xa.titleF, xa.titleC)
	}
	if ya.title != "" {
		w := o.textWidth(ya.title, ya.titleF)
		cx, cy := r.X+4+ascOf(ya.titleF), plot.Y+plot.H/2
		p.rotated(cx, cy, 90, func() { p.text(ya.title, cx-w/2, cy, ya.titleF, ya.titleC) })
	}
	vpos := func(v float64) float64 { return (v - valAx.lo) / (valAx.hi - valAx.lo) }
	py := func(v float64) float64 { return plot.Y + plot.H - vpos(v)*plot.H }
	px := func(v float64) float64 { return plot.X + (v-catAx.lo)/(catAx.hi-catAx.lo)*plot.W }
	// сетка и подписи значений
	for i, v := range vt {
		lab := fmtTick(v)
		if horiz {
			xx := plot.X + vpos(v)*plot.W
			if xa.grid != nil {
				p.polyline([][2]float64{{xx, plot.Y}, {xx, plot.Y + plot.H}}, false, *xa.grid)
			}
			if i < len(xlabs) {
				w := o.textWidth(lab, xa.tickF)
				p.text(lab, xx-w/2, plot.Y+plot.H+ascOf(xa.tickF)+xa.tickF.Px*0.3, xa.tickF, xa.tickC)
			}
			continue
		}
		yy := py(v)
		if ya.grid != nil {
			p.polyline([][2]float64{{plot.X, yy}, {plot.X + plot.W, yy}}, false, *ya.grid)
		}
		if len(ylabs) > 0 {
			w := o.textWidth(lab, ya.tickF)
			p.text(lab, plot.X-w-ya.tickF.Px*0.4, yy+ascOf(ya.tickF)*0.35, ya.tickF, ya.tickC)
		}
	}
	if numX {
		for _, v := range xt {
			xx := px(v)
			if xa.grid != nil {
				p.polyline([][2]float64{{xx, plot.Y}, {xx, plot.Y + plot.H}}, false, *xa.grid)
			}
			if len(xlabs) > 0 {
				lab := fmtTick(v)
				w := o.textWidth(lab, xa.tickF)
				p.text(lab, xx-w/2, plot.Y+plot.H+ascOf(xa.tickF)+xa.tickF.Px*0.3, xa.tickF, xa.tickC)
			}
		}
	}
	if xa.line != nil && !xa.del {
		p.polyline([][2]float64{{plot.X, plot.Y + plot.H}, {plot.X + plot.W, plot.Y + plot.H}}, false, *xa.line)
	}
	if ya.line != nil && !ya.del {
		p.polyline([][2]float64{{plot.X, plot.Y}, {plot.X, plot.Y + plot.H}}, false, *ya.line)
	}
	if numX {
		p.clip(plot)
		for _, s := range series {
			var pts [][2]float64
			for i, v := range s.ys {
				if i >= len(s.xs) || math.IsNaN(v) || math.IsNaN(s.xs[i]) {
					continue
				}
				pts = append(pts, [2]float64{px(s.xs[i]), py(v)})
			}
			x.drawSeriesLine(p, s, pts)
		}
		p.unclip()
		return
	}
	if nCat == 0 {
		return
	}
	slot := plot.W / float64(nCat)
	if horiz {
		slot = plot.H / float64(nCat)
	}
	if len(cats) > 0 && !catAx.del && !catAx.lblNone {
		f := catAx.tickF
		every := 1
		if !horiz {
			mw := 0.0
			for _, c := range cats {
				mw = math.Max(mw, o.textWidth(c, f))
			}
			every = max(1, int(math.Ceil(mw*1.1/slot)))
		}
		for i := 0; i < nCat && i < len(cats); i += every {
			lab := cats[i]
			w := o.textWidth(lab, f)
			if horiz {
				p.text(lab, plot.X-w-f.Px*0.4, plot.Y+plot.H-(float64(i)+0.5)*slot+ascOf(f)*0.35, f, catAx.tickC)
			} else {
				p.text(lab, plot.X+(float64(i)+0.5)*slot-w/2, plot.Y+plot.H+ascOf(f)+f.Px*0.3, f, catAx.tickC)
			}
		}
	}
	if kind == "barChart" || kind == "bar3DChart" {
		n := float64(len(series))
		bw := slot * 0.6 / n
		zero := math.Max(valAx.lo, math.Min(valAx.hi, 0))
		for si, s := range series {
			for i, v := range s.ys {
				if math.IsNaN(v) {
					continue
				}
				if horiz {
					y0 := plot.Y + plot.H - float64(i+1)*slot + slot*0.2 + float64(si)*bw
					x0, x1 := plot.X+vpos(zero)*plot.W, plot.X+vpos(v)*plot.W
					p.fillRect(Rect{math.Min(x0, x1), y0, math.Abs(x1 - x0), bw}, s.col)
				} else {
					x0 := plot.X + float64(i)*slot + slot*0.2 + float64(si)*bw
					y0, y1 := py(zero), py(v)
					p.fillRect(Rect{x0, math.Min(y0, y1), bw, math.Abs(y1 - y0)}, s.col)
				}
			}
		}
		return
	}
	for _, s := range series {
		var pts [][2]float64
		for i, v := range s.ys {
			if !math.IsNaN(v) {
				pts = append(pts, [2]float64{plot.X + (float64(i)+0.5)*slot, py(v)})
			}
		}
		if kind == "areaChart" && len(pts) > 1 {
			z := py(math.Max(valAx.lo, 0))
			poly := append([][2]float64{{pts[0][0], z}}, pts...)
			poly = append(poly, [2]float64{pts[len(pts)-1][0], z})
			c := s.col
			p.polyline(poly, true, pdfStyle{Fill: &c})
			continue
		}
		x.drawSeriesLine(p, s, pts)
	}
}

func lastOr(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func (x *pptx) drawSeriesLine(p *pdfPage, s chSeries, pts [][2]float64) {
	c := s.col
	if s.line && len(pts) > 1 {
		p.polyline(pts, false, pdfStyle{Stroke: &c, W: s.w, Dash: s.dash})
	}
	if s.marker {
		m := math.Max(4, s.w*2.5)
		for _, pt := range pts {
			p.shapePath("ellipse", Rect{pt[0] - m/2, pt[1] - m/2, m, m}, 0, pdfStyle{Fill: &c})
		}
	}
}

func (x *pptx) sliceColor(i int) Color {
	if c, ok := x.theme[fmt.Sprintf("accent%d", i%6+1)]; ok {
		return c
	}
	return Hex(chartPalette[i%len(chartPalette)])
}

func (x *pptx) drawPie(p *pdfPage, s chSeries, r Rect, hole bool) {
	tot := 0.0
	for _, v := range s.ys {
		if !math.IsNaN(v) && v > 0 {
			tot += v
		}
	}
	if tot == 0 {
		return
	}
	rad := math.Min(r.W, r.H) / 2 * 0.9
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	ang := -math.Pi / 2
	white := Hex(0xffffff)
	for i, v := range s.ys {
		if math.IsNaN(v) || v <= 0 {
			continue
		}
		sweep := v / tot * 2 * math.Pi
		pts := [][2]float64{{cx, cy}}
		n := int(math.Max(4, sweep*30))
		for k := 0; k <= n; k++ {
			t := ang + sweep*float64(k)/float64(n)
			pts = append(pts, [2]float64{cx + rad*math.Cos(t), cy + rad*math.Sin(t)})
		}
		c := x.sliceColor(i)
		p.polyline(pts, true, pdfStyle{Fill: &c, Stroke: &white, W: 1})
		ang += sweep
	}
	if hole {
		p.shapePath("ellipse", Rect{cx - rad/2, cy - rad/2, rad, rad}, 0, pdfStyle{Fill: &white})
	}
}

// drawCustGeom — фигура произвольной формы (a:custGeom/a:pathLst): отрезки, кривые Безье, дуги.
func (x *pptx) drawCustGeom(p *pdfPage, cg *Node, t xform, m mapper, st pdfStyle) {
	for _, pa := range cg.Kid("pathLst").KidsNamed("path") {
		pw, ph := emuRaw(pa.Av("w")), emuRaw(pa.Av("h"))
		if pw <= 0 {
			pw = t.ext[0] * emuPt
		}
		if ph <= 0 {
			ph = t.ext[1] * emuPt
		}
		pw, ph = math.Max(pw, 1), math.Max(ph, 1)
		// значение координаты: число или имя встроенной величины фигуры
		val := func(s string, full float64) float64 {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				return v
			}
			switch s {
			case "l", "t":
				return 0
			case "r", "b", "w", "h":
				return full
			case "wd2", "hd2":
				return full / 2
			case "wd4", "hd4":
				return full / 4
			}
			return 0
		}
		var segs []pathSeg
		var cur, start [2]float64
		// для дуг считаем в единицах контура, без поворота группы
		var curRaw [2]float64
		toSlide := func(px, py float64) [2]float64 {
			if t.flipH {
				px = pw - px
			}
			if t.flipV {
				py = ph - py
			}
			a, b := m(t.off[0]+px/pw*t.ext[0], t.off[1]+py/ph*t.ext[1])
			return [2]float64{a, b}
		}
		raw := func(n *Node) [2]float64 { return [2]float64{val(n.Av("x"), pw), val(n.Av("y"), ph)} }
		pt := func(n *Node) [2]float64 { r := raw(n); return toSlide(r[0], r[1]) }
		for _, c := range pa.Elems() {
			pts := c.KidsNamed("pt")
			switch c.Name {
			case "moveTo":
				if len(pts) > 0 {
					cur, curRaw = pt(pts[0]), raw(pts[0])
					start = cur
					segs = append(segs, pathSeg{'M', [][2]float64{cur}})
				}
			case "lnTo":
				if len(pts) > 0 {
					cur, curRaw = pt(pts[0]), raw(pts[0])
					segs = append(segs, pathSeg{'L', [][2]float64{cur}})
				}
			case "cubicBezTo":
				if len(pts) == 3 {
					segs = append(segs, pathSeg{'C', [][2]float64{pt(pts[0]), pt(pts[1]), pt(pts[2])}})
					cur, curRaw = pt(pts[2]), raw(pts[2])
				}
			case "quadBezTo":
				if len(pts) == 2 {
					q, e := pt(pts[0]), pt(pts[1])
					c1 := [2]float64{cur[0] + 2.0/3*(q[0]-cur[0]), cur[1] + 2.0/3*(q[1]-cur[1])}
					c2 := [2]float64{e[0] + 2.0/3*(q[0]-e[0]), e[1] + 2.0/3*(q[1]-e[1])}
					segs = append(segs, pathSeg{'C', [][2]float64{c1, c2, e}})
					cur, curRaw = e, raw(pts[1])
				}
			case "arcTo":
				wr, hr := val(c.Av("wR"), pw), val(c.Av("hR"), ph)
				st0, sw := val(c.Av("stAng"), 0)/60000*math.Pi/180, val(c.Av("swAng"), 0)/60000*math.Pi/180
				// центр эллипса: текущая точка лежит на нём под углом st0
				cx, cy := curRaw[0]-wr*math.Cos(st0), curRaw[1]-hr*math.Sin(st0)
				n := max(1, int(math.Ceil(math.Abs(sw)/(math.Pi/2))))
				d := sw / float64(n)
				k := 4.0 / 3 * math.Tan(d/4)
				a := st0
				for i := 0; i < n; i++ {
					b := a + d
					p0 := [2]float64{cx + wr*math.Cos(a), cy + hr*math.Sin(a)}
					p3 := [2]float64{cx + wr*math.Cos(b), cy + hr*math.Sin(b)}
					c1 := [2]float64{p0[0] - k*wr*math.Sin(a), p0[1] + k*hr*math.Cos(a)}
					c2 := [2]float64{p3[0] + k*wr*math.Sin(b), p3[1] - k*hr*math.Cos(b)}
					segs = append(segs, pathSeg{'C', [][2]float64{toSlide(c1[0], c1[1]), toSlide(c2[0], c2[1]), toSlide(p3[0], p3[1])}})
					curRaw = p3
					a = b
				}
				cur = toSlide(curRaw[0], curRaw[1])
			case "close":
				segs = append(segs, pathSeg{'Z', nil})
				cur = start
			}
		}
		ps := st
		if pa.Av("fill") == "none" {
			ps.Fill = nil
		}
		if v := pa.Av("stroke"); v == "0" || v == "false" {
			ps.Stroke = nil
		}
		p.path(segs, ps)
	}
}

func emuRaw(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
