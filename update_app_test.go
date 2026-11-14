//go:build !windows

package main

import (
	"strings"
	"testing"
	"time"
)

func waitUpd(a *App) {
	for i := 0; i < 200; i++ {
		a.upd.mu.Lock()
		busy := a.upd.checking
		a.upd.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpdateDialogFlow(t *testing.T) {
	fakeGitHub(t, "v99.0.0", []byte("x"), "", false)
	a := NewApp(newTTFText(), &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.BaseS, a.W, a.H = 1, 1366, 860
	cv := newCanvas(1366, 860)
	a.CheckUpdates(true)
	waitUpd(a)
	a.Paint(cv)
	if a.dialog == nil || !strings.Contains(a.dialog.Text, "99.0.0") {
		t.Fatalf("нет диалога обновления: %+v", a.dialog)
	}
	// «Пропустить эту версию» — автоматическая проверка больше не предлагает её
	a.dialog.Buttons[0].Act()
	a.dialog = nil
	if a.Set.SkipVersion != "99.0.0" {
		t.Fatal("версия не запомнена как пропущенная")
	}
	a.CheckUpdates(false)
	waitUpd(a)
	a.Paint(cv)
	if a.dialog != nil {
		t.Error("пропущенная версия снова предложена автоматически")
	}
	// без интернета ручная проверка сообщает об ошибке, автоматическая молчит
	updateAPI = func() string { return "http://127.0.0.1:1/nope" }
	a.CheckUpdates(false)
	waitUpd(a)
	a.Paint(cv)
	if a.dialog != nil {
		t.Error("автоматическая проверка без сети показала окно")
	}
	a.CheckUpdates(true)
	waitUpd(a)
	a.Paint(cv)
	if a.dialog == nil {
		t.Error("ручная проверка без сети ничего не сказала")
	}
}
