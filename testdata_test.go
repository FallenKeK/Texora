package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Файлы из testdata/ (присланные документы и скан) должны открываться и сохраняться без ошибок.
// Тест без шрифтов и окна, поэтому идёт и на Windows.
func TestTestdataOpens(t *testing.T) {
	files, _ := filepath.Glob("testdata/*")
	if len(files) == 0 {
		t.Skip("нет testdata")
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(path)
		if isPptx(name) {
			if _, err := PptxToPDF(data); err != nil { // подробные проверки — в pptx_test.go
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if filepath.Ext(name) == ".pdf" {
			d, err := LoadPDF(data)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if len(d.Pages) == 0 {
				t.Errorf("%s: нет страниц", name)
				continue
			}
			if img, _ := d.RenderPage(0, 0.5, false); img == nil || img.Bounds().Empty() {
				t.Errorf("%s: первая страница не нарисовалась", name)
			}
			continue
		}
		d, err := OpenBytes(name, data)
		if err != nil {
			t.Errorf("%s: не открылся: %v", name, err)
			continue
		}
		for _, ext := range []string{"docx", "odt"} {
			out, err := SaveBytes(d, ext)
			if err != nil {
				t.Errorf("%s → %s: %v", name, ext, err)
				continue
			}
			if _, err := OpenBytes("x."+ext, out); err != nil {
				t.Errorf("%s → %s не читается обратно: %v", name, ext, err)
			}
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
