package main

// Вёрстка: строки, списки, таблицы, страницы со сносками и колонтитулами.

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

const (
	TabStep  = 1.25 * CM
	CellPad  = 5.4
	CellPadV = 0
	FnSep    = 12.0
	measureK = 4.0 // измеряем в 4× размере для точности
)

// Measurer — измерение текста в пунктах с кэшем.
type Measurer struct {
	tb   TextBackend
	w    map[string]float64
	m    map[Font][2]float64
	lead map[Font]float64
	eq   map[string]*EqBox
}

func NewMeasurer(tb TextBackend) *Measurer {
	return &Measurer{tb: tb, w: map[string]float64{}, m: map[Font][2]float64{}, lead: map[Font]float64{}}
}

func (m *Measurer) FontOf(f F) Font {
	size := f.Size
	if f.Sup || f.Sub {
		size *= 0.65
	}
	return Font{Family: f.Font, Px: size, Bold: f.B, Italic: f.I}
}

func (m *Measurer) Width(s string, f Font) float64 {
	if s == "" {
		return 0
	}
	k := f.Family + "\x01" + strconv.FormatFloat(f.Px, 'f', 2, 64) + boolS(f.Bold) + boolS(f.Italic) + "\x01" + s
	if v, ok := m.w[k]; ok {
		return v
	}
	if len(m.w) > 400000 {
		m.w = map[string]float64{}
	}
	big := f
	big.Px *= measureK
	big.Underline = false
	v := m.tb.Measure(s, big) / measureK
	m.w[k] = v
	return v
}

func (m *Measurer) Metrics(f Font) (float64, float64) {
	f.Underline = false
	if v, ok := m.m[f]; ok {
		return v[0], v[1]
	}
	big := f
	big.Px *= measureK
	a, d := m.tb.Metrics(big)
	v := [2]float64{a / measureK, d / measureK}
	m.m[f] = v
	return v[0], v[1]
}

// leader — бэкенд, знающий внешний интерлиньяж шрифта.
type leader interface{ Leading(f Font) float64 }

// Leading — добавка к высоте строки сверх asc+desc (внешний интерлиньяж шрифта), как в Word.
func (m *Measurer) Leading(f Font) float64 {
	ld, ok := m.tb.(leader)
	if !ok {
		return 0
	}
	f.Underline = false
	if v, ok := m.lead[f]; ok {
		return v
	}
	big := f
	big.Px *= measureK
	v := ld.Leading(big) / measureK
	m.lead[f] = v
	return v
}

func boolS(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// ---------------------------------------------------------------- эффективные форматы
func styleOf(d *Doc, p *Para) headingStyle {
	if k := StyleKey(p); k != "" {
		if s, ok := d.StyleDef(k); ok {
			return s
		}
	}
	return headingStyle{Before: -1, After: -1}
}

func EffF(d *Doc, p *Para, f F) F {
	e := F{Font: d.Def.Font, Size: d.Def.Size}
	hs := styleOf(d, p)
	if hs.Size > 0 {
		e.Size = hs.Size
	}
	if (p.PF.Heading > 0 && p.PF.Heading <= 2) || p.PF.Style == "Title" || p.PF.Style == "TOCHeading" {
		e.Font = headingFont(d)
	}
	if hs.Font != "" {
		e.Font = hs.Font
	}
	e.B, e.I = hs.B, hs.I
	e.Color = hs.Color
	if f.Font != "" {
		e.Font = f.Font
	}
	if f.Size > 0 {
		e.Size = f.Size
	}
	if f.Color != "" {
		e.Color = f.Color
	}
	e.B = e.B || f.B
	e.I = e.I || f.I
	e.Caps = hs.Caps || f.Caps
	e.U, e.DU, e.S, e.Sup, e.Sub, e.Bg, e.Link = f.U, f.DU, f.S, f.Sup, f.Sub, f.Bg, f.Link
	e.Ins, e.Del, e.Cmt, e.Lang = f.Ins, f.Del, f.Cmt, f.Lang
	return e
}

func headingFont(d *Doc) string {
	switch d.Def.Font {
	case "Calibri":
		return "Calibri Light"
	}
	return d.Def.Font
}

type EPF struct {
	Align                          string
	IndL, IndR, IndF               float64
	Before, After, Line, LineExact float64
	LineRule                       string
	List                           *List
	PB, Keep                       bool
	Tabs                           []TabStop
}

func EffPF(d *Doc, p *Para) EPF {
	pf := p.PF
	hs := styleOf(d, p)
	e := EPF{Align: pf.Align, IndR: pf.IndR, Line: d.Def.Line, After: d.Def.After, LineExact: pf.LineExact,
		LineRule: pf.LineRule, List: pf.List, PB: pf.PB, Keep: pf.KeepNext || pf.Heading > 0, Tabs: pf.Tabs}
	if e.Align == "" {
		e.Align = hs.Align
	}
	if e.Align == "" {
		e.Align = "left"
	}
	if hs.Before >= 0 {
		e.Before = hs.Before
	}
	if hs.After >= 0 {
		e.After = hs.After
	}
	if hs.Line > 0 {
		e.Line = hs.Line
	}
	e.IndL = hs.IndL
	if hs.Set {
		e.IndF = hs.IndF
	}
	if p.PF.Style == "NoSpacing" {
		e.Line = 1
	}
	if pf.Before.Set {
		e.Before = pf.Before.V
	}
	if pf.After.Set {
		e.After = pf.After.V
	}
	if pf.Line.Set {
		e.Line = pf.Line.V
	}
	if pf.IndL.Set {
		e.IndL = pf.IndL.V
	}
	if pf.IndF.Set {
		e.IndF = pf.IndF.V
	}
	if p.PF.Style == "Quote" {
		if !pf.IndL.Set {
			e.IndL = 43
		}
		e.IndR = math.Max(e.IndR, 43)
		if pf.Align == "" && hs.Align == "" {
			e.Align = "center"
		}
	}
	if pf.List != nil {
		if !pf.IndL.Set {
			e.IndL = 36 * float64(pf.List.Level+1)
		}
		if !pf.IndF.Set {
			e.IndF = -18
		}
	}
	return e
}

// ---------------------------------------------------------------- нумерация
func roman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	s := ""
	for i, v := range vals {
		for n >= v {
			s += syms[i]
			n -= v
		}
	}
	return s
}

func letters(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('a'+n%26)) + s
		n /= 26
	}
	return s
}

func FmtNum(n int, f string) string {
	switch f {
	case "lowerLetter":
		return letters(n)
	case "upperLetter":
		return strings.ToUpper(letters(n))
	case "lowerRoman":
		return roman(n)
	case "upperRoman":
		return strings.ToUpper(roman(n))
	case "russianLower":
		r := []rune("абвгдежзиклмнопрстуфхцчшщэюя")
		return string(r[(n-1)%len(r)])
	case "russianUpper":
		r := []rune("АБВГДЕЖЗИКЛМНОПРСТУФХЦЧШЩЭЮЯ")
		return string(r[(n-1)%len(r)])
	}
	return strconv.Itoa(n)
}

var bullets = []string{"•", "o", "▪"}
var olDefault = []string{"decimal", "lowerLetter", "lowerRoman"}

type ListState struct{ c map[string][]int }

func NewListState() *ListState { return &ListState{c: map[string][]int{}} }

func (s *ListState) Marker(l *List) string {
	key := l.ID
	if key == "" {
		key = l.Kind
	}
	arr := s.c[key]
	lvl := l.Level
	for len(arr) < lvl+1 {
		arr = append(arr, 0)
	}
	arr = arr[:lvl+1]
	for i := 0; i < lvl; i++ {
		if arr[i] == 0 {
			arr[i] = 1
		}
	}
	arr[lvl]++
	s.c[key] = arr
	if l.Kind == "ul" {
		if l.Text != "" && !strings.Contains(l.Text, "%") {
			return l.Text
		}
		return bullets[lvl%3]
	}
	f := l.Fmt
	if f == "" || f == "bullet" {
		f = olDefault[lvl%3]
	}
	if l.Text != "" {
		out := []rune{}
		t := []rune(l.Text)
		for i := 0; i < len(t); i++ {
			if t[i] == '%' && i+1 < len(t) && t[i+1] >= '1' && t[i+1] <= '9' {
				k := int(t[i+1] - '1')
				v := 1
				if k < len(arr) {
					v = arr[k]
				}
				ff := "decimal"
				if k == lvl {
					ff = f
				}
				out = append(out, []rune(FmtNum(v, ff))...)
				i++
				continue
			}
			out = append(out, t[i])
		}
		return string(out)
	}
	if f == "decimal" {
		return FmtNum(arr[lvl], f) + "."
	}
	return FmtNum(arr[lvl], f) + ")"
}

// ---------------------------------------------------------------- строки
const (
	kText = iota
	kTab
	kImg
	kNote
	kField
	kBreak
	kFloat // привязка плавающей картинки (нулевой ширины)
	kEq    // формула
)

type Item struct {
	K        int
	Text     []rune
	O        int
	F        F
	Font     Font
	X, W, WT float64
	SP       int
	Shift    float64
	ID       string
	H        float64
	Lead     string  // заполнитель табуляции
	Hyph     bool    // после текста — знак переноса
	FY       float64 // плавающая картинка: смещение от верха абзаца
	Wrap     string
	Eq       *EqBox
}

func (it *Item) Len() int {
	if it.K == kText || it.K == kTab {
		return len(it.Text)
	}
	return 1
}

type Marker struct {
	Text string
	X    float64
	Font Font
	F    F
}

type Line struct {
	Items   []Item
	H, Base float64
	Trail   float64 // добавка межстрочного интервала под строкой (у последней строки на листе может не помещаться)
	A, B    int
	X0, EX  float64
	Hard    bool
	P       *Para
	Marker  *Marker
	Floats  []Item // плавающие картинки абзаца (на первой строке)
	Float   bool   // служебная строка: картинка «сверху и снизу»
	Rev     bool   // в строке есть исправления
}

// Excl — область, занятая картинкой с обтеканием «вокруг рамки».
type Excl struct {
	Side string
	W    float64
	Rem  float64
}

type ParaL struct {
	Lines         []*Line
	Before, After float64
	PB, Keep      bool
	sig           string
	exOut         Excl
}

type LayoutCtx struct {
	Lists  *ListState
	FnNum  func(id string) string
	Field  func(name string) string
	Cache  map[*Para]*ParaL
	FieldV string
	Cell   int // глубина вложенности в таблицы
	Doc    *Doc
}

// CellPF — абзацы в таблице без своих интервалов: как стиль «Сетка таблицы» в Word (после 0, одинарный).
func CellPF(p *Para, pf EPF) EPF {
	if !p.PF.After.Set {
		pf.After = 0
	}
	if !p.PF.Line.Set && pf.LineExact == 0 {
		pf.Line = 1
	}
	return pf
}

// CSKey — ключ «не добавлять интервал между абзацами одного стиля» (у списков — стиль «Абзац списка»).
func CSKey(p *Para) string {
	if p.PF.CS != "" {
		return p.PF.CS
	}
	if p.PF.List != nil && p.PF.Heading == 0 && p.PF.Style == "" {
		return "ListParagraph"
	}
	return ""
}

type word struct {
	atoms []Item
	w, wt float64
}

const floatGap = 9.0

func floatOf(r Run) bool { return r.T == RImage && r.Wrap != "" }

func (m *Measurer) tokenize(d *Doc, p *Para, ctx *LayoutCtx) ([]Item, int) {
	var atoms []Item
	off := 0
	for _, r := range p.Runs {
		switch r.T {
		case RText:
			f := EffF(d, p, r.F)
			font := m.FontOf(f)
			t := r.Text
			if f.Caps {
				t = []rune(strings.ToUpper(string(t)))
			}
			i := 0
			for i < len(t) {
				if t[i] == '\t' {
					atoms = append(atoms, Item{K: kTab, Text: []rune{'\t'}, O: off + i, F: f, Font: font})
					i++
					continue
				}
				j := i
				for j < len(t) && t[j] != ' ' && t[j] != '\t' {
					j++
				}
				for j < len(t) && t[j] == ' ' {
					j++
				}
				// разрешаем перенос после дефиса внутри слова
				for k := i; k < j-1; k++ {
					if (t[k] == '-' || t[k] == '/') && k > i && t[k+1] != ' ' {
						j = k + 1
						break
					}
				}
				txt := t[i:j]
				tr := strings.TrimRight(string(txt), " ")
				w := m.Width(string(txt), font)
				wt := w
				if len([]rune(tr)) != len(txt) {
					wt = m.Width(tr, font)
				}
				atoms = append(atoms, Item{K: kText, Text: txt, O: off + i, F: f, Font: font, W: w, WT: wt, SP: len(txt) - len([]rune(tr))})
				i = j
			}
			off += len(r.Text)
		case RImage:
			if floatOf(r) {
				atoms = append(atoms, Item{K: kFloat, O: off, ID: r.ID, H: r.H, FY: r.W, Wrap: r.Wrap, Text: []rune(r.Side)})
			} else {
				atoms = append(atoms, Item{K: kImg, O: off, W: r.W, WT: r.W, H: r.H, ID: r.ID})
			}
			off++
		case RNote:
			f := EffF(d, p, F{})
			f.Sup = true
			f.Color = ""
			font := m.FontOf(f)
			s := ctx.FnNum(r.ID)
			w := m.Width(s, font)
			atoms = append(atoms, Item{K: kNote, Text: []rune(s), O: off, F: f, Font: font, W: w, WT: w, ID: r.ID})
			off++
		case RField:
			f := EffF(d, p, r.F)
			font := m.FontOf(f)
			s := ctx.Field(r.Name)
			w := m.Width(s, font)
			atoms = append(atoms, Item{K: kField, Text: []rune(s), O: off, F: f, Font: font, W: w, WT: w, ID: r.Name})
			off++
		case REq:
			f := EffF(d, p, r.F)
			box := m.EqLayout(string(r.Text), f)
			atoms = append(atoms, Item{K: kEq, O: off, F: f, Font: m.FontOf(f), W: box.W, WT: box.W, Eq: box, Text: r.Text})
			off++
		case RBreak:
			atoms = append(atoms, Item{K: kBreak, O: off})
			off++
		}
	}
	return atoms, off
}

func wordsOf(atoms []Item) []*word {
	var ws []*word
	var prev *Item
	for i := range atoms {
		a := atoms[i]
		inline := a.K == kText || a.K == kNote || a.K == kField || a.K == kEq
		glue := false
		if inline && prev != nil {
			if prev.K == kText {
				glue = prev.SP == 0 && !(prev.Text[len(prev.Text)-1] == '-' || prev.Text[len(prev.Text)-1] == '/')
			} else if prev.K == kNote || prev.K == kField || prev.K == kEq {
				glue = true
			}
			if a.K == kText && a.Text[0] == ' ' {
				glue = false
			}
		}
		if glue {
			ws[len(ws)-1].atoms = append(ws[len(ws)-1].atoms, a)
		} else {
			ws = append(ws, &word{atoms: []Item{a}})
		}
		prev = &atoms[i]
	}
	for _, w := range ws {
		w.calc()
	}
	return ws
}

func (w *word) calc() {
	w.w = 0
	for _, a := range w.atoms {
		w.w += a.W
	}
	last := w.atoms[len(w.atoms)-1]
	w.wt = w.w - last.W + last.WT
}

func (m *Measurer) splitWord(w *word, avail float64) []*word {
	var out []*word
	cur := &word{}
	flush := func() {
		if len(cur.atoms) > 0 {
			out = append(out, cur)
		}
		cur = &word{}
	}
	for _, a := range w.atoms {
		if a.K != kText {
			if cur.w+a.W > avail && len(cur.atoms) > 0 {
				flush()
			}
			cur.atoms = append(cur.atoms, a)
			cur.w += a.W
			cur.wt = cur.w
			continue
		}
		start := 0
		for i := 1; i <= len(a.Text); i++ {
			ww := m.Width(string(a.Text[start:i]), a.Font)
			if cur.w+ww > avail && i-1 > start {
				t := a.Text[start : i-1]
				tw := m.Width(string(t), a.Font)
				cur.atoms = append(cur.atoms, Item{K: kText, Text: t, O: a.O + start, F: a.F, Font: a.Font, W: tw, WT: tw})
				cur.w += tw
				cur.wt = cur.w
				flush()
				start = i - 1
			} else if cur.w+ww > avail && len(cur.atoms) > 0 && i-1 == start {
				flush()
			}
		}
		if start < len(a.Text) {
			t := a.Text[start:]
			tr := strings.TrimRight(string(t), " ")
			tw := m.Width(string(t), a.Font)
			cur.atoms = append(cur.atoms, Item{K: kText, Text: t, O: a.O + start, F: a.F, Font: a.Font, W: tw,
				WT: m.Width(tr, a.Font), SP: len(t) - len([]rune(tr))})
			cur.w += tw
			cur.wt = cur.w - tw + cur.atoms[len(cur.atoms)-1].WT
		}
	}
	flush()
	return out
}

// hyphenate пытается отрезать от слова начало с переносом, чтобы оно влезло в room.
func (m *Measurer) hyphenate(w *word, room float64) (*word, *word) {
	if len(w.atoms) == 0 {
		return nil, nil
	}
	// переносим только последний текстовый атом слова, если всё до него помещается
	li := -1
	for i := len(w.atoms) - 1; i >= 0; i-- {
		if w.atoms[i].K == kText {
			li = i
			break
		}
	}
	if li < 0 {
		return nil, nil
	}
	before := 0.0
	for i := 0; i < li; i++ {
		before += w.atoms[i].W
	}
	a := w.atoms[li]
	core := []rune(strings.TrimRight(string(a.Text), " "))
	// отделяем знаки препинания по краям
	s, e := 0, len(core)
	for s < e && !unicode.IsLetter(core[s]) {
		s++
	}
	for e > s && !unicode.IsLetter(core[e-1]) {
		e--
	}
	if e-s < 5 {
		return nil, nil
	}
	pts := Hyphenate(core[s:e])
	hw := m.Width("-", a.Font)
	for k := len(pts) - 1; k >= 0; k-- {
		cut := s + pts[k]
		pw := m.Width(string(a.Text[:cut]), a.Font)
		if before+pw+hw <= room {
			first := &word{atoms: append(append([]Item{}, w.atoms[:li]...),
				Item{K: kText, Text: a.Text[:cut], O: a.O, F: a.F, Font: a.Font, W: pw + hw, WT: pw + hw, Hyph: true})}
			restT := a.Text[cut:]
			tr := strings.TrimRight(string(restT), " ")
			rw := m.Width(string(restT), a.Font)
			rest := &word{atoms: append([]Item{{K: kText, Text: restT, O: a.O + cut, F: a.F, Font: a.Font, W: rw,
				WT: m.Width(tr, a.Font), SP: len(restT) - len([]rune(tr))}}, w.atoms[li+1:]...)}
			first.calc()
			rest.calc()
			return first, rest
		}
	}
	return nil, nil
}

func nextTab(tabs []TabStop, pos float64) TabStop {
	for _, t := range tabs {
		if t.Pos > pos+0.01 {
			return t
		}
	}
	return TabStop{Pos: (math.Floor((pos+0.01)/TabStep) + 1) * TabStep, Align: "left"}
}

func (m *Measurer) LayoutPara(d *Doc, p *Para, W float64, ctx *LayoutCtx, marker string, ex *Excl) *ParaL {
	pf := EffPF(d, p)
	if ctx != nil && ctx.Cell > 0 {
		pf = CellPF(p, pf)
	}
	if strings.HasPrefix(p.PF.Style, "TOC") && p.PF.Style != "TOCHeading" && len(pf.Tabs) == 0 {
		pf.Tabs = []TabStop{{Pos: W - pf.IndR, Align: "right", Leader: "dot"}}
	}
	if ex == nil {
		ex = &Excl{}
	}
	indL, indF := pf.IndL, pf.IndF
	hasMarker := pf.List != nil
	markF := EffF(d, p, p.Mark)
	for _, r := range p.Runs {
		if r.T == RText {
			markF = EffF(d, p, r.F)
			break
		}
	}
	atoms, total := m.tokenize(d, p, ctx)
	// картинка в строке шире места (узкая колонка, ячейка) уменьшается с сохранением пропорций,
	// иначе она залезала бы на соседнюю колонку или за край листа
	if room := W - pf.IndR - math.Max(0, indL); room > 20 {
		for i := range atoms {
			if a := &atoms[i]; a.K == kImg && a.W > room {
				k := room / a.W
				a.W, a.WT, a.H = room, room, a.H*k
			}
		}
	}
	L := &ParaL{Before: pf.Before, After: pf.After, PB: pf.PB, Keep: pf.Keep}
	// плавающие картинки абзаца
	var floats []Item
	topH := 0.0
	for _, a := range atoms {
		if a.K != kFloat {
			continue
		}
		side := string(a.Text)
		w, h := a.FY, a.H
		it := Item{K: kImg, O: a.O, ID: a.ID, W: w, WT: w, H: h, Wrap: a.Wrap}
		switch side {
		case "right":
			it.X = W - w
		case "center":
			it.X = (W - w) / 2
		}
		switch a.Wrap {
		case "topbottom":
			// отдельная служебная строка над текстом абзаца
			ln := &Line{P: p, Float: true, H: h + floatGap, Base: h, A: a.O, B: a.O}
			ln.Items = []Item{it}
			L.Lines = append(L.Lines, ln)
			topH += h + floatGap
		case "square":
			it.FY = 0
			floats = append(floats, it)
			if side == "right" {
				*ex = Excl{Side: "right", W: w + floatGap, Rem: h + floatGap/2}
			} else {
				if side == "center" {
					it.X = 0
					floats[len(floats)-1].X = 0
				}
				*ex = Excl{Side: "left", W: w + floatGap, Rem: h + floatGap/2}
			}
		default: // behind / front
			floats = append(floats, it)
		}
	}
	words := wordsOf(atoms)
	type cur struct {
		words    []*word
		w        float64
		first    bool
		exL, exR float64
		ext      float64 // на сколько строке разрешено выйти за правый отступ (до позиции табуляции)
	}
	newCur := func(first bool) *cur {
		c := &cur{first: first}
		if ex.Rem > 0.01 {
			if ex.Side == "right" {
				c.exR = ex.W
			} else {
				c.exL = ex.W
			}
		}
		return c
	}
	// Список без висячего отступа («– текст» с красной строки, как принято в отчётах по ГОСТ):
	// маркер стоит на красной строке, текст первой строки идёт от следующей позиции табуляции после него.
	// При висячем отступе (обычный список Word) текст начинается от левого отступа.
	markTextX := indL
	if hasMarker && indF >= 0 {
		mf := markF
		mf.Sup, mf.Sub = false, false
		markTextX = nextTab(pf.Tabs, indL+indF+m.Width(marker, m.FontOf(mf))).Pos
	}
	startX := func(c *cur) float64 {
		x := indL
		if c.first && !hasMarker {
			x = indL + indF
		} else if c.first {
			x = markTextX
		}
		return math.Max(x, c.exL)
	}
	avail := func(c *cur) float64 { return math.Min(W-pf.IndR, W-c.exR) - startX(c) }
	limit := func(c *cur) float64 { return avail(c) + c.ext }
	nextA := 0
	textLines := 0
	finish := func(c *cur, last, hard bool) {
		sx, av := startX(c), avail(c)
		var flat []Item
		for _, w := range c.words {
			flat = append(flat, w.atoms...)
		}
		wt := c.w
		if n := len(flat); n > 0 {
			wt = c.w - flat[n-1].W + flat[n-1].WT
		}
		x, per := sx, 0.0
		switch pf.Align {
		case "right":
			x += av - wt
		case "center":
			x += (av - wt) / 2
		case "justify":
			if !last && !hard {
				n := 0
				for i, a := range flat {
					if a.SP > 0 && i < len(flat)-1 {
						n++
					}
				}
				if n > 0 {
					per = math.Max(0, (av-wt)/float64(n))
				}
			}
		}
		ln := &Line{P: p, X0: sx, Hard: hard || last}
		textX := x
		asc, desc, full := 0.0, 0.0, 0.0
		for i := range flat {
			it := flat[i]
			it.X = x
			x += it.W
			if per > 0 && it.SP > 0 && i < len(flat)-1 {
				x += per
				it.W += per
			}
			if it.F.Ins != "" || it.F.Del != "" {
				ln.Rev = true
			}
			switch it.K {
			case kImg:
				asc = math.Max(asc, it.H)
			case kEq:
				asc = math.Max(asc, it.Eq.Asc)
				desc = math.Max(desc, it.Eq.Desc)
			case kBreak, kTab, kFloat:
			default:
				a, de := m.Metrics(it.Font)
				full = math.Max(full, a+de+m.Leading(it.Font))
				sh := 0.0
				if it.F.Sup {
					sh = it.F.Size * 0.33
				} else if it.F.Sub {
					sh = -it.F.Size * 0.14
				}
				it.Shift = sh
				asc = math.Max(asc, a+math.Max(0, sh))
				desc = math.Max(desc, de+math.Max(0, -sh))
			}
			ln.Items = append(ln.Items, it)
		}
		onlySpace := true
		for _, a := range flat {
			if a.K != kBreak && a.K != kTab && a.K != kFloat {
				onlySpace = false
			}
		}
		if onlySpace {
			a, de := m.Metrics(m.FontOf(markF))
			asc, desc = math.Max(asc, a), math.Max(desc, de)
			full = math.Max(full, a+de+m.Leading(m.FontOf(markF)))
		}
		// gap — внешний интерлиньяж: входит в высоту строки (и множится на интервал), буквы не двигает
		gap := math.Max(0, full-(asc+desc))
		// Word и LibreOffice ведут вёрстку в твипах (1/20 пт): высота строки — целое число твипов.
		// Без округления на 30+ строк набегало до 2 пт, и последняя строка листа уезжала на следующий.
		h := math.Floor((asc+desc+gap)*pf.Line*20+0.5) / 20
		if pf.LineExact > 0 {
			if pf.LineRule == "atLeast" {
				h = math.Max(pf.LineExact, asc+desc+gap)
			} else {
				h = pf.LineExact
			}
		}
		ln.H, ln.Base = h, h-desc-gap
		if pf.LineExact == 0 && pf.Line > 1 {
			// «множитель» больше единицы: добавка идёт под строкой, как в Word и LibreOffice
			// (первая строка листа стоит у самого поля, а не опускается на пол-интервала)
			ln.Base = asc
			ln.Trail = h - (asc + desc + gap)
		}
		if pf.LineExact > 0 && pf.LineRule != "atLeast" {
			ln.Base = asc + (h-asc-desc)*0.5
		}
		if len(flat) > 0 {
			ln.A = flat[0].O
			lastA := flat[len(flat)-1]
			ln.B = lastA.O + lastA.Len()
			if lastA.K == kBreak {
				ln.B = lastA.O
				nextA = lastA.O + 1
			} else {
				nextA = ln.B
			}
		} else {
			ln.A, ln.B = nextA, nextA
		}
		ln.EX = x
		if textLines == 0 && hasMarker {
			mf := markF
			mf.U, mf.DU, mf.S, mf.Bg, mf.Link, mf.Sup, mf.Sub = false, false, false, "", "", false, false
			mf.Ins, mf.Del, mf.Cmt = "", "", ""
			mx := math.Max(indL+indF, c.exL)
			if pf.Align == "center" || pf.Align == "right" {
				// номер «едет» вместе с текстом, как в Word
				gap := -indF
				mw := m.Width(marker, m.FontOf(mf))
				if gap < mw+2 {
					gap = mw + 4
				}
				mx = textX - gap
			}
			ln.Marker = &Marker{Text: marker, X: mx, Font: m.FontOf(mf), F: mf}
		}
		if textLines == 0 && len(floats) > 0 {
			ln.Floats = floats
		}
		textLines++
		if ex.Rem > 0 {
			ex.Rem -= h
		}
		L.Lines = append(L.Lines, ln)
	}
	c := newCur(true)
	for wi := 0; wi < len(words); wi++ {
		w := words[wi]
		a0 := w.atoms[0]
		if a0.K == kBreak {
			c.words = append(c.words, w)
			finish(c, false, true)
			c = newCur(false)
			continue
		}
		if a0.K == kTab {
			pos := startX(c) + c.w
			ts := nextTab(pf.Tabs, pos)
			if ts.Pos > math.Min(W-pf.IndR, W-c.exR)+0.5 && len(c.words) > 0 && ts.Align == "left" {
				finish(c, false, false)
				c = newCur(false)
				pos = startX(c)
				ts = nextTab(pf.Tabs, pos)
			}
			seg := 0.0
			if ts.Align == "right" || ts.Align == "center" {
				for k := wi + 1; k < len(words); k++ {
					k0 := words[k].atoms[0].K
					if k0 == kTab || k0 == kBreak {
						break
					}
					if k+1 < len(words) && words[k+1].atoms[0].K != kTab && words[k+1].atoms[0].K != kBreak {
						seg += words[k].w
					} else {
						seg += words[k].wt
					}
				}
				if ts.Align == "center" {
					seg /= 2
				}
			}
			// позиция табуляции правее правого отступа (оглавление: отступ переносит длинные названия раньше,
			// а номер страницы стоит на самом поле) — текст после неё идёт до позиции, как в Word
			if lim := math.Min(W-pf.IndR, W-c.exR); ts.Pos > lim {
				c.ext = math.Max(c.ext, ts.Pos-lim)
			}
			tw := math.Max(1, ts.Pos-pos-seg)
			w.atoms[0].W, w.atoms[0].WT = tw, tw
			w.atoms[0].Lead = ts.Leader
			w.w, w.wt = tw, tw
			c.words = append(c.words, w)
			c.w += tw
			continue
		}
		if c.w+w.wt > limit(c)+0.01 && len(c.words) > 0 {
			if d.Hyphen && ctx.Cell == 0 {
				if f, rest := m.hyphenate(w, limit(c)-c.w); f != nil {
					c.words = append(c.words, f)
					c.w += f.w
					finish(c, false, false)
					c = newCur(false)
					words[wi] = rest
					wi--
					continue
				}
			}
			finish(c, false, false)
			c = newCur(false)
		}
		if w.wt > limit(c)+0.01 {
			if d.Hyphen && ctx.Cell == 0 {
				if f, rest := m.hyphenate(w, limit(c)-c.w); f != nil {
					c.words = append(c.words, f)
					c.w += f.w
					finish(c, false, false)
					c = newCur(false)
					words[wi] = rest
					wi--
					continue
				}
			}
			for i, pc := range m.splitWord(w, limit(c)) {
				if i > 0 {
					finish(c, false, false)
					c = newCur(false)
				}
				c.words = append(c.words, pc)
				c.w += pc.w
			}
			continue
		}
		c.words = append(c.words, w)
		c.w += w.w
	}
	finish(c, true, false)
	L.Lines[len(L.Lines)-1].B = total
	_ = topH
	return L
}

// ---------------------------------------------------------------- блоки
const (
	uLine = iota
	uRow
)

type CellL struct {
	X, W  float64
	Cell  *Cell
	Units []*Unit
	IH    float64
}

type Unit struct {
	K        int
	Line     *Line
	P        *Para
	H, Y     float64
	SB, SA   float64
	Trail    float64 // см. Line.Trail
	Hdr      bool    // строка шапки таблицы (повторяется на каждом листе)
	ColBreak bool    // перед этим блоком — разрыв колонки
	Rep      bool    // повтор шапки на продолжении таблицы (не сама строка)
	PB, Keep bool
	Fns      []string
	Table    *Table
	Row      *Row
	Cells    []*CellL
	First    bool
	Last     bool
	Shade    string
	PW       float64 // ширина колонки текста (для линий абзаца)
}

// VOff — сдвиг содержимого ячейки по вертикали (выравнивание по центру / низу).
func (c *CellL) VOff(u *Unit) float64 {
	if c.Cell.VAlign == "" || u.Table == nil {
		return 0
	}
	pt, _, pb, _ := u.Table.CellPads(c.Cell)
	free := u.H - pt - pb - c.IH
	if free <= 0 {
		return 0
	}
	if c.Cell.VAlign == "center" {
		return free / 2
	}
	return free
}

func fnIds(l *Line) []string {
	var ids []string
	for _, it := range l.Items {
		if it.K == kNote {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

func (m *Measurer) LayoutBlocks(d *Doc, blocks []Block, W float64, ctx *LayoutCtx) []*Unit {
	var units []*Unit
	var prevP *Para
	var prevLast *Unit
	ex := &Excl{}
	for _, b := range blocks {
		if b.P != nil {
			p := b.P
			marker := ""
			if p.PF.List != nil {
				marker = ctx.Lists.Marker(p.PF.List)
			}
			if ex.Rem > 0 && prevLast != nil {
				ex.Rem -= prevLast.SA
			}
			sig := strconv.FormatUint(p.ver, 10) + "|" + strconv.FormatFloat(W, 'f', 2, 64) + "|" + marker + "|" + ctx.FieldV
			if d.Hyphen {
				sig += "|h"
			}
			if ex.Rem > 0.01 {
				sig += "|x" + ex.Side + strconv.FormatFloat(ex.W, 'f', 1, 64) + "/" + strconv.FormatFloat(ex.Rem, 'f', 1, 64)
			}
			for _, r := range p.Runs {
				if r.T == RNote {
					sig += "|" + ctx.FnNum(r.ID)
				} else if r.T == RField && (strings.HasPrefix(r.Name, "PAGEREF") || strings.HasPrefix(r.Name, "CITE") || strings.HasPrefix(r.Name, "SEQ") || strings.HasPrefix(r.Name, "REF")) {
					sig += "|" + ctx.Field(r.Name)
				}
			}
			var L *ParaL
			if ctx.Cache != nil {
				L = ctx.Cache[p]
			}
			if L == nil || L.sig != sig {
				exIn := *ex
				L = m.LayoutPara(d, p, W, ctx, marker, &exIn)
				L.exOut = exIn
				L.sig = sig
				if ctx.Cache != nil {
					ctx.Cache[p] = L
				}
			}
			before := L.Before
			*ex = L.exOut
			same := CSKey(p) != "" && prevP != nil && CSKey(prevP) == CSKey(p)
			if same && prevLast != nil {
				prevLast.SA = 0
			}
			first := true
			for i, ln := range L.Lines {
				u := &Unit{K: uLine, Line: ln, P: p, H: ln.H, Fns: fnIds(ln), Shade: p.PF.Shade, First: i == 0, Last: i == len(L.Lines)-1, PW: W, Trail: ln.Trail}
				if first && !same {
					u.SB = before
				}
				if i == 0 && p.PF.CB {
					u.ColBreak = true
				}
				if !ln.Float {
					first = false
				}
				if i == len(L.Lines)-1 {
					u.SA = L.After
					u.Keep = L.Keep
				}
				if ln.Float {
					u.Keep = true
				}
				u.PB = i == 0 && L.PB
				units = append(units, u)
			}
			prevP = p
			prevLast = units[len(units)-1]
		} else if b.T != nil {
			tu := m.layoutTable(d, b.T, W, ctx)
			if ex.Rem > 0 && len(tu) > 0 {
				tu[0].SB += ex.Rem
				*ex = Excl{}
			}
			units = append(units, tu...)
			prevP, prevLast = nil, nil
		}
	}
	return units
}

func stackUnits(us []*Unit) float64 {
	y := 0.0
	for _, u := range us {
		u.Y = y + u.SB
		y += u.SB + u.H + u.SA
	}
	return y
}

func TableCols(t *Table, W float64) []float64 {
	n := len(t.Cols)
	if n == 0 {
		for _, r := range t.Rows {
			s := 0
			for _, c := range r.Cells {
				s += max(1, c.Span)
			}
			n = max(n, s)
		}
		n = max(n, 1)
	}
	cols := make([]float64, n)
	if len(t.Cols) == n {
		copy(cols, t.Cols)
	} else {
		for i := range cols {
			cols[i] = W / float64(n)
		}
	}
	sum := 0.0
	for _, c := range cols {
		sum += c
	}
	if sum > W+0.5 || sum <= 0 {
		for i := range cols {
			cols[i] = cols[i] * W / math.Max(sum, 1)
		}
	}
	return cols
}

func (m *Measurer) layoutTable(d *Doc, t *Table, W float64, ctx *LayoutCtx) []*Unit {
	cols := TableCols(t, W)
	tw := 0.0
	for _, c := range cols {
		tw += c
	}
	x0 := t.Ind
	switch t.Align {
	case "center":
		x0 = (W - tw) / 2
	case "right":
		x0 = W - tw
	}
	var units []*Unit
	hdr := true // шапка — только идущие подряд с начала строки с признаком «заголовок»
	for ri, row := range t.Rows {
		gi, x, h := 0, x0, 0.0
		hdr = hdr && row.Header
		u := &Unit{K: uRow, Table: t, Row: row, First: ri == 0, Last: ri == len(t.Rows)-1, Hdr: hdr && len(t.Rows) > 1}
		// «не отрывать от следующего» у всех абзацев строки держит её вместе со следующей строкой
		// (обычный способ не дать таблице разорваться); шапку от первой строки тоже не отрываем
		keep, paras := true, 0
		for _, c := range row.Cells {
			for _, b := range c.Blocks {
				if b.P != nil {
					paras++
					keep = keep && b.P.PF.KeepNext
				}
			}
		}
		u.Keep = (keep && paras > 0) || u.Hdr
		for _, c := range row.Cells {
			span := max(1, c.Span)
			w := 0.0
			for k := gi; k < gi+span && k < len(cols); k++ {
				w += cols[k]
			}
			if w <= 0 {
				w = 20
			}
			pt, pl, pb, pr := t.CellPads(c)
			ctx.Cell++
			inner := m.LayoutBlocks(d, c.Blocks, math.Max(10, w-pl-pr), ctx)
			ctx.Cell--
			ih := stackUnits(inner)
			u.Cells = append(u.Cells, &CellL{X: x, W: w, Cell: c, Units: inner, IH: ih})
			if c.VMerge != "continue" {
				h = math.Max(h, ih+pt+pb)
			}
			x += w
			gi += span
			for _, iu := range inner {
				u.Fns = append(u.Fns, iu.Fns...)
			}
		}
		switch {
		case row.H > 0 && row.HRule == "exact":
			h = row.H
		case row.H > 0:
			h = math.Max(h, row.H)
		}
		u.H = math.Max(h, 8)
		units = append(units, u)
	}
	return units
}

// ---------------------------------------------------------------- страницы
type PU struct {
	U    *Unit
	Y    float64
	X, W float64 // смещение колонки от левого поля и её ширина (одна колонка: 0 и ширина текста)
}

// ColBand — полоса листа, свёрстанная в несколько колонок (для линии между колонками).
type ColBand struct {
	Top, Bottom float64
	Cols        Columns
	W           float64 // ширина колонки
}

type StoryL struct {
	Units []*Unit
	H     float64
}

// Geo — геометрия страницы (у разделов может отличаться).
type Geo struct {
	Page                    Page
	W, BodyTop, BodyH, Left float64
}

type PageL struct {
	Units          []PU
	Bands          []ColBand
	cols           Columns // колонки текущего раздела
	col            int     // текущая колонка
	colTop         float64 // откуда начинаются колонки текущего раздела на этом листе
	inCol          int     // сколько блоков уже стоит в текущей колонке
	maxY           float64 // низ самой длинной колонки
	colW           float64
	Fns            []string
	y              float64
	N              int // порядковый номер листа
	Num            int // номер страницы, который печатается
	Header, Footer *StoryL
	HStory, FStory string
	FnH            float64
	G              *Geo
	Sec            int
	Blank          bool
}

type Layout struct {
	Pages                   []*PageL
	W, BodyTop, BodyH, Left float64
	Page                    Page
	FnLay                   map[string]*StoryL
	Numbers                 map[string]int
	Order                   []string
}

func hasRefFields(d *Doc) bool {
	for _, e := range Flatten(&d.Body, nil, nil) {
		for _, r := range e.P.Runs {
			if r.T == RField && strings.HasPrefix(r.Name, "PAGEREF") {
				return true
			}
		}
	}
	return false
}

// CiteOrder — номера источников в порядке первой ссылки в тексте.
func CiteOrder(d *Doc) map[string]int {
	nums := map[string]int{}
	for _, e := range Flatten(&d.Body, nil, nil) {
		for _, r := range e.P.Runs {
			if r.T == RField && strings.HasPrefix(r.Name, "CITE ") {
				for _, id := range strings.Split(strings.Fields(r.Name + " x")[1], ",") {
					if _, ok := nums[id]; !ok {
						nums[id] = len(nums) + 1
					}
				}
			}
		}
	}
	return nums
}

func (d *Doc) FieldValue(name, page, total string) string {
	switch {
	case name == "PAGE":
		return page
	case name == "NUMPAGES":
		return total
	case strings.HasPrefix(name, "PAGEREF "):
		bm := strings.Fields(name)[1]
		if v, ok := d.refPages[bm]; ok {
			return v
		}
		return "?"
	case strings.HasPrefix(name, "SEQ "):
		if d.seqNums == nil {
			d.computeSeq()
		}
		return d.seqValue(name)
	case strings.HasPrefix(name, "REF "):
		if d.seqNums == nil {
			d.computeSeq()
		}
		return d.refValue(name)
	case strings.HasPrefix(name, "CITE "):
		fs := strings.Fields(name)
		var ns []string
		for _, id := range strings.Split(fs[1], ",") {
			if n, ok := d.citeNums[id]; ok {
				ns = append(ns, strconv.Itoa(n))
			} else {
				ns = append(ns, "?")
			}
		}
		s := "[" + strings.Join(ns, ", ")
		if len(fs) > 2 {
			s += ", с. " + strings.Join(fs[2:], " ")
		}
		return s + "]"
	}
	return ""
}

func (m *Measurer) Paginate(d *Doc, cache map[*Para]*ParaL) *Layout {
	d.citeNums = CiteOrder(d)
	d.computeSeq()
	L := m.paginate(d, cache)
	if hasRefFields(d) {
		for iter := 0; iter < 3; iter++ {
			refs := L.Bookmarks()
			same := len(refs) == len(d.refPages)
			for k, v := range refs {
				if d.refPages[k] != v {
					same = false
				}
			}
			if same {
				break
			}
			d.refPages = refs
			L = m.paginate(d, cache)
		}
	}
	return L
}

// Bookmarks — номер страницы каждой закладки.
func (L *Layout) Bookmarks() map[string]string {
	out := map[string]string{}
	var walk func(us []*Unit, num int)
	walk = func(us []*Unit, num int) {
		for _, u := range us {
			if u.P != nil && u.P.PF.Bookmark != "" {
				if _, ok := out[u.P.PF.Bookmark]; !ok {
					out[u.P.PF.Bookmark] = strconv.Itoa(num)
				}
			}
			for _, c := range u.Cells {
				walk(c.Units, num)
			}
		}
	}
	for _, pg := range L.Pages {
		var us []*Unit
		for _, pu := range pg.Units {
			us = append(us, pu.U)
		}
		walk(us, pg.Num)
	}
	return out
}

func (m *Measurer) paginate(d *Doc, cache map[*Para]*ParaL) *Layout {
	ss := d.Sections()
	order := d.FootnoteOrder()
	numbers := map[string]int{}
	for i, id := range order {
		numbers[id] = i + 1
	}
	lists := NewListState()
	mk := func(page, total string, c map[*Para]*ParaL) *LayoutCtx {
		return &LayoutCtx{Lists: lists, Cache: c, FieldV: page + "/" + total, Doc: d,
			FnNum: func(id string) string {
				if n, ok := numbers[id]; ok {
					return strconv.Itoa(n)
				}
				return "?"
			},
			Field: func(name string) string { return d.FieldValue(name, page, total) }}
	}
	storyL := func(bs []Block, W float64, page, total string) *StoryL {
		if len(bs) == 0 {
			return &StoryL{}
		}
		ctx := mk(page, total, nil)
		ctx.Lists = NewListState()
		us := m.LayoutBlocks(d, bs, W, ctx)
		return &StoryL{Units: us, H: stackUnits(us)}
	}
	hfH := map[string]float64{}
	heightOf := func(name string, bs []Block, W float64) float64 {
		k := name + "@" + strconv.FormatFloat(W, 'f', 1, 64)
		if v, ok := hfH[k]; ok {
			return v
		}
		v := storyL(bs, W, "9", "9").H
		hfH[k] = v
		return v
	}

	fnLay := map[string]*StoryL{}
	fnL := func(id string, W float64) *StoryL {
		if fl, ok := fnLay[id]; ok {
			return fl
		}
		n := d.Footnotes[id]
		if n == nil {
			return nil
		}
		bs := n.Blocks
		ctx := mk("", "", nil)
		ctx.Lists = NewListState()
		f := F{Font: d.Def.Font, Size: 10, Sup: true}
		font := m.FontOf(f)
		num := strconv.Itoa(numbers[id])
		mw := m.Width(num, font) + 2
		var us []*Unit
		if len(bs) > 0 && bs[0].P != nil {
			orig := bs[0].P
			cl := *orig
			cl.PF.IndF = O(orig.PF.IndF.V + mw)
			cl.ver = orig.ver + 1<<40
			first := []Block{{P: &cl}}
			us = m.LayoutBlocks(d, append(first, bs[1:]...), W, ctx)
			for _, u := range us {
				if u.P == &cl {
					u.P = orig
					nl := *u.Line
					nl.P = orig
					u.Line = &nl
				}
			}
			us[0].Line.Marker = &Marker{Text: num, X: 0, Font: font, F: f}
		} else {
			us = m.LayoutBlocks(d, bs, W, ctx)
		}
		h := stackUnits(us)
		fl := &StoryL{Units: us, H: math.Max(h, 10)}
		fnLay[id] = fl
		return fl
	}

	var pages []*PageL
	var page *PageL
	num := 0
	fnHeight := func(ids []string) float64 {
		if len(ids) == 0 {
			return 0
		}
		h := FnSep
		for _, id := range ids {
			if fl := fnLay[id]; fl != nil {
				h += fl.H
			}
		}
		return h
	}
	firstInSec := false
	newPage := func(si int, restart int) {
		sv := ss[si]
		pg := sv.Page
		if restart > 0 {
			num = restart
		} else {
			num++
		}
		W := pg.W - pg.Left - pg.Right
		variant := ""
		if sv.TitlePg && firstInSec {
			variant = "first"
		} else if d.EvenOdd && num%2 == 0 {
			variant = "even"
		}
		hs, hb := d.ResolveHF(ss, si, "header", variant)
		fs, fb := d.ResolveHF(ss, si, "footer", variant)
		bodyTop := pg.Top
		if len(hb) > 0 {
			bodyTop = math.Max(pg.Top, pg.Header+heightOf(hs, hb, W)+4)
		}
		bodyBottom := pg.H - pg.Bottom
		if len(fb) > 0 {
			bodyBottom = math.Min(pg.H-pg.Bottom, pg.H-pg.Footer-heightOf(fs, fb, W)-4)
		}
		g := &Geo{Page: pg, W: W, Left: pg.Left, BodyTop: bodyTop, BodyH: math.Max(72, bodyBottom-bodyTop)}
		page = &PageL{G: g, Sec: si, Num: num, HStory: hs, FStory: fs}
		page.startCols(sv.Cols, W, 0)
		pages = append(pages, page)
		firstInSec = false
	}
	// advance — следующая колонка, а после последней — новый лист
	advance := func(si int) {
		if page.col < page.cols.Count()-1 {
			page.col++
			page.y, page.inCol = page.colTop, 0
			return
		}
		newPage(si, 0)
	}
	contains := func(a []string, s string) bool {
		for _, x := range a {
			if x == s {
				return true
			}
		}
		return false
	}
	hdrOf := map[*Table][]*Unit{}
	var place func(u *Unit)
	place = func(u *Unit) {
		if u.K == uRow && u.Hdr && !u.Rep {
			hdrOf[u.Table] = append(hdrOf[u.Table], u)
		}
		// таблица продолжается на новом листе (или в новой колонке) — сначала повтор шапки
		if u.K == uRow && !u.Hdr && len(hdrOf[u.Table]) > 0 &&
			(page.inCol == 0 || page.Units[len(page.Units)-1].U.Table != u.Table) {
			hh := 0.0
			for _, h := range hdrOf[u.Table] {
				hh += h.H
			}
			if hh+u.H < page.G.BodyH*0.6 { // шапка в пол-листа сама себя не стоит
				for _, h := range hdrOf[u.Table] {
					c := *h
					c.Rep, c.Fns, c.SB, c.SA, c.Keep = true, nil, 0, 0, false
					page.Units = append(page.Units, PU{U: &c, Y: page.y, X: page.colX(), W: page.colW})
					page.inCol++
					page.y += c.H
				}
			}
		}
		sb := 0.0
		if page.inCol > 0 {
			sb = u.SB
		}
		page.Units = append(page.Units, PU{U: u, Y: page.y + sb, X: page.colX(), W: page.colW})
		page.inCol++
		page.y += sb + u.H + u.SA
		page.maxY = math.Max(page.maxY, page.y)
		if n := len(page.Bands); n > 0 {
			page.Bands[n-1].Bottom = math.Max(page.Bands[n-1].Bottom, page.y-u.SA)
		}
		for _, id := range u.Fns {
			if fl := fnL(id, page.G.W); fl != nil && !contains(page.Fns, id) {
				page.Fns = append(page.Fns, id)
			}
		}
	}
	for si, sv := range ss {
		W := sv.Page.W - sv.Page.Left - sv.Page.Right
		units := m.LayoutBlocks(d, d.Body[sv.From:sv.To], sv.Cols.Width(W), mk("", "", cache))
		firstInSec = true
		switch {
		case si == 0:
			newPage(si, sv.Start)
		case sv.Type == "continuous" && page != nil && math.Abs(page.G.Page.W-sv.Page.W) < 0.5 &&
			math.Abs(page.G.Page.H-sv.Page.H) < 0.5 && math.Abs(page.G.W-W) < 0.5:
			firstInSec = false
			if sv.Start > 0 {
				num = sv.Start
				page.Num = num
			}
			// раздел «на текущей странице»: продолжаем под самой длинной колонкой предыдущего
			page.y = page.maxY
			page.startCols(sv.Cols, W, page.y)
		default:
			newPage(si, sv.Start)
			want := map[string]int{"oddPage": 1, "evenPage": 0}
			if w, ok := want[sv.Type]; ok && num%2 != w && sv.Start == 0 {
				// пустая страница для выравнивания по чётности
				page.Blank = true
				firstInSec = true
				newPage(si, 0)
			}
		}
		for _, u := range units {
			if u.PB && len(page.Units) > 0 {
				newPage(si, 0)
			}
			if u.ColBreak && page.inCol > 0 {
				advance(si)
			}
			sb := 0.0
			if page.inCol > 0 {
				sb = u.SB
			}
			ids := append([]string{}, page.Fns...)
			for _, id := range u.Fns {
				if fl := fnL(id, page.G.W); fl != nil && !contains(ids, id) {
					ids = append(ids, id)
				}
			}
			limit := page.G.BodyH - fnHeight(ids)
			if page.y+sb+u.H-u.Trail > limit+0.01 && page.inCol > 0 {
				var carry []*Unit
				// Запрет висячих строк (в Word включён по умолчанию): не оставляем одну первую строку абзаца
				// внизу листа и не переносим одну последнюю наверх следующего — уходят минимум две.
				if u.K == uLine && !u.First && u.P != nil && !u.P.PF.NoWidow {
					k := 0 // сколько строк этого абзаца уже стоит в конце листа
					for i := len(page.Units) - 1; i >= 0 && k < page.inCol && page.Units[i].U.K == uLine && page.Units[i].U.P == u.P; i-- {
						k++
					}
					move := 0
					switch {
					case k == 1: // одинокая первая строка
						move = 1
					case u.Last && k == 2: // 2 + 1: вторую строку забрать нельзя (останется одна) — уходит весь абзац
						move = 2
					case u.Last:
						move = 1
					}
					if move > 0 && move < page.inCol {
						for ; move > 0; move-- {
							carry = append([]*Unit{page.Units[len(page.Units)-1].U}, carry...)
							page.Units = page.Units[:len(page.Units)-1]
							page.inCol--
						}
					}
				}
				// связанные «не отрывать от следующего» уходят вместе — но только если помещаются на новом листе
				// (таблицу длиннее листа всё равно придётся рвать)
				carryH := u.H
				for _, cu := range carry {
					carryH += cu.H + cu.SB + cu.SA
				}
				for page.inCol > 1 && page.Units[len(page.Units)-1].U.Keep && !page.Units[len(page.Units)-1].U.Rep {
					last := page.Units[len(page.Units)-1].U
					if carryH+last.H+last.SB+last.SA > (page.G.BodyH-page.colTop)*0.9 {
						break
					}
					carryH += last.H + last.SB + last.SA
					carry = append([]*Unit{last}, carry...)
					page.Units = page.Units[:len(page.Units)-1]
					page.inCol--
				}
				if len(carry) > 0 {
					last := page.Units[len(page.Units)-1]
					page.y = last.Y + last.U.H + last.U.SA
					var keep []string
					for _, id := range page.Fns {
						for _, pu := range page.Units {
							if contains(pu.U.Fns, id) {
								keep = append(keep, id)
								break
							}
						}
					}
					page.Fns = keep
				}
				advance(si)
				for _, cu := range carry {
					place(cu)
				}
			}
			place(u)
		}
	}
	total := strconv.Itoa(len(pages))
	for i, p := range pages {
		p.N = i + 1
		if !p.Blank {
			sn := strconv.Itoa(p.Num)
			if _, hb := d.ResolveHF(ss, p.Sec, "header", hfVariant(p.HStory)); len(hb) > 0 {
				p.Header = storyL(hb, p.G.W, sn, total)
			}
			if _, fb := d.ResolveHF(ss, p.Sec, "footer", hfVariant(p.FStory)); len(fb) > 0 {
				p.Footer = storyL(fb, p.G.W, sn, total)
			}
		}
		p.FnH = fnHeight(p.Fns)
	}
	g0 := pages[0].G
	return &Layout{Pages: pages, W: g0.W, BodyTop: g0.BodyTop, BodyH: g0.BodyH, Left: g0.Left, Page: g0.Page, FnLay: fnLay, Numbers: numbers, Order: order}
}

func hfVariant(story string) string {
	_, k, ok := strings.Cut(story, "/")
	if !ok {
		k = story
	}
	if _, v, ok := strings.Cut(k, "."); ok {
		return v
	}
	return ""
}

// ---------------------------------------------------------------- размещённые строки (для курсора и щелчков)
type Placed struct {
	Page    int
	Story   string
	Line    *Line
	P       *Para
	X, Y, W float64
}

func walkUnits(us []*Unit, ox, oy, w float64, page int, story string, out []Placed, useY func(*Unit) float64) []Placed {
	for _, u := range us {
		y := oy + useY(u)
		if u.K == uLine {
			out = append(out, Placed{Page: page, Story: story, Line: u.Line, P: u.P, X: ox, Y: y, W: w})
		} else {
			for _, c := range u.Cells {
				t := u.Table
				pt, pl, _, pr := t.CellPads(c.Cell)
				out = walkUnits(c.Units, ox+c.X+pl, y+pt+c.VOff(u), c.W-pl-pr, page, story, out, func(x *Unit) float64 { return x.Y })
			}
		}
	}
	return out
}

func (L *Layout) Place() []Placed {
	var out []Placed
	uy := func(u *Unit) float64 { return u.Y }
	for i, pg := range L.Pages {
		g := pg.G
		if pg.Header != nil {
			out = walkUnits(pg.Header.Units, g.Left, g.Page.Header, g.W, i, pg.HStory, out, uy)
		}
		for _, pu := range pg.Units {
			w := pu.W
			if w <= 0 {
				w = g.W
			}
			y := pu.Y
			out = walkUnits([]*Unit{pu.U}, g.Left+pu.X, g.BodyTop, w, i, "body", out, func(*Unit) float64 { return y })
		}
		y := g.BodyTop + g.BodyH - pg.FnH + FnSep
		for _, id := range pg.Fns {
			fl := L.FnLay[id]
			out = walkUnits(fl.Units, g.Left, y, g.W, i, "fn:"+id, out, uy)
			y += fl.H
		}
		if pg.Footer != nil {
			out = walkUnits(pg.Footer.Units, g.Left, g.Page.H-g.Page.Footer-pg.Footer.H, g.W, i, pg.FStory, out, uy)
		}
	}
	return out
}

func (L *Layout) PageOfPara(p *Para) int {
	var has func(us []*Unit) bool
	has = func(us []*Unit) bool {
		for _, u := range us {
			if u.P == p {
				return true
			}
			for _, c := range u.Cells {
				if has(c.Units) {
					return true
				}
			}
		}
		return false
	}
	for _, pg := range L.Pages {
		for _, pu := range pg.Units {
			if has([]*Unit{pu.U}) {
				return pg.Num
			}
		}
	}
	return 1
}

// PageIndexOfPara — индекс листа (с 0), на котором абзац.
func (L *Layout) PageIndexOfPara(p *Para) int {
	for i, pg := range L.Pages {
		for _, pu := range pg.Units {
			if pu.U.P == p {
				return i
			}
		}
	}
	return 0
}

// startCols — с высоты top лист верстается в cols колонок (новый раздел или новый лист).
func (pg *PageL) startCols(cols Columns, W, top float64) {
	pg.cols, pg.col, pg.colTop, pg.inCol = cols, 0, top, 0
	pg.colW = cols.Width(W)
	if cols.Count() > 1 {
		pg.Bands = append(pg.Bands, ColBand{Top: top, Bottom: top, Cols: cols, W: pg.colW})
	}
}

// colX — смещение текущей колонки от левого поля.
func (pg *PageL) colX() float64 { return float64(pg.col) * (pg.colW + pg.cols.ColGap()) }
