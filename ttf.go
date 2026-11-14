//go:build !windows

package main

// Собственный разбор и растеризация TrueType — для тестов и скриншотов вне Windows.

import (
	"math"
	"os"
	"strings"
)

type tpt struct {
	x, y float64
	on   bool
}

// contours возвращает контуры глифа в единицах шрифта.
func (f *ttFont) contours(gid int, depth int) [][]tpt {
	g := f.glyphData(gid)
	if g == nil || depth > 8 {
		return nil
	}
	nc := s16(g, 0)
	if nc >= 0 {
		ends := make([]int, nc)
		for i := range ends {
			ends[i] = u16(g, 10+i*2)
		}
		if nc == 0 {
			return nil
		}
		np := ends[nc-1] + 1
		p := 10 + nc*2
		p += 2 + u16(g, p)
		flags := make([]byte, np)
		for i := 0; i < np; {
			fl := g[p]
			p++
			flags[i] = fl
			i++
			if fl&8 != 0 {
				rep := int(g[p])
				p++
				for k := 0; k < rep && i < np; k++ {
					flags[i] = fl
					i++
				}
			}
		}
		xs, ys := make([]float64, np), make([]float64, np)
		v := 0
		for i := 0; i < np; i++ {
			fl := flags[i]
			if fl&2 != 0 {
				d := int(g[p])
				p++
				if fl&16 == 0 {
					d = -d
				}
				v += d
			} else if fl&16 == 0 {
				v += s16(g, p)
				p += 2
			}
			xs[i] = float64(v)
		}
		v = 0
		for i := 0; i < np; i++ {
			fl := flags[i]
			if fl&4 != 0 {
				d := int(g[p])
				p++
				if fl&32 == 0 {
					d = -d
				}
				v += d
			} else if fl&32 == 0 {
				v += s16(g, p)
				p += 2
			}
			ys[i] = float64(v)
		}
		out := [][]tpt{}
		s := 0
		for _, e := range ends {
			c := []tpt{}
			for i := s; i <= e; i++ {
				c = append(c, tpt{xs[i], ys[i], flags[i]&1 != 0})
			}
			out = append(out, c)
			s = e + 1
		}
		return out
	}
	// составной глиф
	out := [][]tpt{}
	p := 10
	for {
		fl := u16(g, p)
		idx := u16(g, p+2)
		p += 4
		var dx, dy float64
		if fl&1 != 0 {
			dx, dy = float64(s16(g, p)), float64(s16(g, p+2))
			p += 4
		} else {
			dx, dy = float64(int8(g[p])), float64(int8(g[p+1]))
			p += 2
		}
		a, b, c, d := 1.0, 0.0, 0.0, 1.0
		f2 := func(o int) float64 { return float64(s16(g, o)) / 16384 }
		if fl&8 != 0 {
			a = f2(p)
			d = a
			p += 2
		} else if fl&0x40 != 0 {
			a, d = f2(p), f2(p+2)
			p += 4
		} else if fl&0x80 != 0 {
			a, b, c, d = f2(p), f2(p+2), f2(p+4), f2(p+6)
			p += 8
		}
		for _, ct := range f.contours(idx, depth+1) {
			nc := make([]tpt, len(ct))
			for i, q := range ct {
				nc[i] = tpt{q.x*a + q.y*c + dx, q.x*b + q.y*d + dy, q.on}
			}
			out = append(out, nc)
		}
		if fl&0x20 == 0 {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------- растеризация (метод накопления площадей)
type raster struct {
	w, h int
	a    []float32
}

func (r *raster) line(x0, y0, x1, y1 float64) {
	if y0 == y1 {
		return
	}
	dir := float32(1)
	if y0 > y1 {
		dir = -1
		x0, y0, x1, y1 = x1, y1, x0, y0
	}
	dxdy := (x1 - x0) / (y1 - y0)
	x := x0
	if y0 < 0 {
		x -= y0 * dxdy
	}
	for y := int(math.Max(0, y0)); y < r.h && float64(y) < y1; y++ {
		ls := y * r.w
		dy := math.Min(float64(y+1), y1) - math.Max(float64(y), y0)
		xn := x + dxdy*dy
		d := float32(dy) * dir
		xa, xb := x, xn
		if xa > xb {
			xa, xb = xb, xa
		}
		xaf := math.Floor(xa)
		xai := int(xaf)
		xbc := math.Ceil(xb)
		xbi := int(xbc)
		if xai < 0 || xbi+1 >= r.w {
			x = xn
			continue
		}
		if xbi <= xai+1 {
			xmf := float32(0.5*(x+xn) - xaf)
			r.a[ls+xai] += d - d*xmf
			r.a[ls+xai+1] += d * xmf
		} else {
			s := float32(1 / (xb - xa))
			x0f := float32(xa - xaf)
			a0 := 0.5 * s * (1 - x0f) * (1 - x0f)
			x1f := float32(xb - xbc + 1)
			am := 0.5 * s * x1f * x1f
			r.a[ls+xai] += d * a0
			if xbi == xai+2 {
				r.a[ls+xai+1] += d * (1 - a0 - am)
			} else {
				a1 := s * (1.5 - x0f)
				r.a[ls+xai+1] += d * (a1 - a0)
				for xi := xai + 2; xi < xbi-1; xi++ {
					r.a[ls+xi] += d * s
				}
				a2 := a1 + float32(xbi-xai-3)*s
				r.a[ls+xbi-1] += d * (1 - a2 - am)
			}
			r.a[ls+xbi] += d * am
		}
		x = xn
	}
}

func (f *ttFont) glyph(gid int, px float64) *glyphBmp {
	k := glyphKey{gid, int(px*8 + 0.5)}
	if g, ok := f.cache[k]; ok {
		return g
	}
	sc := px / f.upem
	cs := f.contours(gid, 0)
	minx, miny, maxx, maxy := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range cs {
		for _, p := range c {
			minx, maxx = math.Min(minx, p.x*sc), math.Max(maxx, p.x*sc)
			miny, maxy = math.Min(miny, -p.y*sc), math.Max(maxy, -p.y*sc)
		}
	}
	g := &glyphBmp{}
	if len(cs) == 0 {
		f.cache[k] = g
		return g
	}
	g.x0, g.y0 = int(math.Floor(minx))-1, int(math.Floor(miny))-1
	g.w, g.h = int(math.Ceil(maxx))-g.x0+3, int(math.Ceil(maxy))-g.y0+2
	r := &raster{w: g.w, h: g.h, a: make([]float32, g.w*g.h+g.w)}
	tx := func(p tpt) (float64, float64) { return p.x*sc - float64(g.x0), -p.y*sc - float64(g.y0) }
	for _, c := range cs {
		n := len(c)
		if n < 2 {
			continue
		}
		// точка старта — на кривой
		start := 0
		for start < n && !c[start].on {
			start++
		}
		var sx, sy float64
		if start == n {
			sx, sy = tx(tpt{(c[0].x + c[1].x) / 2, (c[0].y + c[1].y) / 2, true})
			start = 0
		} else {
			sx, sy = tx(c[start])
		}
		cx, cy := sx, sy
		var qx, qy float64
		haveCtl := false
		quad := func(ex, ey float64) {
			steps := int(math.Max(2, math.Min(16, math.Hypot(ex-cx, ey-cy)/2)))
			px0, py0 := cx, cy
			for i := 1; i <= steps; i++ {
				t := float64(i) / float64(steps)
				mt := 1 - t
				nx := mt*mt*cx + 2*mt*t*qx + t*t*ex
				ny := mt*mt*cy + 2*mt*t*qy + t*t*ey
				r.line(px0, py0, nx, ny)
				px0, py0 = nx, ny
			}
			cx, cy = ex, ey
		}
		for i := 1; i <= n; i++ {
			p := c[(start+i)%n]
			x, y := tx(p)
			if p.on {
				if haveCtl {
					quad(x, y)
					haveCtl = false
				} else {
					r.line(cx, cy, x, y)
					cx, cy = x, y
				}
			} else {
				if haveCtl {
					mx, my := (qx+x)/2, (qy+y)/2
					quad(mx, my)
				}
				qx, qy = x, y
				haveCtl = true
			}
		}
		if haveCtl {
			quad(sx, sy)
		} else if cx != sx || cy != sy {
			r.line(cx, cy, sx, sy)
		}
	}
	g.cov = make([]float32, g.w*g.h)
	var acc float32
	for i := range g.cov {
		acc += r.a[i]
		v := acc
		if v < 0 {
			v = -v
		}
		if v > 1 {
			v = 1
		}
		g.cov[i] = v
	}
	f.cache[k] = g
	return g
}

// ---------------------------------------------------------------- TextBackend для тестов
type ttfText struct {
	fonts map[string]*ttFont
}

var fontFiles = map[string]string{
	"serif": "liberation/LiberationSerif", "sans": "liberation/LiberationSans", "mono": "liberation/LiberationMono",
	"calibri": "crosextra/Carlito", "cambria": "crosextra/Caladea",
}

func newTTFText() *ttfText { return &ttfText{fonts: map[string]*ttFont{}} }

// ttfPath — файл шрифта, которым на Linux подменяется семейство (метрически совместимые Liberation, Carlito…).
func ttfPath(family string, bold, italic bool) string {
	fam := strings.ToLower(family)
	if strings.Contains(fam, "dejavu") {
		return "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
	}
	base := "serif"
	switch {
	case strings.Contains(fam, "calibri"), strings.Contains(fam, "segoe"), strings.Contains(fam, "corbel"), strings.Contains(fam, "candara"):
		base = "calibri"
	case strings.Contains(fam, "cambria math"), strings.Contains(fam, "constantia"):
		base = "serif" // в Caladea нет математических знаков
	case strings.Contains(fam, "cambria"):
		base = "cambria" // Caladea — метрический двойник Cambria (так же подменяет LibreOffice)
	case strings.Contains(fam, "courier"), strings.Contains(fam, "consolas"), strings.Contains(fam, "mono"), strings.Contains(fam, "lucida console"):
		base = "mono"
	case strings.Contains(fam, "arial"), strings.Contains(fam, "verdana"), strings.Contains(fam, "tahoma"), strings.Contains(fam, "helvetica"),
		strings.Contains(fam, "trebuchet"), strings.Contains(fam, "century gothic"), strings.Contains(fam, "franklin"), strings.Contains(fam, "impact"),
		strings.Contains(fam, "sans"):
		base = "sans"
	}
	style := "Regular"
	switch {
	case bold && italic:
		style = "BoldItalic"
	case bold:
		style = "Bold"
	case italic:
		style = "Italic"
	}
	return "/usr/share/fonts/truetype/" + fontFiles[base] + "-" + style + ".ttf"
}

func (t *ttfText) font(f Font) *ttFont {
	key := ttfPath(f.Family, f.Bold, f.Italic)
	if ft, ok := t.fonts[key]; ok {
		return ft
	}
	ft := loadTTF(key)
	if ft == nil {
		ft = loadTTF("/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf")
	}
	t.fonts[key] = ft
	return ft
}

// systemFont — файл шрифта для вшивания в PDF (тот же, что при вёрстке, поэтому ширины совпадают).
func systemFont(family string, bold, italic bool) (path string, index int, synB, synI, ok bool) {
	p := ttfPath(family, bold, italic)
	if _, err := os.Stat(p); err != nil {
		return "", 0, false, false, false
	}
	return p, 0, false, false, true
}

func isIconFont(f Font) bool {
	return strings.HasPrefix(f.Family, "Segoe Fluent") || strings.HasPrefix(f.Family, "Segoe MDL2")
}

func (t *ttfText) Measure(s string, f Font) float64 {
	if isIconFont(f) {
		return float64(len([]rune(s))) * f.Px
	}
	ft := t.font(f)
	w := 0.0
	for _, r := range s {
		if r == '\t' {
			r = ' '
		}
		w += ft.advance(ft.cmap(r))
	}
	return w * f.Px / ft.upem
}

func (t *ttfText) Metrics(f Font) (float64, float64) {
	if isIconFont(f) {
		return f.Px * 0.9, f.Px * 0.1
	}
	ft := t.font(f)
	return math.Round(ft.asc * f.Px / ft.upem), math.Round(ft.desc * f.Px / ft.upem)
}

// Leading — внешний интерлиньяж шрифта (см. sfntLineMetrics).
func (t *ttfText) Leading(f Font) float64 {
	if isIconFont(f) {
		return 0
	}
	ft := t.font(f)
	return ft.gap * f.Px / ft.upem
}

func (t *ttfText) Draw(cv *Canvas, s string, x, base float64, f Font, c Color) {
	if isIconFont(f) {
		// значков Fluent здесь нет — рисуем аккуратный кружок-заменитель
		for i := range []rune(s) {
			cx := x + f.Px*(float64(i)+0.5)
			cv.StrokeRound(Rect{cx - f.Px*0.38, base - f.Px*0.85, f.Px * 0.76, f.Px * 0.76}, f.Px*0.2, math.Max(1, f.Px/12), c)
		}
		return
	}
	cv.sync()
	ft := t.font(f)
	sc := f.Px / ft.upem
	pen := x
	for _, r := range s {
		gid := ft.cmap(r)
		if r != ' ' && r != '\t' {
			g := ft.glyph(gid, f.Px)
			ox, oy := int(math.Round(pen))+g.x0, int(math.Round(base))+g.y0
			for yy := 0; yy < g.h; yy++ {
				py := oy + yy
				if py < 0 || py >= cv.H || float64(py) < cv.clip.Y || float64(py) >= cv.clip.Y+cv.clip.H {
					continue
				}
				for xx := 0; xx < g.w; xx++ {
					cov := g.cov[yy*g.w+xx]
					if cov <= 0.003 {
						continue
					}
					px := ox + xx
					if px < 0 || px >= cv.W || float64(px) < cv.clip.X || float64(px) >= cv.clip.X+cv.clip.W {
						continue
					}
					cv.blend(py*cv.W+px, c, float64(cov))
				}
			}
		}
		pen += ft.advance(gid) * sc
	}
	if f.Underline {
		cv.FillRect(Rect{x, base + math.Max(1, f.Px*0.08), pen - x, math.Max(1, f.Px/16)}, c)
	}
}
