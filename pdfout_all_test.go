package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

// monoTB — простейший измеритель для вёрстки без системных шрифтов (тест должен идти и на Windows в CI).
type monoTB struct{}

func (monoTB) Measure(s string, f Font) float64                    { return float64(len([]rune(s))) * f.Px * 0.5 }
func (monoTB) Metrics(f Font) (float64, float64)                   { return f.Px * 0.8, f.Px * 0.2 }
func (monoTB) Draw(*Canvas, string, float64, float64, Font, Color) {}

func TestSystemFontLookup(t *testing.T) {
	fams := []string{"Times New Roman", "Arial"}
	if runtime.GOOS == "windows" {
		fams = append(fams, "Cambria Math", "Calibri")
	}
	for _, fam := range fams {
		path, idx, synB, _, ok := systemFont(fam, true, false)
		if !ok {
			t.Errorf("%s не найден", fam)
			continue
		}
		f := parseTTF(mustRead(t, path), idx)
		if f == nil || f.cmap('Ж') == 0 && fam != "Cambria Math" {
			t.Errorf("%s: %s #%d не разбирается или без кириллицы", fam, path, idx)
		}
		if runtime.GOOS == "windows" && (fam == "Times New Roman" || fam == "Arial") && synB {
			t.Errorf("%s: жирное начертание не найдено, хотя оно есть в Windows", fam)
		}
	}
}

func TestExportPDFVectorAnyOS(t *testing.T) {
	d := NewDoc()
	d.Body = nil
	add := func(text string, f F, heading int) {
		p := NewPara()
		p.PF.Heading = heading
		p.Runs = []Run{{T: RText, Text: []rune(text), F: f}}
		d.Body = append(d.Body, Block{P: p})
	}
	add("Глава первая", F{Font: "Times New Roman", Size: 16, B: true}, 1)
	add("Обычный текст и формула x² ≤ ∞.", F{Font: "Times New Roman", Size: 14}, 0)
	add("Курсив Arial", F{Font: "Arial", Size: 12, I: true}, 0)
	L := NewMeasurer(monoTB{}).Paginate(d, map[*Para]*ParaL{})
	out, err := ExportPDFVector(d, L, monoTB{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := LoadPDF(out)
	if err != nil {
		t.Fatalf("PDF не читается: %v", err)
	}
	_, gl := p.RenderPage(0, 0.5, false)
	txt, _ := PageText(gl)
	txt = strings.Join(strings.Fields(txt), " ")
	for _, want := range []string{"Глава первая", "Обычный текст", "≤", "Курсив Arial"} {
		if !strings.Contains(txt, want) {
			t.Errorf("в тексте PDF нет %q: %q", want, txt)
		}
	}
	if bytes.Count(out, []byte("/FontFile")) < 2 {
		t.Errorf("шрифты не вшиты")
	}
	if len(p.Outline) != 1 {
		t.Errorf("закладок %d, ждали 1", len(p.Outline))
	}
}

// Нет файла жирного или курсивного начертания — дорисовываем (обводка и наклон), как Windows на экране.
func TestPDFSyntheticStyles(t *testing.T) {
	path, idx, _, _, ok := systemFont("Times New Roman", false, false)
	if !ok {
		t.Skip("нет шрифта")
	}
	o := &pdfOut{w: &pdfWriter{}, anchors: map[*Para]pdfAnchor{},
		fonts: &pdfFonts{byFile: map[string]*pdfEmb{}, byFace: map[Font]*pdfFace{}, files: map[string][]byte{}}}
	f := Font{Family: "Times New Roman", Bold: true, Italic: true, Px: 12}
	o.fonts.byFace[Font{Family: f.Family, Bold: true, Italic: true}] = &pdfFace{font: o.fonts.load(path, idx), synB: true, synI: true}
	p := &pdfPage{out: o, h: 800, fonts: map[string]bool{}, imgs: map[string]int{}}
	p.text("Жирный", 10, 100, f, Hex(0))
	cs := p.cs.String()
	if !strings.Contains(cs, " 2 Tr ") || !strings.Contains(cs, " 1 0 0.21 1 ") {
		t.Errorf("нет дорисовки жирного или курсива: %s", cs)
	}
}
