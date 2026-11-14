package main

import (
	"strings"
	"unicode"
)

// captionDialog — «Вставить название» (Рисунок 1.2 – …).
func (a *App) captionDialog() {
	e := a.E
	cats := append([]string(nil), CaptionCats...)
	cats = append(cats, "Своя подпись…")
	def := 0
	if e.TableCtx() != nil {
		def = 1
	}
	d := &Dialog{Title: "Название", W: 560}
	cf := &Field{Label: "Подпись", Kind: "select", Opts: cats, Sel: def}
	own := textField("Своя подпись", "")
	txt := textField("Название", "")
	chap := &Field{Label: "Номер с главой: 1.2, в приложениях — А.1", Kind: "check", On: a.Set.CapChapter}
	d.Fields = []*Field{cf, own, txt, chap,
		{Label: "По ГОСТ 7.32: «Рисунок 1 – Название» под рисунком по центру, «Таблица 1 – Название» над таблицей слева. Номера пересчитываются сами.", Kind: "info"}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "Вставить", Primary: true, Act: func() bool {
		cat := cats[cf.Sel]
		if cf.Sel == len(cats)-1 {
			cat = strings.Join(strings.Fields(d.Val(1)), " ")
			if cat == "" {
				d.Info = "Введите свою подпись, например «Фотография»"
				return false
			}
		}
		a.Set.CapChapter = chap.On
		a.saveSettings()
		e.InsertCaption(cat, d.Val(2), chap.On)
		a.after()
		return true
	}}}
	a.openDialog(d)
	d.focus = 2
}

var refModes = []struct{ id, label string }{
	{"l", "Подпись и номер — «Рисунок 1.2»"},
	{"n", "Только номер — «1.2»"},
	{"s", "Кратко — «рис. 1.2», «табл. 1.2»"},
	{"p", "В скобках — «(1.2)» (для формул)"},
}

// crossRefDialog — «Перекрёстная ссылка» на название.
func (a *App) crossRefDialog() {
	e := a.E
	all := Captions(e.Doc, "")
	if len(all) == 0 {
		a.errorBox(a.tr("В документе пока нет названий с номерами.\nВставьте их: инспектор → Ссылки → «Вставить название» или Ctrl+K «название» (или формулу с номером)."))
		return
	}
	var cats []string
	seen := map[string]bool{}
	for _, c := range all {
		if !seen[c.Cat] {
			seen[c.Cat] = true
			cats = append(cats, c.Cat)
		}
	}
	d := &Dialog{Title: "Перекрёстная ссылка", W: 640}
	tf := &Field{Label: "Тип", Kind: "select", Opts: cats}
	lst := &Field{Label: "На что", Kind: "select"}
	var cur []Caption
	fill := func(i int) {
		cur = Captions(e.Doc, cats[i])
		lst.Opts = nil
		for _, c := range cur {
			t := c.Text
			if c.Cat == "Формула" {
				t = "(" + c.Num + ")   " + strings.TrimSpace(strings.TrimSuffix(t, "("+c.Num+")"))
			}
			t = strings.Map(func(r rune) rune {
				if unicode.Is(unicode.Mn, r) {
					return -1
				}
				return r
			}, t)
			if rs := []rune(t); len(rs) > 70 {
				t = string(rs[:68]) + "…"
			}
			lst.Opts = append(lst.Opts, t)
		}
		lst.Sel = 0
	}
	fill(0)
	var modeNames []string
	for _, m := range refModes {
		modeNames = append(modeNames, m.label)
	}
	mf := &Field{Label: "Вставить", Kind: "select", Opts: modeNames}
	tf.OnSel = func(i int) {
		fill(i)
		if cats[i] == "Формула" {
			mf.Sel = 3
		} else {
			mf.Sel = 0
		}
	}
	if cats[0] == "Формула" {
		mf.Sel = 3
	}
	d.Fields = []*Field{tf, lst, mf, {Label: "Ссылка обновится сама, если номер изменится. Ctrl+щелчок — перейти к названию.", Kind: "info"}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "Вставить", Primary: true, Act: func() bool {
		if lst.Sel < 0 || lst.Sel >= len(cur) {
			return false
		}
		c := cur[lst.Sel]
		e.Checkpoint("")
		if c.P.PF.Bookmark == "" {
			c.P.PF.Bookmark = newRefBM()
			c.P.Touch()
		}
		e.Doc.seqNums = nil
		e.InsertCrossRef(c.P.PF.Bookmark, refModes[mf.Sel].id)
		a.after()
		return true
	}}}
	a.openDialog(d)
}

// lofPopup — «Перечень иллюстраций»: выбрать категорию.
func (a *App) lofPopup(r Rect) {
	e := a.E
	seen := map[string]bool{}
	var items []PopItem
	add := func(cat string) {
		if seen[cat] {
			return
		}
		seen[cat] = true
		items = append(items, PopItem{Label: a.tr(map[string]string{"Рисунок": "Перечень рисунков", "Таблица": "Перечень таблиц", "Формула": "Перечень формул"}[cat]) + map[bool]string{true: "", false: "Перечень: " + cat}[cat == "Рисунок" || cat == "Таблица" || cat == "Формула"],
			Icon: "toc", Act: func() { e.InsertLOF(cat); a.after() }})
	}
	for _, c := range Captions(e.Doc, "") {
		add(c.Cat)
	}
	add("Рисунок")
	add("Таблица")
	a.popup = a.listPopup("lof", r.X, r.Y+r.H+a.d(4), a.d(300), items, a.d(300), false)
}
