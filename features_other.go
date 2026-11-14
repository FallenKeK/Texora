//go:build !windows

package main

// go run . features — проверка новых функций: документ со всем сразу, картинки страниц, круговое сохранение.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"
)

func testPNG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			k := uint8((x + y) * 255 / (w + h))
			img.Set(x, y, color.RGBA{c.R/2 + k/2, c.G/2 + k/3, c.B, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func featuresDoc() *Doc {
	b, d := gostDoc()
	d.TitlePg = false
	d.Hyphen = true
	d.EvenOdd = true
	d.Track = false
	// титул — отдельный раздел без номера
	b.add(pCenter, tr("МИНИСТЕРСТВО НАУКИ И ВЫСШЕГО ОБРАЗОВАНИЯ", fPlain))
	b.blank(8)
	b.add(pCenter, tr("КУРСОВАЯ РАБОТА", fBold))
	b.add(pCenter, tr("Проверка возможностей Texora 1.2", fPlain))
	b.blank(10)
	last := b.add(pCenter, tr("Москва 2026", fPlain))
	last.PF.Sect = &Section{ID: "title", Page: d.Page, TitlePg: true, HF: HF{FooterFirst: []Block{{P: NewPara()}}}}
	fill := b.toc("СОДЕРЖАНИЕ", false)
	b.h1("ВВЕДЕНИЕ")
	p := b.add(pBody, tr("Эта работа праверяет орфографию: в предложении есть ошибкка и опечатка «малако». ", fPlain))
	p.Runs = append(p.Runs, tr("Этот фрагмент с примечанием", F{Cmt: "c1"}), tr(" и ", fPlain),
		tr("вставленный текст", F{Ins: "r1"}), tr(" рядом с ", fPlain), tr("удалённым ", F{Del: "r1"}),
		tr("словом. Ссылка на учебник ", fPlain), Run{T: RField, Name: "CITE src1 25"}, tr(" и на статью ", fPlain),
		Run{T: RField, Name: "CITE src2"}, tr(".", fPlain))
	d.Comments["c1"] = &Comment{Author: "Иван Петров", Initials: "ИП", Date: "2026-11-06T18:00:00Z", Text: "Проверьте формулировку и добавьте ссылку на ГОСТ."}
	d.Revs["r1"] = &Rev{Author: "Мария", Date: "2026-11-06T18:05:00Z"}
	b.body("Автоматическая расстановка переносов делает текст по ширине аккуратнее: длинные слова вроде «электроэнергетический», «высокопроизводительный» и «межгосударственный» переносятся по правилам русского языка, а не растягивают пробелы.")
	b.h2("1.1 Формулы")
	b.add(pBody, tr("Корни квадратного уравнения находят по формуле ", fPlain), Run{T: REq, Text: []rune(`x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`)}, tr(", где a ≠ 0.", fPlain))
	b.add(PF{Align: "center"}, Run{T: REq, Text: []rune(`\sum_{i=1}^{n} i^2 = \frac{n(n+1)(2n+1)}{6}`)})
	b.add(PF{Align: "center"}, Run{T: REq, Text: []rune(`\int_0^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2}`)})
	b.add(PF{Align: "center"}, Run{T: REq, Text: []rune(`\lim_{x \to 0} \frac{\sin x}{x} = 1, \quad \left( \frac{a}{b} \right)^2, \quad \vec{F} = m\vec{a}`)})
	b.h2("1.2 Картинки с обтеканием")
	d.Media["img1"] = &Media{Mime: "image/png", Data: testPNG(240, 180, color.RGBA{60, 90, 200, 255})}
	d.Media["img2"] = &Media{Mime: "image/png", Data: testPNG(400, 120, color.RGBA{200, 80, 60, 255})}
	b.add(pBody, Run{T: RImage, ID: "img1", W: 120, H: 90, Wrap: "square", Side: "left"},
		tr("Картинка слева, текст обтекает её справа. Этот абзац достаточно длинный, чтобы занять несколько строк рядом с рисунком и показать, как строки становятся короче, пока не закончится высота картинки. После этого текст снова идёт на всю ширину страницы, как в Word при обтекании «вокруг рамки».", fPlain))
	b.body("Следующий абзац тоже начинается рядом с картинкой, если она ещё не закончилась по высоте, — так ведёт себя Word.")
	b.add(pBody, Run{T: RImage, ID: "img2", W: 260, H: 78, Wrap: "topbottom", Side: "center"},
		tr("Картинка «сверху и снизу» стоит по центру над этим абзацем, текст её не огибает.", fPlain))
	// альбомный раздел
	pl := b.add(pBody, tr("Ниже — альбомная страница с широкой таблицей.", fPlain))
	pl.PF.Sect = &Section{ID: "body1", Page: d.Page}
	land := d.Page
	land.W, land.H = d.Page.H, d.Page.W
	b.h1("1 ШИРОКАЯ ТАБЛИЦА")
	t := NewTable(3, 6, land.W-land.Left-land.Right)
	for i, h := range []string{"Показатель", "Янв", "Фев", "Мар", "Апр", "Итого"} {
		t.Rows[0].Cells[i].Blocks[0].P.Runs = []Run{tr(h, fBold)}
	}
	d.Body = append(d.Body, Block{T: t})
	lp := b.add(pBody, tr("Конец альбомного раздела.", fPlain))
	lp.PF.Sect = &Section{ID: "land", Page: land, Type: ""}
	b.h1("ЗАКЛЮЧЕНИЕ")
	b.body("Разделы, колонтитулы чётных и нечётных страниц, оглавление, ссылки и список литературы работают вместе.")
	b.add(PF{Heading: 1, PB: true}, tr("СПИСОК ИСПОЛЬЗОВАННЫХ ИСТОЧНИКОВ", fPlain))
	d.Sources = []*Source{
		{ID: "src1", Type: "book", Authors: "Иванов И. И., Петров П. П.", Title: "Экономика предприятия", Extra: "учебник для вузов", City: "Москва", Publisher: "Юрайт", Year: "2023", Pages: "412"},
		{ID: "src2", Type: "article", Authors: "Сидорова А. В.", Title: "Цифровые платформы в логистике", Journal: "Вопросы экономики", Year: "2024", Issue: "5", Pages: "45-58"},
		{ID: "src3", Type: "web", Title: "Росстат", Extra: "официальный сайт", URL: "https://rosstat.gov.ru", Accessed: "01.11.2026"},
	}
	b.add(PF{Region: "bib"})
	// колонтитулы: чётные — название работы, нечётные — номер справа
	d.Footer = []Block{{P: &Para{PF: PF{Align: "right"}, Runs: []Run{{T: RField, Name: "PAGE"}}}}}
	d.FooterEven = []Block{{P: &Para{PF: PF{Align: "left"}, Runs: []Run{{T: RField, Name: "PAGE"}, tr(" · Курсовая работа", F{Size: 10})}}}}
	fill(nil)
	UpdateBib(d)
	return d
}

// runFeatures возвращает число форматов (docx, odt), которые не прошли круговое сохранение.
// RTF теряет новые вещи by design (см. ARCHITECTURE.md) и в счёт не идёт.
func runFeatures(tb TextBackend) (fails int) {
	spell.Load("")
	d := featuresDoc()
	m := NewMeasurer(tb)
	L := m.Paginate(d, map[*Para]*ParaL{})
	fmt.Printf("страниц: %d, разделов: %d\n", len(L.Pages), len(d.Sections()))
	for i, pg := range L.Pages {
		fmt.Printf("  лист %d: №%d раздел %d %.0fx%.0f верх=%q низ=%q\n", i+1, pg.Num, pg.Sec, pg.G.Page.W, pg.G.Page.H, pg.HStory, pg.FStory)
	}
	a := NewApp(tb, &fakePlatform{})
	a.cfgDir = ""
	for i, pg := range L.Pages {
		k := 1.2
		cv := newCanvas(int(pg.G.Page.W*k), int(pg.G.Page.H*k))
		cv.Clear(Hex(0xffffff))
		pp := &PagePainter{cv: cv, tb: tb, doc: d, k: k, imgs: map[string]*scaledImg{}, marks: true, m: m, spell: a.spellMarks}
		a.E.SetDoc(d)
		pp.DrawPage(L, pg, "body", true)
		savePNG(cv, fmt.Sprintf("/tmp/feat_p%d.png", i+1))
	}
	// круговое сохранение
	text0 := BlocksToText(d.Body)
	for _, ext := range []string{"docx", "odt", "rtf"} {
		out, err := SaveBytes(d, ext)
		if err != nil {
			fmt.Println(ext, "ошибка:", err)
			fails++
			continue
		}
		os.WriteFile("/tmp/feat."+ext, out, 0o644)
		d2, err := OpenBytes("x."+ext, out)
		if err != nil {
			fmt.Println(ext, "не читается:", err)
			fails++
			continue
		}
		L2 := m.Paginate(d2, map[*Para]*ParaL{})
		eqs, cmts, ins, del, floats, cites, refs, regions := 0, 0, 0, 0, 0, 0, 0, map[string]int{}
		for _, en := range Flatten(&d2.Body, nil, nil) {
			if en.P.PF.Region != "" {
				regions[en.P.PF.Region]++
			}
			for _, r := range en.P.Runs {
				switch {
				case r.T == REq:
					eqs++
				case r.T == RImage && r.Wrap != "":
					floats++
				case r.T == RField && strings.HasPrefix(r.Name, "CITE"):
					cites++
				case r.T == RField && strings.HasPrefix(r.Name, "PAGEREF"):
					refs++
				}
				if r.F.Cmt != "" {
					cmts++
				}
				if r.F.Ins != "" {
					ins++
				}
				if r.F.Del != "" {
					del++
				}
			}
		}
		same := strings.TrimSpace(BlocksToText(d2.Body)) == strings.TrimSpace(text0)
		if ext != "rtf" && (!same || len(L2.Pages) != len(L.Pages)) {
			fails++
		}
		fmt.Printf("%s: страниц %d (было %d), разделов %d, формул %d, плавающих %d, ссылок %d, PAGEREF %d, области %v, примечаний %d/%d, вставок %d, удалений %d, источников %d, чёт/нечёт %v, текст совпал %v\n",
			ext, len(L2.Pages), len(L.Pages), len(d2.Sections()), eqs, floats, cites, refs, regions, cmts, len(d2.Comments), ins, del, len(d2.Sources), d2.EvenOdd, same)
		if !same && ext == "docx" {
			a := strings.Split(strings.TrimSpace(text0), "\n")
			b := strings.Split(strings.TrimSpace(BlocksToText(d2.Body)), "\n")
			for i := 0; i < len(a) && i < len(b); i++ {
				if a[i] != b[i] {
					fmt.Printf("  строка %d:\n   было %q\n   стало %q\n", i, a[i], b[i])
					break
				}
			}
			fmt.Println("  строк было/стало:", len(a), len(b))
		}
	}
	// орфография
	for _, w := range []string{"праверяет", "ошибкка", "малако", "проверяет", "молоко", "электроэнергетический", "Москва", "столами", "переделывающимися", "running", "teh", "Word's"} {
		fmt.Printf("  %s → %v %v\n", w, spell.Correct(w), spell.Suggest(w, 4))
	}
	fmt.Println("переносы:", Hyphenate([]rune("электроэнергетический")), Hyphenate([]rune("hyphenation")))
	return
}

// runInteractive возвращает число проваленных проверок.
func runInteractive(tb TextBackend) (fails int) {
	fp := &fakePlatform{}
	a := NewApp(tb, fp)
	a.BaseS, a.W, a.H = 1, 1366, 860
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.backstage = false
	cv := newCanvas(1366, 860)
	a.Paint(cv)
	e := a.E
	typ := func(s string) {
		for _, r := range s {
			if r == '\n' {
				a.KeyDown(13, Mods{})
				continue
			}
			a.Char(r)
		}
	}
	check := func(name string, ok bool) {
		mark := "OK "
		if !ok {
			mark = "ОШИБКА "
			fails++
		}
		fmt.Println(mark + name)
	}
	a.KeyDown(49, Mods{Ctrl: true, Alt: true})
	typ("Глава первая\nОбычный текст главы.\n")
	a.KeyDown(49, Mods{Ctrl: true, Alt: true})
	typ("Глава вторая\nТекст второй главы.")
	// исправления
	a.KeyDown(69, Mods{Ctrl: true, Shift: true})
	check("режим исправлений включается Ctrl+Shift+E", e.Doc.Track)
	typ(" Новое")
	a.KeyDown(8, Mods{}) // свою вставку стираем по-настоящему
	p := e.Sel.F.P
	check("Backspace по своей вставке удаляет символ", strings.HasSuffix(p.Text(), " Ново"))
	a.KeyDown(36, Mods{}) // в начало строки
	a.KeyDown(46, Mods{}) // Delete по старому тексту — помечает
	check("Delete по старому тексту помечает удаление", p.Runs[0].F.Del != "" && strings.HasPrefix(p.Text(), "Текст"))
	check("курсор перешёл за удалённый символ", e.Sel.F.O == 1)
	n := e.ResolveRevisions(false, true)
	check(fmt.Sprintf("отклонить все (%d)", n), p.Text() == "Текст второй главы." && !e.HasRevisions())
	a.KeyDown(69, Mods{Ctrl: true, Shift: true})
	// примечание
	e.SelectWord(Pos{P: p, O: 2})
	id := e.AddComment("Проверить", "Тест")
	check("примечание создано", id != "" && e.CommentAt() == id)
	e.DeleteComment(id)
	check("примечание удалено", len(e.Doc.Comments) == 0 && e.CommentAt() == "")
	// формула
	a.KeyDown(35, Mods{})
	e.InsertEquation(`\frac{a}{b}`)
	check("формула вставлена", RunAt(p, p.Len()-1, REq) >= 0)
	// раздел с альбомной ориентацией
	e.InsertSectionBreak("")
	a.setPage(func(pg *Page) { pg.W, pg.H = pg.H, pg.W })
	ss := e.Doc.Sections()
	check(fmt.Sprintf("разделов 2, второй альбомный (%d)", len(ss)), len(ss) == 2 && ss[1].Page.W > ss[1].Page.H && ss[0].Page.W < ss[0].Page.H)
	check("страниц стало 2 разной ориентации", len(e.L.Pages) == 2 && e.L.Pages[1].G.Page.W > e.L.Pages[0].G.Page.W)
	// колонтитул второго раздела
	e.SetStory(e.PageStory(1, "header"), 1)
	typ("Верх второго раздела")
	e.SetStory("body", 0)
	check("колонтитул второго раздела на второй странице", e.L.Pages[1].Header != nil && e.L.Pages[0].Header == nil)
	// оглавление в начало
	e.SetCaret(Pos{P: e.flat[0].P}, false)
	ok := e.InsertTOC()
	from, to := RegionSpan(e.Doc, "toc")
	check(fmt.Sprintf("оглавление вставлено (%d пунктов)", to-from), ok && to-from == 2)
	ref := ""
	for _, r := range e.Doc.Body[from].P.Runs {
		if r.T == RField {
			ref = e.Doc.FieldValue(r.Name, "", "")
		}
	}
	check("номер страницы в оглавлении = "+ref, ref == "1")
	// источники и ссылка
	e.SaveSource(&Source{Type: "book", Authors: "Иванов И. И.", Title: "Книга", City: "Москва", Publisher: "Наука", Year: "2020", Pages: "100"})
	e.InsertCitation(e.Doc.Sources[0].ID, "")
	e.InsertBibliography()
	bf, bt := RegionSpan(e.Doc, "bib")
	check("список литературы", bt-bf == 1 && strings.Contains(e.Doc.Body[bf].P.Text(), "Иванов, И. И. Книга"))
	// отмена/повтор
	before := len(e.Doc.Body)
	e.Undo()
	e.Undo()
	e.Redo()
	e.Redo()
	check("отмена и повтор восстанавливают документ", len(e.Doc.Body) == before && len(e.Doc.Sections()) == 2)
	// поиск по регулярке
	e.SetCaret(Pos{P: e.flat[0].P}, false)
	found := e.Find(`Глава\s+(\w+)`, FindOpts{Regex: true})
	cnt := e.ReplaceAll(`Глава (\w+)`, "Часть $1", FindOpts{Regex: true})
	check(fmt.Sprintf("регулярки: найдено, заменено %d", cnt), found && cnt >= 2)
	// стиль
	e.SetStyleDef("h1", headingStyle{Size: 20, B: true, Color: "#ff0000", Align: "center"})
	var h1 *Para
	for _, en := range e.flat {
		if en.P.PF.Heading == 1 {
			h1 = en.P
			break
		}
	}
	ef := EffF(e.Doc, h1, F{})
	check("изменение стиля применилось", ef.Size == 20 && EffPF(e.Doc, h1).Align == "center")
	// переносы
	e.Doc.Hyphen = true
	e.Changed()
	a.Paint(cv)
	savePNG(cv, "/tmp/tx_interactive.png")
	out, err := SaveBytes(e.Doc, "docx")
	d2, err2 := OpenBytes("x.docx", out)
	check("сохранение и чтение docx", err == nil && err2 == nil && len(d2.Sections()) == 2 && d2.Styles["h1"].Size == 20)
	return
}

func runPDFTest(args []string) {
	for _, path := range args {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Println(err)
			continue
		}
		t0 := time.Now()
		d, err := LoadPDF(data)
		if err != nil {
			fmt.Println(path, "ошибка:", err)
			continue
		}
		fmt.Printf("%s: страниц %d, оглавление %d, открыт за %v\n", filepath.Base(path), len(d.Pages), len(d.Outline), time.Since(t0))
		n := min(len(d.Pages), 3)
		for i := 0; i < n; i++ {
			t1 := time.Now()
			img, gl := d.RenderPage(i, 1.5, false)
			txt, _ := PageText(gl)
			fmt.Printf("  стр. %d: %dx%d за %v, символов %d: %q\n", i+1, img.Bounds().Dx(), img.Bounds().Dy(), time.Since(t1), len(gl), firstN(txt, 90))
			f, _ := os.Create(fmt.Sprintf("/tmp/pdft_%s_%d.png", strings.TrimSuffix(filepath.Base(path), ".pdf"), i+1))
			png.Encode(f, img)
			f.Close()
		}
	}
}

func firstN(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " / "))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// runPDFView — проверка просмотрщика PDF в интерфейсе (скриншоты /tmp/tx_pdf_*.png).
func runPDFView(tb TextBackend, file string) {
	a := NewApp(tb, &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.applyTheme()
	a.BaseS = 1
	a.Set.ShowNav = true
	cv := newCanvas(1366, 860)
	a.W, a.H = 1366, 860
	settle := func() {
		for i := 0; i < 400; i++ {
			a.Paint(cv)
			v := a.pdf
			v.mu.Lock()
			pend := false
			for _, r := range v.want {
				if v.needs(r) {
					pend = true
				}
			}
			s := v.searching || v.pending != nil || v.pendMsg != ""
			v.mu.Unlock()
			if !pend && !s {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		a.Paint(cv)
	}
	shot := func(name string) {
		settle()
		savePNG(cv, "/tmp/tx_pdf_"+name+".png")
	}
	t0 := time.Now()
	a.OpenPath(file)
	if a.pdf == nil {
		fmt.Println("не открылся:", a.dialog != nil)
		return
	}
	shot("open")
	fmt.Printf("открыт за %v, вкладок %d, имя %s, зум %.2f\n", time.Since(t0), len(a.tabs), a.FileName, a.zoom)
	a.Set.NavMode = "headings"
	a.pdfGoto(1, -1)
	shot("outline")
	a.Set.NavMode = "pages"
	a.pdfFind("Даламбера", false, 1)
	shot("find")
	fmt.Println("найдено:", a.pdf.found != nil, a.status)
	a.pdfFind("несуществующеесловоxyz", false, 1)
	settle()
	fmt.Println("пусто:", a.status)
	a.pdfSelectAll()
	a.pdfCopy()
	cl := a.P.(*fakePlatform).clip.Text
	fmt.Printf("скопировано %d символов: %q…\n", len([]rune(cl)), firstN(cl, 120))
	// ссылки на странице оглавления
	a.pdfGoto(0, -1)
	settle()
	links := 0
	for _, h := range a.hits {
		if strings.HasPrefix(h.id, "pdflink:") {
			links++
		}
	}
	fmt.Println("ссылок на экране:", links)
	for _, h := range a.hits {
		if strings.HasPrefix(h.id, "pdflink:") {
			h.act()
			break
		}
	}
	fmt.Println("после ссылки страница:", a.pdfCurPage()+1)
	// выделение мышью
	a.pdfGoto(2, -1)
	settle()
	x0, y0 := a.pageLeftOf(2)+a.d(150), a.pageTop(2)+a.d(200)
	a.MouseDown(x0, y0, Mods{}, false)
	a.MouseMove(x0+a.d(300), y0+a.d(60), true)
	a.MouseUp(x0+a.d(300), y0+a.d(60))
	fmt.Printf("выделено: %q\n", firstN(a.pdfSelText(), 100))
	a.SetZoom(2.2)
	shot("zoom")
	a.SetThemeMode("light")
	a.fitPage()
	shot("fitpage")
	a.closeTab(a.cur)
	fmt.Println("после закрытия вкладок:", len(a.tabs), a.pdf == nil)
}

// runEqTest — формулы: картинка /tmp/eqtest.png и обмен через OMML / MathML.
// Возвращает число формул, которые не пережили обмен.
func runEqTest(tb TextBackend) (fails int) {
	m := NewMeasurer(tb)
	cv := newCanvas(1100, 120*len(eqTestSet)/2+60)
	cv.Clear(Hex(0xffffff))
	for i, src := range eqTestSet {
		box := m.EqLayout(src, F{Size: 14})
		k := 1.25
		if box.W*k > 520 {
			k = 520 / box.W
		}
		pp := &PagePainter{cv: cv, tb: tb, k: k, alpha: 1}
		x0 := float64(20 + (i%2)*545)
		y0 := float64(20 + (i/2)*120)
		pp.ox = x0
		pp.oy = y0 + 55 - (box.Desc-box.Asc)/2*k
		pp.drawEq(box, 0, 0, Hex(0x111111))
		cv.FillRect(Rect{x0, y0 + 112, 520, 1}, Hex(0xdddddd))
		// обмен
		om := "<m:oMath xmlns:m=\"m\" xmlns:w=\"w\">" + omml(ParseEq(src)) + "</m:oMath>"
		root, err := ParseXML([]byte(om))
		back := ""
		if err == nil {
			back = OMMLToLinear(root)
		}
		mm, err := ParseXML([]byte(EqMathML(src)))
		backM := ""
		if err == nil {
			backM = MathMLToLinear(mm)
		}
		same := func(a, b string) bool { return EqPlain(a) == EqPlain(b) }
		fmt.Printf("%2d OMML %v  MathML %v  | %s\n", i+1, same(src, back), same(src, backM), EqPlain(src))
		if !same(src, back) || !same(src, backM) {
			fails++
		}
		if !same(src, back) {
			fmt.Println("    OMML→", back, "\n         ", EqPlain(back))
		}
		if !same(src, backM) {
			fmt.Println("    MML→ ", backM, "\n         ", EqPlain(backM))
		}
	}
	savePNG(cv, "/tmp/eqtest.png")
	return
}

// runCaptions — названия, номера формул, перекрёстные ссылки, перечни; обмен docx/odt/rtf.
// runCaptions возвращает число форматов (docx, odt), где после чтения разошлись номера или ссылки.
func runCaptions(tb TextBackend) (fails int) {
	b, d := gostDoc()
	m := NewMeasurer(tb)
	e := NewEditor(m)
	e.SetDoc(d)
	img := func() Run {
		d.Media["img1"] = &Media{Data: testPNG(240, 120, color.RGBA{60, 120, 200, 255}), Mime: "image/png"}
		return Run{T: RImage, ID: "img1", W: 240, H: 120}
	}
	// собираем документ «по-настоящему» через редактор
	b.h1("ВВЕДЕНИЕ")
	ref1 := b.add(pBody, tr("Во введении ссылаемся вперёд: схема показана на ", fPlain))
	b.h1("1 Теория")
	b.add(pBody, tr("Закон Ома в дифференциальной форме:", fPlain))
	eqHere := b.add(pBody)
	b.add(pCenter, img())
	figHere := d.Body[len(d.Body)-1].P
	b.add(pBody, tr("Матрица системы и её определитель:", fPlain))
	eq2Here := b.add(pBody)
	b.h1("2 Практика")
	tbl := NewTable(2, 2, 450)
	tbl.Rows[0].Cells[0].Blocks[0].P.Runs = []Run{tr("U, В", fPlain)}
	tbl.Rows[0].Cells[1].Blocks[0].P.Runs = []Run{tr("I, А", fPlain)}
	d.Body = append(d.Body, Block{T: tbl})
	b.add(pCenter, img())
	fig2 := d.Body[len(d.Body)-1].P
	refs := b.add(pBody, tr("Итоги: ", fPlain))
	b.h1("ПРИЛОЖЕНИЕ А")
	b.add(pCenter, img())
	figA := d.Body[len(d.Body)-1].P
	lofHere := b.add(pBody)
	e.SetDoc(d)
	e.refresh()
	at := func(p *Para) { e.Sel = Sel{Pos{P: p, O: p.Len()}, Pos{P: p, O: p.Len()}} }
	at(figHere)
	e.InsertCaption("Рисунок", "Схема измерительной установки", true)
	at(eqHere)
	e.InsertNumberedEquation(`\vec{j} = \sigma \vec{E}`, true)
	at(eq2Here)
	e.InsertNumberedEquation(`A = \begin{pmatrix} 1 & 2 \\ 3 & 4 \end{pmatrix}, \quad \det A = -2`, true)
	e.Sel = Sel{Pos{P: tbl.Rows[0].Cells[0].Blocks[0].P}, Pos{P: tbl.Rows[0].Cells[0].Blocks[0].P}}
	e.InsertCaption("Таблица", "Результаты измерений", true)
	at(fig2)
	e.InsertCaption("Рисунок", "Вольт-амперная характеристика", true)
	at(figA)
	e.InsertCaption("Рисунок", "Фото стенда", true)
	caps := Captions(d, "")
	bm := map[string]string{}
	for _, c := range caps {
		fmt.Printf("  %-8s %-5s %s\n", c.Cat, c.Num, c.Text)
		bm[c.Num+c.Cat] = c.BM
	}
	at(ref1)
	e.InsertCrossRef(bm["1.1Рисунок"], "s")
	at(refs)
	e.InsertCrossRef(bm["2.1Таблица"], "l")
	e.InsertText(", формула ")
	e.InsertCrossRef(bm["1.2Формула"], "p")
	e.InsertText(", ")
	e.InsertCrossRef(bm["А.1Рисунок"], "l")
	e.InsertText(", номер ")
	e.InsertCrossRef(bm["2.1Рисунок"], "n")
	at(lofHere)
	e.InsertLOF("Рисунок")
	L := m.Paginate(d, map[*Para]*ParaL{})
	fmt.Println("ссылки:", strings.TrimSpace(BlocksToText([]Block{{P: ref1}})), "|", strings.TrimSpace(BlocksToText([]Block{{P: refs}})))
	for i, pg := range L.Pages {
		k := 1.2
		cv := newCanvas(int(pg.G.Page.W*k), int(pg.G.Page.H*k))
		cv.Clear(Hex(0xffffff))
		pp := &PagePainter{cv: cv, tb: tb, doc: d, k: k, imgs: map[string]*scaledImg{}, m: m}
		pp.DrawPage(L, pg, "body", true)
		savePNG(cv, fmt.Sprintf("/tmp/cap_p%d.png", i+1))
	}
	sig := func(d *Doc) string {
		d.computeSeq()
		var out []string
		for _, c := range Captions(d, "") {
			out = append(out, c.Num+c.Cat)
		}
		var rs []string
		for _, en := range Flatten(&d.Body, nil, nil) {
			for _, r := range en.P.Runs {
				if r.T == RField && strings.HasPrefix(r.Name, "REF") {
					rs = append(rs, d.FieldValue(r.Name, "", ""))
				}
			}
		}
		lof := 0
		for _, b := range d.Body {
			if b.P != nil && strings.HasPrefix(b.P.PF.Region, "lof:") {
				lof++
			}
		}
		return strings.Join(out, ",") + " | " + strings.Join(rs, "; ") + fmt.Sprintf(" | перечень %d", lof)
	}
	s0 := sig(d)
	fmt.Println("исходно:", s0)
	for _, ext := range []string{"docx", "odt", "rtf"} {
		out, err := SaveBytes(d, ext)
		if err != nil {
			fmt.Println(ext, err)
			fails++
			continue
		}
		os.WriteFile("/tmp/cap."+ext, out, 0o644)
		d2, err := OpenBytes("x."+ext, out)
		if err != nil {
			fmt.Println(ext, err)
			fails++
			continue
		}
		s2 := sig(d2)
		fmt.Printf("%s: %v  %s\n", ext, s2 == s0, map[bool]string{true: "", false: s2}[s2 == s0])
		if ext != "rtf" && s2 != s0 {
			fails++
		}
	}
	// окна
	a := NewApp(tb, &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.applyTheme()
	a.BaseS = 1
	cv := newCanvas(1366, 860)
	a.W, a.H = 1366, 860
	a.newTab(d, "Курсовая.docx", "")
	a.backstage = false
	a.tab = "refs"
	shot := func(n string) { a.Paint(cv); a.Paint(cv); savePNG(cv, "/tmp/tx_"+n+".png") }
	a.crossRefDialog()
	shot("xref")
	a.dialog = nil
	a.eqPal = 1
	a.eqDialog(nil, -1)
	a.dialog.Fields[0].Edit = NewTextEdit(`f(x) = \begin{cases} x^2, & x \ge 0 \\ -x, & x < 0 \end{cases}`)
	a.dialog.Fields[len(a.dialog.Fields)-2].On = true
	shot("eqpal")
	a.dialog = nil
	a.captionDialog()
	shot("capdlg")
	return
}

// runPerf — сколько стоит кадр (go run . perf [файл]).
func runPerf(tb TextBackend, args []string) {
	a := NewApp(tb, &fakePlatform{})
	a.Set = defaultSettings()
	a.applyTheme()
	a.BaseS = 1.25
	cv := newCanvas(1920, 1040)
	a.W, a.H = 1920, 1040
	if len(args) > 0 {
		a.OpenPath(args[0])
	} else {
		a.newTab(featuresDoc(), "Проверка.docx", "")
	}
	a.backstage = false
	a.Paint(cv)
	f, _ := os.Create("/tmp/perf.prof")
	pprof.StartCPUProfile(f)
	t0 := time.Now()
	n := 40
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			a.scrollY += 40
		}
		a.hover = map[bool]string{true: "b:bold", false: ""}[i%3 == 0]
		a.Tick()
		a.Paint(cv)
	}
	pprof.StopCPUProfile()
	f.Close()
	fmt.Printf("кадр: %.1f мс\n", float64(time.Since(t0).Microseconds())/1000/float64(n))
}

// runPDFPerf — время кадров и отрисовки листов скана; клики по вкладкам панели.
func runPDFPerf(tb TextBackend, file string) {
	a := NewApp(tb, &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.applyTheme()
	a.BaseS = 1
	if os.Getenv("TXS") != "" {
		fmt.Sscan(os.Getenv("TXS"), &a.BaseS)
	}
	a.Set.ShowNav = true
	W, H := int(1366*a.BaseS), int(860*a.BaseS)
	cv := newCanvas(W, H)
	a.W, a.H = float64(W), float64(H)
	t0 := time.Now()
	a.OpenPath(file)
	if a.pdf == nil {
		fmt.Println("не открылся")
		return
	}
	fmt.Println("открыт за", time.Since(t0), "листов", len(a.pdf.Doc.Pages), "зум", a.zoom)
	frame := func() time.Duration {
		t := time.Now()
		a.Paint(cv)
		return time.Since(t)
	}
	var worst time.Duration
	for i := 0; i < 300; i++ {
		d := frame()
		if d > worst {
			worst = d
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Println("худший кадр при загрузке:", worst, "последний:", frame())
	savePNG(cv, "/tmp/tx_pdfperf_open.png")
	a.pdfGoto(60, -1)
	for i := 0; i < 200; i++ {
		frame()
		time.Sleep(20 * time.Millisecond)
	}
	savePNG(cv, "/tmp/tx_pdfperf_60.png")
	// клик по каждой кнопке с id nav:*
	for _, id := range []string{"navtab:headings", "navtab:pages"} {
		_ = id
	}
	for _, h := range a.hits {
		if strings.HasPrefix(h.id, "nav") && !strings.HasPrefix(h.id, "navp:") {
			fmt.Println("кнопка", h.id, h.r)
		}
	}
	t := time.Now()
	n := 0
	for time.Since(t) < 2*time.Second {
		a.Wheel(-120, Mods{})
		frame()
		n++
	}
	fmt.Printf("прокрутка: %d кадров за 2с\n", n)
	// листаем миниатюры до конца и смотрим память
	var ms runtime.MemStats
	peak := uint64(0)
	t = time.Now()
	for time.Since(t) < 8*time.Second {
		a.mx, a.my = a.navRect.X+20, a.navRect.Y+200
		a.Wheel(-240, Mods{})
		frame()
		runtime.ReadMemStats(&ms)
		peak = max(peak, ms.HeapInuse)
		time.Sleep(15 * time.Millisecond)
	}
	runtime.GC()
	runtime.ReadMemStats(&ms)
	fmt.Printf("память: пик %d МБ, после GC %d МБ, картинок %d\n", peak>>20, ms.HeapInuse>>20, len(a.pdf.Doc.images))
	savePNG(cv, "/tmp/tx_pdfperf_scroll.png")
}
