//go:build windows

package main

import (
	"math"
	"testing"
)

// Метрики строки на настоящих шрифтах Windows: должны совпасть с тем, что считает sfntLineMetrics
// по таблицам шрифта (и с Word: Times New Roman — 1,15 кегля).
func TestWinLineMetrics(t *testing.T) {
	g := newGdiText()
	for _, c := range []struct {
		fam            string
		asc, desc, gap float64 // при кегле 2048
	}{{"Times New Roman", 1825, 443, 87}, {"Arial", 1854, 434, 67}, {"Calibri", 1536, 512, 452}} {
		f := Font{Family: c.fam, Px: 2048}
		a, d := g.Metrics(f)
		l := g.Leading(f)
		t.Logf("%s: asc %.0f desc %.0f gap %.1f (высота строки %.3f кегля)", c.fam, a, d, l, (a+d+l)/2048)
		if c.fam == "Calibri" {
			// у Calibri флага USE_TYPO_METRICS нет: win 1950+550, интерлиньяж 0 → 1,22 кегля
			if math.Abs((a+d+l)/2048-1.2207) > 0.01 {
				t.Errorf("Calibri: высота строки %.4f кегля, ждали 1,2207", (a+d+l)/2048)
			}
			continue
		}
		if math.Abs(a-c.asc) > 3 || math.Abs(d-c.desc) > 3 || math.Abs(l-c.gap) > 3 {
			t.Errorf("%s: asc %.0f desc %.0f gap %.1f, ждали %.0f %.0f %.0f", c.fam, a, d, l, c.asc, c.desc, c.gap)
		}
	}
	// вёрстка видит интерлиньяж
	m := NewMeasurer(g)
	if l := m.Leading(Font{Family: "Times New Roman", Px: 14}); l < 0.4 || l > 0.8 {
		t.Errorf("Times New Roman 14: интерлиньяж %.2f пт, ждали около 0,59", l)
	}
}
