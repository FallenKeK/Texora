//go:build !windows

package main

// go run . shots <папка> [презентация.pptx]  (презентация для снимка: testdata/demo.pptx) — снимки для README и страницы проекта:
// демо-курсовая (ничьи настоящие данные), нормоконтроль, светлая тема, презентация.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// demoChart — картинка-график для демо-работы.
func demoChart() []byte {
	w, h := 720, 360
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	grid, axis := color.RGBA{0xdd, 0xdd, 0xe3, 255}, color.RGBA{0x55, 0x55, 0x60, 255}
	for i := 0; i <= 5; i++ {
		y := 20 + i*60
		draw.Draw(img, image.Rect(60, y, w-20, y+1), &image.Uniform{grid}, image.Point{}, draw.Src)
	}
	draw.Draw(img, image.Rect(60, 20, 61, 320), &image.Uniform{axis}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(60, 320, w-20, 321), &image.Uniform{axis}, image.Point{}, draw.Src)
	// четыре кривые: n², n², n log n, n log n
	series := []struct {
		c   color.RGBA
		fn  func(x float64) float64
		dot bool
	}{
		{color.RGBA{0xa3, 0x20, 0x20, 255}, func(x float64) float64 { return x * x }, false},
		{color.RGBA{0xed, 0x7d, 0x31, 255}, func(x float64) float64 { return 0.35 * x * x }, false},
		{color.RGBA{0x2f, 0x5d, 0x8c, 255}, func(x float64) float64 { return 0.5 * x * (1 + 2*x) / 3 * 0.18 }, false},
		{color.RGBA{0x7c, 0x3a, 0xed, 255}, func(x float64) float64 { return 0.12 * x }, false},
	}
	for _, s := range series {
		for px := 62; px < w-22; px++ {
			x := float64(px-60) / float64(w-80)
			y := 320 - int(s.fn(x)*290)
			if y < 20 {
				break
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := 0; dx <= 1; dx++ {
					img.Set(px+dx, y+dy, s.c)
				}
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// demoCourse — курсовая на нейтральную тему. bad — с типичными нарушениями для нормоконтроля.
func demoCourse(bad bool) *Doc {
	b, d := gostDoc()
	d.TitlePg = true
	if bad {
		d.Page.Right = 1 * CM
	}
	d.Media["fig1"] = &Media{Mime: "image/png", Data: demoChart()}
	b.add(pCenter, tr("Министерство науки и высшего образования Российской Федерации", fPlain))
	b.add(pCenter, tr("Университет «Пример»", fPlain))
	b.add(pCenter, tr("Кафедра программной инженерии", fPlain))
	b.blank(4)
	b.add(pCenter, tr("КУРСОВАЯ РАБОТА", F{B: true, Size: 16}))
	b.add(pCenter, tr("по дисциплине «Алгоритмы и структуры данных»", fPlain))
	b.add(pCenter, tr("на тему: «Сравнительный анализ алгоритмов сортировки»", fPlain))
	b.blank(4)
	ind := PF{Align: "left", IndL: O(8.5 * CM), IndF: O(0)}
	b.add(ind, tr("Выполнил: студент группы ИС-21", fPlain))
	b.add(ind, tr("Иванов И. И. __________", fPlain))
	b.add(ind, tr("Руководитель: канд. техн. наук", fPlain))
	b.add(ind, tr("Петров П. П. __________", fPlain))
	b.blank(5)
	b.add(pCenter, tr("Москва – 2026", fPlain))

	fill := b.toc("СОДЕРЖАНИЕ", true)
	intro := "Введение"
	if !bad {
		intro = "ВВЕДЕНИЕ"
	}
	b.h1(intro)
	b.body("Сортировка данных входит в число самых частых операций в программах: от построения отчётов до работы баз данных. "+
		"Выбор алгоритма заметно влияет на время отклика системы при больших объёмах данных [1].",
		"Цель работы – сравнить время выполнения основных алгоритмов сортировки на массивах разного размера и дать рекомендации "+
			"по их применению. Для достижения цели решаются задачи: изучить теоретические оценки сложности, реализовать алгоритмы, "+
			"выполнить замеры и проанализировать результаты.")
	p := b.add(pBody, tr("Объект исследования – алгоритмы сортировки сравнением. Предмет исследования – время их выполнения на случайных и частично упорядоченных данных [2].", fPlain))
	if bad {
		p.Runs[0].F.Font, p.Runs[0].F.Size = "Arial", 11
		p.PF.Line = O(1.15)
	}
	b.h1("1 ТЕОРЕТИЧЕСКИЕ ОСНОВЫ И РЕЗУЛЬТАТЫ ЗАМЕРОВ")
	h := "1.1 Оценки сложности"
	if bad {
		h += "."
	}
	b.h2(h)
	b.body("Для оценки алгоритмов используют асимптотическую сложность. Сортировка пузырьком и вставками в худшем случае требует порядка n² операций, " +
		"а быстрая сортировка и сортировка слиянием – порядка n log n [1]. Среднее время быстрой сортировки описывается выражением")
	b.add(PF{Align: "center", IndF: O(0)}, Run{T: REq, Text: []rune(`T(n) = c \cdot n \log_2 n + O(n) \tag{1.1}`)})
	b.h2("1.2 Результаты замеров")
	ref := "Результаты замеров приведены в таблице 1, а их графическое представление – на рисунке 1 [2]."
	if bad {
		ref = "Результаты замеров приведены в таблице 1 [2]."
	}
	b.body(ref)
	b.add(PF{IndF: O(0), KeepNext: true}, tr("Таблица 1 – Время сортировки, мс", fPlain))
	cell := func(s string, hdr bool, right bool) *Cell {
		pf := PF{IndF: O(0)}
		if right {
			pf.Align = "right"
		}
		c := &Cell{Blocks: []Block{{P: &Para{PF: pf, Runs: []Run{TextRun(s, F{Size: 12, B: hdr})}}}}}
		if hdr {
			c.Shade = "#e9e5ff"
		}
		return c
	}
	row := func(hdr bool, vals ...string) *Row {
		r := &Row{Header: hdr}
		for i, v := range vals {
			r.Cells = append(r.Cells, cell(v, hdr, i > 0))
		}
		return r
	}
	d.Body = append(d.Body, Block{T: &Table{Border: true, Cols: []float64{170, 100, 100, 97},
		Rows: []*Row{row(true, "Алгоритм", "10 000", "100 000", "1 000 000"), row(false, "Пузырьком", "180", "18 400", "—"),
			row(false, "Вставками", "62", "6 100", "—"), row(false, "Слиянием", "2", "24", "310"), row(false, "Быстрая", "1", "19", "240")}}})
	b.add(PF{IndF: O(0)})
	fig := b.add(PF{Align: "center", IndF: O(0), KeepNext: true}, Run{T: RImage, ID: "fig1", W: 300, H: 150})
	_ = fig
	b.add(pCenter, tr("Рисунок 1 – Зависимость времени сортировки от размера массива", fPlain))
	b.body("Из полученных данных видно, что для массивов свыше ста тысяч элементов квадратичные алгоритмы неприменимы, " +
		"а быстрая сортировка и сортировка слиянием показывают близкие результаты.")
	concl := "ЗАКЛЮЧЕНИЕ"
	if bad {
		concl = "Заключение"
	}
	b.h1(concl)
	b.body("В ходе работы выполнено сравнение алгоритмов сортировки. Показано, что выбор алгоритма определяется размером данных " +
		"и требованиями к устойчивости сортировки. Поставленные задачи решены, цель работы достигнута.")
	b.h1("СПИСОК ИСПОЛЬЗОВАННЫХ ИСТОЧНИКОВ")
	src := []string{
		"Кормен Т. Х., Лейзерсон Ч. И., Ривест Р. Л., Штайн К. Алгоритмы: построение и анализ. – 3-е изд. – Москва : Вильямс, 2013. – 1328 с.",
		"Седжвик Р. Алгоритмы на Java. – 4-е изд. – Москва : Вильямс, 2013. – 848 с.",
	}
	if bad {
		src = append(src, "Скиена С. Алгоритмы. Руководство по разработке. – Санкт-Петербург : БХВ-Петербург, 2011. – 720 с.")
	}
	b.sources("src", src...)
	fill(nil)
	return d
}

func runShots(tb TextBackend, args []string) {
	out := "docs/screenshots"
	if len(args) > 0 {
		out = args[0]
	}
	_ = os.MkdirAll(out, 0o755)
	spell.Load("")
	cv := newCanvas(1366, 860)
	// каждый снимок — новое приложение: без лишних вкладок и следов прошлого
	fresh := func(d *Doc, theme, navMode string) *App {
		a := NewApp(tb, &fakePlatform{})
		a.cfgDir = ""
		a.Set = defaultSettings()
		a.Set.ShowNav, a.Set.NavMode = true, navMode
		a.applyTheme()
		a.SetThemeMode(theme)
		a.BaseS = 1
		a.W, a.H = 1366, 860
		a.newTab(d, "Курсовая работа.docx", "")
		a.closeTab(0) // пустой «Документ1», с которым стартует приложение
		a.backstage = false
		a.zoom = 1
		a.Paint(cv)
		a.Paint(cv)
		return a
	}
	save := func(a *App, name string) {
		for i := 0; i < 3; i++ {
			a.Paint(cv)
		}
		savePNG(cv, filepath.Join(out, name+".png"))
	}
	// показать абзац в начале страницы
	show := func(a *App, prefix string, off int) {
		p := findParaByPrefix(a.E.Doc, prefix)
		if c := a.E.Caret(Pos{P: p}); c != nil {
			a.gotoPage(c.Page)
		}
		a.E.SetCaret(Pos{P: p, O: off}, false)
		a.Paint(cv)
		a.Paint(cv)
	}
	a := fresh(demoCourse(false), "dark", "pages")
	show(a, "Результаты замеров приведены", 40)
	save(a, "editor")
	a = fresh(demoCourse(false), "light", "headings")
	show(a, "Сортировка данных входит", 60)
	save(a, "light")
	a = fresh(demoCourse(true), "dark", "pages")
	a.runNormCheck()
	a.normGoto("line")
	if c := a.E.Caret(a.E.Sel.F); c != nil { // страница целиком, а не её конец
		a.scrollY = a.pageY(c.Page) - a.d(22)
		a.clampScroll()
	}
	save(a, "normcheck")
	if len(args) > 1 {
		a = NewApp(tb, &fakePlatform{})
		a.cfgDir = ""
		a.Set = defaultSettings()
		a.Set.ShowNav = true
		a.applyTheme()
		a.SetThemeMode("dark")
		a.BaseS = 1
		a.W, a.H = 1366, 860
		a.OpenPath(args[1])
		a.backstage = false
		for i := 0; i < 120; i++ {
			a.Paint(cv)
		}
		a.SetZoom(0.62) // видны сразу два слайда: карточки и диаграмма с таблицей
		a.pdfGoto(1, -1)
		for i := 0; i < 120; i++ {
			a.Paint(cv)
		}
		save(a, "presentation")
	}
	fmt.Println("снимки:", out)
}

func findParaByPrefix(d *Doc, prefix string) *Para {
	var out *Para
	walkParas(d.Body, func(p *Para, _ bool) {
		if t := p.Text(); out == nil && len(t) >= len(prefix) && t[:len(prefix)] == prefix {
			out = p
		}
	})
	return out
}
