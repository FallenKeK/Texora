//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
)

// runScenario — «живой» сценарий: печатаем как пользователь, жмём кнопки, сохраняем во все форматы.
func runScenario(tb TextBackend) {
	fp := &fakePlatform{}
	a := NewApp(tb, fp)
	a.BaseS, a.W, a.H = 1, 1366, 860
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.backstage = false
	cv := newCanvas(1366, 860)
	a.Paint(cv)
	typ := func(s string) {
		for _, r := range s {
			if r == '\n' {
				a.KeyDown(13, Mods{})
				continue
			}
			a.Char(r)
		}
	}
	key := func(vk int, m Mods) { a.KeyDown(vk, m) }
	ctrl := Mods{Ctrl: true}

	key(49, Mods{Ctrl: true, Alt: true}) // Заголовок 1
	typ("Отчёт о работе за квартал\n")
	typ("Это обычный абзац. В нём есть ")
	key(66, ctrl)
	typ("полужирный")
	key(66, ctrl)
	typ(", ")
	key(73, ctrl)
	typ("курсив")
	key(73, ctrl)
	typ(" и сноска")
	a.Command("footnote")
	typ("Текст сноски внизу страницы.")
	key(27, Mods{})
	key(35, ctrl)
	typ(". Абзац выровнен по ширине, чтобы проверить переносы строк и растяжение пробелов между словами в длинной строке.")
	key(74, ctrl)
	typ("\n")
	key(74, ctrl) // обратно влево
	key(76, ctrl)
	key(50, Mods{Ctrl: true, Alt: true})
	typ("Список задач\n")
	a.Command("bullets")
	typ("Первая задача\nВторая задача\n")
	key(9, Mods{})
	typ("Вложенный пункт\n\n")
	a.Command("numbering")
	typ("Пункт один\nПункт два\n\n")
	typ("Таблица результатов:\n")
	a.E.InsertTable(3, 3)
	for i, s := range []string{"Показатель", "План", "Факт", "Выручка", "100", "120", "Расходы", "80", "75"} {
		if i > 0 {
			key(9, Mods{})
		}
		typ(s)
	}
	a.Command("bold") // ничего страшного
	key(35, ctrl)
	typ("Цвет и размер: ")
	a.E.SetColor("#c00000")
	a.E.SetFont("Times New Roman")
	a.E.SetSize(14)
	typ("красный Times 14")
	a.E.SetColor("")
	a.E.SetFont("")
	a.E.SetSize(11)
	typ(" обычный. H")
	key(187, ctrl)
	typ("2")
	key(187, ctrl)
	typ("O и E = mc")
	key(187, Mods{Ctrl: true, Shift: true})
	typ("2")
	key(187, Mods{Ctrl: true, Shift: true})
	typ("\n")
	// картинка
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y * 2), 200, 255})
		}
	}
	var pb bytes.Buffer
	_ = png.Encode(&pb, img)
	_ = os.WriteFile("/tmp/wln_pic.png", pb.Bytes(), 0o644)
	a.InsertImageFile("/tmp/wln_pic.png")
	typ("\n")
	typ("Ссылка: ")
	a.E.SetLink("https://example.com")
	typ("example.com")
	a.E.SetLink("")
	typ("\n")
	a.Command("pagebreak")
	typ("Вторая страница. ")
	key(53, ctrl)
	typ("Полуторный интервал для этого абзаца, длинного достаточно, чтобы занять пару строк текста на странице документа.")
	// колонтитулы
	a.Command("header")
	typ("Отчёт · стр. ")
	a.Command("pagenum")
	typ(" из ")
	a.Command("numpages")
	key(27, Mods{})
	// оглавление в начало
	key(36, ctrl)
	a.Command("toc")
	// поиск/замена
	n := a.E.ReplaceAll("задача", "задание", FindOpts{})
	fmt.Println("замен:", n)
	// копипаст
	key(36, ctrl)
	a.E.Find("Вторая страница", FindOpts{})
	key(67, ctrl)
	key(35, ctrl)
	typ(" ")
	key(86, ctrl)
	a.E.Relayout()
	st := a.E.Stats()
	fmt.Printf("страниц %d, слов %d, абзацев %d\n", st.Pages, st.Words, st.Paras)

	for _, ext := range []string{"docx", "odt", "rtf", "txt"} {
		fp.save = "/tmp/scen." + ext
		a.SaveAs(ext)
	}
	fp.save = "/tmp/scen.pdf"
	a.Command("pdf")
	fmt.Println("статус:", a.status, "dirty:", a.E.Dirty)

	a.scrollY = 0
	a.Paint(cv)
	savePNG(cv, "/tmp/scen_ui.png")
	// всплывающие окна
	shots := []struct {
		name string
		do   func()
	}{
		{"font", func() { a.fontPopup(a.anchorOf("font")) }},
		{"color", func() {
			a.colorPopup("color", a.anchorFor("color"), themeColors, standardColors, "Авто", func(string) {})
		}},
		{"table", func() {
			a.tab = "insert"
			a.Paint(cv)
			a.tablePicker(a.anchorFor("table"))
			a.popup.Move(a.popup.R.X+80, a.popup.R.Y+90)
		}},
		{"para", func() { a.tab = "home"; a.paragraphDialog() }},
		{"layout", func() { a.dialog = nil; a.tab = "layout" }},
		{"file", func() { a.openHome("home") }},
		{"saveas", func() { a.bsPane = "templates" }},
		{"dark", func() {
			a.backstage = false
			a.newFromTemplate("thesis")
			a.switchTab(-1)
			a.Command("new")
			a.closeTab(a.cur)
			a.switchTab(-1)
			fmt.Println("вкладок:", len(a.tabs), a.FileName)
			a.SetThemeMode("dark")
			a.tab = "home"
			a.contextMenu(500, 400, a.toPage(500, 400))
		}},
	}
	for _, s := range shots {
		a.popup = nil
		s.do()
		a.Paint(cv)
		savePNG(cv, "/tmp/scen_"+s.name+".png")
	}
	// перечитываем сохранённое
	for _, ext := range []string{"docx", "odt", "rtf"} {
		data, _ := os.ReadFile("/tmp/scen." + ext)
		d, err := OpenBytes("x."+ext, data)
		if err != nil {
			fmt.Println(ext, "ошибка чтения:", err)
			continue
		}
		t := BlocksToText(d.Body)
		fmt.Printf("%s: %d симв, сносок %d, верх %q, картинок %d, есть таблица: %v\n", ext, len([]rune(t)), len(d.Footnotes),
			strings.TrimSpace(BlocksToText(d.Header)), len(d.Media), strings.Contains(t, "Выручка"))
	}
}
