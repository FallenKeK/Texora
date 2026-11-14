//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func newTestApp(t *testing.T, cfg string) *App {
	a := NewApp(newTTFText(), &fakePlatform{})
	a.cfgDir = cfg
	a.Set = defaultSettings()
	return a
}

func TestRecoverySessions(t *testing.T) {
	cfg := t.TempDir()
	root := filepath.Join(cfg, "recover")
	fake := func(name string, pid int, age time.Duration) {
		dir := filepath.Join(root, name)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "1.docx"), []byte("x"), 0o644)
		alive := filepath.Join(dir, "alive")
		os.WriteFile(alive, []byte(fmt.Sprint(pid)), 0o644)
		os.Chtimes(alive, time.Now().Add(-age), time.Now().Add(-age))
	}
	// живая чужая Texora
	sleep := exec.Command("sleep", "30")
	if err := sleep.Start(); err != nil {
		t.Skip("нет sleep")
	}
	defer sleep.Process.Kill()
	fake("alive", sleep.Process.Pid, 0)
	// упавшая: процесса уже нет
	gone := exec.Command("true")
	gone.Run()
	fake("crashed", gone.Process.Pid, 0)
	// зависшая давно: отметка старая (pid мог достаться другой программе)
	fake("stale", sleep.Process.Pid, 10*time.Minute)
	// копия версии до 1.4.2
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "0xc000123.docx"), []byte("x"), 0o644)

	a := newTestApp(t, cfg)
	a.heartbeat()
	files, dirs := a.deadSessionFiles()
	var got []string
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		got = append(got, rel)
	}
	sort.Strings(got)
	want := []string{"0xc000123.docx", "crashed/1.docx", "stale/1.docx"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("к восстановлению %v, ждали %v", got, want)
	}
	if len(dirs) != 2 {
		t.Errorf("папок мёртвых сессий %d, ждали 2", len(dirs))
	}

	// своя сессия: копия пишется в свою папку и не видна как чужая
	a.E.Dirty = true
	a.AutosaveAll()
	own, _ := filepath.Glob(filepath.Join(a.sessionDir(), "*.docx"))
	if len(own) != 1 {
		t.Fatalf("своих копий %d, ждали 1", len(own))
	}
	// в тесте обе «Texora» в одном процессе — притворимся, что первая живёт в процессе sleep
	os.WriteFile(filepath.Join(a.sessionDir(), "alive"), []byte(fmt.Sprint(sleep.Process.Pid)), 0o644)
	b := newTestApp(t, cfg)
	b.heartbeat()
	files, _ = b.deadSessionFiles()
	for _, f := range files {
		if filepath.Dir(f) == a.sessionDir() {
			t.Error("вторая Texora предлагает восстановить копии живой первой")
		}
	}
	// сохранение в файл убирает копию, выход — всю папку сессии
	if !a.writeTo(filepath.Join(cfg, "doc.docx")) {
		t.Fatal("не сохранилось")
	}
	if own, _ = filepath.Glob(filepath.Join(a.sessionDir(), "*.docx")); len(own) != 0 {
		t.Error("после сохранения копия осталась")
	}
	dir := a.sessionDir()
	a.endRecoverySession()
	if fileExists(dir) {
		t.Error("после выхода папка сессии осталась")
	}
}
