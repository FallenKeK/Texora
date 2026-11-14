package main

// Просмотр PDF во вкладке Texora: листы рисуются фоновым потоком в кэш,
// интерфейс берёт готовые картинки. Поиск, выделение и копирование текста,
// миниатюры и оглавление в области навигации, ссылки внутри файла.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type pdfBmp struct {
	scale float64
	w, h  int
	pix   []uint32
}

type pdfReq struct {
	page  int
	scale float64
	thumb bool
}

type pdfPos struct{ page, g int }

type pdfFound struct {
	page       int
	start, end int // руны в тексте листа
	rects      []Rect
}

type PdfView struct {
	Doc  *PdfDoc
	Path string
	inv  func()

	mu     sync.Mutex
	pages  map[int]*pdfBmp
	thumbs map[int]*pdfBmp
	glyphs map[int][]PdfGlyph
	links  map[int][]PdfLink
	want   []pdfReq
	wake   chan struct{}
	closed bool
	busy   bool

	docMu sync.Mutex // сам разбор PDF не потокобезопасен
	dest  func(any) (int, float64)

	ys  []float64
	ysK float64

	// поиск
	query     string
	qCase     bool
	gen       int
	found     *pdfFound
	pending   *pdfFound
	pendMsg   string
	searching bool
	dlg       *Dialog

	// выделение
	selA, selB pdfPos
	selOn      bool
	fitPending bool
	navCur     int
}

var errPDFEmpty = errors.New("в файле нет ни одной страницы")

func isPDF(p string, data []byte) bool {
	return strings.EqualFold(filepath.Ext(p), ".pdf") || bytes.HasPrefix(bytes.TrimLeft(data[:min(len(data), 1024)], "\x00\r\n\t "), []byte("%PDF"))
}

func NewPdfView(path string, data []byte, inv func()) (v *PdfView, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("файл повреждён (%v)", r)
		}
	}()
	d, err := LoadPDF(data)
	if err != nil {
		return nil, err
	}
	if len(d.Pages) == 0 {
		return nil, errPDFEmpty
	}
	v = &PdfView{Doc: d, Path: path, inv: inv, pages: map[int]*pdfBmp{}, thumbs: map[int]*pdfBmp{},
		glyphs: map[int][]PdfGlyph{}, links: map[int][]PdfLink{}, wake: make(chan struct{}, 1), fitPending: true}
	v.dest = d.F.destResolver(d.Pages)
	go v.loop()
	return v, nil
}

func (v *PdfView) Close() {
	v.mu.Lock()
	v.closed = true
	v.gen++
	v.mu.Unlock()
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

func (v *PdfView) kick() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

// maxPix — предел размера одной картинки листа (дальше растягиваем).
const pdfMaxPix = 18_000_000

func (v *PdfView) capScale(i int, s float64) float64 {
	p := v.Doc.Pages[i]
	if px := p.W() * p.H() * s * s; px > pdfMaxPix {
		s *= math.Sqrt(pdfMaxPix / px)
	}
	return s
}

func near(a, b float64) bool { return math.Abs(a-b) <= 0.01*b }

func (v *PdfView) needs(r pdfReq) bool {
	m := v.pages
	if r.thumb {
		m = v.thumbs
	}
	b := m[r.page]
	return b == nil || !near(b.scale, r.scale)
}

func (v *PdfView) loop() {
	for range v.wake {
		for {
			v.mu.Lock()
			if v.closed {
				v.mu.Unlock()
				return
			}
			var job *pdfReq
			for _, r := range v.want {
				if v.needs(r) {
					r := r
					job = &r
					break
				}
			}
			v.busy = job != nil
			v.mu.Unlock()
			if job == nil {
				break
			}
			img, gl, links := v.render(job.page, job.scale)
			b := toBmp(img, job.scale)
			v.mu.Lock()
			if job.thumb {
				v.thumbs[job.page] = b
			} else {
				v.pages[job.page] = b
				v.glyphs[job.page] = gl
				v.links[job.page] = links
			}
			v.evict()
			v.mu.Unlock()
			if v.inv != nil {
				v.inv()
			}
		}
	}
}

func (v *PdfView) render(i int, scale float64) (img *image.RGBA, gl []PdfGlyph, links []PdfLink) {
	v.docMu.Lock()
	defer v.docMu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			img = nil
		}
	}()
	img, gl = v.Doc.RenderPage(i, scale, false)
	func() {
		defer func() { recover() }()
		links = v.Doc.F.Links(v.Doc.Pages, i, v.dest)
	}()
	return
}

func toBmp(img *image.RGBA, scale float64) *pdfBmp {
	if img == nil {
		return &pdfBmp{scale: scale}
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	b := &pdfBmp{scale: scale, w: w, h: h, pix: make([]uint32, w*h)}
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			b.pix[y*w+x] = 0xff000000 | uint32(row[x*4])<<16 | uint32(row[x*4+1])<<8 | uint32(row[x*4+2])
		}
	}
	return b
}

// evict — держим в памяти только нужное сейчас (+ немного соседей).
func (v *PdfView) evict() {
	keep := map[int]bool{}
	keepT := map[int]bool{}
	for _, r := range v.want {
		if r.thumb {
			keepT[r.page] = true
		} else {
			keep[r.page] = true
		}
	}
	total := 0
	for _, b := range v.pages {
		total += b.w * b.h
	}
	if total > 3*pdfMaxPix || len(v.pages) > 10 {
		for i, b := range v.pages {
			if !keep[i] {
				total -= b.w * b.h
				delete(v.pages, i)
			}
		}
	}
	if len(v.thumbs) > 120 {
		for i := range v.thumbs {
			if !keepT[i] {
				delete(v.thumbs, i)
			}
		}
	}
}

// pageGlyphs — текст листа (берём из кэша или извлекаем без отрисовки).
func (v *PdfView) pageGlyphs(i int) []PdfGlyph {
	v.mu.Lock()
	gl, ok := v.glyphs[i]
	v.mu.Unlock()
	if ok {
		return gl
	}
	v.docMu.Lock()
	func() {
		defer func() { recover() }()
		_, gl = v.Doc.RenderPage(i, 1, true)
	}()
	v.docMu.Unlock()
	v.mu.Lock()
	v.glyphs[i] = gl
	v.mu.Unlock()
	return gl
}

func (v *PdfView) cachedGlyphs(i int) []PdfGlyph {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.glyphs[i]
}

func (v *PdfView) pageY(a *App, i int) float64 {
	gap := a.d(22)
	n := len(v.Doc.Pages)
	k := a.k()
	if len(v.ys) != n+1 || v.ysK != k {
		v.ys = make([]float64, n+1)
		y := gap
		for j, p := range v.Doc.Pages {
			v.ys[j] = y
			y += p.H()*k + gap
		}
		v.ys[n] = y
		v.ysK = k
	}
	return v.ys[max(0, min(i, n))]
}

// ---------------------------------------------------------------- открытие
func (a *App) openPDF(p string, data []byte) {
	v, err := NewPdfView(p, data, a.invalidate)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, ErrPDFPassword) {
			msg = "Этот PDF защищён паролем. Открытие защищённых файлов пока не поддерживается."
		}
		a.errorBox(a.trf("Не удалось открыть «%s»:", filepath.Base(p)) + "\n" + a.tr(msg))
		return
	}
	reuse := a.pristine() && a.pdf == nil && a.cur >= 0
	a.stash()
	e := NewEditor(a.M)
	e.Author = a.authorName()
	e.SetDoc(NewDoc())
	t := &DocTab{E: e, FileName: filepath.Base(p), FilePath: p, zoom: 1, PageCache: map[string]*scaledImg{}, Pdf: v}
	if reuse {
		a.tabs[a.cur] = t
		i := a.cur
		a.cur = -1
		a.activate(i)
	} else {
		a.tabs = append(a.tabs, t)
		a.cur = -1
		a.activate(len(a.tabs) - 1)
	}
	a.backstage = false
	a.addRecent(p)
	if isPptx(p) {
		a.msg(a.trf("Презентация: %d слайд(ов) — только просмотр", len(v.Doc.Pages)))
	} else {
		a.msg(a.trf("PDF: %d стр. — только чтение", len(v.Doc.Pages)))
	}
}

// ---------------------------------------------------------------- отрисовка
func (a *App) paintPDF(cv *Canvas) {
	v := a.pdf
	th := a.th
	if v.fitPending && a.docRect.W > 0 {
		v.fitPending = false
		a.fitWidth()
		if a.zoom > 1.6 {
			a.SetZoom(1.6)
		}
		a.clampScroll()
	}
	a.pdfPoll()
	k := a.k()
	n := len(v.Doc.Pages)
	top, bot := a.scrollY, a.scrollY+a.docRect.H
	first, last := -1, -1
	for i := 0; i < n; i++ {
		y0 := a.pageY(i)
		_, h := a.pageSize(i)
		if y0+h >= top && y0 <= bot {
			if first < 0 {
				first = i
			}
			last = i
		} else if first >= 0 {
			break
		}
	}
	var want []pdfReq
	if first >= 0 {
		mid := (top + bot) / 2
		vis := []int{}
		for i := first; i <= last; i++ {
			vis = append(vis, i)
		}
		sort.SliceStable(vis, func(x, y int) bool {
			_, hx := a.pageSize(vis[x])
			_, hy := a.pageSize(vis[y])
			return math.Abs(a.pageY(vis[x])+hx/2-mid) < math.Abs(a.pageY(vis[y])+hy/2-mid)
		})
		for _, i := range vis {
			want = append(want, pdfReq{page: i, scale: v.capScale(i, k)})
		}
		for _, i := range []int{last + 1, first - 1} {
			if i >= 0 && i < n {
				want = append(want, pdfReq{page: i, scale: v.capScale(i, k)})
			}
		}
	}
	want = append(want, a.pdfThumbWants()...)
	v.mu.Lock()
	v.want = want
	v.mu.Unlock()
	v.kick()

	sel := a.pdfSelRange()
	for i := max(0, first); first >= 0 && i <= last; i++ {
		pw, ph := a.pageSize(i)
		pr := Rect{math.Round(a.pageLeftOf(i)), math.Round(a.pageTop(i)), math.Round(pw), math.Round(ph)}
		cv.Shadow(pr, a.d(2), a.d(10), th.PageShadow)
		cv.FillRect(pr, Hex(0xffffff))
		v.mu.Lock()
		b := v.pages[i]
		links := v.links[i]
		gl := v.glyphs[i]
		v.mu.Unlock()
		if b != nil && b.w > 0 {
			blitBmp(cv, b, pr)
		} else if b == nil {
			a.centerText(cv, "Загрузка…", Rect{pr.X, pr.Y + a.d(30), pr.W, a.d(24)}, a.uiFont(13), Hex(0x9a9aa8))
		} else {
			a.centerText(cv, "Не удалось отрисовать страницу", Rect{pr.X, pr.Y + a.d(30), pr.W, a.d(24)}, a.uiFont(13), Hex(0xc0392b))
		}
		// выделение
		if s, e, ok := sel(i, len(gl)); ok {
			for _, r := range glyphLines(gl[s:e]) {
				cv.FillRect(Rect{pr.X + r.X*k, pr.Y + r.Y*k, r.W * k, r.H * k}, th.Accent.WithA(0.32))
			}
		}
		// найденное
		if f := v.found; f != nil && f.page == i {
			for _, r := range f.rects {
				rr := Rect{pr.X + r.X*k - 1, pr.Y + r.Y*k - 1, r.W*k + 2, r.H*k + 2}
				cv.FillRect(rr, RGBA(255, 196, 0, 110))
				cv.StrokeRound(rr, 0, math.Max(1, a.S), RGBA(230, 140, 0, 255))
			}
		}
		for _, l := range links {
			l := l
			rr := intersect(Rect{pr.X + l.X*k, pr.Y + l.Y*k, l.W * k, l.H * k}, a.docRect)
			tip := l.URI
			if tip == "" {
				tip = a.trf("Перейти на страницу %d", l.Page+1)
			}
			a.hits = append(a.hits, hit{id: fmt.Sprintf("pdflink:%d:%.0f:%.0f", i, l.X, l.Y), r: rr, cursor: 2, tip: tip, act: func() { a.pdfFollow(l) }})
		}
	}
}

// blitBmp — картинка листа в прямоугольник (1:1, если масштаб совпал; иначе ближайший пиксель).
func blitBmp(cv *Canvas, b *pdfBmp, r Rect) {
	cv.sync()
	x0, y0 := int(r.X), int(r.Y)
	w, h := int(r.W), int(r.H)
	if w <= 0 || h <= 0 {
		return
	}
	cx0, cy0 := max(0, int(cv.clip.X)), max(0, int(cv.clip.Y))
	cx1, cy1 := min(cv.W, int(cv.clip.X+cv.clip.W)), min(cv.H, int(cv.clip.Y+cv.clip.H))
	same := absI(b.w-w) <= 1 && absI(b.h-h) <= 1
	xs := make([]int, 0, w)
	for x := 0; x < w; x++ {
		if same {
			xs = append(xs, min(x, b.w-1))
		} else {
			xs = append(xs, min(b.w-1, x*b.w/w))
		}
	}
	for y := max(y0, cy0); y < min(y0+h, cy1); y++ {
		sy := y - y0
		if !same {
			sy = sy * b.h / h
		}
		sy = min(sy, b.h-1)
		src := b.pix[sy*b.w:]
		dst := cv.Pix[y*cv.W:]
		for x := max(x0, cx0); x < min(x0+w, cx1); x++ {
			dst[x] = src[xs[x-x0]]
		}
	}
}

func absI(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// glyphLines — прямоугольники выделения, слитые по строкам.
func glyphLines(gl []PdfGlyph) []Rect {
	var out []Rect
	for _, g := range gl {
		if g.W <= 0 && g.H <= 0 {
			continue
		}
		if n := len(out); n > 0 {
			r := &out[n-1]
			if math.Abs(g.Y-r.Y) < r.H*0.5 && g.X >= r.X-1 && g.X-(r.X+r.W) < r.H*3 {
				x1 := math.Max(r.X+r.W, g.X+g.W)
				y1 := math.Max(r.Y+r.H, g.Y+g.H)
				r.Y = math.Min(r.Y, g.Y)
				r.W, r.H = x1-r.X, y1-r.Y
				continue
			}
		}
		out = append(out, Rect{g.X, g.Y, g.W, g.H})
	}
	return out
}

func (a *App) pdfFollow(l PdfLink) {
	if l.URI != "" {
		a.P.OpenURL(l.URI)
		return
	}
	a.pdfGoto(l.Page, l.DestY)
}

// pdfGoto — к листу i (и к точке y в пунктах от верха, если известна).
func (a *App) pdfGoto(i int, y float64) {
	if i < 0 || i >= len(a.pdf.Doc.Pages) {
		return
	}
	a.scrollY = a.pageY(i) - a.d(22)
	if y > 0 {
		a.scrollY = a.pageY(i) + y*a.k() - a.d(30)
	}
	a.clampScroll()
	a.invalidate()
}

func (a *App) pdfCurPage() int {
	mid := a.scrollY + a.docRect.H/3
	n := len(a.pdf.Doc.Pages)
	for i := 0; i < n; i++ {
		if a.pageY(i+1) > mid {
			return i
		}
	}
	return n - 1
}

// ---------------------------------------------------------------- навигация (миниатюры и оглавление)
func (a *App) pdfThumbGeom() (tw float64, ys []float64) {
	v := a.pdf
	tw = a.navRect.W - a.d(8) - a.d(56)
	maxW := 0.0
	for _, p := range v.Doc.Pages {
		maxW = math.Max(maxW, p.W())
	}
	k := tw / maxW
	ys = make([]float64, len(v.Doc.Pages)+1)
	for i, p := range v.Doc.Pages {
		ys[i+1] = ys[i] + p.H()*k + a.d(30)
	}
	return
}

func (a *App) pdfThumbWants() []pdfReq {
	if a.navRect.W <= 0 || a.Set.NavMode == "headings" {
		return nil
	}
	v := a.pdf
	tw, ys := a.pdfThumbGeom()
	maxW := 0.0
	for _, p := range v.Doc.Pages {
		maxW = math.Max(maxW, p.W())
	}
	k := tw / maxW
	var out []pdfReq
	bodyH := a.navRect.H - a.d(56)
	for i := range v.Doc.Pages {
		y := ys[i] - a.navScroll
		if y+ys[i+1]-ys[i] < 0 || y > bodyH {
			continue
		}
		out = append(out, pdfReq{page: i, scale: k * 1.25, thumb: true})
	}
	return out
}

func (a *App) paintPDFNavPages(cv *Canvas, body Rect) {
	v := a.pdf
	th := a.th
	tw, ys := a.pdfThumbGeom()
	maxW := 0.0
	for _, p := range v.Doc.Pages {
		maxW = math.Max(maxW, p.W())
	}
	k := tw / maxW
	n := len(v.Doc.Pages)
	cur := a.pdfCurPage()
	if cur != v.navCur { // миниатюра текущей страницы всегда видна
		v.navCur = cur
		if ys[cur] < a.navScroll || ys[cur+1] > a.navScroll+body.H-a.d(10) {
			a.navScroll = ys[cur] - a.d(10)
		}
	}
	a.navMax(ys[n]+a.d(10), body.H)
	for i, p := range v.Doc.Pages {
		pw, ph := p.W()*k, p.H()*k
		step := ph + a.d(30)
		y := body.Y + a.d(8) + ys[i] - a.navScroll
		if y > body.Y+body.H || y+step < body.Y {
			continue
		}
		pr := Rect{math.Round(body.X + a.d(28) + (tw-pw)/2), math.Round(y), math.Round(pw), math.Round(ph)}
		i := i
		id := fmt.Sprintf("navp:%d", i)
		a.hits = append(a.hits, hit{id: id, r: Rect{body.X, y - a.d(4), body.W, step}, act: func() { a.pdfGoto(i, -1) }})
		hl := Rect{body.X + a.d(4), y - a.d(5), body.W - a.d(8), ph + a.d(10)}
		if i != cur {
			a.hoverBg(cv, id, hl, a.d(5))
		}
		cv.Shadow(pr, a.d(2), a.d(5), th.PageShadow)
		cv.FillRect(pr, Hex(0xffffff))
		v.mu.Lock()
		b := v.thumbs[i]
		v.mu.Unlock()
		if b != nil && b.w > 0 {
			cv.SetClip(intersect(pr, body))
			blitBmpSmooth(cv, b, pr)
			cv.SetClip(body)
		}
		if i == cur {
			cv.StrokeRound(pr.Inset(-a.d(1.5)), a.d(2), a.d(2), th.Accent)
		}
		a.centerText(cv, fmt.Sprint(i+1), Rect{body.X + a.d(4), y, a.d(22), a.d(18)}, a.uiFont(11), th.TextDim)
	}
}

// blitBmpSmooth — уменьшение с усреднением 2×2 (для миниатюр).
func blitBmpSmooth(cv *Canvas, b *pdfBmp, r Rect) {
	cv.sync()
	x0, y0, w, h := int(r.X), int(r.Y), int(r.W), int(r.H)
	if w <= 0 || h <= 0 || b.w == 0 {
		return
	}
	cx0, cy0 := max(0, int(cv.clip.X)), max(0, int(cv.clip.Y))
	cx1, cy1 := min(cv.W, int(cv.clip.X+cv.clip.W)), min(cv.H, int(cv.clip.Y+cv.clip.H))
	for y := max(y0, cy0); y < min(y0+h, cy1); y++ {
		sy0 := (y - y0) * b.h / h
		sy1 := min(b.h-1, max(sy0, ((y-y0+1)*b.h/h)-1))
		for x := max(x0, cx0); x < min(x0+w, cx1); x++ {
			sx0 := (x - x0) * b.w / w
			sx1 := min(b.w-1, max(sx0, ((x-x0+1)*b.w/w)-1))
			var rr, gg, bb uint32
			for _, c := range [4]uint32{b.pix[sy0*b.w+sx0], b.pix[sy0*b.w+sx1], b.pix[sy1*b.w+sx0], b.pix[sy1*b.w+sx1]} {
				rr += c >> 16 & 0xff
				gg += c >> 8 & 0xff
				bb += c & 0xff
			}
			cv.Pix[y*cv.W+x] = 0xff000000 | (rr/4)<<16 | (gg/4)<<8 | bb/4
		}
	}
}

func (a *App) paintPDFNavOutline(cv *Canvas, body Rect) {
	v := a.pdf
	th := a.th
	f := a.uiFont(12)
	if len(v.Doc.Outline) == 0 {
		a.textAt(cv, "В этом PDF нет оглавления", body.X+a.d(12), body.Y+a.d(10), f, th.TextDim)
		a.textAt(cv, "(вкладка «Страницы» — миниатюры)", body.X+a.d(12), body.Y+a.d(30), a.uiFont(11), th.TextDim)
		return
	}
	rowH := a.d(28)
	a.navMax(float64(len(v.Doc.Outline))*rowH+a.d(10), body.H)
	cur := a.pdfCurPage()
	act := -1
	for i, o := range v.Doc.Outline {
		if o.Page >= 0 && o.Page <= cur {
			act = i
		}
	}
	for i, o := range v.Doc.Outline {
		y := body.Y + a.d(4) + float64(i)*rowH - a.navScroll
		if y > body.Y+body.H || y+rowH < body.Y {
			continue
		}
		o := o
		id := fmt.Sprintf("navo:%d", i)
		r := Rect{body.X + a.d(4), y, body.W - a.d(8), rowH - a.d(2)}
		a.hits = append(a.hits, hit{id: id, r: r, tip: o.Title, act: func() { a.pdfGoto(o.Page, o.Y) }})
		if i == act {
			cv.FillRound(r, a.d(6), th.Accent.WithA(0.18))
		} else {
			a.hoverBg(cv, id, r, a.d(6))
		}
		x := r.X + a.d(8) + float64(min(o.Level, 6))*a.d(14)
		ff := f
		if o.Level == 0 {
			ff.Bold = true
		}
		title := o.Title
		maxW := r.X + r.W - x - a.d(40)
		for a.measure(title, ff) > maxW && len([]rune(title)) > 3 {
			rs := []rune(title)
			title = string(rs[:len(rs)-2]) + "…"
		}
		a.tb.Draw(cv, title, x, y+a.d(18), ff, th.Text)
		if o.Page >= 0 {
			pn := fmt.Sprint(o.Page + 1)
			a.tb.Draw(cv, pn, r.X+r.W-a.d(8)-a.measure(pn, a.uiFont(11)), y+a.d(18), a.uiFont(11), th.TextDim)
		}
	}
}

// ---------------------------------------------------------------- лента и строка состояния

func (a *App) pdfStatusText() string {
	return a.trf("Страница %d из %d", a.pdfCurPage()+1, len(a.pdf.Doc.Pages)) + "   ·   PDF, " + a.tr("только чтение")
}

// ---------------------------------------------------------------- поиск
func (a *App) pdfFindDialog() {
	v := a.pdf
	q := v.query
	if t := a.pdfSelText(); t != "" && len([]rune(t)) < 80 && !strings.Contains(t, "\n") {
		q = t
	}
	d := &Dialog{Title: "Найти в PDF", W: 460}
	d.Fields = []*Field{textField("Найти", q)}
	cs := &Field{Label: "С учётом регистра", Kind: "check", On: v.qCase}
	d.Fields = append(d.Fields, cs)
	run := func(dir int) func() bool {
		return func() bool {
			s := strings.TrimSpace(d.Val(0))
			if s == "" {
				return false
			}
			a.pdfFind(s, cs.On, dir)
			d.Info = "Ищу…"
			return false
		}
	}
	d.Buttons = []DlgBtn{{Label: "Закрыть"}, {Label: "Назад", Act: run(-1)}, {Label: "Найти далее", Primary: true, Act: run(1)}}
	v.dlg = d
	a.openDialog(d)
}

func normSearch(s string, cs bool) []rune {
	var out []rune
	sp := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !sp && len(out) > 0 {
				out = append(out, ' ')
			}
			sp = true
			continue
		}
		sp = false
		switch r {
		case 'ё':
			r = 'е'
		case 'Ё':
			r = 'Е'
		case '­':
			continue
		}
		if !cs {
			r = unicode.ToLower(r)
		}
		out = append(out, r)
	}
	if n := len(out); n > 0 && out[n-1] == ' ' {
		out = out[:n-1]
	}
	return out
}

// pageRunes — текст листа в виде рун (переводы строк → пробелы) и индексы глифов.
func pageRunes(gl []PdfGlyph, cs bool) ([]rune, []int) {
	s, idx := PageText(gl)
	rs := []rune(s)
	out := make([]rune, len(rs))
	for i, r := range rs {
		switch {
		case r == '\n' || unicode.IsSpace(r):
			r = ' '
		case r == 'ё':
			r = 'е'
		case r == 'Ё':
			r = 'Е'
		}
		if !cs {
			r = unicode.ToLower(r)
		}
		out[i] = r
	}
	return out, idx
}

func runeIndex(h, n []rune, from, dir int) int {
	if len(n) == 0 || len(h) < len(n) {
		return -1
	}
	match := func(i int) bool {
		for j := range n {
			if h[i+j] != n[j] {
				return false
			}
		}
		return true
	}
	if dir > 0 {
		for i := max(0, from); i+len(n) <= len(h); i++ {
			if match(i) {
				return i
			}
		}
	} else {
		for i := min(from, len(h)-len(n)); i >= 0; i-- {
			if match(i) {
				return i
			}
		}
	}
	return -1
}

func (a *App) pdfFind(q string, cs bool, dir int) {
	v := a.pdf
	v.mu.Lock()
	v.gen++
	gen := v.gen
	v.searching = true
	v.mu.Unlock()
	v.query, v.qCase = q, cs
	needle := normSearch(q, cs)
	n := len(v.Doc.Pages)
	startPage, startRune := a.pdfCurPage(), 0
	if dir < 0 {
		startRune = math.MaxInt32
	}
	if f := v.found; f != nil {
		startPage = f.page
		if dir > 0 {
			startRune = f.start + 1
		} else {
			startRune = f.start - 1
		}
	}
	go func() {
		for step := 0; step <= n; step++ {
			p := ((startPage+dir*step)%n + n) % n
			v.mu.Lock()
			stop := v.gen != gen
			v.mu.Unlock()
			if stop {
				return
			}
			gl := v.pageGlyphs(p)
			hay, idx := pageRunes(gl, cs)
			from := 0
			if dir < 0 {
				from = len(hay)
			}
			if step == 0 {
				from = startRune
			}
			if step == n && p == startPage { // круг замкнулся — ищем только до старта
				if dir > 0 && from > 0 {
					hay = hay[:min(len(hay), startRune+len(needle)-1)]
				}
				from = map[bool]int{true: 0, false: len(hay)}[dir > 0]
			}
			if i := runeIndex(hay, needle, from, dir); i >= 0 {
				var sub []PdfGlyph
				seen := map[int]bool{}
				for j := i; j < i+len(needle) && j < len(idx); j++ {
					if g := idx[j]; g >= 0 && !seen[g] {
						seen[g] = true
						sub = append(sub, gl[g])
					}
				}
				f := &pdfFound{page: p, start: i, end: i + len(needle), rects: glyphLines(sub)}
				v.mu.Lock()
				if v.gen == gen {
					v.pending, v.pendMsg, v.searching = f, "", false
				}
				v.mu.Unlock()
				v.inv()
				return
			}
			if step%8 == 7 {
				v.mu.Lock()
				if v.gen == gen {
					v.pendMsg = fmt.Sprintf("Ищу… стр. %d", p+1)
				}
				v.mu.Unlock()
				v.inv()
			}
		}
		v.mu.Lock()
		if v.gen == gen {
			v.pending, v.pendMsg, v.searching = nil, "Ничего не найдено", false
		}
		v.mu.Unlock()
		v.inv()
	}()
}

// pdfPoll — забираем результаты фонового поиска (вызывается при отрисовке).
func (a *App) pdfPoll() {
	v := a.pdf
	v.mu.Lock()
	f, msg := v.pending, v.pendMsg
	v.pending, v.pendMsg = nil, ""
	v.mu.Unlock()
	if f != nil {
		v.found = f
		k := a.k()
		if len(f.rects) > 0 {
			r := f.rects[0]
			y := a.pageY(f.page) + r.Y*k
			if y < a.scrollY+a.d(40) || y+r.H*k > a.scrollY+a.docRect.H-a.d(40) {
				a.scrollY = y - a.docRect.H/3
			}
			x := a.pageLeftOf(f.page) + r.X*k
			if x < a.docRect.X || x > a.docRect.X+a.docRect.W-a.d(30) {
				a.scrollX += x - a.docRect.X - a.docRect.W/3
			}
			a.clampScroll()
		}
		if v.dlg != nil && a.dialog == v.dlg {
			v.dlg.Info = a.trf("Найдено на странице %d", f.page+1)
		}
		a.msg(a.trf("Найдено на странице %d", f.page+1))
	} else if msg != "" {
		if v.dlg != nil && a.dialog == v.dlg {
			v.dlg.Info = msg
		}
		if msg == "Ничего не найдено" {
			v.found = nil
			a.msg(a.tr(msg))
		}
	}
}

// ---------------------------------------------------------------- выделение текста
// pdfHitGlyph — ближайший глиф к точке листа (в пунктах); exact — точно под курсором.
func pdfHitGlyph(gl []PdfGlyph, x, y float64) (best int, exact bool) {
	best = -1
	bd := math.Inf(1)
	for i, g := range gl {
		if x >= g.X && x <= g.X+g.W && y >= g.Y && y <= g.Y+g.H {
			return i, true
		}
		dy := 0.0
		if y < g.Y {
			dy = g.Y - y
		} else if y > g.Y+g.H {
			dy = y - g.Y - g.H
		}
		dx := 0.0
		if x < g.X {
			dx = g.X - x
		} else if x > g.X+g.W {
			dx = x - g.X - g.W
		}
		if d := dy*4 + dx; d < bd {
			bd, best = d, i
		}
	}
	return best, false
}

func (a *App) pdfPosAt(x, y float64) (pdfPos, bool, bool) {
	pt := a.toPage(x, y)
	gl := a.pdf.cachedGlyphs(pt.Page)
	g, exact := pdfHitGlyph(gl, pt.X, pt.Y)
	if g < 0 {
		return pdfPos{pt.Page, 0}, false, false
	}
	// правее середины буквы — позиция после неё
	if pt.X > gl[g].X+gl[g].W/2 && math.Abs(pt.Y-(gl[g].Y+gl[g].H/2)) < gl[g].H {
		g++
	}
	return pdfPos{pt.Page, g}, true, exact
}

func (a *App) pdfMouseDown(x, y float64, m Mods) {
	v := a.pdf
	p, ok, _ := a.pdfPosAt(x, y)
	if !ok {
		v.selOn = false
		a.dragSel = true
		v.selA, v.selB = p, p
		return
	}
	if a.clicks >= 2 {
		gl := v.cachedGlyphs(p.page)
		g := min(p.g, len(gl)-1)
		if a.clicks == 2 {
			s, e := pdfWord(gl, g)
			v.selA, v.selB = pdfPos{p.page, s}, pdfPos{p.page, e}
		} else {
			s, e := pdfLine(gl, g)
			v.selA, v.selB = pdfPos{p.page, s}, pdfPos{p.page, e}
		}
		v.selOn = true
		return
	}
	if m.Shift && v.selOn {
		v.selB = p
	} else {
		v.selA, v.selB = p, p
	}
	v.selOn = true
	a.dragSel = true
}

func (a *App) pdfDragTo(x, y float64) {
	v := a.pdf
	if p, ok, _ := a.pdfPosAt(x, y); ok {
		v.selB = p
		v.selOn = true
	}
	if y > a.docRect.Y+a.docRect.H-a.d(10) {
		a.scrollY += a.d(18)
	} else if y < a.docRect.Y+a.d(10) {
		a.scrollY -= a.d(18)
	}
	a.clampScroll()
}

func sameLine(a, b PdfGlyph) bool { return math.Abs(a.Y-b.Y) < math.Max(a.H, b.H)*0.5 }

func pdfWord(gl []PdfGlyph, g int) (int, int) {
	isSp := func(i int) bool { return strings.TrimSpace(gl[i].S) == "" }
	if g < 0 || g >= len(gl) {
		return 0, 0
	}
	if isSp(g) {
		return g, g + 1
	}
	s, e := g, g+1
	for s > 0 && !isSp(s-1) && sameLine(gl[s-1], gl[s]) && gl[s].X-(gl[s-1].X+gl[s-1].W) < gl[s].H*0.15 {
		s--
	}
	for e < len(gl) && !isSp(e) && sameLine(gl[e-1], gl[e]) && gl[e].X-(gl[e-1].X+gl[e-1].W) < gl[e].H*0.15 {
		e++
	}
	return s, e
}

func pdfLine(gl []PdfGlyph, g int) (int, int) {
	if g < 0 || g >= len(gl) {
		return 0, 0
	}
	s, e := g, g+1
	for s > 0 && sameLine(gl[s-1], gl[s]) {
		s--
	}
	for e < len(gl) && sameLine(gl[e-1], gl[e]) {
		e++
	}
	return s, e
}

// pdfSelRange — функция «диапазон глифов выделения на листе i».
func (a *App) pdfSelRange() func(i, n int) (int, int, bool) {
	v := a.pdf
	if !v.selOn {
		return func(int, int) (int, int, bool) { return 0, 0, false }
	}
	s, e := v.selA, v.selB
	if e.page < s.page || (e.page == s.page && e.g < s.g) {
		s, e = e, s
	}
	return func(i, n int) (int, int, bool) {
		if i < s.page || i > e.page {
			return 0, 0, false
		}
		from, to := 0, n
		if i == s.page {
			from = s.g
		}
		if i == e.page {
			to = e.g
		}
		from, to = max(0, min(from, n)), max(0, min(to, n))
		return from, to, to > from
	}
}

func (a *App) pdfSelText() string {
	v := a.pdf
	if !v.selOn {
		return ""
	}
	rng := a.pdfSelRange()
	lo, hi := min(v.selA.page, v.selB.page), max(v.selA.page, v.selB.page)
	var sb strings.Builder
	for i := lo; i <= hi; i++ {
		gl := v.pageGlyphs(i)
		if s, e, ok := rng(i, len(gl)); ok {
			t, _ := PageText(gl[s:e])
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(t)
		}
	}
	return sb.String()
}

func (a *App) pdfCopy() {
	t := a.pdfSelText()
	if t == "" {
		a.msg(a.tr("Сначала выделите текст мышью (или Ctrl+A — весь текст)"))
		return
	}
	a.P.SetClipboard(ClipData{Text: t})
	a.msg(a.trf("Скопировано символов: %d", len([]rune(t))))
}

func (a *App) pdfSelectAll() {
	v := a.pdf
	n := len(v.Doc.Pages)
	last := v.pageGlyphs(n - 1)
	v.selA, v.selB, v.selOn = pdfPos{0, 0}, pdfPos{n - 1, len(last)}, true
}

// ---------------------------------------------------------------- клавиатура
func (a *App) pdfKey(vk int, m Mods) bool {
	v := a.pdf
	step := a.d(60)
	page := a.docRect.H - a.d(40)
	switch {
	case m.Ctrl && vk == 'F':
		a.pdfFindDialog()
	case vk == 114 || (m.Ctrl && vk == 'G'): // F3
		if v.query != "" {
			a.pdfFind(v.query, v.qCase, map[bool]int{true: -1, false: 1}[m.Shift])
		} else {
			a.pdfFindDialog()
		}
	case m.Ctrl && (vk == 'C' || vk == 45):
		a.pdfCopy()
	case m.Ctrl && vk == 'A':
		a.pdfSelectAll()
	case m.Ctrl && vk == 'P':
		a.Command("print")
	case m.Ctrl && (vk == 187 || vk == 107):
		a.SetZoom(a.zoom * 1.15)
	case m.Ctrl && (vk == 189 || vk == 109):
		a.SetZoom(a.zoom / 1.15)
	case m.Ctrl && vk == '0':
		a.fitWidth()
	case vk == 27:
		v.selOn, v.found = false, nil
	case vk == 38:
		a.scrollY -= step
	case vk == 40:
		a.scrollY += step
	case vk == 37:
		if a.scrollX > 0 {
			a.scrollX -= step
		} else {
			a.pdfGoto(a.pdfCurPage()-1, -1)
		}
	case vk == 39:
		if a.maxPageW()+a.d(44) > a.docRect.W {
			a.scrollX += step
		} else {
			a.pdfGoto(a.pdfCurPage()+1, -1)
		}
	case vk == 33:
		if m.Ctrl {
			a.pdfGoto(a.pdfCurPage()-1, -1)
		} else {
			a.scrollY -= page
		}
	case vk == 34 || vk == 32:
		if m.Ctrl {
			a.pdfGoto(a.pdfCurPage()+1, -1)
		} else if m.Shift && vk == 32 {
			a.scrollY -= page
		} else {
			a.scrollY += page
		}
	case vk == 36:
		a.scrollY = 0
	case vk == 35:
		a.scrollY = a.contentH()
	default:
		return false
	}
	a.clampScroll()
	a.invalidate()
	return true
}

// pdfPrint — печать листов PDF.
func (a *App) pdfPrint() {
	v := a.pdf
	p0 := v.Doc.Pages[0]
	err := a.P.Print(len(v.Doc.Pages), func(i int, dpi float64) *image.RGBA {
		img, _, _ := v.render(i, v.capScale(i, dpi/72))
		return img
	}, p0.W(), p0.H())
	if err != nil {
		a.errorBox(err.Error())
	}
}
