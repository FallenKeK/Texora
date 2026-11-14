package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		less bool
	}{{"1.5.0", "1.5.1", true}, {"1.5.1", "1.5.1", false}, {"1.5.1", "v1.10.0", true}, {"1.10.0", "1.9.9", false},
		{"1.5", "1.5.0", false}, {"1.5.0", "1.5.0.1", true}, {"2.0.0-beta", "2.0.0", false}, {"", "0.0.1", true}} {
		if got := versionLess(c.a, c.b); got != c.less {
			t.Errorf("versionLess(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// fakeGitHub — сервер «последнего релиза» и самого файла.
func fakeGitHub(t *testing.T, tag string, exe []byte, digest string, draft bool) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"body":"Что нового","draft":%v,"assets":[{"name":"notes.txt","browser_download_url":"x"},
				{"name":"Texora.exe","browser_download_url":"%s/Texora.exe","size":%d,"digest":%q}]}`, tag, draft, srv.URL, len(exe), digest)
		case "/Texora.exe":
			w.Write(exe)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := updateAPI
	updateAPI = func() string { return srv.URL + "/latest" }
	t.Cleanup(func() { updateAPI = old })
	return srv
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestFetchAndDownloadUpdate(t *testing.T) {
	exe := []byte("MZ новая версия")
	fakeGitHub(t, "v9.9.9", exe, "sha256:"+sha(exe), false)
	info, err := fetchLatest("1.5.1")
	if err != nil || info == nil {
		t.Fatalf("обновление не найдено: %v %v", info, err)
	}
	if info.Version != "9.9.9" || info.SHA256 != sha(exe) || info.Size != int64(len(exe)) {
		t.Errorf("сведения: %+v", info)
	}
	dir := t.TempDir()
	p, err := downloadUpdate(info, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(exe) {
		t.Error("скачано не то")
	}
	// испорченная контрольная сумма — файл не остаётся
	info.SHA256 = sha([]byte("другое"))
	if _, err := downloadUpdate(info, dir, nil); err == nil {
		t.Error("не заметил подмену файла")
	}
	if _, err := os.Stat(filepath.Join(dir, "Texora.exe.new")); err == nil {
		t.Error("испорченный файл остался на диске")
	}
	// та же или старая версия — обновления нет
	if info, _ := fetchLatest("9.9.9"); info != nil {
		t.Error("предложил ту же версию")
	}
}

func TestDraftIgnored(t *testing.T) {
	fakeGitHub(t, "v9.9.9", []byte("x"), "", true)
	if info, _ := fetchLatest("1.0.0"); info != nil {
		t.Error("черновик релиза предложен как обновление")
	}
}

func TestSwapExe(t *testing.T) {
	dir := t.TempDir()
	self, fresh := filepath.Join(dir, "Texora.exe"), filepath.Join(dir, "Texora.exe.new")
	os.WriteFile(self, []byte("old"), 0o755)
	os.WriteFile(fresh, []byte("new"), 0o755)
	if err := swapExe(self, fresh); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(self); string(b) != "new" {
		t.Errorf("на месте exe: %q", b)
	}
	if b, _ := os.ReadFile(self + ".old"); string(b) != "old" {
		t.Errorf("старый exe не сохранён: %q", b)
	}
	// новый файл пропал — откат, программа остаётся на месте
	os.WriteFile(self, []byte("cur"), 0o755)
	if err := swapExe(self, filepath.Join(dir, "нет")); err == nil {
		t.Error("подмена без файла прошла")
	}
	if b, _ := os.ReadFile(self); string(b) != "cur" {
		t.Errorf("после неудачи exe испорчен: %q", b)
	}
}
