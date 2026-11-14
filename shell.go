package main

// Оболочка окна (с 1.5): тонкая панель инструментов, полоса слева, инспектор справа и палитра команд (Ctrl+K).
// Все команды описаны один раз (buildActions) — из этого списка берут кнопки инспектор, панель и палитра.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// action — команда интерфейса.
type action struct {
	id, icon, label, keys, group string
	on                           bool   // включено (для переключателей и текущих режимов)
	toggle                       bool   // показывать как переключатель
	off                          string // непусто — сейчас недоступна, текст объясняет почему
	act                          func()
}

func (a *App) actionsByID() map[string]*action {
	if a.actIdx == nil {
		a.refreshActions()
	}
	return a.actIdx
}

func (a *App) refreshActions() {
	a.acts = a.buildActions()
	a.actIdx = make(map[string]*action, len(a.acts))
	for _, ac := range a.acts {
		a.actIdx[ac.id] = ac
	}
}

// anchorFor — где открыть всплывающее меню команды: у её кнопки, если она видна, иначе под панелью.
func (a *App) anchorFor(id string) Rect {
	for _, h := range a.hits {
		if h.id == id || h.id == "x:"+id {
			return h.r
		}
	}
	return Rect{a.W/2 - a.d(150), a.d(96), a.d(300), 0}
}

func (a *App) buildActions() []*action {
	var out []*action
	group := ""
	add := func(id, icon, label, keys string, act func()) *action {
		ac := &action{id: id, icon: icon, label: label, keys: keys, group: group, act: act}
		out = append(out, ac)
		return ac
	}
	cmd := func(c string) func() { return func() { a.Command(c) } }
	e := a.E
	pdf := a.pdf != nil
	noPDF := func(ac *action) {
		if pdf {
			ac.off = "в PDF правка недоступна"
		}
	}

	group = "Файл"
	add("new", "new", "Новый документ", "Ctrl+N", cmd("new"))
	add("open", "open", "Открыть…", "Ctrl+O", cmd("open"))
	noPDF(add("save", "save", "Сохранить", "Ctrl+S", cmd("save")))
	noPDF(add("saveas", "copy", "Сохранить как…", "F12", cmd("saveas")))
	noPDF(add("pdf", "pdf", "Экспорт в PDF", "", cmd("pdf")))
	add("print", "print", "Печать…", "Ctrl+P", cmd("print"))
	add("home", "home", "Главная Texora", "", func() { a.openHome("home") })
	add("templates", "template", "Шаблоны", "", func() { a.openHome("templates") })
	add("closetab", "x", "Закрыть документ", "Ctrl+W", func() { a.closeTab(a.cur) })

	group = "Правка"
	noPDF(add("undo", "undo", "Отменить", "Ctrl+Z", cmd("undo")))
	noPDF(add("redo", "redo", "Вернуть", "Ctrl+Y", cmd("redo")))
	noPDF(add("paste", "paste", "Вставить", "Ctrl+V", cmd("paste")))
	noPDF(add("pasteplain", "paste", "Вставить только текст", "Ctrl+Shift+V", cmd("pasteplain")))
	noPDF(add("cut", "cut", "Вырезать", "Ctrl+X", cmd("cut")))
	if pdf {
		add("copy", "copy", "Копировать текст", "Ctrl+C", func() { a.pdfCopy() })
		add("find", "find", "Найти", "Ctrl+F", func() { a.pdfFindDialog() })
	} else {
		add("copy", "copy", "Копировать", "Ctrl+C", cmd("copy"))
		add("find", "find", "Найти", "Ctrl+F", cmd("find"))
	}
	noPDF(add("replace", "replace", "Заменить", "Ctrl+H", cmd("replace")))
	add("selectall", "selectall", "Выделить всё", "Ctrl+A", cmd("selectall"))
	noPDF(add("painter", "painter", "Формат по образцу", "Ctrl+Shift+C", cmd("painter")))
	add("goto", "pagenum", "Перейти к странице…", "Ctrl+G", func() { a.gotoPageDialog() })

	if !pdf {
		ef := e.EffectiveF()
		pf := e.CurrentPF()
		group = "Текст"
		add("bold", "bold", "Полужирный", "Ctrl+B", cmd("bold")).on = ef.B
		add("italic", "italic", "Курсив", "Ctrl+I", cmd("italic")).on = ef.I
		add("under", "under", "Подчёркнутый", "Ctrl+U", cmd("under")).on = ef.U || ef.DU
		add("strike", "strike", "Зачёркнутый", "", cmd("strike")).on = ef.S
		add("sub", "sub", "Подстрочный", "Ctrl+=", cmd("sub")).on = ef.Sub
		add("sup", "sup", "Надстрочный", "Ctrl+Shift+=", cmd("sup")).on = ef.Sup
		add("grow", "grow", "Увеличить шрифт", "Ctrl+Shift+>", func() { e.GrowFont(1); a.after() })
		add("shrink", "shrink", "Уменьшить шрифт", "Ctrl+Shift+<", func() { e.GrowFont(-1); a.after() })
		add("font", "styles", "Шрифт…", "", func() { a.fontPopup(a.anchorFor("font")) })
		add("size", "grow", "Размер шрифта…", "", func() { a.sizePopup(a.anchorFor("size")) })
		add("color", "fontcolor", "Цвет текста…", "", func() { a.colorMenu(a.anchorFor("color")) })
		add("bg", "highlight", "Цвет выделения текста…", "", func() { a.bgMenu(a.anchorFor("bg")) })
		add("case", "case", "Регистр…", "Shift+F3", func() { a.casePopup() })
		add("clear", "clear", "Очистить форматирование", "Ctrl+Пробел", func() { e.ClearFormat(); a.after() })

		group = "Абзац"
		add("bullets", "bullets", "Маркированный список", "Ctrl+Shift+L", cmd("bullets")).on = pf.List != nil && pf.List.Kind == "ul"
		add("numbering", "numbers", "Нумерованный список", "", cmd("numbering")).on = pf.List != nil && pf.List.Kind == "ol"
		add("outdent", "outdent", "Уменьшить отступ", "Ctrl+Shift+M", func() { e.Indent(-1); a.after() })
		add("indent", "indent", "Увеличить отступ", "Ctrl+M", func() { e.Indent(1); a.after() })
		for _, al := range []struct{ id, label, keys string }{{"left", "По левому краю", "Ctrl+L"}, {"center", "По центру", "Ctrl+E"},
			{"right", "По правому краю", "Ctrl+R"}, {"justify", "По ширине", "Ctrl+J"}} {
			al := al
			add("al:"+al.id, "al:"+al.id, al.label, al.keys, func() { e.SetAlign(al.id); a.after() }).on = pf.Align == al.id
		}
		add("spacing", "spacing", "Междустрочный интервал…", "", func() { a.spacingPopup() })
		add("firstline", "firstline", "Красная строка 1,25 см", "", func() {
			off := e.CurrentPF().IndF > 0
			e.SetPF(func(p PF) PF {
				if off {
					p.IndF = Opt{}
				} else {
					p.IndF = O(1.25 * CM)
				}
				return p
			})
			a.after()
		}).on = pf.IndF > 0
		add("para", "para", "Отступы и интервалы…", "", func() { a.paragraphDialog() })

		group = "Стиль"
		p := e.Sel.F.P
		for i, s := range styleGallery {
			i := i
			keys := ""
			if s.h >= 1 && s.h <= 3 {
				keys = fmt.Sprintf("Ctrl+Alt+%d", s.h)
			}
			add(fmt.Sprintf("style:%d", i), "style", "Стиль «"+s.label+"»", keys, func() { a.applyGalleryStyle(i) }).on = p.PF.Heading == s.h && p.PF.Style == s.named
		}
		add("stylemenu", "style", "Все стили и настройка…", "", func() { a.styleMenu(a.anchorFor("stylemenu")) })

		group = "Таблица"
		why := ""
		if e.TableCtx() == nil {
			why = "поставьте курсор в ячейку таблицы"
		}
		for _, t := range []struct{ id, icon, label string }{{"row+", "row+", "Вставить строку снизу"}, {"row-above", "row-above", "Вставить строку сверху"},
			{"col+", "col+", "Вставить столбец справа"}, {"col-left", "col-left", "Вставить столбец слева"}, {"merge", "merge", "Объединить с ячейкой справа"},
			{"row-", "row-", "Удалить строку"}, {"col-", "col-", "Удалить столбец"}, {"border", "border", "Границы вкл/выкл"},
			{"shade", "shade", "Заливка ячейки"}, {"deltable", "deltable", "Удалить таблицу"}} {
			add("t:"+t.id, t.icon, t.label, "", cmd(t.id)).off = why
		}

		group = "Вставка"
		add("table", "table", "Таблица…", "", func() { a.tablePicker(a.anchorFor("table")) })
		add("tabledlg", "table", "Таблица с параметрами…", "", func() { a.tableDialog() })
		add("picture", "picture", "Рисунок…", "", cmd("picture"))
		add("wrap", "wrap", "Обтекание рисунка…", "", func() { a.wrapPopup(a.anchorFor("wrap")) }).on = a.imgSel != nil
		add("eq", "eq", "Формула…", "Alt+=", func() { a.eqDialog(nil, -1) })
		add("symbol", "symbol", "Символ…", "", func() { a.symbolPopup(a.anchorFor("symbol")) })
		add("date", "date", "Дата и время…", "", func() { a.datePopup() })
		add("link", "link", "Гиперссылка…", "Ctrl+Shift+K", cmd("link"))
		add("pagebreak", "pagebreak", "Разрыв страницы", "Ctrl+Enter", cmd("pagebreak"))
		add("breaks", "section", "Разрывы раздела…", "", func() { a.breaksPopup(a.anchorFor("breaks")) })
		add("colbreak", "pagebreak", "Разрыв колонки", "Ctrl+Shift+Enter", func() { e.InsertColumnBreak(); a.after() })

		group = "Колонтитулы"
		add("header", "header", "Верхний колонтитул", "", cmd("header"))
		add("footer", "header", "Нижний колонтитул", "", cmd("footer"))
		add("pagenum", "pagenum", "Номер страницы", "", cmd("pagenum"))
		add("numpages", "numpages", "Число страниц", "", cmd("numpages"))
		tp := add("titlepg", "fitpage", "Особая первая страница", "", cmd("titlepg"))
		tp.on, tp.toggle = e.CurSection().TitlePg, true
		eo := add("evenodd", "fitpage", "Чётные и нечётные", "", func() {
			e.Checkpoint("")
			e.Doc.EvenOdd = !e.Doc.EvenOdd
			e.Changed()
			a.after()
		})
		eo.on, eo.toggle = e.Doc.EvenOdd, true
		add("pagenumfmt", "pagesetup", "Формат номеров страниц…", "", func() { a.pageNumDialog() })

		group = "Страница"
		add("margins", "margins", "Поля…", "", func() { a.marginsPopup() })
		add("orient", "orient", "Ориентация…", "", func() { a.orientPopup() })
		add("pagesize", "size", "Размер листа…", "", func() { a.sizePagePopup() })
		add("pagesetup", "pagesetup", "Параметры страницы…", "", func() { a.pageDialog() })
		cols := e.CurSection().Cols
		add("columns", "columns", "Колонки…", "", func() { a.columnsPopup(a.anchorFor("columns")) }).on = cols.Count() > 1
		for n := 1; n <= 3; n++ {
			n := n
			add(fmt.Sprintf("cols%d", n), "columns", []string{"", "Одна колонка", "Две колонки", "Три колонки"}[n], "", func() { a.setColumns(n, -1) }).on = cols.Count() == n
		}
		cs := add("colsep", "columns", "Линия между колонками", "", func() { a.setColumns(0, map[bool]int{true: 0, false: 1}[cols.Sep]) })
		cs.on, cs.toggle = cols.Sep, true
		if cols.Count() <= 1 {
			cs.off = "сначала сделайте две колонки или больше"
		}
		hy := add("hyph", "hyph", "Автоматические переносы", "", func() {
			e.Checkpoint("")
			e.Doc.Hyphen = !e.Doc.Hyphen
			e.Changed()
			a.after()
		})
		hy.on, hy.toggle = e.Doc.Hyphen, true

		group = "Ссылки"
		add("toc", "toc", "Оглавление", "", cmd("toc"))
		add("update", "update", "Обновить поля", "F9", func() { a.UpdateFields() })
		add("sources", "bib", "Источники…", "", func() { a.sourcesPopup(a.anchorFor("sources")) })
		add("cite", "cite", "Вставить ссылку на источник…", "", func() { a.citePopup(a.anchorFor("cite")) })
		add("bib", "toc", "Список литературы", "", func() { e.InsertBibliography(); a.after() })
		add("bibimport", "open", "Импорт литературы из BibTeX или RIS…", "", func() { a.importBibFile() })
		add("bibpaste", "paste", "Вставить литературу из буфера (BibTeX или RIS)", "", func() { a.importBibClipboard() })
		bx := add("bibexport", "save", "Экспорт литературы в BibTeX…", "", func() { a.exportBib("bib") })
		rx := add("risexport", "save", "Экспорт литературы в RIS…", "", func() { a.exportBib("ris") })
		dd := add("bibdups", "merge", "Объединить повторы в литературе", "", func() { a.mergeDupSources() })
		if len(e.Doc.Sources) == 0 {
			bx.off, rx.off = "список литературы пуст", "список литературы пуст"
		}
		if len(FindDuplicates(e.Doc.Sources)) == 0 {
			dd.off = "повторов нет"
		}
		add("footnote", "footnote", "Вставить сноску", "Ctrl+Alt+F", cmd("footnote"))
		add("nextfn", "next", "Следующая сноска", "", func() { a.nextFootnote() })
		add("caption", "picture", "Вставить название…", "", func() { a.captionDialog() })
		add("xref", "link", "Перекрёстная ссылка…", "", func() { a.crossRefDialog() })
		add("lof", "toc", "Перечень иллюстраций…", "", func() { a.lofPopup(a.anchorFor("lof")) })

		group = "Проверка"
		add("spell", "spell", "Следующая ошибка", "F7", func() { a.nextSpellError() })
		add("normcheck", "check", "Нормоконтроль (ГОСТ 7.32)", "", func() { a.runNormCheck() })
		so := add("spellon", "check", "Проверять при вводе", "", func() {
			a.Set.NoSpell = !a.Set.NoSpell
			a.saveSettings()
			a.invalidate()
		})
		so.on, so.toggle = !a.Set.NoSpell, true
		add("nospell", "lang", "Не проверять выделенный текст", "", func() {
			e.ApplyF(func(f F) F {
				if f.Lang == "-" {
					f.Lang = ""
				} else {
					f.Lang = "-"
				}
				return f
			})
			a.after()
		})
		add("stats", "stats", "Статистика", "", func() { a.statsDialog() })
		add("cmt", "comment", "Создать примечание", "Ctrl+Alt+M", func() { a.commentDialog("") })
		add("cmtdel", "comment-", "Удалить примечание", "", func() {
			if id := e.CommentAt(); id != "" {
				e.DeleteComment(id)
			} else if id := e.Doc.activeCmt; id != "" {
				e.DeleteComment(id)
			}
			a.after()
		})
		add("cmtprev", "prev", "Предыдущее примечание", "", func() { a.gotoComment(-1) })
		add("cmtnext", "next", "Следующее примечание", "", func() { a.gotoComment(1) })
		tr := add("track", "track", "Запись исправлений", "Ctrl+Shift+E", func() {
			e.Doc.Track = !e.Doc.Track
			e.Dirty = true
			if e.Doc.Track {
				a.msg(a.tr("Режим исправлений включён: правки будут видны"))
			}
			a.invalidate()
		})
		tr.on, tr.toggle = e.Doc.Track, true
		mk := add("markup", "markup", "Показывать правки", "", func() {
			a.Set.HideMarkup = !a.Set.HideMarkup
			a.saveSettings()
			a.clampScroll()
			a.invalidate()
		})
		mk.on, mk.toggle = a.showMarks(), true
		add("accept", "accept", "Принять правку", "", func() {
			n := e.ResolveRevisions(true, false)
			e.NextRevision(1)
			a.msg(a.trf("Принято: %d", n))
			a.after()
		})
		add("reject", "reject", "Отклонить правку", "", func() {
			n := e.ResolveRevisions(false, false)
			e.NextRevision(1)
			a.msg(a.trf("Отклонено: %d", n))
			a.after()
		})
		add("revprev", "prev", "Предыдущая правка", "", func() { e.NextRevision(-1); a.ScrollToCaret(); a.after() })
		add("revnext", "next", "Следующая правка", "", func() { e.NextRevision(1); a.ScrollToCaret(); a.after() })
		add("compare", "replace", "Сравнить с другой версией…", "", func() {
			if p := a.P.OpenDialog("doc"); p != "" {
				a.CompareWithFile(p)
			}
		})
		add("acceptall", "check", "Принять все правки", "", func() { n := e.ResolveRevisions(true, true); a.msg(a.trf("Принято: %d", n)); a.after() })
		add("rejectall", "x", "Отклонить все правки", "", func() { n := e.ResolveRevisions(false, true); a.msg(a.trf("Отклонено: %d", n)); a.after() })
	} else {
		group = "PDF"
		add("pdffirst", "back", "В начало", "Home", func() { a.pdfGoto(0, -1) })
		add("pdfprev", "prev", "Предыдущая страница", "PgUp", func() { a.pdfGoto(a.pdfCurPage()-1, -1) })
		add("pdfnext", "next", "Следующая страница", "PgDn", func() { a.pdfGoto(a.pdfCurPage()+1, -1) })
	}

	group = "Вид"
	add("zoomout", "zoomout", "Уменьшить масштаб", "Ctrl+колесо", func() { a.SetZoom(a.zoom / 1.1) })
	add("zoomin", "zoomin", "Увеличить масштаб", "Ctrl+колесо", func() { a.SetZoom(a.zoom * 1.1) })
	add("zoom100", "fitpage", "Масштаб 100%", "", func() { a.SetZoom(1) }).on = math.Abs(a.zoom-1) < 0.005
	add("fitwidth", "fitwidth", "По ширине", "", func() { a.fitWidth() })
	add("fitpage", "fitpage", "Страница целиком", "", func() { a.fitPage() })
	nv := add("nav", "pages", "Панель навигации", "", func() { a.toggleNav("") })
	nv.on, nv.toggle = a.Set.ShowNav, true
	add("navpages", "pages", "Миниатюры страниц", "", func() { a.toggleNav("pages") }).on = a.Set.ShowNav && a.Set.NavMode != "headings"
	add("navheads", "toc", "Заголовки документа", "", func() { a.toggleNav("headings") }).on = a.Set.ShowNav && a.Set.NavMode == "headings"
	if !pdf {
		in := add("inspector", "panel", "Инспектор справа", "", func() {
			a.Set.HideInspector = !a.Set.HideInspector
			a.saveSettings()
		})
		in.on, in.toggle = !a.Set.HideInspector, true
		gr := add("grid", "border", "Сетка таблиц без границ", "", func() {
			a.Set.NoGrid = !a.Set.NoGrid
			a.saveSettings()
			a.invalidate()
		})
		gr.on, gr.toggle = !a.Set.NoGrid, true
	}
	for _, t := range themeNames {
		t := t
		add("theme:"+t.id, map[bool]string{true: "moon", false: "sun"}[t.id != "light"], "Тема: "+t.name, "", func() { a.SetThemeMode(t.id) }).on = a.Set.Theme == t.id
	}

	group = "Справка"
	add("palette", "cmd", "Все команды", "Ctrl+K", func() { a.openPalette() })
	add("keys", "lang", "Сочетания клавиш", "", func() { a.openHome("settings"); a.setScroll = "keys" })
	add("checkupdate", "update", "Проверить обновления", "", func() { a.CheckUpdates(true) })
	add("settings", "gear", "Настройки", "", func() { a.openHome("settings") })
	add("about", "info", "О программе", "", func() { a.openHome("about") })
	return out
}

// colorMenu / bgMenu — палитры цвета текста и выделения.
func (a *App) colorMenu(r Rect) {
	a.colorPopup("color", r, themeColors, standardColors, "Авто", func(c string) {
		if c != "" {
			a.lastColor = c
		}
		a.E.SetColor(c)
		a.after()
	})
}

func (a *App) bgMenu(r Rect) {
	a.colorPopup("bg", r, [][]string{highlightList[:10]}, highlightList[10:], "Нет цвета", func(c string) {
		if c != "" {
			a.lastBg = c
		}
		a.E.SetBg(c)
		a.after()
	})
}

// styleMenu — все стили и их настройка.
func (a *App) styleMenu(r Rect) {
	p := a.E.Sel.F.P
	var items []PopItem
	for i, s := range styleGallery {
		i := i
		items = append(items, PopItem{Label: s.label, Icon: "style", On: p.PF.Heading == s.h && p.PF.Style == s.named, Act: func() { a.applyGalleryStyle(i) }})
	}
	if extra := a.styleMenuItems(); extra != nil {
		items = append(items, PopItem{Sep: true})
		items = append(items, extra...)
	}
	a.popup = a.listPopup("stylemenu", r.X, r.Y+r.H, math.Max(r.W, a.d(220)), items, a.d(420), false)
}

// curStyleName — подпись текущего стиля абзаца.
func (a *App) curStyleName() string {
	p := a.E.Sel.F.P
	for _, s := range styleGallery {
		if p.PF.Heading == s.h && p.PF.Style == s.named {
			return s.label
		}
	}
	if p.PF.Heading > 0 {
		return fmt.Sprintf("Заголовок %d", p.PF.Heading)
	}
	return "Обычный"
}

// ---------------------------------------------------------------- общие элементы

func (a *App) monoFont(px float64) Font { return Font{Family: a.MonoFont, Px: a.d(px)} }

// vline — тонкий вертикальный разделитель.
func (a *App) vline(cv *Canvas, x, cy, h float64) {
	cv.FillRect(Rect{math.Round(x), cy - h/2, math.Max(1, a.S), h}, a.th.Divider)
}

// combo — поле со стрелкой (шрифт, размер, стиль).
func (a *App) combo(cv *Canvas, id, label string, r Rect, font string, open func(Rect)) {
	th := a.th
	a.hits = append(a.hits, hit{id: id, r: r, act: func() { open(r) }})
	cv.FillRound(r, a.d(5), th.Field)
	bc := th.Border
	if a.popup != nil && a.popup.Owner == id {
		bc = th.Accent
	}
	cv.StrokeRound(r, a.d(5), math.Max(1, a.S), bc)
	a.hoverBg(cv, id, r, a.d(5))
	f := a.uiFont(12.5)
	clip := cv.clip
	cv.SetClip(intersect(Rect{r.X, r.Y, r.W - a.d(22), r.H}, clip))
	a.textAt(cv, label, r.X+a.d(9), r.Y+(r.H-a.d(17))/2, f, th.Text)
	cv.SetClip(clip)
	a.icon(cv, "chevron", Rect{r.X + r.W - a.d(20), r.Y + (r.H-a.d(12))/2, a.d(12), a.d(12)}, th.TextDim)
}

// actBtn — квадратная кнопка команды (на панели и в инспекторе).
func (a *App) actBtn(cv *Canvas, ac *action, r Rect) {
	if ac == nil {
		return
	}
	tip := a.tr(ac.label)
	if ac.keys != "" {
		tip += "  ·  " + ac.keys
	}
	if ac.off != "" {
		why := ac.off
		a.hits = append(a.hits, hit{id: "x:" + ac.id, r: r, tip: tip + " — " + a.tr(why), act: func() { a.toast(why) }})
		a.icon(cv, ac.icon, r, a.th.TextDim.WithA(0.4))
		return
	}
	a.iconBtn(cv, "x:"+ac.id, ac.icon, r, ac.on, tip, ac.act)
}

// ---------------------------------------------------------------- панель инструментов

func (a *App) paintToolbar(cv *Canvas, r Rect) {
	th := a.th
	cv.FillRect(r, th.Bg)
	cv.FillRect(Rect{0, r.Y + r.H - math.Max(1, a.S), a.W, math.Max(1, a.S)}, th.Divider)
	acts := a.actionsByID()
	bh := a.d(30)
	y := r.Y + (r.H-bh)/2
	cy := r.Y + r.H/2
	x := a.d(12)
	btn := func(id string) {
		a.actBtn(cv, acts[id], Rect{x, y, a.d(32), bh})
		x += a.d(33)
	}
	sep := func() {
		x += a.d(6)
		a.vline(cv, x, cy, a.d(18))
		x += a.d(7)
	}
	// справа: панель инспектора и вставка
	rx := a.W - a.d(12)
	rbtn := func(id string) {
		rx -= a.d(33)
		a.actBtn(cv, acts[id], Rect{rx, y, a.d(32), bh})
	}
	if a.pdf != nil {
		for _, id := range []string{"open", "print", "copy"} {
			btn(id)
		}
		sep()
		for _, id := range []string{"pdffirst", "pdfprev", "pdfnext"} {
			btn(id)
		}
		n := len(a.pdf.Doc.Pages)
		lbl := fmt.Sprintf("%d / %d", a.pdfCurPage()+1, n)
		w := a.measure(lbl, a.monoFont(12.5)) + a.d(20)
		pr := Rect{x + a.d(4), y, w, bh}
		a.hits = append(a.hits, hit{id: "x:goto", r: pr, act: func() { a.gotoPageDialog() }, tip: a.tr("Перейти к странице…")})
		a.hoverBg(cv, "x:goto", pr, a.d(5))
		a.centerText(cv, lbl, pr, a.monoFont(12.5), th.Text)
		x += w + a.d(8)
		sep()
		for _, id := range []string{"zoomout", "zoom100", "zoomin", "fitwidth", "fitpage"} {
			btn(id)
		}
		sep()
		btn("find")
		return
	}
	for _, id := range []string{"undo", "redo", "save"} {
		btn(id)
	}
	sep()
	// стиль, шрифт, размер — если хватает места
	free := a.W - a.d(12) - x - a.d(33)*4 - a.d(12)
	need := a.d(33)*13 + a.d(60)
	if free-need > a.d(150) {
		cr := Rect{x, y, a.d(140), bh}
		a.combo(cv, "x:stylemenu", a.curStyleName(), cr, "", func(r Rect) { a.styleMenu(r) })
		x += cr.W + a.d(6)
		free -= cr.W + a.d(6)
	}
	if free-need > a.d(210) {
		ef := a.E.EffectiveF()
		fr := Rect{x, y, a.d(150), bh}
		a.combo(cv, "x:font", ef.Font, fr, ef.Font, a.fontPopup)
		x += fr.W + a.d(4)
		sr := Rect{x, y, a.d(54), bh}
		a.combo(cv, "x:size", strings.ReplaceAll(strconv.FormatFloat(ef.Size, 'f', -1, 64), ".", ","), sr, "", a.sizePopup)
		x += sr.W + a.d(4)
	}
	sep()
	for _, id := range []string{"bold", "italic", "under"} {
		btn(id)
	}
	sep()
	for _, id := range []string{"bullets", "numbering"} {
		btn(id)
	}
	sep()
	for _, id := range []string{"al:left", "al:center", "al:right", "al:justify"} {
		btn(id)
	}
	rbtn("inspector")
	rx -= a.d(6)
	a.vline(cv, rx, cy, a.d(18))
	rx -= a.d(7)
	for _, id := range []string{"eq", "picture", "table"} {
		rbtn(id)
	}
	// режим правки колонтитула или сноски — заметная плашка
	var badge string
	var back func()
	switch {
	case a.E.Story == "header" || a.E.Story == "footer":
		badge = map[bool]string{true: "Верхний колонтитул", false: "Нижний колонтитул"}[a.E.Story == "header"]
		back = func() { a.E.SetStory("body", 0); a.after() }
	case strings.HasPrefix(a.E.Story, "fn:"):
		badge = "Сноска"
		back = func() { a.E.SetStory("body", 0); a.after() }
	}
	if badge != "" {
		text := a.tr(badge) + "  ·  Esc"
		f := a.uiFont(12)
		w := a.measure(text, f) + a.d(24)
		br := Rect{rx - w - a.d(10), y + a.d(3), w, bh - a.d(6)}
		if br.X > x+a.d(10) {
			a.hits = append(a.hits, hit{id: "x:story", r: br, act: back, tip: a.tr("Вернуться к основному тексту")})
			cv.FillRound(br, a.d(5), th.Accent.WithA(0.18+0.08*a.anim["x:story"]))
			a.centerText(cv, text, br, f, th.Accent2)
		}
	}
}

// ---------------------------------------------------------------- полоса слева

func (a *App) paintRail(cv *Canvas, r Rect) {
	th := a.th
	cv.FillRect(r, th.Bg)
	cv.FillRect(Rect{r.X + r.W - math.Max(1, a.S), r.Y, math.Max(1, a.S), r.H}, th.Divider)
	acts := a.actionsByID()
	bw := a.d(36)
	x := r.X + (r.W-bw)/2
	y := r.Y + a.d(8)
	item := func(id string, on bool) {
		ac := acts[id]
		if ac == nil {
			return
		}
		br := Rect{x, y, bw, bw}
		if on {
			cv.FillRound(Rect{r.X, br.Y + a.d(8), a.d(2.5), bw - a.d(16)}, a.d(1), th.Accent)
		}
		c := *ac
		c.on = on
		a.actBtn(cv, &c, br)
		y += bw + a.d(4)
	}
	nav := a.Set.ShowNav
	item("navpages", nav && a.Set.NavMode != "headings")
	item("navheads", nav && a.Set.NavMode == "headings")
	item("find", false)
	item("palette", false)
	y = r.Y + r.H - a.d(8) - 2*(bw+a.d(4))
	item("home", false)
	item("settings", false)
}

// ---------------------------------------------------------------- инспектор

var inspTabs = []struct{ id, icon, label string }{{"format", "style", "Формат"}, {"insert", "plus", "Вставка"},
	{"page", "margins", "Страница"}, {"refs", "cite", "Ссылки"}, {"review", "check", "Проверка"}, {"view", "zoomin", "Вид"}}

// inspTab — текущая вкладка инспектора (старые имена вкладок ленты тоже понимаем).
func (a *App) inspTab() string {
	switch a.tab {
	case "", "home", "file":
		return "format"
	case "layout":
		return "page"
	case "help":
		return "view"
	}
	return a.tab
}

type inspector struct {
	a        *App
	cv       *Canvas
	x, w     float64
	y        float64 // текущая строка (с учётом прокрутки)
	body     Rect
	sections int
}

func (s *inspector) visible(r Rect) bool { return r.Y+r.H > s.body.Y && r.Y < s.body.Y+s.body.H }

func (s *inspector) section(title string) {
	a := s.a
	if s.sections > 0 {
		s.y += a.d(10)
		s.cv.FillRect(Rect{s.x, s.y, s.w, math.Max(1, a.S)}, a.th.Divider)
		s.y += a.d(12)
	} else {
		s.y += a.d(4)
	}
	s.sections++
	f := a.uiFont(10.5)
	f.Bold = true
	a.textAt(s.cv, strings.ToUpper(a.tr(title)), s.x, s.y, f, a.th.TextDim)
	s.y += a.d(24)
}

// icons — ряд квадратных кнопок, переносится по ширине.
func (s *inspector) icons(ids ...string) {
	a := s.a
	acts := a.actionsByID()
	gap := a.d(3)
	per := min(len(ids), 8) // до восьми в строку: кнопки чуть сужаются, но ряд не рвётся на один хвост
	bw := math.Min(a.d(32), (s.w-gap*float64(per-1))/float64(per))
	for i, id := range ids {
		if i > 0 && i%per == 0 {
			s.y += bw + gap
		}
		r := Rect{s.x + float64(i%per)*(bw+gap), s.y, bw, bw}
		if acts[id] == nil && inspMissing != nil {
			inspMissing(id)
		}
		if s.visible(r) {
			a.actBtn(s.cv, acts[id], r)
		}
	}
	s.y += bw + a.d(6)
}

// inspMissing — для тестов: инспектор сообщает о кнопках без команды (опечатка в id).
var inspMissing func(id string)

// item — строка «значок, название, сочетание или переключатель».
func (s *inspector) item(id string) {
	a := s.a
	ac := a.actionsByID()[id]
	if ac == nil {
		if inspMissing != nil {
			inspMissing(id)
		}
		return
	}
	th := a.th
	r := Rect{s.x - a.d(6), s.y, s.w + a.d(12), a.d(30)}
	s.y += a.d(31)
	if !s.visible(r) {
		return
	}
	hid := "x:" + ac.id
	col := th.Text
	act := ac.act
	tip := ""
	if ac.off != "" {
		why := ac.off
		act = func() { a.toast(why) }
		col = th.TextDim.WithA(0.55)
		tip = a.tr(why)
	}
	a.hits = append(a.hits, hit{id: hid, r: r, act: act, tip: tip})
	a.hoverBg(s.cv, hid, r, a.d(5))
	if ac.on && !ac.toggle {
		s.cv.FillRound(Rect{r.X, r.Y + a.d(8), a.d(2.5), r.H - a.d(16)}, a.d(1), th.Accent)
	}
	ic := col
	if ac.on && !ac.toggle {
		ic = th.Accent2
	}
	a.icon(s.cv, ac.icon, Rect{r.X + a.d(6), r.Y + a.d(5), a.d(20), a.d(20)}, ic)
	label := a.tr(ac.label)
	f := a.uiFont(12.5)
	maxW := r.W - a.d(40)
	rightW := 0.0
	switch {
	case ac.toggle:
		rightW = a.d(34)
		sw := Rect{r.X + r.W - a.d(38), r.Y + (r.H-a.d(16))/2, a.d(28), a.d(16)}
		if ac.on {
			s.cv.FillRound(sw, sw.H/2, th.Accent)
			s.cv.FillCircle(sw.X+sw.W-sw.H/2, sw.Y+sw.H/2, sw.H/2-a.d(3), Hex(0xffffff))
		} else {
			s.cv.StrokeRound(sw, sw.H/2, math.Max(1, a.S), th.Border)
			s.cv.FillCircle(sw.X+sw.H/2, sw.Y+sw.H/2, sw.H/2-a.d(3.5), th.TextDim)
		}
	case ac.keys != "" && ac.off == "":
		kf := a.monoFont(11)
		kw := a.measure(ac.keys, kf)
		if a.measure(label, f)+kw+a.d(56) < r.W {
			rightW = kw + a.d(12)
			a.textAt(s.cv, ac.keys, r.X+r.W-a.d(8)-kw, r.Y+a.d(8), kf, th.TextDim)
		}
	}
	clip := s.cv.clip
	s.cv.SetClip(intersect(Rect{r.X, r.Y, maxW - rightW + a.d(30), r.H}, clip))
	a.textAt(s.cv, label, r.X+a.d(34), r.Y+a.d(6), f, col)
	s.cv.SetClip(clip)
}

// custom — своя строка высотой h.
func (s *inspector) custom(h float64, draw func(r Rect)) {
	r := Rect{s.x, s.y, s.w, h}
	s.y += h + s.a.d(6)
	if s.visible(r) {
		draw(r)
	}
}

func (a *App) paintInspector(cv *Canvas, r Rect) {
	th := a.th
	cv.FillRect(r, th.Nav)
	cv.FillRect(Rect{r.X, r.Y, math.Max(1, a.S), r.H}, th.Divider)
	a.addHit("insp", r, 0, nil)
	// вкладки-значки
	tab := a.inspTab()
	hh := a.d(42)
	tw := (r.W - a.d(16)) / float64(len(inspTabs))
	for i, t := range inspTabs {
		t := t
		tr := Rect{r.X + a.d(8) + float64(i)*tw, r.Y + a.d(5), tw, hh - a.d(10)}
		id := "it:" + t.id
		on := tab == t.id
		a.hits = append(a.hits, hit{id: id, r: tr, tip: a.tr(t.label), act: func() { a.tab = t.id; a.inspScroll = 0 }})
		a.hoverBg(cv, id, tr, a.d(5))
		col := th.TextDim
		if on {
			col = th.Text
			cv.FillRound(Rect{tr.X + tr.W/2 - a.d(9), r.Y + hh - a.d(2.5), a.d(18), a.d(2)}, a.d(1), th.Accent)
		}
		a.icon(cv, t.icon, tr, col)
	}
	cv.FillRect(Rect{r.X, r.Y + hh, r.W, math.Max(1, a.S)}, th.Divider)
	body := Rect{r.X, r.Y + hh + math.Max(1, a.S), r.W, r.H - hh - math.Max(1, a.S)}
	a.inspBody = body
	clip := cv.clip
	cv.SetClip(intersect(body, clip))
	pad := a.d(16)
	s := &inspector{a: a, cv: cv, x: r.X + pad, w: r.W - 2*pad, y: body.Y + a.d(10) - a.inspScroll, body: body}
	start := s.y
	a.inspContent(s, tab)
	contentH := s.y - start + a.d(24)
	a.inspScroll = math.Max(0, math.Min(a.inspScroll, contentH-body.H))
	if contentH > body.H {
		ratio := body.H / contentH
		bar := math.Max(a.d(24), body.H*ratio)
		pos := a.inspScroll / math.Max(1, contentH-body.H) * (body.H - bar)
		cv.FillRound(Rect{r.X + r.W - a.d(5), body.Y + pos, a.d(3), bar}, a.d(1.5), th.TextDim.WithA(0.35))
	}
	cv.SetClip(clip)
}

// paintInspStrip — свёрнутый инспектор: столбик значков вкладок.
func (a *App) paintInspStrip(cv *Canvas, r Rect) {
	th := a.th
	cv.FillRect(r, th.Bg)
	cv.FillRect(Rect{r.X, r.Y, math.Max(1, a.S), r.H}, th.Divider)
	bw := a.d(34)
	y := r.Y + a.d(8)
	tab := a.inspTab()
	for _, t := range inspTabs {
		t := t
		br := Rect{r.X + (r.W-bw)/2, y, bw, bw}
		id := "is:" + t.id
		on := a.inspPeek && tab == t.id
		a.hits = append(a.hits, hit{id: id, r: br, tip: a.tr(t.label), act: func() {
			if a.inspPeek && a.tab == t.id {
				a.inspPeek = false
				return
			}
			a.tab, a.inspPeek, a.inspScroll = t.id, true, 0
		}})
		if on {
			cv.FillRound(Rect{r.X + r.W - a.d(2.5), br.Y + a.d(8), a.d(2.5), bw - a.d(16)}, a.d(1), th.Accent)
		}
		a.hoverBg(cv, id, br, a.d(5))
		a.icon(cv, t.icon, br, mapCol(on, th.Text, th.TextDim))
		y += bw + a.d(4)
	}
}

func (a *App) inspContent(s *inspector, tab string) {
	th := a.th
	e := a.E
	switch tab {
	case "format":
		if e.TableCtx() != nil {
			s.section("Таблица")
			s.icons("t:row+", "t:row-above", "t:col+", "t:col-left", "t:merge", "t:row-", "t:col-", "t:border", "t:shade", "t:deltable")
		}
		if a.imgSel != nil {
			s.section("Рисунок")
			s.item("wrap")
			s.item("caption")
		}
		s.section("Стиль")
		s.custom(a.d(30), func(r Rect) {
			a.combo(s.cv, "x:stylemenu", a.curStyleName(), r, "", func(r Rect) { a.styleMenu(r) })
		})
		s.section("Текст")
		ef := e.EffectiveF()
		s.custom(a.d(30), func(r Rect) {
			sw := a.d(62)
			a.combo(s.cv, "x:font", ef.Font, Rect{r.X, r.Y, r.W - sw - a.d(6), r.H}, ef.Font, a.fontPopup)
			a.combo(s.cv, "x:size", strings.ReplaceAll(strconv.FormatFloat(ef.Size, 'f', -1, 64), ".", ","), Rect{r.X + r.W - sw, r.Y, sw, r.H}, "", a.sizePopup)
		})
		s.icons("bold", "italic", "under", "strike", "sub", "sup", "grow", "shrink")
		s.custom(a.d(30), func(r Rect) {
			half := (r.W - a.d(6)) / 2
			a.colorField(s.cv, "color", "Цвет", a.lastColor, Rect{r.X, r.Y, half, r.H}, func() { e.SetColor(a.lastColor); a.after() }, a.colorMenu)
			a.colorField(s.cv, "bg", "Выделение", a.lastBg, Rect{r.X + half + a.d(6), r.Y, half, r.H}, func() { e.SetBg(a.lastBg); a.after() }, a.bgMenu)
		})
		s.icons("case", "clear", "painter")
		s.section("Абзац")
		s.icons("al:left", "al:center", "al:right", "al:justify", "bullets", "numbering", "outdent", "indent")
		pf := e.CurrentPF()
		s.custom(a.d(30), func(r Rect) {
			ls := "1"
			if pf.Line > 0 {
				ls = strings.ReplaceAll(strconv.FormatFloat(pf.Line, 'f', -1, 64), ".", ",")
			}
			a.valueField(s.cv, "spacing", "spacing", "Интервал", ls, Rect{r.X, r.Y, (r.W - a.d(6)) / 2, r.H}, func() { a.spacingPopup() })
			ind := "0"
			if pf.IndF > 0 {
				ind = strings.ReplaceAll(strconv.FormatFloat(math.Round(pf.IndF/CM*100)/100, 'f', -1, 64), ".", ",") + " см"
			}
			a.valueField(s.cv, "firstline", "firstline", "Строка", ind, Rect{r.X + (r.W+a.d(6))/2, r.Y, (r.W - a.d(6)) / 2, r.H}, a.actionsByID()["firstline"].act)
		})
		s.item("para")
		s.section("Правка")
		s.icons("paste", "cut", "copy", "find", "replace", "selectall")
	case "insert":
		s.section("Объекты")
		for _, id := range []string{"table", "picture", "eq", "symbol", "date", "link"} {
			s.item(id)
		}
		s.section("Разрывы")
		s.item("pagebreak")
		s.item("colbreak")
		s.item("breaks")
		s.section("Колонтитулы")
		for _, id := range []string{"header", "footer", "pagenum", "numpages", "titlepg", "evenodd", "pagenumfmt"} {
			s.item(id)
		}
	case "page":
		s.section("Лист")
		sec := e.CurSection()
		pg := sec.Page
		s.custom(a.d(44), func(r Rect) {
			orient := "книжная"
			if pg.W > pg.H {
				orient = "альбомная"
			}
			lines := []string{fmt.Sprintf("%s × %s мм · %s", fmtNum(math.Round(pg.W/(CM/10))), fmtNum(math.Round(pg.H/(CM/10))), a.tr(orient)),
				fmt.Sprintf("%s %s · %s %s · %s %s · %s %s", a.tr("лев"), fmtNum(math.Round(pg.Left/(CM/10))), a.tr("прав"), fmtNum(math.Round(pg.Right/(CM/10))),
					a.tr("верх"), fmtNum(math.Round(pg.Top/(CM/10))), a.tr("низ"), fmtNum(math.Round(pg.Bottom/(CM/10))))}
			a.textAt(s.cv, lines[0], r.X, r.Y, a.monoFont(11.5), th.Text)
			a.textAt(s.cv, lines[1], r.X, r.Y+a.d(20), a.monoFont(11), th.TextDim)
		})
		for _, id := range []string{"margins", "orient", "pagesize", "columns", "pagesetup"} {
			s.item(id)
		}
		s.section("Текст")
		s.item("para")
		s.item("breaks")
		s.item("hyph")
	case "refs":
		s.section("Оглавление")
		s.item("toc")
		s.item("update")
		s.section("Литература")
		s.item("sources")
		s.item("cite")
		s.item("bib")
		s.item("bibimport")
		s.section("Сноски")
		s.item("footnote")
		s.item("nextfn")
		s.section("Названия")
		s.item("caption")
		s.item("xref")
		s.item("lof")
	case "review":
		s.section("Нормоконтроль")
		s.item("normcheck")
		a.normPanel(s)
		s.section("Правописание")
		for _, id := range []string{"spell", "spellon", "nospell", "stats"} {
			s.item(id)
		}
		s.section("Примечания")
		s.item("cmt")
		s.icons("cmtprev", "cmtnext", "cmtdel")
		s.section("Сравнение")
		s.item("compare")
		s.section("Исправления")
		s.item("track")
		s.item("markup")
		s.icons("revprev", "revnext", "accept", "reject")
		s.item("acceptall")
		s.item("rejectall")
	case "view":
		s.section("Масштаб")
		s.custom(a.d(32), func(r Rect) {
			acts := a.actionsByID()
			bw := a.d(32)
			a.actBtn(s.cv, acts["zoomout"], Rect{r.X, r.Y, bw, bw})
			zr := Rect{r.X + bw + a.d(4), r.Y, a.d(64), bw}
			a.hits = append(a.hits, hit{id: "x:zoom100", r: zr, act: acts["zoom100"].act, tip: a.tr("Масштаб 100%")})
			a.hoverBg(s.cv, "x:zoom100", zr, a.d(5))
			a.centerText(s.cv, fmt.Sprintf("%d%%", int(a.zoom*100+0.5)), zr, a.monoFont(12.5), th.Text)
			a.actBtn(s.cv, acts["zoomin"], Rect{zr.X + zr.W + a.d(4), r.Y, bw, bw})
			a.actBtn(s.cv, acts["fitwidth"], Rect{r.X + r.W - 2*bw - a.d(3), r.Y, bw, bw})
			a.actBtn(s.cv, acts["fitpage"], Rect{r.X + r.W - bw, r.Y, bw, bw})
		})
		s.section("Панели")
		s.item("nav")
		s.item("inspector")
		s.item("grid")
		s.section("Тема")
		for _, t := range themeNames {
			s.item("theme:" + t.id)
		}
		s.section("Справка")
		s.item("palette")
		s.item("keys")
		s.item("about")
	}
}

// colorField — образец цвета (применить) и стрелка (выбрать другой).
func (a *App) colorField(cv *Canvas, id, label, color string, r Rect, apply func(), menu func(Rect)) {
	th := a.th
	main := Rect{r.X, r.Y, r.W - a.d(24), r.H}
	arrow := Rect{r.X + r.W - a.d(24), r.Y, a.d(24), r.H}
	a.hits = append(a.hits, hit{id: "x:" + id, r: main, act: apply, tip: a.tr(label)})
	a.hits = append(a.hits, hit{id: "x:" + id + ":m", r: arrow, act: func() { menu(r) }, tip: a.tr(label) + "…"})
	cv.FillRound(r, a.d(5), th.Field)
	cv.StrokeRound(r, a.d(5), math.Max(1, a.S), th.Border)
	a.hoverBg(cv, "x:"+id, main, a.d(5))
	a.hoverBg(cv, "x:"+id+":m", arrow, a.d(5))
	sw := Rect{main.X + a.d(8), main.Y + (main.H-a.d(14))/2, a.d(14), a.d(14)}
	cv.FillRound(sw, a.d(3), parseHexColor(color, Hex(0)))
	cv.StrokeRound(sw, a.d(3), math.Max(1, a.S), th.Border)
	a.textAt(cv, label, sw.X+sw.W+a.d(8), main.Y+(main.H-a.d(17))/2, a.uiFont(12), th.Text)
	a.icon(cv, "chevron", Rect{arrow.X + a.d(6), arrow.Y + (arrow.H-a.d(12))/2, a.d(12), a.d(12)}, th.TextDim)
}

// valueField — значение с подписью (интервал, красная строка): щелчок открывает выбор.
func (a *App) valueField(cv *Canvas, id, icon, label, value string, r Rect, act func()) {
	th := a.th
	a.hits = append(a.hits, hit{id: "x:" + id, r: r, act: act, tip: a.tr(label)})
	cv.FillRound(r, a.d(5), th.Field)
	cv.StrokeRound(r, a.d(5), math.Max(1, a.S), th.Border)
	a.hoverBg(cv, "x:"+id, r, a.d(5))
	a.icon(cv, icon, Rect{r.X + a.d(4), r.Y + (r.H-a.d(18))/2, a.d(18), a.d(18)}, th.TextDim)
	vf := a.monoFont(12)
	vw := a.measure(value, vf)
	a.textAt(cv, value, r.X+r.W-a.d(8)-vw, r.Y+(r.H-a.d(16))/2, vf, th.Text)
}

// ---------------------------------------------------------------- палитра команд

func paletteNorm(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "ё", "е")
}

func (a *App) openPalette() {
	a.refreshActions()
	acts := a.acts
	q := ""
	sel := 0
	scroll := 0
	var vis []*action
	recompute := func() {
		vis = vis[:0]
		words := strings.Fields(paletteNorm(q))
		type scored struct {
			ac *action
			s  int
		}
		var sc []scored
		for i, ac := range acts {
			hay := paletteNorm(a.tr(ac.label) + " " + ac.label + " " + a.tr(ac.group) + " " + ac.keys + " " + ac.id)
			ok := true
			for _, w := range words {
				if !strings.Contains(hay, w) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			s := i
			lab := paletteNorm(a.tr(ac.label))
			if len(words) > 0 {
				switch {
				case strings.HasPrefix(lab, words[0]):
					s -= 2000
				case strings.Contains(lab, words[0]):
					s -= 1000
				}
			} else {
				for k, id := range a.palRecent {
					if id == ac.id {
						s = -100 + k
					}
				}
			}
			if ac.off != "" {
				s += 5000
			}
			sc = append(sc, scored{ac, s})
		}
		sort.SliceStable(sc, func(i, j int) bool { return sc[i].s < sc[j].s })
		for _, x := range sc {
			vis = append(vis, x.ac)
		}
		sel, scroll = 0, 0
	}
	recompute()
	w := math.Min(a.d(640), a.W-a.d(40))
	inputH, rowH, footH := a.d(48), a.d(34), a.d(28)
	maxRows := 10
	p := &Popup{Owner: "palette", hover: -1}
	layout := func() {
		n := max(1, min(len(vis), maxRows))
		p.R = Rect{(a.W - w) / 2, a.d(64), w, inputH + float64(n)*rowH + a.d(8) + footH}
	}
	layout()
	run := func(ac *action) {
		a.popup = nil
		if ac.off != "" {
			a.toast(ac.off)
			return
		}
		rec := []string{ac.id}
		for _, id := range a.palRecent {
			if id != ac.id && len(rec) < 6 {
				rec = append(rec, id)
			}
		}
		a.palRecent = rec
		ac.act()
		a.invalidate()
	}
	ensure := func() {
		if sel < scroll {
			scroll = sel
		} else if sel >= scroll+maxRows {
			scroll = sel - maxRows + 1
		}
	}
	rowAt := func(x, y float64) int {
		top := p.R.Y + inputH + a.d(4)
		if y < top || !p.R.Contains(x, y) {
			return -1
		}
		i := scroll + int((y-top)/rowH)
		if i >= len(vis) || i >= scroll+maxRows {
			return -1
		}
		return i
	}
	p.Draw = func(cv *Canvas, p *Popup) {
		th := a.th
		layout()
		cv.FillRect(Rect{0, 0, a.W, a.H}, RGBA(0, 0, 0, uint8(map[bool]int{true: 90, false: 40}[th.Dark])))
		a.glass(cv, p.R, a.d(10))
		// строка ввода
		ir := Rect{p.R.X, p.R.Y, p.R.W, inputH}
		a.icon(cv, "find", Rect{ir.X + a.d(14), ir.Y + (inputH-a.d(20))/2, a.d(20), a.d(20)}, th.TextDim)
		f := a.uiFont(15)
		tx := ir.X + a.d(46)
		if q == "" {
			a.textAt(cv, "Команда или действие…", tx, ir.Y+(inputH-a.d(20))/2, f, th.TextDim)
			cv.FillRect(Rect{tx - a.d(1), ir.Y + a.d(14), a.d(1.5), inputH - a.d(28)}, th.Accent)
		} else {
			a.tb.Draw(cv, q, tx, ir.Y+(inputH-a.d(20))/2+a.d(15), f, th.Text)
			qw := a.tb.Measure(q, f)
			cv.FillRect(Rect{tx + qw + a.d(1), ir.Y + a.d(14), a.d(1.5), inputH - a.d(28)}, th.Accent)
		}
		kf := a.monoFont(11)
		a.keycap(cv, "Esc", ir.X+ir.W-a.d(14), ir.Y+(inputH-a.d(20))/2, kf)
		cv.FillRect(Rect{p.R.X, ir.Y + inputH, p.R.W, math.Max(1, a.S)}, th.Divider)
		// список
		top := ir.Y + inputH + a.d(4)
		if len(vis) == 0 {
			a.textAt(cv, "Ничего не найдено", p.R.X+a.d(46), top+a.d(9), a.uiFont(13), th.TextDim)
		}
		for k := 0; k < maxRows && scroll+k < len(vis); k++ {
			i := scroll + k
			ac := vis[i]
			rr := Rect{p.R.X + a.d(6), top + float64(k)*rowH, p.R.W - a.d(12), rowH}
			if i == sel {
				cv.FillRound(rr, a.d(6), th.Accent.WithA(0.17))
				cv.FillRound(Rect{rr.X, rr.Y + a.d(9), a.d(2.5), rowH - a.d(18)}, a.d(1), th.Accent)
			}
			col := th.Text
			if ac.off != "" {
				col = th.TextDim.WithA(0.6)
			}
			a.icon(cv, ac.icon, Rect{rr.X + a.d(10), rr.Y + (rowH-a.d(20))/2, a.d(20), a.d(20)}, col)
			lf := a.uiFont(13)
			lab := a.tr(ac.label)
			a.textAt(cv, lab, rr.X+a.d(40), rr.Y+(rowH-a.d(18))/2, lf, col)
			gx := rr.X + a.d(40) + a.measure(lab, lf) + a.d(10)
			if ac.on {
				a.icon(cv, "check", Rect{gx - a.d(4), rr.Y + (rowH-a.d(16))/2, a.d(16), a.d(16)}, th.Accent2)
				gx += a.d(18)
			}
			a.textAt(cv, a.tr(ac.group), gx, rr.Y+(rowH-a.d(16))/2, a.uiFont(11.5), th.TextDim)
			if ac.off != "" {
				why := a.tr(ac.off)
				wf := a.uiFont(11)
				a.textAt(cv, why, rr.X+rr.W-a.d(10)-a.measure(why, wf), rr.Y+(rowH-a.d(15))/2, wf, th.TextDim)
			} else if ac.keys != "" {
				a.keycap(cv, ac.keys, rr.X+rr.W-a.d(8), rr.Y+(rowH-a.d(20))/2, kf)
			}
		}
		// подсказка
		fy := p.R.Y + p.R.H - footH
		cv.FillRect(Rect{p.R.X, fy, p.R.W, math.Max(1, a.S)}, th.Divider)
		hint := a.tr("↑↓ выбрать   Enter выполнить   Esc закрыть")
		a.textAt(cv, hint, p.R.X+a.d(16), fy+a.d(7), a.monoFont(10.5), th.TextDim)
		cnt := fmt.Sprintf("%d", len(vis))
		a.textAt(cv, cnt, p.R.X+p.R.W-a.d(16)-a.measure(cnt, a.monoFont(10.5)), fy+a.d(7), a.monoFont(10.5), th.TextDim)
	}
	p.Move = func(x, y float64) {
		if i := rowAt(x, y); i >= 0 {
			sel = i
		}
	}
	p.Click = func(x, y float64) bool {
		if i := rowAt(x, y); i >= 0 {
			run(vis[i])
		}
		return false
	}
	p.Wheel = func(d float64) {
		if d < 0 {
			scroll = min(scroll+1, max(0, len(vis)-maxRows))
		} else {
			scroll = max(0, scroll-1)
		}
	}
	p.Key = func(vk int, m Mods) bool {
		switch vk {
		case 38:
			if len(vis) > 0 {
				sel = (sel - 1 + len(vis)) % len(vis)
				ensure()
			}
		case 40:
			if len(vis) > 0 {
				sel = (sel + 1) % len(vis)
				ensure()
			}
		case 33:
			sel = max(0, sel-maxRows)
			ensure()
		case 34:
			sel = min(max(0, len(vis)-1), sel+maxRows)
			ensure()
		case 13:
			if sel < len(vis) {
				run(vis[sel])
			} else {
				a.popup = nil
			}
		case 8:
			if r := []rune(q); len(r) > 0 {
				q = string(r[:len(r)-1])
				recompute()
			}
		}
		return true
	}
	p.Char = func(r rune) {
		if r >= 32 {
			q += string(r)
			recompute()
		}
	}
	a.popup = p
}

// keycap — сочетание клавиш в рамке, выровненное по правому краю right.
func (a *App) keycap(cv *Canvas, keys string, right, y float64, f Font) {
	th := a.th
	w := a.measure(keys, f) + a.d(12)
	r := Rect{right - w, y, w, a.d(20)}
	cv.FillRound(r, a.d(4), th.Field)
	cv.StrokeRound(r, a.d(4), math.Max(1, a.S), th.Border)
	a.centerText(cv, keys, r, f, th.TextDim)
}

// setColumns — колонки раздела с курсором: n > 0 — число колонок, sep 0/1 — линия (−1 — не трогать).
func (a *App) setColumns(n, sep int) {
	e := a.E
	e.Checkpoint("")
	e.SetSection(func(s *SecView) {
		if n > 0 {
			s.Cols.N = n
			if n == 1 {
				s.Cols = Columns{}
			}
		}
		if sep >= 0 {
			s.Cols.Sep = sep == 1
		}
	})
	e.Changed()
	a.after()
}

func (a *App) columnsPopup(r Rect) {
	cols := a.E.CurSection().Cols
	var items []PopItem
	for n := 1; n <= 3; n++ {
		n := n
		items = append(items, PopItem{Label: []string{"", "Одна", "Две", "Три"}[n], Icon: "columns", On: cols.Count() == n, Act: func() { a.setColumns(n, -1) }})
	}
	items = append(items, PopItem{Sep: true})
	items = append(items, PopItem{Label: "Линия между колонками", Icon: map[bool]string{true: "check", false: "columns"}[cols.Sep], On: cols.Sep,
		Act: func() { a.setColumns(max(2, cols.Count()), map[bool]int{true: 0, false: 1}[cols.Sep]) }})
	a.popup = a.listPopup("columns", r.X, r.Y+r.H, math.Max(r.W, a.d(220)), items, a.d(300), false)
}
