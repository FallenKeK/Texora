package main

// Векторный экспорт в PDF: текст — настоящий текст (шрифты вшиваются урезанными до нужных букв,
// ToUnicode даёт поиск и копирование), линии и заливки — векторами, картинки — как есть.
// Рисует тот же PagePainter, что и экран, только вместо холста — pdfPage (pp.vec).

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"sort"
	"strings"
	"unicode/utf16"
)

// ---------------------------------------------------------------- объекты PDF

type pdfWriter struct {
	objs [][]byte // objs[i] — тело объекта i+1
}

func (w *pdfWriter) alloc() int {
	w.objs = append(w.objs, nil)
	return len(w.objs)
}

func (w *pdfWriter) set(id int, body string) { w.objs[id-1] = []byte(body) }

func (w *pdfWriter) add(body string) int {
	id := w.alloc()
	w.set(id, body)
	return id
}

// stream — поток; dict без << >>, данные сжимаются, если compress.
func (w *pdfWriter) stream(id int, dict string, data []byte, compress bool) {
	if compress {
		var b bytes.Buffer
		zw, _ := zlib.NewWriterLevel(&b, zlib.BestCompression)
		zw.Write(data)
		zw.Close()
		data = b.Bytes()
		dict += " /Filter /FlateDecode"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "<< %s /Length %d >>\nstream\n", strings.TrimSpace(dict), len(data))
	b.Write(data)
	b.WriteString("\nendstream")
	w.objs[id-1] = b.Bytes()
}

func (w *pdfWriter) bytes(root, info int) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(w.objs))
	for i, o := range w.objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n", i+1)
		b.Write(o)
		b.WriteString("\nendobj\n")
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(w.objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(w.objs)+1, root, info, x)
	return b.Bytes()
}

// pdfText — строка PDF в UTF-16BE с BOM (для заголовков закладок и сведений о документе).
func pdfText(s string) string {
	var b strings.Builder
	b.WriteString("<FEFF")
	for _, u := range utf16.Encode([]rune(s)) {
		fmt.Fprintf(&b, "%04X", u)
	}
	b.WriteString(">")
	return b.String()
}

// pdfASCII — строка PDF латиницей (URI): экранируем скобки и обратную косую.
func pdfASCII(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`, "\r", "", "\n", "")
	return "(" + r.Replace(s) + ")"
}

func pdfNum(v float64) string {
	if math.Abs(v-math.Round(v)) < 0.0005 {
		return fmt.Sprintf("%d", int64(math.Round(v)))
	}
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}

func pdfColor(c Color) (r, g, b float64) {
	a := c.A()
	if a <= 0 {
		return 1, 1, 1
	}
	// полупрозрачное — смешиваем с белым листом (в печатной вёрстке такое бывает только у подсветок)
	m := Mix(Hex(0xffffff), c|0xff000000, a)
	return float64(uint32(m)>>16&255) / 255, float64(uint32(m)>>8&255) / 255, float64(uint32(m)&255) / 255
}

// ---------------------------------------------------------------- шрифты

type pdfEmb struct {
	res  string // имя ресурса: F1, F2…
	f    *ttFont
	used map[int][]rune // глиф → буквы (для ToUnicode)
}

type pdfFace struct {
	font       *pdfEmb
	synB, synI bool
}

type pdfFonts struct {
	byFile   map[string]*pdfEmb // путь#индекс
	byFace   map[Font]*pdfFace  // Font без размера
	files    map[string][]byte
	list     []*pdfEmb
	fallback []*pdfEmb
	fbDone   bool
}

// pdfFallbackFamilies — где искать буквы, которых нет в шрифте текста (как подстановка шрифтов в Windows).
var pdfFallbackFamilies = []string{"Cambria Math", "Segoe UI Symbol", "Segoe UI", "Arial", "Times New Roman", "DejaVu Sans"}

func (fs *pdfFonts) load(path string, index int) *pdfEmb {
	key := fmt.Sprintf("%s#%d", path, index)
	if f, ok := fs.byFile[key]; ok {
		return f
	}
	data, ok := fs.files[path]
	if !ok {
		data, _ = os.ReadFile(path)
		fs.files[path] = data
	}
	var pf *pdfEmb
	if tf := parseTTF(data, index); tf != nil && tf.upem > 0 && (tf.glyf != nil || tf.tables["CFF "] != nil) {
		pf = &pdfEmb{res: fmt.Sprintf("F%d", len(fs.list)+1), f: tf, used: map[int][]rune{}}
		fs.list = append(fs.list, pf)
	}
	fs.byFile[key] = pf
	return pf
}

func (fs *pdfFonts) face(f Font) *pdfFace {
	k := Font{Family: f.Family, Bold: f.Bold, Italic: f.Italic}
	if fc, ok := fs.byFace[k]; ok {
		return fc
	}
	fc := &pdfFace{}
	for _, fam := range append([]string{f.Family}, pdfFallbackFamilies...) {
		if path, idx, sb, si, ok := systemFont(fam, f.Bold, f.Italic); ok {
			if pf := fs.load(path, idx); pf != nil {
				fc.font, fc.synB, fc.synI = pf, sb, si
				break
			}
		}
	}
	fs.byFace[k] = fc
	return fc
}

// glyph — шрифт и номер глифа для буквы: сначала шрифт текста, потом запасные.
func (fs *pdfFonts) glyph(main *pdfEmb, r rune) (*pdfEmb, int) {
	if main != nil {
		if g := main.f.cmap(r); g != 0 {
			return main, g
		}
	}
	if !fs.fbDone {
		fs.fbDone = true
		for _, fam := range pdfFallbackFamilies {
			if path, idx, _, _, ok := systemFont(fam, false, false); ok {
				if pf := fs.load(path, idx); pf != nil {
					fs.fallback = append(fs.fallback, pf)
				}
			}
		}
	}
	for _, pf := range fs.fallback {
		if g := pf.f.cmap(r); g != 0 {
			return pf, g
		}
	}
	return main, 0
}

// ---------------------------------------------------------------- страница

type pdfLink struct {
	r    Rect // в пунктах, от левого верхнего угла
	uri  string
	dest string // закладка
}

type pdfAnchor struct {
	page int
	y    float64
}

type pdfPage struct {
	out   *pdfOut
	idx   int
	w, h  float64
	cs    bytes.Buffer
	fonts map[string]bool
	imgs  map[string]int // имя ресурса → объект
	gs    map[string]int // прозрачность: имя ресурса → объект ExtGState
	links []pdfLink
}

func (p *pdfPage) Y(y float64) float64 { return p.h - y }

func (p *pdfPage) fillRect(r Rect, c Color) {
	if c.A() <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	cr, cg, cb := pdfColor(c)
	fmt.Fprintf(&p.cs, "%s %s %s rg %s %s %s %s re f\n", pdfNum(cr), pdfNum(cg), pdfNum(cb),
		pdfNum(r.X), pdfNum(p.Y(r.Y+r.H)), pdfNum(r.W), pdfNum(r.H))
}

func (p *pdfPage) line(x1, y1, x2, y2, w float64, c Color) {
	cr, cg, cb := pdfColor(c)
	fmt.Fprintf(&p.cs, "%s %s %s RG %s w 0 J %s %s m %s %s l S\n", pdfNum(cr), pdfNum(cg), pdfNum(cb), pdfNum(w),
		pdfNum(x1), pdfNum(p.Y(y1)), pdfNum(x2), pdfNum(p.Y(y2)))
}

func (p *pdfPage) strokeRect(r Rect, w float64, c Color) {
	cr, cg, cb := pdfColor(c)
	fmt.Fprintf(&p.cs, "%s %s %s RG %s w %s %s %s %s re S\n", pdfNum(cr), pdfNum(cg), pdfNum(cb), pdfNum(w),
		pdfNum(r.X), pdfNum(p.Y(r.Y+r.H)), pdfNum(r.W), pdfNum(r.H))
}

// text выводит строку с левого края x на базовой линии base; размер — f.Px пунктов.
func (p *pdfPage) text(s string, x, base float64, f Font, c Color) {
	fs := p.out.fonts
	fc := fs.face(f)
	size := f.Px
	cr, cg, cb := pdfColor(c)
	type run struct {
		font *pdfEmb
		gids []int
	}
	var runs []run
	for _, r := range s {
		if r == '\t' || r == ' ' {
			r = ' '
		}
		if r < ' ' || r == '­' || r == '​' {
			continue
		}
		pf, g := fs.glyph(fc.font, r)
		if pf == nil {
			continue
		}
		if _, ok := pf.used[g]; !ok || len(pf.used[g]) == 0 {
			pf.used[g] = []rune{r}
		}
		if len(runs) == 0 || runs[len(runs)-1].font != pf {
			runs = append(runs, run{font: pf})
		}
		runs[len(runs)-1].gids = append(runs[len(runs)-1].gids, g)
	}
	if len(runs) == 0 {
		return
	}
	skew := 0.0
	if fc.synI {
		skew = 0.21 // около 12°, как наклон GDI
	}
	fmt.Fprintf(&p.cs, "BT %s %s %s rg\n", pdfNum(cr), pdfNum(cg), pdfNum(cb))
	if fc.synB {
		fmt.Fprintf(&p.cs, "%s %s %s RG 2 Tr %s w\n", pdfNum(cr), pdfNum(cg), pdfNum(cb), pdfNum(size*0.035))
	}
	pen := x
	for _, rn := range runs {
		p.fonts[rn.font.res] = true
		fmt.Fprintf(&p.cs, "/%s %s Tf 1 0 %s 1 %s %s Tm <", rn.font.res, pdfNum(size), pdfNum(skew), pdfNum(pen), pdfNum(p.Y(base)))
		for _, g := range rn.gids {
			fmt.Fprintf(&p.cs, "%04X", g)
			pen += rn.font.f.advance(g) * size / rn.font.f.upem
		}
		p.cs.WriteString("> Tj\n")
	}
	p.cs.WriteString("ET\n")
}

func (p *pdfPage) image(m *Media, r Rect) {
	if m == nil || r.W <= 0 || r.H <= 0 {
		return
	}
	id := p.out.image(m)
	if id == 0 {
		p.fillRect(r, Hex(0xeeeeee))
		return
	}
	name := fmt.Sprintf("Im%d", id)
	p.imgs[name] = id
	fmt.Fprintf(&p.cs, "q %s 0 0 %s %s %s cm /%s Do Q\n", pdfNum(r.W), pdfNum(r.H), pdfNum(r.X), pdfNum(p.Y(r.Y+r.H)), name)
}

// anchor — где начинается абзац (для закладок PDF и внутренних ссылок).
func (p *pdfPage) anchor(par *Para, y float64) {
	o := p.out
	if _, ok := o.anchors[par]; ok {
		return
	}
	o.anchors[par] = pdfAnchor{page: p.idx, y: y}
	if h := par.PF.Heading; h >= 1 && h <= 3 && par.PF.Region == "" {
		o.heads = append(o.heads, par)
	}
}

func (p *pdfPage) link(r Rect, target string) {
	if target == "" || r.W <= 0 {
		return
	}
	l := pdfLink{r: r}
	if strings.HasPrefix(target, "#") {
		l.dest = target[1:]
	} else {
		l.uri = target
	}
	// соседние куски одной ссылки на строке — одна область
	if n := len(p.links); n > 0 {
		q := &p.links[n-1]
		if q.uri == l.uri && q.dest == l.dest && math.Abs(q.r.Y-r.Y) < 0.5 && r.X-(q.r.X+q.r.W) < 6 && r.X >= q.r.X {
			q.r.W = r.X + r.W - q.r.X
			q.r.H = math.Max(q.r.H, r.H)
			return
		}
	}
	p.links = append(p.links, l)
}

// ---------------------------------------------------------------- документ

type pdfOut struct {
	w       *pdfWriter
	fonts   *pdfFonts
	images  map[*Media]int
	pages   []*pdfPage
	pageIDs []int
	anchors map[*Para]pdfAnchor
	heads   []*Para // заголовки в порядке появления
	gsObj   map[[2]int]int
}

func (o *pdfOut) image(m *Media) int {
	if id, ok := o.images[m]; ok {
		return id
	}
	id := o.embedImage(m.Data)
	o.images[m] = id
	return id
}

func (o *pdfOut) embedImage(data []byte) int {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0
	}
	if format == "jpeg" && (cfg.ColorModel == color.YCbCrModel || cfg.ColorModel == color.GrayModel) {
		cs := "/DeviceRGB"
		if cfg.ColorModel == color.GrayModel {
			cs = "/DeviceGray"
		}
		id := o.w.alloc()
		o.w.stream(id, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode",
			cfg.Width, cfg.Height, cs), data, false)
		return id
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return 0
	}
	rgb := make([]byte, 0, w*h*3)
	alpha := make([]byte, 0, w*h)
	hasAlpha := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			rgb = append(rgb, c.R, c.G, c.B)
			alpha = append(alpha, c.A)
			if c.A != 255 {
				hasAlpha = true
			}
		}
	}
	smask := ""
	if hasAlpha {
		sid := o.w.alloc()
		o.w.stream(sid, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8", w, h), alpha, true)
		smask = fmt.Sprintf(" /SMask %d 0 R", sid)
	}
	// фотографии без прозрачности дешевле хранить JPEG-ом
	if !hasAlpha && format != "png" && format != "gif" {
		var jb bytes.Buffer
		if jpeg.Encode(&jb, img, &jpeg.Options{Quality: 92}) == nil {
			id := o.w.alloc()
			o.w.stream(id, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode", w, h), jb.Bytes(), false)
			return id
		}
	}
	id := o.w.alloc()
	o.w.stream(id, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8%s", w, h, smask), rgb, true)
	return id
}

// ExportPDFVector — PDF с настоящим текстом. tb нужен только для ширины заполнителей табуляции.
func ExportPDFVector(d *Doc, L *Layout, tb TextBackend, progress func(int, int)) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("векторный PDF: %v", r)
		}
	}()
	o := newPdfOut()
	n := len(L.Pages)
	for i, pg := range L.Pages {
		p := o.newPage(pg.G.Page.W, pg.G.Page.H)
		pp := &PagePainter{vec: p, tb: tb, doc: d, k: 1, alpha: 1}
		pp.DrawPage(L, pg, "body", true)
		if progress != nil {
			progress(i+1, n)
		}
	}
	title := ""
	for _, b := range d.Body {
		if b.P != nil && b.P.PF.Style == "Title" { // «Введение» как заголовок файла хуже, чем имя файла
			title = strings.TrimSpace(BlocksToText([]Block{b}))
			break
		}
	}
	return o.finish(title), nil
}

func newPdfOut() *pdfOut {
	return &pdfOut{w: &pdfWriter{}, images: map[*Media]int{}, anchors: map[*Para]pdfAnchor{},
		fonts: &pdfFonts{byFile: map[string]*pdfEmb{}, byFace: map[Font]*pdfFace{}, files: map[string][]byte{}}}
}

// newPage — новый лист w×h пунктов.
func (o *pdfOut) newPage(w, h float64) *pdfPage {
	p := &pdfPage{out: o, idx: len(o.pages), w: w, h: h, fonts: map[string]bool{}, imgs: map[string]int{}, gs: map[string]int{}}
	o.pages = append(o.pages, p)
	o.pageIDs = append(o.pageIDs, o.w.alloc())
	return p
}

// finish — шрифты, листы, ссылки, закладки и сведения о файле; готовый PDF.
func (o *pdfOut) finish(title string) []byte {
	catalog, pagesID, info := o.w.alloc(), o.w.alloc(), o.w.alloc()
	n := len(o.pages)
	// шрифты (после всех страниц: известно, какие буквы нужны)
	fontRef := map[string]int{}
	for _, pf := range o.fonts.list {
		if len(pf.used) > 0 {
			fontRef[pf.res] = o.embedFont(pf)
		}
	}
	bk := map[string]pdfAnchor{}
	for p, a := range o.anchors {
		if p.PF.Bookmark != "" {
			if _, ok := bk[p.PF.Bookmark]; !ok || a.page < bk[p.PF.Bookmark].page {
				bk[p.PF.Bookmark] = a
			}
		}
	}
	dest := func(a pdfAnchor) string {
		return fmt.Sprintf("[%d 0 R /XYZ 0 %s null]", o.pageIDs[a.page], pdfNum(o.pages[a.page].Y(math.Max(0, a.y-6))))
	}
	var kids []string
	for i, p := range o.pages {
		var res strings.Builder
		res.WriteString("<< /ProcSet [/PDF /Text /ImageB /ImageC]")
		if len(p.fonts) > 0 {
			res.WriteString(" /Font <<")
			for _, k := range sortedKeys(p.fonts) {
				fmt.Fprintf(&res, " /%s %d 0 R", k, fontRef[k])
			}
			res.WriteString(" >>")
		}
		if len(p.gs) > 0 {
			res.WriteString(" /ExtGState <<")
			for _, k := range sortedKeys(p.gs) {
				fmt.Fprintf(&res, " /%s %d 0 R", k, p.gs[k])
			}
			res.WriteString(" >>")
		}
		if len(p.imgs) > 0 {
			res.WriteString(" /XObject <<")
			for _, k := range sortedKeys(p.imgs) {
				fmt.Fprintf(&res, " /%s %d 0 R", k, p.imgs[k])
			}
			res.WriteString(" >>")
		}
		res.WriteString(" >>")
		var annots []string
		for _, l := range p.links {
			rect := fmt.Sprintf("[%s %s %s %s]", pdfNum(l.r.X), pdfNum(p.Y(l.r.Y+l.r.H)), pdfNum(l.r.X+l.r.W), pdfNum(p.Y(l.r.Y)))
			if l.uri != "" {
				annots = append(annots, fmt.Sprintf("%d 0 R", o.w.add(fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect %s /Border [0 0 0] /A << /S /URI /URI %s >> >>", rect, pdfASCII(l.uri)))))
			} else if a, ok := bk[l.dest]; ok {
				annots = append(annots, fmt.Sprintf("%d 0 R", o.w.add(fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect %s /Border [0 0 0] /Dest %s >>", rect, dest(a)))))
			}
		}
		cid := o.w.alloc()
		o.w.stream(cid, "", p.cs.Bytes(), true)
		an := ""
		if len(annots) > 0 {
			an = " /Annots [" + strings.Join(annots, " ") + "]"
		}
		o.w.set(o.pageIDs[i], fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Resources %s /Contents %d 0 R%s >>",
			pagesID, pdfNum(p.w), pdfNum(p.h), res.String(), cid, an))
		kids = append(kids, fmt.Sprintf("%d 0 R", o.pageIDs[i]))
	}
	o.w.set(pagesID, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	// закладки PDF по заголовкам
	cat := fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R", pagesID)
	if ol := o.outline(dest); ol != 0 {
		cat += fmt.Sprintf(" /Outlines %d 0 R /PageMode /UseOutlines", ol)
	}
	o.w.set(catalog, cat+" >>")
	inf := "<< /Producer (Texora " + AppVersion + ") /Creator (Texora)"
	if title != "" {
		inf += " /Title " + pdfText(title)
	}
	o.w.set(info, inf+" >>")
	return o.w.bytes(catalog, info)
}

// outline — дерево закладок PDF из заголовков 1–3 (оглавление и его строки сами в него не попадают).
func (o *pdfOut) outline(dest func(pdfAnchor) string) int {
	type node struct {
		id, lvl int
		title   string
		a       pdfAnchor
		kids    []*node
		parent  *node
	}
	root := &node{lvl: 0}
	stack := []*node{root}
	for _, p := range o.heads {
		lvl := p.PF.Heading
		t := strings.Join(strings.Fields(BlocksToText([]Block{{P: p}})), " ")
		if t == "" {
			continue
		}
		for len(stack) > 1 && stack[len(stack)-1].lvl >= lvl {
			stack = stack[:len(stack)-1]
		}
		par := stack[len(stack)-1]
		nd := &node{lvl: lvl, title: t, a: o.anchors[p], parent: par}
		par.kids = append(par.kids, nd)
		stack = append(stack, nd)
	}
	if len(root.kids) == 0 {
		return 0
	}
	var assign func(n *node)
	assign = func(n *node) {
		n.id = o.w.alloc()
		for _, k := range n.kids {
			assign(k)
		}
	}
	assign(root)
	var count func(n *node) int
	count = func(n *node) int {
		c := len(n.kids)
		for _, k := range n.kids {
			c += count(k)
		}
		return c
	}
	var write func(n *node)
	write = func(n *node) {
		var b strings.Builder
		if n == root {
			b.WriteString("<< /Type /Outlines")
		} else {
			fmt.Fprintf(&b, "<< /Title %s /Parent %d 0 R /Dest %s", pdfText(n.title), n.parent.id, dest(n.a))
			sib := n.parent.kids
			for i, k := range sib {
				if k == n {
					if i > 0 {
						fmt.Fprintf(&b, " /Prev %d 0 R", sib[i-1].id)
					}
					if i+1 < len(sib) {
						fmt.Fprintf(&b, " /Next %d 0 R", sib[i+1].id)
					}
				}
			}
		}
		if len(n.kids) > 0 {
			c := count(n)
			if n != root && n.lvl >= 2 {
				c = -c // глубокие уровни свёрнуты
			}
			fmt.Fprintf(&b, " /First %d 0 R /Last %d 0 R /Count %d", n.kids[0].id, n.kids[len(n.kids)-1].id, c)
		}
		b.WriteString(" >>")
		o.w.set(n.id, b.String())
		for _, k := range n.kids {
			write(k)
		}
	}
	write(root)
	return root.id
}

// ---------------------------------------------------------------- вшивание шрифта

func (o *pdfOut) embedFont(pf *pdfEmb) int {
	f := pf.f
	k := 1000 / f.upem
	name := "Font"
	if best, _ := sfntNames(f.tables["name"]); best[6] != "" {
		name = best[6]
	} else if best[1] != "" {
		name = best[1]
	}
	name = strings.Map(func(r rune) rune {
		if r > ' ' && r < 127 && !strings.ContainsRune("()<>[]{}/%#", r) {
			return r
		}
		return -1
	}, name)
	if name == "" {
		name = "Font"
	}
	// метка подмножества: шесть заглавных букв, разные для разных шрифтов
	tag := []byte("TXAAAA")
	num := 0
	fmt.Sscanf(pf.res, "F%d", &num)
	for i, n := 5, num; i >= 2; i-- {
		tag[i] = byte('A' + n%26)
		n /= 26
	}
	base := string(tag) + "+" + name

	gids := make([]int, 0, len(pf.used))
	for g := range pf.used {
		gids = append(gids, g)
	}
	sort.Ints(gids)

	var fileRef, cidType string
	if f.glyf != nil {
		sub := subsetTTF(f, pf.used)
		id := o.w.alloc()
		o.w.stream(id, fmt.Sprintf("/Length1 %d", len(sub)), sub, true)
		fileRef, cidType = fmt.Sprintf("/FontFile2 %d 0 R", id), "/CIDFontType2"
	} else {
		// CFF (OpenType .otf): вшиваем файл целиком
		data := f.data
		if string(data[:4]) == "ttcf" {
			data = ttcExtract(f)
		}
		id := o.w.alloc()
		o.w.stream(id, "/Subtype /OpenType", data, true)
		fileRef, cidType = fmt.Sprintf("/FontFile3 %d 0 R", id), "/CIDFontType0"
	}
	head := f.tables["head"]
	bbox := fmt.Sprintf("[%s %s %s %s]", pdfNum(float64(s16(head, 36))*k), pdfNum(float64(s16(head, 38))*k),
		pdfNum(float64(s16(head, 40))*k), pdfNum(float64(s16(head, 42))*k))
	italic, flags := 0.0, 4
	if post := f.tables["post"]; len(post) >= 8 {
		italic = float64(int32(u32(post, 4))) / 65536
	}
	if italic != 0 {
		flags |= 64
	}
	capH := f.asc * 0.7
	if os2 := f.tables["OS/2"]; len(os2) >= 90 && u16(os2, 0) >= 2 {
		capH = float64(s16(os2, 88))
	}
	desc := o.w.add(fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags %d /FontBBox %s /ItalicAngle %s /Ascent %s /Descent %s /CapHeight %s /StemV 80 %s >>",
		base, flags, bbox, pdfNum(italic), pdfNum(f.asc*k), pdfNum(-f.desc*k), pdfNum(capH*k), fileRef))

	var w strings.Builder
	for _, g := range gids {
		fmt.Fprintf(&w, "%d [%s] ", g, pdfNum(f.advance(g)*k))
	}
	cid := o.w.add(fmt.Sprintf("<< /Type /Font /Subtype %s /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /W [%s] %s>>",
		cidType, base, desc, strings.TrimSpace(w.String()), map[bool]string{true: "/CIDToGIDMap /Identity ", false: ""}[f.glyf != nil]))

	// ToUnicode: глиф → буквы, чтобы текст искался и копировался
	var cm strings.Builder
	cm.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	for i := 0; i < len(gids); i += 100 {
		part := gids[i:min(i+100, len(gids))]
		fmt.Fprintf(&cm, "%d beginbfchar\n", len(part))
		for _, g := range part {
			fmt.Fprintf(&cm, "<%04X> <", g)
			for _, u := range utf16.Encode(pf.used[g]) {
				fmt.Fprintf(&cm, "%04X", u)
			}
			cm.WriteString(">\n")
		}
		cm.WriteString("endbfchar\n")
	}
	cm.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	tu := o.w.alloc()
	o.w.stream(tu, "", []byte(cm.String()), true)

	return o.w.add(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>", base, cid, tu))
}

// subsetTTF — шрифт только с нужными глифами. Номера глифов сохраняются (остальные пустые),
// поэтому в PDF можно писать номера глифов как есть (CIDToGIDMap /Identity).
func subsetTTF(f *ttFont, used map[int][]rune) []byte {
	numGlyphs := u16(f.tables["maxp"], 4)
	keep := map[int]bool{0: true}
	var add func(g, depth int)
	add = func(g, depth int) {
		if keep[g] && g != 0 || g >= numGlyphs || depth > 8 {
			return
		}
		keep[g] = true
		d := f.glyphData(g)
		if len(d) < 10 || s16(d, 0) >= 0 {
			return
		}
		// составной глиф: добавляем части
		o := 10
		for o+4 <= len(d) {
			flags, comp := u16(d, o), u16(d, o+2)
			add(comp, depth+1)
			o += 4
			if flags&1 != 0 {
				o += 4
			} else {
				o += 2
			}
			switch {
			case flags&8 != 0:
				o += 2
			case flags&0x40 != 0:
				o += 4
			case flags&0x80 != 0:
				o += 8
			}
			if flags&0x20 == 0 {
				break
			}
		}
	}
	for g := range used {
		add(g, 0)
	}
	var glyf bytes.Buffer
	loca := make([]byte, 4*(numGlyphs+1))
	for g := 0; g < numGlyphs; g++ {
		putU32(loca, g*4, uint32(glyf.Len()))
		if keep[g] {
			glyf.Write(f.glyphData(g))
			for glyf.Len()%4 != 0 {
				glyf.WriteByte(0)
			}
		}
	}
	putU32(loca, numGlyphs*4, uint32(glyf.Len()))
	head := append([]byte(nil), f.tables["head"]...)
	putU32(head, 8, 0)
	head[50], head[51] = 0, 1 // длинная loca
	tables := map[string][]byte{"head": head, "loca": loca, "glyf": glyf.Bytes()}
	for _, t := range []string{"cmap", "hhea", "hmtx", "maxp", "cvt ", "fpgm", "prep", "OS/2"} {
		if b := f.tables[t]; b != nil {
			tables[t] = b
		}
	}
	if post := f.tables["post"]; len(post) >= 32 {
		p := append([]byte(nil), post[:32]...)
		putU32(p, 0, 0x00030000) // формат 3: без имён глифов (они могут весить больше самих букв)
		tables["post"] = p
	}
	out := sfntBuild(tables)
	// поправка контрольной суммы в head
	sum := uint32(0)
	for i := 0; i+4 <= len(out); i += 4 {
		sum += uint32(u32(out, i))
	}
	if ho := sfntTableOffset(out, "head"); ho >= 0 {
		putU32(out, ho+8, 0xB1B0AFBA-sum)
	}
	return out
}

func putU32(b []byte, o int, v uint32) {
	b[o], b[o+1], b[o+2], b[o+3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

func sfntChecksum(b []byte) uint32 {
	sum := uint32(0)
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		sum += uint32(w[0])<<24 | uint32(w[1])<<16 | uint32(w[2])<<8 | uint32(w[3])
	}
	return sum
}

// sfntBuild собирает файл шрифта из таблиц.
func sfntBuild(tables map[string][]byte) []byte {
	ver := uint32(0x00010000)
	if tables["CFF "] != nil {
		ver = 0x4F54544F // OTTO
	}
	tags := sortedKeys(tables)
	n := len(tags)
	es, sr := 0, 1
	for sr*2 <= n {
		sr *= 2
		es++
	}
	var b bytes.Buffer
	hdr := make([]byte, 12+16*n)
	putU32(hdr, 0, ver)
	hdr[4], hdr[5] = byte(n>>8), byte(n)
	hdr[6], hdr[7] = byte(sr*16>>8), byte(sr*16)
	hdr[8], hdr[9] = byte(es>>8), byte(es)
	hdr[10], hdr[11] = byte((n*16-sr*16)>>8), byte(n*16-sr*16)
	off := len(hdr)
	var body bytes.Buffer
	for i, t := range tags {
		d := tables[t]
		o := 12 + i*16
		copy(hdr[o:], t)
		putU32(hdr, o+4, sfntChecksum(d))
		putU32(hdr, o+8, uint32(off+body.Len()))
		putU32(hdr, o+12, uint32(len(d)))
		body.Write(d)
		for body.Len()%4 != 0 {
			body.WriteByte(0)
		}
	}
	b.Write(hdr)
	b.Write(body.Bytes())
	return b.Bytes()
}

func sfntTableOffset(font []byte, tag string) int {
	n := u16(font, 4)
	for i := 0; i < n; i++ {
		o := 12 + i*16
		if string(font[o:o+4]) == tag {
			return u32(font, o+8)
		}
	}
	return -1
}

// ttcExtract — отдельный файл шрифта из коллекции (для CFF, который не урезается).
func ttcExtract(f *ttFont) []byte {
	return sfntBuild(f.tables)
}

// ---------------------------------------------------------------- фигуры (для слайдов)

// pdfStyle — заливка и обводка фигуры (nil — нет).
type pdfStyle struct {
	Fill, Stroke *Color
	W            float64   // толщина обводки, пт
	Dash         []float64 // пунктир (длины штрихов и промежутков), nil — сплошная
}

func (p *pdfPage) applyStyle(st pdfStyle) string {
	var b strings.Builder
	fa, sa := 1.0, 1.0
	if st.Fill != nil {
		fa = st.Fill.A()
	}
	if st.Stroke != nil {
		sa = st.Stroke.A()
	}
	if fa < 0.999 || sa < 0.999 {
		b.WriteString("/" + p.alphaGS(fa, sa) + " gs ")
	}
	if st.Fill != nil {
		r, g, bl := pdfColor(*st.Fill)
		fmt.Fprintf(&b, "%s %s %s rg ", pdfNum(r), pdfNum(g), pdfNum(bl))
	}
	if st.Stroke != nil {
		r, g, bl := pdfColor(*st.Stroke)
		fmt.Fprintf(&b, "%s %s %s RG %s w ", pdfNum(r), pdfNum(g), pdfNum(bl), pdfNum(math.Max(0.1, st.W)))
		if len(st.Dash) > 0 {
			b.WriteString("[")
			for _, d := range st.Dash {
				b.WriteString(pdfNum(d) + " ")
			}
			b.WriteString("] 0 d ")
		} else {
			b.WriteString("[] 0 d ")
		}
	}
	return b.String()
}

func paintOp(st pdfStyle) string {
	switch {
	case st.Fill != nil && st.Stroke != nil:
		return "B"
	case st.Fill != nil:
		return "f"
	case st.Stroke != nil:
		return "S"
	}
	return "n"
}

// alphaGS — ресурс ExtGState с прозрачностью заливки и обводки.
func (p *pdfPage) alphaGS(fill, stroke float64) string {
	o := p.out
	key := [2]int{int(math.Round(fill * 1000)), int(math.Round(stroke * 1000))}
	if o.gsObj == nil {
		o.gsObj = map[[2]int]int{}
	}
	id, ok := o.gsObj[key]
	if !ok {
		id = o.w.add(fmt.Sprintf("<< /Type /ExtGState /ca %s /CA %s >>", pdfNum(float64(key[0])/1000), pdfNum(float64(key[1])/1000)))
		o.gsObj[key] = id
	}
	name := fmt.Sprintf("GS%d_%d", key[0], key[1])
	p.gs[name] = id
	return name
}

// visible — без невидимых частей (прозрачность 0): их не рисуем вовсе.
func (st pdfStyle) visible() pdfStyle {
	if st.Fill != nil && st.Fill.A() < 0.002 {
		st.Fill = nil
	}
	if st.Stroke != nil && st.Stroke.A() < 0.002 {
		st.Stroke = nil
	}
	return st
}

// pathSeg — отрезок контура: M (точка), L (точка), C (три точки), Z.
type pathSeg struct {
	op  byte
	pts [][2]float64
}

// path — произвольный контур (координаты сверху вниз).
func (p *pdfPage) path(segs []pathSeg, st pdfStyle) {
	st = st.visible()
	if len(segs) == 0 || (st.Fill == nil && st.Stroke == nil) {
		return
	}
	var b strings.Builder
	b.WriteString("q 1 J 1 j " + p.applyStyle(st))
	for _, sg := range segs {
		for _, pt := range sg.pts {
			fmt.Fprintf(&b, "%s %s ", pdfNum(pt[0]), pdfNum(p.Y(pt[1])))
		}
		switch sg.op {
		case 'M':
			b.WriteString("m ")
		case 'L':
			b.WriteString("l ")
		case 'C':
			b.WriteString("c ")
		case 'Z':
			b.WriteString("h ")
		}
	}
	b.WriteString(paintOp(st) + " Q\n")
	p.cs.WriteString(b.String())
}

// shapePath рисует фигуру: rect, roundRect (rad — радиус), ellipse; координаты — сверху вниз.
func (p *pdfPage) shapePath(kind string, r Rect, rad float64, st pdfStyle) {
	st = st.visible()
	if st.Fill == nil && st.Stroke == nil {
		return
	}
	var b strings.Builder
	b.WriteString("q " + p.applyStyle(st))
	x0, y0, x1, y1 := r.X, p.Y(r.Y+r.H), r.X+r.W, p.Y(r.Y)
	const k = 0.5523 // приближение дуги кривой Безье
	switch kind {
	case "ellipse":
		cx, cy, rx, ry := (x0+x1)/2, (y0+y1)/2, r.W/2, r.H/2
		fmt.Fprintf(&b, "%s %s m ", pdfNum(cx+rx), pdfNum(cy))
		fmt.Fprintf(&b, "%s %s %s %s %s %s c ", pdfNum(cx+rx), pdfNum(cy+ry*k), pdfNum(cx+rx*k), pdfNum(cy+ry), pdfNum(cx), pdfNum(cy+ry))
		fmt.Fprintf(&b, "%s %s %s %s %s %s c ", pdfNum(cx-rx*k), pdfNum(cy+ry), pdfNum(cx-rx), pdfNum(cy+ry*k), pdfNum(cx-rx), pdfNum(cy))
		fmt.Fprintf(&b, "%s %s %s %s %s %s c ", pdfNum(cx-rx), pdfNum(cy-ry*k), pdfNum(cx-rx*k), pdfNum(cy-ry), pdfNum(cx), pdfNum(cy-ry))
		fmt.Fprintf(&b, "%s %s %s %s %s %s c h ", pdfNum(cx+rx*k), pdfNum(cy-ry), pdfNum(cx+rx), pdfNum(cy-ry*k), pdfNum(cx+rx), pdfNum(cy))
	case "roundRect":
		q := math.Min(rad, math.Min(r.W, r.H)/2)
		fmt.Fprintf(&b, "%s %s m ", pdfNum(x0+q), pdfNum(y0))
		fmt.Fprintf(&b, "%s %s l %s %s %s %s %s %s c ", pdfNum(x1-q), pdfNum(y0), pdfNum(x1-q+q*k), pdfNum(y0), pdfNum(x1), pdfNum(y0+q-q*k), pdfNum(x1), pdfNum(y0+q))
		fmt.Fprintf(&b, "%s %s l %s %s %s %s %s %s c ", pdfNum(x1), pdfNum(y1-q), pdfNum(x1), pdfNum(y1-q+q*k), pdfNum(x1-q+q*k), pdfNum(y1), pdfNum(x1-q), pdfNum(y1))
		fmt.Fprintf(&b, "%s %s l %s %s %s %s %s %s c ", pdfNum(x0+q), pdfNum(y1), pdfNum(x0+q-q*k), pdfNum(y1), pdfNum(x0), pdfNum(y1-q+q*k), pdfNum(x0), pdfNum(y1-q))
		fmt.Fprintf(&b, "%s %s l %s %s %s %s %s %s c h ", pdfNum(x0), pdfNum(y0+q), pdfNum(x0), pdfNum(y0+q-q*k), pdfNum(x0+q-q*k), pdfNum(y0), pdfNum(x0+q), pdfNum(y0))
	default:
		fmt.Fprintf(&b, "%s %s %s %s re ", pdfNum(x0), pdfNum(y0), pdfNum(r.W), pdfNum(r.H))
	}
	b.WriteString(paintOp(st) + " Q\n")
	p.cs.WriteString(b.String())
}

// polyline — ломаная по точкам (closed — замкнуть); координаты — сверху вниз.
func (p *pdfPage) polyline(pts [][2]float64, closed bool, st pdfStyle) {
	st = st.visible()
	if len(pts) < 2 || (st.Fill == nil && st.Stroke == nil) {
		return
	}
	var b strings.Builder
	b.WriteString("q 1 J 1 j " + p.applyStyle(st))
	for i, pt := range pts {
		op := "l"
		if i == 0 {
			op = "m"
		}
		fmt.Fprintf(&b, "%s %s %s ", pdfNum(pt[0]), pdfNum(p.Y(pt[1])), op)
	}
	if closed {
		b.WriteString("h ")
	}
	b.WriteString(paintOp(st) + " Q\n")
	p.cs.WriteString(b.String())
}

// clip — дальнейшее рисование только внутри r (до unclip).
func (p *pdfPage) clip(r Rect) {
	fmt.Fprintf(&p.cs, "q %s %s %s %s re W n\n", pdfNum(r.X), pdfNum(p.Y(r.Y+r.H)), pdfNum(r.W), pdfNum(r.H))
}

func (p *pdfPage) unclip() { p.cs.WriteString("Q\n") }

// textWidth — ширина строки теми же шрифтами, что попадут в PDF (для переноса строк в слайдах).
func (o *pdfOut) textWidth(s string, f Font) float64 {
	fc := o.fonts.face(f)
	w := 0.0
	for _, r := range s {
		if r == '\t' {
			r = ' '
		}
		pf, g := o.fonts.glyph(fc.font, r)
		if pf == nil {
			continue
		}
		w += pf.f.advance(g) * f.Px / pf.f.upem
	}
	return w
}

// fontMetrics — высота над и под базовой линией и межстрочный зазор (пт) для шрифта f.
func (o *pdfOut) fontMetrics(f Font) (asc, desc, gap float64) {
	fc := o.fonts.face(f)
	if fc.font == nil {
		return f.Px * 0.9, f.Px * 0.22, 0
	}
	t := fc.font.f
	k := f.Px / t.upem
	return t.asc * k, t.desc * k, t.gap * k
}

// rotated — fn рисует в системе, повёрнутой на deg градусов против часовой вокруг (cx, cy).
func (p *pdfPage) rotated(cx, cy, deg float64, fn func()) {
	a := deg * math.Pi / 180
	c, s := math.Cos(a), math.Sin(a)
	y := p.Y(cy)
	fmt.Fprintf(&p.cs, "q %s %s %s %s %s %s cm\n", pdfNum(c), pdfNum(s), pdfNum(-s), pdfNum(c),
		pdfNum(cx-cx*c+y*s), pdfNum(y-cx*s-y*c))
	fn()
	p.cs.WriteString("Q\n")
}
