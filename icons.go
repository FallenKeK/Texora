package main

// Векторные иконки интерфейса, нарисованные своим кодом (сетка 20×20).

import "math"

type iconPainter struct {
	cv  *Canvas
	x0  float64
	y0  float64
	s   float64 // пикселей на единицу сетки
	col Color
	lw  float64
	a   *App
}

func (p *iconPainter) X(v float64) float64 { return p.x0 + v*p.s }
func (p *iconPainter) Y(v float64) float64 { return p.y0 + v*p.s }

func (p *iconPainter) line(x1, y1, x2, y2 float64) {
	p.cv.Line(p.X(x1), p.Y(y1), p.X(x2), p.Y(y2), p.lw, p.col)
}

func (p *iconPainter) poly(pts ...float64) {
	for i := 0; i+3 < len(pts); i += 2 {
		p.line(pts[i], pts[i+1], pts[i+2], pts[i+3])
	}
}

func (p *iconPainter) rect(x, y, w, h, r float64) {
	p.cv.StrokeRound(Rect{p.X(x), p.Y(y), w * p.s, h * p.s}, r*p.s, p.lw, p.col)
}

func (p *iconPainter) fill(x, y, w, h, r float64, c Color) {
	p.cv.FillRound(Rect{p.X(x), p.Y(y), w * p.s, h * p.s}, r*p.s, c)
}

func (p *iconPainter) dot(x, y, r float64) { p.cv.FillCircle(p.X(x), p.Y(y), r*p.s, p.col) }

func (p *iconPainter) arc(cx, cy, r, a0, a1 float64) {
	n := int(math.Max(6, math.Abs(a1-a0)/0.25))
	px, py := cx+r*math.Cos(a0), cy+r*math.Sin(a0)
	for i := 1; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(n)
		nx, ny := cx+r*math.Cos(a), cy+r*math.Sin(a)
		p.line(px, py, nx, ny)
		px, py = nx, ny
	}
}

func (p *iconPainter) text(s string, x, y, size float64, bold, italic bool, c Color) {
	f := Font{Family: p.a.UIFont, Px: size * p.s, Bold: bold, Italic: italic}
	w := p.a.tb.Measure(s, f)
	asc, desc := p.a.tb.Metrics(f)
	p.a.tb.Draw(p.cv, s, p.X(x)-w/2, p.Y(y)+(asc-desc)/2, f, c)
}

func (a *App) icon(cv *Canvas, name string, r Rect, col Color) {
	sz := math.Min(r.W, r.H)
	box := math.Min(sz, a.d(20))
	p := &iconPainter{cv: cv, x0: math.Round(r.X + (r.W-box)/2), y0: math.Round(r.Y + (r.H-box)/2), s: box / 20, col: col, lw: math.Max(1, 1.35*a.S), a: a}
	acc := a.th.Accent
	switch name {
	case "save":
		p.rect(3, 3, 14, 14, 2)
		p.rect(6, 3, 8, 5, 0.5)
		p.rect(6, 11, 8, 6, 0.5)
	case "undo":
		p.arc(10, 11, 5.5, math.Pi*1.05, math.Pi*2.35)
		p.poly(3.2, 6.5, 4.6, 10.8, 8.8, 9.6)
	case "redo":
		p.arc(10, 11, 5.5, -math.Pi*0.35, math.Pi*0.95)
		p.poly(16.8, 6.5, 15.4, 10.8, 11.2, 9.6)
	case "min":
		p.line(5, 10, 15, 10)
	case "max":
		p.rect(5, 5, 10, 10, 1)
	case "restore":
		p.rect(5, 7, 8, 8, 1)
		p.poly(7, 7, 7, 5, 15, 5, 15, 13, 13, 13)
	case "close":
		p.line(5, 5, 15, 15)
		p.line(15, 5, 5, 15)
	case "moon":
		p.arc(10, 10, 6, math.Pi*0.35, math.Pi*1.65)
		p.arc(13.5, 7.5, 5, math.Pi*0.62, math.Pi*1.38)
	case "sun":
		p.arc(10, 10, 3.5, 0, math.Pi*2)
		for i := 0; i < 8; i++ {
			an := float64(i) * math.Pi / 4
			p.line(10+6*math.Cos(an), 10+6*math.Sin(an), 10+8*math.Cos(an), 10+8*math.Sin(an))
		}
	case "paste":
		p.rect(4, 4, 12, 14, 1.5)
		p.fill(7, 2.5, 6, 3, 1, col)
		p.line(7, 10, 13, 10)
		p.line(7, 13, 11, 13)
	case "cut":
		p.arc(6, 15, 2.5, 0, math.Pi*2)
		p.arc(14, 15, 2.5, 0, math.Pi*2)
		p.line(7.5, 13, 14, 3)
		p.line(12.5, 13, 6, 3)
	case "copy":
		p.rect(7, 6, 10, 12, 1.5)
		p.poly(5, 14, 3.5, 14, 3.5, 2.5, 12, 2.5, 12, 4)
	case "painter":
		p.rect(4, 3, 12, 5, 1)
		p.poly(16, 5.5, 17.5, 5.5, 17.5, 10, 10, 10, 10, 12)
		p.fill(8.8, 12, 2.4, 6, 0.8, col)
	case "grow":
		p.text("A", 8, 11, 13, false, false, col)
		p.poly(14, 8, 16, 5, 18, 8)
	case "shrink":
		p.text("A", 8, 11, 11, false, false, col)
		p.poly(14, 5, 16, 8, 18, 5)
	case "case":
		p.text("Aa", 10, 10.5, 11, false, false, col)
	case "clear":
		p.text("A", 8, 10, 13, false, false, col)
		p.fill(11, 12, 7, 4, 1, Mix(col, a.th.Surface, 0.55))
		p.line(12, 17, 18, 11)
	case "bold":
		p.text("B", 10, 10.5, 14, true, false, col) // B I U, а не Ж К Ч из русского Word
	case "italic":
		p.text("I", 10, 10.5, 14, false, true, col)
	case "under":
		p.text("U", 10, 9.5, 13, false, false, col)
		p.line(5, 17, 15, 17)
	case "strike":
		p.text("abc", 10, 10.5, 11, false, false, col)
		p.line(3, 10.5, 17, 10.5)
	case "sub":
		p.text("x", 8, 9.5, 13, false, false, col)
		p.text("2", 14.5, 14, 8, false, false, col)
	case "sup":
		p.text("x", 8, 11, 13, false, false, col)
		p.text("2", 14.5, 5.5, 8, false, false, col)
	case "fontcolor":
		p.text("A", 10, 8.5, 14, false, false, col)
	case "highlight":
		p.poly(6, 14, 12, 3, 16, 5, 10, 16, 6, 14)
		p.line(6, 14, 5, 16.5)
	case "bullets":
		for i, y := range []float64{5, 10, 15} {
			_ = i
			p.dot(4.5, y, 1.3)
			p.line(8, y, 17, y)
		}
	case "numbers":
		for i, y := range []float64{5, 10, 15} {
			p.text(string(rune('1'+i)), 4.5, y, 6.5, false, false, col)
			p.line(8, y, 17, y)
		}
	case "indent":
		p.line(3, 4, 17, 4)
		p.line(9, 8, 17, 8)
		p.line(9, 12, 17, 12)
		p.line(3, 16, 17, 16)
		p.poly(3, 7.5, 6, 10, 3, 12.5)
	case "outdent":
		p.line(3, 4, 17, 4)
		p.line(9, 8, 17, 8)
		p.line(9, 12, 17, 12)
		p.line(3, 16, 17, 16)
		p.poly(6, 7.5, 3, 10, 6, 12.5)
	case "al:left", "al:center", "al:right", "al:justify":
		ws := []float64{14, 9, 14, 7}
		for i, y := range []float64{4, 8, 12, 16} {
			w := ws[i]
			if name == "al:justify" {
				w = 14
				if i == 3 {
					w = 8
				}
			}
			x := 3.0
			switch name {
			case "al:center":
				x = 10 - w/2
			case "al:right":
				x = 17 - w
			}
			p.line(x, y, x+w, y)
		}
	case "spacing":
		for _, y := range []float64{5, 10, 15} {
			p.line(9, y, 17, y)
		}
		p.line(4.5, 3, 4.5, 17)
		p.poly(2.5, 5, 4.5, 3, 6.5, 5)
		p.poly(2.5, 15, 4.5, 17, 6.5, 15)
	case "para":
		p.text("¶", 10, 10.5, 15, false, false, col)
	case "firstline":
		p.line(8, 5, 17, 5)
		p.line(3, 9, 17, 9)
		p.line(3, 13, 17, 13)
		p.line(3, 17, 12, 17)
		p.poly(3, 3, 6, 5, 3, 7)
	case "find":
		p.arc(8.5, 8.5, 5, 0, math.Pi*2)
		p.line(12.2, 12.2, 17, 17)
	case "replace":
		p.poly(4, 7, 16, 7)
		p.poly(13, 4, 16, 7, 13, 10)
		p.poly(16, 13, 4, 13)
		p.poly(7, 10, 4, 13, 7, 16)
	case "selectall":
		p.cv.StrokeRound(Rect{p.X(3), p.Y(3), 14 * p.s, 14 * p.s}, 1.5*p.s, p.lw, col.WithA(0.55))
		p.fill(6, 6, 8, 8, 1, acc.WithA(0.35))
	case "pagebreak":
		p.poly(5, 8, 5, 3, 15, 3, 15, 8)
		p.poly(5, 12, 5, 17, 15, 17, 15, 12)
		for x := 3.0; x < 17; x += 3 {
			p.line(x, 10, x+1.6, 10)
		}
	case "table":
		p.rect(3, 4, 14, 12, 1.5)
		p.line(3, 8, 17, 8)
		p.line(3, 12, 17, 12)
		p.line(8, 4, 8, 16)
		p.line(12.5, 4, 12.5, 16)
	case "picture":
		p.rect(3, 4, 14, 12, 1.5)
		p.poly(4, 15, 8.5, 9.5, 12, 13, 14, 11, 16.5, 14.5)
		p.dot(13, 7.5, 1.4)
	case "link":
		p.cv.StrokeRound(Rect{p.X(2.5), p.Y(7), 9 * p.s, 6 * p.s}, 3*p.s, p.lw, col)
		p.cv.StrokeRound(Rect{p.X(8.5), p.Y(7), 9 * p.s, 6 * p.s}, 3*p.s, p.lw, col)
	case "footnote":
		p.text("ab", 8, 11.5, 11, false, false, col)
		p.text("1", 15.5, 5.5, 7, true, false, acc)
		p.line(3, 17, 10, 17)
	case "toc":
		for i, y := range []float64{4.5, 8.5, 12.5, 16.5} {
			x := 3.0 + float64(i%2)*2.5
			p.line(x, y, 11, y)
			p.dot(13, y, 0.6)
			p.dot(15, y, 0.6)
			p.dot(17, y, 0.6)
		}
	case "header", "footer":
		p.rect(4, 2.5, 12, 15, 1.5)
		y := 5.5
		if name == "footer" {
			y = 14.5
		}
		p.fill(5.5, y-1.2, 9, 2.4, 0.8, acc)
		p.line(6.5, 9, 13.5, 9)
		p.line(6.5, 11.5, 13.5, 11.5)
	case "pagenum":
		p.rect(4, 2.5, 12, 15, 1.5)
		p.text("#", 10, 10.5, 9, true, false, acc)
	case "numpages":
		p.text("Σ", 10, 10.5, 14, false, false, col)
	case "symbol":
		p.text("Ω", 10, 10.5, 15, false, false, col)
	case "date":
		p.rect(3, 4.5, 14, 12.5, 1.5)
		p.line(3, 8.5, 17, 8.5)
		p.line(7, 2.5, 7, 6)
		p.line(13, 2.5, 13, 6)
		p.dot(7, 12, 0.9)
		p.dot(10, 12, 0.9)
		p.dot(13, 12, 0.9)
	case "margins":
		p.rect(4, 2.5, 12, 15, 1.5)
		p.cv.StrokeRound(Rect{p.X(6.5), p.Y(5), 7 * p.s, 10 * p.s}, 0, math.Max(1, a.S), acc)
	case "orient":
		p.rect(3, 6, 10, 12, 1.2)
		p.rect(8, 2.5, 9.5, 8, 1.2)
	case "size":
		p.rect(5, 2.5, 10, 15, 1.2)
		p.poly(5, 2.5, 15, 17.5)
	case "pagesetup":
		p.rect(4, 2.5, 12, 15, 1.5)
		p.arc(10, 10, 2.5, 0, math.Pi*2)
	case "zoomin", "zoomout":
		p.arc(8.5, 8.5, 5.5, 0, math.Pi*2)
		p.line(12.5, 12.5, 17, 17)
		p.line(6, 8.5, 11, 8.5)
		if name == "zoomin" {
			p.line(8.5, 6, 8.5, 11)
		}
	case "fitwidth":
		p.rect(3, 5, 14, 10, 1.5)
		p.poly(6, 8, 4.5, 10, 6, 12)
		p.poly(14, 8, 15.5, 10, 14, 12)
	case "fitpage":
		p.rect(5, 2.5, 10, 15, 1.5)
	case "stats":
		p.fill(4, 10, 3, 7, 0.5, col)
		p.fill(8.5, 6, 3, 11, 0.5, col)
		p.fill(13, 3, 3, 14, 0.5, acc)
	case "row+", "row-above":
		p.rect(3, 3, 14, 9, 1)
		p.line(3, 7.5, 17, 7.5)
		p.line(10, 13.5, 10, 18.5)
		p.line(7.5, 16, 12.5, 16)
	case "col+", "col-left":
		p.rect(3, 3, 9, 14, 1)
		p.line(7.5, 3, 7.5, 17)
		p.line(13.5, 10, 18.5, 10)
		p.line(16, 7.5, 16, 12.5)
	case "row-", "col-", "deltable":
		p.rect(3, 3, 11, 11, 1)
		p.line(11.5, 11.5, 17.5, 17.5)
		p.line(17.5, 11.5, 11.5, 17.5)
	case "merge":
		p.rect(3, 5, 14, 10, 1)
		p.poly(6, 10, 9, 10)
		p.poly(14, 10, 11, 10)
		p.poly(7.5, 8.5, 9, 10, 7.5, 11.5)
		p.poly(12.5, 8.5, 11, 10, 12.5, 11.5)
	case "border":
		p.rect(3, 3, 14, 14, 0.5)
		for x := 5.0; x < 16; x += 2.5 {
			p.dot(x, 10, 0.55)
			p.dot(10, x, 0.55)
		}
	case "shade":
		p.rect(3, 3, 14, 14, 1)
		p.fill(3.5, 3.5, 6.5, 13, 0.5, acc.WithA(0.5))
	case "new":
		p.poly(5, 2.5, 12, 2.5, 16, 6.5, 16, 17.5, 5, 17.5, 5, 2.5)
		p.poly(12, 2.5, 12, 6.5, 16, 6.5)
	case "open":
		p.poly(2.5, 16, 2.5, 4, 8, 4, 9.5, 6, 16, 6, 16, 9)
		p.poly(2.5, 16, 5.5, 9, 18, 9, 15, 16, 2.5, 16)
	case "print":
		p.rect(3, 7, 14, 7, 1.5)
		p.rect(6, 3, 8, 4, 0.5)
		p.rect(6, 11, 8, 6, 0.5)
	case "pdf":
		p.poly(5, 2.5, 12, 2.5, 16, 6.5, 16, 17.5, 5, 17.5, 5, 2.5)
		p.text("PDF", 10.5, 12, 5.5, true, false, Hex(0xc42b1c))
	case "info":
		p.arc(10, 10, 7, 0, math.Pi*2)
		p.line(10, 9, 10, 14)
		p.dot(10, 6.3, 1)
	case "back":
		p.arc(10, 10, 7.5, 0, math.Pi*2)
		p.poly(11, 6.5, 7.5, 10, 11, 13.5)
	case "chevron":
		p.poly(6, 8, 10, 12, 14, 8)
	case "check":
		p.poly(4.5, 10.5, 8.5, 14.5, 15.5, 6)
	case "styles":
		p.text("Aa", 10, 10.5, 12, false, false, col)
	case "lang":
		p.arc(10, 10, 7, 0, math.Pi*2)
		p.line(3, 10, 17, 10)
		p.arc(10, 10, 3.5, -math.Pi/2, math.Pi/2)
		p.arc(10, 10, 3.5, math.Pi/2, math.Pi*1.5)
	case "home":
		p.poly(3, 9.5, 10, 3.5, 17, 9.5)
		p.poly(5, 8, 5, 16.5, 15, 16.5, 15, 8)
		p.rect(8.5, 11.5, 3, 5, 0.5)
	case "star", "starfill":
		var pts []float64
		for i := 0; i <= 10; i++ {
			r := 7.0
			if i%2 == 1 {
				r = 3.2
			}
			ang := -math.Pi/2 + float64(i)*math.Pi/5
			pts = append(pts, 10+r*math.Cos(ang), 10.6+r*math.Sin(ang))
		}
		if name == "starfill" {
			var poly [][2]float64
			for i := 0; i+1 < len(pts); i += 2 {
				poly = append(poly, [2]float64{p.X(pts[i]), p.Y(pts[i+1])})
			}
			p.cv.FillPoly(poly, Hex(0xf5b301))
		} else {
			p.poly(pts...)
		}
	case "gear":
		p.arc(10, 10, 2.6, 0, math.Pi*2)
		for i := 0; i < 8; i++ {
			ang := float64(i) * math.Pi / 4
			p.line(10+5*math.Cos(ang), 10+5*math.Sin(ang), 10+7.3*math.Cos(ang), 10+7.3*math.Sin(ang))
		}
		p.arc(10, 10, 5.2, 0, math.Pi*2)
	case "columns": // текст в две колонки
		for _, x := range []float64{3, 11} {
			for _, y := range []float64{5, 8.5, 12, 15.5} {
				p.line(x, y, x+6, y)
			}
		}
	case "pages": // две страницы — миниатюры
		p.rect(6.5, 2.5, 10, 13, 1.2)
		p.poly(4, 5.5, 3.5, 5.5, 3.5, 17.5, 13, 17.5, 13, 17)
	case "panel": // окно с правой панелью — инспектор
		p.rect(2.5, 3.5, 15, 13, 1.5)
		p.line(12, 3.5, 12, 16.5)
		p.fill(12.5, 4.2, 4.3, 11.6, 0.8, acc.WithA(0.55))
	case "cmd": // палитра команд
		p.rect(2.5, 4.5, 15, 11, 2)
		p.poly(5.5, 8, 8, 10, 5.5, 12)
		p.line(9.5, 12, 14, 12)
	case "template":
		p.rect(3, 3, 6, 6, 1)
		p.rect(11, 3, 6, 6, 1)
		p.rect(3, 11, 6, 6, 1)
		p.rect(11, 11, 6, 6, 1)
	case "exit":
		p.poly(11, 3.5, 4, 3.5, 4, 16.5, 11, 16.5)
		p.line(8, 10, 17, 10)
		p.poly(14, 7, 17, 10, 14, 13)
	case "plus":
		p.line(10, 4.5, 10, 15.5)
		p.line(4.5, 10, 15.5, 10)
	case "spell":
		p.text("абв", 10, 8, 9, true, false, col)
		p.poly(4, 14, 8, 17.5, 16, 10)
	case "comment":
		p.rect(3, 4, 14, 10, 2)
		p.poly(7, 14, 6, 17.5, 11, 14)
		p.line(6, 8, 14, 8)
		p.line(6, 11, 11, 11)
	case "comment-":
		p.rect(3, 4, 14, 10, 2)
		p.poly(7, 14, 6, 17.5, 11, 14)
		p.line(7, 6.5, 13, 11.5)
		p.line(13, 6.5, 7, 11.5)
	case "track":
		p.rect(3, 3, 11, 14, 1.5)
		p.line(5.5, 7, 11.5, 7)
		p.line(5.5, 10, 9, 10)
		p.line(10, 17.5, 17, 10.5)
		p.line(17, 10.5, 15.5, 9)
		p.line(15.5, 9, 8.5, 16)
		p.fill(8.3, 16, 2, 2, 0.5, acc)
	case "markup":
		p.rect(2.5, 4, 15, 12, 2)
		p.text("ab", 10, 10, 9, false, false, acc)
		p.line(5, 12.5, 15, 12.5)
	case "accept":
		p.rect(3, 3, 14, 14, 3)
		p.poly(6, 10, 9, 13, 14.5, 7)
	case "reject":
		p.rect(3, 3, 14, 14, 3)
		p.line(7, 7, 13, 13)
		p.line(13, 7, 7, 13)
	case "prev":
		p.poly(12.5, 4.5, 7, 10, 12.5, 15.5)
	case "next":
		p.poly(7.5, 4.5, 13, 10, 7.5, 15.5)
	case "eq":
		p.text("π", 6.5, 10, 13, false, true, col)
		p.line(11, 6, 17, 6)
		p.text("x", 14, 12.5, 9, false, true, acc)
		p.line(11, 14, 17, 14)
	case "section":
		p.line(3, 6, 17, 6)
		p.line(3, 14, 17, 14)
		for x := 3.0; x < 17; x += 3 {
			p.line(x, 10, x+1.6, 10)
		}
	case "cite":
		p.text("[1]", 10, 10, 10, true, false, acc)
	case "bib":
		p.rect(4, 3, 12, 14, 1.5)
		p.line(7, 7, 13, 7)
		p.line(7, 10, 13, 10)
		p.line(7, 13, 11, 13)
		p.line(4, 3, 4, 17)
	case "wrap":
		p.fill(3, 4, 7, 7, 1, acc)
		p.line(12, 5, 17, 5)
		p.line(12, 8, 17, 8)
		p.line(12, 11, 17, 11)
		p.line(3, 14, 17, 14)
		p.line(3, 17, 14, 17)
	case "hyph":
		p.text("пе-", 9, 8, 9, false, false, col)
		p.text("ре", 7, 15, 9, false, false, acc)
	case "update":
		p.arc(10, 10, 6, math.Pi*0.15, math.Pi*1.75)
		p.poly(14.5, 2.5, 15.8, 6.2, 12, 6.8)
	case "regex":
		p.text(".*", 10, 10, 12, true, false, acc)
	case "style":
		p.text("Aa", 10, 10, 13, false, false, col)
		p.line(4, 16.5, 16, 16.5)
	case "x":
		p.line(6, 6, 14, 14)
		p.line(14, 6, 6, 14)
	case "recent":
		p.arc(10, 10, 7, 0, math.Pi*2)
		p.poly(10, 5.5, 10, 10, 13, 12)
	default:
		p.rect(4, 4, 12, 12, 2)
	}
}

// Цвета бренда Texora.
var (
	brandMain  = Hex(0x7c3aed)
	brandLight = Hex(0xa78bfa)
	brandDeep  = Hex(0x5b21b6)
)

// LogoFlat — плоский логотип: белая «T» в фиолетовой скруглённой плитке.
func LogoFlat(cv *Canvas, r Rect) {
	s := r.W / 32
	tile := Rect{r.X + 1*s, r.Y + 1*s, 30 * s, 30 * s}
	// вертикальный градиент полосками
	steps := int(math.Max(8, tile.H))
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps)
		cv.SetClip(Rect{r.X, tile.Y + tile.H*t, r.W, tile.H/float64(steps) + 1})
		cv.FillRound(tile, 7*s, Mix(Hex(0x8b5cf6), brandDeep, t))
	}
	cv.ResetClip()
	cv.FillRound(Rect{r.X + 8*s, r.Y + 8*s, 16 * s, 4.6 * s}, 1.6*s, Hex(0xffffff))
	cv.FillRound(Rect{r.X + 13.7*s, r.Y + 8*s, 4.6 * s, 16.5 * s}, 1.6*s, Hex(0xffffff))
}

// LogoIso — объёмная «T» в изометрии (для заставки и «О программе»).
func LogoIso(cv *Canvas, r Rect) {
	u := r.W / 4.2
	ox, oy := r.X+0.1*u, r.Y+0.75*u
	dx, dy := 0.75*u, -0.6*u // направление глубины
	P := func(x, y float64) [2]float64 { return [2]float64{ox + x*u, oy + y*u} }
	D := func(p [2]float64) [2]float64 { return [2]float64{p[0] + dx, p[1] + dy} }
	front := Hex(0x8b5cf6)
	top := brandLight
	side := Hex(0x4c1d95)
	quad := func(a, b [2]float64, c Color) { cv.FillPoly([][2]float64{a, b, D(b), D(a)}, c) }
	// ножка: правая грань
	quad(P(2.1, 1), P(2.1, 3.2), side)
	// перекладина: верх и правая грань
	quad(P(0, 0), P(3.2, 0), top)
	quad(P(3.2, 0), P(3.2, 1), side)
	// лицевая сторона
	cv.FillPoly([][2]float64{P(0, 0), P(3.2, 0), P(3.2, 1), P(2.1, 1), P(2.1, 3.2), P(1.1, 3.2), P(1.1, 1), P(0, 1)}, front)
	// блик на лицевой стороне
	cv.FillPoly([][2]float64{P(0, 0), P(3.2, 0), P(3.2, 0.12), P(0, 0.12)}, Hex(0xffffff).WithA(0.18))
}

// AppIcon рисует значок приложения (плоский логотип). Альфа — в старшем байте.
func AppIcon(size int) []uint32 {
	cv := &Canvas{W: size, H: size, Pix: make([]uint32, size*size)}
	cv.ResetClip()
	cv.Clear(Hex(0))
	// рисуем на чёрном и на белом, чтобы восстановить прозрачность
	LogoFlat(cv, Rect{0, 0, float64(size), float64(size)})
	black := append([]uint32(nil), cv.Pix...)
	cv.Clear(Hex(0xffffff))
	LogoFlat(cv, Rect{0, 0, float64(size), float64(size)})
	out := make([]uint32, size*size)
	for i := range out {
		b, w := black[i], cv.Pix[i]
		a := 255 - (int(w&0xff) - int(b&0xff))
		if a <= 0 {
			continue
		}
		a = min(a, 255)
		un := func(sh uint) uint32 { return uint32(min(255, int(b>>sh&0xff)*255/a)) }
		out[i] = uint32(a)<<24 | un(16)<<16 | un(8)<<8 | un(0)
	}
	return out
}
