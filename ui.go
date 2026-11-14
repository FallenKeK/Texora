package main

// Интерфейс Texora: заголовок, вкладки, лента, документ, строка состояния, «Файл».

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Font struct {
	Family                  string
	Px                      float64
	Bold, Italic, Underline bool
}

type TextBackend interface {
	Measure(s string, f Font) float64
	Metrics(f Font) (asc, desc float64)
	Draw(cv *Canvas, s string, x, baseline float64, f Font, c Color)
}

type Mods struct{ Ctrl, Shift, Alt bool }

type ClipData struct {
	Text, HTML, RTF string
	Image           []byte
	ImageMime       string
	Internal        []byte
}

type Platform interface {
	Invalidate()
	Minimize()
	ToggleMaximize()
	Close()
	SetTitle(string)
	OpenDialog(kind string) string
	SaveDialog(name string, ext string) (string, string)
	SetClipboard(ClipData)
	GetClipboard() ClipData
	Print(pages int, render func(page int, dpi float64) *image.RGBA, wPt, hPt float64) error
	SystemDark() bool
	OpenURL(string)
	Fonts() []string
	Quit()
}

type hit struct {
	id     string
	r      Rect
	cursor int
	act    func()
	tip    string
}

type imgDrag struct {
	pos    Pos
	w0, h0 float64
	x0, y0 float64
	w, h   float64
	page   int
	ix, iy float64
}

type App struct {
	tb        TextBackend
	M         *Measurer
	E         *Editor
	P         Platform
	W, H, S   float64
	th        Theme
	Set       Settings
	BaseS     float64
	UIFont    string
	MonoFont  string // цифры, сочетания клавиш, счётчики
	Maximized bool
	Diag      string
	SysFonts  []string

	hits        []hit
	hover       string
	press       string
	anim        map[string]float64
	tab         string
	backstage   bool
	bsPane      string
	popup       *Popup
	dialog      *Dialog
	ctxMenu     bool
	zoom        float64
	scrollY     float64
	scrollX     float64
	docRect     Rect
	inspRect    Rect // инспектор справа (с 1.5)
	inspBody    Rect
	inspScroll  float64
	inspStrip   bool // инспектор свёрнут в полосу значков
	inspPeek    bool // свёрнутый инспектор выдвинут поверх документа
	acts        []*action
	actIdx      map[string]*action
	palRecent   []string  // последние команды из палитры
	upd         updState  // автообновление
	norm        normState // нормоконтроль
	afterClose  func()    // выполнить при закрытии программы (после вопросов о сохранении)
	dragSel     bool
	dragScroll  bool
	dragOff     float64
	drag        *imgDrag
	imgSel      *Pos
	clicks      int
	lastClick   time.Time
	lastCX      float64
	lastCY      float64
	CaretOn     bool
	status      string
	statusT     time.Time
	stE         *Editor // кэш счётчика слов для строки состояния
	stGen       int64
	stTxt       Stats
	stAt        time.Time
	stStale     bool
	toastS      string // всплывающая подсказка над документом
	toastT      time.Time
	tip         string
	FileName    string
	FilePath    string
	scrollCaret bool
	lastFrame   time.Time
	paintMs     float64
	lastColor   string
	lastBg      string
	cfgDir      string
	PageCache   map[string]*scaledImg
	caseCycle   int
	pdf         *PdfView

	tabs       []*DocTab
	cur        int
	untitledN  int
	setScroll  string
	bsScroll   float64
	navScroll  float64
	navRect    Rect
	tplCache   map[string]*tplPreview
	saveTicks  int
	recSess    string             // сессия автосохранения: recover/<pid-время>
	recKeys    map[*Editor]string // копия каждой вкладки
	recSeq     int
	bsFromDoc  bool
	mx, my     float64
	pageYs     []float64
	pageYsL    *Layout
	pageYsK    float64
	cmtRects   []cmtRect
	spellCache map[*Para]spellEntry
	eqPal      int
	docGen     atomic.Int64 // меняется при любой правке/команде — сбрасывает кэш листов
	pageBmps   map[int]*pageBmp
	thumbBmps  map[int]*pageBmp
	frame      int
	scrollAnim bool
	scrollGoal float64
	scrollSeen float64
}

func (a *App) d(v float64) float64 { return v * a.S }
func (a *App) invalidate() {
	a.docGen.Add(1)
	if a.P != nil {
		a.P.Invalidate()
	}
}

func (a *App) addHit(id string, r Rect, cursor int, act func()) {
	a.hits = append(a.hits, hit{id: id, r: r, cursor: cursor, act: act})
}

func (a *App) hitAt(x, y float64) *hit {
	for i := len(a.hits) - 1; i >= 0; i-- {
		if a.hits[i].r.Contains(x, y) {
			return &a.hits[i]
		}
	}
	return nil
}

func (a *App) msg(s string) { a.status, a.statusT = s, time.Now() }

// toast — заметное сообщение над документом (и в строке состояния), гаснет само.
func (a *App) toast(s string) {
	a.msg(s)
	a.toastS, a.toastT = s, time.Now()
	a.invalidate()
}

func (a *App) paintToast(cv *Canvas) {
	if a.toastS == "" {
		return
	}
	el := time.Since(a.toastT).Seconds()
	if el > 3.2 {
		a.toastS = ""
		return
	}
	al := math.Min(1, math.Min(el*6, (3.2-el)*3))
	s := a.tr(a.toastS)
	if r := []rune(s); len(r) > 0 {
		s = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	f := a.uiFont(13.5)
	w := a.measure(s, f) + a.d(36)
	dr := a.docRect
	if dr.W <= 0 {
		dr = Rect{0, 0, a.W, a.H}
	}
	r := Rect{math.Round(dr.X + (dr.W-w)/2), math.Round(dr.Y + dr.H - a.d(70)), math.Round(w), a.d(38)}
	cv.Shadow(r, a.d(19), a.d(12), RGBA(0, 0, 0, uint8(90*al)))
	cv.FillRound(r, a.d(19), a.th.Popup.WithA(0.96*al))
	cv.StrokeRound(r, a.d(19), math.Max(1, a.S), a.th.Accent.WithA(0.8*al))
	a.centerText(cv, s, r, f, a.th.Text.WithA(al))
}

func (a *App) uiFont(px float64) Font { return Font{Family: a.UIFont, Px: a.d(px)} }

func (a *App) textAt(cv *Canvas, s string, x, y float64, f Font, c Color) {
	s = a.tr(s)
	asc, _ := a.tb.Metrics(f)
	a.tb.Draw(cv, s, x, y+asc, f, c)
}

func (a *App) centerText(cv *Canvas, s string, r Rect, f Font, c Color) {
	s = a.tr(s)
	w := a.tb.Measure(s, f)
	asc, desc := a.tb.Metrics(f)
	a.tb.Draw(cv, s, r.X+(r.W-w)/2, r.Y+(r.H-(asc+desc))/2+asc, f, c)
}

// ---------------------------------------------------------------- анимации
func (a *App) Tick() bool {
	now := time.Now()
	dt := now.Sub(a.lastFrame).Seconds()
	if a.lastFrame.IsZero() || dt > 0.1 {
		dt = 0.016
	}
	a.lastFrame = now
	busy := a.toastS != ""
	if a.stStale {
		busy = true
	}
	if a.scrollAnim {
		if a.scrollY != a.scrollSeen { // прокрутили чем-то другим — анимацию бросаем
			a.scrollAnim = false
		} else {
			a.scrollY += (a.scrollGoal - a.scrollY) * (1 - math.Exp(-dt*22))
			if math.Abs(a.scrollGoal-a.scrollY) < 0.5 {
				a.scrollY = a.scrollGoal
				a.scrollAnim = false
			} else {
				busy = true
			}
			a.clampScroll()
			a.scrollSeen = a.scrollY
		}
	}
	k := 1 - math.Exp(-dt*20)
	targets := map[string]bool{a.hover: true}
	if a.press != "" {
		targets["p:"+a.press] = true
	}
	for id := range targets {
		if _, ok := a.anim[id]; !ok && id != "" {
			a.anim[id] = 0
		}
	}
	for id, v := range a.anim {
		t := 0.0
		if targets[id] {
			t = 1
		}
		nv := v + (t-v)*k
		if math.Abs(nv-t) < 0.02 {
			nv = t
		} else {
			busy = true
		}
		if nv == 0 && t == 0 {
			delete(a.anim, id)
			continue
		}
		a.anim[id] = nv
	}
	return busy
}

// ---------------------------------------------------------------- геометрия документа
func (a *App) k() float64 { return 96.0 / 72 * a.zoom * a.S }

func (a *App) pageGeom() (pw, ph, gap float64) {
	pg := a.E.Doc.Page
	if L := a.E.L; L != nil && len(L.Pages) > 0 {
		pg = L.Pages[0].G.Page
	}
	k := a.k()
	return pg.W * k, pg.H * k, a.d(22)
}

// pageSize — размер листа i на экране.
func (a *App) pageSize(i int) (float64, float64) {
	k := a.k()
	if a.pdf != nil {
		p := a.pdf.Doc.Pages[max(0, min(i, len(a.pdf.Doc.Pages)-1))]
		return p.W() * k, p.H() * k
	}
	L := a.E.L
	if i < 0 || i >= len(L.Pages) {
		pg := a.E.Doc.Page
		return pg.W * k, pg.H * k
	}
	g := L.Pages[i].G.Page
	return g.W * k, g.H * k
}

// pageY — верх листа i без учёта прокрутки.
func (a *App) pageY(i int) float64 {
	gap := a.d(22)
	if a.pdf != nil {
		return a.pdf.pageY(a, i)
	}
	L := a.E.L
	if len(a.pageYs) != len(L.Pages)+1 || a.pageYsL != L || a.pageYsK != a.k() {
		a.pageYs = make([]float64, len(L.Pages)+1)
		y := gap
		for j := range L.Pages {
			a.pageYs[j] = y
			_, h := a.pageSize(j)
			y += h + gap
		}
		a.pageYs[len(L.Pages)] = y
		a.pageYsL, a.pageYsK = L, a.k()
	}
	return a.pageYs[max(0, min(i, len(a.pageYs)-1))]
}

func (a *App) maxPageW() float64 {
	w := 0.0
	for i := range a.numPages() {
		pw, _ := a.pageSize(i)
		w = math.Max(w, pw)
	}
	return w
}

// cmtW — ширина полосы примечаний справа от листов.
func (a *App) cmtW() float64 {
	if a.showMarks() && len(a.E.Doc.Comments) > 0 {
		return a.d(250)
	}
	return 0
}

func (a *App) showMarks() bool { return !a.Set.HideMarkup }

func (a *App) contentH() float64 {
	return a.pageY(a.numPages())
}

// numPages — число листов текущей вкладки (документа или PDF).
func (a *App) numPages() int {
	if a.pdf != nil {
		return len(a.pdf.Doc.Pages)
	}
	return len(a.E.L.Pages)
}

func (a *App) pageLeftOf(i int) float64 {
	gap := a.d(22)
	pw, _ := a.pageSize(i)
	mw := a.maxPageW()
	avail := a.docRect.W - a.d(14) - a.cmtW()
	return a.docRect.X + math.Max(gap, (avail-mw)/2) + (mw-pw)/2 - a.scrollX
}

func (a *App) pageLeft() float64 { return a.pageLeftOf(0) }

func (a *App) pageTop(i int) float64 {
	return a.docRect.Y + a.pageY(i) - a.scrollY
}

func (a *App) toPage(x, y float64) PagePoint {
	gap := a.d(22)
	n := a.numPages()
	yy := y - a.docRect.Y + a.scrollY
	i := 0
	for i < n-1 && yy > a.pageY(i+1)-gap/2 {
		i++
	}
	k := a.k()
	return PagePoint{Page: i, X: (x - a.pageLeftOf(i)) / k, Y: (y - a.pageTop(i)) / k}
}

func (a *App) clampScroll() {
	maxY := math.Max(0, a.contentH()-a.docRect.H)
	a.scrollY = math.Max(0, math.Min(a.scrollY, maxY))
	gap := a.d(22)
	maxX := math.Max(0, a.maxPageW()+2*gap+a.cmtW()-a.docRect.W+a.d(14))
	a.scrollX = math.Max(0, math.Min(a.scrollX, maxX))
}

func (a *App) ScrollToCaret() {
	c := a.E.Caret(a.E.Sel.F)
	if c == nil {
		return
	}
	k := a.k()
	y := a.pageTop(c.Page) + c.Y*k
	h := c.H * k
	if y < a.docRect.Y+a.d(10) {
		a.scrollY -= a.docRect.Y + a.d(40) - y
	} else if y+h > a.docRect.Y+a.docRect.H-a.d(10) {
		a.scrollY += y + h - (a.docRect.Y + a.docRect.H) + a.d(40)
	}
	a.clampScroll()
}

func (a *App) after() {
	a.CaretOn = true
	a.scrollCaret = true
	a.invalidate()
}

func (a *App) iconBtn(cv *Canvas, id, icon string, r Rect, on bool, tip string, act func()) {
	a.hits = append(a.hits, hit{id: id, r: r, act: act, tip: tip})
	th := a.th
	hv, pr := a.anim[id], a.anim["p:"+id]
	rad := a.d(5)
	if on {
		cv.FillRound(r, rad, th.Accent.WithA(0.16))
	}
	if hv > 0 {
		cv.FillRound(r, rad, th.Hover.WithA(th.Hover.A()*hv))
	}
	if pr > 0 {
		cv.FillRound(r, rad, th.Press.WithA(th.Press.A()*pr))
	}
	col := th.Text
	if on {
		col = th.Accent
	}
	a.icon(cv, icon, r, col)
}

func (a *App) hoverBg(cv *Canvas, id string, r Rect, rad float64) {
	if hv := a.anim[id]; hv > 0 {
		cv.FillRound(r, rad, a.th.Hover.WithA(a.th.Hover.A()*hv))
	}
	if pr := a.anim["p:"+id]; pr > 0 {
		cv.FillRound(r, rad, a.th.Press.WithA(a.th.Press.A()*pr))
	}
}

func (a *App) badge(cv *Canvas, text string, y, h float64, id string, act func()) {
	f := a.uiFont(12)
	w := a.measure(text, f) + a.d(24)
	r := Rect{a.W - w - a.d(14), y + a.d(5), w, h - a.d(10)}
	a.hits = append(a.hits, hit{id: "badge:" + id, r: r, act: act})
	cv.FillRound(r, a.d(12), a.th.Accent.WithA(0.16+0.1*a.anim["badge:"+id]))
	a.centerText(cv, text, r, f, a.th.Accent)
}

var DocFonts = []string{"Times New Roman", "Calibri", "Arial", "Cambria", "Georgia", "Verdana", "Tahoma", "Segoe UI",
	"Courier New", "Consolas", "Trebuchet MS", "Book Antiqua", "Palatino Linotype", "Century Gothic", "Garamond",
	"Comic Sans MS", "Impact", "Arial Narrow", "Arial Black", "Franklin Gothic Medium", "Lucida Console", "Candara",
	"Constantia", "Corbel", "Calibri Light", "Bahnschrift", "Sitka Text", "Gabriola", "Monotype Corsiva", "Bookman Old Style"}

func (a *App) fontPopup(r Rect) {
	cur := a.E.EffectiveF().Font
	seen := map[string]bool{}
	var items []PopItem
	add := func(f string) {
		if seen[strings.ToLower(f)] {
			return
		}
		seen[strings.ToLower(f)] = true
		f2 := f
		items = append(items, PopItem{Label: f, Font: f, On: strings.EqualFold(f, cur), Act: func() { a.E.SetFont(f2); a.after() }})
	}
	add(a.E.Doc.Def.Font)
	if cur != "" {
		add(cur)
	}
	items = append(items, PopItem{Sep: true})
	sys := map[string]bool{}
	for _, f := range a.SysFonts {
		sys[strings.ToLower(f)] = true
	}
	for _, f := range DocFonts {
		if len(a.SysFonts) == 0 || sys[strings.ToLower(f)] {
			add(f)
		}
	}
	if len(a.SysFonts) > 0 {
		items = append(items, PopItem{Sep: true})
		for _, f := range a.SysFonts {
			if !strings.HasPrefix(f, "@") {
				add(f)
			}
		}
	}
	a.popup = a.listPopup("font", r.X, r.Y+r.H+a.d(4), a.d(270), items, a.d(480), true)
}

func (a *App) sizePopup(r Rect) {
	cur := a.E.EffectiveF().Size
	var items []PopItem
	for _, s := range FontSizes {
		s := s
		items = append(items, PopItem{Label: strings.ReplaceAll(strconv.FormatFloat(s, 'f', -1, 64), ".", ","), On: s == cur, Act: func() { a.E.SetSize(s); a.after() }})
	}
	p := a.listPopup("size", r.X, r.Y+r.H+a.d(4), a.d(90), items, a.d(420), true)
	baseKey := p.Key
	p.Key = func(vk int, m Mods) bool {
		if vk == 13 && p.filter != "" {
			if v, err := strconv.ParseFloat(strings.ReplaceAll(p.filter, ",", "."), 64); err == nil && v >= 1 && v <= 1638 {
				a.E.SetSize(v)
				a.after()
				return false
			}
		}
		return baseKey(vk, m)
	}
	a.popup = p
}

var themeColors = [][]string{
	{"#ffffff", "#000000", "#e7e6e6", "#44546a", "#4472c4", "#ed7d31", "#a5a5a5", "#ffc000", "#5b9bd5", "#70ad47"},
	{"#f2f2f2", "#7f7f7f", "#d0cece", "#d6dce4", "#d9e2f3", "#fbe5d5", "#ededed", "#fff2cc", "#deebf6", "#e2efd9"},
	{"#d8d8d8", "#595959", "#aeabab", "#adb9ca", "#b4c6e7", "#f7cbac", "#dbdbdb", "#fee599", "#bdd7ee", "#c5e0b3"},
	{"#bfbfbf", "#3f3f3f", "#757070", "#8496b0", "#8eaadb", "#f4b183", "#c9c9c9", "#ffd965", "#9cc3e5", "#a8d08d"},
	{"#a5a5a5", "#262626", "#3a3838", "#323f4f", "#2f5496", "#c55a11", "#7b7b7b", "#bf9000", "#2e75b5", "#538135"},
	{"#7f7f7f", "#0c0c0c", "#171616", "#222a35", "#1f3864", "#833c0b", "#525252", "#7f6000", "#1e4e79", "#375623"},
}
var standardColors = []string{"#c00000", "#ff0000", "#ffc000", "#ffff00", "#92d050", "#00b050", "#00b0f0", "#0070c0", "#002060", "#7030a0"}
var highlightList = []string{"#ffff00", "#00ff00", "#00ffff", "#ff00ff", "#0000ff", "#ff0000", "#000080", "#008080", "#008000", "#800080", "#800000", "#808000", "#808080", "#c0c0c0", "#000000"}

func (a *App) colorPopup(owner string, r Rect, rows [][]string, extra []string, noneLabel string, pick func(string)) {
	cell := a.d(22)
	gap := a.d(4)
	cols := 10
	nRows := len(rows)
	h := a.d(44) + float64(nRows)*(cell+gap) + a.d(10)
	if len(extra) > 0 {
		h += a.d(30) + cell
	}
	w := float64(cols)*(cell+gap) + a.d(20)
	p := &Popup{R: Rect{r.X, r.Y + r.H + a.d(4), w, h}, Owner: owner, hover: -1}
	type sw struct {
		c string
		r Rect
	}
	var sws []sw
	layout := func() {
		sws = nil
		y := p.R.Y + a.d(44)
		for _, row := range rows {
			for i, c := range row {
				sws = append(sws, sw{c, Rect{p.R.X + a.d(10) + float64(i)*(cell+gap), y, cell, cell}})
			}
			y += cell + gap
		}
		if len(extra) > 0 {
			y += a.d(26)
			for i, c := range extra {
				sws = append(sws, sw{c, Rect{p.R.X + a.d(10) + float64(i%cols)*(cell+gap), y + float64(i/cols)*(cell+gap), cell, cell}})
			}
		}
	}
	layout()
	none := Rect{p.R.X + a.d(8), p.R.Y + a.d(8), w - a.d(16), a.d(28)}
	p.Draw = func(cv *Canvas, p *Popup) {
		th := a.th
		a.glass(cv, p.R, a.d(10))
		if p.hover == -2 {
			cv.FillRound(none, a.d(5), th.Hover)
		}
		a.textAt(cv, noneLabel, none.X+a.d(10), none.Y+a.d(6), a.uiFont(13), th.Text)
		for i, s := range sws {
			cv.FillRound(s.r, a.d(3), parseHexColor(s.c, 0))
			cv.StrokeRound(s.r, a.d(3), math.Max(1, a.S), th.Border)
			if i == p.hover {
				cv.StrokeRound(s.r.Inset(-a.d(2)), a.d(4), a.d(2), th.Accent)
			}
		}
		if len(extra) > 0 {
			a.textAt(cv, "Стандартные цвета", p.R.X+a.d(10), sws[len(sws)-len(extra)].r.Y-a.d(22), a.uiFont(12), th.TextDim)
		}
	}
	p.Move = func(x, y float64) {
		p.hover = -1
		if none.Contains(x, y) {
			p.hover = -2
		}
		for i, s := range sws {
			if s.r.Inset(-a.d(2)).Contains(x, y) {
				p.hover = i
			}
		}
	}
	p.Click = func(x, y float64) bool {
		p.Move(x, y)
		if p.hover == -2 {
			pick("")
			return true
		}
		if p.hover >= 0 {
			pick(sws[p.hover].c)
			return true
		}
		return false
	}
	p.Key = func(vk int, m Mods) bool { return true }
	a.popup = p
}

func (a *App) tablePicker(r Rect) {
	cell, gap := a.d(18), a.d(3)
	cols, rows := 10, 8
	w := float64(cols)*(cell+gap) + a.d(20)
	h := float64(rows)*(cell+gap) + a.d(80)
	p := &Popup{R: Rect{r.X, r.Y + r.H + a.d(4), w, h}, Owner: "tablepick"}
	hc, hr := 0, 0
	more := Rect{}
	p.Draw = func(cv *Canvas, p *Popup) {
		th := a.th
		a.glass(cv, p.R, a.d(10))
		label := "Вставка таблицы"
		if hc > 0 {
			label = a.trf("Таблица %d × %d", hc, hr)
		}
		a.textAt(cv, label, p.R.X+a.d(10), p.R.Y+a.d(10), a.uiFont(13), th.Text)
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				cr := Rect{p.R.X + a.d(10) + float64(x)*(cell+gap), p.R.Y + a.d(36) + float64(y)*(cell+gap), cell, cell}
				if x < hc && y < hr {
					cv.FillRound(cr, a.d(2), th.Accent.WithA(0.35))
					cv.StrokeRound(cr, a.d(2), math.Max(1, a.S), th.Accent)
				} else {
					cv.FillRound(cr, a.d(2), th.Field)
					cv.StrokeRound(cr, a.d(2), math.Max(1, a.S), th.Border)
				}
			}
		}
		more = Rect{p.R.X + a.d(6), p.R.Y + p.R.H - a.d(36), p.R.W - a.d(12), a.d(30)}
		if hc == -1 {
			cv.FillRound(more, a.d(5), th.Hover)
		}
		a.icon(cv, "table", Rect{more.X + a.d(6), more.Y + a.d(5), a.d(20), a.d(20)}, th.Text)
		a.textAt(cv, "Вставить таблицу…", more.X+a.d(34), more.Y+a.d(7), a.uiFont(13), th.Text)
	}
	p.Move = func(x, y float64) {
		hc, hr = 0, 0
		if more.Contains(x, y) {
			hc = -1
			return
		}
		cx := int((x - p.R.X - a.d(10)) / (cell + gap))
		cy := int((y - p.R.Y - a.d(36)) / (cell + gap))
		if x > p.R.X+a.d(10) && y > p.R.Y+a.d(36) && cx < cols && cy < rows {
			hc, hr = cx+1, cy+1
		}
	}
	p.Click = func(x, y float64) bool {
		p.Move(x, y)
		if hc == -1 {
			a.tableDialog()
			return true
		}
		if hc > 0 {
			a.E.InsertTable(hr, hc)
			a.after()
			return true
		}
		return false
	}
	p.Key = func(vk int, m Mods) bool { return true }
	a.popup = p
}

var symbols = []string{"©", "®", "™", "§", "¶", "°", "±", "×", "÷", "≈", "≠", "≤", "≥", "∞", "√", "∑", "π", "α", "β", "γ",
	"δ", "λ", "μ", "Ω", "€", "£", "¥", "₽", "$", "¢", "«", "»", "„", "“", "”", "‘", "’", "—", "–", "…",
	"•", "·", "→", "←", "↑", "↓", "↔", "⇒", "✓", "✗", "★", "☆", "♥", "♦", "♣", "♠", "№", "½", "¼", "¾"}

func (a *App) symbolPopup(r Rect) {
	cell := a.d(30)
	cols := 10
	rows := (len(symbols) + cols - 1) / cols
	p := &Popup{R: Rect{r.X, r.Y + r.H + a.d(4), float64(cols)*cell + a.d(16), float64(rows)*cell + a.d(16)}, Owner: "symbol", hover: -1}
	cellR := func(i int) Rect {
		return Rect{p.R.X + a.d(8) + float64(i%cols)*cell, p.R.Y + a.d(8) + float64(i/cols)*cell, cell, cell}
	}
	p.Draw = func(cv *Canvas, p *Popup) {
		th := a.th
		a.glass(cv, p.R, a.d(10))
		for i, s := range symbols {
			cr := cellR(i)
			if i == p.hover {
				cv.FillRound(cr, a.d(4), th.Hover)
			}
			a.centerText(cv, s, cr, Font{Family: "Segoe UI Symbol", Px: a.d(16)}, th.Text)
		}
	}
	p.Move = func(x, y float64) {
		p.hover = -1
		for i := range symbols {
			if cellR(i).Contains(x, y) {
				p.hover = i
			}
		}
	}
	p.Click = func(x, y float64) bool {
		p.Move(x, y)
		if p.hover >= 0 {
			a.E.InsertText(symbols[p.hover])
			a.after()
			return true
		}
		return false
	}
	p.Key = func(vk int, m Mods) bool { return true }
	a.popup = p
}

var styleGallery = []struct {
	label string
	h     int
	named string
}{{"Обычный", 0, ""}, {"Без интервала", 0, "NoSpacing"}, {"Заголовок 1", 1, ""}, {"Заголовок 2", 2, ""}, {"Заголовок 3", 3, ""},
	{"Название", 0, "Title"}, {"Подзаголовок", 0, "Subtitle"}, {"Цитата", 0, "Quote"}}

func (a *App) applyGalleryStyle(i int) {
	s := styleGallery[i]
	if s.h > 0 {
		a.E.SetHeading(s.h)
	} else {
		a.E.SetNamedStyle(s.named)
	}
	a.after()
}

// ---------------------------------------------------------------- документ на экране
type pageKey struct {
	pg     *PageL
	k      float64
	active string
	rs, re Pos
	sel    bool
	gen    int64
	marks  bool
	grid   bool
}

type pageBmp struct {
	key   pageKey
	cv    *Canvas
	frame int
}

// blitCanvas — скопировать картинку src в dst в точку (x, y) с учётом отсечения dst.
func blitCanvas(dst, src *Canvas, x, y int) {
	dst.sync()
	cx0, cy0 := max(0, int(dst.clip.X)), max(0, int(dst.clip.Y))
	cx1, cy1 := min(dst.W, int(math.Ceil(dst.clip.X+dst.clip.W))), min(dst.H, int(math.Ceil(dst.clip.Y+dst.clip.H)))
	x0, x1 := max(x, cx0), min(x+src.W, cx1)
	if x1 <= x0 {
		return
	}
	for yy := max(y, cy0); yy < min(y+src.H, cy1); yy++ {
		sr := (yy - y) * src.W
		copy(dst.Pix[yy*dst.W+x0:yy*dst.W+x1], src.Pix[sr+x0-x:sr+x1-x])
	}
}

func (a *App) paintDocument(cv *Canvas) {
	a.frame++
	e := a.E
	L := e.L
	th := a.th
	k := a.k()
	rs, re := e.Range()
	hasSel := !e.Collapsed()
	var caret *CaretRect
	if a.CaretOn && e.Collapsed() && a.dialog == nil && a.imgSel == nil {
		caret = e.Caret(e.Sel.F)
	}
	active := e.Story
	if strings.HasPrefix(active, "fn:") {
		active = "body"
	}
	pp := &PagePainter{cv: cv, tb: a.tb, doc: e.Doc, k: k, imgs: a.PageCache, marks: a.showMarks(), m: e.M, spell: a.spellMarks, grid: !a.Set.NoGrid}
	for i, pg := range L.Pages {
		top := a.pageTop(i)
		pw, ph := a.pageSize(i)
		left := a.pageLeftOf(i)
		g := pg.G
		if top > a.docRect.Y+a.docRect.H || top+ph < a.docRect.Y {
			continue
		}
		left, top = math.Round(left), math.Round(top)
		pr := Rect{left, top, math.Round(pw), math.Round(ph)}
		cv.Shadow(pr, a.d(2), a.d(10), th.PageShadow)
		// содержимое листа берём из кэша: прокрутка и наведение мыши не перерисовывают текст заново
		key := pageKey{pg: pg, k: k, active: active, rs: rs, re: re, sel: hasSel, gen: a.docGen.Load(), marks: pp.marks, grid: pp.grid}
		pb := a.pageBmps[i]
		if pb == nil || pb.key != key || pb.cv.W != int(pr.W) || pb.cv.H != int(pr.H) {
			if pb == nil || pb.cv.W != int(pr.W) || pb.cv.H != int(pr.H) {
				if pb != nil && pb.cv.Release != nil {
					pb.cv.Release()
				}
				pc := &Canvas{W: int(pr.W), H: int(pr.H)}
				pc.Pix = make([]uint32, pc.W*pc.H)
				if PrepareCanvas != nil {
					PrepareCanvas(pc)
				}
				pb = &pageBmp{cv: pc}
				if a.pageBmps == nil {
					a.pageBmps = map[int]*pageBmp{}
				}
				a.pageBmps[i] = pb
			}
			pc := pb.cv
			pc.ResetClip()
			pc.Clear(Hex(0xffffff))
			if hasSel {
				for pi := range e.Placed {
					pl := &e.Placed[pi]
					if pl.Page != i || pl.Story != e.Story {
						continue
					}
					if x1, x2, ok := e.SelSpan(pl, rs, re); ok {
						pc.FillRect(Rect{x1 * k, pl.Y * k, (x2 - x1) * k, pl.Line.H * k}, RGBA(102, 156, 230, 110))
					}
				}
			}
			pp.cv = pc
			pp.ox, pp.oy = 0, 0
			pp.DrawPage(L, pg, active, false)
			pp.cv = cv
			pc.sync()
			pb.key = key
		}
		pb.frame = a.frame
		blitCanvas(cv, pb.cv, int(left), int(top))
		if IsHF(active) {
			hy := math.Max(g.BodyTop-2, g.Page.Header+12)
			if pg.Header != nil {
				hy = math.Max(g.BodyTop-2, g.Page.Header+pg.Header.H+2)
			}
			fy := g.Page.H - g.Page.Footer - 14
			if pg.Footer != nil {
				fy = g.Page.H - g.Page.Footer - pg.Footer.H - 2
			}
			for x := 0.0; x < pw; x += a.d(8) {
				cv.FillRect(Rect{left + x, top + hy*k, a.d(4), math.Max(1, a.S)}, th.Accent)
				cv.FillRect(Rect{left + x, top + fy*k, a.d(4), math.Max(1, a.S)}, th.Accent)
			}
			lbl := a.hfLabel(pg.HStory, pg.Sec)
			ly := top + hy*k + a.d(4)
			if strings.Contains(active, "footer") {
				lbl = a.hfLabel(pg.FStory, pg.Sec)
				ly = top + fy*k - a.d(22)
			}
			lr := Rect{left, ly, a.measure(lbl, a.uiFont(11)) + a.d(14), a.d(18)}
			cv.FillRound(lr, a.d(4), th.Accent)
			a.centerText(cv, lbl, lr, a.uiFont(11), Hex(0xffffff))
		}
		if caret != nil && caret.Page == i {
			cv.FillRect(Rect{math.Round(left + caret.X*k), top + caret.Y*k, math.Max(1, math.Round(a.S*1.1)), caret.H * k}, Hex(0x000000))
		}
		if a.imgSel != nil {
			a.paintImgSel(cv, i, left, top, k)
		}
		if pg.Blank {
			a.centerText(cv, a.tr("Пустая страница"), Rect{left, top + ph/2 - a.d(10), pw, a.d(20)}, a.uiFont(12), Hex(0xb0b0b0))
		}
	}
	// листы и миниатюры, которые давно не видны, выбрасываем
	for i, pb := range a.thumbBmps {
		if a.frame-pb.frame > 2 && len(a.thumbBmps) > 24 || i >= len(L.Pages) {
			if pb.cv.Release != nil {
				pb.cv.Release()
			}
			delete(a.thumbBmps, i)
		}
	}
	for i, pb := range a.pageBmps {
		if a.frame-pb.frame > 2 && len(a.pageBmps) > 6 || i >= len(L.Pages) {
			if pb.cv.Release != nil {
				pb.cv.Release()
			}
			delete(a.pageBmps, i)
		}
	}
	a.paintComments(cv)
	if a.drag != nil {
		d := a.drag
		r := Rect{a.pageLeftOf(d.page) + d.ix*k, a.pageTop(d.page) + d.iy*k, d.w * k, d.h * k}
		cv.StrokeRound(r, 0, a.d(1), th.Accent)
	}
}

// hfLabel — подпись колонтитула при редактировании.
func (a *App) hfLabel(story string, sec int) string {
	kind := story
	if _, k, ok := strings.Cut(story, "/"); ok {
		kind = k
	}
	lbl := "Верхний колонтитул"
	if strings.HasPrefix(kind, "footer") {
		lbl = "Нижний колонтитул"
	}
	switch hfVariant(kind) {
	case "first":
		lbl += a.tr(" первой страницы")
	case "even":
		lbl += a.tr(" чётной страницы")
	default:
		if a.E.Doc.EvenOdd {
			lbl += a.tr(" нечётной страницы")
		}
	}
	lbl = a.tr(lbl)
	if n := len(a.E.Doc.Sections()); n > 1 {
		lbl += a.trf(" — раздел %d", sec+1)
	}
	return lbl
}

func (a *App) imgRect() (Rect, int, bool) {
	if a.imgSel == nil {
		return Rect{}, 0, false
	}
	pos := *a.imgSel
	for _, pl := range a.E.Placed {
		if pl.P != pos.P || pl.Story != a.E.Story {
			continue
		}
		for _, it := range pl.Line.Floats {
			if it.O == pos.O {
				return Rect{pl.X + it.X, pl.Y + it.FY, it.W, it.H}, pl.Page, true
			}
		}
		for _, it := range pl.Line.Items {
			if it.K == kImg && it.O == pos.O {
				return Rect{pl.X + it.X, pl.Y + pl.Line.Base - it.H, it.W, it.H}, pl.Page, true
			}
		}
	}
	return Rect{}, 0, false
}

func (a *App) paintImgSel(cv *Canvas, page int, left, top, k float64) {
	r, pg, ok := a.imgRect()
	if !ok || pg != page {
		return
	}
	sr := Rect{left + r.X*k, top + r.Y*k, r.W * k, r.H * k}
	cv.StrokeRound(sr, 0, math.Max(1, a.S), a.th.Accent)
	hs := a.d(8)
	for _, c := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0.5, 0}, {0.5, 1}, {0, 0.5}, {1, 0.5}} {
		hr := Rect{sr.X + c[0]*sr.W - hs/2, sr.Y + c[1]*sr.H - hs/2, hs, hs}
		cv.FillRound(hr, hs/2, Hex(0xffffff))
		cv.StrokeRound(hr, hs/2, math.Max(1, a.S), a.th.Accent)
	}
	a.addHit("imghandle", Rect{sr.X + sr.W - hs, sr.Y + sr.H - hs, hs * 2, hs * 2}, 3, nil)
}

func (a *App) paintScrollbar(cv *Canvas) {
	ch := a.contentH()
	if ch <= a.docRect.H {
		return
	}
	w := a.d(6)
	if a.anim["scroll"] > 0 || a.dragScroll {
		w = a.d(6) + a.d(4)*math.Max(a.anim["scroll"], map[bool]float64{true: 1, false: 0}[a.dragScroll])
	}
	track := Rect{a.docRect.X + a.docRect.W - a.d(14), a.docRect.Y + a.d(4), a.d(14), a.docRect.H - a.d(8)}
	a.addHit("scroll", track, 0, nil)
	th := math.Max(a.d(36), track.H*a.docRect.H/ch)
	y := track.Y + (track.H-th)*a.scrollY/math.Max(1, ch-a.docRect.H)
	cv.FillRound(Rect{track.X + track.W - w - a.d(3), y, w, th}, w/2, a.th.TextDim.WithA(0.45+0.2*a.anim["scroll"]))
}

// statsCached — счётчик слов пересчитывается только после правки: на документе в сотню страниц
// честный подсчёт занимал до 80% времени каждого кадра.
func (a *App) statsCached() Stats {
	gen := a.docGen.Load()
	if a.stE != a.E || (a.stGen != gen && (a.stAt.IsZero() || time.Since(a.stAt) > 400*time.Millisecond || len(a.E.L.Pages) < 20)) {
		a.stE, a.stGen, a.stTxt, a.stAt, a.stStale = a.E, gen, a.E.textStats(), time.Now(), false
	} else if a.stGen != gen {
		a.stStale = true // догоним в Tick
	}
	st := a.stTxt
	st.Pages = len(a.E.L.Pages)
	st.Page = 1
	if c := a.E.Caret(a.E.Sel.F); c != nil {
		st.Page = c.Page + 1
	}
	return st
}

func (a *App) paintStatus(cv *Canvas, r Rect) {
	th := a.th
	cv.FillRect(r, th.Bg)
	cv.FillRect(Rect{0, r.Y, a.W, math.Max(1, a.S)}, th.Divider)
	f := a.monoFont(11.5)
	st := a.statsCached()
	x := a.d(14)
	item := func(id, text string, act func()) {
		w := a.measure(text, f) + a.d(16)
		rr := Rect{x - a.d(8), r.Y + a.d(2), w, r.H - a.d(4)}
		a.hits = append(a.hits, hit{id: id, r: rr, act: act})
		a.hoverBg(cv, id, rr, a.d(4))
		a.textAt(cv, text, x, r.Y+(r.H-a.d(15))/2, f, th.TextDim)
		x += w + a.d(6)
	}
	if a.pdf != nil {
		item("st:page", a.pdfStatusText(), func() { a.gotoPageDialog() })
	} else {
		item("st:page", a.trf("стр. %d / %d", st.Page, st.Pages), func() { a.gotoPageDialog() })
		item("st:words", a.trf("%d слов", st.Words), func() { a.statsDialog() })
		item("st:lang", "RU", nil)
		if a.E.Doc.Track {
			item("st:track", a.tr("исправления"), a.actionsByID()["track"].act)
		}
	}
	msg := a.tip
	if a.status != "" && time.Since(a.statusT) < 6*time.Second {
		msg = a.status
	}
	zs := fmt.Sprintf("%d%%", int(a.zoom*100+0.5))
	zx := a.W - a.d(14) - a.measure(zs, f)
	a.textAt(cv, zs, zx, r.Y+(r.H-a.d(15))/2, f, th.TextDim)
	sw := a.d(110)
	sr := Rect{zx - a.d(36) - sw, r.Y + r.H/2 - a.d(2), sw, a.d(4)}
	cv.FillRound(sr, a.d(2), th.Divider)
	pos := (math.Log(a.zoom) - math.Log(0.3)) / (math.Log(4) - math.Log(0.3))
	knob := Rect{sr.X + pos*sw - a.d(5), sr.Y - a.d(5), a.d(10), a.d(14)}
	cv.FillRound(knob, a.d(5), th.Accent)
	a.hits = append(a.hits, hit{id: "zoomslider", r: Rect{sr.X - a.d(6), r.Y, sw + a.d(12), r.H}})
	a.iconBtn(cv, "st:zout", "zoomout", Rect{sr.X - a.d(30), r.Y + a.d(3), a.d(24), a.d(22)}, false, "", func() { a.SetZoom(a.zoom / 1.1) })
	a.iconBtn(cv, "st:zin", "zoomin", Rect{sr.X + sw + a.d(6), r.Y + a.d(3), a.d(24), a.d(22)}, false, "", func() { a.SetZoom(a.zoom * 1.1) })
	if msg != "" {
		mf := a.uiFont(12)
		mx := sr.X - a.d(44) - a.measure(msg, mf)
		if mx > x+a.d(10) {
			a.textAt(cv, msg, mx, r.Y+(r.H-a.d(16))/2, mf, th.TextDim)
		}
	}
}

// ---------------------------------------------------------------- меню и диалоги команд
func (a *App) anchorOf(id string) Rect {
	for _, h := range a.hits {
		if h.id == id {
			return h.r
		}
	}
	return Rect{a.W / 2, a.d(170), 0, 0}
}

func (a *App) casePopup() {
	r := a.anchorOf("b:case")
	items := []PopItem{
		{Label: "Как в предложениях.", Act: func() { a.E.ChangeCase("sentence"); a.after() }},
		{Label: "все строчные", Act: func() { a.E.ChangeCase("lower"); a.after() }},
		{Label: "ВСЕ ПРОПИСНЫЕ", Act: func() { a.E.ChangeCase("upper"); a.after() }},
		{Label: "Начинать С Прописных", Act: func() { a.E.ChangeCase("title"); a.after() }},
		{Label: "иЗМЕНИТЬ рЕГИСТР", Act: func() { a.E.ChangeCase("toggle"); a.after() }},
	}
	a.popup = a.listPopup("case", r.X, r.Y+r.H+a.d(4), a.d(240), items, a.d(400), false)
}

func (a *App) spacingPopup() {
	r := a.anchorOf("b:spacing")
	cur := a.E.CurrentPF().Line
	var items []PopItem
	for _, v := range []float64{1, 1.15, 1.5, 2, 2.5, 3} {
		v := v
		items = append(items, PopItem{Label: strings.ReplaceAll(strconv.FormatFloat(v, 'f', -1, 64), ".", ","), On: math.Abs(v-cur) < 0.01,
			Act: func() { a.E.SetLineSpacing(v); a.after() }})
	}
	items = append(items, PopItem{Sep: true}, PopItem{Label: "Другие варианты…", Act: func() { a.paragraphDialog() }},
		PopItem{Label: "Убрать интервал после абзаца", Act: func() { a.E.SetPF(func(p PF) PF { p.After = O(0); return p }); a.after() }},
		PopItem{Label: "Добавить интервал перед абзацем", Act: func() { a.E.SetPF(func(p PF) PF { p.Before = O(12); return p }); a.after() }})
	a.popup = a.listPopup("spacing", r.X, r.Y+r.H+a.d(4), a.d(270), items, a.d(400), false)
}

func (a *App) datePopup() {
	r := a.anchorOf("i:date")
	now := time.Now()
	months := []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	opts := []string{now.Format("02.01.2006"), a.trf("%d %s %d г.", now.Day(), months[now.Month()-1], now.Year()),
		now.Format("02.01.2006 15:04"), now.Format("15:04"), now.Format("2006-01-02")}
	var items []PopItem
	for _, o := range opts {
		o := o
		items = append(items, PopItem{Label: o, Act: func() { a.E.InsertText(o); a.after() }})
	}
	a.popup = a.listPopup("date", r.X, r.Y+r.H+a.d(4), a.d(240), items, a.d(400), false)
}

type pageSize struct {
	name string
	w, h float64 // мм
}

var pageSizes = []pageSize{{"A4", 210, 297}, {"A5", 148, 210}, {"A3", 297, 420}, {"B5", 176, 250}, {"Letter", 215.9, 279.4}, {"Legal", 215.9, 355.6}}

func (a *App) setPage(fn func(p *Page)) {
	a.E.Checkpoint("")
	a.E.SetSection(func(s *SecView) { fn(&s.Page) })
	a.E.Changed()
	a.after()
}

func (a *App) marginsPopup() {
	r := a.anchorOf("l:margins")
	presets := []struct {
		name        string
		t, b, l, rr float64
	}{{"Обычные (ГОСТ): 2 / 2 / 3 / 1,5 см", 2, 2, 3, 1.5}, {"Word: 2,5 см со всех сторон", 2.5, 2.5, 2.5, 2.5}, {"Узкие: 1,27 см", 1.27, 1.27, 1.27, 1.27},
		{"Средние: 2,54 / 1,91 см", 2.54, 2.54, 1.91, 1.91}, {"Широкие: 2,54 / 5,08 см", 2.54, 2.54, 5.08, 5.08}}
	var items []PopItem
	for _, p := range presets {
		p := p
		items = append(items, PopItem{Label: p.name, Icon: "margins", Act: func() {
			a.setPage(func(pg *Page) { pg.Top, pg.Bottom, pg.Left, pg.Right = p.t*CM, p.b*CM, p.l*CM, p.rr*CM })
		}})
	}
	items = append(items, PopItem{Sep: true}, PopItem{Label: "Настраиваемые поля…", Act: func() { a.pageDialog() }})
	a.popup = a.listPopup("margins", r.X, r.Y+r.H+a.d(4), a.d(320), items, a.d(400), false)
}

func (a *App) orientPopup() {
	r := a.anchorOf("l:orient")
	pg := a.E.CurSection().Page
	land := pg.W > pg.H
	set := func(l bool) {
		if l == land {
			return
		}
		a.setPage(func(p *Page) {
			p.W, p.H = p.H, p.W
			p.Top, p.Right, p.Bottom, p.Left = p.Left, p.Top, p.Right, p.Bottom
		})
	}
	a.popup = a.listPopup("orient", r.X, r.Y+r.H+a.d(4), a.d(200), []PopItem{
		{Label: "Книжная", Icon: "fitpage", On: !land, Act: func() { set(false) }},
		{Label: "Альбомная", Icon: "fitwidth", On: land, Act: func() { set(true) }}}, a.d(200), false)
}

func (a *App) sizePagePopup() {
	r := a.anchorOf("l:size")
	pg := a.E.CurSection().Page
	var items []PopItem
	for _, s := range pageSizes {
		s := s
		on := math.Abs(math.Min(pg.W, pg.H)-s.w*72/25.4) < 2 && math.Abs(math.Max(pg.W, pg.H)-s.h*72/25.4) < 2
		items = append(items, PopItem{Label: s.name, Right: a.trf("%g × %g мм", s.w, s.h), On: on, Act: func() {
			a.setPage(func(p *Page) {
				w, h := s.w*72/25.4, s.h*72/25.4
				if p.W > p.H {
					w, h = h, w
				}
				p.W, p.H = w, h
			})
		}})
	}
	items = append(items, PopItem{Sep: true}, PopItem{Label: "Другие размеры…", Act: func() { a.pageDialog() }})
	a.popup = a.listPopup("psize", r.X, r.Y+r.H+a.d(4), a.d(260), items, a.d(400), false)
}

func fmtNum(v float64) string {
	return strings.ReplaceAll(strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64), ".", ",")
}
func parseNum(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
	return v, err == nil
}

func (a *App) pageDialog() {
	pg := a.E.CurSection().Page
	c := func(v float64) string { return fmtNum(v / CM) }
	d := &Dialog{Title: "Параметры страницы", W: 440, Fields: []*Field{
		textField("Ширина, см", c(pg.W)), textField("Высота, см", c(pg.H)),
		textField("Верхнее поле, см", c(pg.Top)), textField("Нижнее поле, см", c(pg.Bottom)),
		textField("Левое поле, см", c(pg.Left)), textField("Правое поле, см", c(pg.Right)),
		textField("Колонтитул сверху, см", c(pg.Header)), textField("Колонтитул снизу, см", c(pg.Footer)),
	}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "OK", Primary: true, Act: func() bool {
		var v [8]float64
		for i := range v {
			x, ok := parseNum(d.Val(i))
			if !ok || x < 0 || x > 200 {
				d.Info = "Проверьте число в поле «" + d.Fields[i].Label + "»"
				return false
			}
			v[i] = x * CM
		}
		if v[0] < 72 || v[1] < 72 || v[4]+v[5] > v[0]-36 || v[2]+v[3] > v[1]-36 {
			d.Info = "Поля не помещаются на странице"
			return false
		}
		a.setPage(func(p *Page) {
			p.W, p.H, p.Top, p.Bottom, p.Left, p.Right, p.Header, p.Footer = v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]
		})
		return true
	}}}
	a.openDialog(d)
}

func (a *App) paragraphDialog() {
	pf := a.E.CurrentPF()
	d := &Dialog{Title: "Абзац", W: 440, Fields: []*Field{
		textField("Отступ слева, см", fmtNum(pf.IndL/CM)), textField("Отступ справа, см", fmtNum(pf.IndR/CM)),
		textField("Первая строка, см", fmtNum(pf.IndF/CM)),
		textField("Интервал перед, пт", fmtNum(pf.Before)), textField("Интервал после, пт", fmtNum(pf.After)),
		textField("Междустрочный (множитель)", fmtNum(pf.Line)),
		{Label: "Не отрывать от следующего", Kind: "check", On: pf.Keep},
		{Label: "С новой страницы", Kind: "check", On: pf.PB},
		{Label: "Запрет висячих строк", Kind: "check", On: !a.E.Sel.F.P.PF.NoWidow},
		{Label: "Отрицательная первая строка — выступ (как у списков)", Kind: "info"},
	}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "OK", Primary: true, Act: func() bool {
		var v [6]float64
		for i := range v {
			x, ok := parseNum(d.Val(i))
			if !ok {
				d.Info = "Проверьте число в поле «" + d.Fields[i].Label + "»"
				return false
			}
			v[i] = x
		}
		if v[5] <= 0 || v[5] > 10 {
			d.Info = "Множитель интервала — от 0,1 до 10"
			return false
		}
		keep, pb, widow := d.Fields[6].On, d.Fields[7].On, d.Fields[8].On
		a.E.SetPF(func(p PF) PF {
			p.IndL, p.IndR, p.IndF = O(v[0]*CM), v[1]*CM, O(v[2]*CM)
			if v[0] == 0 && p.List == nil {
				p.IndL = Opt{}
			}
			if v[2] == 0 {
				p.IndF = Opt{}
			}
			p.Before, p.After, p.Line, p.LineExact = O(v[3]), O(v[4]), O(v[5]), 0
			p.KeepNext, p.PB, p.NoWidow = keep && p.Heading == 0, pb, !widow
			return p
		})
		a.after()
		return true
	}}}
	a.openDialog(d)
}

func (a *App) tableDialog() {
	d := &Dialog{Title: "Вставка таблицы", W: 360, Fields: []*Field{textField("Число столбцов", "3"), textField("Число строк", "3")}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "Вставить", Primary: true, Act: func() bool {
		c, ok1 := parseNum(d.Val(0))
		r, ok2 := parseNum(d.Val(1))
		if !ok1 || !ok2 || c < 1 || r < 1 || c > 63 || r > 1000 {
			d.Info = "Столбцов: 1–63, строк: 1–1000"
			return false
		}
		a.E.InsertTable(int(r), int(c))
		a.after()
		return true
	}}}
	a.openDialog(d)
}

func (a *App) linkDialog() {
	cur := a.E.CurrentF().Link
	if cur == "" {
		cur = "https://"
	}
	d := &Dialog{Title: "Гиперссылка", W: 460, Fields: []*Field{textField("Адрес", cur), {Label: "Пустой адрес — убрать ссылку", Kind: "info"}}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "OK", Primary: true, Act: func() bool {
		u := strings.TrimSpace(d.Val(0))
		if u == "https://" {
			u = ""
		}
		a.E.SetLink(u)
		a.after()
		return true
	}}}
	a.openDialog(d)
}

func (a *App) findDialog(replace bool) {
	sel := ""
	if !a.E.Collapsed() {
		s, e := a.E.Range()
		if s.P == e.P {
			sel = string(s.P.Runes()[s.O:e.O])
		}
	}
	d := &Dialog{Title: "Найти", W: 460}
	d.Fields = []*Field{textField("Найти", sel)}
	if replace {
		d.Title = "Найти и заменить"
		d.Fields = append(d.Fields, textField("Заменить на", ""))
	}
	cs := &Field{Label: "С учётом регистра", Kind: "check"}
	wh := &Field{Label: "Только слово целиком", Kind: "check"}
	rx := &Field{Label: "Регулярные выражения (замена: $1, $2…)", Kind: "check"}
	d.Fields = append(d.Fields, cs, wh, rx)
	opts := func() FindOpts { return FindOpts{Case: cs.On, Whole: wh.On, Regex: rx.On} }
	findNext := DlgBtn{Label: "Найти далее", Primary: true, Act: func() bool {
		if rx.On {
			if _, err := regexp.Compile(d.Val(0)); err != nil {
				d.Info = "Ошибка в выражении: " + err.Error()
				return false
			}
		}
		if a.E.Find(d.Val(0), opts()) {
			d.Info = ""
			a.scrollCaret = true
		} else {
			d.Info = "Ничего не найдено"
		}
		return false
	}}
	if replace {
		d.Buttons = []DlgBtn{{Label: "Закрыть"}, {Label: "Заменить все", Act: func() bool {
			n := a.E.ReplaceAll(d.Val(0), d.Val(1), opts())
			d.Info = a.trf("Заменено: %d", n)
			return false
		}}, {Label: "Заменить", Act: func() bool {
			if !a.E.Replace(d.Val(0), d.Val(1), opts()) {
				d.Info = "Больше не найдено"
			}
			a.scrollCaret = true
			return false
		}}, findNext}
	} else {
		d.Buttons = []DlgBtn{{Label: "Закрыть"}, findNext}
	}
	a.openDialog(d)
}

func (a *App) statsDialog() {
	st := a.E.Stats()
	d := &Dialog{Title: "Статистика", W: 380, Fields: []*Field{
		{Label: a.trf("Страниц: %d", st.Pages), Kind: "info"},
		{Label: a.trf("Слов: %d", st.Words), Kind: "info"},
		{Label: a.trf("Знаков (без пробелов): %d", st.CharsNoSpace), Kind: "info"},
		{Label: a.trf("Знаков (с пробелами): %d", st.Chars), Kind: "info"},
		{Label: a.trf("Абзацев: %d", st.Paras), Kind: "info"},
	}, Buttons: []DlgBtn{{Label: "Закрыть", Primary: true}}}
	a.openDialog(d)
}

func (a *App) gotoPageDialog() {
	d := &Dialog{Title: "Перейти к странице", W: 340, Fields: []*Field{textField("Номер страницы", "")}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "Перейти", Primary: true, Act: func() bool {
		n, ok := parseNum(d.Val(0))
		if !ok || int(n) < 1 || int(n) > a.numPages() {
			d.Info = a.trf("Введите число от 1 до %d", a.numPages())
			return false
		}
		a.gotoPage(int(n) - 1)
		return true
	}}}
	a.openDialog(d)
}

func (a *App) confirm(title, text string, buttons ...DlgBtn) {
	a.openDialog(&Dialog{Title: title, Text: text, W: 460, Buttons: buttons})
}

func (a *App) errorBox(text string) {
	a.confirm("Texora", text, DlgBtn{Label: "OK", Primary: true})
}

// ---------------------------------------------------------------- файлы
const AppVersion = "1.6.2"

func (a *App) Save() bool {
	if a.pdf != nil {
		a.msg(a.tr("PDF открыт только для чтения"))
		return false
	}
	if a.FilePath == "" || !a.saveable(extOf(a.FilePath)) {
		return a.SaveAs("")
	}
	return a.writeTo(a.FilePath)
}

func (a *App) writeTo(p string) bool {
	ext := extOf(p)
	data, err := SaveBytes(a.E.Doc, ext)
	if err != nil {
		a.errorBox("Не удалось сохранить: " + err.Error())
		return false
	}
	tmp := p + ".wltmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		a.errorBox("Не удалось сохранить файл:\n" + err.Error())
		return false
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			a.errorBox("Не удалось сохранить файл (возможно, он открыт в другой программе):\n" + err.Error())
			return false
		}
	}
	a.FilePath, a.FileName = p, filepath.Base(p)
	a.E.Dirty = false
	a.addRecent(p)
	a.msg("Сохранено: " + a.FileName)
	a.dropRecovery(a.E) // документ в файле — копия для восстановления больше не нужна
	return true
}

func (a *App) SaveAs(ext string) bool {
	if a.pdf != nil {
		a.msg(a.tr("PDF открыт только для чтения"))
		return false
	}
	if ext == "" {
		ext = extOf(a.FileName)
		if !a.saveable(ext) {
			ext = "docx"
		}
	}
	name := strings.TrimSuffix(a.FileName, filepath.Ext(a.FileName)) + "." + ext
	p, chosen := a.P.SaveDialog(name, ext)
	if p == "" {
		return false
	}
	if chosen == "pdf" || extOf(p) == "pdf" {
		a.exportPDFTo(p)
		return false
	}
	if extOf(p) == "" {
		p += "." + chosen
	}
	return a.writeTo(p)
}

func (a *App) exportPDFTo(p string) {
	if extOf(p) != "pdf" {
		p += ".pdf"
	}
	d, L := a.E.Doc, a.E.L
	note := ""
	data, err := ExportPDFVector(d, L, a.tb, nil)
	if err != nil {
		// запасной путь: страницы картинками (текст в таком PDF не ищется, но документ не потерян)
		note = " " + a.tr("(картинками: векторный экспорт не удался)")
		data, err = ExportPDF(L, func(i int) *image.RGBA { return a.renderPage(d, L, i, 200) }, nil)
	}
	if err == nil {
		err = os.WriteFile(p, data, 0o644)
	}
	if err != nil {
		a.errorBox("Не удалось сохранить PDF:\n" + err.Error())
		return
	}
	a.msg("PDF сохранён: " + filepath.Base(p) + note)
}

// renderPage — картинка страницы для печати и PDF; платформа может подменить подготовку холста.
var PrepareCanvas func(*Canvas)

func (a *App) renderPage(d *Doc, L *Layout, page int, dpi float64) *image.RGBA {
	return RenderPageImage(d, L, a.tb, page, dpi, PrepareCanvas)
}

// ---------------------------------------------------------------- буфер обмена
type clipPayload struct {
	Blocks []Block
	Media  map[string]*Media
}

func init() {
	gob.Register(&Media{})
}

func (a *App) copySelection(cut bool) {
	e := a.E
	if e.Collapsed() {
		return
	}
	bs := e.SelectedBlocks()
	media := map[string]*Media{}
	for _, b := range bs {
		for _, r := range b.P.Runs {
			if r.T == RImage {
				media[r.ID] = e.Doc.Media[r.ID]
			}
		}
	}
	var buf bytes.Buffer
	_ = gob.NewEncoder(&buf).Encode(clipPayload{Blocks: bs, Media: media})
	tmp := &Doc{Def: e.Doc.Def, Media: e.Doc.Media, Footnotes: map[string]*Note{}, Page: e.Doc.Page}
	cd := ClipData{Text: strings.ReplaceAll(BlocksToText(bs), "￼", ""), HTML: CFHTML(BlocksToHTML(bs, e.Doc)),
		RTF: RTFString(tmp, bs, false), Internal: buf.Bytes()}
	a.P.SetClipboard(cd)
	if cut {
		e.DeleteSelection()
	}
	a.after()
}

func (a *App) paste(plain bool) {
	cd := a.P.GetClipboard()
	e := a.E
	if !plain && len(cd.Internal) > 0 {
		var p clipPayload
		if gob.NewDecoder(bytes.NewReader(cd.Internal)).Decode(&p) == nil && len(p.Blocks) > 0 {
			for k, v := range p.Media {
				e.Doc.Media[k] = v
			}
			e.PasteBlocks(p.Blocks)
			a.after()
			return
		}
	}
	if !plain && cd.RTF != "" {
		if d, err := ReadRTF([]byte(cd.RTF)); err == nil && len(d.Body) > 0 {
			bs := d.Body
			if n := len(bs); n > 1 && bs[n-1].P != nil && len(bs[n-1].P.Runs) == 0 {
				bs = bs[:n-1]
			}
			for k, v := range d.Media {
				e.Doc.Media[k] = v
			}
			for _, en := range Flatten(&bs, nil, nil) {
				for i := range en.P.Runs {
					if en.P.Runs[i].F.Font == e.Doc.Def.Font {
						en.P.Runs[i].F.Font = ""
					}
				}
			}
			e.PasteBlocks(bs)
			a.after()
			return
		}
	}
	if !plain && cd.HTML != "" {
		bs, media := HTMLToBlocks(cd.HTML)
		if len(bs) > 0 {
			for k, v := range media {
				e.Doc.Media[k] = v
			}
			e.PasteBlocks(bs)
			a.after()
			return
		}
	}
	if len(cd.Image) > 0 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(cd.Image)); err == nil {
			e.InsertImage(cd.ImageMime, cd.Image, float64(cfg.Width)*0.75, float64(cfg.Height)*0.75)
			a.after()
			return
		}
	}
	if cd.Text != "" {
		e.InsertText(cd.Text)
		a.after()
	}
}

func (a *App) InsertImageFile(p string) {
	data, err := os.ReadFile(p)
	if err != nil {
		a.errorBox("Не удалось открыть рисунок: " + err.Error())
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		a.errorBox("Этот формат рисунка не поддерживается. Подойдут PNG, JPEG, GIF.")
		return
	}
	a.E.InsertImage("image/"+format, data, float64(cfg.Width)*0.75, float64(cfg.Height)*0.75)
	a.after()
}

// ---------------------------------------------------------------- команды
func (a *App) Command(c string) {
	e := a.E
	if a.pdf != nil {
		switch c {
		case "open", "new":
		case "print":
			a.pdfPrint()
			return
		case "find", "replace":
			a.pdfFindDialog()
			return
		case "copy":
			a.pdfCopy()
			return
		case "selectall":
			a.pdfSelectAll()
			return
		default:
			a.msg(a.tr("PDF открыт только для чтения"))
			return
		}
	}
	switch c {
	case "save":
		a.Save()
	case "saveas":
		a.SaveAs("")
	case "open":
		if p := a.P.OpenDialog("doc"); p != "" {
			a.OpenPath(p)
		}
	case "new":
		a.newTab(NewDoc(), "", "")
		a.backstage = false
	case "print":
		L := e.L
		d := e.Doc
		if err := a.P.Print(len(L.Pages), func(i int, dpi float64) *image.RGBA { return a.renderPage(d, L, i, dpi) }, L.Page.W, L.Page.H); err != nil {
			a.errorBox("Печать не удалась: " + err.Error())
		}
	case "pdf":
		name := strings.TrimSuffix(a.FileName, filepath.Ext(a.FileName)) + ".pdf"
		if p, _ := a.P.SaveDialog(name, "pdf"); p != "" {
			a.exportPDFTo(p)
		}
	case "undo":
		e.Undo()
	case "redo":
		e.Redo()
	case "cut":
		a.copySelection(true)
	case "copy":
		a.copySelection(false)
	case "paste":
		a.paste(false)
	case "pasteplain":
		a.paste(true)
	case "painter":
		if e.HasPainter() {
			e.painter = nil
		} else {
			e.CopyFormat()
			a.msg("Формат скопирован — выделите текст, к которому его применить")
		}
	case "bold":
		e.Toggle("b")
	case "italic":
		e.Toggle("i")
	case "under":
		e.Toggle("u")
	case "strike":
		e.Toggle("s")
	case "sub":
		e.Toggle("sub")
	case "sup":
		e.Toggle("sup")
	case "bullets":
		e.ToggleList("ul")
	case "numbering":
		e.ToggleList("ol")
	case "find":
		a.findDialog(false)
	case "replace":
		a.findDialog(true)
	case "selectall":
		e.SelectAll()
	case "pagebreak":
		e.InsertPageBreak()
	case "picture":
		if p := a.P.OpenDialog("image"); p != "" {
			a.InsertImageFile(p)
		}
	case "link":
		a.linkDialog()
	case "footnote":
		e.InsertFootnote()
	case "toc":
		if !e.InsertTOC() {
			a.errorBox("В документе нет заголовков. Выделите абзацы и примените стиль «Заголовок 1», «Заголовок 2» или «Заголовок 3».")
		}
	case "header", "footer":
		page := 0
		if cr := e.Caret(e.Sel.F); cr != nil && e.Story == "body" {
			page = cr.Page
		} else {
			page = e.HFPage
		}
		e.SetStory(e.PageStory(page, c), page)
	case "pagenum":
		e.InsertField("PAGE")
	case "numpages":
		e.InsertField("NUMPAGES")
	case "titlepg":
		e.Checkpoint("")
		e.SetSection(func(s *SecView) { s.TitlePg = !s.TitlePg })
		e.Changed()
	case "row+", "row-above", "col+", "col-left", "row-", "col-", "merge", "border", "shade", "deltable":
		op := c
		if c == "deltable" {
			op = "del"
		}
		if !e.TableOp(op) {
			a.toast("Поставьте курсор в ячейку таблицы")
		}
	}
	a.after()
}

func (a *App) SetZoom(z float64) {
	old := a.zoom
	a.zoom = math.Max(0.3, math.Min(4, z))
	a.scrollY *= a.zoom / old
	a.scrollX = 0
	a.invalidate()
}

func (a *App) fitWidth() {
	if a.pdf != nil {
		a.SetZoom((a.docRect.W - a.d(70)) / (a.maxPageW() / a.k() * 96 / 72 * a.S))
		return
	}
	pg := a.E.Doc.Page
	a.SetZoom((a.docRect.W - a.d(70)) / (pg.W * 96 / 72 * a.S))
}

func (a *App) fitPage() {
	if a.pdf != nil {
		p := a.pdf.Doc.Pages[a.pdfCurPage()]
		a.SetZoom(math.Min((a.docRect.H-a.d(50))/(p.H()*96/72*a.S), (a.docRect.W-a.d(70))/(p.W()*96/72*a.S)))
		a.pdfGoto(a.pdfCurPage(), -1)
		return
	}
	pg := a.E.Doc.Page
	a.SetZoom(math.Min((a.docRect.H-a.d(50))/(pg.H*96/72*a.S), (a.docRect.W-a.d(70))/(pg.W*96/72*a.S)))
}

// ---------------------------------------------------------------- мышь
func (a *App) MouseMove(x, y float64, buttons bool) {
	a.mx, a.my = x, y
	if a.dialog != nil {
		h := a.hitAt(x, y)
		a.hover = ""
		if h != nil && strings.HasPrefix(h.id, "dlgbtn:") {
			a.hover = h.id
		}
		return
	}
	if a.popup != nil && a.popup.R.Contains(x, y) {
		if a.popup.Move != nil {
			a.popup.Move(x, y)
		}
		a.hover = ""
		return
	}
	if a.dragScroll {
		ch := a.contentH()
		trackH := a.docRect.H - a.d(8)
		th := math.Max(a.d(36), trackH*a.docRect.H/ch)
		a.scrollY = (y - a.dragOff - a.docRect.Y - a.d(4)) / math.Max(1, trackH-th) * (ch - a.docRect.H)
		a.clampScroll()
		return
	}
	if a.drag != nil {
		pt := a.toPage(x, y)
		d := a.drag
		nw := math.Max(10, pt.X-d.ix)
		d.w = nw
		d.h = nw * d.h0 / d.w0
		return
	}
	if a.press == "zoomslider" {
		a.zoomFromSlider(x)
		return
	}
	if a.dragSel && a.pdf != nil {
		a.pdfDragTo(x, y)
		return
	}
	if a.dragSel {
		pt := a.toPage(x, y)
		if pos, ok := a.E.Hit(pt); ok {
			a.E.Sel.F = pos
		}
		if y > a.docRect.Y+a.docRect.H-a.d(10) {
			a.scrollY += a.d(18)
		} else if y < a.docRect.Y+a.d(10) {
			a.scrollY -= a.d(18)
		}
		a.clampScroll()
		return
	}
	h := a.hitAt(x, y)
	a.hover, a.tip = "", ""
	if h != nil && h.id != "doc" && h.id != "caption" && h.id != "bs-bg" && h.id != "popup" {
		a.hover = h.id
		a.tip = h.tip
	}
}

func (a *App) zoomFromSlider(x float64) {
	for _, h := range a.hits {
		if h.id == "zoomslider" {
			t := clamp01((x - h.r.X - a.d(6)) / (h.r.W - a.d(12)))
			a.SetZoom(math.Exp(math.Log(0.3) + t*(math.Log(4)-math.Log(0.3))))
		}
	}
}

func (a *App) MouseLeave() { a.hover, a.tip = "", "" }

func (a *App) MouseDown(x, y float64, m Mods, right bool) {
	now := time.Now()
	if now.Sub(a.lastClick) < 450*time.Millisecond && math.Abs(x-a.lastCX) < a.d(4) && math.Abs(y-a.lastCY) < a.d(4) {
		a.clicks++
	} else {
		a.clicks = 1
	}
	a.lastClick, a.lastCX, a.lastCY = now, x, y
	if a.dialog != nil {
		if a.popup != nil {
			if a.popup.R.Contains(x, y) {
				if a.popup.Click(x, y) {
					a.popup = nil
				}
			} else {
				a.popup = nil
			}
			return
		}
		a.press = ""
		if h := a.hitAt(x, y); h != nil && strings.HasPrefix(h.id, "dlgbtn:") {
			a.press = h.id
		}
		a.dialogClick(x, y)
		return
	}
	if a.popup != nil {
		if a.popup.R.Contains(x, y) {
			if a.popup.Click(x, y) {
				a.popup = nil
			}
			return
		}
		owner := a.popup.Owner
		a.popup = nil
		if h := a.hitAt(x, y); h != nil && (h.id == owner || h.id == owner+":m") {
			return
		}
	}
	if a.inspPeek && !a.inspRect.Contains(x, y) {
		if h := a.hitAt(x, y); h == nil || !strings.HasPrefix(h.id, "is:") {
			a.inspPeek = false
		}
	}
	h := a.hitAt(x, y)
	if h == nil {
		return
	}
	a.press = h.id
	switch h.id {
	case "scroll":
		ch := a.contentH()
		trackH := a.docRect.H - a.d(8)
		th := math.Max(a.d(36), trackH*a.docRect.H/ch)
		ty := a.docRect.Y + a.d(4) + (trackH-th)*a.scrollY/math.Max(1, ch-a.docRect.H)
		if y < ty || y > ty+th {
			a.scrollY = (y - th/2 - a.docRect.Y) / math.Max(1, trackH-th) * (ch - a.docRect.H)
			a.clampScroll()
			ty = a.docRect.Y + a.d(4) + (trackH-th)*a.scrollY/math.Max(1, ch-a.docRect.H)
		}
		a.dragScroll = true
		a.dragOff = y - ty
		return
	case "zoomslider":
		a.zoomFromSlider(x)
		return
	case "imghandle":
		if r, pg, ok := a.imgRect(); ok {
			a.drag = &imgDrag{pos: *a.imgSel, w0: r.W, h0: r.H, w: r.W, h: r.H, page: pg, ix: r.X, iy: r.Y}
		}
		return
	case "doc":
		if a.pdf != nil {
			if !right {
				a.pdfMouseDown(x, y, m)
			}
			return
		}
		a.docMouseDown(x, y, m, right)
	}
}

func (a *App) docMouseDown(x, y float64, m Mods, right bool) {
	e := a.E
	pt := a.toPage(x, y)
	if right {
		a.contextMenu(x, y, pt)
		return
	}
	// картинка
	if pos, _, _, ok := e.ImageAt(pt); ok && a.clicks == 1 {
		a.imgSel = &pos
		e.Sel = Sel{pos, Pos{P: pos.P, O: pos.O + 1}}
		return
	}
	a.imgSel = nil
	zone := e.ZoneAt(pt)
	if zone != e.Story {
		hf := IsHF(zone)
		switch {
		case hf && e.Story == "body" && a.clicks < 2:
			return
		case zone == "body" && IsHF(e.Story) && a.clicks < 2:
			e.SetStory("body", 0)
		default:
			e.SetStory(zone, pt.Page)
		}
	}
	pos, ok := e.Hit(pt)
	if !ok {
		return
	}
	e.GoalX = math.NaN()
	if m.Ctrl && a.clicks == 1 && e.Story == "body" {
		if f := pos.P.FmtAt(pos.O + 1); strings.HasPrefix(f.Link, "#") {
			a.gotoBookmark(f.Link[1:])
			return
		}
	}
	if a.clicks == 2 {
		if i, _ := e.EqNear(pos); i >= 0 {
			a.eqDialog(pos.P, i)
			return
		}
	}
	switch {
	case a.clicks == 2:
		e.SelectWord(pos)
	case a.clicks >= 3:
		e.SelectPara(pos)
	default:
		e.SetCaret(pos, m.Shift)
		a.dragSel = true
	}
	a.CaretOn = true
}

func (a *App) MouseUp(x, y float64) {
	if a.dragSel && a.pdf != nil {
		a.dragSel = false
	}
	if a.dragSel {
		a.dragSel = false
		if a.E.HasPainter() && !a.E.Collapsed() {
			a.E.PasteFormat()
		}
	}
	if a.dragScroll {
		a.dragScroll = false
		a.press = ""
		return
	}
	if a.drag != nil {
		d := a.drag
		a.drag = nil
		a.E.ResizeImage(d.pos, d.w, d.h)
		p := d.pos
		a.imgSel = &p
		a.press = ""
		return
	}
	p := a.press
	a.press = ""
	if a.dialog != nil {
		return
	}
	h := a.hitAt(x, y)
	if h == nil || h.id != p || h.act == nil {
		return
	}
	h.act()
}

func (a *App) Wheel(delta float64, m Mods) {
	if a.popup != nil && a.popup.Wheel != nil {
		a.popup.Wheel(delta * a.d(0.6))
		return
	}
	if a.dialog != nil {
		return
	}
	if a.backstage {
		a.bsScroll -= delta * a.d(1.2)
		return
	}
	if a.navRect.Contains(a.mx, a.my) {
		a.navScroll = math.Max(0, a.navScroll-delta*a.d(1.2))
		return
	}
	if a.inspRect.Contains(a.mx, a.my) {
		a.inspScroll = math.Max(0, a.inspScroll-delta*a.d(1.2))
		return
	}
	if m.Ctrl {
		if delta > 0 {
			a.SetZoom(a.zoom * 1.1)
		} else {
			a.SetZoom(a.zoom / 1.1)
		}
		return
	}
	if m.Shift {
		a.scrollX -= delta * a.d(1.2)
		a.clampScroll()
		return
	}
	// плавная прокрутка: задаём цель, Tick доводит до неё за несколько кадров
	if !a.scrollAnim {
		a.scrollGoal = a.scrollY
	}
	a.scrollGoal -= delta * a.d(1.2)
	maxY := math.Max(0, a.contentH()-a.docRect.H)
	a.scrollGoal = math.Max(0, math.Min(a.scrollGoal, maxY))
	a.scrollAnim = true
	a.scrollSeen = a.scrollY
}

func (a *App) contextMenu(x, y float64, pt PagePoint) {
	e := a.E
	if pos, _, _, ok := e.ImageAt(pt); ok {
		a.imgSel = &pos
		e.Sel = Sel{pos, Pos{P: pos.P, O: pos.O + 1}}
		items := append(a.wrapItems(), PopItem{Sep: true},
			PopItem{Label: "Вырезать", Icon: "cut", Right: "Ctrl+X", Act: func() { a.Command("cut") }},
			PopItem{Label: "Копировать", Icon: "copy", Right: "Ctrl+C", Act: func() { a.Command("copy") }})
		a.popup = a.listPopup("ctx", x, y, a.d(300), items, a.d(560), false)
		return
	}
	if pos, ok := e.Hit(pt); ok && e.Collapsed() {
		e.SetCaret(pos, false)
	}
	var top []PopItem
	if e.Collapsed() {
		top = a.spellItems(e.Sel.F)
	}
	if i, _ := e.EqNear(e.Sel.F); i >= 0 {
		p := e.Sel.F.P
		top = append(top, PopItem{Label: a.tr("Изменить формулу…"), Icon: "eq", Act: func() { a.eqDialog(p, i) }}, PopItem{Sep: true})
	}
	items := []PopItem{
		{Label: "Вырезать", Icon: "cut", Right: "Ctrl+X", Act: func() { a.Command("cut") }},
		{Label: "Копировать", Icon: "copy", Right: "Ctrl+C", Act: func() { a.Command("copy") }},
		{Label: "Вставить", Icon: "paste", Right: "Ctrl+V", Act: func() { a.Command("paste") }},
		{Label: "Вставить только текст", Icon: "paste", Right: "Ctrl+Shift+V", Act: func() { a.Command("pasteplain") }},
		{Sep: true},
		{Label: "Абзац…", Icon: "para", Act: func() { a.paragraphDialog() }},
		{Label: "Гиперссылка…", Icon: "link", Right: "Ctrl+Shift+K", Act: func() { a.linkDialog() }},
	}
	if e.CurrentF().Link != "" {
		u := e.CurrentF().Link
		if strings.HasPrefix(u, "#") {
			items = append(items, PopItem{Label: "Перейти к заголовку", Icon: "link", Act: func() { a.gotoBookmark(u[1:]) }})
		} else {
			items = append(items, PopItem{Label: "Открыть ссылку", Icon: "link", Act: func() { a.P.OpenURL(u) }})
		}
	}
	items = append(items, PopItem{Sep: true}, PopItem{Label: "Создать примечание", Icon: "comment", Right: "Ctrl+Alt+M", Act: func() { a.commentDialog("") }})
	if id := e.CommentAt(); id != "" {
		items = append(items, PopItem{Label: "Удалить примечание", Icon: "comment-", Act: func() { e.DeleteComment(id); a.after() }})
	}
	if f := e.Sel.F.P.FmtAt(e.Sel.F.O); f.Ins != "" || f.Del != "" || e.Sel.F.P.FmtAt(e.Sel.F.O+1).Ins != "" || e.Sel.F.P.FmtAt(e.Sel.F.O+1).Del != "" {
		items = append(items, PopItem{Label: "Принять исправление", Icon: "accept", Act: func() { e.ResolveRevisions(true, false); a.after() }},
			PopItem{Label: "Отклонить исправление", Icon: "reject", Act: func() { e.ResolveRevisions(false, false); a.after() }})
	}
	if st := a.styleMenuItems(); st != nil {
		items = append(append(items, PopItem{Sep: true}), st...)
	}
	if _, to := RegionSpan(e.Doc, "toc"); to >= 0 && e.Sel.F.P.PF.Region != "" {
		items = append(items, PopItem{Label: "Обновить поле", Icon: "update", Right: "F9", Act: func() { a.UpdateFields() }})
	}
	items = append(top, items...)
	if e.TableCtx() != nil {
		items = append(items, PopItem{Sep: true},
			PopItem{Label: "Вставить строку снизу", Icon: "row+", Act: func() { a.Command("row+") }},
			PopItem{Label: "Вставить столбец справа", Icon: "col+", Act: func() { a.Command("col+") }},
			PopItem{Label: "Удалить строку", Icon: "row-", Act: func() { a.Command("row-") }},
			PopItem{Label: "Удалить столбец", Icon: "col-", Act: func() { a.Command("col-") }},
			PopItem{Label: "Объединить с ячейкой справа", Icon: "merge", Act: func() { a.Command("merge") }},
			PopItem{Label: "Удалить таблицу", Icon: "deltable", Act: func() { a.Command("deltable") }})
	}
	a.popup = a.listPopup("ctx", x, y, a.d(270), items, a.d(560), false)
}

// ---------------------------------------------------------------- клавиатура
func (a *App) KeyDown(vk int, m Mods) bool {
	a.CaretOn = true
	if a.dialog != nil {
		if a.popup != nil {
			if vk == 27 || (a.popup.Key != nil && !a.popup.Key(vk, m)) {
				a.popup = nil
			}
			return true
		}
		a.dialogKey(vk, m)
		return true
	}
	if a.popup != nil {
		if vk == 27 {
			a.popup = nil
			return true
		}
		if a.popup.Key != nil {
			if !a.popup.Key(vk, m) {
				a.popup = nil
			}
			return true
		}
	}
	if vk == 27 && a.inspPeek {
		a.inspPeek = false
		return true
	}
	if m.Ctrl && !m.Alt && !m.Shift && vk == 75 { // Ctrl+K — палитра команд (гиперссылка — Ctrl+Shift+K)
		a.openPalette()
		return true
	}
	if a.backstage {
		switch {
		case vk == 27 && a.bsFromDoc:
			a.backstage = false
		case m.Ctrl && vk == 78:
			a.Command("new")
		case m.Ctrl && vk == 79:
			a.Command("open")
		case vk == 33 || vk == 38:
			a.bsScroll -= a.d(60)
		case vk == 34 || vk == 40:
			a.bsScroll += a.d(60)
		}
		return true
	}
	if a.pdf != nil {
		if a.pdfKey(vk, m) {
			return true
		}
		// дальше пропускаем только общие сочетания (открыть, создать, закрыть, вкладки)
		if !(m.Ctrl && (vk == 'O' || vk == 'N' || vk == 'W' || vk == 9 || vk == 115)) && vk != 112 {
			return true
		}
	}
	e := a.E
	ctrl, sh, alt := m.Ctrl, m.Shift, m.Alt
	handled := true
	if a.imgSel != nil && (vk == 8 || vk == 46) {
		a.imgSel = nil
		e.DeleteSelection()
		a.after()
		return true
	}
	if vk != 16 && vk != 17 && vk != 18 {
		a.imgSel = nil
	}
	switch {
	case vk == 37:
		e.MoveH(-1, ctrl, sh)
	case vk == 39:
		e.MoveH(1, ctrl, sh)
	case vk == 38:
		e.MoveV(-1, sh)
	case vk == 40:
		e.MoveV(1, sh)
	case vk == 33 || vk == 34:
		for i := 0; i < 25; i++ {
			e.MoveV(map[bool]int{true: -1, false: 1}[vk == 33], sh)
		}
	case vk == 36:
		e.Home(ctrl, sh)
	case vk == 35:
		e.End(ctrl, sh)
	case vk == 8:
		e.Backspace(ctrl)
	case vk == 46:
		e.Delete(ctrl)
	case vk == 13:
		switch {
		case ctrl && sh:
			e.InsertColumnBreak()
		case ctrl:
			e.InsertPageBreak()
		case sh:
			e.InsertInline(Run{T: RBreak})
		default:
			e.Enter()
		}
	case vk == 9 && ctrl:
		a.switchTab(map[bool]int{true: -1, false: 1}[sh])
	case vk == 9:
		e.Tab(sh)
	case vk == 27:
		if e.Story != "body" {
			e.SetStory("body", 0)
		} else if e.HasPainter() {
			e.painter = nil
		} else {
			handled = false
		}
	case vk == 118: // F7
		a.nextSpellError()
	case vk == 120: // F9
		a.UpdateFields()
	case ctrl && alt && vk == 77: // Ctrl+Alt+M
		a.commentDialog("")
	case ctrl && sh && vk == 69: // Ctrl+Shift+E
		e.Doc.Track = !e.Doc.Track
		e.Dirty = true
		a.msg(map[bool]string{true: a.tr("Режим исправлений включён"), false: a.tr("Режим исправлений выключен")}[e.Doc.Track])
	case alt && !ctrl && vk == 187: // Alt+=
		a.eqDialog(nil, -1)
	case vk == 114 && sh: // Shift+F3
		modes := []string{"lower", "upper", "title"}
		a.caseCycle = (a.caseCycle + 1) % 3
		e.ChangeCase(modes[a.caseCycle])
	case vk == 123: // F12
		a.SaveAs("")
	case ctrl && alt && vk >= 49 && vk <= 51:
		e.SetHeading(vk - 48)
	case ctrl && alt && vk == 70:
		e.InsertFootnote()
	case ctrl && !alt:
		switch vk {
		case 66:
			e.Toggle("b")
		case 73:
			e.Toggle("i")
		case 85:
			e.Toggle("u")
		case 68:
			if sh {
				e.Toggle("du")
			} else {
				handled = false
			}
		case 90:
			if sh {
				e.Redo()
			} else {
				e.Undo()
			}
		case 89:
			e.Redo()
		case 65:
			if sh {
				e.Toggle("caps")
			} else {
				e.SelectAll()
			}
		case 67:
			if sh {
				e.CopyFormat()
				a.msg("Формат скопирован")
			} else {
				a.copySelection(false)
			}
		case 88:
			a.copySelection(true)
		case 86:
			if sh {
				if e.HasPainter() {
					e.PasteFormat()
				} else {
					a.paste(true)
				}
			} else {
				a.paste(false)
			}
		case 69:
			e.SetAlign("center")
		case 76:
			if sh {
				e.ToggleList("ul")
			} else {
				e.SetAlign("left")
			}
		case 82:
			e.SetAlign("right")
		case 74:
			e.SetAlign("justify")
		case 187:
			if sh {
				e.Toggle("sup")
			} else {
				e.Toggle("sub")
			}
		case 190:
			if sh {
				e.GrowFont(1)
			}
		case 188:
			if sh {
				e.GrowFont(-1)
			}
		case 221:
			e.SetSize(e.EffectiveF().Size + 1)
		case 219:
			e.SetSize(math.Max(1, e.EffectiveF().Size-1))
		case 32:
			e.ClearFormat()
		case 49:
			e.SetLineSpacing(1)
		case 50:
			e.SetLineSpacing(2)
		case 53:
			e.SetLineSpacing(1.5)
		case 77:
			if sh {
				e.Indent(-1)
			} else {
				e.Indent(1)
			}
		case 78:
			if sh {
				e.SetHeading(0)
			} else {
				a.Command("new")
			}
		case 83:
			if sh {
				a.SaveAs("")
			} else {
				a.Save()
			}
		case 79:
			a.Command("open")
		case 80:
			a.Command("print")
		case 70:
			a.findDialog(false)
		case 72:
			a.findDialog(true)
		case 75:
			a.linkDialog()
		case 71:
			a.gotoPageDialog()
		case 87:
			a.closeTab(a.cur)
		default:
			handled = false
		}
	default:
		handled = false
	}
	if handled {
		a.after()
	}
	return handled
}

func (a *App) Char(r rune) {
	if a.dialog != nil {
		if a.popup != nil && a.popup.Char != nil {
			a.popup.Char(r)
			return
		}
		a.dialogChar(r)
		return
	}
	if a.popup != nil {
		if a.popup.Char != nil {
			a.popup.Char(r)
		}
		return
	}
	if a.backstage || a.pdf != nil {
		return
	}
	if r < 32 || r == 127 {
		return
	}
	a.imgSel = nil
	a.E.InsertText(string(r))
	a.after()
}

// Cursor: 0 стрелка, 1 I-курсор, 2 рука, 3 диагональ.
func (a *App) Cursor(x, y float64) int {
	if a.dialog != nil || a.backstage {
		return 0
	}
	if a.popup != nil && a.popup.R.Contains(x, y) {
		return 0
	}
	h := a.hitAt(x, y)
	if h == nil {
		return 0
	}
	if h.id == "doc" && a.pdf != nil {
		if _, _, exact := a.pdfPosAt(x, y); exact || a.dragSel {
			return 1
		}
		return 0
	}
	if h.id == "doc" {
		pt := a.toPage(x, y)
		if _, _, _, ok := a.E.ImageAt(pt); ok {
			return 0
		}
	}
	return h.cursor
}

func (a *App) IsCaption(x, y float64) bool {
	h := a.hitAt(x, y)
	return h != nil && h.id == "caption"
}
