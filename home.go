package main

// Стартовый экран Texora: меню слева, главная, недавние, избранное, шаблоны, настройки, о программе.

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

type tplPreview struct {
	d *Doc
	L *Layout
}

func (a *App) tplPreview(id string) *tplPreview {
	if p, ok := a.tplCache[id]; ok {
		return p
	}
	d := a.templateDoc(id)
	e := NewEditor(a.M)
	e.SetDoc(d)
	p := &tplPreview{d: d, L: e.L}
	a.tplCache[id] = p
	return p
}

func (a *App) newFromTemplate(id string) {
	var name string
	for _, t := range templatesList {
		if t.id == id && id != "blank" {
			name = t.name + ".docx"
		}
	}
	d := a.templateDoc(id)
	if name != "" {
		// имя без номера, но не повторяем уже открытые
		base := strings.TrimSuffix(name, ".docx")
		n := 1
		for a.nameTaken(name) {
			n++
			name = fmt.Sprintf("%s %d.docx", base, n)
		}
	}
	a.useDoc(d, name, "")
	a.tab = "format"
}

func (a *App) nameTaken(n string) bool {
	a.stash()
	for _, t := range a.tabs {
		if strings.EqualFold(t.FileName, n) {
			return true
		}
	}
	return false
}

type homeItem struct {
	id, icon, label string
	act             func()
}

func (a *App) paintHome(cv *Canvas, r Rect) {
	th := a.th
	a.addHit("bs-bg", r, 0, nil)
	menuW := a.d(232)
	cv.FillRect(Rect{r.X, r.Y, a.W, math.Max(1, a.S)}, th.Divider)
	menu := Rect{r.X, r.Y + math.Max(1, a.S), menuW, r.H - math.Max(1, a.S)}
	cv.FillRect(menu, th.Nav)
	cv.FillRect(Rect{menu.X + menu.W - math.Max(1, a.S), menu.Y, math.Max(1, a.S), menu.H}, th.Divider)
	y := menu.Y + a.d(12)
	item := func(it homeItem) {
		ir := Rect{menu.X + a.d(10), y, menu.W - a.d(20), a.d(32)}
		id := "bs:" + it.id
		a.hits = append(a.hits, hit{id: id, r: ir, act: it.act})
		on := a.bsPane == it.id
		if on {
			cv.FillRound(ir, a.d(5), th.Hover)
			cv.FillRound(Rect{ir.X, ir.Y + a.d(8), a.d(2.5), ir.H - a.d(16)}, a.d(1), th.Accent)
		} else {
			a.hoverBg(cv, id, ir, a.d(5))
		}
		col := th.TextDim
		if on {
			col = th.Text
		}
		a.icon(cv, it.icon, Rect{ir.X + a.d(10), ir.Y + a.d(6), a.d(20), a.d(20)}, mapCol(on, th.Accent2, col))
		a.textAt(cv, it.label, ir.X+a.d(40), ir.Y+(ir.H-a.d(17))/2, a.uiFont(13), col)
		y += a.d(34)
	}
	sep := func() {
		cv.FillRect(Rect{menu.X + a.d(16), y + a.d(6), menu.W - a.d(32), math.Max(1, a.S)}, th.Divider)
		y += a.d(14)
	}
	if a.bsFromDoc {
		item(homeItem{"back", "back", "К документу", func() { a.backstage = false }})
		sep()
	}
	pane := func(p string) func() { return func() { a.bsPane, a.bsScroll = p, 0 } }
	item(homeItem{"home", "home", "Главная", pane("home")})
	item(homeItem{"open", "open", "Открыть", func() { a.Command("open") }})
	item(homeItem{"recent", "recent", "Недавние", pane("recent")})
	item(homeItem{"favorites", "star", "Избранное", pane("favorites")})
	item(homeItem{"templates", "template", "Шаблоны", pane("templates")})
	if a.bsFromDoc {
		sep()
		item(homeItem{"save", "save", "Сохранить", func() { a.backstage = false; a.Command("save") }})
		item(homeItem{"saveas", "copy", "Сохранить как", func() { a.backstage = false; a.Command("saveas") }})
		item(homeItem{"print", "print", "Печать", func() { a.backstage = false; a.Command("print") }})
		item(homeItem{"pdf", "pdf", "Экспорт в PDF", func() { a.backstage = false; a.Command("pdf") }})
		item(homeItem{"closedoc", "x", "Закрыть документ", func() { a.closeTab(a.cur) }})
	}
	y = math.Max(y+a.d(10), menu.Y+menu.H-a.d(124))
	sep()
	item(homeItem{"settings", "gear", "Настройки", pane("settings")})
	item(homeItem{"about", "info", "О программе", pane("about")})
	item(homeItem{"exit", "exit", "Выход", func() { a.RequestClose() }})

	area := Rect{r.X + menuW + a.d(40), r.Y + a.d(12), r.W - menuW - a.d(80), r.H - a.d(20)}
	cv.SetClip(Rect{r.X + menuW, r.Y, r.W - menuW, r.H})
	defer cv.ResetClip()
	var contentH float64
	switch a.bsPane {
	case "recent":
		contentH = a.paneFiles(cv, area, "Недавние документы", a.Set.Recent, "Здесь появятся документы, которые вы открывали.")
	case "favorites":
		contentH = a.paneFiles(cv, area, "Избранное", a.Set.Favorites, "Отмечайте документы звёздочкой в списке недавних — они появятся здесь.")
	case "templates":
		contentH = a.paneTemplates(cv, area)
	case "settings":
		contentH = a.paneSettings(cv, area)
	case "about":
		contentH = a.paneAbout(cv, area)
	default:
		contentH = a.paneHome(cv, area)
	}
	a.bsScroll = math.Max(0, math.Min(a.bsScroll, contentH-area.H))
}

func (a *App) h1(cv *Canvas, s string, x, y float64) {
	a.textAt(cv, s, x, y, Font{Family: a.UIFont, Px: a.d(26), Bold: true}, a.th.Text)
}

// button — кнопка-пилюля.
func (a *App) button(cv *Canvas, id, icon, label string, x, y float64, primary bool, act func()) float64 {
	th := a.th
	f := a.uiFont(13)
	f.Bold = true
	w := a.measure(label, f) + a.d(36)
	if icon != "" {
		w += a.d(24)
	}
	r := Rect{x, y, w, a.d(36)}
	a.hits = append(a.hits, hit{id: id, r: r, act: act})
	col := th.Text
	if primary {
		cv.FillRound(r, a.d(6), Mix(th.Accent, Hex(0xffffff), 0.12*a.anim[id]))
		col = th.AccentText
	} else {
		cv.FillRound(r, a.d(6), th.Field)
		cv.StrokeRound(r, a.d(6), math.Max(1, a.S), th.Border)
		a.hoverBg(cv, id, r, a.d(6))
	}
	tx := r.X + a.d(18)
	if icon != "" {
		a.icon(cv, icon, Rect{tx - a.d(2), r.Y + a.d(8), a.d(20), a.d(20)}, col)
		tx += a.d(24)
	}
	a.textAt(cv, label, tx, r.Y+(r.H-a.d(18))/2, f, col)
	return w
}

func (a *App) paneHome(cv *Canvas, area Rect) float64 {
	th := a.th
	y := area.Y + a.d(16) - a.bsScroll
	// шапка без баннера: версия, вопрос, две кнопки и подсказка про палитру
	a.textAt(cv, "TEXORA  "+AppVersion, area.X, y+a.d(8), a.monoFont(11), th.TextDim)
	a.textAt(cv, a.tr("С чего начнём?"), area.X, y+a.d(30), Font{Family: a.UIFont, Px: a.d(30), Bold: true}, th.Text)
	bx := area.X
	by := y + a.d(88)
	bx += a.button(cv, "h:new", "plus", "Новый документ", bx, by, true, func() { a.newFromTemplate("blank") }) + a.d(10)
	bx += a.button(cv, "h:open", "open", "Открыть файл", bx, by, false, func() { a.Command("open") }) + a.d(18)
	a.textAt(cv, "Ctrl+N · Ctrl+O · Ctrl+K "+a.tr("все команды"), bx, by+a.d(10), a.monoFont(11), th.TextDim)
	y = by + a.d(36) + a.d(40)
	// шаблоны
	a.capsLabel(cv, "Создать", area.X, y+a.d(6))
	more := a.tr("Все шаблоны →")
	mf := a.uiFont(13)
	mw := a.measure(more, mf)
	mr := Rect{area.X + area.W - mw - a.d(16), y - a.d(4), mw + a.d(16), a.d(30)}
	a.hits = append(a.hits, hit{id: "h:alltpl", r: mr, act: func() { a.bsPane, a.bsScroll = "templates", 0 }})
	a.hoverBg(cv, "h:alltpl", mr, a.d(6))
	a.tb.Draw(cv, more, mr.X+a.d(8), mr.Y+a.d(20), mf, th.Accent2)
	y += a.d(40)
	cardW := a.d(150)
	n := int(math.Max(1, math.Floor((area.W+a.d(16))/(cardW+a.d(16)))))
	n = min(n, len(templatesList))
	for i := 0; i < n; i++ {
		a.tplCard(cv, templatesList[i], area.X+float64(i)*(cardW+a.d(16)), y, cardW, false)
	}
	y += cardW*1.414 + a.d(70)
	// недавние
	a.capsLabel(cv, "Последние документы", area.X, y)
	cv.FillRect(Rect{area.X, y + a.d(26), area.W, math.Max(1, a.S)}, th.Divider)
	y += a.d(36)
	rec := a.Set.Recent
	if len(rec) > 8 {
		rec = rec[:8]
	}
	if len(rec) == 0 {
		a.textAt(cv, a.tr("Здесь появятся документы, которые вы открывали."), area.X, y, a.uiFont(13.5), th.TextDim)
		y += a.d(30)
	}
	for i, p := range rec {
		a.fileRow(cv, fmt.Sprintf("hr:%d", i), p, area.X, y, area.W)
		y += a.d(52)
	}
	return y + a.bsScroll - area.Y + a.d(20)
}

func (a *App) fileRow(cv *Canvas, id, p string, x, y, w float64) {
	th := a.th
	ir := Rect{x, y, w, a.d(48)}
	a.hits = append(a.hits, hit{id: id, r: ir, act: func() { a.OpenPath(p) }, tip: p})
	a.hoverBg(cv, id, ir, a.d(5))
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(p), "."))
	badge := Rect{ir.X + a.d(10), ir.Y + a.d(9), a.d(30), a.d(30)}
	cv.StrokeRound(badge, a.d(5), math.Max(1, a.S), th.Border)
	a.centerText(cv, ext, badge, a.monoFont(9), th.TextDim)
	clip := cv.clip
	cv.SetClip(intersect(Rect{ir.X, ir.Y, ir.W - a.d(50), ir.H}, clip))
	a.tb.Draw(cv, filepath.Base(p), ir.X+a.d(52), ir.Y+a.d(21), a.uiFont(14), th.Text)
	a.tb.Draw(cv, filepath.Dir(p), ir.X+a.d(52), ir.Y+a.d(39), a.uiFont(11), th.TextDim)
	cv.SetClip(clip)
	star := "star"
	if a.isFav(p) {
		star = "starfill"
	}
	a.iconBtn(cv, id+":star", star, Rect{ir.X + ir.W - a.d(42), ir.Y + a.d(9), a.d(30), a.d(30)}, false, "В избранное", func() { a.toggleFav(p) })
}

func (a *App) paneFiles(cv *Canvas, area Rect, title string, files []string, empty string) float64 {
	y := area.Y + a.d(16) - a.bsScroll
	a.h1(cv, title, area.X, y)
	y += a.d(56)
	if len(files) == 0 {
		for i, l := range a.wrapText(a.tr(empty), area.W, a.uiFont(14)) {
			a.tb.Draw(cv, l, area.X, y+a.d(16)+float64(i)*a.d(22), a.uiFont(14), a.th.TextDim)
		}
		return a.d(140)
	}
	for i, p := range files {
		a.fileRow(cv, fmt.Sprintf("fl:%d", i), p, area.X, y, area.W)
		y += a.d(52)
	}
	return y + a.bsScroll - area.Y + a.d(20)
}

// tplCard — карточка шаблона с живой миниатюрой первой страницы.
func (a *App) tplCard(cv *Canvas, t tplInfo, x, y, w float64, withDesc bool) float64 {
	th := a.th
	pv := a.tplPreview(t.id)
	ph := w * pv.L.Page.H / pv.L.Page.W
	id := "tpl:" + t.id
	h := ph + a.d(48)
	if withDesc {
		h += a.d(60)
	}
	card := Rect{x - a.d(6), y - a.d(6), w + a.d(12), h}
	a.hits = append(a.hits, hit{id: id, r: card, act: func() { a.newFromTemplate(t.id) }, tip: t.desc})
	a.hoverBg(cv, id, card, a.d(6))
	pr := Rect{x, y, w, ph}
	cv.Shadow(pr, a.d(4), a.d(8), th.PageShadow)
	cv.FillRound(pr, a.d(4), Hex(0xffffff))
	if a.anim[id] > 0 {
		cv.StrokeRound(pr.Inset(-a.d(1.5)), a.d(5), a.d(2), th.Accent.WithA(a.anim[id]))
	}
	clip := cv.clip
	cv.SetClip(intersect(pr, clip))
	if t.id == "blank" {
		a.centerText(cv, "+", pr, Font{Family: a.UIFont, Px: a.d(40)}, Hex(0xa0a3ab))
	} else {
		pp := &PagePainter{cv: cv, tb: a.tb, doc: pv.d, k: w / pv.L.Page.W, imgs: map[string]*scaledImg{}, ox: pr.X, oy: pr.Y}
		pp.DrawPage(pv.L, pv.L.Pages[0], "body", true)
	}
	cv.SetClip(clip)
	a.textAt(cv, t.name, x, y+ph+a.d(12), a.uiFont(12.5), th.Text)
	if withDesc {
		for i, l := range a.wrapText(a.tr(t.desc), w, a.uiFont(11.5)) {
			if i >= 4 {
				break
			}
			a.tb.Draw(cv, l, x, y+ph+a.d(48)+float64(i)*a.d(15), a.uiFont(11.5), th.TextDim)
		}
	}
	return h
}

func (a *App) paneTemplates(cv *Canvas, area Rect) float64 {
	y := area.Y + a.d(16) - a.bsScroll
	a.h1(cv, "Шаблоны", area.X, y)
	y += a.d(44)
	a.textAt(cv, a.tr("Готовые документы с правильными полями, шрифтами и структурой — останется вписать своё."), area.X, y, a.uiFont(13.5), a.th.TextDim)
	y += a.d(44)
	cardW := a.d(176)
	cols := int(math.Max(1, math.Floor((area.W+a.d(24))/(cardW+a.d(24)))))
	rowH := 0.0
	for i, t := range templatesList {
		c := i % cols
		if c == 0 && i > 0 {
			y += rowH + a.d(24)
		}
		rowH = a.tplCard(cv, t, area.X+float64(c)*(cardW+a.d(24)), y, cardW, true)
	}
	return y + rowH + a.bsScroll - area.Y + a.d(30)
}

// ---------------------------------------------------------------- настройки
type segOpt struct {
	label string
	on    bool
	act   func()
}

func (a *App) segmented(cv *Canvas, id string, x, y float64, opts []segOpt) {
	th := a.th
	f := a.uiFont(13)
	var ws []float64
	total := 0.0
	for _, o := range opts {
		w := a.measure(o.label, f) + a.d(30)
		ws = append(ws, w)
		total += w
	}
	r := Rect{x, y, total + a.d(6), a.d(36)}
	cv.FillRound(r, a.d(10), th.Field)
	cv.StrokeRound(r, a.d(10), math.Max(1, a.S), th.Border)
	cx := x + a.d(3)
	for i, o := range opts {
		sr := Rect{cx, y + a.d(3), ws[i], a.d(30)}
		hid := fmt.Sprintf("%s:%d", id, i)
		a.hits = append(a.hits, hit{id: hid, r: sr, act: o.act})
		col := th.Text
		if o.on {
			cv.FillRound(sr, a.d(8), th.Accent)
			col = th.AccentText
		} else {
			a.hoverBg(cv, hid, sr, a.d(8))
		}
		a.centerText(cv, o.label, sr, f, col)
		cx += ws[i]
	}
}

// capsLabel — подпись раздела мелкими заглавными (как в инспекторе).
func (a *App) capsLabel(cv *Canvas, s string, x, y float64) {
	f := a.uiFont(10.5)
	f.Bold = true
	a.textAt(cv, strings.ToUpper(a.tr(s)), x, y, f, a.th.TextDim)
}

func (a *App) section(cv *Canvas, title string, x, y, w float64) float64 {
	a.textAt(cv, title, x, y, Font{Family: a.UIFont, Px: a.d(17), Bold: true}, a.th.Text)
	return y + a.d(36)
}

func (a *App) label(cv *Canvas, s string, x, y float64) {
	a.textAt(cv, s, x, y, a.uiFont(13.5), a.th.TextDim)
}

var shortcutList = [][2]string{
	{"Ctrl+N", "Новый документ"}, {"Ctrl+O", "Открыть"}, {"Ctrl+S", "Сохранить"}, {"F12 / Ctrl+Shift+S", "Сохранить как"},
	{"Ctrl+P", "Печать"}, {"Ctrl+W", "Закрыть документ"}, {"Ctrl+Tab", "Следующий документ"},
	{"Ctrl+Z / Ctrl+Y", "Отменить / вернуть"}, {"Ctrl+X / C / V", "Вырезать / копировать / вставить"},
	{"Ctrl+Shift+V", "Вставить только текст"}, {"Ctrl+A", "Выделить всё"}, {"Ctrl+F / Ctrl+H", "Найти / заменить"},
	{"Ctrl+G", "Перейти к странице"}, {"Ctrl+B / I / U", "Полужирный / курсив / подчёркнутый"},
	{"Ctrl+Shift+D", "Двойное подчёркивание"}, {"Ctrl+= / Ctrl+Shift+=", "Подстрочный / надстрочный"},
	{"Ctrl+Shift+> / <", "Размер шрифта больше / меньше"}, {"Ctrl+Пробел", "Очистить форматирование"},
	{"Shift+F3", "Регистр букв"}, {"Ctrl+L / E / R / J", "Выравнивание: влево / центр / вправо / по ширине"},
	{"Ctrl+1 / 2 / 5", "Интервал 1 / 2 / 1,5"}, {"Ctrl+M / Ctrl+Shift+M", "Отступ больше / меньше"},
	{"Ctrl+Shift+L", "Маркированный список"}, {"Ctrl+Alt+1 / 2 / 3", "Заголовок 1 / 2 / 3"}, {"Ctrl+Shift+N", "Обычный стиль"},
	{"Ctrl+Enter", "Разрыв страницы"}, {"Ctrl+Shift+Enter", "Разрыв колонки"}, {"Shift+Enter", "Разрыв строки"}, {"Ctrl+K", "Все команды (палитра)"}, {"Ctrl+Shift+K", "Гиперссылка"},
	{"Ctrl+Alt+F", "Сноска"}, {"Ctrl+Shift+C / V", "Копировать / вставить формат"}, {"Ctrl+колесо", "Масштаб"},
	{"F7", "Следующая ошибка орфографии"}, {"F9", "Обновить оглавление и список литературы"}, {"Alt+=", "Формула"},
	{"Ctrl+Alt+M", "Примечание"}, {"Ctrl+Shift+E", "Режим исправлений"}, {"Ctrl+щелчок", "Перейти по ссылке оглавления"},
}

func (a *App) paneSettings(cv *Canvas, area Rect) float64 {
	th := a.th
	y := area.Y + a.d(16) - a.bsScroll
	a.h1(cv, "Настройки", area.X, y)
	y += a.d(64)
	x := area.X
	y = a.section(cv, "Оформление", x, y, area.W)
	a.label(cv, "Тема", x, y)
	y += a.d(28)
	cardW, cardH := a.d(150), a.d(96)
	for i, t := range themeNames {
		t := t
		r := Rect{x + float64(i)*(cardW+a.d(14)), y, cardW, cardH}
		id := "set:theme:" + t.id
		a.hits = append(a.hits, hit{id: id, r: r, act: func() { a.SetThemeMode(t.id) }})
		pv := themes[t.id]
		if t.id == "system" {
			pv = themes["light"]
		}
		cv.FillRound(r, a.d(10), pv.Bg)
		if t.id == "system" { // половина тёмная
			clip := cv.clip
			cv.SetClip(intersect(Rect{r.X + r.W/2, r.Y, r.W / 2, r.H}, clip))
			cv.FillRound(r, a.d(10), themes["dark"].Bg)
			cv.SetClip(clip)
		}
		cv.FillRound(Rect{r.X + a.d(10), r.Y + a.d(10), r.W - a.d(20), a.d(16)}, a.d(4), pv.Surface)
		cv.FillRound(Rect{r.X + a.d(14), r.Y + a.d(14), a.d(26), a.d(8)}, a.d(3), th.Accent)
		cv.FillRound(Rect{r.X + r.W/2 - a.d(22), r.Y + a.d(32), a.d(44), a.d(32)}, a.d(2), Hex(0xffffff))
		on := a.Set.Theme == t.id
		if on {
			cv.StrokeRound(r.Inset(-a.d(2)), a.d(12), a.d(2.5), th.Accent)
		} else {
			cv.StrokeRound(r, a.d(10), math.Max(1, a.S), th.Border)
			if a.anim[id] > 0 {
				cv.StrokeRound(r.Inset(-a.d(2)), a.d(12), a.d(2), th.Accent.WithA(0.5*a.anim[id]))
			}
		}
		lr := Rect{r.X, r.Y + r.H - a.d(26), r.W, a.d(22)}
		if t.id == "system" {
			lw := a.measure(t.name, a.uiFont(12.5)) + a.d(16)
			cv.FillRound(Rect{r.X + (r.W-lw)/2, lr.Y, lw, lr.H}, a.d(6), pv.Bg)
		}
		a.centerText(cv, t.name, lr, a.uiFont(12.5), pv.Text)
	}
	y += cardH + a.d(26)
	a.label(cv, "Акцентный цвет", x, y)
	y += a.d(30)
	for i, c := range accentColors {
		c := c
		cr := Rect{x + float64(i)*a.d(46), y, a.d(34), a.d(34)}
		id := "set:acc:" + c.name
		a.hits = append(a.hits, hit{id: id, r: cr, act: func() { a.Set.Accent = c.hex; a.applyTheme(); a.saveSettings() }, tip: c.name})
		col := parseHexColor(c.hex, brandMain)
		cv.FillCircle(cr.X+cr.W/2, cr.Y+cr.H/2, cr.W/2-a.d(3)+a.d(1)*a.anim[id], col)
		if a.Set.Accent == c.hex {
			cv.StrokeRound(cr, cr.W/2, a.d(2), th.Text)
			a.icon(cv, "check", cr.Inset(a.d(7)), Hex(0xffffff))
		}
	}
	y += a.d(58)
	a.label(cv, "Размер интерфейса", x, y)
	y += a.d(30)
	var sc []segOpt
	for _, v := range []float64{0.9, 1, 1.1, 1.25, 1.5} {
		v := v
		sc = append(sc, segOpt{fmt.Sprintf("%d%%", int(v*100+0.5)), math.Abs(a.Set.UIScale-v) < 0.01, func() { a.Set.UIScale = v; a.saveSettings() }})
	}
	a.segmented(cv, "set:scale", x, y, sc)
	y += a.d(70)

	y = a.section(cv, "Сохранение", x, y, area.W)
	a.label(cv, "Автосохранение копии для восстановления после сбоя", x, y)
	y += a.d(30)
	var as []segOpt
	for _, v := range []int{0, 1, 5, 10} {
		v := v
		l := "Выкл."
		if v > 0 {
			l = a.trf("%d мин", v)
		}
		as = append(as, segOpt{l, a.Set.AutosaveMin == v, func() { a.Set.AutosaveMin = v; a.saveSettings() }})
	}
	a.segmented(cv, "set:auto", x, y, as)
	y += a.d(52)
	a.checkbox(cv, "set:autofile", "Сохранять изменения прямо в файл документа (если он уже сохранён)", x, y, a.Set.AutosaveFile,
		func() { a.Set.AutosaveFile = !a.Set.AutosaveFile; a.saveSettings() })
	y += a.d(32)
	a.checkbox(cv, "set:autoupdate", "Проверять обновления раз в день (нужен интернет)", x, y, !a.Set.NoAutoUpdate,
		func() { a.Set.NoAutoUpdate = !a.Set.NoAutoUpdate; a.saveSettings() })
	y += a.d(64)

	y = a.section(cv, "Правописание и рецензирование", x, y, area.W)
	a.checkbox(cv, "set:spell", "Проверять орфографию при вводе (русский и английский)", x, y, !a.Set.NoSpell,
		func() { a.Set.NoSpell = !a.Set.NoSpell; a.saveSettings() })
	y += a.d(40)
	a.label(cv, a.trf("Имя автора в примечаниях и исправлениях: %s", a.authorName()), x, y)
	a.button(cv, "set:author", "gear", "Изменить имя…", x, y+a.d(28), false, func() { a.authorDialog() })
	y += a.d(90)

	y = a.section(cv, "Язык интерфейса", x, y, area.W)
	a.segmented(cv, "set:lang", x, y, []segOpt{{"Русский", a.Set.Lang == "ru", func() { a.Set.Lang = "ru"; a.saveSettings() }},
		{"English", a.Set.Lang == "en", func() { a.Set.Lang = "en"; a.saveSettings() }}})
	y += a.d(70)

	keysY := y
	y = a.section(cv, "Сочетания клавиш", x, y, area.W)
	colW := math.Min(area.W/2, a.d(470))
	half := (len(shortcutList) + 1) / 2
	for i, k := range shortcutList {
		cx := x + float64(i/half)*colW
		cy := y + float64(i%half)*a.d(30)
		kf := Font{Family: "Consolas", Px: a.d(12.5)}
		kw := a.tb.Measure(a.tr(k[0]), kf) + a.d(14)
		kr := Rect{cx, cy, kw, a.d(24)}
		cv.FillRound(kr, a.d(6), th.Field)
		cv.StrokeRound(kr, a.d(6), math.Max(1, a.S), th.Border)
		a.centerText(cv, k[0], kr, kf, th.Text)
		a.textAt(cv, k[1], cx+math.Max(kw, a.d(150))+a.d(10), cy+a.d(4), a.uiFont(12.5), th.TextDim)
	}
	y += float64(half)*a.d(30) + a.d(30)
	if a.setScroll == "keys" {
		a.bsScroll += keysY - area.Y - a.d(10)
		a.setScroll = ""
		a.invalidate()
	}
	return y + a.bsScroll - area.Y
}

func (a *App) checkbox(cv *Canvas, id, label string, x, y float64, on bool, act func()) {
	th := a.th
	f := a.uiFont(13.5)
	w := a.measure(label, f) + a.d(40)
	r := Rect{x - a.d(6), y - a.d(4), w + a.d(12), a.d(32)}
	a.hits = append(a.hits, hit{id: id, r: r, act: act})
	a.hoverBg(cv, id, r, a.d(8))
	box := Rect{x, y + a.d(2), a.d(20), a.d(20)}
	if on {
		cv.FillRound(box, a.d(5), th.Accent)
		a.icon(cv, "check", box, Hex(0xffffff))
	} else {
		cv.FillRound(box, a.d(5), th.Field)
		cv.StrokeRound(box, a.d(5), math.Max(1, a.S*1.2), th.TextDim)
	}
	a.textAt(cv, label, x+a.d(32), y+a.d(3), f, th.Text)
}

func (a *App) paneAbout(cv *Canvas, area Rect) float64 {
	th := a.th
	y := area.Y + a.d(30) - a.bsScroll
	lg := a.d(190)
	LogoIso(cv, Rect{area.X, y, lg, lg})
	x := area.X + lg + a.d(40)
	a.textAt(cv, AppName, x, y+a.d(20), Font{Family: a.UIFont, Px: a.d(44), Bold: true}, th.Text)
	a.textAt(cv, a.tr("Версия")+" "+AppVersion, x, y+a.d(82), a.uiFont(14), th.Accent2)
	a.textAt(cv, "Пиши. Создавай. Вдохновляйся.", x, y+a.d(110), a.uiFont(16), th.TextDim)
	y += lg + a.d(40)
	for _, l := range []string{
		"Текстовый редактор для Windows, полностью свой: движок вёрстки, форматы файлов, интерфейс.",
		"Открывает и сохраняет .docx, .odt, .rtf, .txt, читает .doc, экспортирует в PDF.",
		"Работает без интернета, без Microsoft Office и без LibreOffice.",
		"",
		"Словари проверки орфографии и переносов — из проекта LibreOffice: русский © 1997–2008 Александр Лебедев,",
		"английский — SCOWL © Kevin Atkinson и др., шаблоны переносов — TeX hyphenation patterns. Лицензии BSD/MPL-совместимые.",
	} {
		a.textAt(cv, l, area.X, y, a.uiFont(14), th.Text)
		y += a.d(28)
	}
	y += a.d(10)
	if a.Diag != "" {
		a.textAt(cv, a.Diag, area.X, y, a.uiFont(12), th.TextDim)
		y += a.d(24)
	}
	return y + a.bsScroll - area.Y
}

func (a *App) authorDialog() {
	d := &Dialog{Title: "Имя автора", W: 440, Fields: []*Field{textField("Имя и фамилия", a.authorName()),
		{Label: "Так подписываются примечания и правки в режиме исправлений", Kind: "info"}}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "OK", Primary: true, Act: func() bool {
		v := strings.TrimSpace(d.Val(0))
		if v == "" {
			d.Info = "Имя не может быть пустым"
			return false
		}
		a.Set.Author = v
		a.saveSettings()
		for _, t := range a.tabs {
			if t.E != nil {
				t.E.Author = v
				t.E.rev = ""
			}
		}
		a.E.Author, a.E.rev = v, ""
		return true
	}}}
	a.openDialog(d)
}
