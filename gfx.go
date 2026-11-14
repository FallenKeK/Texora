package main

// Собственный растеризатор: сглаженные прямоугольники, скругления, тени.
// Пиксели в формате 0xAARRGGBB (так их и хранит 32-битный DIB в Windows).

import (
	"math"
	"sort"
)

type Color uint32

func RGB(r, g, b uint8) Color { return Color(0xff000000 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)) }
func RGBA(r, g, b, a uint8) Color {
	return Color(uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b))
}
func Hex(v uint32) Color   { return Color(0xff000000 | v) }
func (c Color) A() float64 { return float64(c>>24) / 255 }
func (c Color) WithA(a float64) Color {
	return Color(uint32(clamp01(a)*255+0.5)<<24 | uint32(c)&0xffffff)
}

type Canvas struct {
	W, H int
	Pix  []uint32
	clip Rect
	Sync func() // вызывается перед записью пикселей (чтобы системный вывод текста успел дорисовать)
	// Target — системный дескриптор поверхности (HBITMAP в Windows), куда выводит текст платформа.
	Target  uintptr
	Release func()
}

func (cv *Canvas) sync() {
	if cv.Sync != nil {
		cv.Sync()
	}
}

type Rect struct{ X, Y, W, H float64 }

func (r Rect) Contains(x, y float64) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }
func (r Rect) Inset(d float64) Rect       { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }

func clamp01(v float64) float64 {
	// без вызовов math — это самое горячее место отрисовки
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (cv *Canvas) SetClip(r Rect) { cv.clip = r }
func (cv *Canvas) ResetClip()     { cv.clip = Rect{0, 0, float64(cv.W), float64(cv.H)} }

func (cv *Canvas) blend(i int, c Color, cov float64) {
	a := uint32(float64(c>>24)*cov + 0.5) // 0..255
	if a == 0 {
		return
	}
	cv.Pix[i] = blendPix(cv.Pix[i], uint32(c), a)
}

// blendPix — целочисленное смешивание (a 0..255).
func blendPix(d, c, a uint32) uint32 {
	if a >= 255 {
		return c | 0xff000000
	}
	ia := 255 - a
	rb := ((c&0xff00ff)*a + (d&0xff00ff)*ia + 0x800080) >> 8 & 0xff00ff
	g := ((c&0xff00)*a + (d&0xff00)*ia + 0x8000) >> 8 & 0xff00
	return 0xff000000 | rb | g
}

// span — закрасить отрезок строки [x0, x1) с покрытием cov.
func (cv *Canvas) span(row, x0, x1 int, c Color, cov float64) {
	a := uint32(float64(c>>24)*cov + 0.5)
	if a == 0 || x1 <= x0 {
		return
	}
	px := cv.Pix[row+x0 : row+x1]
	if a >= 255 {
		v := uint32(c) | 0xff000000
		for i := range px {
			px[i] = v
		}
		return
	}
	cc := uint32(c)
	for i, d := range px {
		px[i] = blendPix(d, cc, a)
	}
}

func (cv *Canvas) Clear(c Color) {
	cv.sync()
	v := uint32(c) | 0xff000000
	for i := range cv.Pix {
		cv.Pix[i] = v
	}
}

// bounds пересекает прямоугольник с областью отсечения и возвращает целочисленные границы.
func (cv *Canvas) bounds(x0, y0, x1, y1 float64) (int, int, int, int) {
	cx0, cy0 := cv.clip.X, cv.clip.Y
	cx1, cy1 := cv.clip.X+cv.clip.W, cv.clip.Y+cv.clip.H
	ix0 := int(math.Floor(math.Max(x0, cx0)))
	iy0 := int(math.Floor(math.Max(y0, cy0)))
	ix1 := int(math.Ceil(math.Min(x1, cx1)))
	iy1 := int(math.Ceil(math.Min(y1, cy1)))
	if ix0 < 0 {
		ix0 = 0
	}
	if iy0 < 0 {
		iy0 = 0
	}
	if ix1 > cv.W {
		ix1 = cv.W
	}
	if iy1 > cv.H {
		iy1 = cv.H
	}
	return ix0, iy0, ix1, iy1
}

// FillRect — прямоугольник со сглаженными дробными краями.
func (cv *Canvas) FillRect(r Rect, c Color) {
	cv.sync()
	x0, y0, x1, y1 := cv.bounds(r.X, r.Y, r.X+r.W, r.Y+r.H)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	// целиком покрытые столбцы [fx0, fx1)
	fx0 := max(x0, int(math.Ceil(r.X)))
	fx1 := min(x1, int(math.Floor(r.X+r.W)))
	for y := y0; y < y1; y++ {
		cy := math.Min(float64(y+1), r.Y+r.H) - math.Max(float64(y), r.Y)
		if cy <= 0 {
			continue
		}
		cy = clamp01(cy)
		row := y * cv.W
		if fx1 > fx0 {
			for x := x0; x < fx0; x++ {
				cx := math.Min(float64(x+1), r.X+r.W) - math.Max(float64(x), r.X)
				if cx > 0 {
					cv.blend(row+x, c, clamp01(cx)*cy)
				}
			}
			cv.span(row, fx0, fx1, c, cy)
			for x := fx1; x < x1; x++ {
				cx := math.Min(float64(x+1), r.X+r.W) - math.Max(float64(x), r.X)
				if cx > 0 {
					cv.blend(row+x, c, clamp01(cx)*cy)
				}
			}
			continue
		}
		for x := x0; x < x1; x++ {
			cx := math.Min(float64(x+1), r.X+r.W) - math.Max(float64(x), r.X)
			if cx > 0 {
				cv.blend(row+x, c, clamp01(cx)*cy)
			}
		}
	}
}

// sdRound — расстояние со знаком до скруглённого прямоугольника (внутри < 0).
func sdRound(px, py float64, r Rect, rad float64) float64 {
	dx := px - (r.X + r.W/2)
	if dx < 0 {
		dx = -dx
	}
	dy := py - (r.Y + r.H/2)
	if dy < 0 {
		dy = -dy
	}
	dx -= r.W/2 - rad
	dy -= r.H/2 - rad
	switch {
	case dx > 0 && dy > 0:
		return math.Sqrt(dx*dx+dy*dy) - rad
	case dx > 0:
		return dx - rad
	case dy > 0:
		return dy - rad
	case dx > dy:
		return dx - rad
	}
	return dy - rad
}

func (cv *Canvas) FillRound(r Rect, rad float64, c Color) {
	cv.sync()
	if rad <= 0.01 {
		cv.FillRect(r, c)
		return
	}
	rad = math.Min(rad, math.Min(r.W, r.H)/2)
	x0, y0, x1, y1 := cv.bounds(r.X-1, r.Y-1, r.X+r.W+1, r.Y+r.H+1)
	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		row := y * cv.W
		inner := py > r.Y+rad+1 && py < r.Y+r.H-rad-1
		for x := x0; x < x1; x++ {
			px := float64(x) + 0.5
			if inner && px > r.X+1 && px < r.X+r.W-1 {
				// сплошной отрезок до правого края
				end := min(x1, int(math.Ceil(r.X+r.W-1.5)))
				if end > x {
					cv.span(row, x, end, c, 1)
					x = end - 1
					continue
				}
				cv.blend(row+x, c, 1)
				continue
			}
			cov := clamp01(0.5 - sdRound(px, py, r, rad))
			if cov > 0 {
				cv.blend(row+x, c, cov)
			}
		}
	}
}

func (cv *Canvas) StrokeRound(r Rect, rad, w float64, c Color) {
	cv.sync()
	rad = math.Min(rad, math.Min(r.W, r.H)/2)
	x0, y0, x1, y1 := cv.bounds(r.X-w-1, r.Y-w-1, r.X+r.W+w+1, r.Y+r.H+w+1)
	ir := Rect{r.X + w/2, r.Y + w/2, r.W - w, r.H - w}
	irad := math.Max(rad-w/2, 0.01)
	// внутренность (дальше рамки) пропускаем
	in := r.Inset(math.Max(rad, w) + w + 2)
	ix0, ix1 := int(math.Ceil(in.X)), int(math.Floor(in.X+in.W))
	iy0, iy1 := int(math.Ceil(in.Y)), int(math.Floor(in.Y+in.H))
	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		row := y * cv.W
		for x := x0; x < x1; x++ {
			if y >= iy0 && y < iy1 && x >= ix0 && x < ix1 {
				x = ix1 - 1
				continue
			}
			px := float64(x) + 0.5
			d := math.Abs(sdRound(px, py, ir, irad)) - w/2
			cov := clamp01(0.5 - d)
			if cov > 0 {
				cv.blend(row+x, c, cov)
			}
		}
	}
}

// Shadow — мягкая тень под скруглённым прямоугольником.
func (cv *Canvas) Shadow(r Rect, rad, blur float64, c Color) {
	cv.sync()
	x0, y0, x1, y1 := cv.bounds(r.X-blur*2, r.Y-blur*2, r.X+r.W+blur*2, r.Y+r.H+blur*2)
	// внутренность прямоугольника всегда закрыта тем, что рисуется поверх тени — пропускаем её
	in := r.Inset(rad + 2)
	ix0, ix1 := int(math.Ceil(in.X)), int(math.Floor(in.X+in.W))
	iy0, iy1 := int(math.Ceil(in.Y)), int(math.Floor(in.Y+in.H))
	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		row := y * cv.W
		for x := x0; x < x1; x++ {
			if y >= iy0 && y < iy1 && x >= ix0 && x < ix1 {
				x = ix1 - 1
				continue
			}
			px := float64(x) + 0.5
			d := sdRound(px, py, r, rad)
			t := clamp01((d + blur*0.3) / (blur * 2))
			a := (1 - t) * (1 - t) * (1 - t)
			if a > 0.002 {
				cv.blend(row+x, c, a)
			}
		}
	}
}

func (cv *Canvas) FillCircle(cx, cy, rad float64, c Color) {
	cv.FillRound(Rect{cx - rad, cy - rad, rad * 2, rad * 2}, rad, c)
}

// Line — отрезок заданной толщины со сглаживанием.
func (cv *Canvas) Line(xa, ya, xb, yb, w float64, c Color) {
	cv.sync()
	x0, y0, x1, y1 := cv.bounds(math.Min(xa, xb)-w, math.Min(ya, yb)-w, math.Max(xa, xb)+w, math.Max(ya, yb)+w)
	dx, dy := xb-xa, yb-ya
	l2 := dx*dx + dy*dy
	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		row := y * cv.W
		for x := x0; x < x1; x++ {
			px := float64(x) + 0.5
			t := 0.0
			if l2 > 0 {
				t = clamp01(((px-xa)*dx + (py-ya)*dy) / l2)
			}
			d := math.Hypot(px-(xa+t*dx), py-(ya+t*dy)) - w/2
			cov := clamp01(0.5 - d)
			if cov > 0 {
				cv.blend(row+x, c, cov)
			}
		}
	}
}

// Mix — смесь двух цветов (для плавных наведений).
func Mix(a, b Color, t float64) Color {
	t = clamp01(t)
	ch := func(s uint) uint32 {
		va, vb := float64((uint32(a)>>s)&0xff), float64((uint32(b)>>s)&0xff)
		return uint32(va+(vb-va)*t+0.5) << s
	}
	return Color(ch(24) | ch(16) | ch(8) | ch(0))
}

// BlurRound размывает то, что уже нарисовано внутри скруглённого прямоугольника (эффект «стекла»).
func (cv *Canvas) BlurRound(r Rect, rad float64, radius int) {
	cv.sync()
	x0, y0, x1, y1 := cv.bounds(r.X, r.Y, r.X+r.W, r.Y+r.H)
	w, h := x1-x0, y1-y0
	if w <= 2 || h <= 2 || radius < 1 {
		return
	}
	// работаем с окрестностью, чтобы края не темнели
	ex0, ey0 := max(0, x0-radius*2), max(0, y0-radius*2)
	ex1, ey1 := min(cv.W, x1+radius*2), min(cv.H, y1+radius*2)
	ew, eh := ex1-ex0, ey1-ey0
	ch := [3][]float32{make([]float32, ew*eh), make([]float32, ew*eh), make([]float32, ew*eh)}
	for y := 0; y < eh; y++ {
		for x := 0; x < ew; x++ {
			p := cv.Pix[(y+ey0)*cv.W+x+ex0]
			ch[0][y*ew+x], ch[1][y*ew+x], ch[2][y*ew+x] = float32(p>>16&0xff), float32(p>>8&0xff), float32(p&0xff)
		}
	}
	tmp := make([]float32, max(ew, eh))
	box := func(buf []float32, n, stride, start int) {
		// скользящее среднее по линии
		var sum float32
		cnt := 0
		for i := 0; i < n && i <= radius; i++ {
			sum += buf[start+i*stride]
			cnt++
		}
		for i := 0; i < n; i++ {
			tmp[i] = sum / float32(cnt)
			if j := i + radius + 1; j < n {
				sum += buf[start+j*stride]
				cnt++
			}
			if j := i - radius; j >= 0 {
				sum -= buf[start+j*stride]
				cnt--
			}
		}
		for i := 0; i < n; i++ {
			buf[start+i*stride] = tmp[i]
		}
	}
	for c := 0; c < 3; c++ {
		for pass := 0; pass < 2; pass++ {
			for y := 0; y < eh; y++ {
				box(ch[c], ew, 1, y*ew)
			}
			for x := 0; x < ew; x++ {
				box(ch[c], eh, ew, x)
			}
		}
	}
	for y := y0; y < y1; y++ {
		py := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			cov := clamp01(0.5 - sdRound(float64(x)+0.5, py, r, rad))
			if cov <= 0 {
				continue
			}
			k := (y-ey0)*ew + x - ex0
			b := uint32(ch[0][k]+0.5)<<16 | uint32(ch[1][k]+0.5)<<8 | uint32(ch[2][k]+0.5) | 0xff000000
			i := y*cv.W + x
			if cov >= 1 {
				cv.Pix[i] = b
			} else {
				cv.Pix[i] = uint32(Mix(Color(cv.Pix[i]), Color(b), cov))
			}
		}
	}
}

// FillPoly — сглаженная заливка многоугольника (4×4 выборки на пиксель).
func (cv *Canvas) FillPoly(pts [][2]float64, c Color) {
	cv.sync()
	if len(pts) < 3 {
		return
	}
	minX, minY, maxX, maxY := pts[0][0], pts[0][1], pts[0][0], pts[0][1]
	for _, p := range pts {
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	x0, y0, x1, y1 := cv.bounds(minX, minY, maxX+1, maxY+1)
	n := len(pts)
	xs := make([]float64, 0, 8)
	for y := y0; y < y1; y++ {
		var acc [4096]float32
		if x1-x0 > len(acc) {
			return
		}
		for s := 0; s < 4; s++ {
			sy := float64(y) + (float64(s)+0.5)/4
			xs = xs[:0]
			for i := 0; i < n; i++ {
				a, b := pts[i], pts[(i+1)%n]
				if (a[1] <= sy) != (b[1] <= sy) {
					xs = append(xs, a[0]+(sy-a[1])/(b[1]-a[1])*(b[0]-a[0]))
				}
			}
			sort.Float64s(xs)
			for k := 0; k+1 < len(xs); k += 2 {
				l, r := xs[k], xs[k+1]
				for x := max(x0, int(math.Floor(l))); x < min(x1, int(math.Ceil(r))); x++ {
					cl := math.Max(l, float64(x))
					cr := math.Min(r, float64(x+1))
					if cr > cl {
						acc[x-x0] += float32((cr - cl) / 4)
					}
				}
			}
		}
		for x := x0; x < x1; x++ {
			if a := float64(acc[x-x0]); a > 0 {
				cv.blend(y*cv.W+x, c, math.Min(1, a))
			}
		}
	}
}
