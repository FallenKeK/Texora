//go:build !windows

package main

import (
	"math"
	"strings"
	"testing"
)

// Регрессии по багам, найденным сверкой на реальных курсовых (ноябрь 2026).

func para(text string, pf PF, f F) *Para {
	p := NewPara()
	p.PF = pf
	if text != "" {
		p.Runs = []Run{{T: RText, Text: []rune(text), F: f}}
	}
	return p
}

func docOf(ps ...*Para) *Doc {
	d := NewDoc()
	d.Body = nil
	for _, p := range ps {
		d.Body = append(d.Body, Block{P: p})
	}
	return d
}

var tnr14 = F{Font: "Times New Roman", Size: 14}

// Times New Roman: высота строки 1,15 кегля (как в Word), а не 1,107.
func TestLineHeightIncludesLeading(t *testing.T) {
	m := NewMeasurer(newTTFText())
	d := docOf(para("Строка", PF{Line: O(1)}, tnr14))
	L := m.LayoutPara(d, d.Body[0].P, 400, &LayoutCtx{}, "", nil)
	if h := L.Lines[0].H; math.Abs(h-14*1.15) > 0.15 {
		t.Errorf("высота строки %.2f пт, ждали %.2f", h, 14*1.15)
	}
	d = docOf(para("Строка", PF{Line: O(1.5)}, tnr14))
	L = m.LayoutPara(d, d.Body[0].P, 400, &LayoutCtx{}, "", nil)
	ln := L.Lines[0]
	if math.Abs(ln.H-14*1.15*1.5) > 0.2 || math.Abs(ln.Trail-14*1.15*0.5) > 0.2 {
		t.Errorf("полуторный: H %.2f Trail %.2f", ln.H, ln.Trail)
	}
	if ln.Base > 14 { // буквы у верха строки, добавка снизу
		t.Errorf("базовая линия %.2f — добавка интервала оказалась над строкой", ln.Base)
	}
}

// Оглавление: правый отступ + табуляция на самом поле — номер остаётся на строке.
func TestTabBeyondRightIndent(t *testing.T) {
	m := NewMeasurer(newTTFText())
	W := 480.0
	pf := PF{IndR: 30, Tabs: []TabStop{{Pos: W, Align: "right", Leader: "dot"}}}
	d := docOf(para("Введение\t4", pf, tnr14))
	L := m.LayoutPara(d, d.Body[0].P, W, &LayoutCtx{}, "", nil)
	if len(L.Lines) != 1 {
		t.Fatalf("строк %d, ждали 1 (номер страницы упал на новую строку)", len(L.Lines))
	}
	last := L.Lines[0].Items[len(L.Lines[0].Items)-1]
	if math.Abs(last.X+last.WT-W) > 1 {
		t.Errorf("номер кончается на %.1f, ждали %.1f", last.X+last.WT, W)
	}
}

// Список «– текст» с красной строки: маркер не наезжает на текст.
func TestListMarkerWithFirstLineIndent(t *testing.T) {
	m := NewMeasurer(newTTFText())
	pf := PF{IndL: O(0), IndF: O(34), List: &List{ID: "l", Kind: "ul", Text: "–"}, Tabs: []TabStop{{Pos: 51.5, Align: "left"}}}
	d := docOf(para("анализ условий решения специальных задач самолётами дальней авиации по опыту локальных войн", pf, tnr14))
	L := m.LayoutPara(d, d.Body[0].P, 480, &LayoutCtx{}, "–", nil)
	ln := L.Lines[0]
	if ln.Marker == nil || math.Abs(ln.Marker.X-34) > 0.5 {
		t.Fatalf("маркер: %+v", ln.Marker)
	}
	if x := ln.Items[0].X; math.Abs(x-51.5) > 0.5 {
		t.Errorf("текст первой строки с %.1f, ждали 51.5 (позиция табуляции после маркера)", x)
	}
	if x := L.Lines[1].Items[0].X; x > 0.5 {
		t.Errorf("вторая строка с %.1f, ждали от левого края", x)
	}
}

// Запрет висячих строк: абзац не оставляет одну строку внизу или вверху листа.
func TestWidowOrphanControl(t *testing.T) {
	tb := newTTFText()
	long := strings.Repeat("слово ", 60) // несколько строк
	for _, noWidow := range []bool{false, true} {
		for filler := 20; filler < 34; filler++ {
			var ps []*Para
			for i := 0; i < filler; i++ {
				ps = append(ps, para("строка", PF{Line: O(1.5)}, tnr14))
			}
			ps = append(ps, para(long, PF{Line: O(1.5), NoWidow: noWidow}, tnr14))
			d := docOf(ps...)
			L := NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{})
			target := d.Body[len(d.Body)-1].P
			for pi, pg := range L.Pages {
				n := 0
				for _, pu := range pg.Units {
					if pu.U.P == target {
						n++
					}
				}
				total := 0
				for _, q := range L.Pages {
					for _, pu := range q.Units {
						if pu.U.P == target {
							total++
						}
					}
				}
				if !noWidow && n == 1 && total > 1 {
					t.Errorf("filler %d: на листе %d одна строка абзаца из %d", filler, pi+1, total)
				}
			}
		}
	}
}

func cellOf(text string, keep bool) *Cell {
	return &Cell{Blocks: []Block{{P: para(text, PF{KeepNext: keep}, F{Font: "Times New Roman", Size: 12})}}, Span: 1}
}

// Шапка таблицы повторяется на продолжении; «не отрывать от следующего» держит строки вместе.
func TestTableHeaderRepeatAndKeep(t *testing.T) {
	tb := newTTFText()
	mk := func(rows int, keep bool) *Doc {
		d := NewDoc()
		d.Body = nil
		for i := 0; i < 20; i++ {
			d.Body = append(d.Body, Block{P: para("строка", PF{Line: O(1.5)}, tnr14)})
		}
		tbl := &Table{Border: true, Cols: []float64{200, 200}}
		tbl.Rows = append(tbl.Rows, &Row{Header: true, Cells: []*Cell{cellOf("Шапка", keep), cellOf("Значение", keep)}})
		for i := 0; i < rows; i++ {
			tbl.Rows = append(tbl.Rows, &Row{Cells: []*Cell{cellOf("ячейка", keep), cellOf("1", keep)}})
		}
		d.Body = append(d.Body, Block{T: tbl}, Block{P: para("после", PF{}, tnr14)})
		return d
	}
	// длинная таблица рвётся — на втором листе сначала шапка
	d := mk(60, false)
	L := NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{})
	if len(L.Pages) < 2 {
		t.Fatal("таблица не разорвалась")
	}
	first := L.Pages[1].Units[0].U
	if first.K != uRow || !first.Rep || !first.Hdr {
		t.Errorf("второй лист начинается не с повтора шапки: %+v", first)
	}
	// короткая таблица с «не отрывать» целиком уходит на следующий лист
	d = mk(8, true)
	L = NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{})
	pages := map[int]bool{}
	for pi, pg := range L.Pages {
		for _, pu := range pg.Units {
			if pu.U.K == uRow && !pu.U.Rep {
				pages[pi] = true
			}
		}
	}
	if len(pages) != 1 {
		t.Errorf("таблица с «не отрывать от следующего» оказалась на %d листах", len(pages))
	}
}

// <w:tblHeader w:val="off"/> — не шапка; свои поля ячейки читаются и пишутся; запрет висячих строк тоже.
func TestDocxTableFlagsRoundTrip(t *testing.T) {
	d := NewDoc()
	d.Body = nil
	c := cellOf("a", false)
	c.Pad = &[4]float64{2, 4, 2, 4}
	tbl := &Table{Border: true, Cols: []float64{200}, Rows: []*Row{{Header: true, Cells: []*Cell{cellOf("h", false)}}, {Cells: []*Cell{c}}}}
	d.Body = append(d.Body, Block{T: tbl}, Block{P: para("x", PF{NoWidow: true}, tnr14)})
	for _, ext := range []string{"docx", "odt"} {
		out, err := SaveBytes(d, ext)
		if err != nil {
			t.Fatal(err)
		}
		if ext == "docx" {
			// как пишет docx-js: признак есть у каждой строки, у обычных — off
			out = replaceInZip(t, out, "word/document.xml", "<w:tr>", `<w:tr><w:trPr><w:tblHeader w:val="off"/></w:trPr>`, 1)
		}
		d2, err := OpenBytes("x."+ext, out)
		if err != nil {
			t.Fatal(err)
		}
		t2 := d2.Body[0].T
		if !t2.Rows[0].Header || t2.Rows[1].Header {
			t.Errorf("%s: шапка %v %v, ждали true false", ext, t2.Rows[0].Header, t2.Rows[1].Header)
		}
		if p := t2.Rows[1].Cells[0].Pad; p == nil || math.Abs(p[0]-2) > 0.05 || math.Abs(p[1]-4) > 0.05 {
			t.Errorf("%s: поля ячейки %v, ждали 2/4", ext, p)
		}
		if ext == "docx" && !d2.Body[1].P.PF.NoWidow {
			t.Errorf("запрет висячих строк (выключен) потерялся")
		}
	}
}

// Свой знак маркера («–») переживает ODT.
func TestODTCustomBullet(t *testing.T) {
	d := docOf(para("пункт", PF{List: &List{ID: "l", Kind: "ul", Text: "–"}}, tnr14))
	out, err := SaveBytes(d, "odt")
	if err != nil {
		t.Fatal(err)
	}
	d2, err := OpenBytes("x.odt", out)
	if err != nil {
		t.Fatal(err)
	}
	if l := d2.Body[0].P.PF.List; l == nil || l.Text != "–" {
		t.Errorf("маркер после ODT: %+v", l)
	}
}

// Колонтитул: заглушка не остаётся после отмены, а намеренно пустой колонтитул не стирается заходом в него.
func TestHeaderFooterPlaceholder(t *testing.T) {
	a := NewApp(newTTFText(), &fakePlatform{})
	a.cfgDir = ""
	e := a.E
	e.SetStory("footer", 0)
	e.InsertText("x")
	e.SetStory("body", 0)
	for len(e.undo) > 0 {
		e.Undo()
	}
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	if len(e.Doc.Footer) != 0 {
		t.Errorf("после отмены остался колонтитул из %d абзацев", len(e.Doc.Footer))
	}
	e.Doc.FooterFirst = []Block{{P: NewPara()}} // намеренно пустой: титульный лист без номера
	e.SetStory("footer.first", 0)
	e.SetStory("body", 0)
	if len(e.Doc.FooterFirst) != 1 {
		t.Error("заход в пустой колонтитул первой страницы стёр его")
	}
}

// PDF с размером листа за пределами разумного не роняет отрисовку.
func TestPDFInsanePageSize(t *testing.T) {
	tb := newTTFText()
	d := docOf(para("текст", PF{}, tnr14))
	out, err := ExportPDFVector(d, NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{}), tb, nil)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(out), "/MediaBox [0 0 ")
	bad := []byte(strings.Replace(string(out), string(out[i:i+24]), "/MediaBox [0 0 9e18 9e18", 1))
	p, err := LoadPDF(bad)
	if err != nil {
		t.Skip("файл не открылся — тоже годится:", err)
	}
	if img, _ := p.RenderPage(0, 2, false); img == nil || img.Bounds().Dx() > 20000 {
		t.Errorf("лист нарисован неразумного размера")
	}
}

// replaceInZip — правит один файл внутри zip (для проверки чтения чужой разметки).
func replaceInZip(t *testing.T, data []byte, name, old, new string, skip int) []byte {
	t.Helper()
	files, err := unzipAll(data)
	if err != nil {
		t.Fatal(err)
	}
	s := string(files[name])
	// заменяем все вхождения, кроме первых skip
	parts := strings.Split(s, old)
	if len(parts) <= skip+1 {
		t.Fatalf("в %s нет %q после %d вхождений", name, old, skip)
	}
	s = strings.Join(parts[:skip+1], old) + new + strings.Join(parts[skip+1:], new)
	files[name] = []byte(s)
	var fs []wfile
	for _, k := range sortedKeys(files) {
		fs = append(fs, wfile{name: k, data: files[k]})
	}
	out, err := zipFiles(fs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
