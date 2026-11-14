package main

// Отрисовка страниц документа на холст. Один код для экрана, печати и PDF.

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strconv"
	"strings"
)

type PagePainter struct {
	cv     *Canvas
	tb     TextBackend
	doc    *Doc
	k      float64 // пикселей на пункт
	ox, oy float64
	alpha  float64
	imgs   map[string]*scaledImg
	marks  bool                 // показывать исправления и примечания
	spell  func(*Para) [][2]int // ошибки орфографии в абзаце (только на экране)
	m      *Measurer
	fronts []frontImg
	grid   bool     // сетка таблиц без границ (только на экране, как «Отобразить сетку» в Word)
	vec    *pdfPage // не nil — рисуем не в пиксели, а векторами в PDF (тогда cv не нужен, k = 1)
}

// Примитивы рисования: в пиксельный холст или в PDF.
func (pp *PagePainter) fillRect(r Rect, c Color) {
	if pp.vec != nil {
		pp.vec.fillRect(r, c)
		return
	}
	pp.cv.FillRect(r, c)
}

func (pp *PagePainter) line(x1, y1, x2, y2, w float64, c Color) {
	if pp.vec != nil {
		pp.vec.line(x1, y1, x2, y2, w, c)
		return
	}
	pp.cv.Line(x1, y1, x2, y2, w, c)
}

type frontImg struct {
	it   *Item
	x, y float64
}

type scaledImg struct {
	w, h int
	pix  []uint32
}

func parseHexColor(s string, def Color) Color {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return def
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return def
	}
	return Hex(uint32(v))
}

func (pp *PagePainter) X(v float64) float64 { return pp.ox + v*pp.k }
func (pp *PagePainter) Y(v float64) float64 { return pp.oy + v*pp.k }

func (pp *PagePainter) col(c Color) Color {
	if pp.alpha < 1 {
		return c.WithA(c.A() * pp.alpha)
	}
	return c
}

func (pp *PagePainter) text(s string, x, base float64, f F, font Font, c Color) {
	if s == "" {
		return
	}
	ft := font
	ft.Px = font.Px * pp.k
	ft.Underline = false
	col := c
	if pp.alpha < 1 {
		col = Mix(Hex(0xffffff), c, pp.alpha)
	}
	if pp.vec != nil {
		pp.vec.text(s, pp.X(x), pp.Y(base), ft, col)
		return
	}
	if pd, ok := pp.tb.(posDrawer); ok {
		// буквы ставим туда, куда их посчитала вёрстка (иначе хинтинг GDI «съедает» или раздувает пробелы)
		pd.DrawScaled(pp.cv, s, pp.X(x), pp.Y(base), ft, col, font.Px*measureK)
		return
	}
	pp.tb.Draw(pp.cv, s, pp.X(x), pp.Y(base), ft, col)
}

// posDrawer — бэкенд, умеющий рисовать с позициями букв из эталонного размера refPx.
type posDrawer interface {
	DrawScaled(cv *Canvas, s string, x, baseline float64, f Font, c Color, refPx float64)
}

func (pp *PagePainter) drawImage(id string, x, y, w, h float64) {
	m := pp.doc.Media[id]
	r := Rect{pp.X(x), pp.Y(y), w * pp.k, h * pp.k}
	if pp.vec != nil && m != nil {
		pp.vec.image(m, r)
		return
	}
	if m == nil {
		pp.fillRect(r, pp.col(Hex(0xdddddd)))
		return
	}
	if m.img == nil {
		img, _, err := image.Decode(bytes.NewReader(m.Data))
		if err != nil {
			m.img = false
		} else {
			m.img = img
		}
	}
	img, ok := m.img.(image.Image)
	if !ok {
		pp.fillRect(r, pp.col(Hex(0xeeeeee)))
		if pp.vec != nil {
			pp.vec.strokeRect(r, 0.5, Hex(0xbbbbbb))
		} else {
			pp.cv.StrokeRound(r, 0, 1, Hex(0xbbbbbb))
		}
		return
	}
	iw, ih := int(math.Round(r.W)), int(math.Round(r.H))
	if iw < 1 || ih < 1 {
		return
	}
	key := id + ":" + strconv.Itoa(iw) + "x" + strconv.Itoa(ih)
	si := pp.imgs[key]
	if si == nil {
		si = scaleImage(img, iw, ih)
		if pp.imgs != nil {
			pp.imgs[key] = si
		}
	}
	pp.cv.sync()
	x0, y0 := int(math.Round(r.X)), int(math.Round(r.Y))
	for yy := 0; yy < ih; yy++ {
		py := y0 + yy
		if py < 0 || py >= pp.cv.H || float64(py) < pp.cv.clip.Y || float64(py) >= pp.cv.clip.Y+pp.cv.clip.H {
			continue
		}
		for xx := 0; xx < iw; xx++ {
			px := x0 + xx
			if px < 0 || px >= pp.cv.W {
				continue
			}
			c := Color(si.pix[yy*iw+xx])
			a := c.A() * pp.alpha
			if a > 0 {
				pp.cv.blend(py*pp.cv.W+px, c|0xff000000, a)
			}
		}
	}
}

// scaleImage — уменьшение/увеличение с усреднением (без внешних библиотек).
func scaleImage(img image.Image, w, h int) *scaledImg {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := &scaledImg{w: w, h: h, pix: make([]uint32, w*h)}
	for y := 0; y < h; y++ {
		sy0 := b.Min.Y + y*sh/h
		sy1 := b.Min.Y + max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			sx0 := b.Min.X + x*sw/w
			sx1 := b.Min.X + max((x+1)*sw/w, x*sw/w+1)
			var r, g, bl, a, n uint32
			stepY := max(1, (sy1-sy0)/4)
			stepX := max(1, (sx1-sx0)/4)
			for yy := sy0; yy < sy1; yy += stepY {
				for xx := sx0; xx < sx1; xx += stepX {
					cr, cg, cb, ca := img.At(xx, yy).RGBA()
					r += cr >> 8
					g += cg >> 8
					bl += cb >> 8
					a += ca >> 8
					n++
				}
			}
			if n == 0 {
				continue
			}
			r, g, bl, a = r/n, g/n, bl/n, a/n
			if a > 0 && a < 255 { // из premultiplied в обычный
				r, g, bl = min(255, r*255/a), min(255, g*255/a), min(255, bl*255/a)
			}
			out.pix[y*w+x] = a<<24 | r<<16 | g<<8 | bl
		}
	}
	return out
}

// revColor — цвет автора правки.
func revColor(d *Doc, id string) Color {
	pal := []uint32{0xc0392b, 0x2e7d32, 0x6a1b9a, 0x1565c0, 0xef6c00, 0x00838f}
	a := id
	if r := d.Revs[id]; r != nil {
		a = r.Author
	}
	h := 0
	for _, c := range a {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return Hex(pal[h%len(pal)])
}

func (pp *PagePainter) drawFloat(it *Item, x, y float64) {
	pp.drawImage(it.ID, x+it.X, y+it.FY, it.W, it.H)
}

func (pp *PagePainter) DrawLine(ln *Line, x, y float64) {
	for i := range ln.Floats {
		if ln.Floats[i].Wrap == "front" {
			pp.fronts = append(pp.fronts, frontImg{it: &ln.Floats[i], x: x, y: y})
		} else {
			pp.drawFloat(&ln.Floats[i], x, y)
		}
	}
	if ln.Rev && pp.marks {
		pp.fillRect(Rect{pp.X(x - 14), pp.Y(y), math.Max(1, 0.8*pp.k), ln.H * pp.k}, pp.col(Hex(0x7f7f7f)))
	}
	if ln.Marker != nil {
		m := ln.Marker
		sh := 0.0
		if m.F.Sup {
			sh = m.F.Size * 0.33
		}
		pp.text(m.Text, x+m.X, y+ln.Base-sh, m.F, m.Font, parseHexColor(m.F.Color, Hex(0)))
	}
	for i := range ln.Items {
		it := &ln.Items[i]
		ix := x + it.X
		switch it.K {
		case kImg:
			pp.drawImage(it.ID, ix, y+ln.Base-it.H, it.W, it.H)
			continue
		case kBreak, kFloat:
			continue
		}
		f := it.F
		if f.Cmt != "" && pp.marks {
			c := Hex(0xfff2a8)
			if cm := pp.doc.Comments[f.Cmt]; cm != nil && cm.Done {
				c = Hex(0xf0f0f0)
			}
			if pp.doc.activeCmt == f.Cmt {
				c = Hex(0xffd966)
			}
			pp.fillRect(Rect{pp.X(ix), pp.Y(y), it.W * pp.k, ln.H * pp.k}, pp.col(c))
		}
		if f.Bg != "" && it.K != kTab {
			pp.fillRect(Rect{pp.X(ix), pp.Y(y), it.W * pp.k, ln.H * pp.k}, pp.col(parseHexColor(f.Bg, Hex(0xffff00))))
		}
		col := parseHexColor(f.Color, Hex(0x000000))
		if f.Link != "" && f.Color == "" && !strings.HasPrefix(f.Link, "#_Toc") {
			col = Hex(0x0563c1)
		}
		rev := f.Ins != "" || f.Del != ""
		if rev && pp.marks {
			id := f.Ins
			if id == "" {
				id = f.Del
			}
			col = revColor(pp.doc, id)
		}
		by := y + ln.Base - it.Shift
		uw := it.W
		if i == len(ln.Items)-1 {
			uw = it.WT
		}
		lw := math.Max(0.5, f.Size/18)
		if it.K == kEq {
			pp.drawEq(it.Eq, ix, by, col)
		} else if it.K == kTab {
			if it.Lead == "dot" {
				dw := pp.tbWidth(".", it.Font)
				if dw > 0 {
					n := int((it.W - dw) / (dw * 1.6))
					for k := 0; k < n; k++ {
						pp.text(".", ix+it.W-dw*1.6*float64(k+1), by, f, it.Font, col)
					}
				}
			}
		} else {
			t := strings.TrimRight(string(it.Text), " ")
			if pp.vec != nil && !it.Hyph && len(t) < len(string(it.Text)) {
				t += " " // настоящий пробел в PDF: так текст правильно копируется и ищется (Антиплагиат)
			}
			if f.Caps {
				t = strings.ToUpper(t)
			}
			if it.Hyph {
				t += "-"
			}
			pp.text(t, ix, by, f, it.Font, col)
		}
		if pp.vec != nil && f.Link != "" {
			pp.vec.link(Rect{pp.X(ix), pp.Y(y), uw * pp.k, ln.H * pp.k}, f.Link)
		}
		if pp.spell != nil && it.K == kText && ln.P != nil {
			for _, sp := range pp.spell(ln.P) {
				a, b := max(sp[0], it.O), min(sp[1], it.O+len(it.Text))
				if a >= b {
					continue
				}
				x1 := ix + pp.tbWidth(string(it.Text[:a-it.O]), it.Font)
				x2 := ix + pp.tbWidth(string(it.Text[:b-it.O]), it.Font)
				pp.squiggle(x1, x2, by+math.Max(1.5, f.Size*0.16))
			}
		}
		if f.U || f.DU || (f.Link != "" && !strings.HasPrefix(f.Link, "#_Toc")) || (f.Ins != "" && pp.marks) {
			uy := by + math.Max(1, f.Size*0.12)
			pp.fillRect(Rect{pp.X(ix), pp.Y(uy), uw * pp.k, math.Max(1, lw*pp.k)}, pp.col(col))
			if f.DU {
				pp.fillRect(Rect{pp.X(ix), pp.Y(uy + lw*2.2), uw * pp.k, math.Max(1, lw*pp.k)}, pp.col(col))
			}
		}
		if f.S || (f.Del != "" && pp.marks) {
			sy := by - f.Size*0.28
			pp.fillRect(Rect{pp.X(ix), pp.Y(sy), uw * pp.k, math.Max(1, lw*pp.k)}, pp.col(col))
		}
	}
}

func (pp *PagePainter) tbWidth(s string, f Font) float64 {
	if pp.m != nil {
		return pp.m.Width(s, f)
	}
	ft := f
	ft.Px *= 4
	return pp.tb.Measure(s, ft) / 4
}

func (pp *PagePainter) squiggle(x1, x2, y float64) {
	k := pp.k
	step := math.Max(2, 2*k/1.33)
	amp := math.Max(1, 0.9*k/1.33)
	c := Hex(0xe53935)
	px, py := pp.X(x1), pp.Y(y)
	up := true
	for xx := pp.X(x1) + step; xx <= pp.X(x2)+0.5; xx += step {
		ny := pp.Y(y)
		if up {
			ny -= amp
		}
		pp.line(px, py, xx, ny, math.Max(1, 0.75*k/1.33), c)
		px, py = xx, ny
		up = !up
	}
}

func sameBorder(a, b *Border) bool {
	if a == nil || a.W <= 0 {
		return b == nil || b.W <= 0
	}
	return b != nil && a.W == b.W && a.Color == b.Color && a.Style == b.Style
}

func hasBdr(b *Border) bool { return b != nil && b.W > 0 }

// paraBorders — линии над и под абзацем (подряд идущие абзацы с одинаковыми линиями — одна рамка).
func (pp *PagePainter) paraBorders(us []*Unit, i int, ox, y float64) {
	u := us[i]
	pf := u.P.PF
	if !hasBdr(pf.BdrTop) && !hasBdr(pf.BdrBottom) {
		return
	}
	same := func(q *Para) bool {
		return q != nil && sameBorder(q.PF.BdrTop, pf.BdrTop) && sameBorder(q.PF.BdrBottom, pf.BdrBottom)
	}
	ep := EffPF(pp.doc, u.P)
	x0, x1 := ox+ep.IndL, ox+u.PW-ep.IndR
	if pf.List != nil || ep.IndF < 0 {
		x0 += math.Min(0, ep.IndF)
	}
	draw := func(b *Border, yy float64) {
		c := Hex(0)
		if b.Color != "" {
			c = parseHexColor(b.Color, c)
		}
		lw := math.Max(1, b.W*pp.k)
		pp.fillRect(Rect{pp.X(x0), pp.Y(yy) - lw/2, (x1 - x0) * pp.k, lw}, pp.col(c))
		if b.Style == "double" {
			pp.fillRect(Rect{pp.X(x0), pp.Y(yy) + lw*1.5, (x1 - x0) * pp.k, lw}, pp.col(c))
		}
	}
	if u.First && hasBdr(pf.BdrTop) {
		var prev *Para
		if i > 0 && us[i-1].K == uLine {
			prev = us[i-1].P
		}
		if !same(prev) {
			draw(pf.BdrTop, y-1)
		}
	}
	if u.Last && hasBdr(pf.BdrBottom) {
		var next *Para
		if i+1 < len(us) && us[i+1].K == uLine {
			next = us[i+1].P
		}
		if !same(next) {
			draw(pf.BdrBottom, y+u.H+1)
		}
	}
}

func (pp *PagePainter) DrawUnits(us []*Unit, ox, oy float64, useY func(*Unit) float64) {
	for i, u := range us {
		y := oy + useY(u)
		if u.K == uLine {
			if pp.vec != nil && u.P != nil && u.First {
				pp.vec.anchor(u.P, pp.Y(y))
			}
			pp.DrawLine(u.Line, ox, y)
			if u.P != nil && pp.doc != nil && u.PW > 0 {
				pp.paraBorders(us, i, ox, y)
			}
		} else {
			pp.drawRow(u, ox, y)
		}
	}
}

func (pp *PagePainter) drawRow(u *Unit, ox, oy float64) {
	t := u.Table
	ri := 0
	for i, r := range t.Rows {
		if r == u.Row {
			ri = i
			break
		}
	}
	// ячейка ниже продолжает объединение? (тогда нижней линии нет)
	contBelow := func(g int) bool {
		if ri+1 >= len(t.Rows) {
			return false
		}
		pos := 0
		for ci := 0; ci < g && ci < len(u.Row.Cells); ci++ {
			pos += max(1, u.Row.Cells[ci].Span)
		}
		p2 := 0
		for _, c := range t.Rows[ri+1].Cells {
			if p2 == pos {
				return c.VMerge == "continue"
			}
			p2 += max(1, c.Span)
		}
		return false
	}
	nc := len(u.Cells)
	for ci, c := range u.Cells {
		x, w, h := ox+c.X, c.W, u.H
		if c.Cell.Shade != "" {
			pp.fillRect(Rect{pp.X(x), pp.Y(oy), w * pp.k, h * pp.k}, pp.col(parseHexColor(c.Cell.Shade, Hex(0xeeeeee))))
		}
		cpt, cpl, _, _ := t.CellPads(c.Cell)
		pp.DrawUnits(c.Units, x+cpl, oy+cpt+c.VOff(u), func(x *Unit) float64 { return x.Y })
		line := func(b *Border, r Rect) {
			col := Hex(0)
			if b.Color != "" {
				col = parseHexColor(b.Color, col)
			}
			pp.fillRect(r, pp.col(col))
		}
		lwOf := func(b *Border) float64 { return math.Max(1, b.W*pp.k) }
		gridCol := pp.col(RGBA(110, 160, 230, 170))
		dash := func(horiz bool, x0, y0, l float64) {
			if !pp.grid {
				return
			}
			step := math.Max(4, 3*pp.k)
			for d := 0.0; d < l; d += step * 2 {
				s := math.Min(step, l-d)
				if horiz {
					pp.fillRect(Rect{math.Round(x0 + d), math.Round(y0), s, 1}, gridCol)
				} else {
					pp.fillRect(Rect{math.Round(x0), math.Round(y0 + d), 1, s}, gridCol)
				}
			}
		}
		if c.Cell.VMerge != "continue" {
			if b := t.EdgeBorder(ri, ci, nc, c.Cell, 0); b != nil {
				lw := lwOf(b)
				line(b, Rect{pp.X(x) - lw/2, pp.Y(oy) - lw/2, w*pp.k + lw, lw})
			} else {
				dash(true, pp.X(x), pp.Y(oy), w*pp.k)
			}
		}
		if !contBelow(ci) {
			if b := t.EdgeBorder(ri, ci, nc, c.Cell, 2); b != nil {
				lw := lwOf(b)
				line(b, Rect{pp.X(x) - lw/2, pp.Y(oy+h) - lw/2, w*pp.k + lw, lw})
			} else {
				dash(true, pp.X(x), pp.Y(oy+h), w*pp.k)
			}
		}
		if b := t.EdgeBorder(ri, ci, nc, c.Cell, 1); b != nil {
			lw := lwOf(b)
			line(b, Rect{pp.X(x) - lw/2, pp.Y(oy), lw, h * pp.k})
		} else {
			dash(false, pp.X(x), pp.Y(oy), h*pp.k)
		}
		if b := t.EdgeBorder(ri, ci, nc, c.Cell, 3); b != nil {
			lw := lwOf(b)
			line(b, Rect{pp.X(x+w) - lw/2, pp.Y(oy), lw, h * pp.k})
		} else {
			dash(false, pp.X(x+w), pp.Y(oy), h*pp.k)
		}
	}
}

// DrawPage рисует страницу; active — какая история редактируется.
func (pp *PagePainter) DrawPage(L *Layout, pg *PageL, active string, print bool) {
	hf := IsHF(active)
	g := pg.G
	pp.alpha = 1
	if hf {
		pp.alpha = 0.45
	}
	pp.fronts = nil
	ys := map[*Unit]float64{}
	for _, pu := range pg.Units {
		ys[pu.U] = pu.Y
	}
	// подряд идущие блоки одной колонки рисуются вместе (линии абзацев смотрят на соседей)
	for i := 0; i < len(pg.Units); {
		j := i
		var us []*Unit
		for j < len(pg.Units) && pg.Units[j].X == pg.Units[i].X {
			us = append(us, pg.Units[j].U)
			j++
		}
		pp.DrawUnits(us, g.Left+pg.Units[i].X, g.BodyTop, func(u *Unit) float64 { return ys[u] })
		i = j
	}
	// линии между колонками
	for _, b := range pg.Bands {
		if !b.Cols.Sep || b.Bottom <= b.Top {
			continue
		}
		gap := b.Cols.ColGap()
		for c := 1; c < b.Cols.Count(); c++ {
			x := g.Left + float64(c)*(b.W+gap) - gap/2
			pp.fillRect(Rect{pp.X(x) - math.Max(0.5, 0.5*pp.k)/2, pp.Y(g.BodyTop + b.Top), math.Max(0.5, 0.5*pp.k), (b.Bottom - b.Top) * pp.k}, pp.col(Hex(0)))
		}
	}
	if len(pg.Fns) > 0 {
		y := g.BodyTop + g.BodyH - pg.FnH
		pp.fillRect(Rect{pp.X(g.Left), pp.Y(y + FnSep/2), g.W / 3 * pp.k, math.Max(1, 0.5*pp.k)}, pp.col(Hex(0)))
		y += FnSep
		for _, id := range pg.Fns {
			fl := L.FnLay[id]
			pp.DrawUnits(fl.Units, g.Left, y, func(u *Unit) float64 { return u.Y })
			y += fl.H
		}
	}
	pp.alpha = 1
	if !hf && !print {
		pp.alpha = 0.8
	}
	if pg.Header != nil {
		pp.DrawUnits(pg.Header.Units, g.Left, g.Page.Header, func(u *Unit) float64 { return u.Y })
	}
	if pg.Footer != nil {
		pp.DrawUnits(pg.Footer.Units, g.Left, g.Page.H-g.Page.Footer-pg.Footer.H, func(u *Unit) float64 { return u.Y })
	}
	pp.alpha = 1
	for _, f := range pp.fronts {
		pp.drawFloat(f.it, f.x, f.y)
	}
	pp.fronts = nil
}
