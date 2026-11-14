package main

// Декодер CCITT Group 3/4 (факс) — так хранят чёрно-белые сканы в PDF (фильтр CCITTFaxDecode)
// и MMR-области в JBIG2.

import "strings"

const (
	ccWhiteTerm = "00110101 000111 0111 1000 1011 1100 1110 1111 10011 10100 00111 01000 001000 000011 110100 110101 " +
		"101010 101011 0100111 0001100 0001000 0010111 0000011 0000100 0101000 0101011 0010011 0100100 0011000 00000010 00000011 00011010 " +
		"00011011 00010010 00010011 00010100 00010101 00010110 00010111 00101000 00101001 00101010 00101011 00101100 00101101 00000100 00000101 00001010 " +
		"00001011 01010010 01010011 01010100 01010101 00100100 00100101 01011000 01011001 01011010 01011011 01001010 01001011 00110010 00110011 00110100"
	ccWhiteMake = "11011 10010 010111 0110111 00110110 00110111 01100100 01100101 01101000 01100111 011001100 011001101 011010010 011010011 " +
		"011010100 011010101 011010110 011010111 011011000 011011001 011011010 011011011 010011000 010011001 010011010 011000 010011011"
	ccBlackTerm = "0000110111 010 11 10 011 0011 0010 00011 000101 000100 0000100 0000101 0000111 00000100 00000111 000011000 " +
		"0000010111 0000011000 0000001000 00001100111 00001101000 00001101100 00000110111 00000101000 00000010111 00000011000 000011001010 000011001011 000011001100 000011001101 000001101000 000001101001 " +
		"000001101010 000001101011 000011010010 000011010011 000011010100 000011010101 000011010110 000011010111 000001101100 000001101101 000011011010 000011011011 000001010100 000001010101 000001010110 000001010111 " +
		"000001100100 000001100101 000001010010 000001010011 000000100100 000000110111 000000111000 000000100111 000000101000 000001011000 000001011001 000000101011 000000101100 000001011010 000001100110 000001100111"
	ccBlackMake = "0000001111 000011001000 000011001001 000001011011 000000110011 000000110100 000000110101 0000001101100 0000001101101 " +
		"0000001001010 0000001001011 0000001001100 0000001001101 0000001110010 0000001110011 0000001110100 0000001110101 0000001110110 " +
		"0000001110111 0000001010010 0000001010011 0000001010100 0000001010101 0000001011010 0000001011011 0000001100100 0000001100101"
	ccExtMake = "00000001000 00000001100 00000001101 000000010010 000000010011 000000010100 000000010101 000000010110 000000010111 " +
		"000000011100 000000011101 000000011110 000000011111"
)

// таблица: ключ = длина<<16 | код
var ccRuns [2]map[uint32]int

func init() {
	for c := 0; c < 2; c++ {
		ccRuns[c] = map[uint32]int{}
		term, mk := ccWhiteTerm, ccWhiteMake
		if c == 1 {
			term, mk = ccBlackTerm, ccBlackMake
		}
		add := func(code string, v int) {
			k := uint32(0)
			for _, ch := range code {
				k = k<<1 | uint32(ch-'0')
			}
			ccRuns[c][uint32(len(code))<<16|k] = v
		}
		for i, s := range strings.Fields(term) {
			add(s, i)
		}
		for i, s := range strings.Fields(mk) {
			add(s, 64*(i+1))
		}
		for i, s := range strings.Fields(ccExtMake) {
			add(s, 1792+64*i)
		}
	}
}

type bitReader struct {
	b   []byte
	pos int // в битах
}

func (r *bitReader) bit() int {
	if r.pos>>3 >= len(r.b) {
		r.pos++
		return 0
	}
	v := int(r.b[r.pos>>3]>>(7-uint(r.pos&7))) & 1
	r.pos++
	return v
}

func (r *bitReader) peek(n int) uint32 {
	v := uint32(0)
	for i := 0; i < n; i++ {
		p := r.pos + i
		b := uint32(0)
		if p>>3 < len(r.b) {
			b = uint32(r.b[p>>3]>>(7-uint(p&7))) & 1
		}
		v = v<<1 | b
	}
	return v
}

func (r *bitReader) eof() bool { return r.pos>>3 >= len(r.b) }
func (r *bitReader) align()    { r.pos = (r.pos + 7) &^ 7 }

// run — одна длина серии (с кодами «дополнения»); -1 — ошибка.
func (r *bitReader) run(color int) int {
	total := 0
	for guard := 0; guard < 100; guard++ {
		v, ok := -1, false
		for n := 2; n <= 13; n++ {
			if v, ok = ccRuns[color][uint32(n)<<16|r.peek(n)]; ok {
				r.pos += n
				break
			}
		}
		if !ok {
			return -1
		}
		total += v
		if v < 64 {
			return total
		}
	}
	return -1
}

// mode2D — код режима для двумерного кодирования.
const (
	mP = iota
	mH
	mV0
	mVR1
	mVR2
	mVR3
	mVL1
	mVL2
	mVL3
	mExt
	mEOL
	mErr
)

func (r *bitReader) mode() int {
	switch {
	case r.peek(1) == 1:
		r.pos++
		return mV0
	case r.peek(3) == 3:
		r.pos += 3
		return mVR1
	case r.peek(3) == 2:
		r.pos += 3
		return mVL1
	case r.peek(3) == 1:
		r.pos += 3
		return mH
	case r.peek(4) == 1:
		r.pos += 4
		return mP
	case r.peek(6) == 3:
		r.pos += 6
		return mVR2
	case r.peek(6) == 2:
		r.pos += 6
		return mVL2
	case r.peek(7) == 3:
		r.pos += 7
		return mVR3
	case r.peek(7) == 2:
		r.pos += 7
		return mVL3
	case r.peek(7) == 1:
		r.pos += 7
		return mExt
	case r.peek(12) == 1:
		r.pos += 12
		return mEOL
	}
	return mErr
}

// CCITTParams — параметры фильтра.
type CCITTParams struct {
	K                int
	Columns, Rows    int
	BlackIs1         bool
	EncodedByteAlign bool
	EndOfBlock       bool
}

// ccittDecode возвращает строки по (Columns+7)/8 байт; 1 — чёрный, если BlackIs1, иначе 0 — чёрный (как ждёт PDF).
func ccittDecode(data []byte, p CCITTParams) []byte {
	w := p.Columns
	if w <= 0 {
		w = 1728
	}
	stride := (w + 7) / 8
	r := &bitReader{b: data}
	ref := []int{w, w}
	var out []byte
	rowsMax := p.Rows
	if rowsMax <= 0 {
		rowsMax = 1 << 20
	}
	for row := 0; row < rowsMax; row++ {
		if r.eof() {
			break
		}
		if p.EncodedByteAlign && p.K >= 0 {
			// в G3 выравнивание перед EOL; пропускаем нулевые биты до EOL
			if r.peek(12) != 1 {
				r.align()
			}
		} else if p.EncodedByteAlign {
			r.align()
		}
		twoD := p.K < 0
		if p.K >= 0 {
			// необязательный EOL (+ бит режима при K>0)
			skipped := 0
			for r.peek(12) != 1 && r.peek(11) == 0 && skipped < 64 && !r.eof() {
				r.pos++ // заполнитель нулями перед EOL
				skipped++
			}
			if r.peek(12) == 1 {
				r.pos += 12
				if p.K > 0 {
					twoD = r.bit() == 0
				}
			} else if p.K > 0 {
				twoD = r.bit() == 0
			}
		}
		var cur []int
		ok := true
		if twoD {
			cur, ok = decode2D(r, ref, w)
		} else {
			cur, ok = decode1D(r, w)
		}
		if !ok {
			// конец блока (EOFB) или ошибка
			if len(cur) == 0 {
				break
			}
		}
		if cap(out)-len(out) < stride {
			nc := make([]byte, len(out), max(2*cap(out), stride*min(max(p.Rows, 64), len(data)*8+64)))
			copy(nc, out)
			out = nc
		}
		line := out[len(out) : len(out)+stride]
		// cur — позиции смены цвета; начало — белый
		black := false
		x := 0
		for _, c := range append(cur, w) {
			c = min(max(c, x), w)
			if black {
				fillBits(line, x, c)
			}
			x = c
			black = !black
		}
		if !p.BlackIs1 {
			for i := range line {
				line[i] = ^line[i]
			}
		}
		out = out[:len(out)+stride]
		ref = append(cur[:len(cur):len(cur)], w, w)
		if !ok {
			break
		}
	}
	if p.Rows > 0 {
		need := p.Rows * stride
		for len(out) < need {
			fill := byte(0xff)
			if p.BlackIs1 {
				fill = 0
			}
			out = append(out, fill)
		}
	}
	return out
}

func decode1D(r *bitReader, w int) ([]int, bool) {
	var cur []int
	a0, color := 0, 0
	for a0 < w {
		n := r.run(color)
		if n < 0 {
			return cur, false
		}
		a0 += n
		cur = append(cur, min(a0, w))
		color ^= 1
	}
	// последняя смена на правом крае не нужна
	if len(cur) > 0 && cur[len(cur)-1] >= w {
		cur = cur[:len(cur)-1]
	}
	return cur, true
}

func decode2D(r *bitReader, ref []int, w int) ([]int, bool) {
	var cur []int
	a0, color := -1, 0
	bi := 0
	findB1 := func() (int, int) {
		// b1 — первая смена на опорной строке правее a0 с цветом, противоположным текущему
		for bi > 0 && ref[bi-1] > a0 {
			bi--
		}
		for bi < len(ref) && (ref[bi] <= a0 || bi%2 != color) {
			bi++
		}
		b1 := w
		if bi < len(ref) {
			b1 = ref[bi]
		}
		b2 := w
		if bi+1 < len(ref) {
			b2 = ref[bi+1]
		}
		return b1, b2
	}
	for a0 < w {
		m := r.mode()
		b1, b2 := findB1()
		switch m {
		case mP:
			a0 = b2
		case mH:
			start := max(a0, 0)
			n1 := r.run(color)
			n2 := r.run(color ^ 1)
			if n1 < 0 || n2 < 0 {
				return cur, false
			}
			a1 := min(start+n1, w)
			a2 := min(a1+n2, w)
			cur = append(cur, a1, a2)
			a0 = a2
		case mV0, mVR1, mVR2, mVR3, mVL1, mVL2, mVL3:
			d := 0
			switch m {
			case mVR1:
				d = 1
			case mVR2:
				d = 2
			case mVR3:
				d = 3
			case mVL1:
				d = -1
			case mVL2:
				d = -2
			case mVL3:
				d = -3
			}
			a1 := min(max(b1+d, 0), w)
			cur = append(cur, a1)
			a0 = a1
			color ^= 1
		default:
			return cur, false
		}
	}
	// убираем смены на правом крае
	for len(cur) > 0 && cur[len(cur)-1] >= w {
		cur = cur[:len(cur)-1]
	}
	return cur, true
}

// fillBits — выставить биты [x0, x1) в строке (старший бит — левый).
func fillBits(line []byte, x0, x1 int) {
	for x0 < x1 && x0&7 != 0 {
		line[x0>>3] |= 0x80 >> uint(x0&7)
		x0++
	}
	for x0+8 <= x1 {
		line[x0>>3] = 0xff
		x0 += 8
	}
	for x0 < x1 {
		line[x0>>3] |= 0x80 >> uint(x0&7)
		x0++
	}
}
