package main

// Автообновление: раз в сутки (и по команде) Texora спрашивает GitHub Releases о последней версии,
// предлагает обновиться, скачивает Texora.exe, сверяет размер и SHA-256, подменяет свой exe и перезапускается.
// Без интернета ничего не происходит: проверка молча не удаётся.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UpdateRepo — откуда брать релизы (владелец/репозиторий на GitHub). Репозиторий должен быть публичным:
// программа ходит в API без токена.
var UpdateRepo = "FallenKeK/Texora"

// updateAPI — адрес «последнего релиза»; переменная, чтобы тесты подставили свой сервер.
var updateAPI = func() string { return "https://api.github.com/repos/" + UpdateRepo + "/releases/latest" }

type updateInfo struct {
	Version string // без «v»
	Notes   string
	URL     string // прямая ссылка на Texora.exe
	Size    int64
	SHA256  string // hex, если GitHub его сообщил
}

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Body   string `json:"body"`
	Draft  bool   `json:"draft"`
	Pre    bool   `json:"prerelease"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"` // "sha256:…"
	} `json:"assets"`
}

// versionLess — a < b для версий вида 1.5.1 (лишние части и «v» в начале допускаются).
func versionLess(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := 0, 0
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

var updClient = &http.Client{Timeout: 20 * time.Second}

// fetchLatest — сведения о последнем релизе; nil без ошибки — новее нет.
func fetchLatest(current string) (*updateInfo, error) {
	req, _ := http.NewRequest("GET", updateAPI(), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Texora/"+current)
	resp, err := updClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("сервер обновлений ответил %d", resp.StatusCode)
	}
	var r ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if r.Draft || r.Pre || !versionLess(current, r.Tag) {
		return nil, nil
	}
	for _, as := range r.Assets {
		if strings.EqualFold(as.Name, "Texora.exe") {
			return &updateInfo{Version: strings.TrimPrefix(r.Tag, "v"), Notes: r.Body, URL: as.URL, Size: as.Size,
				SHA256: strings.TrimPrefix(as.Digest, "sha256:")}, nil
		}
	}
	return nil, nil
}

// downloadUpdate качает exe рядом с текущим (Texora.exe.new) и проверяет размер и SHA-256.
func downloadUpdate(u *updateInfo, dir string, progress func(done, total int64)) (string, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(u.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("загрузка: ответ %d", resp.StatusCode)
	}
	tmp := filepath.Join(dir, "Texora.exe.new")
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var n int64
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			f.Write(buf[:k])
			h.Write(buf[:k])
			n += int64(k)
			if progress != nil {
				progress(n, u.Size)
			}
			if n > 512<<20 {
				rerr = errors.New("файл обновления подозрительно большой")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if u.Size > 0 && n != u.Size {
		os.Remove(tmp)
		return "", fmt.Errorf("скачано %d байт вместо %d", n, u.Size)
	}
	if u.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), u.SHA256) {
		os.Remove(tmp)
		return "", errors.New("контрольная сумма не совпала — файл повреждён")
	}
	return tmp, nil
}

// swapExe ставит новый exe на место текущего. Запущенный exe Windows удалить не даёт, но переименовать даёт:
// текущий уходит в Texora.exe.old (его удалит следующий запуск), новый встаёт на его имя.
func swapExe(self, fresh string) error {
	old := self + ".old"
	os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		return err
	}
	if err := os.Rename(fresh, self); err != nil {
		os.Rename(old, self) // откат
		return err
	}
	return nil
}

// cleanupOldExe — остаток прошлого обновления.
func cleanupOldExe() {
	if self, err := os.Executable(); err == nil {
		os.Remove(self + ".old")
		os.Remove(filepath.Join(filepath.Dir(self), "Texora.exe.new"))
	}
}

// ---------------------------------------------------------------- в приложении

type updState struct {
	mu       sync.Mutex
	found    *updateInfo
	err      error
	manual   bool // проверка по команде: сообщить и «нет обновлений»
	checking bool
	progress string
	ready    string // путь к скачанному exe
}

// CheckUpdates — проверка в фоне. manual: по команде пользователя (покажет результат в любом случае).
func (a *App) CheckUpdates(manual bool) {
	u := &a.upd
	u.mu.Lock()
	if u.checking {
		u.mu.Unlock()
		return
	}
	u.checking, u.manual = true, manual
	u.mu.Unlock()
	if manual {
		a.msg(a.tr("Проверяю обновления…"))
	}
	go func() {
		info, err := fetchLatest(AppVersion)
		u.mu.Lock()
		u.found, u.err, u.checking = info, err, false
		u.mu.Unlock()
		if a.P != nil {
			a.P.Invalidate()
		}
	}()
	if !manual {
		a.Set.LastUpdateCheck = time.Now().Unix()
		a.saveSettings()
	}
}

// autoCheckUpdates — раз в сутки при запуске, если не выключено в настройках.
func (a *App) autoCheckUpdates() {
	if a.Set.NoAutoUpdate || a.cfgDir == "" {
		return
	}
	if time.Since(time.Unix(a.Set.LastUpdateCheck, 0)) < 24*time.Hour {
		return
	}
	a.CheckUpdates(false)
}

// pollUpdate вызывается в каждом кадре (UI-поток): забирает результат фоновой проверки или загрузки.
func (a *App) pollUpdate() {
	u := &a.upd
	u.mu.Lock()
	found, err, manual, prog, ready := u.found, u.err, u.manual, u.progress, u.ready
	u.found, u.err, u.ready = nil, nil, ""
	if found != nil || err != nil {
		u.manual = false
	}
	u.mu.Unlock()
	if prog != "" {
		a.status, a.statusT = prog, time.Now()
	}
	switch {
	case ready != "":
		a.installUpdate(ready)
	case found != nil:
		if !manual && found.Version == a.Set.SkipVersion {
			return
		}
		notes := strings.TrimSpace(found.Notes)
		if r := []rune(notes); len(r) > 600 {
			notes = string(r[:600]) + "…"
		}
		a.confirm(a.tr("Обновление Texora"), a.trf("Доступна версия %s (у вас %s).", found.Version, AppVersion)+"\n\n"+notes,
			DlgBtn{Label: "Пропустить эту версию", Act: func() bool { a.Set.SkipVersion = found.Version; a.saveSettings(); return true }},
			DlgBtn{Label: "Позже"},
			DlgBtn{Label: "Обновить", Primary: true, Act: func() bool { a.startDownload(found); return true }})
	case manual && err != nil:
		a.errorBox(a.tr("Не удалось проверить обновления:") + "\n" + err.Error())
	case manual && err == nil && found == nil && !u.checking:
		a.msg(a.trf("Установлена последняя версия: %s", AppVersion))
	}
}

func (a *App) startDownload(info *updateInfo) {
	self, err := os.Executable()
	if err != nil {
		a.errorBox(err.Error())
		return
	}
	u := &a.upd
	a.msg(a.tr("Загружаю обновление…"))
	go func() {
		last := time.Time{}
		path, err := downloadUpdate(info, filepath.Dir(self), func(done, total int64) {
			if time.Since(last) < 300*time.Millisecond || total <= 0 {
				return
			}
			last = time.Now()
			u.mu.Lock()
			u.progress = fmt.Sprintf("%s %d%%", a.tr("Загружаю обновление…"), done*100/total)
			u.mu.Unlock()
			if a.P != nil {
				a.P.Invalidate()
			}
		})
		u.mu.Lock()
		u.progress = ""
		if err != nil {
			u.err, u.manual = err, true
		} else {
			u.ready = path
		}
		u.mu.Unlock()
		if a.P != nil {
			a.P.Invalidate()
		}
	}()
}

// installUpdate — новый exe скачан и проверен: спрашиваем про несохранённое, подменяем и перезапускаемся.
func (a *App) installUpdate(fresh string) {
	self, err := os.Executable()
	if err != nil {
		a.errorBox(err.Error())
		return
	}
	a.confirm(a.tr("Обновление Texora"), a.tr("Обновление загружено. Перезапустить Texora сейчас? Несохранённые документы будут предложено сохранить."),
		DlgBtn{Label: "Позже", Act: func() bool {
			// поставим при следующем запуске сами: файл уже лежит рядом
			a.Set.PendingUpdate = fresh
			a.saveSettings()
			return true
		}},
		DlgBtn{Label: "Перезапустить", Primary: true, Act: func() bool {
			a.afterClose = func() {
				if err := swapExe(self, fresh); err != nil {
					return
				}
				exec.Command(self).Start()
			}
			a.RequestClose()
			return true
		}})
}

// applyPendingUpdate — при запуске: обновление скачали раньше, но перезапуск отложили.
func (a *App) applyPendingUpdate() bool {
	p := a.Set.PendingUpdate
	if p == "" {
		return false
	}
	a.Set.PendingUpdate = ""
	a.saveSettings()
	self, err := os.Executable()
	if err != nil || filepath.Dir(p) != filepath.Dir(self) {
		return false
	}
	if _, err := os.Stat(p); err != nil {
		return false
	}
	if swapExe(self, p) != nil {
		return false
	}
	return exec.Command(self).Start() == nil
}
