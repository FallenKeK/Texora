package main

// Texora: бренд, темы, настройки, вкладки документов, панель навигации, главный кадр.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

const AppName = "Texora"

// ---------------------------------------------------------------- настройки
type Settings struct {
	Theme           string   `json:"theme"`  // dark, light, violet, system
	Accent          string   `json:"accent"` // "" — фирменный фиолетовый
	UIScale         float64  `json:"uiScale"`
	AutosaveMin     int      `json:"autosaveMin"`
	AutosaveFile    bool     `json:"autosaveFile"`
	Lang            string   `json:"lang"`
	Recent          []string `json:"recent"`
	Favorites       []string `json:"favorites"`
	ShowNav         bool     `json:"showNav"`
	CapChapter      bool     `json:"capChapter"`
	NavMode         string   `json:"navMode"`
	Author          string   `json:"author"`
	HideMarkup      bool     `json:"hideMarkup"`
	NoSpell         bool     `json:"noSpell"`
	NoGrid          bool     `json:"noGrid"` // не показывать сетку таблиц без границ
	HideInspector   bool     `json:"hideInspector"`
	NoAutoUpdate    bool     `json:"noAutoUpdate"` // не проверять обновления раз в сутки
	LastUpdateCheck int64    `json:"lastUpdateCheck"`
	SkipVersion     string   `json:"skipVersion"`   // «пропустить эту версию»
	PendingUpdate   string   `json:"pendingUpdate"` // скачанный exe, который поставить при следующем запуске
}

func defaultSettings() Settings {
	return Settings{Theme: "dark", UIScale: 1, AutosaveMin: 1, Lang: "ru", ShowNav: true, NavMode: "pages", CapChapter: true}
}

func (a *App) loadSettings() {
	a.Set = defaultSettings()
	if a.cfgDir == "" {
		return
	}
	if b, err := os.ReadFile(filepath.Join(a.cfgDir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &a.Set)
	} else {
		// перенос недавних файлов из прежней версии (WordLike)
		old := filepath.Join(filepath.Dir(a.cfgDir), "WordLike", "recent.txt")
		if b, err := os.ReadFile(old); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					a.Set.Recent = append(a.Set.Recent, l)
				}
			}
		}
	}
	if a.Set.UIScale < 0.8 || a.Set.UIScale > 2 {
		a.Set.UIScale = 1
	}
	if a.Set.Lang != "en" {
		a.Set.Lang = "ru"
	}
}

func (a *App) saveSettings() {
	if a.cfgDir == "" {
		return
	}
	if b, err := json.MarshalIndent(a.Set, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(a.cfgDir, "settings.json"), b, 0o644)
	}
}

// ---------------------------------------------------------------- темы
type Theme struct {
	Bg, Surface, Border, Text, TextDim, Hover, Press, Accent, AccentText, Canvas, PageShadow, Divider Color
	Popup, Field, Sel, Accent2, Nav                                                                   Color
	Dark                                                                                              bool
}

// Темы с 1.5: нейтральный графит и бумажная светлая — фиолетовый только акцентом (курсор, выделение, активное).
// «violet» — прежняя фиолетовая тема, для тех, кому она по душе.
var themes = map[string]Theme{
	"dark": {Bg: Hex(0x111214), Surface: Hex(0x17181b), Border: Hex(0x2a2c31), Text: Hex(0xe4e5e8), TextDim: Hex(0x8a8e97),
		Hover: RGBA(255, 255, 255, 14), Press: RGBA(255, 255, 255, 26), Accent: Hex(0x7c3aed), AccentText: Hex(0xffffff),
		Canvas: Hex(0x0c0d0f), PageShadow: RGBA(0, 0, 0, 170), Divider: Hex(0x222328), Popup: Hex(0x1c1d21), Field: Hex(0x101114),
		Sel: RGBA(124, 58, 237, 95), Accent2: Hex(0xa78bfa), Nav: Hex(0x141518), Dark: true},
	"light": {Bg: Hex(0xf4f4f5), Surface: Hex(0xfbfbfb), Border: Hex(0xdfe0e4), Text: Hex(0x16171a), TextDim: Hex(0x6a6e77),
		Hover: RGBA(20, 22, 30, 12), Press: RGBA(20, 22, 30, 24), Accent: Hex(0x7c3aed), AccentText: Hex(0xffffff),
		Canvas: Hex(0xe8e9ec), PageShadow: RGBA(20, 22, 30, 45), Divider: Hex(0xe6e7ea), Popup: Hex(0xffffff), Field: Hex(0xffffff),
		Sel: RGBA(124, 58, 237, 60), Accent2: Hex(0x6d28d9), Nav: Hex(0xf7f7f8)},
	"violet": {Bg: Hex(0x171329), Surface: Hex(0x221c3a), Border: Hex(0x3d3461), Text: Hex(0xdcd6ef), TextDim: Hex(0x9d93bf),
		Hover: RGBA(255, 255, 255, 18), Press: RGBA(255, 255, 255, 34), Accent: Hex(0x7c3aed), AccentText: Hex(0xffffff),
		Canvas: Hex(0x110e20), PageShadow: RGBA(0, 0, 0, 150), Divider: Hex(0x3a3160), Popup: Hex(0x292341), Field: Hex(0x1b162f),
		Sel: RGBA(124, 58, 237, 90), Accent2: Hex(0xa78bfa), Nav: Hex(0x1c1731), Dark: true},
}

// themeNames — подписи тем в настройках и инспекторе.
var themeNames = []struct{ id, name string }{{"dark", "Графит"}, {"light", "Светлая"}, {"violet", "Фиолетовая"}, {"system", "Как в Windows"}}

var accentColors = []struct{ name, hex string }{{"Фиолетовый", ""}, {"Синий", "#2563eb"}, {"Бирюзовый", "#0d9488"},
	{"Зелёный", "#16a34a"}, {"Оранжевый", "#ea580c"}, {"Розовый", "#db2777"}}

func (a *App) applyTheme() {
	m := a.Set.Theme
	if m == "system" {
		m = "light"
		if a.P != nil && a.P.SystemDark() {
			m = "dark"
		}
	}
	th, ok := themes[m]
	if !ok {
		th = themes["dark"]
	}
	if a.Set.Accent != "" {
		c := parseHexColor(a.Set.Accent, th.Accent)
		th.Accent = c
		if th.Dark {
			th.Accent2 = Mix(c, Hex(0xffffff), 0.35)
		} else {
			th.Accent2 = c
		}
		th.Sel = c.WithA(0.3)
	}
	a.th = th
}

func (a *App) SetThemeMode(m string) {
	a.Set.Theme = m
	a.applyTheme()
	a.saveSettings()
}

// ---------------------------------------------------------------- создание
func NewApp(tb TextBackend, p Platform) *App {
	// мягкий предел памяти: сборщик мусора работает чаще, а не раздувает процесс до гигабайтов
	debug.SetMemoryLimit(700 << 20)
	m := NewMeasurer(tb)
	eqMeasure = m
	a := &App{tb: tb, M: m, P: p, UIFont: "Segoe UI", MonoFont: "Consolas", anim: map[string]float64{}, tab: "format",
		CaretOn: true, lastColor: "#c00000", lastBg: "#ffff00", tplCache: map[string]*tplPreview{}}
	if dir, err := os.UserConfigDir(); err == nil {
		a.cfgDir = filepath.Join(dir, AppName)
		_ = os.MkdirAll(a.cfgDir, 0o755)
	}
	a.loadSettings()
	a.applyTheme()
	cfg := a.cfgDir
	go func() {
		spell.Load(cfg)
		a.docGen.Add(1) // слой страницы кэшируется: после загрузки словаря подчёркивания надо перерисовать
		if p != nil {
			p.Invalidate()
		}
	}()
	a.newTab(NewDoc(), "", "")
	a.backstage, a.bsPane = true, "home"
	return a
}

// ---------------------------------------------------------------- вкладки документов
type DocTab struct {
	E                      *Editor
	FileName, FilePath     string
	zoom, scrollX, scrollY float64
	imgSel                 *Pos
	PageCache              map[string]*scaledImg
	Pdf                    *PdfView
}

func (a *App) stash() {
	if a.cur < 0 || a.cur >= len(a.tabs) {
		return
	}
	t := a.tabs[a.cur]
	t.E, t.FileName, t.FilePath, t.zoom, t.scrollX, t.scrollY, t.imgSel, t.PageCache, t.Pdf =
		a.E, a.FileName, a.FilePath, a.zoom, a.scrollX, a.scrollY, a.imgSel, a.PageCache, a.pdf
}

func (a *App) activate(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.stash()
	a.cur = i
	t := a.tabs[i]
	a.E, a.FileName, a.FilePath, a.zoom, a.scrollX, a.scrollY, a.imgSel, a.PageCache, a.pdf =
		t.E, t.FileName, t.FilePath, t.zoom, t.scrollX, t.scrollY, t.imgSel, t.PageCache, t.Pdf
	a.popup = nil
	a.drag, a.dragSel = nil, false
	a.navScroll = 0
	a.invalidate()
}

func (a *App) newTab(d *Doc, name, path string) {
	a.stash()
	e := NewEditor(a.M)
	e.Author = a.authorName()
	e.SetDoc(d)
	if name == "" {
		a.untitledN++
		name = fmt.Sprintf("Документ%d.docx", a.untitledN)
	}
	a.tabs = append(a.tabs, &DocTab{E: e, FileName: name, FilePath: path, zoom: 1, PageCache: map[string]*scaledImg{}})
	a.cur = -1
	a.activate(len(a.tabs) - 1)
}

// pristine — нетронутый пустой документ, который можно заменить открываемым файлом.
func (a *App) pristine() bool {
	d := a.E.Doc
	return a.FilePath == "" && !a.E.Dirty && len(d.Body) == 1 && d.Body[0].P != nil && d.Body[0].P.Len() == 0 &&
		len(d.Header) == 0 && len(d.Footer) == 0
}

// useDoc показывает документ: в текущей вкладке, если она пустая, иначе — в новой.
func (a *App) useDoc(d *Doc, name, path string) {
	if a.pristine() {
		a.E.SetDoc(d)
		a.FileName, a.FilePath = name, path
		a.PageCache = map[string]*scaledImg{}
		a.imgSel = nil
		a.scrollX, a.scrollY, a.zoom = 0, 0, 1
		if name == "" {
			a.untitledN++
			a.FileName = fmt.Sprintf("Документ%d.docx", a.untitledN)
		}
	} else {
		a.newTab(d, name, path)
	}
	a.backstage = false
	a.after()
}

func (a *App) switchTab(dir int) {
	if len(a.tabs) < 2 {
		return
	}
	a.activate((a.cur + dir + len(a.tabs)) % len(a.tabs))
}

func (a *App) closeTab(i int) {
	a.activate(i)
	a.withSaved(func() {
		if a.pdf != nil {
			a.pdf.Close()
			a.pdf = nil
		}
		a.dropRecovery(a.E)
		idx := a.cur
		a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
		a.cur = -1
		if len(a.tabs) == 0 {
			a.newTab(NewDoc(), "", "")
			a.backstage, a.bsPane = true, "home"
			return
		}
		a.activate(min(idx, len(a.tabs)-1))
	})
}

// withSaved выполняет действие после вопроса о несохранённых изменениях в текущем документе.
func (a *App) withSaved(then func()) {
	if !a.E.Dirty {
		then()
		return
	}
	a.backstage = false
	a.confirm(AppName, a.trf("Сохранить изменения в документе «%s»?", a.FileName),
		DlgBtn{Label: "Отмена"},
		DlgBtn{Label: "Не сохранять", Act: func() bool { a.dialog = nil; then(); return false }},
		DlgBtn{Label: "Сохранить", Primary: true, Act: func() bool {
			a.dialog = nil
			if a.Save() {
				then()
			}
			return false
		}})
}

// RequestClose — закрытие окна: по очереди спрашиваем про каждый изменённый документ.
func (a *App) RequestClose() {
	a.stash()
	for i, t := range a.tabs {
		if t.E.Dirty {
			a.activate(i)
			a.withSaved(func() {
				a.E.Dirty = false
				a.dropRecovery(a.E)
				a.RequestClose()
			})
			return
		}
	}
	a.saveSettings()
	a.endRecoverySession()
	if a.afterClose != nil { // например, подмена exe при обновлении
		a.afterClose()
		a.afterClose = nil
	}
	a.P.Quit()
}

func (a *App) WindowTitle() string {
	t := strings.TrimSuffix(a.FileName, filepath.Ext(a.FileName))
	if a.E.Dirty {
		t += " *"
	}
	return t + " — " + AppName
}

// ---------------------------------------------------------------- файлы
func (a *App) addRecent(p string) {
	out := []string{p}
	for _, r := range a.Set.Recent {
		if !strings.EqualFold(r, p) && len(out) < 30 {
			out = append(out, r)
		}
	}
	a.Set.Recent = out
	a.saveSettings()
}

func (a *App) isFav(p string) bool {
	for _, f := range a.Set.Favorites {
		if strings.EqualFold(f, p) {
			return true
		}
	}
	return false
}

func (a *App) toggleFav(p string) {
	var out []string
	found := false
	for _, f := range a.Set.Favorites {
		if strings.EqualFold(f, p) {
			found = true
			continue
		}
		out = append(out, f)
	}
	if !found {
		out = append([]string{p}, out...)
	}
	a.Set.Favorites = out
	a.saveSettings()
}

func (a *App) OpenPath(p string) {
	for i, t := range a.tabs { // уже открыт — просто переключаемся
		fp := t.FilePath
		if i == a.cur {
			fp = a.FilePath
		}
		if fp != "" && strings.EqualFold(filepath.Clean(fp), filepath.Clean(p)) {
			a.activate(i)
			a.backstage = false
			return
		}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		a.errorBox(a.tr("Не удалось открыть файл:") + "\n" + err.Error())
		return
	}
	if isPDF(p, data) {
		a.openPDF(p, data)
		return
	}
	if isPptx(p) {
		pdf, err := PptxToPDF(data)
		if err != nil {
			a.errorBox(a.trf("Не удалось открыть «%s»:", filepath.Base(p)) + "\n" + a.tr(err.Error()))
			return
		}
		a.openPDF(p, pdf)
		return
	}
	d, err := OpenBytes(filepath.Base(p), data)
	if err != nil {
		a.errorBox(a.trf("Не удалось открыть «%s»:", filepath.Base(p)) + "\n" + err.Error())
		return
	}
	name, path := filepath.Base(p), p
	old := !a.saveable(extOf(p))
	if old {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + ".docx"
		path = ""
	}
	a.useDoc(d, name, path)
	if old {
		a.msg(a.tr("Открыт файл старого формата — сохранение будет в .docx"))
	} else {
		a.msg(a.tr("Открыт:") + " " + p)
	}
	a.addRecent(p)
}

func (a *App) saveable(ext string) bool {
	for _, f := range SaveFormats {
		if f.Ext == ext && ext != "pdf" {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- автосохранение и восстановление
//
// Копии лежат в recover/<сессия>/<номер>.docx (+ .txt: имя и путь). Сессия — один запуск Texora:
// «pid-время». Живая сессия раз в минуту обновляет файл alive. При запуске предлагаются только копии
// мёртвых сессий (процесса нет или alive давно не обновлялся), поэтому две открытые Texora
// не путают и не стирают копии друг друга. При нормальном выходе папка сессии удаляется.

const recoverStale = 3 * time.Minute

func (a *App) recoverDir() string { return filepath.Join(a.cfgDir, "recover") }

func (a *App) sessionDir() string {
	if a.recSess == "" {
		a.recSess = fmt.Sprintf("%d-%x", os.Getpid(), time.Now().UnixNano())
	}
	return filepath.Join(a.recoverDir(), a.recSess)
}

// heartbeat — «сессия жива»: pid в файле alive, время — дата изменения файла.
func (a *App) heartbeat() {
	if a.cfgDir == "" {
		return
	}
	dir := a.sessionDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "alive"), []byte(fmt.Sprint(os.Getpid())), 0o644)
}

func (a *App) recoverKey(e *Editor) string {
	if a.recKeys == nil {
		a.recKeys = map[*Editor]string{}
	}
	k, ok := a.recKeys[e]
	if !ok {
		a.recSeq++
		k = fmt.Sprint(a.recSeq)
		a.recKeys[e] = k
	}
	return filepath.Join(a.sessionDir(), k)
}

func (a *App) dropRecovery(e *Editor) {
	if a.cfgDir == "" {
		return
	}
	k := a.recoverKey(e)
	os.Remove(k + ".docx")
	os.Remove(k + ".txt")
}

// endRecoverySession — нормальный выход: копии этой сессии больше не нужны.
func (a *App) endRecoverySession() {
	if a.cfgDir != "" && a.recSess != "" {
		os.RemoveAll(a.sessionDir())
	}
}

// Autosave вызывается платформой раз в минуту.
func (a *App) Autosave() {
	a.stash()
	a.heartbeat()
	a.saveTicks++
	if a.Set.AutosaveMin <= 0 || a.saveTicks%a.Set.AutosaveMin != 0 || a.cfgDir == "" {
		return
	}
	for i, t := range a.tabs {
		if !t.E.Dirty {
			continue
		}
		if a.Set.AutosaveFile && t.FilePath != "" && a.saveable(extOf(t.FilePath)) && a.dialog == nil {
			cur := a.cur
			a.activate(i)
			a.writeTo(a.FilePath)
			a.activate(cur)
			continue
		}
		if data, err := WriteDocx(t.E.Doc); err == nil {
			k := a.recoverKey(t.E)
			_ = os.WriteFile(k+".docx", data, 0o644)
			_ = os.WriteFile(k+".txt", []byte(t.FileName+"\n"+t.FilePath), 0o644)
		}
	}
}

// AutosaveAll — немедленная копия всех изменённых документов (перед аварийным выходом).
func (a *App) AutosaveAll() {
	m := a.Set.AutosaveMin
	a.Set.AutosaveMin, a.saveTicks = 1, 0
	a.Autosave()
	a.Set.AutosaveMin = m
}

// deadSessionFiles — копии, которые никто из запущенных Texora не ведёт, и папки их сессий.
func (a *App) deadSessionFiles() (files, dirs []string) {
	root := a.recoverDir()
	// копии версий до 1.4.2 лежали прямо в recover/ — их хозяина уже не узнать
	flat, _ := filepath.Glob(filepath.Join(root, "*.docx"))
	files = append(files, flat...)
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if !e.IsDir() || e.Name() == a.recSess {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if sessionAlive(dir) {
			continue
		}
		fs, _ := filepath.Glob(filepath.Join(dir, "*.docx"))
		files = append(files, fs...)
		dirs = append(dirs, dir)
	}
	// старая копия из WordLike
	if old := filepath.Join(filepath.Dir(a.cfgDir), "WordLike", "recover.docx"); fileExists(old) {
		files = append(files, old)
	}
	return
}

func sessionAlive(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "alive"))
	if err != nil {
		return false
	}
	if time.Since(st.ModTime()) > recoverStale {
		return false
	}
	b, _ := os.ReadFile(filepath.Join(dir, "alive"))
	pid := 0
	fmt.Sscan(string(b), &pid)
	return pid > 0 && pid != os.Getpid() && processAlive(pid)
}

func (a *App) OfferRecovery() {
	if a.cfgDir == "" {
		return
	}
	a.heartbeat()
	files, dirs := a.deadSessionFiles()
	clear := func() {
		for _, f := range files {
			os.Remove(f)
			os.Remove(strings.TrimSuffix(f, ".docx") + ".txt")
		}
		for _, d := range dirs {
			os.RemoveAll(d)
		}
	}
	if len(files) == 0 {
		clear() // пустые папки упавших сессий
		return
	}
	var names []string
	for _, f := range files {
		info, _ := os.ReadFile(strings.TrimSuffix(f, ".docx") + ".txt")
		n := strings.SplitN(string(info), "\n", 2)[0]
		if n == "" {
			n = a.tr("Документ")
		}
		names = append(names, "«"+n+"»")
	}
	a.confirm(a.tr("Восстановление"), a.trf("Texora был закрыт, не сохранив изменения: %s. Восстановить последние копии?", strings.Join(names, ", ")),
		DlgBtn{Label: "Нет", Act: func() bool { clear(); return true }},
		DlgBtn{Label: "Восстановить", Primary: true, Act: func() bool {
			for _, f := range files {
				data, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				d, err := ReadDocx(data)
				if err != nil {
					continue
				}
				info, _ := os.ReadFile(strings.TrimSuffix(f, ".docx") + ".txt")
				parts := strings.SplitN(string(info), "\n", 2)
				name, path := "", ""
				if parts[0] != "" {
					name = parts[0]
				}
				if len(parts) > 1 {
					path = strings.TrimSpace(parts[1])
				}
				a.useDoc(d, name, path)
				a.E.Dirty = true
			}
			clear()
			a.AutosaveAll() // восстановленное сразу под защитой этой сессии
			return true
		}})
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ---------------------------------------------------------------- прочее
func (a *App) toggleNav(mode string) {
	if mode == "" || (a.Set.ShowNav && a.Set.NavMode == mode) {
		a.Set.ShowNav = !a.Set.ShowNav
	} else {
		a.Set.ShowNav = true
	}
	if mode != "" {
		a.Set.NavMode = mode
	}
	a.navScroll = 0
	a.saveSettings()
}

func (a *App) openHome(pane string) {
	a.stash()
	a.backstage, a.bsPane, a.bsScroll = true, pane, 0
	a.bsFromDoc = true
	a.popup = nil
}

func (a *App) nextFootnote() {
	e := a.E
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	start := e.Sel.F
	found := false
	pass := 0
	for pass < 2 && !found {
		for _, en := range e.flat {
			p := en.P
			off := 0
			for _, r := range p.Runs {
				if r.T == RNote {
					after := pass == 1 || a.posAfter(p, off, start)
					if after {
						e.SetCaret(Pos{P: p, O: off + 1}, false)
						found = true
						break
					}
				}
				off += r.Len()
			}
			if found {
				break
			}
		}
		pass++
	}
	if !found {
		a.msg(a.tr("В документе нет сносок"))
	}
	a.after()
}

// posAfter — стоит ли позиция (p, off) после позиции курсора.
func (a *App) posAfter(p *Para, off int, cur Pos) bool {
	e := a.E
	ip, ic := e.fidx[p], e.fidx[cur.P]
	if ip != ic {
		return ip > ic
	}
	return off >= cur.O
}

// ---------------------------------------------------------------- стекло
func (a *App) glass(cv *Canvas, r Rect, rad float64) {
	th := a.th
	cv.Shadow(r, rad, a.d(18), RGBA(0, 0, 0, uint8(map[bool]int{true: 120, false: 50}[th.Dark])))
	cv.BlurRound(r, rad, int(math.Max(4, a.d(9))))
	cv.FillRound(r, rad, th.Popup.WithA(0.80))
	cv.StrokeRound(r, rad, math.Max(1, a.S), th.Border.WithA(0.9))
}

// ---------------------------------------------------------------- главный кадр
func (a *App) Paint(cv *Canvas) {
	t0 := time.Now()
	if a.BaseS == 0 {
		a.BaseS = a.S
	}
	a.S = a.BaseS * a.Set.UIScale
	a.pollUpdate()
	a.hits = a.hits[:0]
	cv.ResetClip()
	th := a.th
	cv.Clear(th.Bg)
	titleH := a.d(44)
	if a.backstage {
		a.paintHome(cv, Rect{0, titleH, a.W, a.H - titleH})
		a.paintTitle(cv, titleH)
		a.paintOverlays(cv)
		a.paintMs = float64(time.Since(t0).Microseconds()) / 1000
		return
	}
	a.refreshActions()
	toolH, statusH := a.d(42), a.d(26)
	top := titleH + toolH
	bodyH := a.H - top - statusH
	railW := a.d(48)
	navW := 0.0
	if a.Set.ShowNav && a.W > a.d(900) {
		navW = a.d(236)
		a.navRect = Rect{railW, top, navW, bodyH}
	} else {
		a.navRect = Rect{}
	}
	// инспектор: целиком, если рядом помещается лист с примечаниями, иначе узкая полоса значков
	// (щелчок по значку выдвигает инспектор поверх документа) — документ важнее панели
	inspW, full := 0.0, a.d(288)
	a.inspStrip = false
	a.inspRect, a.inspBody = Rect{}, Rect{}
	if a.pdf == nil && !a.Set.HideInspector {
		need := a.maxPageW() + a.cmtW()
		if a.W-railW-navW-full >= need {
			inspW = full
			a.inspRect = Rect{a.W - inspW, top, inspW, bodyH}
			a.inspPeek = false
		} else {
			inspW = a.d(44)
			a.inspStrip = true
		}
	} else {
		a.inspPeek = false
	}
	a.docRect = Rect{railW + navW, top, a.W - railW - navW - inspW, bodyH}
	if a.scrollCaret {
		a.ScrollToCaret()
		a.scrollCaret = false
	}
	a.clampScroll()

	cv.FillRect(a.docRect, th.Canvas)
	cv.SetClip(a.docRect)
	a.addHit("doc", a.docRect, 1, nil)
	if a.pdf != nil {
		a.paintPDF(cv)
	} else {
		a.paintDocument(cv)
	}
	a.paintScrollbar(cv)
	cv.ResetClip()
	if navW > 0 {
		a.paintNav(cv, a.navRect)
		cv.ResetClip()
	}
	if inspW > 0 && !a.inspStrip {
		a.paintInspector(cv, a.inspRect)
		cv.ResetClip()
	}
	if a.inspStrip {
		strip := Rect{a.W - inspW, top, inspW, bodyH}
		a.paintInspStrip(cv, strip)
		if a.inspPeek {
			a.inspRect = Rect{strip.X - full, top, full, bodyH}
			cv.Shadow(a.inspRect, 0, a.d(16), RGBA(0, 0, 0, uint8(map[bool]int{true: 140, false: 50}[th.Dark])))
			a.paintInspector(cv, a.inspRect)
			cv.ResetClip()
		}
	}
	a.paintRail(cv, Rect{0, top, railW, bodyH})
	a.paintTitle(cv, titleH)
	a.paintToolbar(cv, Rect{0, titleH, a.W, toolH})
	a.paintStatus(cv, Rect{0, a.H - statusH, a.W, statusH})
	a.paintOverlays(cv)
	a.paintMs = float64(time.Since(t0).Microseconds()) / 1000
}

func (a *App) paintOverlays(cv *Canvas) {
	cv.ResetClip()
	a.paintToast(cv)
	if a.dialog != nil {
		a.addHit("dialog-bg", Rect{0, 0, a.W, a.H}, 0, nil)
		a.paintDialog(cv)
	}
	// выпадающие списки рисуем поверх диалога, иначе они прячутся под ним
	cv.ResetClip()
	if a.popup != nil {
		a.addHit("popup", a.popup.R, 0, nil)
		a.popup.Draw(cv, a.popup)
	}
}

// ---------------------------------------------------------------- заголовок с вкладками документов
func (a *App) paintTitle(cv *Canvas, h float64) {
	th := a.th
	cv.FillRect(Rect{0, 0, a.W, h}, th.Bg)
	a.addHit("caption", Rect{0, 0, a.W, h}, 0, nil)
	LogoFlat(cv, Rect{a.d(12), a.d(11), a.d(22), a.d(22)})
	a.hits = append(a.hits, hit{id: "t:logo", r: Rect{a.d(10), a.d(8), a.d(26), a.d(28)}, act: func() { a.openHome("home") }, tip: "Главная Texora"})
	x := a.d(42)
	if a.backstage {
		a.textAt(cv, AppName, x, a.d(12), Font{Family: a.UIFont, Px: a.d(15), Bold: true}, th.Text)
		x += a.measure(AppName, Font{Family: a.UIFont, Px: a.d(15), Bold: true}) + a.d(10)
	}
	x += a.d(10)
	bw := a.d(46)
	rx := a.W - bw*3
	// поле палитры команд: «Команды … Ctrl K»
	pw := math.Min(a.d(230), (a.W-x)*0.28)
	pr := Rect{rx - a.d(48) - pw, a.d(8), pw, h - a.d(16)}
	if pw > a.d(110) {
		a.hits = append(a.hits, hit{id: "t:palette", r: pr, act: func() { a.openPalette() }, tip: a.tr("Все команды Texora")})
		cv.FillRound(pr, a.d(6), th.Field)
		cv.StrokeRound(pr, a.d(6), math.Max(1, a.S), mapCol(a.anim["t:palette"] > 0, th.Accent.WithA(0.6), th.Border))
		a.icon(cv, "find", Rect{pr.X + a.d(8), pr.Y + (pr.H-a.d(16))/2, a.d(16), a.d(16)}, th.TextDim)
		a.textAt(cv, "Команды", pr.X+a.d(30), pr.Y+(pr.H-a.d(16))/2, a.uiFont(12), th.TextDim)
		a.keycap(cv, "Ctrl K", pr.X+pr.W-a.d(5), pr.Y+(pr.H-a.d(20))/2, a.monoFont(10.5))
	} else {
		pr.X, pr.W = rx-a.d(84), 0
	}
	// вкладки документов
	a.stash()
	xmax := pr.X - a.d(8)
	n := len(a.tabs)
	tw := math.Max(a.d(96), math.Min(a.d(220), (xmax-x-a.d(36))/float64(n)))
	for i, t := range a.tabs {
		if x+tw > xmax-a.d(30) && i > 0 {
			break
		}
		i := i
		r := Rect{x, a.d(7), tw - a.d(4), h - a.d(11)}
		id := fmt.Sprintf("dtab:%d", i)
		active := i == a.cur && !a.backstage
		a.hits = append(a.hits, hit{id: id, r: r, act: func() { a.activate(i); a.backstage = false }, tip: t.FileName})
		if active {
			cv.FillRound(r, a.d(6), th.Surface)
			cv.StrokeRound(r, a.d(6), math.Max(1, a.S), th.Border)
			cv.FillRect(Rect{r.X + a.d(8), r.Y + r.H - a.d(2), r.W - a.d(16), a.d(2)}, th.Accent)
		} else {
			a.hoverBg(cv, id, r, a.d(6))
		}
		a.icon(cv, "new", Rect{r.X + a.d(8), r.Y + (r.H-a.d(16))/2, a.d(16), a.d(16)}, mapCol(active, th.Accent2, th.TextDim))
		name := strings.TrimSuffix(t.FileName, filepath.Ext(t.FileName))
		col := th.TextDim
		if active {
			col = th.Text
		}
		cv.SetClip(Rect{r.X, r.Y, r.W - a.d(30), r.H})
		a.tb.Draw(cv, name, r.X+a.d(30), r.Y+r.H/2+a.d(4.5), a.uiFont(12.5), col)
		cv.ResetClip()
		if t.E.Dirty {
			cv.FillCircle(r.X+r.W-a.d(14), r.Y+r.H/2, a.d(3.5), th.Accent2)
		}
		if active || a.hover == id || a.hover == id+":x" {
			xr := Rect{r.X + r.W - a.d(25), r.Y + (r.H-a.d(22))/2, a.d(22), a.d(22)}
			if t.E.Dirty {
				cv.FillCircle(xr.X+xr.W/2, xr.Y+xr.H/2, a.d(9), mapCol(active, th.Surface, th.Bg))
			}
			a.iconBtn(cv, id+":x", "x", xr, false, "Закрыть (Ctrl+W)", func() { a.closeTab(i) })
		}
		x += tw
	}
	a.iconBtn(cv, "t:newtab", "plus", Rect{x, a.d(9), a.d(28), a.d(28)}, false, "Новый документ (Ctrl+N)", func() { a.Command("new") })

	ic := "moon"
	if th.Dark {
		ic = "sun"
	}
	a.iconBtn(cv, "t:theme", ic, Rect{rx - a.d(42), a.d(7), a.d(34), a.d(30)}, false, "Светлая / тёмная тема", func() {
		if a.th.Dark {
			a.SetThemeMode("light")
		} else {
			a.SetThemeMode("dark")
		}
	})
	for i, b := range []struct{ id, ic string }{{"min", "min"}, {"max", "max"}, {"close", "close"}} {
		r := Rect{rx + bw*float64(i), 0, bw, h}
		if b.id == "max" && a.Maximized {
			b.ic = "restore"
		}
		id := "w:" + b.id
		var act func()
		switch b.id {
		case "min":
			act = func() { a.P.Minimize() }
		case "max":
			act = func() { a.P.ToggleMaximize() }
		case "close":
			act = func() { a.RequestClose() }
		}
		a.hits = append(a.hits, hit{id: id, r: r, act: act})
		hv := a.anim[id]
		col := th.Text
		if b.id == "close" {
			if hv > 0 {
				cv.FillRect(r, Hex(0xc42b1c).WithA(hv))
			}
			col = Mix(th.Text, Hex(0xffffff), hv)
		} else if hv > 0 {
			cv.FillRect(r, th.Hover.WithA(th.Hover.A()*hv))
		}
		a.icon(cv, b.ic, Rect{r.X + (r.W-a.d(14))/2, r.Y + (r.H-a.d(14))/2, a.d(14), a.d(14)}, col)
	}
}

func mapCol(b bool, x, y Color) Color {
	if b {
		return x
	}
	return y
}

// ---------------------------------------------------------------- панель навигации
func (a *App) paintNav(cv *Canvas, r Rect) {
	th := a.th
	cv.FillRect(r, th.Nav)
	cv.FillRect(Rect{r.X + r.W - math.Max(1, a.S), r.Y, math.Max(1, a.S), r.H}, th.Divider)
	a.addHit("nav", r, 0, nil)
	// заголовок панели: режим выбирается на полосе слева
	title := "Страницы"
	if a.Set.NavMode == "headings" {
		title = map[bool]string{true: "Оглавление", false: "Заголовки"}[a.pdf != nil]
	}
	f := a.uiFont(10.5)
	f.Bold = true
	a.textAt(cv, strings.ToUpper(a.tr(title)), r.X+a.d(16), r.Y+a.d(16), f, th.TextDim)
	n := len(a.E.L.Pages)
	if a.pdf != nil {
		n = len(a.pdf.Doc.Pages)
	}
	if a.Set.NavMode != "headings" {
		cnt := fmt.Sprint(n)
		mf := a.monoFont(11)
		a.textAt(cv, cnt, r.X+r.W-a.d(44)-a.measure(cnt, mf), r.Y+a.d(16), mf, th.TextDim)
	}
	a.iconBtn(cv, "nav:close", "x", Rect{r.X + r.W - a.d(36), r.Y + a.d(10), a.d(26), a.d(26)}, false, "Скрыть панель", func() { a.toggleNav("") })
	cv.FillRect(Rect{r.X, r.Y + a.d(44), r.W, math.Max(1, a.S)}, th.Divider)
	body := Rect{r.X + a.d(4), r.Y + a.d(50), r.W - a.d(8), r.H - a.d(56)}
	cv.SetClip(body)
	defer cv.ResetClip()
	switch {
	case a.pdf != nil && a.Set.NavMode == "headings":
		a.paintPDFNavOutline(cv, body)
	case a.pdf != nil:
		a.paintPDFNavPages(cv, body)
	case a.Set.NavMode == "headings":
		a.paintNavHeadings(cv, body)
	default:
		a.paintNavPages(cv, body)
	}
}

func (a *App) navMax(content, view float64) {
	a.navScroll = math.Max(0, math.Min(a.navScroll, content-view))
}

func (a *App) paintNavPages(cv *Canvas, body Rect) {
	th := a.th
	L := a.E.L
	tw := body.W - a.d(56)
	maxW := 0.0
	for _, pg := range L.Pages {
		maxW = math.Max(maxW, pg.G.Page.W)
	}
	k := tw / maxW
	ys := make([]float64, len(L.Pages)+1)
	for i, pg := range L.Pages {
		ys[i+1] = ys[i] + pg.G.Page.H*k + a.d(30)
	}
	a.navMax(ys[len(L.Pages)]+a.d(10), body.H)
	curPage := 0
	if c := a.E.Caret(a.E.Sel.F); c != nil {
		curPage = c.Page
	}
	pp := &PagePainter{cv: cv, tb: a.tb, doc: a.E.Doc, k: k, imgs: a.PageCache}
	for i, pg := range L.Pages {
		pw, thH := pg.G.Page.W*k, pg.G.Page.H*k
		step := thH + a.d(30)
		y := body.Y + a.d(8) + ys[i] - a.navScroll
		if y > body.Y+body.H || y+step < body.Y {
			continue
		}
		pr := Rect{body.X + a.d(28) + (tw-pw)/2, y, pw, thH}
		i := i
		id := fmt.Sprintf("navp:%d", i)
		a.hits = append(a.hits, hit{id: id, r: Rect{body.X, y - a.d(4), body.W, step}, act: func() { a.gotoPage(i) }})
		if i != curPage {
			a.hoverBg(cv, id, Rect{body.X + a.d(4), y - a.d(5), body.W - a.d(8), thH + a.d(10)}, a.d(5))
		}
		cv.Shadow(pr, a.d(2), a.d(5), th.PageShadow)
		cv.FillRect(pr, Hex(0xffffff))
		if i == curPage {
			cv.StrokeRound(pr.Inset(-a.d(1.5)), a.d(2), a.d(2), th.Accent)
		}
		pr = Rect{math.Round(pr.X), math.Round(pr.Y), math.Round(pr.W), math.Round(pr.H)}
		key := pageKey{pg: pg, k: k, gen: a.docGen.Load()}
		tb := a.thumbBmps[i]
		if tb == nil || tb.key != key || tb.cv.W != int(pr.W) || tb.cv.H != int(pr.H) {
			if tb == nil || tb.cv.W != int(pr.W) || tb.cv.H != int(pr.H) {
				if tb != nil && tb.cv.Release != nil {
					tb.cv.Release()
				}
				pc := &Canvas{W: max(1, int(pr.W)), H: max(1, int(pr.H))}
				pc.Pix = make([]uint32, pc.W*pc.H)
				if PrepareCanvas != nil {
					PrepareCanvas(pc)
				}
				tb = &pageBmp{cv: pc}
				if a.thumbBmps == nil {
					a.thumbBmps = map[int]*pageBmp{}
				}
				a.thumbBmps[i] = tb
			}
			pc := tb.cv
			pc.ResetClip()
			pc.Clear(Hex(0xffffff))
			pp.cv = pc
			pp.ox, pp.oy = 0, 0
			pp.DrawPage(L, pg, "body", true)
			pp.cv = cv
			pc.sync()
			tb.key = key
		}
		tb.frame = a.frame
		cv.SetClip(intersect(pr, body))
		blitCanvas(cv, tb.cv, int(pr.X), int(pr.Y))
		cv.SetClip(body)
		a.centerText(cv, fmt.Sprint(pg.Num), Rect{body.X + a.d(4), y, a.d(22), a.d(18)}, a.monoFont(10.5), mapCol(i == curPage, th.Accent2, th.TextDim))
	}
}

func intersect(a, b Rect) Rect {
	x0, y0 := math.Max(a.X, b.X), math.Max(a.Y, b.Y)
	x1, y1 := math.Min(a.X+a.W, b.X+b.W), math.Min(a.Y+a.H, b.Y+b.H)
	return Rect{x0, y0, math.Max(0, x1-x0), math.Max(0, y1-y0)}
}

func (a *App) gotoPage(i int) {
	a.scrollY = a.pageY(i) - a.d(22)
	if a.pdf != nil {
		a.clampScroll()
		a.invalidate()
		return
	}
	g := a.E.L.Pages[max(0, min(i, len(a.E.L.Pages)-1))].G
	if pos, ok := a.E.Hit(PagePoint{Page: i, X: g.Left + 1, Y: g.BodyTop + 1}); ok {
		if a.E.Story != "body" {
			a.E.SetStory("body", 0)
		}
		a.E.SetCaret(pos, false)
	}
	a.clampScroll()
	a.CaretOn = true
	a.invalidate()
}

type navHead struct {
	lvl  int
	text string
	p    *Para
}

func (a *App) headings() []navHead {
	var out []navHead
	for _, en := range a.E.flat {
		p := en.P
		if en.Cell != nil {
			continue
		}
		lvl := p.PF.Heading
		if lvl == 0 && p.PF.Style == "Title" {
			lvl = 1
		}
		if lvl == 0 {
			continue
		}
		t := strings.TrimSpace(strings.ReplaceAll(p.Text(), "￼", ""))
		if t != "" {
			out = append(out, navHead{lvl, t, p})
		}
	}
	return out
}

func (a *App) paintNavHeadings(cv *Canvas, body Rect) {
	th := a.th
	hs := a.headings()
	if len(hs) == 0 {
		for i, l := range a.wrapText(a.tr("Заголовков пока нет. Примените к абзацам стиль «Заголовок 1», «Заголовок 2»… — и они появятся здесь."), body.W-a.d(24), a.uiFont(12)) {
			a.tb.Draw(cv, l, body.X+a.d(12), body.Y+a.d(24)+float64(i)*a.d(18), a.uiFont(12), th.TextDim)
		}
		return
	}
	rowH := a.d(30)
	a.navMax(float64(len(hs))*rowH+a.d(10), body.H)
	// текущий заголовок — последний перед курсором
	curIdx := -1
	ci := a.E.fidx[a.E.Sel.F.P]
	for i, h := range hs {
		if a.E.fidx[h.p] <= ci {
			curIdx = i
		}
	}
	for i, h := range hs {
		y := body.Y + a.d(4) + float64(i)*rowH - a.navScroll
		if y > body.Y+body.H || y+rowH < body.Y {
			continue
		}
		h := h
		r := Rect{body.X + a.d(4), y, body.W - a.d(8), rowH - a.d(2)}
		id := fmt.Sprintf("navh:%d", i)
		a.hits = append(a.hits, hit{id: id, r: r, tip: h.text, act: func() {
			if a.E.Story != "body" {
				a.E.SetStory("body", 0)
			}
			a.E.SetCaret(Pos{P: h.p, O: 0}, false)
			a.after()
		}})
		if i == curIdx {
			cv.FillRound(r, a.d(6), th.Accent.WithA(0.18))
			cv.FillRound(Rect{r.X, r.Y + a.d(7), a.d(3), r.H - a.d(14)}, a.d(1.5), th.Accent)
		} else {
			a.hoverBg(cv, id, r, a.d(6))
		}
		f := a.uiFont(12.5)
		col := th.TextDim
		if h.lvl == 1 {
			f.Bold = true
			col = th.Text
		}
		ind := a.d(12) + float64(h.lvl-1)*a.d(14)
		cv.SetClip(intersect(Rect{r.X, r.Y, r.W - a.d(6), r.H}, body))
		a.tb.Draw(cv, h.text, r.X+ind, r.Y+r.H/2+a.d(4.5), f, col)
		cv.SetClip(body)
	}
}
