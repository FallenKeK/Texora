package main

import (
	"strings"
	"testing"
)

func TestNormAuthor(t *testing.T) {
	for in, want := range map[string]string{"Иванов, Иван Петрович": "Иванов И. П.", "Иван Петрович Иванов": "Иванов И. П.",
		"И. П. Иванов": "Иванов И. П.", "Иванов И.П.": "Иванов И. П.", "Smith, John": "Smith J.", "Jean-Paul Sartre": "Sartre J. P.",
		"{Иванов}, И. П.": "Иванов И. П.", "Петров": "Петров"} {
		if got := normAuthor(in); got != want {
			t.Errorf("normAuthor(%q) = %q, ждали %q", in, got, want)
		}
	}
}

const sampleBib = `% из eLibrary
@article{ivanov2020,
  author = {Иванов, Иван Петрович and Петров, П. С.},
  title = {Радиолокация {БЛА} малой дальности},
  journal = "Радиотехника",
  year = 2020,
  number = {5},
  pages = {12--19},
  doi = {10.1234/rt.2020.5}
}
@book{book1, author = {Сидоров, А. А.}, title = {Основы радиоэлектроники}, address = {Москва}, publisher = {Наука}, year = {2018}, pagetotal = {320}}
@online{site, title = {ГЛОНАСС}, url = {https://glonass-iac.ru}, urldate = {2024-03-05}, organization = {ИАЦ КВНО}}
@standard{gost, title = {Система стандартов по информации, библиотечному и издательскому делу}, number = {ГОСТ Р 7.0.100–2018},
  subtitle = {национальный стандарт Российской Федерации : издание официальное}, address = {Москва}, publisher = {Стандартинформ}, year = {2018}, pagetotal = {70}}
@comment{пропускается}
`

func TestParseBibTeX(t *testing.T) {
	ss := ParseBibTeX(sampleBib)
	if len(ss) != 4 {
		t.Fatalf("записей %d, ждали 4", len(ss))
	}
	a := ss[0]
	if a.Type != "article" || a.Authors != "Иванов И. П., Петров П. С." || a.Title != "Радиолокация БЛА малой дальности" ||
		a.Journal != "Радиотехника" || a.Year != "2020" || a.Issue != "5" || a.Pages != "12–19" || a.URL != "https://doi.org/10.1234/rt.2020.5" {
		t.Errorf("статья: %+v", a)
	}
	if b := ss[1]; b.Type != "book" || b.City != "Москва" || b.Publisher != "Наука" || b.Pages != "320" {
		t.Errorf("книга: %+v", b)
	}
	if w := ss[2]; w.Type != "web" || w.Accessed != "05.03.2024" || w.Publisher != "ИАЦ КВНО" {
		t.Errorf("сайт: %+v", w)
	}
	g := ss[3]
	if g.Type != "standard" || g.Number != "ГОСТ Р 7.0.100–2018" {
		t.Errorf("стандарт: %+v", g)
	}
	got := FormatSource(g)
	want := "ГОСТ Р 7.0.100–2018. Система стандартов по информации, библиотечному и издательскому делу : национальный стандарт Российской Федерации : издание официальное. – Москва : Стандартинформ, 2018. – 70 с."
	if got != want {
		t.Errorf("стандарт по ГОСТ:\n%s\n%s", got, want)
	}
	if got := FormatSource(a); !strings.HasPrefix(got, "Иванов, И. П. Радиолокация БЛА малой дальности / И. П. Иванов, П. С. Петров // Радиотехника. – 2020. – № 5. – С. 12–19.") {
		t.Errorf("статья по ГОСТ: %s", got)
	}
}

func TestBibTeXAndRISRoundTrip(t *testing.T) {
	ss := ParseBibTeX(sampleBib)
	pat := &Source{ID: "p", Type: "patent", Number: "1234567", Journal: "МПК G01S 13/90", Title: "Способ обзора",
		Extra: "№ 2020100001 : заявл. 10.01.2020 : опубл. 15.06.2021", Authors: "Иванов И. И., Петров П. П.", Publisher: "ООО «Пример»", Pages: "12"}
	ss = append(ss, pat)
	want := "Патент № 1234567 Российская Федерация, МПК G01S 13/90. Способ обзора : № 2020100001 : заявл. 10.01.2020 : опубл. 15.06.2021 / И. И. Иванов, П. П. Петров ; заявитель ООО «Пример». – 12 с."
	if got := FormatSource(pat); got != want {
		t.Errorf("патент:\n%s\n%s", got, want)
	}
	for name, conv := range map[string]func([]*Source) []*Source{
		"bibtex": func(s []*Source) []*Source { return ParseBibliography(ExportBibTeX(s)) },
		"ris":    func(s []*Source) []*Source { return ParseBibliography(ExportRIS(s)) },
	} {
		back := conv(ss)
		if len(back) != len(ss) {
			t.Fatalf("%s: записей %d, было %d", name, len(back), len(ss))
		}
		for i := range ss {
			if a, b := FormatSource(ss[i]), FormatSource(back[i]); a != b {
				t.Errorf("%s: запись %d после обмена другая:\n%s\n%s", name, i, a, b)
			}
		}
	}
	if k := bibKey(ss[0]); k != "ivanov2020" {
		t.Errorf("ключ BibTeX %q", k)
	}
}

func TestParseRIS(t *testing.T) {
	src := "TY  - JOUR\r\nAU  - Иванов, Иван\r\nAU  - Smith, J.\r\nTI  - Заголовок статьи\r\nT2  - Вестник\r\nPY  - 2021/05/01\r\nSP  - 3\r\nEP  - 9\r\nIS  - 2\r\nDO  - 10.1/x\r\nER  - \r\n"
	ss := ParseRIS(src)
	if len(ss) != 1 {
		t.Fatalf("записей %d", len(ss))
	}
	s := ss[0]
	if s.Authors != "Иванов И., Smith J." || s.Journal != "Вестник" || s.Year != "2021" || s.Pages != "3–9" || s.URL != "https://doi.org/10.1/x" {
		t.Errorf("RIS: %+v", s)
	}
}

func TestDuplicatesMerge(t *testing.T) {
	a := NewApp(monoTB{}, nil)
	a.cfgDir = ""
	e := a.E
	s1 := &Source{ID: "s1", Type: "book", Authors: "Иванов И. И.", Title: "Основы радиолокации", Year: "2019"}
	s2 := &Source{ID: "s2", Type: "book", Authors: "Иванов И.", Title: "ОСНОВЫ  радиолокации.", Year: "2019", City: "Москва", Publisher: "Наука"}
	s3 := &Source{ID: "s3", Type: "book", Authors: "Петров П.", Title: "Основы радиолокации", Year: "2019"} // другой автор
	e.Doc.Sources = []*Source{s1, s2, s3}
	p := e.Doc.Body[0].P
	p.Runs = []Run{{T: RText, Text: []rune("Текст ")}, {T: RField, Name: "CITE s2,s1 с. 5"}}
	if g := FindDuplicates(e.Doc.Sources); len(g) != 1 || len(g[0]) != 2 {
		t.Fatalf("группы дублей: %v", g)
	}
	if n := e.MergeDuplicates(); n != 1 {
		t.Fatalf("убрано %d", n)
	}
	if len(e.Doc.Sources) != 2 || e.Doc.Sources[0].City != "Москва" {
		t.Errorf("после объединения: %+v", e.Doc.Sources[0])
	}
	if name := e.Doc.Body[0].P.Runs[1].Name; name != "CITE s1 с. 5" {
		t.Errorf("ссылка после объединения: %q", name)
	}
	// импорт того же списка ещё раз ничего не добавляет
	added, skipped := e.ImportSources([]*Source{{ID: "x", Type: "book", Authors: "Иванов И. И.", Title: "основы радиолокации", Year: "2019"}})
	if added != 0 || skipped != 1 {
		t.Errorf("повторный импорт: добавлено %d, пропущено %d", added, skipped)
	}
}
