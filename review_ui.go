package main

// Интерфейс рецензирования: орфография, примечания на полях, режим исправлений.

import (
	"math"
	"os"
	"strings"
	"time"
)

type spellEntry struct {
	ver   uint64
	gen   int
	marks [][2]int
}

type cmtRect struct {
	id string
	r  Rect
}

func (a *App) authorName() string {
	if a.Set.Author != "" {
		return a.Set.Author
	}
	for _, k := range []string{"USERNAME", "USER"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "Автор"
}

// spellMarks — неверные слова абзаца (кэш по версии абзаца).
func (a *App) spellMarks(p *Para) [][2]int {
	if a.Set.NoSpell || !spell.Ready() || p == nil {
		return nil
	}
	if a.spellCache == nil || len(a.spellCache) > 20000 {
		a.spellCache = map[*Para]spellEntry{}
	}
	g := spell.Gen()
	ce, ok := a.spellCache[p]
	if !ok || ce.ver != p.ver || ce.gen != g {
		t := p.Runes()
		var marks [][2]int
		for _, w := range SpellWords(p) {
			if !spell.Correct(string(t[w[0]:w[1]])) {
				marks = append(marks, w)
			}
		}
		ce = spellEntry{ver: p.ver, gen: g, marks: marks}
		a.spellCache[p] = ce
	}
	// слово, которое сейчас набирают, не подчёркиваем
	if cp := a.E.Sel.F; cp.P == p && a.E.Collapsed() && len(ce.marks) > 0 {
		var out [][2]int
		for _, m := range ce.marks {
			if cp.O >= m[0] && cp.O <= m[1] && a.E.lastKind == "type" {
				continue
			}
			out = append(out, m)
		}
		return out
	}
	return ce.marks
}

// misspelledAt — неверное слово под позицией.
func (a *App) misspelledAt(pos Pos) (int, int, bool) {
	for _, m := range a.spellMarks(pos.P) {
		if pos.O >= m[0] && pos.O <= m[1] {
			return m[0], m[1], true
		}
	}
	return 0, 0, false
}

// spellItems — пункты контекстного меню для слова с ошибкой.
func (a *App) spellItems(pos Pos) []PopItem {
	s, en, ok := a.misspelledAt(pos)
	if !ok {
		return nil
	}
	e := a.E
	p := pos.P
	word := string(p.Runes()[s:en])
	var items []PopItem
	for _, sg := range spell.Suggest(word, 6) {
		sg := sg
		items = append(items, PopItem{Label: sg, Icon: "spell", Act: func() {
			e.Sel = Sel{Pos{P: p, O: s}, Pos{P: p, O: en}}
			e.InsertText(sg)
			a.after()
		}})
	}
	if len(items) == 0 {
		items = append(items, PopItem{Label: a.tr("Нет вариантов"), Icon: "info"})
	}
	items = append(items,
		PopItem{Label: a.tr("Пропустить всё"), Icon: "next", Act: func() { spell.Ignore(word); a.invalidate() }},
		PopItem{Label: a.tr("Добавить в словарь"), Icon: "plus", Act: func() { spell.AddUser(word); a.invalidate() }},
		PopItem{Sep: true})
	return items
}

// nextSpellError — F7: найти следующую ошибку от курсора и показать варианты.
func (a *App) nextSpellError() {
	if !spell.Ready() {
		a.msg(a.tr("Словари ещё загружаются…"))
		return
	}
	e := a.E
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	_, from := e.Range()
	start := e.fidx[from.P]
	n := len(e.flat)
	for k := 0; k <= n; k++ {
		i := (start + k) % n
		p := e.flat[i].P
		for _, m := range a.spellMarks(p) {
			if k == 0 && m[0] < from.O {
				continue
			}
			e.Sel = Sel{Pos{P: p, O: m[0]}, Pos{P: p, O: m[1]}}
			a.ScrollToCaret()
			a.after()
			if c := e.Caret(Pos{P: p, O: m[0]}); c != nil {
				k := a.k()
				x := a.pageLeftOf(c.Page) + c.X*k
				y := a.pageTop(c.Page) + (c.Y+c.H)*k + a.d(4)
				pos := Pos{P: p, O: m[0]}
				items := a.spellItems(pos)
				items = append(items, PopItem{Label: a.tr("Следующая ошибка"), Icon: "next", Right: "F7", Act: func() {
					e.Sel = Sel{Pos{P: p, O: m[1]}, Pos{P: p, O: m[1]}}
					a.nextSpellError()
				}})
				a.popup = a.listPopup("spell", x, y, a.d(250), items, a.d(420), false)
			}
			return
		}
	}
	a.msg(a.tr("Ошибок не найдено"))
}

// ---------------------------------------------------------------- примечания на полях
func shortDate(iso string) string {
	t, err := time.Parse("2006-01-02T15:04:05Z", iso)
	if err != nil {
		return ""
	}
	return t.Local().Format("02.01.2006 15:04")
}

func (a *App) paintComments(cv *Canvas) {
	a.cmtRects = nil
	cw := a.cmtW()
	if cw == 0 {
		return
	}
	e := a.E
	th := a.th
	k := a.k()
	// якоря: первая строка с примечанием
	type anchor struct {
		id      string
		page    int
		y, x    float64
		started bool
	}
	seen := map[string]bool{}
	var list []anchor
	for _, pl := range e.Placed {
		if pl.Story != "body" {
			continue
		}
		for _, it := range pl.Line.Items {
			if it.F.Cmt != "" && !seen[it.F.Cmt] && e.Doc.Comments[it.F.Cmt] != nil {
				seen[it.F.Cmt] = true
				list = append(list, anchor{id: it.F.Cmt, page: pl.Page, y: pl.Y, x: pl.X + it.X})
			}
		}
	}
	lastBottom := -1e9
	lastPage := -1
	for _, an := range list {
		c := e.Doc.Comments[an.id]
		pw, _ := a.pageSize(an.page)
		left := a.pageLeftOf(an.page)
		top := a.pageTop(an.page)
		x := left + pw + a.d(14)
		w := cw - a.d(24)
		y := top + an.y*k
		if an.page == lastPage && y < lastBottom+a.d(8) {
			y = lastBottom + a.d(8)
		}
		f := a.uiFont(12)
		lines := a.wrapText(c.Text, w-a.d(20), f)
		if len(lines) > 8 {
			lines = append(lines[:8], "…")
		}
		h := a.d(30) + float64(len(lines))*a.d(17) + a.d(8)
		lastBottom, lastPage = y+h, an.page
		if y > a.docRect.Y+a.docRect.H || y+h < a.docRect.Y {
			continue
		}
		r := Rect{x, y, w, h}
		active := e.Doc.activeCmt == an.id
		// линия к тексту
		ay := top + an.y*k + a.d(6)
		lc := Hex(0xd4a72c)
		if active {
			lc = th.Accent
		}
		cv.Line(left+an.x*k, ay, left+pw, ay, math.Max(1, a.S*0.8), lc.WithA(0.7))
		cv.Line(left+pw, ay, x, y+a.d(12), math.Max(1, a.S*0.8), lc.WithA(0.7))
		cv.Shadow(r, a.d(6), a.d(6), RGBA(0, 0, 0, 40))
		bg := Hex(0xfffbe6)
		if c.Done {
			bg = Hex(0xf3f3f3)
		}
		cv.FillRound(r, a.d(6), bg)
		bc := Hex(0xe8d48a)
		if active {
			bc = th.Accent
		}
		cv.StrokeRound(r, a.d(6), math.Max(1, a.S*map[bool]float64{true: 1.6, false: 1}[active]), bc)
		col := Hex(0x2b2b2b)
		if c.Done {
			col = Hex(0x8a8a8a)
		}
		name := c.Author
		if c.Done {
			name += " · " + a.tr("решено")
		}
		a.textAt(cv, name, x+a.d(10), y+a.d(7), Font{Family: a.UIFont, Px: a.d(12), Bold: true}, col)
		if d := shortDate(c.Date); d != "" {
			dw := a.measure(d, a.uiFont(10))
			a.textAt(cv, d, x+w-dw-a.d(10), y+a.d(9), a.uiFont(10), Hex(0x8a8a8a))
		}
		ly := y + a.d(27)
		for _, l := range lines {
			a.textAt(cv, l, x+a.d(10), ly, f, col)
			ly += a.d(17)
		}
		id := an.id
		a.cmtRects = append(a.cmtRects, cmtRect{id: id, r: r})
		a.addHit("cmt:"+id, r, 0, func() { a.commentMenu(id, r) })
	}
}

func (a *App) commentMenu(id string, r Rect) {
	e := a.E
	c := e.Doc.Comments[id]
	if c == nil {
		return
	}
	e.SelectComment(id)
	items := []PopItem{
		{Label: a.tr("Изменить примечание…"), Icon: "comment", Act: func() { a.commentDialog(id) }},
		{Label: map[bool]string{false: a.tr("Отметить как решённое"), true: a.tr("Вернуть в работу")}[c.Done], Icon: "check", Act: func() {
			e.Checkpoint("")
			c.Done = !c.Done
			e.Changed()
			a.after()
		}},
		{Label: a.tr("Удалить примечание"), Icon: "comment-", Act: func() { e.DeleteComment(id); a.after() }},
	}
	a.popup = a.listPopup("cmtmenu", r.X, r.Y+r.H+a.d(2), a.d(240), items, a.d(300), false)
	a.after()
}

func (a *App) commentDialog(id string) {
	e := a.E
	text := ""
	if id != "" {
		if c := e.Doc.Comments[id]; c != nil {
			text = c.Text
		}
	}
	title := "Новое примечание"
	if id != "" {
		title = "Примечание"
	}
	d := &Dialog{Title: title, W: 520, Fields: []*Field{textField("Текст", text),
		{Label: a.trf("Автор: %s (меняется в настройках)", a.authorName()), Kind: "info"}}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "OK", Primary: true, Act: func() bool {
		t := strings.TrimSpace(d.Val(0))
		if t == "" {
			d.Info = "Напишите текст примечания"
			return false
		}
		if id == "" {
			if e.AddComment(t, a.authorName()) == "" {
				d.Info = "Поставьте курсор в слово или выделите текст"
				return false
			}
		} else if c := e.Doc.Comments[id]; c != nil {
			e.Checkpoint("")
			c.Text = t
			e.Changed()
		}
		a.after()
		return true
	}}}
	a.openDialog(d)
}

func (a *App) gotoComment(dir int) {
	e := a.E
	order := e.Doc.CommentOrder()
	if len(order) == 0 {
		a.msg(a.tr("Примечаний нет"))
		return
	}
	cur := e.Doc.activeCmt
	idx := -1
	for i, id := range order {
		if id == cur {
			idx = i
		}
	}
	idx += dir
	if idx < 0 {
		idx = len(order) - 1
	}
	if idx >= len(order) {
		idx = 0
	}
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	e.SelectComment(order[idx])
	a.ScrollToCaret()
	a.after()
}
