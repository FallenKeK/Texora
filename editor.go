package main

// Редактор: позиции, выделение, правка, отмена, команды форматирования.

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

type Pos struct {
	P  *Para
	O  int
	Up bool // курсор в конце строки при мягком переносе
}

type Sel struct{ A, F Pos }

type snapshot struct {
	doc   *Doc
	story string
	a, f  [2]int
}

type Editor struct {
	Doc      *Doc
	M        *Measurer
	L        *Layout
	Placed   []Placed
	byPara   map[*Para][]int
	Story    string
	HFPage   int
	flat     []Entry
	fidx     map[*Para]int
	Sel      Sel
	Pending  *F
	GoalX    float64
	phStory  string // колонтитул, в котором при входе поставлен пустой абзац-заглушка
	undo     []snapshot
	redo     []snapshot
	lastKind string
	burst    int
	Dirty    bool
	cache    map[*Para]*ParaL
	bodySel  *Sel
	painter  *F // формат по образцу
	painterP *PF
	slCache  []Placed
	slStory  string
	rev      string // текущая правка (режим исправлений)
	Author   string
}

func NewEditor(m *Measurer) *Editor {
	e := &Editor{M: m, GoalX: math.NaN()}
	e.SetDoc(NewDoc())
	return e
}

func (e *Editor) SetDoc(d *Doc) {
	e.Doc = d
	if len(d.Body) == 0 {
		d.Body = []Block{{P: NewPara()}}
	}
	e.Story = "body"
	e.cache = map[*Para]*ParaL{}
	e.refresh()
	e.Sel = Sel{Pos{P: e.flat[0].P}, Pos{P: e.flat[0].P}}
	e.undo, e.redo = nil, nil
	e.Dirty = false
	e.Pending = nil
	e.bodySel = nil
	e.Relayout()
}

func (e *Editor) blocks() *[]Block {
	b := e.Doc.Story(e.Story)
	if b == nil {
		e.Story = "body"
		b = &e.Doc.Body
	}
	return b
}

func (e *Editor) refresh() {
	arr := e.blocks()
	if len(*arr) == 0 {
		*arr = append(*arr, Block{P: NewPara()})
	}
	e.flat = Flatten(arr, nil, nil)
	if len(e.flat) == 0 {
		*arr = append(*arr, Block{P: NewPara()})
		e.flat = Flatten(arr, nil, nil)
	}
	e.fidx = make(map[*Para]int, len(e.flat))
	for i, en := range e.flat {
		e.fidx[en.P] = i
	}
}

func (e *Editor) entry(p *Para) Entry { return e.flat[e.fidx[p]] }

func (e *Editor) cmp(a, b Pos) int {
	d := e.fidx[a.P] - e.fidx[b.P]
	if d != 0 {
		return d
	}
	return a.O - b.O
}

func (e *Editor) Range() (Pos, Pos) {
	if e.cmp(e.Sel.A, e.Sel.F) <= 0 {
		return e.Sel.A, e.Sel.F
	}
	return e.Sel.F, e.Sel.A
}

func (e *Editor) Collapsed() bool { return e.Sel.A.P == e.Sel.F.P && e.Sel.A.O == e.Sel.F.O }

func (e *Editor) clampPos(p Pos) Pos {
	if _, ok := e.fidx[p.P]; !ok {
		return Pos{P: e.flat[0].P}
	}
	p.O = max(0, min(p.O, p.P.Len()))
	return p
}

func (e *Editor) SetCaret(p Pos, extend bool) {
	p = e.clampPos(p)
	if extend {
		e.Sel.F = p
	} else {
		e.Sel = Sel{p, p}
	}
	e.Pending = nil
}

// ---------------------------------------------------------------- отмена
func hfEmpty(arr []Block) bool {
	for _, b := range arr {
		if b.P == nil || len(b.P.Runs) > 0 {
			return false
		}
	}
	return true
}

func (e *Editor) snap() snapshot {
	s := snapshot{doc: e.Doc.Clone(), story: e.Story}
	// абзац-заглушка, поставленный при входе в пустой колонтитул, в снимок не идёт: после отмены
	// он остался бы в документе пустым колонтитулом (и перекрыл бы колонтитул предыдущего раздела)
	if e.phStory != "" {
		if arr := s.doc.Story(e.phStory); arr != nil && hfEmpty(*arr) {
			*arr = nil
		}
	}
	s.a = [2]int{e.fidx[e.Sel.A.P], e.Sel.A.O}
	s.f = [2]int{e.fidx[e.Sel.F.P], e.Sel.F.O}
	return s
}

func (e *Editor) restore(s snapshot) {
	*e.Doc = *s.doc.Clone()
	e.Story = s.story
	if e.Doc.Story(e.Story) == nil {
		e.Story = "body"
	}
	e.phStory = ""
	if arr := e.Doc.Story(e.Story); IsHF(e.Story) && len(*arr) == 0 {
		*arr = []Block{{P: NewPara()}}
		e.phStory = e.Story
	}
	e.refresh()
	dec := func(x [2]int) Pos {
		en := e.flat[min(x[0], len(e.flat)-1)]
		return Pos{P: en.P, O: min(x[1], en.P.Len())}
	}
	e.Sel = Sel{dec(s.a), dec(s.f)}
}

// Checkpoint сохраняет состояние перед правкой. kind склеивает подряд идущий ввод.
func (e *Editor) Checkpoint(kind string) {
	if kind != "" && kind == e.lastKind && e.burst < 40 {
		e.burst++
		return
	}
	e.undo = append(e.undo, e.snap())
	if len(e.undo) > 200 {
		e.undo = e.undo[1:]
	}
	e.redo = nil
	e.lastKind = kind
	e.burst = 0
}

func (e *Editor) CanUndo() bool { return len(e.undo) > 0 }
func (e *Editor) CanRedo() bool { return len(e.redo) > 0 }

func (e *Editor) Undo() {
	if len(e.undo) == 0 {
		return
	}
	e.redo = append(e.redo, e.snap())
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.restore(s)
	e.lastKind = ""
	e.Changed()
}

func (e *Editor) Redo() {
	if len(e.redo) == 0 {
		return
	}
	e.undo = append(e.undo, e.snap())
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.restore(s)
	e.lastKind = ""
	e.Changed()
}

func (e *Editor) Changed() {
	e.Dirty = true
	e.refresh()
	e.Sel.A, e.Sel.F = e.clampPos(e.Sel.A), e.clampPos(e.Sel.F)
	e.Relayout()
}

// ---------------------------------------------------------------- вёрстка и геометрия
func (e *Editor) Relayout() {
	e.L = e.M.Paginate(e.Doc, e.cache)
	e.Placed = e.L.Place()
	e.byPara = map[*Para][]int{}
	for i, pl := range e.Placed {
		e.byPara[pl.P] = append(e.byPara[pl.P], i)
	}
	e.slCache = nil
}

func (e *Editor) linesOf(p *Para) []Placed {
	var out []Placed
	for _, i := range e.byPara[p] {
		if e.Placed[i].Story == e.Story && !e.Placed[i].Line.Float {
			out = append(out, e.Placed[i])
		}
	}
	if IsHF(e.Story) && len(out) > 0 {
		var on []Placed
		for _, pl := range out {
			if pl.Page == e.HFPage {
				on = append(on, pl)
			}
		}
		if len(on) == 0 {
			pg := out[0].Page
			for _, pl := range out {
				if pl.Page == pg {
					on = append(on, pl)
				}
			}
		}
		out = on
	}
	return out
}

func (e *Editor) LineFor(p Pos) *Placed {
	arr := e.linesOf(p.P)
	if len(arr) == 0 {
		return nil
	}
	var c []int
	for i, pl := range arr {
		if pl.Line.A <= p.O && p.O <= pl.Line.B {
			c = append(c, i)
		}
	}
	if len(c) == 0 {
		if p.O <= arr[0].Line.A {
			return &arr[0]
		}
		return &arr[len(arr)-1]
	}
	if len(c) > 1 {
		if p.Up {
			return &arr[c[0]]
		}
		for _, i := range c {
			if p.O < arr[i].Line.B {
				return &arr[i]
			}
		}
		return &arr[c[len(c)-1]]
	}
	return &arr[c[0]]
}

func (e *Editor) XAt(pl *Placed, o int) float64 {
	ln := pl.Line
	var items []*Item
	for i := range ln.Items {
		if ln.Items[i].K != kBreak {
			items = append(items, &ln.Items[i])
		}
	}
	if len(items) == 0 {
		return pl.X + ln.EX
	}
	for _, it := range items {
		L := it.Len()
		if o >= it.O && o <= it.O+L {
			if o == it.O {
				return pl.X + it.X
			}
			if o == it.O+L {
				return pl.X + it.X + it.W
			}
			return pl.X + it.X + e.M.Width(string(it.Text[:o-it.O]), it.Font)
		}
	}
	last := items[len(items)-1]
	if o <= items[0].O {
		return pl.X + items[0].X
	}
	return pl.X + last.X + last.W
}

func (e *Editor) OffsetAt(pl *Placed, x float64) int {
	ln := pl.Line
	rx := x - pl.X
	var items []*Item
	for i := range ln.Items {
		if ln.Items[i].K != kBreak {
			items = append(items, &ln.Items[i])
		}
	}
	if len(items) == 0 {
		return ln.A
	}
	if rx <= items[0].X {
		return items[0].O
	}
	for _, it := range items {
		if rx < it.X+it.W {
			if it.K != kText {
				if rx < it.X+it.W/2 {
					return it.O
				}
				return it.O + 1
			}
			prev := 0.0
			for k := 1; k <= len(it.Text); k++ {
				w := it.W
				if k < len(it.Text) {
					w = e.M.Width(string(it.Text[:k]), it.Font)
				}
				if rx < it.X+(prev+w)/2 {
					return it.O + k - 1
				}
				prev = w
			}
			return it.O + len(it.Text)
		}
	}
	return ln.B
}

type CaretRect struct {
	Page    int
	X, Y, H float64
	PL      *Placed
}

func (e *Editor) Caret(p Pos) *CaretRect {
	pl := e.LineFor(p)
	if pl == nil {
		return nil
	}
	x := e.XAt(pl, p.O)
	f := e.TypingFormat(p)
	ef := EffF(e.Doc, p.P, f)
	ef.Sup, ef.Sub = false, false
	a, d := e.M.Metrics(e.M.FontOf(ef))
	h := math.Min(pl.Line.H, a+d)
	return &CaretRect{Page: pl.Page, X: x, Y: pl.Y + pl.Line.Base - a, H: h, PL: pl}
}

// SelRects — прямоугольники выделения для строки.
func (e *Editor) SelSpan(pl *Placed, s, en Pos) (float64, float64, bool) {
	idx, ok := e.fidx[pl.P]
	if !ok {
		return 0, 0, false
	}
	si, ei := e.fidx[s.P], e.fidx[en.P]
	if idx < si || idx > ei {
		return 0, 0, false
	}
	a, b := pl.Line.A, pl.Line.B
	if idx == si {
		a = max(s.O, a)
	}
	if idx == ei {
		b = min(en.O, b)
	}
	nl := idx < ei && b == pl.Line.B && pl.Line.Hard
	if a > b || (a == b && !nl) {
		return 0, 0, false
	}
	x1 := e.XAt(pl, a)
	x2 := e.XAt(pl, b)
	if nl {
		x2 += 4
	}
	return x1, math.Max(x2, x1+2), true
}

// ---------------------------------------------------------------- щелчки
type PagePoint struct {
	Page int
	X, Y float64
}

func (e *Editor) ZoneAt(pt PagePoint) string {
	L := e.L
	if pt.Page < 0 || pt.Page >= len(L.Pages) {
		return "body"
	}
	pg := L.Pages[pt.Page]
	g := pg.G
	if pt.Y < g.BodyTop-2 {
		return pg.HStory
	}
	if pt.Y > g.BodyTop+g.BodyH+2 {
		return pg.FStory
	}
	if len(pg.Fns) > 0 && pt.Y > g.BodyTop+g.BodyH-pg.FnH {
		best, bd := "", math.MaxFloat64
		for _, pl := range e.Placed {
			if pl.Page == pt.Page && strings.HasPrefix(pl.Story, "fn:") {
				d := math.Abs(pt.Y - (pl.Y + pl.Line.H/2))
				if d < bd {
					bd, best = d, pl.Story
				}
			}
		}
		if best != "" {
			return best
		}
	}
	return "body"
}

func (e *Editor) Hit(pt PagePoint) (Pos, bool) {
	var best *Placed
	bd := math.MaxFloat64
	for i := range e.Placed {
		pl := &e.Placed[i]
		if pl.Page != pt.Page || pl.Story != e.Story || pl.Line.Float {
			continue
		}
		dy := 0.0
		if pt.Y < pl.Y {
			dy = pl.Y - pt.Y
		} else if pt.Y > pl.Y+pl.Line.H {
			dy = pt.Y - pl.Y - pl.Line.H
		}
		dx := 0.0
		if pt.X < pl.X {
			dx = pl.X - pt.X
		} else if pt.X > pl.X+pl.W {
			dx = pt.X - pl.X - pl.W
		}
		d := dy*10 + dx
		if d < bd {
			bd, best = d, pl
		}
	}
	if best == nil {
		return Pos{}, false
	}
	o := e.OffsetAt(best, pt.X)
	return Pos{P: best.P, O: o, Up: o == best.Line.B && best.Line.B != best.P.Len()}, true
}

// ImageAt — картинка под точкой (для выделения и изменения размера).
func (e *Editor) ImageAt(pt PagePoint) (Pos, *Item, *Placed, bool) {
	for i := range e.Placed {
		pl := &e.Placed[i]
		if pl.Page != pt.Page || pl.Story != e.Story {
			continue
		}
		for k := range pl.Line.Floats {
			it := &pl.Line.Floats[k]
			x, y := pl.X+it.X, pl.Y+it.FY
			if pt.X >= x && pt.X <= x+it.W && pt.Y >= y && pt.Y <= y+it.H {
				return Pos{P: pl.P, O: it.O}, it, pl, true
			}
		}
		for k := range pl.Line.Items {
			it := &pl.Line.Items[k]
			if it.K != kImg {
				continue
			}
			x, y := pl.X+it.X, pl.Y+pl.Line.Base-it.H
			if pt.X >= x && pt.X <= x+it.W && pt.Y >= y && pt.Y <= y+it.H {
				return Pos{P: pl.P, O: it.O}, it, pl, true
			}
		}
	}
	return Pos{}, nil, nil, false
}

func (e *Editor) SetStory(name string, page int) {
	if name == e.Story {
		return
	}
	prev := e.Story
	if e.Doc.Story(name) == nil {
		name = "body"
	}
	// уходим из колонтитула: заглушку, поставленную при входе, убираем, если в неё ничего не написали.
	// Колонтитул, который был пустым и до входа (титульный лист без номера), не трогаем.
	if arr := e.Doc.Story(prev); prev == e.phStory && arr != nil && hfEmpty(*arr) {
		*arr = nil
	}
	e.phStory = ""
	if IsHF(name) && len(*e.Doc.Story(name)) == 0 {
		*e.Doc.Story(name) = []Block{{P: NewPara()}}
		e.phStory = name
	}
	if prev == "body" {
		s := e.Sel
		e.bodySel = &s
	}
	e.Story = name
	e.HFPage = page
	e.refresh()
	if name == "body" && e.bodySel != nil {
		_, okA := e.fidx[e.bodySel.A.P]
		_, okF := e.fidx[e.bodySel.F.P]
		if okA && okF {
			e.Sel = *e.bodySel
		} else {
			e.Sel = Sel{Pos{P: e.flat[0].P}, Pos{P: e.flat[0].P}}
		}
	} else {
		e.Sel = Sel{Pos{P: e.flat[0].P}, Pos{P: e.flat[0].P}}
	}
	e.Sel.A, e.Sel.F = e.clampPos(e.Sel.A), e.clampPos(e.Sel.F)
	e.Relayout()
}

// ---------------------------------------------------------------- навигация
func (e *Editor) prev(p Pos) Pos {
	if p.O > 0 {
		return Pos{P: p.P, O: p.O - 1}
	}
	if i := e.fidx[p.P]; i > 0 {
		q := e.flat[i-1].P
		return Pos{P: q, O: q.Len()}
	}
	return p
}

func (e *Editor) next(p Pos) Pos {
	if p.O < p.P.Len() {
		return Pos{P: p.P, O: p.O + 1}
	}
	if i := e.fidx[p.P]; i < len(e.flat)-1 {
		return Pos{P: e.flat[i+1].P}
	}
	return p
}

func (e *Editor) wordPrev(p Pos) Pos {
	t := p.P.Runes()
	o := p.O
	if o == 0 {
		return e.prev(p)
	}
	for o > 0 && !IsWordRune(t[o-1]) {
		o--
	}
	for o > 0 && IsWordRune(t[o-1]) {
		o--
	}
	return Pos{P: p.P, O: o}
}

func (e *Editor) wordNext(p Pos) Pos {
	t := p.P.Runes()
	o := p.O
	if o >= len(t) {
		return e.next(p)
	}
	for o < len(t) && IsWordRune(t[o]) {
		o++
	}
	for o < len(t) && !IsWordRune(t[o]) {
		o++
	}
	return Pos{P: p.P, O: o}
}

func (e *Editor) MoveH(d int, word, extend bool) {
	e.GoalX = math.NaN()
	if !extend && !e.Collapsed() {
		s, en := e.Range()
		if d < 0 {
			e.SetCaret(s, false)
		} else {
			e.SetCaret(en, false)
		}
		return
	}
	f := e.Sel.F
	var p Pos
	switch {
	case d < 0 && word:
		p = e.wordPrev(f)
	case d < 0:
		p = e.prev(f)
	case word:
		p = e.wordNext(f)
	default:
		p = e.next(f)
	}
	e.SetCaret(p, extend)
}

func (e *Editor) storyLines() []Placed {
	if e.slCache != nil && e.slStory == e.Story {
		return e.slCache
	}
	var out []Placed
	for _, pl := range e.Placed {
		if pl.Story != e.Story {
			continue
		}
		if (IsHF(e.Story)) && pl.Page != e.HFPage {
			continue
		}
		out = append(out, pl)
	}
	sortPlaced(out)
	e.slCache, e.slStory = out, e.Story
	return out
}

func sortPlaced(a []Placed) {
	less := func(x, y Placed) bool {
		if x.Page != y.Page {
			return x.Page < y.Page
		}
		if x.Y != y.Y {
			return x.Y < y.Y
		}
		return x.X < y.X
	}
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && less(a[j], a[j-1]); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func (e *Editor) MoveV(d int, extend bool) {
	c := e.Caret(e.Sel.F)
	if c == nil {
		return
	}
	x := e.GoalX
	if math.IsNaN(x) {
		x = c.X
	}
	key := func(pl Placed) float64 { return float64(pl.Page)*100000 + pl.Y }
	ck := key(*c.PL)
	lines := e.storyLines()
	var cand []Placed
	for _, pl := range lines {
		k := key(pl)
		if (d > 0 && k > ck+0.5) || (d < 0 && k < ck-0.5) {
			cand = append(cand, pl)
		}
	}
	if len(cand) == 0 {
		if d > 0 {
			last := e.flat[len(e.flat)-1].P
			e.SetCaret(Pos{P: last, O: last.Len()}, extend)
		} else {
			e.SetCaret(Pos{P: e.flat[0].P}, extend)
		}
		e.GoalX = x
		return
	}
	rk := key(cand[0])
	for _, pl := range cand {
		if d > 0 {
			rk = math.Min(rk, key(pl))
		} else {
			rk = math.Max(rk, key(pl))
		}
	}
	var target *Placed
	for i := range cand {
		if math.Abs(key(cand[i])-rk) < c.PL.Line.H*0.6 {
			if target == nil {
				target = &cand[i]
			}
			if x >= cand[i].X-1 && x <= cand[i].X+cand[i].W+1 {
				target = &cand[i]
				break
			}
		}
	}
	o := e.OffsetAt(target, x)
	e.SetCaret(Pos{P: target.P, O: o, Up: o == target.Line.B && o != target.P.Len()}, extend)
	e.GoalX = x
}

func (e *Editor) Home(ctrl, extend bool) {
	e.GoalX = math.NaN()
	if ctrl {
		e.SetCaret(Pos{P: e.flat[0].P}, extend)
		return
	}
	pl := e.LineFor(e.Sel.F)
	o := 0
	if pl != nil {
		o = pl.Line.A
	}
	e.SetCaret(Pos{P: e.Sel.F.P, O: o}, extend)
}

func (e *Editor) End(ctrl, extend bool) {
	e.GoalX = math.NaN()
	if ctrl {
		p := e.flat[len(e.flat)-1].P
		e.SetCaret(Pos{P: p, O: p.Len()}, extend)
		return
	}
	pl := e.LineFor(e.Sel.F)
	if pl == nil {
		return
	}
	soft := pl.Line.B < e.Sel.F.P.Len() && !pl.Line.Hard
	e.SetCaret(Pos{P: e.Sel.F.P, O: pl.Line.B, Up: soft}, extend)
}

func (e *Editor) SelectAll() {
	last := e.flat[len(e.flat)-1].P
	e.Sel = Sel{Pos{P: e.flat[0].P}, Pos{P: last, O: last.Len()}}
}

func (e *Editor) SelectWord(p Pos) {
	t := p.P.Runes()
	a, b := p.O, p.O
	isW := func(i int) bool { return i >= 0 && i < len(t) && IsWordRune(t[i]) }
	if isW(a) || isW(a-1) {
		for a > 0 && isW(a-1) {
			a--
		}
		for b < len(t) && isW(b) {
			b++
		}
		for b < len(t) && t[b] == ' ' {
			b++
		}
	} else if b < len(t) {
		b++
	}
	e.Sel = Sel{Pos{P: p.P, O: a}, Pos{P: p.P, O: b}}
}

func (e *Editor) SelectPara(p Pos) { e.Sel = Sel{Pos{P: p.P}, Pos{P: p.P, O: p.P.Len()}} }

// ---------------------------------------------------------------- правка текста
func (e *Editor) deleteSel() bool {
	if e.Collapsed() {
		return false
	}
	if e.Doc.Track {
		e.trackDeleteSel()
		return true
	}
	s, en := e.Range()
	if s.P == en.P {
		s.P.Delete(s.O, en.O)
	} else {
		i0, i1 := e.fidx[s.P], e.fidx[en.P]
		es, ee := e.flat[i0], e.flat[i1]
		s.P.Delete(s.O, s.P.Len())
		en.P.Delete(0, en.O)
		if es.Arr == ee.Arr {
			ai, bi := IndexOfPara(*es.Arr, s.P), IndexOfPara(*es.Arr, en.P)
			RemoveBlocks(es.Arr, ai+1, bi-ai-1)
			RemoveBlocks(es.Arr, ai+1, 1)
			s.P.Runs = append(s.P.Runs, en.P.Runs...)
			s.P.Normalize()
		} else {
			for k := i0 + 1; k < i1; k++ {
				p := e.flat[k].P
				p.Delete(0, p.Len())
			}
		}
	}
	e.refresh()
	e.Sel = Sel{Pos{P: s.P, O: s.O}, Pos{P: s.P, O: s.O}}
	return true
}

func (e *Editor) TypingFormat(p Pos) F {
	var f F
	if e.Pending != nil {
		f = *e.Pending
	} else {
		f = p.P.FmtAt(p.O)
	}
	if f.Link != "" {
		next := p.P.FmtAt(p.O + 1)
		if next.Link != f.Link || p.O >= p.P.Len() {
			f.Link = ""
		}
	}
	f.Del = ""
	f.Ins = ""
	if e.Doc.Track {
		f.Ins = e.trackRev()
	}
	return f
}

func (e *Editor) InsertText(text string) {
	if text == "" {
		return
	}
	kind := ""
	if len([]rune(text)) == 1 && text != " " {
		kind = "type"
	}
	e.Checkpoint(kind)
	e.deleteSel()
	f := e.TypingFormat(e.Sel.F)
	parts := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	for i, part := range parts {
		if i > 0 {
			e.splitHere()
		}
		if part != "" {
			p := e.Sel.F
			o := p.P.Insert(p.O, TextRun(part, f))
			e.Sel = Sel{Pos{P: p.P, O: o}, Pos{P: p.P, O: o}}
		}
	}
	e.Pending = nil
	e.Changed()
}

func (e *Editor) InsertInline(r Run) {
	e.Checkpoint("")
	e.deleteSel()
	p := e.Sel.F
	if r.T == RField {
		r.F = e.TypingFormat(p)
	}
	o := p.P.Insert(p.O, r)
	e.Sel = Sel{Pos{P: p.P, O: o}, Pos{P: p.P, O: o}}
	e.Changed()
}

func (e *Editor) splitHere() {
	p, o := e.Sel.F.P, e.Sel.F.O
	en := e.entry(p)
	keep := p.FmtAt(o)
	tail := p.Delete(o, p.Len())
	np := &Para{PF: p.PF, Runs: tail}
	if p.PF.List != nil {
		l := *p.PF.List
		np.PF.List = &l
	}
	np.PF.PB = false
	if (p.PF.Heading > 0 || p.PF.Style == "Title" || p.PF.Style == "Subtitle") && len(tail) == 0 {
		np.PF.Heading, np.PF.Style = 0, ""
		np.PF.Before, np.PF.After = Opt{}, Opt{}
		keep = F{}
	}
	np.Normalize()
	if len(np.Runs) == 0 {
		np.Mark = keep
	}
	if len(p.Runs) == 0 {
		p.Mark = keep
	}
	i := IndexOfPara(*en.Arr, p)
	InsertBlocks(en.Arr, i+1, Block{P: np})
	e.refresh()
	e.Sel = Sel{Pos{P: np}, Pos{P: np}}
}

func (e *Editor) Enter() {
	e.Checkpoint("")
	e.deleteSel()
	p := e.Sel.F.P
	if p.PF.List != nil && p.Len() == 0 {
		p.PF.List, p.PF.IndL, p.PF.IndF = nil, Opt{}, Opt{}
		p.Touch()
	} else {
		e.splitHere()
	}
	e.Changed()
}

func (e *Editor) Backspace(word bool) {
	if !e.Collapsed() {
		e.Checkpoint("")
		e.deleteSel()
		e.Changed()
		return
	}
	if e.Doc.Track {
		e.trackBackspace(word)
		return
	}
	p, o := e.Sel.F.P, e.Sel.F.O
	if o > 0 {
		e.Checkpoint("del")
		a := o - 1
		if word {
			a = e.wordPrev(Pos{P: p, O: o}).O
		}
		p.Delete(a, o)
		e.Sel = Sel{Pos{P: p, O: a}, Pos{P: p, O: a}}
		e.Changed()
		return
	}
	if p.PF.List != nil {
		e.Checkpoint("")
		p.PF.List, p.PF.IndL, p.PF.IndF = nil, Opt{}, Opt{}
		p.Touch()
		e.Changed()
		return
	}
	if p.PF.PB {
		e.Checkpoint("")
		p.PF.PB = false
		p.Touch()
		e.Changed()
		return
	}
	en := e.entry(p)
	idx := IndexOfPara(*en.Arr, p)
	if idx > 0 {
		prev := (*en.Arr)[idx-1]
		if prev.P != nil {
			e.Checkpoint("")
			L := prev.P.Len()
			if L == 0 && len(prev.P.Runs) == 0 {
				RemoveBlocks(en.Arr, idx-1, 1)
				e.Changed()
				return
			}
			prev.P.Runs = append(prev.P.Runs, p.Runs...)
			prev.P.Normalize()
			RemoveBlocks(en.Arr, idx, 1)
			e.refresh()
			e.Sel = Sel{Pos{P: prev.P, O: L}, Pos{P: prev.P, O: L}}
			e.Changed()
			return
		}
		if prev.T != nil && p.Len() == 0 && idx < len(*en.Arr)-1 {
			e.Checkpoint("")
			RemoveBlocks(en.Arr, idx, 1)
			e.refresh()
		}
	}
	e.SetCaret(e.prev(Pos{P: p, O: o}), false)
}

func (e *Editor) Delete(word bool) {
	if !e.Collapsed() {
		e.Checkpoint("")
		e.deleteSel()
		e.Changed()
		return
	}
	if e.Doc.Track {
		e.trackDelete(word)
		return
	}
	p, o := e.Sel.F.P, e.Sel.F.O
	if o < p.Len() {
		e.Checkpoint("del")
		b := o + 1
		if word {
			b = max(b, e.wordNext(Pos{P: p, O: o}).O)
		}
		p.Delete(o, b)
		e.Changed()
		return
	}
	en := e.entry(p)
	idx := IndexOfPara(*en.Arr, p)
	if idx+1 < len(*en.Arr) && (*en.Arr)[idx+1].P != nil {
		e.Checkpoint("")
		nx := (*en.Arr)[idx+1].P
		p.Runs = append(p.Runs, nx.Runs...)
		p.Normalize()
		RemoveBlocks(en.Arr, idx+1, 1)
		e.Changed()
	}
}

func (e *Editor) Tab(shift bool) {
	en := e.entry(e.Sel.F.P)
	if en.Cell != nil {
		t := en.Cell.Table
		var cells []*Cell
		for _, r := range t.Rows {
			cells = append(cells, r.Cells...)
		}
		i := 0
		for k, c := range cells {
			if c == en.Cell.Cell {
				i = k
			}
		}
		var target *Cell
		if shift && i > 0 {
			target = cells[i-1]
		} else if !shift {
			if i+1 < len(cells) {
				target = cells[i+1]
			} else {
				e.TableOp("row+")
				target = t.Rows[len(t.Rows)-1].Cells[0]
			}
		}
		if target == nil {
			return
		}
		ps := Flatten(&target.Blocks, nil, nil)
		first, last := ps[0].P, ps[len(ps)-1].P
		e.Sel = Sel{Pos{P: first}, Pos{P: last, O: last.Len()}}
		return
	}
	p := e.Sel.F.P
	if p.PF.List != nil && e.Sel.F.O == 0 && e.Collapsed() {
		e.Indent(map[bool]int{true: -1, false: 1}[shift])
		return
	}
	if !shift {
		e.InsertText("\t")
	}
}

// ---------------------------------------------------------------- формат
func (e *Editor) SelParas() []*Para {
	s, en := e.Range()
	var out []*Para
	for i := e.fidx[s.P]; i <= e.fidx[en.P]; i++ {
		out = append(out, e.flat[i].P)
	}
	return out
}

func (e *Editor) CurrentF() F {
	s, _ := e.Range()
	if e.Pending != nil {
		return *e.Pending
	}
	if e.Collapsed() {
		return s.P.FmtAt(s.O)
	}
	return s.P.FmtAt(s.O + 1)
}

func (e *Editor) EffectiveF() F {
	s, _ := e.Range()
	return EffF(e.Doc, s.P, e.CurrentF())
}

func (e *Editor) CurrentPF() EPF { return EffPF(e.Doc, e.Sel.F.P) }

func (e *Editor) ApplyF(fn func(F) F) {
	if e.Collapsed() {
		p := e.Sel.F.P
		if p.Len() == 0 {
			e.Checkpoint("")
			p.Mark = fn(p.Mark)
			p.Touch()
			nf := p.Mark
			e.Pending = &nf
			e.Changed()
			return
		}
		nf := fn(e.CurrentF())
		e.Pending = &nf
		return
	}
	e.Checkpoint("")
	s, en := e.Range()
	for _, p := range e.SelParas() {
		a, b := 0, p.Len()
		if p == s.P {
			a = s.O
		}
		if p == en.P {
			b = en.O
		}
		p.Format(a, b, fn)
	}
	e.Changed()
}

func (e *Editor) Toggle(key string) {
	ef := e.EffectiveF()
	var v bool
	switch key {
	case "b":
		v = !ef.B
	case "i":
		v = !ef.I
	case "u":
		v = !ef.U
	case "du":
		v = !ef.DU
	case "s":
		v = !ef.S
	case "sup":
		v = !ef.Sup
	case "sub":
		v = !ef.Sub
	case "caps":
		v = !ef.Caps
	}
	e.ApplyF(func(f F) F {
		switch key {
		case "b":
			f.B = v
		case "i":
			f.I = v
		case "u":
			f.U, f.DU = v, false
		case "du":
			f.DU, f.U = v, false
		case "s":
			f.S = v
		case "sup":
			f.Sup = v
			if v {
				f.Sub = false
			}
		case "sub":
			f.Sub = v
			if v {
				f.Sup = false
			}
		case "caps":
			f.Caps = v
		}
		return f
	})
}

func (e *Editor) SetFont(name string) { e.ApplyF(func(f F) F { f.Font = name; return f }) }
func (e *Editor) SetSize(v float64)   { e.ApplyF(func(f F) F { f.Size = v; return f }) }
func (e *Editor) SetColor(c string)   { e.ApplyF(func(f F) F { f.Color = c; return f }) }
func (e *Editor) SetBg(c string)      { e.ApplyF(func(f F) F { f.Bg = c; return f }) }
func (e *Editor) ClearFormat() {
	e.ApplyF(func(f F) F { return F{Link: f.Link} })
}

var FontSizes = []float64{8, 9, 10, 10.5, 11, 12, 14, 16, 18, 20, 22, 24, 26, 28, 36, 48, 72}

func (e *Editor) GrowFont(d int) {
	cur := e.EffectiveF().Size
	nv := cur
	if d > 0 {
		nv = cur + 2
		for _, s := range FontSizes {
			if s > cur {
				nv = s
				break
			}
		}
	} else {
		nv = math.Max(1, cur-1)
		for i := len(FontSizes) - 1; i >= 0; i-- {
			if FontSizes[i] < cur {
				nv = FontSizes[i]
				break
			}
		}
	}
	e.SetSize(nv)
}

// ChangeCase: "upper", "lower", "title", "sentence", "toggle".
func (e *Editor) ChangeCase(mode string) {
	if e.Collapsed() {
		e.SelectWord(e.Sel.F)
		if e.Collapsed() {
			return
		}
	}
	e.Checkpoint("")
	s, en := e.Range()
	startSentence := true
	for _, p := range e.SelParas() {
		a, b := 0, p.Len()
		if p == s.P {
			a = s.O
		}
		if p == en.P {
			b = en.O
		}
		pos := 0
		prevLetter := false
		if a > 0 {
			t := p.Runes()
			prevLetter = IsWordRune(t[a-1])
		}
		for i := range p.Runs {
			r := &p.Runs[i]
			if r.T != RText {
				pos++
				continue
			}
			nt := append([]rune{}, r.Text...)
			for k, ch := range nt {
				o := pos + k
				if o >= a && o < b {
					switch mode {
					case "upper":
						nt[k] = unicode.ToUpper(ch)
					case "lower":
						nt[k] = unicode.ToLower(ch)
					case "toggle":
						if unicode.IsUpper(ch) {
							nt[k] = unicode.ToLower(ch)
						} else {
							nt[k] = unicode.ToUpper(ch)
						}
					case "title":
						if !prevLetter {
							nt[k] = unicode.ToUpper(ch)
						} else {
							nt[k] = unicode.ToLower(ch)
						}
					case "sentence":
						if startSentence && unicode.IsLetter(ch) {
							nt[k] = unicode.ToUpper(ch)
							startSentence = false
						} else {
							nt[k] = unicode.ToLower(ch)
						}
						if ch == '.' || ch == '!' || ch == '?' {
							startSentence = true
						}
					}
				}
				prevLetter = IsWordRune(ch)
			}
			r.Text = nt
			pos += len(r.Text)
		}
		p.Touch()
		startSentence = true
	}
	e.Changed()
}

func (e *Editor) SetPF(fn func(PF) PF) {
	e.Checkpoint("")
	for _, p := range e.SelParas() {
		p.PF = fn(p.PF)
		p.Touch()
	}
	e.Changed()
}

func (e *Editor) SetAlign(a string) {
	e.SetPF(func(pf PF) PF {
		if a == "left" {
			pf.Align = ""
		} else {
			pf.Align = a
		}
		return pf
	})
}

func (e *Editor) SetHeading(n int) {
	e.SetPF(func(pf PF) PF {
		pf.Heading, pf.Style = n, ""
		if n > 0 {
			pf.Before, pf.After, pf.List = Opt{}, Opt{}, nil
		}
		return pf
	})
}

func (e *Editor) SetNamedStyle(name string) {
	e.SetPF(func(pf PF) PF {
		pf.Heading, pf.Style = 0, name
		pf.Before, pf.After, pf.Line = Opt{}, Opt{}, Opt{}
		if name != "" {
			pf.List = nil
		}
		return pf
	})
}

func (e *Editor) SetLineSpacing(v float64) {
	e.SetPF(func(pf PF) PF { pf.Line, pf.LineExact, pf.LineRule = O(v), 0, ""; return pf })
}

func (e *Editor) ToggleList(kind string) {
	paras := e.SelParas()
	all := true
	for _, p := range paras {
		if p.PF.List == nil || p.PF.List.Kind != kind {
			all = false
		}
	}
	if all {
		e.SetPF(func(pf PF) PF { pf.List, pf.IndL, pf.IndF = nil, Opt{}, Opt{}; return pf })
		return
	}
	first := e.fidx[paras[0]]
	id := NewID("l")
	if first > 0 {
		if pl := e.flat[first-1].P.PF.List; pl != nil && pl.Kind == kind {
			id = pl.ID
		}
	}
	e.SetPF(func(pf PF) PF {
		lvl := 0
		if pf.List != nil {
			lvl = pf.List.Level
		}
		pf.List = &List{ID: id, Kind: kind, Level: lvl}
		pf.IndL, pf.IndF, pf.Heading, pf.Style = Opt{}, Opt{}, 0, ""
		return pf
	})
}

func (e *Editor) Indent(d int) {
	e.SetPF(func(pf PF) PF {
		if pf.List != nil {
			l := *pf.List
			l.Level = max(0, min(8, l.Level+d))
			l.Fmt, l.Text = "", ""
			pf.List = &l
			return pf
		}
		v := math.Max(0, pf.IndL.V+float64(d)*TabStep)
		if v == 0 {
			pf.IndL = Opt{}
		} else {
			pf.IndL = O(v)
		}
		return pf
	})
}

// ---------------------------------------------------------------- формат по образцу
func (e *Editor) CopyFormat() {
	f := e.CurrentF()
	pf := e.Sel.F.P.PF
	e.painter, e.painterP = &f, &pf
}

func (e *Editor) HasPainter() bool { return e.painter != nil }

func (e *Editor) PasteFormat() {
	if e.painter == nil {
		return
	}
	f, pf := *e.painter, *e.painterP
	e.painter, e.painterP = nil, nil
	if e.Collapsed() {
		e.SelectWord(e.Sel.F)
	}
	e.ApplyF(func(F) F { return f })
	e.SetPF(func(old PF) PF {
		n := pf
		n.List = old.List
		if pf.List != nil {
			n.List = pf.List
		}
		return n
	})
}

// ---------------------------------------------------------------- вставка объектов
func (e *Editor) InsertPageBreak() {
	if e.Story != "body" {
		return
	}
	e.Checkpoint("")
	e.deleteSel()
	p, o := e.Sel.F.P, e.Sel.F.O
	if o == 0 && e.fidx[p] > 0 {
		p.PF.PB = true
		p.Touch()
	} else {
		e.splitHere()
		e.Sel.F.P.PF.PB = true
		e.Sel.F.P.Touch()
	}
	e.Changed()
}

// InsertColumnBreak — следующий текст начинается в новой колонке (в разделе с одной колонкой — на новой странице, как в Word).
func (e *Editor) InsertColumnBreak() {
	if e.Story != "body" {
		return
	}
	e.Checkpoint("")
	e.deleteSel()
	p, o := e.Sel.F.P, e.Sel.F.O
	if !(o == 0 && e.fidx[p] > 0) {
		e.splitHere()
		p = e.Sel.F.P
	}
	if e.CurSection().Cols.Count() > 1 {
		p.PF.CB = true
	} else {
		p.PF.PB = true
	}
	p.Touch()
	e.Changed()
}

func (e *Editor) AddMedia(mime string, data []byte) string {
	id := NewID("m")
	e.Doc.Media[id] = &Media{Mime: mime, Data: data}
	return id
}

func (e *Editor) InsertImage(mime string, data []byte, w, h float64) {
	maxW := e.L.W
	if w > maxW {
		h = h * maxW / w
		w = maxW
	}
	id := e.AddMedia(mime, data)
	e.InsertInline(Run{T: RImage, ID: id, W: w, H: h})
}

func (e *Editor) ResizeImage(p Pos, w, h float64) {
	for i := range p.P.Runs {
		pos := 0
		for k := 0; k < i; k++ {
			pos += p.P.Runs[k].Len()
		}
		if pos == p.O && p.P.Runs[i].T == RImage {
			e.Checkpoint("img")
			p.P.Runs[i].W, p.P.Runs[i].H = math.Max(8, w), math.Max(8, h)
			p.P.Touch()
			e.Changed()
			return
		}
	}
}

func (e *Editor) InsertFootnote() {
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	id := NewID("f")
	fp := NewPara()
	fp.PF.After, fp.PF.Line = O(0), O(1)
	fp.Mark = F{Size: 10}
	e.Doc.Footnotes[id] = &Note{Blocks: []Block{{P: fp}}}
	e.InsertInline(Run{T: RNote, ID: id})
	page := 0
	if c := e.Caret(e.Sel.F); c != nil {
		page = c.Page
	}
	e.SetStory("fn:"+id, page)
}

func (e *Editor) InsertField(name string) {
	if !IsHF(e.Story) {
		page := 0
		if c := e.Caret(e.Sel.F); c != nil {
			page = c.Page
		}
		e.SetStory(e.PageStory(page, "footer"), page)
	}
	e.InsertInline(Run{T: RField, Name: name})
}

func (e *Editor) SetLink(url string) {
	if e.Collapsed() {
		if url == "" {
			return
		}
		e.InsertText(url)
		p, o := e.Sel.F.P, e.Sel.F.O
		e.Checkpoint("")
		n := len([]rune(url))
		p.Format(o-n, o, func(f F) F { f.Link = url; return f })
		e.Changed()
		return
	}
	e.ApplyF(func(f F) F { f.Link = url; return f })
}

func (e *Editor) insertBlocksAtCaret(bs []Block) {
	p, o := e.Sel.F.P, e.Sel.F.O
	en := e.entry(p)
	L := p.Len()
	idx := IndexOfPara(*en.Arr, p)
	switch {
	case L == 0 || o == 0:
		InsertBlocks(en.Arr, idx, bs...)
	case o == L:
		InsertBlocks(en.Arr, idx+1, bs...)
		after := idx + 1 + len(bs)
		if after >= len(*en.Arr) || (*en.Arr)[after].P == nil {
			np := &Para{PF: p.PF}
			np.PF.PB, np.PF.Heading, np.PF.Style, np.PF.List = false, 0, "", nil
			InsertBlocks(en.Arr, after, Block{P: np})
		}
	default:
		e.splitHere()
		idx = IndexOfPara(*en.Arr, p)
		InsertBlocks(en.Arr, idx+1, bs...)
	}
	e.refresh()
}

func (e *Editor) InsertTable(rows, cols int) {
	e.Checkpoint("")
	e.deleteSel()
	t := NewTable(rows, cols, e.L.W)
	e.insertBlocksAtCaret([]Block{{T: t}})
	first := t.Rows[0].Cells[0].Blocks[0].P
	e.Sel = Sel{Pos{P: first}, Pos{P: first}}
	e.Changed()
}

func (e *Editor) TableCtx() *CellCtx { return e.entry(e.Sel.F.P).Cell }

func (e *Editor) TableOp(op string) bool {
	c := e.TableCtx()
	if c == nil {
		return false
	}
	t, ri, ci := c.Table, c.Row, c.Col
	e.Checkpoint("")
	empty := func(span int) *Cell { return &Cell{Blocks: []Block{{P: NewPara()}}, Span: max(1, span)} }
	grid := func(r *Row, k int) int {
		g := 0
		for i := 0; i < k && i < len(r.Cells); i++ {
			g += max(1, r.Cells[i].Span)
		}
		return g
	}
	switch op {
	case "row+", "row-above":
		nr := &Row{}
		for _, x := range t.Rows[ri].Cells {
			nr.Cells = append(nr.Cells, empty(x.Span))
		}
		at := ri + 1
		if op == "row-above" {
			at = ri
		}
		t.Rows = append(t.Rows[:at], append([]*Row{nr}, t.Rows[at:]...)...)
	case "col+", "col-left":
		g := grid(t.Rows[ri], ci)
		if op == "col+" {
			g += max(1, t.Rows[ri].Cells[ci].Span)
		}
		for _, r := range t.Rows {
			acc, k := 0, 0
			for k < len(r.Cells) && acc+max(1, r.Cells[k].Span) <= g {
				acc += max(1, r.Cells[k].Span)
				k++
			}
			if acc == g || k >= len(r.Cells) {
				r.Cells = append(r.Cells[:k], append([]*Cell{empty(1)}, r.Cells[k:]...)...)
			} else {
				r.Cells[k].Span = max(1, r.Cells[k].Span) + 1
			}
		}
		cols := TableCols(t, e.L.W)
		src := min(max(g-1, 0), len(cols)-1)
		if op == "col-left" {
			src = min(g, len(cols)-1)
		}
		w := cols[src] / 2
		cols[src] = w
		ins := g
		t.Cols = append(cols[:ins], append([]float64{w}, cols[ins:]...)...)
	case "row-":
		if len(t.Rows) == 1 {
			e.undo = e.undo[:len(e.undo)-1]
			return e.TableOp("del")
		}
		t.Rows = append(t.Rows[:ri], t.Rows[ri+1:]...)
	case "col-":
		g := grid(t.Rows[ri], ci)
		ncols := grid(t.Rows[0], len(t.Rows[0].Cells))
		if ncols <= 1 {
			e.undo = e.undo[:len(e.undo)-1]
			return e.TableOp("del")
		}
		for _, r := range t.Rows {
			acc := 0
			for k := 0; k < len(r.Cells); k++ {
				sp := max(1, r.Cells[k].Span)
				if g >= acc && g < acc+sp {
					if sp > 1 {
						r.Cells[k].Span = sp - 1
					} else {
						r.Cells = append(r.Cells[:k], r.Cells[k+1:]...)
					}
					break
				}
				acc += sp
			}
		}
		if g < len(t.Cols) {
			t.Cols = append(t.Cols[:g], t.Cols[g+1:]...)
		}
	case "merge":
		r := t.Rows[ri]
		if ci+1 < len(r.Cells) {
			cur, nx := r.Cells[ci], r.Cells[ci+1]
			cur.Span = max(1, cur.Span) + max(1, nx.Span)
			for _, b := range nx.Blocks {
				if b.T != nil || (b.P != nil && len(b.P.Runs) > 0) {
					cur.Blocks = append(cur.Blocks, b)
				}
			}
			r.Cells = append(r.Cells[:ci+1], r.Cells[ci+2:]...)
		}
	case "border":
		t.Border = !t.Border
		t.TB = nil // «все границы» / «без границ» заменяют границы из Word
		for _, r := range t.Rows {
			for _, c := range r.Cells {
				c.B = [4]*Border{}
			}
		}
	case "shade":
		if c.Cell.Shade == "" {
			c.Cell.Shade = "#d9e2f3"
		} else {
			c.Cell.Shade = ""
		}
	case "del":
		i := IndexOfTable(*c.Arr, t)
		RemoveBlocks(c.Arr, i, 1)
		if len(*c.Arr) == 0 || i >= len(*c.Arr) || (*c.Arr)[min(i, len(*c.Arr)-1)].P == nil {
			InsertBlocks(c.Arr, min(i, len(*c.Arr)), Block{P: NewPara()})
		}
		e.refresh()
		target := (*c.Arr)[min(i, len(*c.Arr)-1)].P
		if target == nil {
			target = e.flat[0].P
		}
		e.Sel = Sel{Pos{P: target}, Pos{P: target}}
	}
	e.Changed()
	return true
}

// ---------------------------------------------------------------- поиск
type FindOpts struct{ Case, Whole, Regex bool }

func fold(s []rune, cs bool) []rune {
	if cs {
		return s
	}
	o := make([]rune, len(s))
	for i, r := range s {
		o[i] = unicode.ToLower(r)
	}
	return o
}

func indexRunes(h, n []rune, from int) int {
	for i := from; i+len(n) <= len(h); i++ {
		ok := true
		for k := range n {
			if h[i+k] != n[k] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func wholeOK(t []rune, at, n int) bool {
	if at > 0 && IsWordRune(t[at-1]) {
		return false
	}
	if at+n < len(t) && IsWordRune(t[at+n]) {
		return false
	}
	return true
}

func (e *Editor) Find(q string, o FindOpts) bool {
	if q == "" {
		return false
	}
	if o.Regex {
		_, en := e.Range()
		a, b, ok := e.findRegex(q, o, en)
		if ok {
			e.Sel = Sel{a, b}
		}
		return ok
	}
	needle := fold([]rune(q), o.Case)
	_, en := e.Range()
	start := e.fidx[en.P]
	n := len(e.flat)
	for k := 0; k <= n; k++ {
		p := e.flat[(start+k)%n].P
		t := fold(p.Runes(), o.Case)
		from := 0
		if k == 0 {
			from = en.O
		}
		for {
			at := indexRunes(t, needle, from)
			if at < 0 {
				break
			}
			if !o.Whole || wholeOK(t, at, len(needle)) {
				e.Sel = Sel{Pos{P: p, O: at}, Pos{P: p, O: at + len(needle)}}
				return true
			}
			from = at + 1
		}
	}
	return false
}

func (e *Editor) Replace(q, r string, o FindOpts) bool {
	s, en := e.Range()
	if s.P == en.P && !e.Collapsed() {
		sel := string(s.P.Runes()[s.O:en.O])
		if o.Regex {
			if a, b, ok := e.findRegex(q, o, s); ok && a.P == s.P && a.O == s.O && b.O == en.O {
				e.InsertText(regexReplacement(q, r, sel, o.Case))
			}
			return e.Find(q, o)
		}
		t := fold([]rune(sel), o.Case)
		if string(t) == string(fold([]rune(q), o.Case)) {
			e.InsertText(r)
		}
	}
	return e.Find(q, o)
}

func (e *Editor) ReplaceAll(q, r string, o FindOpts) int {
	if q == "" {
		return 0
	}
	e.Checkpoint("")
	count := 0
	needle := fold([]rune(q), o.Case)
	var re *regexp.Regexp
	if o.Regex {
		pat := uniRegex(q)
		if !o.Case {
			pat = "(?i)" + pat
		}
		var err error
		if re, err = regexp.Compile(pat); err != nil {
			e.undo = e.undo[:len(e.undo)-1]
			return 0
		}
	}
	type hitR struct {
		a, b int
		rep  string
	}
	for _, en := range e.flat {
		p := en.P
		var hits []hitR
		if re != nil {
			src := p.Runes()
			s := string(src)
			for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
				if m[0] == m[1] {
					continue
				}
				a := len([]rune(s[:m[0]]))
				b := a + len([]rune(s[m[0]:m[1]]))
				if o.Whole && !wholeOK(src, a, b-a) {
					continue
				}
				rep := string(re.ExpandString(nil, r, s, m))
				hits = append(hits, hitR{a, b, rep})
			}
		} else {
			t := fold(p.Runes(), o.Case)
			from := 0
			for {
				at := indexRunes(t, needle, from)
				if at < 0 {
					break
				}
				if !o.Whole || wholeOK(t, at, len(needle)) {
					hits = append(hits, hitR{at, at + len(needle), r})
				}
				from = at + len(needle)
			}
		}
		for i := len(hits) - 1; i >= 0; i-- {
			h := hits[i]
			f := p.FmtAt(h.a + 1)
			f.Del = ""
			if e.Doc.Track {
				e.trackDeleteIn(p, h.a, h.b)
				f.Ins = e.trackRev()
			} else {
				p.Delete(h.a, h.b)
			}
			if h.rep != "" {
				p.Insert(h.a, TextRun(h.rep, f))
			}
			count++
		}
	}
	if count > 0 {
		e.Changed()
	} else {
		e.undo = e.undo[:len(e.undo)-1]
	}
	return count
}

// ---------------------------------------------------------------- буфер обмена
func (e *Editor) SelectedBlocks() []Block {
	s, en := e.Range()
	var out []Block
	for _, p := range e.SelParas() {
		a, b := 0, p.Len()
		if p == s.P {
			a = s.O
		}
		if p == en.P {
			b = en.O
		}
		np := &Para{PF: p.PF, Runs: p.Slice(a, b)}
		if p.PF.List != nil {
			l := *p.PF.List
			np.PF.List = &l
		}
		out = append(out, Block{P: np})
	}
	return out
}

func (e *Editor) DeleteSelection() {
	if e.Collapsed() {
		return
	}
	e.Checkpoint("")
	e.deleteSel()
	e.Changed()
}

// PasteBlocks вставляет абзацы/таблицы (media уже добавлены в документ).
func (e *Editor) PasteBlocks(bs []Block) {
	if len(bs) == 0 {
		return
	}
	e.Checkpoint("")
	e.deleteSel()
	p, o := e.Sel.F.P, e.Sel.F.O
	start := Pos{P: p, O: o}
	if len(bs) == 1 && bs[0].P != nil {
		no := p.Insert(o, bs[0].P.Runs...)
		e.Sel = Sel{Pos{P: p, O: no}, Pos{P: p, O: no}}
		if e.Doc.Track {
			e.markIns(start, e.Sel.F)
		}
		e.Changed()
		return
	}
	en := e.entry(p)
	tail := p.Delete(o, p.Len())
	rest := bs
	if bs[0].P != nil {
		wasEmpty := p.Len() == 0
		p.Runs = append(p.Runs, bs[0].P.Runs...)
		if wasEmpty || o == 0 {
			p.PF = bs[0].P.PF
		}
		p.Normalize()
		rest = bs[1:]
	}
	var last *Para
	if n := len(rest); n > 0 && rest[n-1].P != nil {
		last = rest[n-1].P
	} else {
		last = &Para{PF: p.PF}
		last.PF.PB = false
		rest = append(rest, Block{P: last})
	}
	lo := last.Len()
	last.Runs = append(last.Runs, tail...)
	last.Normalize()
	idx := IndexOfPara(*en.Arr, p)
	InsertBlocks(en.Arr, idx+1, rest...)
	e.refresh()
	e.Sel = Sel{Pos{P: last, O: lo}, Pos{P: last, O: lo}}
	if e.Doc.Track {
		e.markIns(start, e.Sel.F)
	}
	e.Changed()
}

// ---------------------------------------------------------------- сводка
type Stats struct {
	Words, Chars, CharsNoSpace, Paras, Pages, Page int
}

// Stats — подсчёт по всему документу. Дорогой на больших работах, поэтому строка состояния
// берёт StatsCached, а здесь считаем честно.
func (e *Editor) Stats() Stats {
	st := e.textStats()
	st.Pages = len(e.L.Pages)
	st.Page = 1
	if c := e.Caret(e.Sel.F); c != nil {
		st.Page = c.Page + 1
	}
	return st
}

// textStats — слова, знаки и абзацы (не зависят от положения курсора и разбивки на страницы).
func (e *Editor) textStats() Stats {
	t := BlocksToText(e.Doc.Body)
	st := Stats{}
	inWord := false
	for _, r := range t {
		if IsWordRune(r) {
			if !inWord {
				st.Words++
			}
			inWord = true
		} else {
			inWord = false
		}
		if r != '\n' {
			st.Chars++
			if !unicode.IsSpace(r) {
				st.CharsNoSpace++
			}
		}
	}
	for _, en := range Flatten(&e.Doc.Body, nil, nil) {
		if en.P.Len() > 0 {
			st.Paras++
		}
	}
	return st
}

// ---------------------------------------------------------------- разделы
// PageStory — история колонтитула kind (header/footer), которая видна на листе page.
func (e *Editor) PageStory(page int, kind string) string {
	L := e.L
	if page < 0 || page >= len(L.Pages) {
		return kind
	}
	pg := L.Pages[page]
	if kind == "header" {
		return pg.HStory
	}
	return pg.FStory
}

// caretSection — индекс раздела, в котором курсор (или колонтитул, который правим).
func (e *Editor) caretSection() int {
	if IsHF(e.Story) {
		if e.HFPage < len(e.L.Pages) {
			return e.L.Pages[e.HFPage].Sec
		}
		return 0
	}
	if e.Story != "body" {
		return 0
	}
	en := e.entry(e.Sel.F.P)
	arr := e.Doc.Body
	top := -1
	if en.Cell == nil {
		top = IndexOfPara(arr, e.Sel.F.P)
	} else {
		top = IndexOfTable(arr, en.Cell.Table)
	}
	return e.Doc.SectionAt(max(0, top)).Index
}

// SetSection меняет свойства раздела, в котором курсор.
func (e *Editor) SetSection(fn func(s *SecView)) {
	ss := e.Doc.Sections()
	sv := ss[min(e.caretSection(), len(ss)-1)]
	fn(&sv)
	if sv.Owner != nil {
		sv.Owner.Page, sv.Owner.TitlePg, sv.Owner.Start, sv.Owner.Type, sv.Owner.Cols = sv.Page, sv.TitlePg, sv.Start, sv.Type, sv.Cols
	} else {
		e.Doc.Page, e.Doc.TitlePg, e.Doc.Start, e.Doc.Type, e.Doc.Cols = sv.Page, sv.TitlePg, sv.Start, sv.Type, sv.Cols
	}
}

func (e *Editor) CurSection() SecView {
	ss := e.Doc.Sections()
	return ss[min(e.caretSection(), len(ss)-1)]
}

// InsertSectionBreak — разрыв раздела после абзаца с курсором. typ: "" (со следующей страницы), continuous, oddPage, evenPage.
func (e *Editor) InsertSectionBreak(typ string) {
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	if e.entry(e.Sel.F.P).Cell != nil {
		return
	}
	e.Checkpoint("")
	e.deleteSel()
	cur := e.CurSection()
	e.splitHere()
	// абзац до курсора закрывает раздел; новый раздел начинается с абзаца с курсором
	idx := IndexOfPara(e.Doc.Body, e.Sel.F.P)
	if idx <= 0 {
		return
	}
	prev := e.Doc.Body[idx-1].P
	ns := &Section{ID: NewID("s"), Page: cur.Page, Cols: cur.Cols, TitlePg: cur.TitlePg, Type: cur.Type, Start: cur.Start, HF: cur.HF.Clone()}
	*cur.HF = HF{}
	if cur.Owner != nil {
		cur.Owner.Start, cur.Owner.Type = 0, typ
	} else {
		e.Doc.Start, e.Doc.Type = 0, typ
	}
	prev.PF.Sect = ns
	prev.Touch()
	e.Changed()
}
