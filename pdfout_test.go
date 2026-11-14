//go:build !windows

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// Векторный PDF: текст ищется (а не картинка), шрифты вшиты и читаются, есть закладки и ссылки.
func TestExportPDFVector(t *testing.T) {
	data, err := os.ReadFile("testdata/feat.docx")
	if err != nil {
		t.Skip("нет testdata/feat.docx")
	}
	d, err := OpenBytes("feat.docx", data)
	if err != nil {
		t.Fatal(err)
	}
	// внешняя ссылка на первом же тексте основного абзаца
	linked := false
	for _, en := range Flatten(&d.Body, nil, nil) {
		for i := range en.P.Runs {
			if r := &en.P.Runs[i]; !linked && r.T == RText && strings.Contains(string(r.Text), "Автоматическая") {
				r.F.Link, linked = "https://example.com/a(b)", true
			}
		}
	}
	if !linked {
		t.Fatal("не нашёл абзац для ссылки")
	}
	tb := newTTFText()
	L := NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{})
	out, err := ExportPDFVector(d, L, tb, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadPDF(out)
	if err != nil {
		t.Fatalf("свой же PDF не читается: %v", err)
	}
	if len(p.Pages) != len(L.Pages) {
		t.Errorf("страниц %d, в вёрстке %d", len(p.Pages), len(L.Pages))
	}
	var all strings.Builder
	for i := range p.Pages {
		_, gl := p.RenderPage(i, 0.5, false)
		txt, _ := PageText(gl)
		all.WriteString(txt + "\n")
	}
	text := strings.Join(strings.Fields(all.String()), " ")
	for _, want := range []string{"ВВЕДЕНИЕ", "Автоматическая расстановка переносов", "электроэнергетический", "ЗАКЛЮЧЕНИЕ"} {
		if !strings.Contains(text, want) {
			t.Errorf("в тексте PDF нет %q", want)
		}
	}
	if len(p.Outline) == 0 {
		t.Error("нет закладок по заголовкам")
	}
	for _, want := range []string{"/FontFile2", "/ToUnicode", "/Dest [", `/URI (https://example.com/a\(b\))`} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("в PDF нет %s", want)
		}
	}
	if bytes.Contains(out, []byte("/DCTDecode")) && len(out) > 2_000_000 {
		t.Errorf("подозрительно большой PDF: %d байт — страницы картинками?", len(out))
	}
}

// Урезанный шрифт — правильный TrueType: глифы те же, лишние пустые.
func TestSubsetTTF(t *testing.T) {
	path, idx, _, _, ok := systemFont("Times New Roman", false, false)
	if !ok {
		t.Skip("нет шрифта")
	}
	f := parseTTF(mustRead(t, path), idx)
	used := map[int][]rune{}
	for _, r := range "Привет, ÅÉ!" { // Å/É могут быть составными глифами
		used[f.cmap(r)] = []rune{r}
	}
	sub := subsetTTF(f, used)
	g := parseTTF(sub, 0)
	if g == nil {
		t.Fatal("урезанный шрифт не разбирается")
	}
	if len(sub) >= len(f.data)/3 {
		t.Errorf("урезанный шрифт %d байт из %d — почти не урезан", len(sub), len(f.data))
	}
	for gid := range used {
		if !bytes.Equal(trimZero(g.glyphData(gid)), trimZero(f.glyphData(gid))) {
			t.Errorf("глиф %d отличается", gid)
		}
		if g.advance(gid) != f.advance(gid) {
			t.Errorf("ширина глифа %d отличается", gid)
		}
	}
	if g.glyphData(f.cmap('Z')) != nil && !containsGid(used, f.cmap('Z')) {
		t.Error("ненужный глиф Z остался в шрифте")
	}
}

func trimZero(b []byte) []byte { return bytes.TrimRight(b, "\x00") }

func containsGid(m map[int][]rune, g int) bool { _, ok := m[g]; return ok }
