package main

// Интерфейс новых функций: формулы, источники и ссылки, разрывы разделов, обтекание, стили, поля.

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- формулы
type eqItem struct{ label, val string }

// eqPalettes — палитры по разделам (как в MathType). «|» — куда встанет курсор.
var eqPalettes = []struct {
	name  string
	items []eqItem
}{
	{"Шаблоны", []eqItem{{"a/b", "\\frac{|}{}"}, {"x²", "^{|}"}, {"xᵢ", "_{|}"}, {"xᵢⁿ", "_{|}^{}"}, {"√x", "\\sqrt{|}"},
		{"ⁿ√x", "\\sqrt[n]{|}"}, {"( )", "\\left( | \\right)"}, {"[ ]", "\\left[ | \\right]"}, {"{ }", "\\left\\{ | \\right\\}"},
		{"|x|", "\\left\\vert | \\right\\vert"}, {"‖x‖", "\\left\\| | \\right\\|"}, {"⟨x⟩", "\\left\\langle | \\right\\rangle"},
		{"⌊x⌋", "\\left\\lfloor | \\right\\rfloor"}, {"(ⁿₖ)", "\\binom{|}{}"}, {"x⃗", "\\vec{|}"}, {"x̄", "\\bar{|}"}, {"x̂", "\\hat{|}"},
		{"ẋ", "\\dot{|}"}, {"ẍ", "\\ddot{|}"}, {"x͠", "\\tilde{|}"}, {"над", "\\overset{|}{}"}, {"под", "\\underset{|}{}"},
		{"⏞", "\\overbrace{|}^{}"}, {"⏟", "\\underbrace{|}_{}"}, {"текст", "\\text{|}"}}},
	{"Матрицы и системы", []eqItem{{"(2×2)", "\\begin{pmatrix} | & \\\\ & \\end{pmatrix}"},
		{"(3×3)", "\\begin{pmatrix} | & & \\\\ & & \\\\ & & \\end{pmatrix}"}, {"[2×2]", "\\begin{bmatrix} | & \\\\ & \\end{bmatrix}"},
		{"det 2×2", "\\begin{vmatrix} | & \\\\ & \\end{vmatrix}"}, {"det 3×3", "\\begin{vmatrix} | & & \\\\ & & \\\\ & & \\end{vmatrix}"},
		{"столбец", "\\begin{pmatrix} | \\\\ \\\\ \\end{pmatrix}"}, {"(m×n)", "\\begin{pmatrix} a_{11} & \\cdots & a_{1n} \\\\ \\vdots & \\ddots & \\vdots \\\\ a_{m1} & \\cdots & a_{mn} \\end{pmatrix}"},
		{"система 2", "\\begin{cases} | \\\\ \\end{cases}"}, {"система 3", "\\begin{cases} | \\\\ \\\\ \\end{cases}"},
		{"по частям", "\\begin{cases} |, & x \\ge 0 \\\\ , & x < 0 \\end{cases}"},
		{"вывод в строках", "| &= \\\\ &= "}, {"строки по центру", "\\begin{gathered} | \\\\ \\end{gathered}"},
		{"новая строка \\\\", " \\\\ |"}, {"столбец &", " & |"}, {"⋮", "\\vdots"}, {"⋯", "\\cdots"}, {"⋱", "\\ddots"}}},
	{"Анализ", []eqItem{{"∑", "\\sum_{i=1}^{n} |"}, {"∏", "\\prod_{i=1}^{n} |"}, {"∫", "\\int |\\, dx"}, {"∫ₐᵇ", "\\int_{a}^{b} |\\, dx"},
		{"∬", "\\iint_{D} |\\, dx\\, dy"}, {"∭", "\\iiint_{V} |\\, dV"}, {"∮", "\\oint_{L} |"}, {"lim", "\\lim_{x \\to |}"},
		{"lim→∞", "\\lim_{n \\to \\infty} |"}, {"dy/dx", "\\dv{|}{x}"}, {"d²y/dx²", "\\dv[2]{|}{x}"}, {"∂u/∂x", "\\pdv{|}{x}"},
		{"∂²u/∂x²", "\\pdv[2]{|}{x}"}, {"f′", "|'"}, {"f″", "|''"}, {"dx", "\\, dx"}, {"∇", "\\nabla"}, {"Δ", "\\Delta"},
		{"∞", "\\infty"}, {"max", "\\max_{|}"}, {"min", "\\min_{|}"}, {"sin", "\\sin |"}, {"cos", "\\cos |"}, {"tg", "\\tg |"},
		{"ln", "\\ln |"}, {"log", "\\log_{|}"}, {"eˣ", "e^{|}"}}},
	{"Греческие", []eqItem{{"α", "\\alpha"}, {"β", "\\beta"}, {"γ", "\\gamma"}, {"δ", "\\delta"}, {"ε", "\\varepsilon"}, {"ζ", "\\zeta"},
		{"η", "\\eta"}, {"θ", "\\theta"}, {"ι", "\\iota"}, {"κ", "\\kappa"}, {"λ", "\\lambda"}, {"μ", "\\mu"}, {"ν", "\\nu"}, {"ξ", "\\xi"},
		{"π", "\\pi"}, {"ρ", "\\rho"}, {"σ", "\\sigma"}, {"τ", "\\tau"}, {"υ", "\\upsilon"}, {"φ", "\\varphi"}, {"χ", "\\chi"},
		{"ψ", "\\psi"}, {"ω", "\\omega"}, {"Γ", "\\Gamma"}, {"Δ", "\\Delta"}, {"Θ", "\\Theta"}, {"Λ", "\\Lambda"}, {"Ξ", "\\Xi"},
		{"Π", "\\Pi"}, {"Σ", "\\Sigma"}, {"Φ", "\\Phi"}, {"Ψ", "\\Psi"}, {"Ω", "\\Omega"}, {"ℏ", "\\hbar"}, {"ℓ", "\\ell"}}},
	{"Операторы и отношения", []eqItem{{"±", "\\pm"}, {"∓", "\\mp"}, {"·", "\\cdot"}, {"×", "\\times"}, {"÷", "\\div"}, {"∘", "\\circ"},
		{"≤", "\\le"}, {"≥", "\\ge"}, {"≠", "\\ne"}, {"≈", "\\approx"}, {"≡", "\\equiv"}, {"∼", "\\sim"}, {"≅", "\\cong"},
		{"∝", "\\propto"}, {"≪", "\\ll"}, {"≫", "\\gg"}, {"⊕", "\\oplus"}, {"⊗", "\\otimes"}, {"⊥", "\\perp"}, {"∥", "\\parallel"},
		{"∠", "\\angle"}, {"°", "^{\\circ}"}, {"′", "'"}, {"!", "!"}, {"≝", "\\overset{\\text{def}}{=}"}}},
	{"Стрелки и логика", []eqItem{{"→", "\\to"}, {"←", "\\leftarrow"}, {"↔", "\\leftrightarrow"}, {"⇒", "\\Rightarrow"},
		{"⇐", "\\Leftarrow"}, {"⇔", "\\Leftrightarrow"}, {"⟹", "\\implies"}, {"⟺", "\\iff"}, {"↦", "\\mapsto"}, {"↑", "\\uparrow"},
		{"↓", "\\downarrow"}, {"⇌", "\\rightleftharpoons"}, {"∀", "\\forall"}, {"∃", "\\exists"}, {"∄", "\\nexists"}, {"¬", "\\neg"},
		{"∧", "\\land"}, {"∨", "\\lor"}, {"∴", "\\therefore"}, {"∵", "\\because"}}},
	{"Множества", []eqItem{{"∈", "\\in"}, {"∉", "\\notin"}, {"∋", "\\ni"}, {"⊂", "\\subset"}, {"⊆", "\\subseteq"}, {"⊃", "\\supset"},
		{"⊇", "\\supseteq"}, {"∪", "\\cup"}, {"∩", "\\cap"}, {"∖", "\\setminus"}, {"∅", "\\varnothing"}, {"ℕ", "\\mathbb{N}"},
		{"ℤ", "\\mathbb{Z}"}, {"ℚ", "\\mathbb{Q}"}, {"ℝ", "\\mathbb{R}"}, {"ℂ", "\\mathbb{C}"}, {"⋃", "\\bigcup_{i=1}^{n} |"},
		{"⋂", "\\bigcap_{i=1}^{n} |"}, {"{x | }", "\\left\\{ x \\mid | \\right\\}"}, {"жирный", "\\mathbf{|}"}, {"𝒜", "\\mathcal{|}"}}},
}

var eqExamples = []string{"x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}", "E = mc^2", "\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}",
	"\\int_0^1 x^2 dx = \\frac{1}{3}", "a^2 + b^2 = c^2", "\\lim_{x \\to 0} \\frac{\\sin x}{x} = 1",
	"\\begin{cases} x + y = 5 \\\\ 2x - y = 1 \\end{cases}", "A = \\begin{pmatrix} 1 & 2 \\\\ 3 & 4 \\end{pmatrix}",
	"\\det A = \\begin{vmatrix} a & b \\\\ c & d \\end{vmatrix} = ad - bc", "(a+b)^2 &= a^2 + 2ab + b^2 \\\\ &= (a+b)(a+b)",
	"\\dv[2]{x}{t} + \\omega_0^2 x = 0", "\\pdv{u}{t} = a^2 \\pdv[2]{u}{x}", "\\oint_L \\vec{E} \\cdot d\\vec{l} = -\\dv{\\Phi}{t}",
	"F(\\omega) = \\int_{-\\infty}^{\\infty} f(t) e^{-i\\omega t} dt", "\\binom{n}{k} = \\frac{n!}{k!(n-k)!}",
	"u_C(t) = U_0 \\left( 1 - e^{-t/\\tau} \\right)", "Z = \\sqrt{R^2 + \\left( \\omega L - \\frac{1}{\\omega C} \\right)^2}"}

func (a *App) eqDialog(p *Para, idx int) {
	e := a.E
	src := ""
	if p != nil && idx >= 0 {
		src = string(p.Runs[idx].Text)
	}
	d := &Dialog{Title: "Формула", W: 720}
	in := textField("Запись", src)
	if src == "" {
		in.Edit = NewTextEdit("")
	}
	prev := &Field{Kind: "custom", H: 150}
	prev.Paint = func(cv *Canvas, r Rect) {
		cv.FillRound(r, a.d(8), Hex(0xffffff))
		cv.StrokeRound(r, a.d(8), math.Max(1, a.S), a.th.Border)
		s := in.Edit.String()
		if strings.TrimSpace(s) == "" {
			a.centerText(cv, a.tr("Здесь будет формула. Пример: x^2 + \\frac{1}{2}"), r, a.uiFont(12), Hex(0x9a9a9a))
			return
		}
		size := 20.0
		box := e.M.EqLayout(s, F{Size: size})
		k := a.S * 96 / 72
		if box.W*k > r.W-a.d(24) {
			k = (r.W - a.d(24)) / box.W
		}
		if (box.Asc+box.Desc)*k > r.H-a.d(12) {
			k = math.Min(k, (r.H-a.d(12))/(box.Asc+box.Desc))
		}
		pp := &PagePainter{cv: cv, tb: a.tb, doc: e.Doc, k: k, alpha: 1}
		pp.ox = r.X + (r.W-box.W*k)/2
		pp.oy = r.Y + (r.H-(box.Asc+box.Desc)*k)/2 + box.Asc*k
		pp.drawEq(box, 0, 0, Hex(0x111111))
	}
	chips := &Field{Kind: "chips", Target: 0}
	setPal := func(i int) {
		chips.Opts, chips.Vals = nil, nil
		for _, t := range eqPalettes[i].items {
			chips.Opts = append(chips.Opts, t.label)
			chips.Vals = append(chips.Vals, t.val)
		}
	}
	var palNames []string
	for _, pl := range eqPalettes {
		palNames = append(palNames, pl.name)
	}
	pal := &Field{Label: "Палитра", Kind: "select", Opts: palNames, Sel: a.eqPal}
	pal.OnSel = func(i int) { a.eqPal = i; setPal(i) }
	setPal(min(a.eqPal, len(eqPalettes)-1))
	num := &Field{Label: "Отдельной строкой с номером справа — (1), (1.2)", Kind: "check"}
	chap := &Field{Label: "Номер с главой (1.2) — по заголовкам 1 уровня", Kind: "check", On: true}
	d.Fields = []*Field{in, prev, pal, chips,
		{Label: "Как в LaTeX: ^ степень, _ индекс, \\frac{a}{b}, \\sqrt{x}; & и \\\\ — столбцы и строки (матрицы, системы, выводы).", Kind: "info"}}
	ex := &Field{Label: "Примеры", Kind: "select", Opts: append([]string{"—"}, eqExamples...)}
	ex.OnSel = func(i int) {
		if i > 0 {
			in.Edit = NewTextEdit(eqExamples[i-1])
		}
	}
	d.Fields = append(d.Fields, ex)
	if p == nil {
		d.Fields = append(d.Fields, num, chap)
	}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: map[bool]string{true: "Вставить", false: "Сохранить"}[p == nil], Primary: true, Act: func() bool {
		s := strings.TrimSpace(in.Edit.String())
		if s == "" {
			d.Info = "Введите формулу"
			return false
		}
		switch {
		case p != nil:
			e.ReplaceEquation(p, idx, s)
		case num.On:
			e.InsertNumberedEquation(s, chap.On)
		default:
			e.InsertEquation(s)
		}
		a.after()
		return true
	}}}
	a.openDialog(d)
}

// ---------------------------------------------------------------- источники
func (a *App) sourceDialog(s *Source) {
	e := a.E
	cur := Source{Type: "book"}
	if s != nil {
		cur = *s
	}
	typeIdx := 0
	var typeNames []string
	for i, t := range SourceTypes {
		typeNames = append(typeNames, t.Name)
		if t.ID == cur.Type {
			typeIdx = i
		}
	}
	d := &Dialog{Title: map[bool]string{true: "Новый источник", false: "Источник"}[s == nil], W: 600}
	tf := &Field{Label: "Тип", Kind: "select", Opts: typeNames, Sel: typeIdx}
	fl := []*Field{tf,
		textField("Авторы", cur.Authors), textField("Заглавие", cur.Title), textField("Сведения к заглавию", cur.Extra),
		textField("Город", cur.City), textField("Издательство / организация", cur.Publisher), textField("Год", cur.Year),
		textField("Страниц / страницы", cur.Pages), textField("Журнал / сайт (у патента — МПК)", cur.Journal), textField("Номер выпуска", cur.Issue),
		textField("Номер стандарта или патента", cur.Number), textField("URL", cur.URL), textField("Дата обращения", cur.Accessed),
		{Label: "Авторы через запятую: «Иванов И. И., Петров П. П.». Оформление — ГОСТ Р 7.0.100-2018. У патента в поле «Город» — страна, в «Сведениях к заглавию» — № заявки и даты.", Kind: "info"}}
	prev := &Field{Kind: "custom", H: 64}
	collect := func() *Source {
		ns := cur
		ns.Type = SourceTypes[tf.Sel].ID
		ns.Authors, ns.Title, ns.Extra = d.Val(1), d.Val(2), d.Val(3)
		ns.City, ns.Publisher, ns.Year, ns.Pages = d.Val(4), d.Val(5), d.Val(6), d.Val(7)
		ns.Journal, ns.Issue, ns.Number, ns.URL, ns.Accessed = d.Val(8), d.Val(9), d.Val(10), d.Val(11), d.Val(12)
		if ns.Type == "web" && ns.Publisher == "" {
			ns.Publisher = ns.Journal
		}
		return &ns
	}
	prev.Paint = func(cv *Canvas, r Rect) {
		cv.FillRound(r, a.d(6), a.th.Field)
		t := FormatSource(collect())
		y := r.Y + a.d(6)
		for i, l := range a.wrapText(t, r.W-a.d(20), a.uiFont(12)) {
			if i > 2 {
				break
			}
			a.textAt(cv, l, r.X+a.d(10), y, a.uiFont(12), a.th.Text)
			y += a.d(17)
		}
	}
	d.Fields = append(fl, prev)
	btns := []DlgBtn{{Label: "Отмена"}}
	if s != nil {
		id := s.ID
		btns = append(btns, DlgBtn{Label: "Удалить", Act: func() bool { e.DeleteSource(id); a.after(); return true }})
	}
	btns = append(btns, DlgBtn{Label: "Сохранить", Primary: true, Act: func() bool {
		ns := collect()
		if strings.TrimSpace(ns.Title) == "" {
			d.Info = "Нужно хотя бы заглавие"
			return false
		}
		e.SaveSource(ns)
		a.after()
		return true
	}})
	d.Buttons = btns
	a.openDialog(d)
}

func shortSource(s *Source) string {
	t := FormatSource(s)
	if r := []rune(t); len(r) > 70 {
		t = string(r[:68]) + "…"
	}
	return t
}

func (a *App) sourcesPopup(r Rect) {
	var items []PopItem
	items = append(items, PopItem{Label: a.tr("Добавить источник…"), Icon: "plus", Act: func() { a.sourceDialog(nil) }})
	items = append(items, PopItem{Label: a.tr("Импорт из BibTeX или RIS…"), Icon: "open", Act: func() { a.importBibFile() }})
	items = append(items, PopItem{Label: a.tr("Вставить из буфера (BibTeX или RIS)"), Icon: "paste", Act: func() { a.importBibClipboard() }})
	if len(a.E.Doc.Sources) > 0 {
		items = append(items, PopItem{Label: a.tr("Экспорт в BibTeX…"), Icon: "save", Act: func() { a.exportBib("bib") }})
		items = append(items, PopItem{Label: a.tr("Экспорт в RIS…"), Icon: "save", Act: func() { a.exportBib("ris") }})
		if n := len(FindDuplicates(a.E.Doc.Sources)); n > 0 {
			items = append(items, PopItem{Label: a.trf("Объединить повторы (%d)", n), Icon: "merge", Act: func() { a.mergeDupSources() }})
		}
	}
	if len(a.E.Doc.Sources) > 0 {
		items = append(items, PopItem{Sep: true})
	}
	for _, s := range a.E.Doc.Sources {
		s := s
		items = append(items, PopItem{Label: shortSource(s), Icon: "bib", Act: func() { a.sourceDialog(s) }})
	}
	a.popup = a.listPopup("sources", r.X, r.Y+r.H+a.d(4), a.d(520), items, a.d(440), len(items) > 10)
}

func (a *App) citePopup(r Rect) {
	e := a.E
	if len(e.Doc.Sources) == 0 {
		a.sourceDialog(nil)
		return
	}
	var items []PopItem
	nums := CiteOrder(e.Doc)
	for _, s := range e.Doc.Sources {
		s := s
		right := ""
		if n, ok := nums[s.ID]; ok {
			right = "[" + strconv.Itoa(n) + "]"
		}
		items = append(items, PopItem{Label: shortSource(s), Icon: "cite", Right: right, Act: func() {
			d := &Dialog{Title: "Ссылка на источник", W: 460, Fields: []*Field{textField("Страница (необязательно)", ""),
				{Label: "Будет вставлено [n] или [n, с. 25]", Kind: "info"}}}
			d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "Вставить", Primary: true, Act: func() bool {
				e.InsertCitation(s.ID, d.Val(0))
				a.after()
				return true
			}}}
			a.openDialog(d)
		}})
	}
	items = append(items, PopItem{Sep: true}, PopItem{Label: a.tr("Добавить источник…"), Icon: "plus", Act: func() { a.sourceDialog(nil) }})
	a.popup = a.listPopup("cite", r.X, r.Y+r.H+a.d(4), a.d(520), items, a.d(440), len(items) > 10)
}

// UpdateFields — F9: оглавление, список литературы, номера страниц.
func (a *App) UpdateFields() {
	e := a.E
	e.Checkpoint("")
	t, n := UpdateTOC(e.Doc)
	b := UpdateBib(e.Doc)
	lof := UpdateLOF(e.Doc)
	e.Changed()
	switch {
	case lof > 0 && !t && !b:
		a.msg(a.trf("Обновлено перечней иллюстраций: %d", lof))
	case t && b:
		a.msg(a.trf("Обновлено: оглавление (%d) и список литературы", n))
	case t:
		a.msg(a.trf("Оглавление обновлено: %d пунктов", n))
	case b:
		a.msg(a.tr("Список литературы обновлён"))
	default:
		a.msg(a.tr("Поля обновлены"))
	}
	a.after()
}

// ---------------------------------------------------------------- разрывы и разделы
func (a *App) breaksPopup(r Rect) {
	e := a.E
	items := []PopItem{
		{Label: a.tr("Разрыв страницы"), Icon: "pagebreak", Right: "Ctrl+Enter", Act: func() { a.Command("pagebreak") }},
		{Sep: true},
		{Label: a.tr("Раздел со следующей страницы"), Icon: "section", Act: func() { e.InsertSectionBreak(""); a.after() }},
		{Label: a.tr("Раздел на текущей странице"), Icon: "section", Act: func() { e.InsertSectionBreak("continuous"); a.after() }},
		{Label: a.tr("Раздел с чётной страницы"), Icon: "section", Act: func() { e.InsertSectionBreak("evenPage"); a.after() }},
		{Label: a.tr("Раздел с нечётной страницы"), Icon: "section", Act: func() { e.InsertSectionBreak("oddPage"); a.after() }},
	}
	if len(e.Doc.Sections()) > 1 {
		items = append(items, PopItem{Sep: true}, PopItem{Label: a.tr("Убрать разрыв раздела перед курсором"), Icon: "x", Act: func() {
			a.removeSectionBreak()
		}})
	}
	a.popup = a.listPopup("breaks", r.X, r.Y+r.H+a.d(4), a.d(330), items, a.d(420), false)
}

func (a *App) removeSectionBreak() {
	e := a.E
	if e.Story != "body" {
		return
	}
	en := e.entry(e.Sel.F.P)
	if en.Cell != nil {
		return
	}
	idx := IndexOfPara(e.Doc.Body, e.Sel.F.P)
	for i := idx; i >= 0; i-- {
		if p := e.Doc.Body[i].P; p != nil && p.PF.Sect != nil && (i < idx || i == idx) {
			e.Checkpoint("")
			s := p.PF.Sect
			// раздел, который шёл за разрывом, наследует колонтитулы, если своих нет
			next := e.Doc.SectionAt(i + 1)
			for _, k := range HFKinds {
				if len(*next.HF.Get(k)) == 0 {
					*next.HF.Get(k) = *s.HF.Get(k)
				}
			}
			p.PF.Sect = nil
			p.Touch()
			e.Changed()
			a.after()
			a.msg(a.tr("Разрыв раздела удалён"))
			return
		}
	}
}

func (a *App) pageNumDialog() {
	e := a.E
	cur := e.CurSection()
	start := ""
	if cur.Start > 0 {
		start = strconv.Itoa(cur.Start)
	}
	d := &Dialog{Title: "Номера страниц", W: 460, Fields: []*Field{
		textField("Начать раздел с номера", start),
		{Label: "Пусто — продолжить нумерацию предыдущего раздела", Kind: "info"},
		{Label: "Особый колонтитул для первой страницы раздела", Kind: "check", On: cur.TitlePg},
		{Label: "Разные колонтитулы для чётных и нечётных", Kind: "check", On: e.Doc.EvenOdd},
	}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "OK", Primary: true, Act: func() bool {
		n := 0
		if v := strings.TrimSpace(d.Val(0)); v != "" {
			x, err := strconv.Atoi(v)
			if err != nil || x < 0 || x > 99999 {
				d.Info = "Номер — целое число"
				return false
			}
			n = x
		}
		e.Checkpoint("")
		tp, eo := d.Fields[2].On, d.Fields[3].On
		e.SetSection(func(s *SecView) { s.Start, s.TitlePg = n, tp })
		e.Doc.EvenOdd = eo
		e.Changed()
		a.after()
		return true
	}}}
	a.openDialog(d)
}

// ---------------------------------------------------------------- обтекание картинок
var wrapModes = []struct{ wrap, side, label string }{
	{"", "", "В тексте"}, {"square", "left", "Вокруг рамки, картинка слева"}, {"square", "right", "Вокруг рамки, картинка справа"},
	{"topbottom", "left", "Сверху и снизу, слева"}, {"topbottom", "center", "Сверху и снизу, по центру"}, {"topbottom", "right", "Сверху и снизу, справа"},
	{"behind", "center", "За текстом"}, {"front", "center", "Перед текстом"},
}

func (a *App) wrapItems() []PopItem {
	e := a.E
	if a.imgSel == nil {
		return nil
	}
	pos := *a.imgSel
	i := RunAt(pos.P, pos.O, RImage)
	if i < 0 {
		return nil
	}
	r := pos.P.Runs[i]
	var items []PopItem
	for _, m := range wrapModes {
		m := m
		on := r.Wrap == m.wrap && (m.wrap == "" || r.Side == m.side || (m.side == "left" && r.Side == ""))
		items = append(items, PopItem{Label: a.tr(m.label), Icon: "wrap", On: on, Act: func() {
			e.SetWrap(pos, m.wrap, m.side)
			a.after()
		}})
	}
	return items
}

func (a *App) wrapPopup(r Rect) {
	items := a.wrapItems()
	if items == nil {
		a.msg(a.tr("Сначала щёлкните по картинке"))
		return
	}
	a.popup = a.listPopup("wrap", r.X, r.Y+r.H+a.d(4), a.d(300), items, a.d(400), false)
}

// ---------------------------------------------------------------- стили
func (a *App) styleDialog(key string) {
	e := a.E
	s, _ := e.Doc.StyleDef(key)
	name := StyleNames[key]
	if name == "" {
		name = key
	}
	font := s.Font
	if font == "" {
		font = e.Doc.Def.Font
		if strings.HasPrefix(key, "h") && (key == "h1" || key == "h2") || key == "Title" {
			font = headingFont(e.Doc)
		}
	}
	size := s.Size
	if size == 0 {
		size = e.Doc.Def.Size
	}
	aligns := []string{"left", "center", "right", "justify"}
	alignNames := []string{"По левому краю", "По центру", "По правому краю", "По ширине"}
	ai := 0
	for i, v := range aligns {
		if v == s.Align {
			ai = i
		}
	}
	line := s.Line
	if line == 0 {
		line = e.Doc.Def.Line
	}
	d := &Dialog{Title: a.trf("Стиль «%s»", a.tr(name)), W: 480, Fields: []*Field{
		textField("Шрифт", font), textField("Размер, пт", fmtNum(size)), textField("Цвет (#RRGGBB или пусто)", s.Color),
		{Label: "Полужирный", Kind: "check", On: s.B}, {Label: "Курсив", Kind: "check", On: s.I},
		{Label: "Все прописные", Kind: "check", On: s.Caps},
		{Label: "Выравнивание", Kind: "select", Opts: alignNames, Sel: ai},
		textField("Интервал перед, пт", fmtNum(math.Max(0, s.Before))), textField("Интервал после, пт", fmtNum(math.Max(0, s.After))),
		textField("Междустрочный (множитель)", fmtNum(line)), textField("Первая строка, см", fmtNum(s.IndF/CM)),
	}}
	d.Buttons = []DlgBtn{{Label: "Отмена"}, {Label: "Сбросить", Act: func() bool {
		e.Checkpoint("")
		delete(e.Doc.Styles, key)
		e.touchStyle(key)
		e.Changed()
		a.after()
		return true
	}}, {Label: "OK", Primary: true, Act: func() bool {
		var v [5]float64
		for i, fi := range []int{1, 7, 8, 9, 10} {
			x, ok := parseNum(d.Val(fi))
			if !ok {
				d.Info = "Проверьте число в поле «" + d.Fields[fi].Label + "»"
				return false
			}
			v[i] = x
		}
		ns := s
		ns.Font = strings.TrimSpace(d.Val(0))
		ns.Size, ns.Before, ns.After, ns.Line, ns.IndF, ns.Set = v[0], v[1], v[2], v[3], v[4]*CM, true
		c := strings.TrimSpace(d.Val(2))
		if c != "" && !strings.HasPrefix(c, "#") {
			c = "#" + c
		}
		ns.Color = strings.ToLower(c)
		ns.B, ns.I, ns.Caps = d.Fields[3].On, d.Fields[4].On, d.Fields[5].On
		ns.Align = aligns[d.Fields[6].Sel]
		if ns.Align == "left" {
			ns.Align = ""
		}
		e.SetStyleDef(key, ns)
		a.after()
		return true
	}}}
	a.openDialog(d)
}

func (a *App) styleMenuItems() []PopItem {
	e := a.E
	key := StyleKey(e.Sel.F.P)
	if key == "" {
		return nil
	}
	name := a.tr(StyleNames[key])
	return []PopItem{
		{Label: a.trf("Изменить стиль «%s»…", name), Icon: "style", Act: func() { a.styleDialog(key) }},
		{Label: a.trf("Обновить «%s» по выделенному", name), Icon: "update", Act: func() { e.UpdateStyleFromPara(key); a.after() }},
	}
}

// gotoBookmark — переход к заголовку по ссылке из оглавления.
func (a *App) gotoBookmark(bm string) {
	e := a.E
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	for _, en := range e.flat {
		if en.P.PF.Bookmark == bm {
			e.SetCaret(Pos{P: en.P}, false)
			a.ScrollToCaret()
			a.after()
			return
		}
	}
	a.msg(a.tr("Заголовок не найден — обновите оглавление (F9)"))
}

// ---------------------------------------------------------------- импорт и экспорт литературы

func (a *App) importBibText(src, from string) {
	ss := ParseBibliography(src)
	if len(ss) == 0 {
		a.errorBox(a.tr("Не нашлось ни одной записи BibTeX или RIS") + " (" + from + ").")
		return
	}
	added, skipped := a.E.ImportSources(ss)
	msg := a.trf("Добавлено источников: %d", added)
	if skipped > 0 {
		msg += ", " + a.trf("уже были в списке: %d", skipped)
	}
	a.toast(msg)
	a.after()
}

func (a *App) importBibFile() {
	p := a.P.OpenDialog("bib")
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		a.errorBox(a.tr("Не удалось открыть файл:") + "\n" + err.Error())
		return
	}
	a.importBibText(decodeText(data), filepath.Base(p))
}

func (a *App) importBibClipboard() {
	a.importBibText(a.P.GetClipboard().Text, a.tr("буфер обмена"))
}

func (a *App) exportBib(ext string) {
	name := strings.TrimSuffix(a.FileName, filepath.Ext(a.FileName)) + "." + ext
	p, _ := a.P.SaveDialog(name, ext)
	if p == "" {
		return
	}
	text := ExportBibTeX(a.E.Doc.Sources)
	if ext == "ris" {
		text = ExportRIS(a.E.Doc.Sources)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		a.errorBox(a.tr("Не удалось сохранить файл:") + "\n" + err.Error())
		return
	}
	a.msg(a.trf("Сохранено источников: %d", len(a.E.Doc.Sources)))
}

func (a *App) mergeDupSources() {
	groups := FindDuplicates(a.E.Doc.Sources)
	var lines []string
	for _, g := range groups {
		lines = append(lines, "• "+shortSource(g[0])+a.trf(" — повторов: %d", len(g)-1))
	}
	a.confirm(a.tr("Повторы в списке литературы"), strings.Join(lines, "\n")+"\n\n"+a.tr("Объединить? Ссылки в тексте перейдут на оставшиеся источники, недостающие сведения возьмутся из повторов."),
		DlgBtn{Label: "Отмена"},
		DlgBtn{Label: "Объединить", Primary: true, Act: func() bool {
			n := a.E.MergeDuplicates()
			a.toast(a.trf("Убрано повторов: %d", n))
			a.after()
			return true
		}})
}
