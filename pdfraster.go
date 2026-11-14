package main

// Читалка PDF, часть 3: растеризатор (сглаженная заливка, обводка, обрезка, картинки, градиенты).

import (
	"image"
	"math"
	"sort"
)

type pdfMat [6]float64

var identMat = pdfMat{1, 0, 0, 1, 0, 0}

// mul: сначала m, потом n.
func (m pdfMat) mul(n pdfMat) pdfMat {
	return pdfMat{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func (m pdfMat) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func (m pdfMat) inv() (pdfMat, bool) {
	det := m[0]*m[3] - m[1]*m[2]
	if math.Abs(det) < 1e-12 {
		return identMat, false
	}
	a, b, c, d := m[3]/det, -m[1]/det, -m[2]/det, m[0]/det
	return pdfMat{a, b, c, d, -(m[4]*a + m[5]*c), -(m[4]*b + m[5]*d)}, true
}

func (m pdfMat) scale() float64 { return math.Sqrt(math.Abs(m[0]*m[3] - m[1]*m[2])) }

// ---------------------------------------------------------------- контуры в пикселях
type rpath struct {
	subs [][]float64 // ломаные (x,y,…), каждая — отдельный замкнутый/незамкнутый контур
	cl   []bool
}

type pathBuilder struct {
	p      rpath
	cur    []float64
	sx, sy float64
	x, y   float64
	has    bool
	tol    float64
}

func (b *pathBuilder) flush(closed bool) {
	if len(b.cur) >= 4 {
		b.p.subs = append(b.p.subs, b.cur)
		b.p.cl = append(b.p.cl, closed)
	}
	b.cur = nil
}

func (b *pathBuilder) moveTo(x, y float64) {
	b.flush(false)
	b.cur = []float64{x, y}
	b.sx, b.sy, b.x, b.y, b.has = x, y, x, y, true
}

func (b *pathBuilder) lineTo(x, y float64) {
	if !b.has {
		b.moveTo(x, y)
		return
	}
	b.cur = append(b.cur, x, y)
	b.x, b.y = x, y
}

func (b *pathBuilder) curveTo(x1, y1, x2, y2, x3, y3 float64) {
	if !b.has {
		b.moveTo(x1, y1)
	}
	x0, y0 := b.x, b.y
	l := math.Hypot(x1-x0, y1-y0) + math.Hypot(x2-x1, y2-y1) + math.Hypot(x3-x2, y3-y2)
	n := int(math.Ceil(math.Sqrt(l/math.Max(b.tol, 0.05)) * 1.2))
	n = max(2, min(n, 100))
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		u := 1 - t
		a, bb, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		b.cur = append(b.cur, a*x0+bb*x1+c*x2+d*x3, a*y0+bb*y1+c*y2+d*y3)
	}
	b.x, b.y = x3, y3
}

func (b *pathBuilder) quadTo(x1, y1, x2, y2 float64) {
	x0, y0 := b.x, b.y
	b.curveTo(x0+2.0/3*(x1-x0), y0+2.0/3*(y1-y0), x2+2.0/3*(x1-x2), y2+2.0/3*(y1-y2), x2, y2)
}

func (b *pathBuilder) closePath() {
	if b.has && len(b.cur) >= 2 {
		b.cur = append(b.cur, b.sx, b.sy)
		b.flush(true)
		b.cur = []float64{b.sx, b.sy}
		b.x, b.y = b.sx, b.sy
	}
}

func (b *pathBuilder) done() rpath {
	b.flush(false)
	return b.p
}

// ---------------------------------------------------------------- маска покрытия
type covMask struct {
	x0, y0, w, h int
	a            []float32
}

type edge struct {
	x0, y0, x1, y1 float64
	dir            int
}

// rasterize — покрытие контура (ломаные в пикселях) с правилом nonzero/even-odd, в пределах [0,W)×[0,H).
func rasterize(p rpath, W, H int, evenOdd bool) *covMask {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var edges []edge
	for _, s := range p.subs {
		n := len(s) / 2
		if n < 2 {
			continue
		}
		for i := 0; i < n; i++ {
			x0, y0 := s[2*i], s[2*i+1]
			j := (i + 1) % n
			x1, y1 := s[2*j], s[2*j+1]
			if i == n-1 && x0 == s[0] && y0 == s[1] {
				continue
			}
			minX, maxX = math.Min(minX, x0), math.Max(maxX, x0)
			minY, maxY = math.Min(minY, y0), math.Max(maxY, y0)
			if y0 == y1 {
				continue
			}
			if math.IsNaN(x0) || math.IsNaN(x1) || math.IsInf(x0, 0) || math.IsInf(y1, 0) {
				continue
			}
			if y0 < y1 {
				edges = append(edges, edge{x0, y0, x1, y1, 1})
			} else {
				edges = append(edges, edge{x1, y1, x0, y0, -1})
			}
		}
	}
	if len(edges) == 0 {
		return nil
	}
	ix0 := max(0, int(math.Floor(minX)))
	iy0 := max(0, int(math.Floor(minY)))
	ix1 := min(W, int(math.Ceil(maxX))+1)
	iy1 := min(H, int(math.Ceil(maxY))+1)
	if ix1 <= ix0 || iy1 <= iy0 {
		return nil
	}
	m := &covMask{x0: ix0, y0: iy0, w: ix1 - ix0, h: iy1 - iy0}
	m.a = make([]float32, m.w*m.h)
	sort.Slice(edges, func(i, j int) bool { return edges[i].y0 < edges[j].y0 })
	S := 4
	if maxY-minY < 48 {
		S = 6
	}
	inv := 1 / float32(S)
	type xing struct {
		x   float64
		dir int
	}
	xs := make([]xing, 0, 64)
	active := make([]int, 0, 64)
	next := 0
	row := make([]float32, m.w+2)
	for y := iy0; y < iy1; y++ {
		for i := range row {
			row[i] = 0
		}
		any := false
		for s := 0; s < S; s++ {
			yy := float64(y) + (float64(s)+0.5)/float64(S)
			for next < len(edges) && edges[next].y0 <= yy {
				active = append(active, next)
				next++
			}
			xs = xs[:0]
			k := 0
			for _, ei := range active {
				e := &edges[ei]
				if e.y1 <= yy {
					continue
				}
				active[k] = ei
				k++
				if e.y0 > yy {
					continue
				}
				t := (yy - e.y0) / (e.y1 - e.y0)
				xs = append(xs, xing{e.x0 + t*(e.x1-e.x0), e.dir})
			}
			active = active[:k]
			if len(xs) < 2 {
				continue
			}
			sort.Slice(xs, func(i, j int) bool { return xs[i].x < xs[j].x })
			wind := 0
			for i := 0; i < len(xs)-1; i++ {
				if evenOdd {
					wind ^= 1
				} else {
					wind += xs[i].dir
				}
				if wind == 0 {
					continue
				}
				a, b := xs[i].x-float64(ix0), xs[i+1].x-float64(ix0)
				if b <= a {
					continue
				}
				a = math.Max(a, 0)
				b = math.Min(b, float64(m.w))
				if b <= a {
					continue
				}
				any = true
				ia, ib := int(a), int(b)
				if ia == ib {
					row[ia] += float32(b-a) * inv
					continue
				}
				row[ia] += float32(float64(ia+1)-a) * inv
				for q := ia + 1; q < ib; q++ {
					row[q] += inv
				}
				if ib < m.w {
					row[ib] += float32(b-float64(ib)) * inv
				}
			}
		}
		if any {
			o := (y - iy0) * m.w
			for i := 0; i < m.w; i++ {
				v := row[i]
				if v > 1 {
					v = 1
				}
				m.a[o+i] = v
			}
		}
		if next >= len(edges) && len(active) == 0 {
			break
		}
	}
	return m
}

// ---------------------------------------------------------------- обводка
func orientCCW(pts []float64) []float64 {
	area := 0.0
	n := len(pts) / 2
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		area += pts[2*i]*pts[2*j+1] - pts[2*j]*pts[2*i+1]
	}
	if area < 0 {
		out := make([]float64, len(pts))
		for i := 0; i < n; i++ {
			out[2*i], out[2*i+1] = pts[2*(n-1-i)], pts[2*(n-1-i)+1]
		}
		return out
	}
	return pts
}

func disc(cx, cy, r float64) []float64 {
	n := max(8, min(32, int(r*2)))
	pts := make([]float64, 0, 2*n)
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		pts = append(pts, cx+r*math.Cos(a), cy+r*math.Sin(a))
	}
	return pts
}

func dashPolyline(s []float64, dash []float64, phase float64) [][]float64 {
	total := 0.0
	for _, d := range dash {
		total += d
	}
	if total <= 0 || len(dash) == 0 {
		return [][]float64{s}
	}
	var out [][]float64
	di := 0
	rem := dash[0]
	ph := math.Mod(phase, total)
	for ph > 0 {
		if ph >= rem {
			ph -= rem
			di = (di + 1) % len(dash)
			rem = dash[di]
		} else {
			rem -= ph
			ph = 0
		}
	}
	on := di%2 == 0
	var cur []float64
	if on {
		cur = []float64{s[0], s[1]}
	}
	for i := 0; i+3 < len(s); i += 2 {
		x0, y0, x1, y1 := s[i], s[i+1], s[i+2], s[i+3]
		l := math.Hypot(x1-x0, y1-y0)
		pos := 0.0
		for pos < l {
			step := math.Min(rem, l-pos)
			pos += step
			rem -= step
			t := pos / math.Max(l, 1e-9)
			px, py := x0+(x1-x0)*t, y0+(y1-y0)*t
			if on {
				cur = append(cur, px, py)
			}
			if rem <= 1e-9 {
				if on && len(cur) >= 4 {
					out = append(out, cur)
				}
				on = !on
				di = (di + 1) % len(dash)
				rem = dash[di]
				if on {
					cur = []float64{px, py}
				} else {
					cur = nil
				}
			}
			if len(out) > 20000 {
				return out
			}
		}
	}
	if on && len(cur) >= 4 {
		out = append(out, cur)
	}
	return out
}

// strokePath превращает обводку в замкнутые многоугольники (одинаково ориентированные).
func strokePath(p rpath, w float64, cap, join int, dash []float64, phase float64) rpath {
	var out rpath
	hw := w / 2
	add := func(poly []float64) {
		out.subs = append(out.subs, orientCCW(poly))
		out.cl = append(out.cl, true)
	}
	for si, s0 := range p.subs {
		lines := [][]float64{s0}
		if len(dash) > 0 {
			lines = dashPolyline(s0, dash, phase)
		}
		closed := p.cl[si] && len(dash) == 0
		for _, s := range lines {
			n := len(s) / 2
			if n < 2 {
				continue
			}
			for i := 0; i < n-1; i++ {
				x0, y0, x1, y1 := s[2*i], s[2*i+1], s[2*i+2], s[2*i+3]
				dx, dy := x1-x0, y1-y0
				l := math.Hypot(dx, dy)
				if l < 1e-9 {
					continue
				}
				nx, ny := -dy/l*hw, dx/l*hw
				ex, ey := 0.0, 0.0
				if cap == 2 {
					ex, ey = dx/l*hw, dy/l*hw
				}
				sx0, sy0, sx1, sy1 := x0, y0, x1, y1
				if cap == 2 && !closed {
					if i == 0 {
						sx0, sy0 = x0-ex, y0-ey
					}
					if i == n-2 {
						sx1, sy1 = x1+ex, y1+ey
					}
				}
				add([]float64{sx0 + nx, sy0 + ny, sx1 + nx, sy1 + ny, sx1 - nx, sy1 - ny, sx0 - nx, sy0 - ny})
				// стыки
				if w >= 1.5 && (i < n-2 || closed) {
					if join == 1 || join == 0 {
						add(disc(x1, y1, hw))
					} else {
						add(disc(x1, y1, hw*0.9))
					}
				}
			}
			if cap == 1 && !closed && w >= 1.5 {
				add(disc(s[0], s[1], hw))
				add(disc(s[2*n-2], s[2*n-1], hw))
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- холст страницы
type pdfCanvas struct {
	w, h int
	pix  []uint8 // RGB
}

func newPdfCanvas(w, h int) *pdfCanvas {
	c := &pdfCanvas{w: w, h: h, pix: make([]uint8, w*h*3)}
	for i := range c.pix {
		c.pix[i] = 255
	}
	return c
}

func (c *pdfCanvas) toRGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	for i, j := 0, 0; i < len(c.pix); i, j = i+3, j+4 {
		img.Pix[j], img.Pix[j+1], img.Pix[j+2], img.Pix[j+3] = c.pix[i], c.pix[i+1], c.pix[i+2], 255
	}
	return img
}

// clip — область обрезки: прямоугольник и (необязательно) маска.
type pdfClip struct {
	x0, y0, x1, y1 float64
	mask           *covMask
}

func (cl *pdfClip) at(x, y int) float32 {
	if float64(x) < cl.x0-1 || float64(y) < cl.y0-1 || float64(x) >= cl.x1+1 || float64(y) >= cl.y1+1 {
		return 0
	}
	if cl.mask == nil {
		return 1
	}
	m := cl.mask
	if x < m.x0 || y < m.y0 || x >= m.x0+m.w || y >= m.y0+m.h {
		return 0
	}
	return m.a[(y-m.y0)*m.w+x-m.x0]
}

// fillMask закрашивает цветом с учётом покрытия, прозрачности и обрезки.
func (c *pdfCanvas) fillMask(m *covMask, r, g, b uint8, alpha float32, cl *pdfClip) {
	if m == nil {
		return
	}
	for y := 0; y < m.h; y++ {
		py := m.y0 + y
		if py < 0 || py >= c.h {
			continue
		}
		o := y * m.w
		for x := 0; x < m.w; x++ {
			a := m.a[o+x]
			if a <= 0.002 {
				continue
			}
			px := m.x0 + x
			if px < 0 || px >= c.w {
				continue
			}
			if cl != nil {
				a *= cl.at(px, py)
				if a <= 0.002 {
					continue
				}
			}
			a *= alpha
			i := (py*c.w + px) * 3
			if a >= 0.999 {
				c.pix[i], c.pix[i+1], c.pix[i+2] = r, g, b
			} else {
				ia := 1 - a
				c.pix[i] = uint8(float32(c.pix[i])*ia + float32(r)*a + 0.5)
				c.pix[i+1] = uint8(float32(c.pix[i+1])*ia + float32(g)*a + 0.5)
				c.pix[i+2] = uint8(float32(c.pix[i+2])*ia + float32(b)*a + 0.5)
			}
		}
	}
}

// intersectClip — новая обрезка = старая ∩ маска контура.
func intersectClip(old *pdfClip, m *covMask, W, H int) *pdfClip {
	nc := &pdfClip{x0: 0, y0: 0, x1: float64(W), y1: float64(H)}
	if old != nil {
		*nc = *old
	}
	if m == nil {
		nc.x0, nc.y0, nc.x1, nc.y1 = 0, 0, 0, 0
		nc.mask = nil
		return nc
	}
	nc.x0 = math.Max(nc.x0, float64(m.x0))
	nc.y0 = math.Max(nc.y0, float64(m.y0))
	nc.x1 = math.Min(nc.x1, float64(m.x0+m.w))
	nc.y1 = math.Min(nc.y1, float64(m.y0+m.h))
	nm := &covMask{x0: m.x0, y0: m.y0, w: m.w, h: m.h, a: make([]float32, len(m.a))}
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			v := m.a[y*m.w+x]
			if v > 0 && old != nil {
				v *= old.at(m.x0+x, m.y0+y)
			}
			nm.a[y*m.w+x] = v
		}
	}
	nc.mask = nm
	return nc
}

// isRect — путь — прямоугольник по осям (быстрая обрезка).
func isRect(p rpath) (float64, float64, float64, float64, bool) {
	if len(p.subs) != 1 {
		return 0, 0, 0, 0, false
	}
	s := p.subs[0]
	n := len(s) / 2
	if n < 4 || n > 5 {
		return 0, 0, 0, 0, false
	}
	xs := map[float64]bool{}
	ys := map[float64]bool{}
	for i := 0; i < n; i++ {
		xs[math.Round(s[2*i]*100)/100] = true
		ys[math.Round(s[2*i+1]*100)/100] = true
	}
	if len(xs) != 2 || len(ys) != 2 {
		return 0, 0, 0, 0, false
	}
	for i := 0; i < n-1; i++ {
		if s[2*i] != s[2*i+2] && s[2*i+1] != s[2*i+3] {
			return 0, 0, 0, 0, false
		}
	}
	x0, x1, y0, y1 := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for i := 0; i < n; i++ {
		x0, x1 = math.Min(x0, s[2*i]), math.Max(x1, s[2*i])
		y0, y1 = math.Min(y0, s[2*i+1]), math.Max(y1, s[2*i+1])
	}
	return x0, y0, x1, y1, true
}

// ---------------------------------------------------------------- картинки
type pdfImage struct {
	w, h int
	rgb  []uint8 // RGB
	a    []uint8 // альфа (nil — непрозрачно)
	mask bool    // маска-трафарет: рисуется цветом заливки
	mips map[int]*pdfImage
}

func (im *pdfImage) downscaled(f int) *pdfImage {
	if f <= 1 {
		return im
	}
	if im.mips == nil {
		im.mips = map[int]*pdfImage{}
	}
	if m, ok := im.mips[f]; ok {
		return m
	}
	nw, nh := max(1, im.w/f), max(1, im.h/f)
	out := &pdfImage{w: nw, h: nh, rgb: make([]uint8, nw*nh*3), mask: im.mask}
	if im.a != nil {
		out.a = make([]uint8, nw*nh)
	}
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			var r, g, b, a, n int
			for dy := 0; dy < f; dy++ {
				sy := y*f + dy
				if sy >= im.h {
					break
				}
				for dx := 0; dx < f; dx++ {
					sx := x*f + dx
					if sx >= im.w {
						break
					}
					i := sy*im.w + sx
					r += int(im.rgb[3*i])
					g += int(im.rgb[3*i+1])
					b += int(im.rgb[3*i+2])
					if im.a != nil {
						a += int(im.a[i])
					}
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			o := y*nw + x
			out.rgb[3*o], out.rgb[3*o+1], out.rgb[3*o+2] = uint8(r/n), uint8(g/n), uint8(b/n)
			if out.a != nil {
				out.a[o] = uint8(a / n)
			}
		}
	}
	im.mips[f] = out
	return out
}

// drawImage рисует картинку в единичный квадрат, отображённый матрицей m.
func (c *pdfCanvas) drawImage(im *pdfImage, m pdfMat, cl *pdfClip, fill [3]uint8, alpha float32) {
	if im == nil || im.w == 0 || im.h == 0 {
		return
	}
	// углы
	xs := [4]float64{}
	ys := [4]float64{}
	for i, p := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		xs[i], ys[i] = m.apply(p[0], p[1])
	}
	x0, x1 := math.Min(math.Min(xs[0], xs[1]), math.Min(xs[2], xs[3])), math.Max(math.Max(xs[0], xs[1]), math.Max(xs[2], xs[3]))
	y0, y1 := math.Min(math.Min(ys[0], ys[1]), math.Min(ys[2], ys[3])), math.Max(math.Max(ys[0], ys[1]), math.Max(ys[2], ys[3]))
	ix0, iy0 := max(0, int(math.Floor(x0))), max(0, int(math.Floor(y0)))
	ix1, iy1 := min(c.w, int(math.Ceil(x1))), min(c.h, int(math.Ceil(y1)))
	if cl != nil {
		ix0, iy0 = max(ix0, int(cl.x0)), max(iy0, int(cl.y0))
		ix1, iy1 = min(ix1, int(math.Ceil(cl.x1))), min(iy1, int(math.Ceil(cl.y1)))
	}
	if ix1 <= ix0 || iy1 <= iy0 {
		return
	}
	// уменьшенная копия, если картинка гораздо крупнее места
	pxW := math.Hypot(xs[1]-xs[0], ys[1]-ys[0])
	pxH := math.Hypot(xs[2]-xs[0], ys[2]-ys[0])
	f := int(math.Min(float64(im.w)/math.Max(pxW, 1), float64(im.h)/math.Max(pxH, 1)))
	src := im
	if f >= 2 && !im.mask {
		src = im.downscaled(f)
	}
	inv, ok := m.inv()
	if !ok {
		return
	}
	smooth := float64(src.w) < pxW*0.9 && !im.mask
	for y := iy0; y < iy1; y++ {
		for x := ix0; x < ix1; x++ {
			u, v := inv.apply(float64(x)+0.5, float64(y)+0.5)
			if u < 0 || u >= 1 || v < 0 || v >= 1 {
				continue
			}
			a := alpha
			if cl != nil {
				a *= cl.at(x, y)
				if a <= 0.002 {
					continue
				}
			}
			fx := u*float64(src.w) - 0.5
			fy := (1-v)*float64(src.h) - 0.5
			var r, g, b, sa float32
			if smooth {
				xa, ya := int(math.Floor(fx)), int(math.Floor(fy))
				tx, ty := float32(fx-float64(xa)), float32(fy-float64(ya))
				for k := 0; k < 4; k++ {
					sx, sy := xa+k%2, ya+k/2
					sx = max(0, min(src.w-1, sx))
					sy = max(0, min(src.h-1, sy))
					wgt := (1 - tx)
					if k%2 == 1 {
						wgt = tx
					}
					if k/2 == 1 {
						wgt *= ty
					} else {
						wgt *= 1 - ty
					}
					i := sy*src.w + sx
					r += float32(src.rgb[3*i]) * wgt
					g += float32(src.rgb[3*i+1]) * wgt
					b += float32(src.rgb[3*i+2]) * wgt
					if src.a != nil {
						sa += float32(src.a[i]) * wgt
					} else {
						sa += 255 * wgt
					}
				}
			} else {
				sx := max(0, min(src.w-1, int(fx+0.5)))
				sy := max(0, min(src.h-1, int(fy+0.5)))
				i := sy*src.w + sx
				r, g, b = float32(src.rgb[3*i]), float32(src.rgb[3*i+1]), float32(src.rgb[3*i+2])
				sa = 255
				if src.a != nil {
					sa = float32(src.a[i])
				}
			}
			if im.mask {
				// трафарет: rgb хранит 0 (рисовать) / 255 (прозрачно)
				a *= 1 - r/255
				r, g, b = float32(fill[0]), float32(fill[1]), float32(fill[2])
			} else {
				a *= sa / 255
			}
			if a <= 0.002 {
				continue
			}
			i := (y*c.w + x) * 3
			ia := 1 - a
			c.pix[i] = uint8(float32(c.pix[i])*ia + r*a + 0.5)
			c.pix[i+1] = uint8(float32(c.pix[i+1])*ia + g*a + 0.5)
			c.pix[i+2] = uint8(float32(c.pix[i+2])*ia + b*a + 0.5)
		}
	}
}
