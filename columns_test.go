//go:build !windows

package main

import (
	"math"
	"strings"
	"testing"
)

func colDoc(n int, sep bool, paras int) *Doc {
	d := NewDoc()
	d.Body = nil
	for i := 0; i < paras; i++ {
		d.Body = append(d.Body, Block{P: para(strings.Repeat("слово ", 30)+"конец", PF{Line: O(1.15)}, tnr14)})
	}
	d.Cols = Columns{N: n, Sep: sep}
	return d
}

func TestColumnsLayout(t *testing.T) {
	d := colDoc(2, true, 30)
	L := NewMeasurer(newTTFText()).Paginate(d, map[*Para]*ParaL{})
	pg := L.Pages[0]
	W := pg.G.W
	cw := d.Cols.Width(W)
	if math.Abs(cw-(W-1.25*CM)/2) > 0.01 {
		t.Fatalf("ширина колонки %.2f", cw)
	}
	xs := map[float64]int{}
	for _, pu := range pg.Units {
		xs[pu.X]++
		if math.Abs(pu.W-cw) > 0.01 {
			t.Errorf("блок шириной %.2f, ждали %.2f", pu.W, cw)
		}
		if pu.U.K == uLine && pu.U.Line.EX > cw+0.5 {
			t.Errorf("строка вылезает за колонку: %.1f > %.1f", pu.U.Line.EX, cw)
		}
	}
	if len(xs) != 2 || xs[0] == 0 || xs[cw+1.25*CM] == 0 {
		t.Errorf("колонки на листе: %v", xs)
	}
	// текст идёт по порядку: первая колонка, потом вторая
	lastX := 0.0
	for _, pu := range pg.Units {
		if pu.X < lastX {
			t.Fatal("после второй колонки снова первая")
		}
		lastX = pu.X
	}
	if len(pg.Bands) != 1 || !pg.Bands[0].Cols.Sep || pg.Bands[0].Bottom <= pg.Bands[0].Top {
		t.Errorf("полоса колонок: %+v", pg.Bands)
	}
	// строки вдвое короче — их примерно вдвое больше, а листов столько же
	lines := func(L *Layout) int {
		n := 0
		for _, pg := range L.Pages {
			n += len(pg.Units)
		}
		return n
	}
	one := NewMeasurer(newTTFText()).Paginate(colDoc(1, false, 30), map[*Para]*ParaL{})
	if r := float64(lines(L)) / float64(lines(one)); r < 1.8 || r > 2.4 {
		t.Errorf("строк в две колонки %d, в одну %d", lines(L), lines(one))
	}
	if d := len(L.Pages) - len(one.Pages); d < -1 || d > 1 {
		t.Errorf("листов в две колонки %d, в одну %d", len(L.Pages), len(one.Pages))
	}
	// курсор и щелчки видят колонку
	right := 0
	for _, p := range L.Place() {
		if p.Page == 0 && p.Story == "body" && p.X > pg.G.Left+cw {
			right++
			if math.Abs(p.W-cw) > 0.01 {
				t.Errorf("ширина строки во второй колонке для курсора %.1f", p.W)
			}
		}
	}
	if right == 0 {
		t.Error("Place не знает о второй колонке")
	}
}

func TestColumnBreakAndContinuous(t *testing.T) {
	d := colDoc(2, false, 2)
	d.Body[1].P.PF.CB = true
	L := NewMeasurer(newTTFText()).Paginate(d, map[*Para]*ParaL{})
	for _, pu := range L.Pages[0].Units {
		if pu.U.P == d.Body[1].P && pu.X == 0 {
			t.Fatal("после разрыва колонки абзац остался в первой колонке")
		}
	}
	// раздел в одну колонку, затем на той же странице раздел в две
	d = colDoc(2, false, 6)
	first := d.Body[1].P
	first.PF.Sect = &Section{ID: "a", Page: d.Page, Cols: Columns{}}
	d.Type = "continuous"
	L = NewMeasurer(newTTFText()).Paginate(d, map[*Para]*ParaL{})
	pg := L.Pages[0]
	var oneBottom, twoTop float64
	twoTop = 1e9
	for _, pu := range pg.Units {
		if pu.U.P == d.Body[0].P || pu.U.P == first {
			oneBottom = math.Max(oneBottom, pu.Y)
			if math.Abs(pu.W-pg.G.W) > 0.01 {
				t.Errorf("первый раздел не во всю ширину: %.1f", pu.W)
			}
		} else {
			twoTop = math.Min(twoTop, pu.Y)
		}
	}
	if twoTop < oneBottom {
		t.Errorf("колонки начались выше конца раздела в одну колонку: %.1f < %.1f", twoTop, oneBottom)
	}
	if len(L.Pages) != 1 && len(pg.Bands) == 0 {
		t.Error("непрерывный раздел с колонками не начался на той же странице")
	}
}

func TestColumnsRoundTrip(t *testing.T) {
	d := colDoc(3, true, 3)
	d.Cols.Gap = 20
	d.Body[2].P.PF.CB = true
	for _, ext := range []string{"docx", "odt"} {
		out, err := SaveBytes(d, ext)
		if err != nil {
			t.Fatal(err)
		}
		d2, err := OpenBytes("x."+ext, out)
		if err != nil {
			t.Fatal(err)
		}
		if d2.Cols.N != 3 || math.Abs(d2.Cols.Gap-20) > 0.1 || !d2.Cols.Sep {
			t.Errorf("%s: колонки после чтения %+v", ext, d2.Cols)
		}
		cb := false
		for _, b := range d2.Body {
			if b.P != nil && b.P.PF.CB {
				cb = true
			}
		}
		if !cb {
			t.Errorf("%s: разрыв колонки потерялся", ext)
		}
	}
}
