package main

import (
	"sort"
	"strings"
	"testing"
)

// normTestDoc — документ, оформленный по ГОСТ 7.32: титул, содержание, введение, глава с рисунком и таблицей, литература.
func normTestDoc() *Doc {
	d := NewDoc()
	d.Page = Page{W: 21 * CM, H: 29.7 * CM, Left: 3 * CM, Right: 1.5 * CM, Top: 2 * CM, Bottom: 2 * CM, Header: 1.25 * CM, Footer: 1.25 * CM}
	d.Def = Defaults{Font: "Times New Roman", Size: 14, Line: 1.5}
	d.TitlePg = true
	fp := NewPara()
	fp.PF.Align = "center"
	fp.Runs = []Run{{T: RField, Name: "PAGE"}}
	d.Footer = []Block{{P: fp}}
	d.Body = nil
	f := F{Font: "Times New Roman", Size: 14}
	para := func(text string, mod func(p *Para)) *Para {
		p := NewPara()
		p.PF.Align, p.PF.IndF, p.PF.Line = "justify", O(1.25*CM), O(1.5)
		p.Runs = []Run{{T: RText, Text: []rune(text), F: f}}
		if mod != nil {
			mod(p)
		}
		d.Body = append(d.Body, Block{P: p})
		return p
	}
	center := func(p *Para) { p.PF.Align, p.PF.IndF = "center", O(0) }
	h1 := func(p *Para) { center(p); p.PF.PB = true; p.Runs[0].F.B = true; p.PF.KeepNext = true }
	para("Министерство науки", center)
	para("КУРСОВАЯ РАБОТА", center)
	para("СОДЕРЖАНИЕ", func(p *Para) { h1(p); p.PF.PB = true })
	para("Введение\t3", func(p *Para) { p.PF.IndF = O(0) })
	para("1 Анализ\t4", func(p *Para) { p.PF.IndF = O(0) })
	para("ВВЕДЕНИЕ", h1)
	para("Работа посвящена анализу условий применения. Цель работы — обоснование требований к аппаратуре, которые изложены ниже [1].", nil)
	para("1 Анализ условий применения", func(p *Para) { p.PF.PB = true; p.Runs[0].F.B = true; p.PF.KeepNext = true })
	para("Структура комплекса показана на рисунке 1, а основные характеристики сведены в таблицу 1 [2, с. 15–17].", nil)
	img := NewPara()
	img.PF.Align = "center"
	img.Runs = []Run{{T: RImage, ID: "m1", W: 100, H: 80}}
	d.Body = append(d.Body, Block{P: img})
	para("Рисунок 1 – Структура комплекса", center)
	para("Таблица 1 – Характеристики", func(p *Para) { p.PF.Align, p.PF.IndF = "", O(0) })
	cell := func(s string) *Cell {
		p := NewPara()
		p.Runs = []Run{{T: RText, Text: []rune(s), F: f}}
		return &Cell{Blocks: []Block{{P: p}}}
	}
	d.Body = append(d.Body, Block{T: &Table{Border: true, Cols: []float64{100, 100},
		Rows: []*Row{{Cells: []*Cell{cell("Параметр"), cell("Значение")}}, {Cells: []*Cell{cell("Масса"), cell("10 кг")}}}}})
	para("По результатам анализа сформулированы требования, приведённые в таблице 1, и выводы по разделу.", nil)
	para("ЗАКЛЮЧЕНИЕ", h1)
	para("Поставленные задачи решены полностью, результаты могут быть использованы в дальнейшей работе.", nil)
	para("СПИСОК ИСПОЛЬЗОВАННЫХ ИСТОЧНИКОВ", h1)
	para("1. Иванов И. И. Радиолокация : учебник. – Москва : Наука, 2020. – 300 с.", nil)
	para("2. Петров П. П. Связь : учебник. – Москва : Наука, 2021. – 200 с.", nil)
	return d
}

func normKindIDs(is []NormIssue) []string {
	m := map[string]bool{}
	for _, i := range is {
		m[i.Kind] = true
	}
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestNormCleanDoc(t *testing.T) {
	d := normTestDoc()
	if is := NormCheck(d); len(is) > 0 {
		for _, i := range is {
			txt := ""
			if i.P != nil {
				txt = normText(d, i.P)
			}
			t.Errorf("лишнее замечание %s (%s): %q", i.Kind, i.Msg, txt)
		}
	}
}

func findPara(d *Doc, prefix string) *Para {
	var out *Para
	walkParas(d.Body, func(p *Para, _ bool) {
		if out == nil && strings.HasPrefix(p.Text(), prefix) {
			out = p
		}
	})
	return out
}

func TestNormFindsAndFixes(t *testing.T) {
	cases := []struct {
		kind string
		mod  func(d *Doc)
	}{
		{"margins", func(d *Doc) { d.Page.Right = 1 * CM }},
		{"page", func(d *Doc) { d.Page.W, d.Page.H = 612, 792 }},
		{"pagenum", func(d *Doc) { d.Footer = nil }},
		{"titlepg", func(d *Doc) { d.TitlePg = false }},
		{"font", func(d *Doc) { findPara(d, "Работа").Runs[0].F.Font = "Arial" }},
		{"size", func(d *Doc) { findPara(d, "Работа").Runs[0].F.Size = 10 }},
		{"sizemix", func(d *Doc) { findPara(d, "Работа").Runs[0].F.Size = 13 }},
		{"line", func(d *Doc) { findPara(d, "Работа").PF.Line = O(1.15) }},
		{"indent", func(d *Doc) { findPara(d, "Работа").PF.IndF = O(1 * CM) }},
		{"align", func(d *Doc) { findPara(d, "Работа").PF.Align = "left" }},
		{"color", func(d *Doc) { findPara(d, "Работа").Runs[0].F.Color = "#ff0000" }},
		{"hdot", func(d *Doc) {
			p := findPara(d, "1 Анализ условий")
			p.Runs[0].Text = append(p.Runs[0].Text, '.')
		}},
		{"hcase", func(d *Doc) { findPara(d, "ВВЕДЕНИЕ").Runs[0].Text = []rune("Введение") }},
		{"hpage", func(d *Doc) { findPara(d, "ЗАКЛЮЧЕНИЕ").PF.PB = false }},
		{"figcap", func(d *Doc) {
			p := findPara(d, "Рисунок 1")
			p.Runs[0].Text = []rune("Структура комплекса")
		}},
		{"figfmt", func(d *Doc) {
			findPara(d, "Рисунок 1").Runs[0].Text = []rune("Рис. 1. Структура комплекса.")
		}},
		{"tabcap", func(d *Doc) {
			findPara(d, "Таблица 1").Runs[0].Text = []rune("Характеристики комплекса")
		}},
		{"tabfmt", func(d *Doc) {
			findPara(d, "Таблица 1").Runs[0].Text = []rune("Таблица 1 - Характеристики")
		}},
		{"figref", func(d *Doc) {
			findPara(d, "Структура").Runs[0].Text = []rune("Основные характеристики сведены в таблицу 1 [2, с. 15–17].")
		}},
		{"tabref", func(d *Doc) {
			findPara(d, "По результатам").Runs[0].Text = []rune("По результатам анализа сформулированы выводы.")
			findPara(d, "Структура").Runs[0].Text = []rune("Структура комплекса показана на рисунке 1 [2, с. 15–17].")
		}},
		{"srcref", func(d *Doc) {
			p := findPara(d, "Работа")
			p.Runs[0].Text = []rune(strings.Replace(p.Text(), " [1]", "", 1))
		}},
		{"srcbad", func(d *Doc) {
			p := findPara(d, "Работа")
			p.Runs[0].Text = []rune(strings.Replace(p.Text(), "[1]", "[1, 7]", 1))
		}},
		{"spaces", func(d *Doc) {
			p := findPara(d, "Работа")
			p.Runs[0].Text = []rune(strings.Replace(p.Text(), "посвящена", "посвящена  ", 1))
		}},
		{"dash", func(d *Doc) {
			p := findPara(d, "Работа")
			p.Runs[0].Text = []rune(strings.Replace(p.Text(), " — ", " - ", 1))
		}},
	}
	for _, c := range cases {
		d := normTestDoc()
		c.mod(d)
		is := NormCheck(d)
		got := normKindIDs(is)
		if !containsStr(got, c.kind) {
			t.Errorf("%s: не нашлось, есть %v", c.kind, got)
			continue
		}
		for _, k := range got {
			if k != c.kind && !(c.kind == "figcap" && k == "figref") && !(c.kind == "tabcap" && k == "tabref") && !((c.kind == "figcap" || c.kind == "tabcap") && k == "indent") {
				t.Errorf("%s: заодно нашлось %q", c.kind, k)
			}
		}
		if !normKind(c.kind).Fix {
			continue
		}
		if n := NormFix(d, is, c.kind); n == 0 {
			t.Errorf("%s: ничего не исправлено", c.kind)
		}
		if left := NormCheck(d); containsStr(normKindIDs(left), c.kind) {
			t.Errorf("%s: после исправления осталось %v", c.kind, normKindIDs(left))
		}
	}
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestNormEmptyParas(t *testing.T) {
	d := normTestDoc()
	at := -1
	for i, b := range d.Body {
		if b.P != nil && strings.HasPrefix(b.P.Text(), "ЗАКЛЮЧЕНИЕ") {
			at = i
			b.P.PF.PB = false
		}
	}
	var empties []Block
	for i := 0; i < 6; i++ {
		empties = append(empties, Block{P: NewPara()})
	}
	d.Body = append(d.Body[:at], append(empties, d.Body[at:]...)...)
	is := NormCheck(d)
	if !containsStr(normKindIDs(is), "empty") {
		t.Fatalf("пустые абзацы не замечены: %v", normKindIDs(is))
	}
	n0 := len(d.Body)
	NormFix(d, is, "empty")
	if len(d.Body) != n0-6 {
		t.Errorf("абзацев %d, ждали %d", len(d.Body), n0-6)
	}
	if p := findPara(d, "ЗАКЛЮЧЕНИЕ"); !p.PF.PB {
		t.Errorf("заключение не с новой страницы")
	}
	if k := normKindIDs(NormCheck(d)); len(k) > 0 {
		t.Errorf("после исправления: %v", k)
	}
}

func TestRefNums(t *testing.T) {
	for in, want := range map[string]string{"1": "1", "1, 2 и 4–6": "1 2 4 5 6", "1.2–1.4": "1.2 1.3 1.4", "А.1": "А.1", "2.1 и 2.3": "2.1 2.3"} {
		if got := strings.Join(refNums(in), " "); got != want {
			t.Errorf("refNums(%q) = %q, ждали %q", in, got, want)
		}
	}
	for in, want := range map[string][]int{"1": {1}, "2, с. 15–17": {2}, "3–5": {3, 4, 5}, "1; 4": {1, 4}} {
		got, ok := citeNumsOf(in)
		if !ok || len(got) != len(want) {
			t.Errorf("citeNumsOf(%q) = %v, ждали %v", in, got, want)
		}
	}
	if _, ok := citeNumsOf("Иванов, 2020"); ok {
		t.Errorf("[Иванов, 2020] — не номерная ссылка")
	}
}

// Шаблоны Texora сами должны проходить нормоконтроль.
func TestNormTemplatesClean(t *testing.T) {
	m := NewMeasurer(monoTB{})
	for name, d := range map[string]*Doc{"course": courseDoc(m, false), "thesis": courseDoc(m, true), "essay": essayDoc(m)} {
		for _, i := range NormCheck(d) {
			s := ""
			if i.P != nil {
				s = normText(d, i.P)
			}
			t.Errorf("%s: %s %s %q", name, i.Kind, i.Msg, s)
		}
	}
}
