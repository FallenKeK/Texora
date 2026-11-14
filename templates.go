package main

import "strconv"

// Шаблоны документов Texora: курсовая, диплом, реферат, доклад (по ГОСТ 7.32 / требованиям вузов), деловое письмо, отчёт.

type tplInfo struct{ id, name, desc string }

var templatesList = []tplInfo{
	{"blank", "Пустой документ", "Чистый лист: Calibri 11, поля как в Word"},
	{"course", "Курсовая работа", "Титульный лист, содержание, введение, две главы, заключение, список литературы. ГОСТ: Times New Roman 14, интервал 1,5"},
	{"thesis", "Дипломная работа", "ВКР: титул с подписями, содержание, три главы с выводами, заключение, источники, приложение"},
	{"essay", "Реферат", "Титульный лист, содержание, введение, два раздела, заключение и список литературы"},
	{"talk", "Доклад", "Тема, автор, введение, основная часть, выводы и источники — без титульного листа"},
	{"letter", "Деловое письмо", "Бланк организации, исходящий номер, адресат, обращение, подпись, исполнитель"},
	{"report", "Отчёт", "Современный отчёт: резюме, цели, таблица результатов, выводы и следующие шаги"},
}

type docB struct {
	d    *Doc
	font string
	size float64
}

func (b *docB) add(pf PF, runs ...Run) *Para {
	p := &Para{PF: pf, Runs: runs}
	b.d.Body = append(b.d.Body, Block{P: p})
	return p
}

func (b *docB) f(mod func(*F)) F {
	f := F{}
	if mod != nil {
		mod(&f)
	}
	return f
}

func tr(s string, f F) Run { return TextRun(s, f) }

func gostDoc() (*docB, *Doc) {
	d := NewDoc()
	d.Body = nil
	d.Def = Defaults{Font: "Times New Roman", Size: 14, Line: 1.5, After: 0}
	d.Page.Top, d.Page.Bottom, d.Page.Left, d.Page.Right = 2*CM, 2*CM, 3*CM, 1.5*CM
	d.Page.Header, d.Page.Footer = 1.25*CM, 1.25*CM
	d.Footer = []Block{{P: &Para{PF: PF{Align: "center", IndF: O(0)}, Runs: []Run{{T: RField, Name: "PAGE"}}}}}
	// стили по ГОСТ 7.32: заголовки — тем же шрифтом, чёрные, полужирные
	tnr := "Times New Roman"
	d.Styles["h1"] = headingStyle{Size: 14, B: true, Color: "#000000", Font: tnr, Align: "center", Before: 0, After: 12, Set: true}
	d.Styles["h2"] = headingStyle{Size: 14, B: true, Color: "#000000", Font: tnr, Align: "justify", Before: 12, After: 6, IndF: 1.25 * CM, Set: true}
	d.Styles["h3"] = headingStyle{Size: 14, B: true, Color: "#000000", Font: tnr, Align: "justify", Before: 6, After: 6, IndF: 1.25 * CM, Set: true}
	d.Styles["TOCHeading"] = headingStyle{Size: 14, B: true, Color: "#000000", Font: tnr, Align: "center", Before: 0, After: 12, Set: true}
	d.Styles["TOC1"] = headingStyle{Before: 0, After: 0, Set: true}
	d.Styles["TOC2"] = headingStyle{Before: 0, After: 0, IndL: 0.5 * CM, Set: true}
	d.Styles["TOC3"] = headingStyle{Before: 0, After: 0, IndL: 1 * CM, Set: true}
	return &docB{d: d}, d
}

var (
	fBold   = F{B: true}
	fPlain  = F{}
	pCenter = PF{Align: "center", IndF: O(0)}
	pRight  = PF{Align: "right", IndF: O(0)}
	pLeft   = PF{Align: "left", IndF: O(0)}
	pBody   = PF{Align: "justify", IndF: O(1.25 * CM)}
)

func (b *docB) body(texts ...string) {
	for _, t := range texts {
		b.add(pBody, tr(t, fPlain))
	}
}

// h1 — структурный элемент ГОСТ: с новой страницы, по центру, прописными, полужирный.
func (b *docB) h1(text string) *Para {
	return b.add(PF{Heading: 1, PB: true}, tr(text, fPlain))
}

func (b *docB) h2(text string) *Para {
	return b.add(PF{Heading: 2}, tr(text, fPlain))
}

func (b *docB) blank(n int) {
	for i := 0; i < n; i++ {
		b.add(pCenter)
	}
}

func (b *docB) sources(id string, items ...string) {
	for _, s := range items {
		b.add(PF{Align: "justify", List: &List{ID: id, Kind: "ol", Fmt: "decimal", Text: "%1."}}, tr(s, fPlain))
	}
}

// toc — настоящее оглавление (поле TOC): заполняется в конце сборки шаблона.
func (b *docB) toc(title string, pb bool) func(*Measurer) {
	b.add(PF{Style: "TOCHeading", PB: pb}, tr(title, fPlain))
	b.add(PF{Region: "toc"})
	return func(*Measurer) { UpdateTOC(b.d) }
}

func (a *App) templateDoc(id string) *Doc {
	switch id {
	case "course":
		return courseDoc(a.M, false)
	case "thesis":
		return courseDoc(a.M, true)
	case "essay":
		return essayDoc(a.M)
	case "talk":
		return talkDoc()
	case "letter":
		return letterDoc()
	case "report":
		return reportDoc()
	}
	return NewDoc()
}

func courseDoc(m *Measurer, thesis bool) *Doc {
	b, d := gostDoc()
	d.TitlePg = true
	b.add(pCenter, tr("Министерство науки и высшего образования Российской Федерации", fPlain))
	if thesis {
		b.add(pCenter, tr("Федеральное государственное бюджетное образовательное учреждение высшего образования", fPlain))
	}
	b.add(pCenter, tr("«[Полное наименование университета]»", fPlain))
	b.add(pCenter, tr("[Институт / факультет]", fPlain))
	b.add(pCenter, tr("Кафедра [название кафедры]", fPlain))
	if thesis {
		b.blank(1)
		b.add(pRight, tr("Допустить к защите", fPlain))
		b.add(pRight, tr("Заведующий кафедрой", fPlain))
		b.add(pRight, tr("__________ [И. О. Фамилия]", fPlain))
		b.add(pRight, tr("«___» __________ 2026 г.", fPlain))
		b.blank(1)
		b.add(pCenter, tr("ВЫПУСКНАЯ КВАЛИФИКАЦИОННАЯ РАБОТА", F{B: true, Size: 16}))
		b.add(pCenter, tr("на тему: «[Тема работы]»", fPlain))
		b.add(pCenter, tr("Направление подготовки [00.00.00 Название направления]", fPlain))
		b.blank(2)
	} else {
		b.blank(4)
		b.add(pCenter, tr("КУРСОВАЯ РАБОТА", F{B: true, Size: 16}))
		b.add(pCenter, tr("по дисциплине «[Название дисциплины]»", fPlain))
		b.add(pCenter, tr("на тему: «[Тема работы]»", fPlain))
		b.blank(4)
	}
	ind := PF{Align: "left", IndL: O(8.5 * CM), IndF: O(0)}
	b.add(ind, tr("Выполнил(а): студент(ка)", fPlain))
	b.add(ind, tr("группы [номер]", fPlain))
	b.add(ind, tr("[Фамилия И. О.] __________", fPlain))
	b.add(ind, tr(map[bool]string{true: "Руководитель:", false: "Проверил(а):"}[thesis], fPlain))
	b.add(ind, tr("[должность, учёная степень]", fPlain))
	b.add(ind, tr("[Фамилия И. О.] __________", fPlain))
	if thesis {
		b.add(ind, tr("Нормоконтролёр:", fPlain))
		b.add(ind, tr("[Фамилия И. О.] __________", fPlain))
	}
	b.blank(map[bool]int{true: 2, false: 5}[thesis])
	b.add(pCenter, tr("[Город] 2026", fPlain))

	fill := b.toc("СОДЕРЖАНИЕ", true)
	b.h1("ВВЕДЕНИЕ")
	b.body("[Актуальность темы исследования. Объясните, почему выбранная тема важна сейчас и какие проблемы она помогает решить.]",
		"Цель работы — [сформулируйте цель одним предложением].",
		"Для достижения цели поставлены задачи: [перечислите 3–5 задач, соответствующих параграфам работы].",
		"Объект исследования — [что изучается]. Предмет исследования — [какая сторона объекта рассматривается].",
		"[Методы исследования, теоретическая и практическая значимость, структура работы: введение, "+
			map[bool]string{true: "три главы", false: "две главы"}[thesis]+", заключение, список использованных источников.]")
	chapters := [][]string{
		{"ГЛАВА 1. ТЕОРЕТИЧЕСКИЕ ОСНОВЫ [ТЕМЫ]", "1.1 Понятие и сущность [предмета исследования]", "1.2 Подходы и методы [изучения]", "1.3 Нормативная база [при необходимости]"},
		{"ГЛАВА 2. АНАЛИЗ [ОБЪЕКТА ИССЛЕДОВАНИЯ]", "2.1 Характеристика [объекта]", "2.2 Анализ [показателей / практики]", "2.3 Выявленные проблемы"},
	}
	if thesis {
		chapters = append(chapters, []string{"ГЛАВА 3. РАЗРАБОТКА ПРЕДЛОЖЕНИЙ [ПО ТЕМЕ]", "3.1 Предлагаемые мероприятия", "3.2 Оценка эффективности"})
	} else {
		chapters[0] = chapters[0][:3]
		chapters[1] = chapters[1][:3]
	}
	for ci, ch := range chapters {
		b.h1(ch[0])
		for _, sub := range ch[1:] {
			b.h2(sub)
			b.body("[Текст параграфа. Каждый абзац начинается с красной строки 1,25 см, выравнивание по ширине.]",
				"[Ссылки на источники оформляйте в квадратных скобках, например: [1, с. 25]; на все источники списка должны быть ссылки: [2], [3].]")
		}
		if thesis {
			b.add(pBody, tr("Выводы по главе "+strconv.Itoa(ci+1)+": ", F{B: true}), tr("[кратко подведите итоги главы].", fPlain))
		}
	}
	b.h1("ЗАКЛЮЧЕНИЕ")
	b.body("[Подведите итоги: насколько достигнута цель, какие результаты получены по каждой задаче, какие выводы и рекомендации можно сделать.]")
	b.h1("СПИСОК ИСПОЛЬЗОВАННЫХ ИСТОЧНИКОВ")
	b.sources("src", "[Фамилия И. О.] [Название книги] / [И. О. Фамилия]. — [Город] : [Издательство], [год]. — [число] с.",
		"[Фамилия И. О.] [Название статьи] // [Название журнала]. — [год]. — № [номер]. — С. [страницы].",
		"[Название сайта] : [сайт]. — URL: [адрес] (дата обращения: [дд.мм.гггг]).")
	if thesis {
		b.h1("ПРИЛОЖЕНИЕ А")
		b.add(pCenter, tr("[Название приложения]", F{B: true}))
		b.body("[Таблицы, схемы, расчёты и другие материалы, которые не вошли в основной текст.]")
	}
	fill(m)
	return d
}

func essayDoc(m *Measurer) *Doc {
	b, d := gostDoc()
	d.TitlePg = true
	b.add(pCenter, tr("Министерство науки и высшего образования Российской Федерации", fPlain))
	b.add(pCenter, tr("«[Наименование учебного заведения]»", fPlain))
	b.add(pCenter, tr("Кафедра [название кафедры]", fPlain))
	b.blank(6)
	b.add(pCenter, tr("РЕФЕРАТ", F{B: true, Size: 16}))
	b.add(pCenter, tr("по дисциплине «[Название дисциплины]»", fPlain))
	b.add(pCenter, tr("на тему: «[Тема реферата]»", fPlain))
	b.blank(5)
	ind := PF{Align: "left", IndL: O(8.5 * CM), IndF: O(0)}
	b.add(ind, tr("Выполнил(а): студент(ка)", fPlain))
	b.add(ind, tr("группы [номер]", fPlain))
	b.add(ind, tr("[Фамилия И. О.]", fPlain))
	b.add(ind, tr("Проверил(а):", fPlain))
	b.add(ind, tr("[Фамилия И. О.]", fPlain))
	b.blank(4)
	b.add(pCenter, tr("[Город] 2026", fPlain))
	fill := b.toc("СОДЕРЖАНИЕ", true)
	b.h1("ВВЕДЕНИЕ")
	b.body("[Почему выбрана тема, цель и задачи реферата, какие источники использованы.]")
	b.h1("1 [НАЗВАНИЕ ПЕРВОГО РАЗДЕЛА]")
	b.body("[Основное содержание раздела со ссылками на источники [1].]")
	b.h1("2 [НАЗВАНИЕ ВТОРОГО РАЗДЕЛА]")
	b.body("[Основное содержание раздела со ссылками на источники [2].]")
	b.h1("ЗАКЛЮЧЕНИЕ")
	b.body("[Главные выводы по теме реферата.]")
	b.h1("СПИСОК ИСПОЛЬЗОВАННЫХ ИСТОЧНИКОВ")
	b.sources("src", "[Фамилия И. О.] [Название] / [И. О. Фамилия]. — [Город] : [Издательство], [год]. — [число] с.",
		"[Фамилия И. О.] [Название статьи] // [Журнал]. — [год]. — № [номер]. — С. [страницы].")
	fill(m)
	return d
}

func talkDoc() *Doc {
	b, _ := gostDoc()
	b.add(PF{Align: "center", IndF: O(0), After: O(6)}, tr("[ТЕМА ДОКЛАДА]", F{B: true, Size: 16}))
	b.add(PF{Align: "center", IndF: O(0), After: O(18)}, tr("[Фамилия Имя Отчество], [группа / должность, организация]", F{I: true}))
	h := func(s string) {
		b.add(PF{Heading: 2, Align: "left", IndF: O(1.25 * CM), Before: O(12), After: O(6), KeepNext: true},
			tr(s, F{B: true, Font: "Times New Roman", Size: 14, Color: "#000000"}))
	}
	h("Введение")
	b.body("[Обращение к слушателям, актуальность темы и главная мысль доклада — 3–4 предложения.]")
	h("Основная часть")
	b.body("[Первый тезис и аргументы к нему.]", "[Второй тезис, примеры, цифры.]", "[Третий тезис.]")
	h("Выводы")
	b.body("[Кратко повторите главную мысль и предложите, что делать дальше. Поблагодарите за внимание.]")
	h("Источники")
	b.sources("src", "[Автор. Название. — Город : Издательство, год.]", "[Название сайта. — URL: адрес (дата обращения: дд.мм.гггг).]")
	return b.d
}

func letterDoc() *Doc {
	d := NewDoc()
	d.Body = nil
	d.Def = Defaults{Font: "Arial", Size: 11, Line: 1.15, After: 6}
	d.Page.Top, d.Page.Bottom, d.Page.Left, d.Page.Right = 2*CM, 2*CM, 2.5*CM, 1.5*CM
	b := &docB{d: d}
	accent := "#5b21b6"
	b.add(PF{After: O(0)}, tr("[НАЗВАНИЕ ОРГАНИЗАЦИИ]", F{B: true, Size: 16, Color: accent}))
	b.add(PF{After: O(18)}, tr("[Адрес] · тел. [номер] · [e-mail] · ИНН [номер]", F{Size: 9, Color: "#595959"}))
	t := &Table{Cols: []float64{(d.Page.W - d.Page.Left - d.Page.Right) / 2, (d.Page.W - d.Page.Left - d.Page.Right) / 2}}
	left := []Block{{P: &Para{PF: PF{After: O(0)}, Runs: []Run{tr("Исх. № [___]", fPlain)}}}, {P: &Para{Runs: []Run{tr("от «___» __________ 2026 г.", fPlain)}}}}
	right := []Block{{P: &Para{PF: PF{Align: "right", After: O(0)}, Runs: []Run{tr("[Должность адресата]", fPlain)}}},
		{P: &Para{PF: PF{Align: "right", After: O(0)}, Runs: []Run{tr("[Название организации]", fPlain)}}},
		{P: &Para{PF: PF{Align: "right"}, Runs: []Run{tr("[И. О. Фамилия]", F{B: true})}}}}
	t.Rows = []*Row{{Cells: []*Cell{{Blocks: left, Span: 1}, {Blocks: right, Span: 1}}}}
	d.Body = append(d.Body, Block{T: t})
	b.add(PF{Before: O(18), After: O(12), Align: "center"}, tr("Уважаемый(ая) [Имя Отчество]!", F{B: true}))
	for _, s := range []string{"[Первый абзац: цель письма — сообщить, предложить, попросить или подтвердить.]",
		"[Второй абзац: подробности — условия, сроки, суммы, ссылки на договоры и предыдущую переписку.]",
		"[Третий абзац: что вы ожидаете от адресата и в какой срок. Контакты для связи.]"} {
		b.add(PF{Align: "justify", IndF: O(1.25 * CM)}, tr(s, fPlain))
	}
	b.add(PF{Before: O(18)}, tr("С уважением,", fPlain))
	w := d.Page.W - d.Page.Left - d.Page.Right
	sig := &Table{Cols: []float64{w * 0.45, w * 0.25, w * 0.30}}
	cell := func(s string, al string) *Cell {
		return &Cell{Blocks: []Block{{P: &Para{PF: PF{Align: al}, Runs: []Run{tr(s, fPlain)}}}}, Span: 1}
	}
	sig.Rows = []*Row{{Cells: []*Cell{cell("[Должность]", "left"), cell("__________", "center"), cell("[И. О. Фамилия]", "right")}}}
	d.Body = append(d.Body, Block{T: sig})
	b.add(PF{})
	d.Footer = []Block{{P: &Para{Runs: []Run{tr("Исполнитель: [Фамилия И. О.], тел. [номер]", F{Size: 8, Color: "#7f7f7f"})}}}}
	return d
}

func reportDoc() *Doc {
	d := NewDoc()
	d.Body = nil
	b := &docB{d: d}
	b.add(PF{Style: "Title"}, tr("[Название отчёта]", fPlain))
	b.add(PF{Style: "Subtitle"}, tr("[Отдел / проект] · [период]", fPlain))
	b.add(PF{After: O(18)}, tr("Подготовил: ", F{B: true}), tr("[Фамилия И. О.]", fPlain), tr("   Дата: ", F{B: true}), tr("[дд.мм.гггг]", fPlain))
	b.add(PF{Heading: 1}, tr("Краткое резюме", fPlain))
	b.add(PF{}, tr("[Два-три предложения: главное, что нужно знать читателю, если он прочтёт только этот раздел.]", fPlain))
	b.add(PF{Heading: 1}, tr("Цели и задачи", fPlain))
	for _, s := range []string{"[Цель 1]", "[Цель 2]", "[Цель 3]"} {
		b.add(PF{List: &List{ID: "goals", Kind: "ul"}}, tr(s, fPlain))
	}
	b.add(PF{Heading: 1}, tr("Результаты", fPlain))
	b.add(PF{}, tr("[Что сделано за период. Ключевые показатели — в таблице.]", fPlain))
	w := d.Page.W - d.Page.Left - d.Page.Right
	t := &Table{Border: true, Cols: []float64{w * 0.4, w * 0.2, w * 0.2, w * 0.2}}
	row := func(hdr bool, cells ...string) {
		r := &Row{Header: hdr}
		for i, c := range cells {
			al := "right"
			if i == 0 {
				al = "left"
			}
			f := fPlain
			shade := ""
			if hdr {
				f, shade = F{B: true}, "#ede9fe"
			}
			r.Cells = append(r.Cells, &Cell{Span: 1, Shade: shade, Blocks: []Block{{P: &Para{PF: PF{Align: al}, Runs: []Run{tr(c, f)}}}}})
		}
		t.Rows = append(t.Rows, r)
	}
	row(true, "Показатель", "План", "Факт", "Отклонение")
	row(false, "[Показатель 1]", "[0]", "[0]", "[0 %]")
	row(false, "[Показатель 2]", "[0]", "[0]", "[0 %]")
	row(false, "[Показатель 3]", "[0]", "[0]", "[0 %]")
	d.Body = append(d.Body, Block{T: t})
	b.add(PF{Heading: 1}, tr("Выводы", fPlain))
	b.add(PF{}, tr("[Что получилось, что нет и почему.]", fPlain))
	b.add(PF{Heading: 1}, tr("Следующие шаги", fPlain))
	for _, s := range []string{"[Шаг 1 — ответственный, срок]", "[Шаг 2 — ответственный, срок]", "[Шаг 3 — ответственный, срок]"} {
		b.add(PF{List: &List{ID: "steps", Kind: "ol", Fmt: "decimal", Text: "%1."}}, tr(s, fPlain))
	}
	d.Footer = []Block{{P: &Para{PF: PF{Align: "right"}, Runs: []Run{tr("Стр. ", F{Size: 9}), {T: RField, Name: "PAGE", F: F{Size: 9}},
		tr(" из ", F{Size: 9}), {T: RField, Name: "NUMPAGES", F: F{Size: 9}}}}}}
	return d
}
