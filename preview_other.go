//go:build !windows

package main

// Режим проверки вне Windows:
//   go run . ui                      — скриншоты интерфейса
//   go run . render in.docx out      — вёрстка страниц в PNG + круговое сохранение

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

func savePNG(cv *Canvas, name string) {
	img := image.NewRGBA(image.Rect(0, 0, cv.W, cv.H))
	for i, p := range cv.Pix {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = uint8(p>>16), uint8(p>>8), uint8(p), 255
	}
	f, _ := os.Create(name)
	_ = png.Encode(f, img)
	f.Close()
}

func newCanvas(w, h int) *Canvas {
	cv := &Canvas{W: w, H: h, Pix: make([]uint32, w*h)}
	cv.ResetClip()
	return cv
}

func renderPages(tb TextBackend, d *Doc, L *Layout, prefix string, k float64, maxPages int) {
	for i, pg := range L.Pages {
		if i >= maxPages {
			break
		}
		cv := newCanvas(int(pg.G.Page.W*k), int(pg.G.Page.H*k))
		cv.Clear(Hex(0xffffff))
		pp := &PagePainter{cv: cv, tb: tb, doc: d, k: k, imgs: map[string]*scaledImg{}}
		pp.DrawPage(L, pg, "body", true)
		savePNG(cv, fmt.Sprintf("%s_p%d.png", prefix, i+1))
	}
}

func main() {
	tb := newTTFText()
	args := os.Args[1:]
	if len(args) >= 3 && args[0] == "render" {
		data, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		d, err := OpenBytes(filepath.Base(args[1]), data)
		if err != nil {
			fmt.Println("ОШИБКА:", err)
			os.Exit(1)
		}
		m := NewMeasurer(tb)
		L := m.Paginate(d, map[*Para]*ParaL{})
		fmt.Printf("страниц: %d, сносок: %d, верх: %q, низ: %q, шрифт: %s %.1f\n", len(L.Pages), len(d.Footnotes),
			strings.TrimSpace(BlocksToText(d.Header)), strings.TrimSpace(BlocksToText(d.Footer)), d.Def.Font, d.Def.Size)
		renderPages(tb, d, L, args[2], 1.0, 3)
		pdf, err := ExportPDFVector(d, L, tb, nil)
		if err == nil {
			_ = os.WriteFile(args[2]+".pdf", pdf, 0o644)
		}
		for _, ext := range []string{"docx", "odt", "rtf"} {
			out, err := SaveBytes(d, ext)
			if err != nil {
				fmt.Println("сохранение", ext, err)
				continue
			}
			_ = os.WriteFile(args[2]+"_out."+ext, out, 0o644)
		}
		return
	}
	if len(args) >= 1 && args[0] == "mkres" {
		mkres()
		return
	}
	if len(args) >= 1 && args[0] == "perf" {
		runPerf(tb, args[1:])
		return
	}
	if len(args) >= 1 && args[0] == "captions" {
		runCaptions(tb)
		return
	}
	if len(args) >= 1 && args[0] == "eqtest" {
		runEqTest(tb)
		return
	}
	if len(args) >= 2 && args[0] == "pdfperf" {
		runPDFPerf(tb, args[1])
		return
	}
	if len(args) >= 2 && args[0] == "pdfview" {
		runPDFView(tb, args[1])
		return
	}
	if len(args) >= 1 && args[0] == "pdftest" {
		runPDFTest(args[1:])
		return
	}
	if len(args) >= 1 && args[0] == "interactive" {
		runInteractive(tb)
		return
	}
	if len(args) >= 1 && args[0] == "features" {
		runFeatures(tb)
		return
	}
	if len(args) >= 1 && args[0] == "shots" {
		runShots(tb, args[1:])
		return
	}
	if len(args) >= 2 && args[0] == "normshot" {
		runNormShot(tb, args[1:])
		return
	}
	if len(args) >= 1 && args[0] == "scenario" {
		runScenario(tb)
		return
	}
	a := NewApp(tb, &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.applyTheme()
	a.BaseS = 1
	a.Diag = "Windows 10.0.22631 · 96 dpi"
	a.Set.Recent = []string{`C:\Users\Иван\Documents\Курсовая по экономике.docx`, `C:\Users\Иван\Documents\Отчёт за октябрь.docx`, `D:\Учёба\Реферат история.odt`}
	a.Set.Favorites = a.Set.Recent[:1]
	cv := newCanvas(1366, 860)
	a.W, a.H = 1366, 860
	shot := func(name string) {
		a.Paint(cv)
		a.Paint(cv)
		savePNG(cv, "/tmp/tx_"+name+".png")
	}
	shot("home")
	a.bsPane = "templates"
	shot("templates")
	a.bsPane = "settings"
	shot("settings")
	a.bsPane = "about"
	shot("about")
	a.newFromTemplate("course")
	shot("editor")
	a.Set.NavMode = "headings"
	a.E.SetCaret(Pos{P: a.E.flat[40].P}, false)
	a.scrollCaret = true
	shot("headings")
	a.newFromTemplate("report")
	a.SetThemeMode("light")
	a.Set.NavMode = "pages"
	shot("light")
	a.newFromTemplate("letter")
	a.SetThemeMode("violet")
	a.colorPopup("color", a.anchorFor("color"), themeColors, standardColors, "Авто", func(string) {})
	shot("violet")
	a.popup = nil
	a.SetThemeMode("dark")
	a.Set.Lang = "en"
	a.paragraphDialog()
	shot("en_dialog")
	a.dialog = nil
	a.openHome("home")
	shot("en_home")
	// новые функции 1.2
	a.Set.Lang = "ru"
	spell.Load("")
	a.newTab(featuresDoc(), "Проверка 1.2.docx", "")
	a.backstage = false
	a.Set.ShowNav = false
	a.tab = "review"
	a.zoom = 0.9
	e := a.E
	for _, en := range e.flat {
		if strings.Contains(en.P.Text(), "праверяет") {
			e.SetCaret(Pos{P: en.P, O: 15}, false)
			break
		}
	}
	a.scrollCaret = true
	shot("review")
	a.openPalette()
	for _, r := range "табл" {
		a.popup.Char(r)
	}
	shot("palette")
	a.popup = nil
	a.tab = "insert"
	shot("insp_insert")
	a.tab = "page"
	shot("insp_page")
	w0, h0 := a.W, a.H
	a.W, a.H = 1100, 760
	cvn := newCanvas(1100, 760)
	a.Paint(cvn)
	a.inspPeek, a.tab = true, "format"
	a.Paint(cvn)
	a.Paint(cvn)
	savePNG(cvn, "/tmp/tx_narrow_peek.png")
	a.inspPeek = false
	a.W, a.H = w0, h0
	c := e.Caret(e.Sel.F)
	k := a.k()
	a.contextMenu(a.pageLeftOf(c.Page)+c.X*k, a.pageTop(c.Page)+(c.Y+c.H)*k, PagePoint{Page: c.Page, X: c.X, Y: c.Y + 2})
	shot("spellmenu")
	a.popup = nil
	a.tab = "insert"
	a.eqDialog(nil, -1)
	a.dialog.Fields[0].Edit = NewTextEdit(`x = \frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`)
	shot("eqdialog")
	a.dialog = nil
	a.tab = "refs"
	a.sourceDialog(e.Doc.Sources[0])
	shot("source")
	a.dialog = nil
	a.tab = "layout"
	a.zoom = 0.6
	for i := range e.L.Pages {
		if e.L.Pages[i].G.Page.W > e.L.Pages[i].G.Page.H {
			a.scrollY = a.pageY(i) - a.d(40)
			break
		}
	}
	a.clampScroll()
	shot("landscape")
}

type fakePlatform struct {
	clip ClipData
	save string
	open string
}

func (f *fakePlatform) Invalidate()                                                       {}
func (f *fakePlatform) Minimize()                                                         {}
func (f *fakePlatform) ToggleMaximize()                                                   {}
func (f *fakePlatform) Close()                                                            {}
func (f *fakePlatform) Quit()                                                             {}
func (f *fakePlatform) SetTitle(string)                                                   {}
func (f *fakePlatform) OpenDialog(string) string                                          { return f.open }
func (f *fakePlatform) SaveDialog(name, ext string) (string, string)                      { return f.save, ext }
func (f *fakePlatform) SetClipboard(c ClipData)                                           { f.clip = c }
func (f *fakePlatform) GetClipboard() ClipData                                            { return f.clip }
func (f *fakePlatform) Print(int, func(int, float64) *image.RGBA, float64, float64) error { return nil }
func (f *fakePlatform) SystemDark() bool                                                  { return false }
func (f *fakePlatform) OpenURL(string)                                                    {}
func (f *fakePlatform) Fonts() []string                                                   { return nil }

func init() {
	if len(os.Args) > 1 && os.Args[1] == "tplpng" {
		tb := newTTFText()
		a := NewApp(tb, &fakePlatform{})
		a.cfgDir = ""
		for _, t := range templatesList {
			d := a.templateDoc(t.id)
			e := NewEditor(a.M)
			e.SetDoc(d)
			for i := range e.L.Pages {
				if i > 1 {
					break
				}
				img := RenderPageImage(d, e.L, tb, i, 60, nil)
				f, _ := os.Create(fmt.Sprintf("/tmp/tpl_%s_%d.png", t.id, i))
				png.Encode(f, img)
				f.Close()
			}
			data, _ := WriteDocx(d)
			os.WriteFile("/tmp/tpl_"+t.id+".docx", data, 0o644)
			fmt.Println(t.id, len(e.L.Pages), "стр.")
		}
		os.Exit(0)
	}
}

// normShot — снимки: документ с панелью нормоконтроля (и после «Исправить» первой группы), презентация.
func runNormShot(tb TextBackend, files []string) {
	a := NewApp(tb, &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.applyTheme()
	a.BaseS = 1
	cv := newCanvas(1366, 860)
	a.W, a.H = 1366, 860
	for i, f := range files {
		a.OpenPath(f)
		a.backstage = false
		name := fmt.Sprintf("/tmp/tx_norm%d", i+1)
		if isPptx(f) {
			for k := 0; k < 100; k++ {
				a.Paint(cv)
			}
			savePNG(cv, name+".png")
			continue
		}
		a.runNormCheck()
		a.Paint(cv)
		a.Paint(cv)
		savePNG(cv, name+".png")
		if gs := normGroups(a.normIssues()); len(gs) > 0 {
			a.normGoto(gs[0].k.ID)
			a.Paint(cv)
			a.Paint(cv)
			savePNG(cv, name+"_goto.png")
		}
	}
}
