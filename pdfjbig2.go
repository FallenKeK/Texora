package main

// Декодер JBIG2 (ITU T.88) для фильтра JBIG2Decode: так сжимают чёрно-белые сканы книг.
// Поддержано: обобщённая область (арифметика и MMR), словари символов и текстовые области
// (арифметика и стандартные таблицы Хаффмана), уточнение символов. Нет: полутона, свои таблицы Хаффмана.

import (
	"encoding/binary"
	"sort"
)

// ---------------------------------------------------------------- арифметический декодер MQ
type qeEntry struct {
	qe         uint32
	nmps, nlps uint8
	sw         uint8
}

var qeTable = [47]qeEntry{
	{0x5601, 1, 1, 1}, {0x3401, 2, 6, 0}, {0x1801, 3, 9, 0}, {0x0AC1, 4, 12, 0}, {0x0521, 5, 29, 0}, {0x0221, 38, 33, 0},
	{0x5601, 7, 6, 1}, {0x5401, 8, 14, 0}, {0x4801, 9, 14, 0}, {0x3801, 10, 14, 0}, {0x3001, 11, 17, 0}, {0x2401, 12, 18, 0},
	{0x1C01, 13, 20, 0}, {0x1601, 29, 21, 0}, {0x5601, 15, 14, 1}, {0x5401, 16, 14, 0}, {0x5101, 17, 15, 0}, {0x4801, 18, 16, 0},
	{0x3801, 19, 17, 0}, {0x3401, 20, 18, 0}, {0x3001, 21, 19, 0}, {0x2801, 22, 19, 0}, {0x2401, 23, 20, 0}, {0x2201, 24, 21, 0},
	{0x1C01, 25, 22, 0}, {0x1801, 26, 23, 0}, {0x1601, 27, 24, 0}, {0x1401, 28, 25, 0}, {0x1201, 29, 26, 0}, {0x1101, 30, 27, 0},
	{0x0AC1, 31, 28, 0}, {0x09C1, 32, 29, 0}, {0x08A1, 33, 30, 0}, {0x0521, 34, 31, 0}, {0x0441, 35, 32, 0}, {0x02A1, 36, 33, 0},
	{0x0221, 37, 34, 0}, {0x0141, 38, 35, 0}, {0x0111, 39, 36, 0}, {0x0085, 40, 37, 0}, {0x0049, 41, 38, 0}, {0x0025, 42, 39, 0},
	{0x0015, 43, 40, 0}, {0x0009, 44, 41, 0}, {0x0005, 45, 42, 0}, {0x0001, 45, 43, 0}, {0x5601, 46, 46, 0},
}

type mqDecoder struct {
	data        []byte
	bp, end     int
	chigh, clow uint32
	ct          int
	a           uint32
	budget      *int64
}

func newMQ(data []byte, start, end int) *mqDecoder {
	d := &mqDecoder{data: data, bp: start, end: end, budget: new(int64)}
	if start < len(data) {
		d.chigh = uint32(data[start])
	}
	d.byteIn()
	d.chigh = ((d.chigh << 7) & 0xffff) | ((d.clow >> 9) & 0x7f)
	d.clow = (d.clow << 7) & 0xffff
	d.ct -= 7
	d.a = 0x8000
	return d
}

func (d *mqDecoder) at(i int) uint32 {
	if i < d.end && i < len(d.data) {
		return uint32(d.data[i])
	}
	return 0xff
}

func (d *mqDecoder) byteIn() {
	if d.at(d.bp) == 0xff {
		if d.at(d.bp+1) > 0x8f {
			d.clow += 0xff00
			d.ct = 8
		} else {
			d.bp++
			d.clow += d.at(d.bp) << 9
			d.ct = 7
		}
	} else {
		d.bp++
		if d.bp < d.end {
			d.clow += d.at(d.bp) << 8
		} else {
			d.clow += 0xff00
		}
		d.ct = 8
	}
	if d.clow > 0xffff {
		d.chigh += d.clow >> 16
		d.clow &= 0xffff
	}
}

// j2workLimit — сколько арифметических решений разрешено на один поток: битые заголовки
// с гигантскими размерами иначе вешают отрисовку на минуты.
const j2workLimit = 150_000_000

func (d *mqDecoder) bit(cx []uint8, pos int) int {
	*d.budget++
	if *d.budget > j2workLimit {
		panic("jbig2: слишком много работы")
	}
	idx := int(cx[pos] >> 1)
	mps := int(cx[pos] & 1)
	q := qeTable[idx]
	var bit int
	a := d.a - q.qe
	if d.chigh < q.qe {
		if a < q.qe {
			a = q.qe
			bit = mps
			idx = int(q.nmps)
		} else {
			a = q.qe
			bit = 1 ^ mps
			if q.sw == 1 {
				mps = bit
			}
			idx = int(q.nlps)
		}
	} else {
		d.chigh -= q.qe
		if a&0x8000 != 0 {
			d.a = a
			return mps
		}
		if a < q.qe {
			bit = 1 ^ mps
			if q.sw == 1 {
				mps = bit
			}
			idx = int(q.nlps)
		} else {
			bit = mps
			idx = int(q.nmps)
		}
	}
	for {
		if d.ct == 0 {
			d.byteIn()
		}
		a <<= 1
		d.chigh = ((d.chigh << 1) & 0xffff) | ((d.clow >> 15) & 1)
		d.clow = (d.clow << 1) & 0xffff
		d.ct--
		if a&0x8000 != 0 {
			break
		}
	}
	d.a = a
	cx[pos] = uint8(idx<<1 | mps)
	return bit
}

// ---------------------------------------------------------------- контекст декодирования одного сегмента
type j2ctx struct {
	dec *mqDecoder
	cx  map[string][]uint8
}

func (c *j2ctx) get(name string, size int) []uint8 {
	if v, ok := c.cx[name]; ok && len(v) >= size {
		return v
	}
	v := make([]uint8, size)
	c.cx[name] = v
	return v
}

// integer — процедура декодирования целого (A.2); ok=false — OOB.
func (c *j2ctx) integer(name string) (int, bool) {
	cx := c.get(name, 512)
	prev := 1
	read := func(n int) int {
		v := 0
		for i := 0; i < n; i++ {
			b := c.dec.bit(cx, prev)
			if prev < 256 {
				prev = prev<<1 | b
			} else {
				prev = ((prev<<1 | b) & 511) | 256
			}
			v = v<<1 | b
		}
		return v
	}
	sign := read(1)
	var v int
	switch {
	case read(1) == 0:
		v = read(2)
	case read(1) == 0:
		v = read(4) + 4
	case read(1) == 0:
		v = read(6) + 20
	case read(1) == 0:
		v = read(8) + 84
	case read(1) == 0:
		v = read(12) + 340
	default:
		v = read(32) + 4436
	}
	if sign == 0 {
		return v, true
	}
	if v > 0 {
		return -v, true
	}
	return 0, false
}

func (c *j2ctx) iaid(n int) int {
	size := 1 << uint(min(n, 24)+1)
	cx := c.get("IAID", size)
	prev := 1
	for i := 0; i < n; i++ {
		prev = (prev<<1 | c.dec.bit(cx, prev&(size-1))) & (size*2 - 1)
	}
	if n < 31 {
		return prev & (1<<uint(n) - 1)
	}
	return prev & 0x7fffffff
}

type j2bitmap [][]uint8

// j2maxPix — предел размера одной битовой карты: битый заголовок не должен просить гигабайты.
const j2maxPix = 100_000_000

func newBitmap(w, h int) j2bitmap {
	if w < 0 || h < 0 || w > 1<<20 || h > 1<<20 || (w > 0 && h > j2maxPix/w) {
		panic("jbig2: недопустимый размер")
	}
	b := make(j2bitmap, h)
	buf := make([]uint8, w*h)
	for i := range b {
		b[i] = buf[i*w : (i+1)*w : (i+1)*w]
	}
	return b
}

func (b j2bitmap) w() int {
	if len(b) == 0 {
		return 0
	}
	return len(b[0])
}

type j2pt struct{ x, y int }

var genericTemplates = [4][]j2pt{
	{{-1, -2}, {0, -2}, {1, -2}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {2, -1}, {-4, 0}, {-3, 0}, {-2, 0}, {-1, 0}},
	{{-1, -2}, {0, -2}, {1, -2}, {2, -2}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {2, -1}, {-3, 0}, {-2, 0}, {-1, 0}},
	{{-1, -2}, {0, -2}, {1, -2}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-2, 0}, {-1, 0}},
	{{-3, -1}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-4, 0}, {-3, 0}, {-2, 0}, {-1, 0}},
}

var genericReused = [4]int{0x9b25, 0x0795, 0x00e5, 0x0195}

// generic — обобщённая область (6.2), арифметическое кодирование.
func (c *j2ctx) generic(w, h, tpl int, prediction bool, at []j2pt) j2bitmap {
	t := append(append([]j2pt(nil), genericTemplates[tpl]...), at...)
	sort.SliceStable(t, func(i, j int) bool {
		if t[i].y != t[j].y {
			return t[i].y < t[j].y
		}
		return t[i].x < t[j].x
	})
	cx := c.get("GB", 1<<16)
	// строки с полями по краям: соседи за границей всегда нули, без проверок в горячем цикле
	padX, padY := 0, 0
	for _, p := range t {
		padX = max(padX, absI(p.x))
		padY = max(padY, -p.y)
	}
	if w < 0 || h < 0 || w > 1<<20 || h > 1<<20 || (w > 0 && h > j2maxPix/w) {
		panic("jbig2: недопустимый размер")
	}
	if padX > 256 || padY > 256 || w <= 0 || h <= 0 {
		return c.genericSlow(w, h, t, cx, prediction, tpl)
	}
	W2 := w + 2*padX
	buf := make([]uint8, (h+padY)*W2)
	bm := make(j2bitmap, h)
	for y := range bm {
		o := (y+padY)*W2 + padX
		bm[y] = buf[o : o+w : o+w]
	}
	off := make([]int, len(t))
	for i, p := range t {
		off[i] = p.y*W2 + p.x
	}
	ltp := 0
	for y := 0; y < h; y++ {
		if prediction {
			ltp ^= c.dec.bit(cx, genericReused[tpl])
			if ltp != 0 {
				if y > 0 {
					copy(bm[y], bm[y-1])
				}
				continue
			}
		}
		base := (y+padY)*W2 + padX
		row := bm[y]
		for x := 0; x < w; x++ {
			i := base + x
			ctx := 0
			for _, o := range off {
				ctx = ctx<<1 | int(buf[i+o])
			}
			row[x] = uint8(c.dec.bit(cx, ctx&0xffff))
		}
	}
	return bm
}

func (c *j2ctx) genericSlow(w, h int, t []j2pt, cx []uint8, prediction bool, tpl int) j2bitmap {
	bm := newBitmap(w, h)
	ltp := 0
	for y := 0; y < h; y++ {
		if prediction {
			ltp ^= c.dec.bit(cx, genericReused[tpl])
			if ltp != 0 {
				if y > 0 {
					copy(bm[y], bm[y-1])
				}
				continue
			}
		}
		row := bm[y]
		for x := 0; x < w; x++ {
			ctx := 0
			for _, p := range t {
				yy, xx := y+p.y, x+p.x
				ctx <<= 1
				if yy >= 0 && xx >= 0 && xx < w && (yy < y || xx < x) {
					ctx |= int(bm[yy][xx])
				}
			}
			row[x] = uint8(c.dec.bit(cx, ctx&0xffff))
		}
	}
	return bm
}

var refineCoding = [2][]j2pt{{{0, -1}, {1, -1}, {-1, 0}}, {{-1, -1}, {0, -1}, {1, -1}, {-1, 0}}}
var refineRef = [2][]j2pt{{{0, -1}, {1, -1}, {-1, 0}, {0, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}}, {{0, -1}, {-1, 0}, {0, 0}, {1, 0}, {0, 1}, {1, 1}}}

// refine — уточнение (6.3) по опорной картинке.
func (c *j2ctx) refine(w, h, tpl int, ref j2bitmap, dx, dy int, at []j2pt) j2bitmap {
	coding := append([]j2pt(nil), refineCoding[tpl]...)
	reference := append([]j2pt(nil), refineRef[tpl]...)
	if tpl == 0 && len(at) >= 2 {
		coding = append(coding, at[0])
		reference = append(reference, at[1])
	}
	cx := c.get("GR", 1<<16)
	bm := newBitmap(w, h)
	rh, rw := len(ref), ref.w()
	for y := 0; y < h; y++ {
		row := bm[y]
		for x := 0; x < w; x++ {
			ctx := 0
			for _, p := range coding {
				yy, xx := y+p.y, x+p.x
				ctx <<= 1
				if yy >= 0 && xx >= 0 && xx < w && (yy < y || xx < x) {
					ctx |= int(bm[yy][xx])
				}
			}
			for _, p := range reference {
				yy, xx := y+p.y-dy, x+p.x-dx
				ctx <<= 1
				if yy >= 0 && yy < rh && xx >= 0 && xx < rw {
					ctx |= int(ref[yy][xx])
				}
			}
			row[x] = uint8(c.dec.bit(cx, ctx&0xffff))
		}
	}
	return bm
}

// mmrBitmap — область, сжатая как факс G4.
func mmrBitmap(data []byte, w, h int) j2bitmap {
	raw := ccittDecode(data, CCITTParams{K: -1, Columns: w, Rows: h, BlackIs1: true})
	bm := newBitmap(w, h)
	stride := (w + 7) / 8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*stride + x>>3
			if i < len(raw) && raw[i]&(0x80>>uint(x&7)) != 0 {
				bm[y][x] = 1
			}
		}
	}
	return bm
}

// ---------------------------------------------------------------- Хаффман (приложение B)
type hLine struct {
	low, plen, rlen, code int
	lower, oob            bool
}

type hTable struct {
	m map[uint64]*hLine // длина<<32 | код
}

type j2bits struct {
	b   []byte
	pos int // биты
	end int // байты
}

func (r *j2bits) bit() int {
	if r.pos>>3 >= r.end || r.pos>>3 >= len(r.b) {
		r.pos++
		return 0
	}
	v := int(r.b[r.pos>>3]>>(7-uint(r.pos&7))) & 1
	r.pos++
	return v
}

func (r *j2bits) bits(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v = v<<1 | r.bit()
	}
	return v
}

func (r *j2bits) align() { r.pos = (r.pos + 7) &^ 7 }

func newHTable(lines []*hLine, assign bool) *hTable {
	if assign {
		maxL := 0
		for _, l := range lines {
			maxL = max(maxL, l.plen)
		}
		hist := make([]int, maxL+1)
		for _, l := range lines {
			hist[l.plen]++
		}
		hist[0] = 0
		first := 0
		for cl := 1; cl <= maxL; cl++ {
			first = (first + hist[cl-1]) << 1
			code := first
			for _, l := range lines {
				if l.plen == cl {
					l.code = code
					code++
				}
			}
		}
	}
	t := &hTable{m: map[uint64]*hLine{}}
	for _, l := range lines {
		if l.plen > 0 {
			t.m[uint64(l.plen)<<32|uint64(l.code)] = l
		}
	}
	return t
}

// decode — значение; ok=false — OOB.
func (t *hTable) decode(r *j2bits) (int, bool) {
	code := 0
	for n := 1; n <= 32; n++ {
		code = code<<1 | r.bit()
		if l, ok := t.m[uint64(n)<<32|uint64(code)]; ok {
			if l.oob {
				return 0, false
			}
			if l.lower {
				return l.low - r.bits(l.rlen), true
			}
			return l.low + r.bits(l.rlen), true
		}
	}
	return 0, false
}

var stdTablesRaw = [16][][]int{
	1:  {{0, 1, 4, 0x0}, {16, 2, 8, 0x2}, {272, 3, 16, 0x6}, {65808, 4, 32, 0xe}},
	2:  {{0, 1, 0, 0x0}, {1, 2, 0, 0x2}, {2, 3, 0, 0x6}, {3, 4, 3, 0xe}, {11, 5, 6, 0x1e}, {75, 6, 32, 0x3e}, {6, 0x3f}},
	3:  {{-256, 8, 8, 0xfe}, {0, 1, 0, 0x0}, {1, 2, 0, 0x2}, {2, 3, 0, 0x6}, {3, 4, 3, 0xe}, {11, 5, 6, 0x1e}, {-257, 8, 32, 0xff, 1}, {75, 7, 32, 0x7e}, {6, 0x3e}},
	4:  {{1, 1, 0, 0x0}, {2, 2, 0, 0x2}, {3, 3, 0, 0x6}, {4, 4, 3, 0xe}, {12, 5, 6, 0x1e}, {76, 5, 32, 0x1f}},
	5:  {{-255, 7, 8, 0x7e}, {1, 1, 0, 0x0}, {2, 2, 0, 0x2}, {3, 3, 0, 0x6}, {4, 4, 3, 0xe}, {12, 5, 6, 0x1e}, {-256, 7, 32, 0x7f, 1}, {76, 6, 32, 0x3e}},
	6:  {{-2048, 5, 10, 0x1c}, {-1024, 4, 9, 0x8}, {-512, 4, 8, 0x9}, {-256, 4, 7, 0xa}, {-128, 5, 6, 0x1d}, {-64, 5, 5, 0x1e}, {-32, 4, 5, 0xb}, {0, 2, 7, 0x0}, {128, 3, 7, 0x2}, {256, 3, 8, 0x3}, {512, 4, 9, 0xc}, {1024, 4, 10, 0xd}, {-2049, 6, 32, 0x3e, 1}, {2048, 6, 32, 0x3f}},
	7:  {{-1024, 4, 9, 0x8}, {-512, 3, 8, 0x0}, {-256, 4, 7, 0x9}, {-128, 5, 6, 0x1a}, {-64, 5, 5, 0x1b}, {-32, 4, 5, 0xa}, {0, 4, 5, 0xb}, {32, 5, 5, 0x1c}, {64, 5, 6, 0x1d}, {128, 4, 7, 0xc}, {256, 3, 8, 0x1}, {512, 3, 9, 0x2}, {1024, 3, 10, 0x3}, {-1025, 5, 32, 0x1e, 1}, {2048, 5, 32, 0x1f}},
	8:  {{-15, 8, 3, 0xfc}, {-7, 9, 1, 0x1fc}, {-5, 8, 1, 0xfd}, {-3, 9, 0, 0x1fd}, {-2, 7, 0, 0x7c}, {-1, 4, 0, 0xa}, {0, 2, 1, 0x0}, {2, 5, 0, 0x1a}, {3, 6, 0, 0x3a}, {4, 3, 4, 0x4}, {20, 6, 1, 0x3b}, {22, 4, 4, 0xb}, {38, 4, 5, 0xc}, {70, 5, 6, 0x1b}, {134, 5, 7, 0x1c}, {262, 6, 7, 0x3c}, {390, 7, 8, 0x7d}, {646, 6, 10, 0x3d}, {-16, 9, 32, 0x1fe, 1}, {1670, 9, 32, 0x1ff}, {2, 0x1}},
	9:  {{-31, 8, 4, 0xfc}, {-15, 9, 2, 0x1fc}, {-11, 8, 2, 0xfd}, {-7, 9, 1, 0x1fd}, {-5, 7, 1, 0x7c}, {-3, 4, 1, 0xa}, {-1, 3, 1, 0x2}, {1, 3, 1, 0x3}, {3, 5, 1, 0x1a}, {5, 6, 1, 0x3a}, {7, 3, 5, 0x4}, {39, 6, 2, 0x3b}, {43, 4, 5, 0xb}, {75, 4, 6, 0xc}, {139, 5, 7, 0x1b}, {267, 5, 8, 0x1c}, {523, 6, 8, 0x3c}, {779, 7, 9, 0x7d}, {1291, 6, 11, 0x3d}, {-32, 9, 32, 0x1fe, 1}, {3339, 9, 32, 0x1ff}, {2, 0x0}},
	10: {{-21, 7, 4, 0x7a}, {-5, 8, 0, 0xfc}, {-4, 7, 0, 0x7b}, {-3, 5, 0, 0x18}, {-2, 2, 2, 0x0}, {2, 5, 0, 0x19}, {3, 6, 0, 0x36}, {4, 7, 0, 0x7c}, {5, 8, 0, 0xfd}, {6, 2, 6, 0x1}, {70, 5, 5, 0x1a}, {102, 6, 5, 0x37}, {134, 6, 6, 0x38}, {198, 6, 7, 0x39}, {326, 6, 8, 0x3a}, {582, 6, 9, 0x3b}, {1094, 6, 10, 0x3c}, {2118, 7, 11, 0x7d}, {-22, 8, 32, 0xfe, 1}, {4166, 8, 32, 0xff}, {2, 0x2}},
	11: {{1, 1, 0, 0x0}, {2, 2, 1, 0x2}, {4, 4, 0, 0xc}, {5, 4, 1, 0xd}, {7, 5, 1, 0x1c}, {9, 5, 2, 0x1d}, {13, 6, 2, 0x3c}, {17, 7, 2, 0x7a}, {21, 7, 3, 0x7b}, {29, 7, 4, 0x7c}, {45, 7, 5, 0x7d}, {77, 7, 6, 0x7e}, {141, 7, 32, 0x7f}},
	12: {{1, 1, 0, 0x0}, {2, 2, 0, 0x2}, {3, 3, 1, 0x6}, {5, 5, 0, 0x1c}, {6, 5, 1, 0x1d}, {8, 6, 1, 0x3c}, {10, 7, 0, 0x7a}, {11, 7, 1, 0x7b}, {13, 7, 2, 0x7c}, {17, 7, 3, 0x7d}, {25, 7, 4, 0x7e}, {41, 8, 5, 0xfe}, {73, 8, 32, 0xff}},
	13: {{1, 1, 0, 0x0}, {2, 3, 0, 0x4}, {3, 4, 0, 0xc}, {4, 5, 0, 0x1c}, {5, 4, 1, 0xd}, {7, 3, 3, 0x5}, {15, 6, 1, 0x3a}, {17, 6, 2, 0x3b}, {21, 6, 3, 0x3c}, {29, 6, 4, 0x3d}, {45, 6, 5, 0x3e}, {77, 7, 6, 0x7e}, {141, 7, 32, 0x7f}},
	14: {{-2, 3, 0, 0x4}, {-1, 3, 0, 0x5}, {0, 1, 0, 0x0}, {1, 3, 0, 0x6}, {2, 3, 0, 0x7}},
	15: {{-24, 7, 4, 0x7c}, {-8, 6, 2, 0x3c}, {-4, 5, 1, 0x1c}, {-2, 4, 0, 0xc}, {-1, 3, 0, 0x4}, {0, 1, 0, 0x0}, {1, 3, 0, 0x5}, {2, 4, 0, 0xd}, {3, 5, 1, 0x1d}, {5, 6, 2, 0x3d}, {9, 7, 4, 0x7d}, {-25, 7, 32, 0x7e, 1}, {25, 7, 32, 0x7f}},
}

var stdTables [16]*hTable

func stdTable(n int) *hTable {
	if stdTables[n] == nil {
		var ls []*hLine
		for _, r := range stdTablesRaw[n] {
			if len(r) == 2 {
				ls = append(ls, &hLine{plen: r[0], code: r[1], oob: true})
				continue
			}
			ls = append(ls, &hLine{low: r[0], plen: r[1], rlen: r[2], code: r[3], lower: len(r) > 4})
		}
		stdTables[n] = newHTable(ls, false)
	}
	return stdTables[n]
}

// ---------------------------------------------------------------- сегменты
type j2seg struct {
	num, typ int
	refs     []int
	data     []byte
}

func j2segments(b []byte) []j2seg {
	var out []j2seg
	p := 0
	u32 := func(i int) int {
		if i+4 > len(b) {
			return 0
		}
		return int(binary.BigEndian.Uint32(b[i:]))
	}
	for p+11 <= len(b) {
		s := j2seg{num: u32(p)}
		flags := b[p+4]
		s.typ = int(flags & 0x3f)
		pageLarge := flags&0x40 != 0
		rf := b[p+5]
		cnt := int(rf >> 5)
		q := p + 6
		if cnt == 7 {
			cnt = u32(p+5) & 0x1fffffff
			q = p + 9 + (cnt+8)/8
		}
		sz := 1
		if s.num > 65536 {
			sz = 4
		} else if s.num > 256 {
			sz = 2
		}
		for i := 0; i < cnt && q+sz <= len(b); i++ {
			switch sz {
			case 1:
				s.refs = append(s.refs, int(b[q]))
			case 2:
				s.refs = append(s.refs, int(binary.BigEndian.Uint16(b[q:])))
			default:
				s.refs = append(s.refs, u32(q))
			}
			q += sz
		}
		if pageLarge {
			q += 4
		} else {
			q++
		}
		if q+4 > len(b) {
			break
		}
		length := u32(q)
		q += 4
		if uint32(length) == 0xffffffff {
			// длина неизвестна (только у обобщённой области): ищем маркер конца
			end := j2findEnd(b, q)
			length = end - q
		}
		if length < 0 || q+length > len(b) {
			length = len(b) - q
		}
		s.data = b[q : q+length]
		out = append(out, s)
		p = q + length
		if s.typ == 51 { // конец файла
			break
		}
	}
	return out
}

func j2findEnd(b []byte, q int) int {
	if q+18 > len(b) {
		return len(b)
	}
	mmr := b[q+17]&1 != 0
	for i := q + 18; i+6 <= len(b); i++ {
		if mmr && b[i] == 0 && b[i+1] == 0 {
			return i + 6
		}
		if !mmr && b[i] == 0xff && b[i+1] == 0xac {
			return i + 6
		}
	}
	return len(b)
}

type j2region struct {
	w, h, x, y int
	op         int
}

func j2regionInfo(d []byte) (j2region, []byte) {
	if len(d) < 17 {
		return j2region{}, nil
	}
	be := binary.BigEndian
	return j2region{w: int(be.Uint32(d)), h: int(be.Uint32(d[4:])), x: int(be.Uint32(d[8:])), y: int(be.Uint32(d[12:])), op: int(d[16] & 7)}, d[17:]
}

// fit — область не выходит за страницу по высоте (у «полосатых» страниц высота бывает 0xffffffff).
func (r *j2region) fit(pw, ph int) bool {
	if r.w <= 0 || r.w > 1<<17 || r.x < 0 || r.y < 0 || r.y >= ph || r.x >= pw*2 {
		return false
	}
	r.h = min(r.h, ph-r.y)
	return r.h > 0
}

func s8(b byte) int { return int(int8(b)) }

// j2page — страница, на которую собираются области.
type j2page struct {
	bm     j2bitmap
	defPix uint8
}

func (pg *j2page) put(r j2region, bm j2bitmap) {
	W, H := pg.bm.w(), len(pg.bm)
	for y := 0; y < len(bm); y++ {
		py := r.y + y
		if py < 0 || py >= H {
			continue
		}
		dst := pg.bm[py]
		for x, v := range bm[y] {
			px := r.x + x
			if px < 0 || px >= W {
				continue
			}
			switch r.op {
			case 0:
				dst[px] |= v
			case 1:
				dst[px] &= v
			case 2:
				dst[px] ^= v
			case 3:
				dst[px] = ^(dst[px] ^ v) & 1
			default:
				dst[px] = v
			}
		}
	}
}

type j2state struct {
	symbols map[int][]j2bitmap // экспортированные символы словарей по номеру сегмента
	page    *j2page
	w, h    int
	work    int64 // общий счётчик работы по всему потоку
}

// jbig2Decode — картинка w×h в упакованном виде (строки по (w+7)/8 байт), 0 — чёрный (как ждёт PDF).
func jbig2Decode(data, globals []byte, w, h int) (out []byte) {
	st := &j2state{symbols: map[int][]j2bitmap{}, w: w, h: h}
	st.page = &j2page{bm: newBitmap(w, h)}
	defer func() {
		if r := recover(); r != nil {
			// повреждённый поток — отдаём то, что успели собрать
			out = st.pack()
		}
	}()
	for _, s := range j2segments(globals) {
		st.segment(s)
	}
	for _, s := range j2segments(data) {
		st.segment(s)
	}
	return st.pack()
}

func (st *j2state) pack() []byte {
	stride := (st.w + 7) / 8
	out := make([]byte, stride*st.h)
	for i := range out {
		out[i] = 0xff
	}
	for y := 0; y < st.h && y < len(st.page.bm); y++ {
		row := st.page.bm[y]
		for x := 0; x < st.w && x < len(row); x++ {
			if row[x] != 0 {
				out[y*stride+x>>3] &^= 0x80 >> uint(x&7)
			}
		}
	}
	return out
}

func (st *j2state) segment(s j2seg) {
	d := s.data
	switch s.typ {
	case 48: // сведения о странице
		if len(d) >= 17 {
			st.page.defPix = (d[16] >> 2) & 1
			if st.page.defPix != 0 {
				for _, row := range st.page.bm {
					for i := range row {
						row[i] = 1
					}
				}
			}
		}
	case 0:
		st.symbols[s.num] = st.symbolDict(s)
	case 4, 6, 7:
		r, rest := j2regionInfo(d)
		if rest == nil || !r.fit(st.w, st.h) {
			return
		}
		bm := st.textRegion(s, r, rest)
		if bm != nil {
			st.page.put(r, bm)
		}
	case 36, 38, 39:
		r, rest := j2regionInfo(d)
		if len(rest) < 1 || !r.fit(st.w, st.h) {
			return
		}
		flags := rest[0]
		mmr := flags&1 != 0
		tpl := int(flags>>1) & 3
		pred := flags&8 != 0
		rest = rest[1:]
		var bm j2bitmap
		if mmr {
			bm = mmrBitmap(rest, r.w, r.h)
		} else {
			n := 1
			if tpl == 0 {
				n = 4
			}
			if len(rest) < 2*n {
				return
			}
			var at []j2pt
			for i := 0; i < n; i++ {
				at = append(at, j2pt{s8(rest[2*i]), s8(rest[2*i+1])})
			}
			rest = rest[2*n:]
			c := &j2ctx{dec: newMQ(rest, 0, len(rest)), cx: map[string][]uint8{}}
			c.dec.budget = &st.work
			bm = c.generic(r.w, r.h, tpl, pred, at)
		}
		if s.typ != 36 {
			st.page.put(r, bm)
		}
	}
}

func (st *j2state) inputSymbols(refs []int) []j2bitmap {
	var syms []j2bitmap
	for _, r := range refs {
		syms = append(syms, st.symbols[r]...)
	}
	return syms
}

func codeLen(n int) int {
	i, v := 0, 1
	for v < n {
		v <<= 1
		i++
	}
	return i
}

// symbolDict — словарь символов (6.5).
func (st *j2state) symbolDict(s j2seg) []j2bitmap {
	d := s.data
	if len(d) < 2 {
		return nil
	}
	flags := int(binary.BigEndian.Uint16(d))
	huff := flags&1 != 0
	refAgg := flags&2 != 0
	dhSel, dwSel := (flags>>2)&3, (flags>>4)&3
	bmSel, aggSel := (flags>>6)&1, (flags>>7)&1
	tpl := (flags >> 10) & 3
	rtpl := (flags >> 12) & 1
	p := 2
	var at, rat []j2pt
	if !huff {
		n := 1
		if tpl == 0 {
			n = 4
		}
		for i := 0; i < n && p+1 < len(d); i++ {
			at = append(at, j2pt{s8(d[p]), s8(d[p+1])})
			p += 2
		}
	}
	if refAgg && rtpl == 0 {
		for i := 0; i < 2 && p+1 < len(d); i++ {
			rat = append(rat, j2pt{s8(d[p]), s8(d[p+1])})
			p += 2
		}
	}
	if p+8 > len(d) {
		return nil
	}
	nExp := int(binary.BigEndian.Uint32(d[p:]))
	nNew := int(binary.BigEndian.Uint32(d[p+4:]))
	if nNew < 0 || nNew > 1<<20 {
		panic("jbig2: слишком много символов")
	}
	p += 8
	in := st.inputSymbols(s.refs)
	body := d[p:]
	c := &j2ctx{cx: map[string][]uint8{}}
	var hr *j2bits
	var tDH, tDW, tBM, tAGG *hTable
	if huff {
		hr = &j2bits{b: body, end: len(body)}
		tDH = stdTable(map[int]int{0: 4, 1: 5}[dhSel])
		tDW = stdTable(map[int]int{0: 2, 1: 3}[dwSel])
		tBM, tAGG = stdTable(1), stdTable(1)
		if dhSel == 3 || dwSel == 3 || bmSel == 1 || aggSel == 1 || tDH == nil || tDW == nil {
			return nil // свои таблицы не поддерживаем
		}
	} else {
		c.dec = newMQ(body, 0, len(body))
		c.dec.budget = &st.work
	}
	symLen := codeLen(len(in) + nNew)
	var news []j2bitmap
	var widths []int
	curH := 0
	guard := 0
	for len(news) < nNew && guard < 1_000_000 {
		guard++
		var dh int
		if huff {
			dh, _ = tDH.decode(hr)
		} else {
			dh, _ = c.integer("IADH")
		}
		curH += dh
		curW, totalW := 0, 0
		first := len(widths)
		for {
			var dw int
			var ok bool
			if huff {
				dw, ok = tDW.decode(hr)
			} else {
				dw, ok = c.integer("IADW")
			}
			if !ok {
				break
			}
			curW += dw
			totalW += curW
			if curW <= 0 || curH <= 0 || curW > 1<<15 || curH > 1<<15 {
				return nil
			}
			switch {
			case refAgg && !huff:
				ninst, _ := c.integer("IAAI")
				all := append(append([]j2bitmap(nil), in...), news...)
				if ninst > 1 {
					bm := st.textRegionDecode(c, nil, false, true, curW, curH, 0, ninst, 1, all, symLen, false, 0, 1, 0, rtpl, rat, 0, nil)
					news = append(news, bm)
				} else {
					id := c.iaid(symLen)
					rdx, _ := c.integer("IARDX")
					rdy, _ := c.integer("IARDY")
					if id >= len(all) {
						return nil
					}
					news = append(news, c.refine(curW, curH, rtpl, all[id], rdx, rdy, rat))
				}
			case huff:
				widths = append(widths, curW)
			default:
				news = append(news, c.generic(curW, curH, tpl, false, at))
			}
		}
		if huff {
			size, _ := tBM.decode(hr)
			hr.align()
			var coll j2bitmap
			start := hr.pos >> 3
			if size == 0 {
				coll = newBitmap(totalW, curH)
				for y := 0; y < curH; y++ {
					for x := 0; x < totalW; x++ {
						coll[y][x] = uint8(hr.bit())
					}
					hr.align()
				}
			} else {
				end := min(start+size, len(body))
				coll = mmrBitmap(body[start:end], totalW, curH)
				hr.pos = end * 8
			}
			x0 := 0
			for _, wd := range widths[first:] {
				sym := newBitmap(wd, curH)
				for y := 0; y < curH; y++ {
					copy(sym[y], coll[y][x0:x0+wd])
				}
				news = append(news, sym)
				x0 += wd
			}
		}
	}
	_ = tAGG
	// какие символы экспортируются
	total := len(in) + nNew
	flagsEx := make([]bool, 0, total)
	cur := false
	for len(flagsEx) < total && guard < 2_000_000 {
		guard++
		var run int
		if huff {
			run, _ = stdTable(1).decode(hr)
		} else {
			run, _ = c.integer("IAEX")
		}
		if run < 0 {
			break
		}
		for i := 0; i < run && len(flagsEx) < total; i++ {
			flagsEx = append(flagsEx, cur)
		}
		cur = !cur
	}
	var out []j2bitmap
	for i := 0; i < len(flagsEx); i++ {
		if !flagsEx[i] {
			continue
		}
		if i < len(in) {
			out = append(out, in[i])
		} else if i-len(in) < len(news) {
			out = append(out, news[i-len(in)])
		}
	}
	_ = nExp
	return out
}

// textRegion — сегмент текстовой области (6.4).
func (st *j2state) textRegion(s j2seg, r j2region, d []byte) j2bitmap {
	if len(d) < 2 {
		return nil
	}
	flags := int(binary.BigEndian.Uint16(d))
	p := 2
	huff := flags&1 != 0
	refine := flags&2 != 0
	logSS := (flags >> 2) & 3
	corner := (flags >> 4) & 3
	transposed := flags&64 != 0
	op := (flags >> 7) & 3
	defPix := (flags >> 9) & 1
	dsOff := int(int32(uint32(flags)<<17) >> 27)
	rtpl := (flags >> 15) & 1
	hflags := 0
	if huff {
		if p+2 > len(d) {
			return nil
		}
		hflags = int(binary.BigEndian.Uint16(d[p:]))
		p += 2
	}
	var rat []j2pt
	if refine && rtpl == 0 {
		for i := 0; i < 2 && p+1 < len(d); i++ {
			rat = append(rat, j2pt{s8(d[p]), s8(d[p+1])})
			p += 2
		}
	}
	if p+4 > len(d) {
		return nil
	}
	nInst := int(binary.BigEndian.Uint32(d[p:]))
	if nInst < 0 || nInst > 1<<22 {
		panic("jbig2: слишком много вхождений")
	}
	p += 4
	syms := st.inputSymbols(s.refs)
	body := d[p:]
	c := &j2ctx{cx: map[string][]uint8{}}
	symLen := codeLen(len(syms))
	var ht *j2huffText
	if huff {
		if refine {
			return nil
		}
		hr := &j2bits{b: body, end: len(body)}
		ht = newHuffText(hr, hflags, len(syms))
		if ht == nil {
			return nil
		}
	} else {
		c.dec = newMQ(body, 0, len(body))
		c.dec.budget = &st.work
	}
	return st.textRegionDecode(c, ht, huff, refine, r.w, r.h, defPix, nInst, 1<<uint(logSS), syms, symLen, transposed, dsOff, corner, op, rtpl, rat, logSS, nil)
}

type j2huffText struct {
	r               *j2bits
	ids, fs, ds, dt *hTable
}

func newHuffText(r *j2bits, hflags, nsyms int) *j2huffText {
	fsSel, dsSel, dtSel := hflags&3, (hflags>>2)&3, (hflags>>4)&3
	if fsSel == 3 || dsSel == 3 || dtSel == 3 {
		return nil
	}
	var runLines []*hLine
	for i := 0; i <= 34; i++ {
		runLines = append(runLines, &hLine{low: i, plen: r.bits(4)})
	}
	runT := newHTable(runLines, true)
	var lines []*hLine
	for i := 0; i < nsyms; {
		cl, ok := runT.decode(r)
		if !ok {
			return nil
		}
		switch {
		case cl == 32:
			if i == 0 {
				return nil
			}
			n := r.bits(2) + 3
			prev := lines[i-1].plen
			for j := 0; j < n; j++ {
				lines = append(lines, &hLine{low: i, plen: prev})
				i++
			}
		case cl == 33 || cl == 34:
			n := r.bits(3) + 3
			if cl == 34 {
				n = r.bits(7) + 11
			}
			for j := 0; j < n; j++ {
				lines = append(lines, &hLine{low: i, plen: 0})
				i++
			}
		default:
			lines = append(lines, &hLine{low: i, plen: cl})
			i++
		}
	}
	r.align()
	return &j2huffText{r: r, ids: newHTable(lines, true), fs: stdTable(fsSel + 6), ds: stdTable(dsSel + 8), dt: stdTable(dtSel + 11)}
}

func (st *j2state) textRegionDecode(c *j2ctx, ht *j2huffText, huff, refine bool, w, h, defPix, nInst, stripSize int,
	syms []j2bitmap, symLen int, transposed bool, dsOff, corner, op, rtpl int, rat []j2pt, logSS int, _ any) j2bitmap {
	bm := newBitmap(w, h)
	if defPix != 0 {
		for _, row := range bm {
			for i := range row {
				row[i] = 1
			}
		}
	}
	readInt := func(name string, t *hTable) (int, bool) {
		if huff {
			return t.decode(ht.r)
		}
		return c.integer(name)
	}
	var dtT, fsT, dsT *hTable
	if huff {
		dtT, fsT, dsT = ht.dt, ht.fs, ht.ds
	}
	v, _ := readInt("IADT", dtT)
	stripT := -v * stripSize
	firstS := 0
	inst := 0
	for inst < nInst {
		dt, _ := readInt("IADT", dtT)
		stripT += dt * stripSize
		dfs, _ := readInt("IAFS", fsT)
		firstS += dfs
		curS := firstS
		for guard := 0; guard < 1_000_000; guard++ {
			curT := 0
			if stripSize > 1 {
				if huff {
					curT = ht.r.bits(logSS)
				} else {
					curT, _ = c.integer("IAIT")
				}
			}
			t := stripT + curT
			var id int
			if huff {
				id, _ = ht.ids.decode(ht.r)
			} else {
				id = c.iaid(symLen)
			}
			if id < 0 || id >= len(syms) {
				return bm
			}
			sym := syms[id]
			ri := 0
			if refine {
				if huff {
					ri = ht.r.bit()
				} else {
					ri, _ = c.integer("IARI")
				}
			}
			sw, sh := sym.w(), len(sym)
			if ri != 0 {
				rdw, _ := c.integer("IARDW")
				rdh, _ := c.integer("IARDH")
				rdx, _ := c.integer("IARDX")
				rdy, _ := c.integer("IARDY")
				sw += rdw
				sh += rdh
				if sw <= 0 || sh <= 0 {
					return bm
				}
				sym = c.refine(sw, sh, rtpl, sym, (rdw>>1)+rdx, (rdh>>1)+rdy, rat)
			}
			inc := 0
			if !transposed {
				if corner > 1 {
					curS += sw - 1
				} else {
					inc = sw - 1
				}
			} else if corner&1 == 0 {
				curS += sh - 1
			} else {
				inc = sh - 1
			}
			offT := t
			if corner&1 == 0 {
				offT = t - (sh - 1)
			}
			offS := curS
			if corner&2 != 0 {
				offS = curS - (sw - 1)
			}
			put := func(y, x int, v uint8) {
				if y < 0 || y >= h || x < 0 || x >= w {
					return
				}
				switch op {
				case 0:
					bm[y][x] |= v
				case 1:
					bm[y][x] &= v
				case 2:
					bm[y][x] ^= v
				case 3:
					bm[y][x] = ^(bm[y][x] ^ v) & 1
				}
			}
			if transposed {
				for s2 := 0; s2 < sh; s2++ {
					for t2 := 0; t2 < sw; t2++ {
						put(offS+s2, offT+t2, sym[s2][t2])
					}
				}
			} else {
				for t2 := 0; t2 < sh; t2++ {
					for s2 := 0; s2 < sw; s2++ {
						put(offT+t2, offS+s2, sym[t2][s2])
					}
				}
			}
			inst++
			ds, ok := readInt("IADS", dsT)
			if !ok {
				break
			}
			curS += inc + ds + dsOff
		}
	}
	return bm
}
