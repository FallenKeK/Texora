package main

// Читалка PDF, часть 2: шрифты — TrueType, CFF (Type1C/CID), Type1 (eexec), Type3, кодировки, ToUnicode.

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
)

// gpath — контур глифа в долях кегля (1.0 = em). Операции: M L Q C Z.
type gpath struct {
	ops []byte
	pts []float64
}

func (g *gpath) moveTo(x, y float64) { g.ops = append(g.ops, 'M'); g.pts = append(g.pts, x, y) }
func (g *gpath) lineTo(x, y float64) { g.ops = append(g.ops, 'L'); g.pts = append(g.pts, x, y) }
func (g *gpath) quadTo(x1, y1, x, y float64) {
	g.ops = append(g.ops, 'Q')
	g.pts = append(g.pts, x1, y1, x, y)
}
func (g *gpath) curveTo(x1, y1, x2, y2, x, y float64) {
	g.ops = append(g.ops, 'C')
	g.pts = append(g.pts, x1, y1, x2, y2, x, y)
}
func (g *gpath) close() { g.ops = append(g.ops, 'Z') }

// ---------------------------------------------------------------- TrueType
type sfnt struct {
	data     []byte
	tables   map[string][]byte
	upem     float64
	locaLong bool
	numG     int
	cmaps    map[[2]int][]byte
	names    map[string]int
}

func parseSFNT(data []byte, index int) *sfnt {
	if len(data) < 12 {
		return nil
	}
	off := 0
	if string(data[:4]) == "ttcf" {
		n := int(binary.BigEndian.Uint32(data[8:]))
		if index >= n {
			index = 0
		}
		off = int(binary.BigEndian.Uint32(data[12+4*index:]))
	}
	if off+12 > len(data) {
		return nil
	}
	nt := int(binary.BigEndian.Uint16(data[off+4:]))
	f := &sfnt{data: data, tables: map[string][]byte{}, cmaps: map[[2]int][]byte{}}
	for i := 0; i < nt; i++ {
		e := off + 12 + 16*i
		if e+16 > len(data) {
			break
		}
		tag := string(data[e : e+4])
		o := int(binary.BigEndian.Uint32(data[e+8:]))
		l := int(binary.BigEndian.Uint32(data[e+12:]))
		if o < 0 || o > len(data) {
			continue
		}
		f.tables[tag] = data[o:min(len(data), o+l)]
	}
	if h := f.tables["head"]; len(h) >= 54 {
		f.upem = float64(binary.BigEndian.Uint16(h[18:]))
		f.locaLong = binary.BigEndian.Uint16(h[50:]) == 1
	}
	if f.upem == 0 {
		f.upem = 1000
	}
	if m := f.tables["maxp"]; len(m) >= 6 {
		f.numG = int(binary.BigEndian.Uint16(m[4:]))
	}
	if c := f.tables["cmap"]; len(c) >= 4 {
		n := int(binary.BigEndian.Uint16(c[2:]))
		for i := 0; i < n && 4+8*i+8 <= len(c); i++ {
			pid := int(binary.BigEndian.Uint16(c[4+8*i:]))
			eid := int(binary.BigEndian.Uint16(c[6+8*i:]))
			o := int(binary.BigEndian.Uint32(c[8+8*i:]))
			if o < len(c) {
				f.cmaps[[2]int{pid, eid}] = c[o:]
			}
		}
	}
	return f
}

// cmapLookup — номер глифа по коду в подтаблице cmap (форматы 0, 4, 6, 12).
func cmapLookup(t []byte, code int) int {
	if len(t) < 6 {
		return 0
	}
	u16 := func(o int) int {
		if o+2 > len(t) {
			return 0
		}
		return int(binary.BigEndian.Uint16(t[o:]))
	}
	switch u16(0) {
	case 0:
		if code < 256 && 6+code < len(t) {
			return int(t[6+code])
		}
	case 4:
		segX2 := u16(6)
		for i := 0; i < segX2; i += 2 {
			end := u16(14 + i)
			if code > end {
				continue
			}
			start := u16(16 + segX2 + i)
			if code < start {
				return 0
			}
			delta := int16(u16(16 + 2*segX2 + i))
			ro := u16(16 + 3*segX2 + i)
			if ro == 0 {
				return (code + int(delta)) & 0xffff
			}
			gi := u16(16 + 3*segX2 + i + ro + 2*(code-start))
			if gi == 0 {
				return 0
			}
			return (gi + int(delta)) & 0xffff
		}
	case 6:
		first, cnt := u16(6), u16(8)
		if code >= first && code < first+cnt {
			return u16(10 + 2*(code-first))
		}
	case 12:
		if len(t) < 16 {
			return 0
		}
		n := int(binary.BigEndian.Uint32(t[12:]))
		for i := 0; i < n && 16+12*i+12 <= len(t); i++ {
			s := int(binary.BigEndian.Uint32(t[16+12*i:]))
			e := int(binary.BigEndian.Uint32(t[20+12*i:]))
			g := int(binary.BigEndian.Uint32(t[24+12*i:]))
			if code >= s && code <= e {
				return g + code - s
			}
		}
	}
	return 0
}

func (f *sfnt) postNames() map[string]int {
	if f.names != nil {
		return f.names
	}
	f.names = map[string]int{}
	p := f.tables["post"]
	if len(p) < 34 || binary.BigEndian.Uint32(p) != 0x00020000 {
		return f.names
	}
	n := int(binary.BigEndian.Uint16(p[32:]))
	if 34+2*n > len(p) {
		return f.names
	}
	var custom []string
	o := 34 + 2*n
	for o < len(p) {
		l := int(p[o])
		if o+1+l > len(p) {
			break
		}
		custom = append(custom, string(p[o+1:o+1+l]))
		o += 1 + l
	}
	for g := 0; g < n; g++ {
		idx := int(binary.BigEndian.Uint16(p[34+2*g:]))
		if idx < 258 {
			if idx < len(macGlyphNames) {
				f.names[macGlyphNames[idx]] = g
			}
		} else if idx-258 < len(custom) {
			f.names[custom[idx-258]] = g
		}
	}
	return f.names
}

func (f *sfnt) glyph(gid int, depth int) *gpath {
	g := &gpath{}
	loca, glyf := f.tables["loca"], f.tables["glyf"]
	if loca == nil || glyf == nil || depth > 8 {
		return g
	}
	var s, e int
	if f.locaLong {
		if 4*gid+8 > len(loca) {
			return g
		}
		s, e = int(binary.BigEndian.Uint32(loca[4*gid:])), int(binary.BigEndian.Uint32(loca[4*gid+4:]))
	} else {
		if 2*gid+4 > len(loca) {
			return g
		}
		s, e = 2*int(binary.BigEndian.Uint16(loca[2*gid:])), 2*int(binary.BigEndian.Uint16(loca[2*gid+2:]))
	}
	if e <= s || e > len(glyf) {
		return g
	}
	d := glyf[s:e]
	if len(d) < 10 {
		return g
	}
	nc := int(int16(binary.BigEndian.Uint16(d)))
	sc := 1 / f.upem
	if nc >= 0 {
		if 10+2*nc+2 > len(d) {
			return g
		}
		ends := make([]int, nc)
		for i := range ends {
			ends[i] = int(binary.BigEndian.Uint16(d[10+2*i:]))
		}
		np := 0
		if nc > 0 {
			np = ends[nc-1] + 1
		}
		il := int(binary.BigEndian.Uint16(d[10+2*nc:]))
		p := 12 + 2*nc + il
		flags := make([]byte, 0, np)
		for len(flags) < np && p < len(d) {
			fl := d[p]
			p++
			flags = append(flags, fl)
			if fl&8 != 0 && p < len(d) {
				r := int(d[p])
				p++
				for k := 0; k < r; k++ {
					flags = append(flags, fl)
				}
			}
		}
		if len(flags) < np {
			return g
		}
		xs, ys := make([]float64, np), make([]float64, np)
		v := 0
		for i := 0; i < np; i++ {
			fl := flags[i]
			if fl&2 != 0 {
				if p >= len(d) {
					return g
				}
				dx := int(d[p])
				p++
				if fl&16 == 0 {
					dx = -dx
				}
				v += dx
			} else if fl&16 == 0 {
				if p+2 > len(d) {
					return g
				}
				v += int(int16(binary.BigEndian.Uint16(d[p:])))
				p += 2
			}
			xs[i] = float64(v)
		}
		v = 0
		for i := 0; i < np; i++ {
			fl := flags[i]
			if fl&4 != 0 {
				if p >= len(d) {
					return g
				}
				dy := int(d[p])
				p++
				if fl&32 == 0 {
					dy = -dy
				}
				v += dy
			} else if fl&32 == 0 {
				if p+2 > len(d) {
					return g
				}
				v += int(int16(binary.BigEndian.Uint16(d[p:])))
				p += 2
			}
			ys[i] = float64(v)
		}
		start := 0
		for _, end := range ends {
			if end < start || end >= np {
				break
			}
			n := end - start + 1
			on := func(i int) bool { return flags[start+(i%n)]&1 != 0 }
			pt := func(i int) (float64, float64) { k := start + (i % n); return xs[k] * sc, ys[k] * sc }
			// начальная точка — первая точка «на контуре» (или середина двух внеконтурных)
			first := -1
			for i := 0; i < n; i++ {
				if on(i) {
					first = i
					break
				}
			}
			var sx, sy float64
			if first < 0 {
				x0, y0 := pt(0)
				x1, y1 := pt(1)
				sx, sy = (x0+x1)/2, (y0+y1)/2
				first = 0
			} else {
				sx, sy = pt(first)
			}
			g.moveTo(sx, sy)
			var cx, cy float64
			hasC := false
			for k := 1; k <= n; k++ {
				i := first + k
				x, y := pt(i)
				if on(i) {
					if hasC {
						g.quadTo(cx, cy, x, y)
						hasC = false
					} else {
						g.lineTo(x, y)
					}
				} else {
					if hasC {
						mx, my := (cx+x)/2, (cy+y)/2
						g.quadTo(cx, cy, mx, my)
					}
					cx, cy, hasC = x, y, true
				}
			}
			if hasC {
				g.quadTo(cx, cy, sx, sy)
			}
			g.close()
			start = end + 1
		}
		return g
	}
	// составной глиф
	p := 10
	for p+4 <= len(d) {
		fl := int(binary.BigEndian.Uint16(d[p:]))
		gi := int(binary.BigEndian.Uint16(d[p+2:]))
		p += 4
		var dx, dy float64
		if fl&1 != 0 {
			if p+4 > len(d) {
				break
			}
			dx, dy = float64(int16(binary.BigEndian.Uint16(d[p:]))), float64(int16(binary.BigEndian.Uint16(d[p+2:])))
			p += 4
		} else {
			if p+2 > len(d) {
				break
			}
			dx, dy = float64(int8(d[p])), float64(int8(d[p+1]))
			p += 2
		}
		a, b, c, dd := 1.0, 0.0, 0.0, 1.0
		f2 := func(o int) float64 { return float64(int16(binary.BigEndian.Uint16(d[o:]))) / 16384 }
		switch {
		case fl&8 != 0 && p+2 <= len(d):
			a = f2(p)
			dd = a
			p += 2
		case fl&0x40 != 0 && p+4 <= len(d):
			a, dd = f2(p), f2(p+2)
			p += 4
		case fl&0x80 != 0 && p+8 <= len(d):
			a, b, c, dd = f2(p), f2(p+2), f2(p+4), f2(p+6)
			p += 8
		}
		sub := f.glyph(gi, depth+1)
		k := 0
		for _, op := range sub.ops {
			n := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3, 'Z': 0}[op]
			g.ops = append(g.ops, op)
			for j := 0; j < n; j++ {
				x, y := sub.pts[k], sub.pts[k+1]
				k += 2
				g.pts = append(g.pts, a*x+c*y+dx*sc, b*x+dd*y+dy*sc)
			}
		}
		if fl&0x20 == 0 {
			break
		}
	}
	return g
}

// ---------------------------------------------------------------- CFF
type cffFont struct {
	data     []byte
	charStr  [][]byte
	gsubrs   [][]byte
	subrs    [][]byte
	fdSubrs  [][][]byte
	fdSelect []byte // gid → fd
	charset  []int  // gid → sid/cid
	encoding map[int]int
	names    map[string]int
	cidToGid map[int]int
	isCID    bool
	matrix   [6]float64
	strings  []string
}

func cffIndex(b []byte, p int) ([][]byte, int) {
	if p+2 > len(b) {
		return nil, len(b)
	}
	n := int(binary.BigEndian.Uint16(b[p:]))
	if n == 0 {
		return nil, p + 2
	}
	if p+3 > len(b) {
		return nil, len(b)
	}
	os := int(b[p+2])
	p += 3
	offs := make([]int, n+1)
	for i := 0; i <= n; i++ {
		v := 0
		for k := 0; k < os; k++ {
			if p >= len(b) {
				return nil, len(b)
			}
			v = v<<8 | int(b[p])
			p++
		}
		offs[i] = v
	}
	base := p - 1
	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		s, e := base+offs[i], base+offs[i+1]
		if s < 0 || e > len(b) || s > e {
			out[i] = nil
			continue
		}
		out[i] = b[s:e]
	}
	return out, base + offs[n]
}

func cffDict(b []byte) map[int][]float64 {
	d := map[int][]float64{}
	var st []float64
	for i := 0; i < len(b); {
		c := int(b[i])
		switch {
		case c <= 21:
			op := c
			i++
			if c == 12 && i < len(b) {
				op = 1200 + int(b[i])
				i++
			}
			d[op] = st
			st = nil
		case c == 28 && i+2 < len(b):
			st = append(st, float64(int16(binary.BigEndian.Uint16(b[i+1:]))))
			i += 3
		case c == 29 && i+4 < len(b):
			st = append(st, float64(int32(binary.BigEndian.Uint32(b[i+1:]))))
			i += 5
		case c == 30:
			i++
			var s strings.Builder
		real:
			for i < len(b) {
				for _, nib := range []int{int(b[i] >> 4), int(b[i] & 15)} {
					switch {
					case nib <= 9:
						s.WriteByte(byte('0' + nib))
					case nib == 0xa:
						s.WriteByte('.')
					case nib == 0xb:
						s.WriteByte('E')
					case nib == 0xc:
						s.WriteString("E-")
					case nib == 0xe:
						s.WriteByte('-')
					case nib == 0xf:
						i++
						break real
					}
				}
				i++
			}
			v, _ := strconv.ParseFloat(s.String(), 64)
			st = append(st, v)
		case c >= 32 && c <= 246:
			st = append(st, float64(c-139))
			i++
		case c >= 247 && c <= 250 && i+1 < len(b):
			st = append(st, float64((c-247)*256+int(b[i+1])+108))
			i += 2
		case c >= 251 && c <= 254 && i+1 < len(b):
			st = append(st, float64(-(c-251)*256-int(b[i+1])-108))
			i += 2
		default:
			i++
		}
	}
	return d
}

func parseCFF(b []byte) *cffFont {
	if len(b) < 4 {
		return nil
	}
	f := &cffFont{data: b, matrix: [6]float64{0.001, 0, 0, 0.001, 0, 0}}
	p := int(b[2])
	_, p = cffIndex(b, p) // имена
	tops, p := cffIndex(b, p)
	strs, p := cffIndex(b, p)
	for _, s := range strs {
		f.strings = append(f.strings, string(s))
	}
	f.gsubrs, _ = cffIndex(b, p)
	if len(tops) == 0 {
		return nil
	}
	top := cffDict(tops[0])
	if m := top[1207]; len(m) == 6 {
		copy(f.matrix[:], m)
	}
	if cs := top[17]; len(cs) > 0 {
		f.charStr, _ = cffIndex(b, int(cs[0]))
	}
	n := len(f.charStr)
	readPrivate := func(d map[int][]float64) [][]byte {
		pr := d[18]
		if len(pr) < 2 {
			return nil
		}
		sz, off := int(pr[0]), int(pr[1])
		if off+sz > len(b) || off < 0 {
			return nil
		}
		pd := cffDict(b[off : off+sz])
		if s := pd[19]; len(s) > 0 {
			subs, _ := cffIndex(b, off+int(s[0]))
			return subs
		}
		return nil
	}
	f.subrs = readPrivate(top)
	_, f.isCID = top[1230]
	if f.isCID {
		if fa := top[1236]; len(fa) > 0 {
			fds, _ := cffIndex(b, int(fa[0]))
			for _, fd := range fds {
				f.fdSubrs = append(f.fdSubrs, readPrivate(cffDict(fd)))
			}
		}
		if fs := top[1237]; len(fs) > 0 {
			f.fdSelect = make([]byte, n)
			o := int(fs[0])
			if o < len(b) {
				switch b[o] {
				case 0:
					copy(f.fdSelect, b[min(len(b), o+1):])
				case 3:
					if o+3 <= len(b) {
						nr := int(binary.BigEndian.Uint16(b[o+1:]))
						for r := 0; r < nr; r++ {
							q := o + 3 + 3*r
							if q+5 > len(b) {
								break
							}
							first := int(binary.BigEndian.Uint16(b[q:]))
							fd := b[q+2]
							next := int(binary.BigEndian.Uint16(b[q+3:]))
							for g := first; g < next && g < n; g++ {
								f.fdSelect[g] = fd
							}
						}
					}
				}
			}
		}
	}
	// charset
	f.charset = make([]int, n)
	if cs := top[15]; len(cs) > 0 && cs[0] > 2 {
		o := int(cs[0])
		if o < len(b) {
			fmtc := b[o]
			o++
			g := 1
			switch fmtc {
			case 0:
				for g < n && o+2 <= len(b) {
					f.charset[g] = int(binary.BigEndian.Uint16(b[o:]))
					g++
					o += 2
				}
			case 1, 2:
				for g < n && o+3 <= len(b) {
					first := int(binary.BigEndian.Uint16(b[o:]))
					var nl int
					if fmtc == 1 {
						nl = int(b[o+2])
						o += 3
					} else {
						if o+4 > len(b) {
							break
						}
						nl = int(binary.BigEndian.Uint16(b[o+2:]))
						o += 4
					}
					for k := 0; k <= nl && g < n; k++ {
						f.charset[g] = first + k
						g++
					}
				}
			}
		}
	} else {
		for g := range f.charset {
			f.charset[g] = g // ISOAdobe: sid == gid
		}
	}
	if f.isCID {
		f.cidToGid = map[int]int{}
		for g, c := range f.charset {
			f.cidToGid[c] = g
		}
	} else {
		f.names = map[string]int{}
		for g, sid := range f.charset {
			f.names[f.sidName(sid)] = g
		}
		// встроенная кодировка
		f.encoding = map[int]int{}
		if en := top[16]; len(en) > 0 && en[0] > 1 {
			o := int(en[0])
			if o+2 <= len(b) {
				fm := b[o] & 0x7f
				cnt := int(b[o+1])
				switch fm {
				case 0:
					for k := 0; k < cnt && o+2+k < len(b); k++ {
						f.encoding[int(b[o+2+k])] = k + 1
					}
				case 1:
					g := 1
					for k := 0; k < cnt && o+2+2*k+1 < len(b); k++ {
						first, nl := int(b[o+2+2*k]), int(b[o+3+2*k])
						for c := first; c <= first+nl; c++ {
							f.encoding[c] = g
							g++
						}
					}
				}
			}
		} else {
			for c, nm := range standardEncoding {
				if nm != "" {
					if g, ok := f.names[nm]; ok {
						f.encoding[c] = g
					}
				}
			}
		}
	}
	return f
}

func (f *cffFont) sidName(sid int) string {
	if sid < len(cffStdStrings) {
		return cffStdStrings[sid]
	}
	if sid-len(cffStdStrings) < len(f.strings) {
		return f.strings[sid-len(cffStdStrings)]
	}
	return ""
}

func subrBias(n int) int {
	if n < 1240 {
		return 107
	}
	if n < 33900 {
		return 1131
	}
	return 32768
}

// glyph — контур по номеру глифа (Type2 charstring).
func (f *cffFont) glyph(gid int) *gpath {
	g := &gpath{}
	if gid < 0 || gid >= len(f.charStr) {
		return g
	}
	subrs := f.subrs
	if f.isCID && f.fdSelect != nil && gid < len(f.fdSelect) && int(f.fdSelect[gid]) < len(f.fdSubrs) {
		subrs = f.fdSubrs[f.fdSelect[gid]]
	}
	var st []float64
	x, y := 0.0, 0.0
	nStems := 0
	haveW := false
	open := false
	m := f.matrix
	tp := func(px, py float64) (float64, float64) { return m[0]*px + m[2]*py + m[4], m[1]*px + m[3]*py + m[5] }
	move := func(dx, dy float64) {
		if open {
			g.close()
		}
		x += dx
		y += dy
		g.moveTo(tp(x, y))
		open = true
	}
	line := func(dx, dy float64) {
		x += dx
		y += dy
		g.lineTo(tp(x, y))
	}
	curve := func(a, b, c, d, e, ff float64) {
		x1, y1 := x+a, y+b
		x2, y2 := x1+c, y1+d
		x, y = x2+e, y2+ff
		p1x, p1y := tp(x1, y1)
		p2x, p2y := tp(x2, y2)
		p3x, p3y := tp(x, y)
		g.curveTo(p1x, p1y, p2x, p2y, p3x, p3y)
	}
	stems := func() {
		if !haveW && len(st)%2 == 1 {
			st = st[1:]
		}
		haveW = true
		nStems += len(st) / 2
		st = st[:0]
	}
	var trans [32]float64
	var run func(code []byte, depth int) bool
	run = func(code []byte, depth int) bool {
		if depth > 10 {
			return true
		}
		for i := 0; i < len(code); {
			b0 := int(code[i])
			if b0 >= 32 || b0 == 28 {
				switch {
				case b0 == 28 && i+2 < len(code):
					st = append(st, float64(int16(binary.BigEndian.Uint16(code[i+1:]))))
					i += 3
				case b0 <= 246:
					st = append(st, float64(b0-139))
					i++
				case b0 <= 250 && i+1 < len(code):
					st = append(st, float64((b0-247)*256+int(code[i+1])+108))
					i += 2
				case b0 <= 254 && i+1 < len(code):
					st = append(st, float64(-(b0-251)*256-int(code[i+1])-108))
					i += 2
				case b0 == 255 && i+4 < len(code):
					st = append(st, float64(int32(binary.BigEndian.Uint32(code[i+1:])))/65536)
					i += 5
				default:
					return true
				}
				continue
			}
			i++
			switch b0 {
			case 1, 3, 18, 23: // hstem vstem hstemhm vstemhm
				stems()
			case 19, 20: // hintmask cntrmask
				if len(st) > 0 {
					stems()
				}
				haveW = true
				i += (nStems + 7) / 8
			case 21: // rmoveto
				if !haveW && len(st) > 2 {
					st = st[1:]
				}
				haveW = true
				if len(st) >= 2 {
					move(st[0], st[1])
				}
				st = st[:0]
			case 22: // hmoveto
				if !haveW && len(st) > 1 {
					st = st[1:]
				}
				haveW = true
				if len(st) >= 1 {
					move(st[0], 0)
				}
				st = st[:0]
			case 4: // vmoveto
				if !haveW && len(st) > 1 {
					st = st[1:]
				}
				haveW = true
				if len(st) >= 1 {
					move(0, st[0])
				}
				st = st[:0]
			case 5: // rlineto
				for k := 0; k+1 < len(st); k += 2 {
					line(st[k], st[k+1])
				}
				st = st[:0]
			case 6, 7: // hlineto vlineto
				h := b0 == 6
				for _, v := range st {
					if h {
						line(v, 0)
					} else {
						line(0, v)
					}
					h = !h
				}
				st = st[:0]
			case 8: // rrcurveto
				for k := 0; k+5 < len(st); k += 6 {
					curve(st[k], st[k+1], st[k+2], st[k+3], st[k+4], st[k+5])
				}
				st = st[:0]
			case 24: // rcurveline
				k := 0
				for ; k+5 < len(st)-2; k += 6 {
					curve(st[k], st[k+1], st[k+2], st[k+3], st[k+4], st[k+5])
				}
				if k+1 < len(st) {
					line(st[k], st[k+1])
				}
				st = st[:0]
			case 25: // rlinecurve
				k := 0
				for ; k+1 < len(st)-6; k += 2 {
					line(st[k], st[k+1])
				}
				if k+5 < len(st) {
					curve(st[k], st[k+1], st[k+2], st[k+3], st[k+4], st[k+5])
				}
				st = st[:0]
			case 26: // vvcurveto
				k := 0
				dx1 := 0.0
				if len(st)%2 == 1 {
					dx1 = st[0]
					k = 1
				}
				for ; k+3 < len(st); k += 4 {
					curve(dx1, st[k], st[k+1], st[k+2], 0, st[k+3])
					dx1 = 0
				}
				st = st[:0]
			case 27: // hhcurveto
				k := 0
				dy1 := 0.0
				if len(st)%2 == 1 {
					dy1 = st[0]
					k = 1
				}
				for ; k+3 < len(st); k += 4 {
					curve(st[k], dy1, st[k+1], st[k+2], st[k+3], 0)
					dy1 = 0
				}
				st = st[:0]
			case 30, 31: // vhcurveto hvcurveto
				h := b0 == 31
				k := 0
				for k+3 < len(st) {
					last := k+4 == len(st)-1
					if h {
						ex := 0.0
						if last {
							ex = st[k+4]
						}
						curve(st[k], 0, st[k+1], st[k+2], ex, st[k+3])
					} else {
						ex := 0.0
						if last {
							ex = st[k+4]
						}
						curve(0, st[k], st[k+1], st[k+2], st[k+3], ex)
					}
					k += 4
					if last {
						k++
					}
					h = !h
				}
				st = st[:0]
			case 10, 29: // callsubr callgsubr
				if len(st) == 0 {
					return true
				}
				set := subrs
				if b0 == 29 {
					set = f.gsubrs
				}
				idx := int(st[len(st)-1]) + subrBias(len(set))
				st = st[:len(st)-1]
				if idx >= 0 && idx < len(set) {
					if run(set[idx], depth+1) {
						return true
					}
				}
			case 11: // return
				return false
			case 14: // endchar
				if !haveW && (len(st) == 1 || len(st) == 5) {
					st = st[1:]
				}
				if len(st) >= 4 {
					// seac: базовая буква + надстрочный знак
					ach, bch := int(st[3]), int(st[2])
					adx, ady := st[0], st[1]
					if open {
						g.close()
						open = false
					}
					for _, part := range []struct {
						ch     int
						dx, dy float64
					}{{bch, 0, 0}, {ach, adx, ady}} {
						if part.ch < len(standardEncoding) {
							if gi, ok := f.names[standardEncoding[part.ch]]; ok {
								sub := f.glyph(gi)
								k := 0
								for _, op := range sub.ops {
									cnt := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3}[op]
									g.ops = append(g.ops, op)
									for j := 0; j < cnt; j++ {
										g.pts = append(g.pts, sub.pts[k]+part.dx*m[0], sub.pts[k+1]+part.dy*m[3])
										k += 2
									}
								}
							}
						}
					}
				}
				if open {
					g.close()
					open = false
				}
				return true
			case 12:
				if i >= len(code) {
					return true
				}
				e := int(code[i])
				i++
				pop := func() float64 {
					if len(st) == 0 {
						return 0
					}
					v := st[len(st)-1]
					st = st[:len(st)-1]
					return v
				}
				switch e {
				case 35: // flex
					if len(st) >= 12 {
						curve(st[0], st[1], st[2], st[3], st[4], st[5])
						curve(st[6], st[7], st[8], st[9], st[10], st[11])
					}
					st = st[:0]
				case 34: // hflex
					if len(st) >= 7 {
						curve(st[0], 0, st[1], st[2], st[3], 0)
						curve(st[4], 0, st[5], -st[2], st[6], 0)
					}
					st = st[:0]
				case 36: // hflex1
					if len(st) >= 9 {
						curve(st[0], st[1], st[2], st[3], st[4], 0)
						curve(st[5], 0, st[6], st[7], st[8], -(st[1] + st[3] + st[7]))
					}
					st = st[:0]
				case 37: // flex1
					if len(st) >= 11 {
						dx := st[0] + st[2] + st[4] + st[6] + st[8]
						dy := st[1] + st[3] + st[5] + st[7] + st[9]
						curve(st[0], st[1], st[2], st[3], st[4], st[5])
						if math.Abs(dx) > math.Abs(dy) {
							curve(st[6], st[7], st[8], st[9], st[10], -dy)
						} else {
							curve(st[6], st[7], st[8], st[9], -dx, st[10])
						}
					}
					st = st[:0]
				case 9:
					st = append(st, math.Abs(pop()))
				case 10:
					b, a := pop(), pop()
					st = append(st, a+b)
				case 11:
					b, a := pop(), pop()
					st = append(st, a-b)
				case 12:
					b, a := pop(), pop()
					if b != 0 {
						st = append(st, a/b)
					} else {
						st = append(st, 0)
					}
				case 14:
					st = append(st, -pop())
				case 24:
					b, a := pop(), pop()
					st = append(st, a*b)
				case 26:
					st = append(st, math.Sqrt(math.Abs(pop())))
				case 18:
					pop()
				case 27:
					v := pop()
					st = append(st, v, v)
				case 28:
					b, a := pop(), pop()
					st = append(st, b, a)
				case 20:
					i2 := int(pop())
					v := pop()
					if i2 >= 0 && i2 < 32 {
						trans[i2] = v
					}
				case 21:
					i2 := int(pop())
					if i2 >= 0 && i2 < 32 {
						st = append(st, trans[i2])
					}
				case 23:
					st = append(st, 0.5)
				default:
					st = st[:0]
				}
			default:
				st = st[:0]
			}
		}
		return false
	}
	run(f.charStr[gid], 0)
	if open {
		g.close()
	}
	return g
}

// ---------------------------------------------------------------- Type1 (eexec)
type type1Font struct {
	chars  map[string][]byte
	subrs  [][]byte
	enc    map[int]string
	matrix [6]float64
	lenIV  int
}

func t1decrypt(b []byte, r uint16, skip int) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = c ^ byte(r>>8)
		r = (uint16(c)+r)*52845 + 22719
	}
	if skip > len(out) {
		return nil
	}
	return out[skip:]
}

func parseType1(data []byte, len1, len2 int) *type1Font {
	// формат PFB (сегменты 0x80)
	if len(data) > 6 && data[0] == 0x80 {
		var clear, enc []byte
		for p := 0; p+6 <= len(data) && data[p] == 0x80; {
			t := data[p+1]
			l := int(binary.LittleEndian.Uint32(data[p+2:]))
			p += 6
			if t == 3 || p+l > len(data) {
				break
			}
			if t == 1 && enc == nil {
				clear = append(clear, data[p:p+l]...)
			} else {
				enc = append(enc, data[p:p+l]...)
			}
			p += l
		}
		data = append(clear, enc...)
		len1, len2 = len(clear), len(enc)
	}
	if len1 <= 0 || len1 > len(data) {
		len1 = bytes.Index(data, []byte("eexec"))
		if len1 < 0 {
			return nil
		}
		len1 += 5
		for len1 < len(data) && isWS(data[len1]) {
			len1++
		}
	}
	f := &type1Font{chars: map[string][]byte{}, enc: map[int]string{}, matrix: [6]float64{0.001, 0, 0, 0.001, 0, 0}, lenIV: 4}
	clear := data[:len1]
	// кодировка
	if bytes.Contains(clear, []byte("StandardEncoding def")) {
		for c, n := range standardEncoding {
			if n != "" {
				f.enc[c] = n
			}
		}
	}
	for _, line := range bytes.Split(clear, []byte("dup ")) {
		fs := strings.Fields(string(line))
		if len(fs) >= 3 && fs[2] == "put" && strings.HasPrefix(fs[1], "/") {
			if c, err := strconv.Atoi(fs[0]); err == nil {
				f.enc[c] = fs[1][1:]
			}
		}
	}
	if i := bytes.Index(clear, []byte("/FontMatrix")); i >= 0 {
		l := &pdfLexer{b: clear, p: i + 11}
		if o, err := l.object(); err == nil {
			if a, ok := o.(pdfArray); ok && len(a) == 6 {
				for k := range a {
					f.matrix[k], _ = a[k].(float64)
				}
			}
		}
	}
	enc := data[len1:]
	// шестнадцатеричный eexec
	if len(enc) >= 4 && hexv(enc[0]) >= 0 && hexv(enc[1]) >= 0 && hexv(enc[2]) >= 0 && hexv(enc[3]) >= 0 {
		enc = asciiHex(enc)
	}
	priv := t1decrypt(enc, 55665, 4)
	if i := bytes.Index(priv, []byte("/lenIV")); i >= 0 {
		l := &pdfLexer{b: priv, p: i + 6}
		if t, _ := l.token(); t != nil {
			if v, ok := t.(float64); ok {
				f.lenIV = int(v)
			}
		}
	}
	readBin := func(p int) (int, []byte, int) { // "<n> RD <bytes>"
		l := &pdfLexer{b: priv, p: p}
		t, _ := l.token()
		n, _ := t.(float64)
		_, _ = l.token() // RD или -|
		start := l.p + 1
		end := start + int(n)
		if end > len(priv) || n < 0 {
			return 0, nil, len(priv)
		}
		return int(n), priv[start:end], end
	}
	if i := bytes.Index(priv, []byte("/Subrs")); i >= 0 {
		l := &pdfLexer{b: priv, p: i + 6}
		t, _ := l.token()
		cnt, _ := t.(float64)
		f.subrs = make([][]byte, int(cnt))
		p := l.p
		for k := 0; k < int(cnt); k++ {
			j := bytes.Index(priv[p:], []byte("dup "))
			if j < 0 {
				break
			}
			l := &pdfLexer{b: priv, p: p + j + 4}
			t, _ := l.token()
			idx, _ := t.(float64)
			_, b, end := readBin(l.p)
			if int(idx) < len(f.subrs) && b != nil {
				f.subrs[int(idx)] = t1decrypt(b, 4330, f.lenIV)
			}
			p = end
		}
	}
	if i := bytes.Index(priv, []byte("/CharStrings")); i >= 0 {
		p := i + 12
		for {
			j := bytes.IndexByte(priv[p:], '/')
			if j < 0 {
				break
			}
			p += j + 1
			s := p
			for p < len(priv) && !isWS(priv[p]) && !isDelim(priv[p]) {
				p++
			}
			name := string(priv[s:p])
			if name == "" {
				continue
			}
			l := &pdfLexer{b: priv, p: p}
			t, _ := l.token()
			if _, ok := t.(float64); !ok {
				if k, ok := t.(pdfKeyword); ok && (k == "end" || k == "readonly") {
					break
				}
				continue
			}
			_, b, end := readBin(p)
			if b == nil {
				break
			}
			f.chars[name] = t1decrypt(b, 4330, f.lenIV)
			p = end
		}
	}
	return f
}

func (f *type1Font) glyph(name string) *gpath {
	g := &gpath{}
	code, ok := f.chars[name]
	if !ok {
		return g
	}
	m := f.matrix
	tp := func(px, py float64) (float64, float64) { return m[0]*px + m[2]*py + m[4], m[1]*px + m[3]*py + m[5] }
	var st, ps []float64
	x, y := 0.0, 0.0
	open := false
	flex := false
	var flexPts []float64
	move := func(nx, ny float64) {
		x, y = nx, ny
		if flex {
			flexPts = append(flexPts, x, y)
			return
		}
		if open {
			g.close()
		}
		g.moveTo(tp(x, y))
		open = true
	}
	curve := func(x1, y1, x2, y2, x3, y3 float64) {
		a, b := tp(x1, y1)
		c, d := tp(x2, y2)
		e, ff := tp(x3, y3)
		g.curveTo(a, b, c, d, e, ff)
		x, y = x3, y3
	}
	var run func(c []byte, depth int) bool
	run = func(c []byte, depth int) bool {
		if depth > 10 {
			return true
		}
		for i := 0; i < len(c); {
			v := int(c[i])
			if v >= 32 {
				switch {
				case v <= 246:
					st = append(st, float64(v-139))
					i++
				case v <= 250 && i+1 < len(c):
					st = append(st, float64((v-247)*256+int(c[i+1])+108))
					i += 2
				case v <= 254 && i+1 < len(c):
					st = append(st, float64(-(v-251)*256-int(c[i+1])-108))
					i += 2
				case i+4 < len(c):
					st = append(st, float64(int32(binary.BigEndian.Uint32(c[i+1:]))))
					i += 5
				default:
					return true
				}
				continue
			}
			i++
			arg := func(k int) float64 {
				if k < len(st) {
					return st[k]
				}
				return 0
			}
			switch v {
			case 13: // hsbw
				x, y = arg(0), 0
				st = st[:0]
			case 9: // closepath
				st = st[:0]
			case 21:
				move(x+arg(0), y+arg(1))
				st = st[:0]
			case 22:
				move(x+arg(0), y)
				st = st[:0]
			case 4:
				move(x, y+arg(0))
				st = st[:0]
			case 5:
				x, y = x+arg(0), y+arg(1)
				g.lineTo(tp(x, y))
				st = st[:0]
			case 6:
				x += arg(0)
				g.lineTo(tp(x, y))
				st = st[:0]
			case 7:
				y += arg(0)
				g.lineTo(tp(x, y))
				st = st[:0]
			case 8:
				x1, y1 := x+arg(0), y+arg(1)
				x2, y2 := x1+arg(2), y1+arg(3)
				curve(x1, y1, x2, y2, x2+arg(4), y2+arg(5))
				st = st[:0]
			case 30: // vhcurveto
				x1, y1 := x, y+arg(0)
				x2, y2 := x1+arg(1), y1+arg(2)
				curve(x1, y1, x2, y2, x2+arg(3), y2)
				st = st[:0]
			case 31: // hvcurveto
				x1, y1 := x+arg(0), y
				x2, y2 := x1+arg(1), y1+arg(2)
				curve(x1, y1, x2, y2, x2, y2+arg(3))
				st = st[:0]
			case 1, 3:
				st = st[:0]
			case 10: // callsubr
				if len(st) == 0 {
					return true
				}
				k := int(st[len(st)-1])
				st = st[:len(st)-1]
				if k >= 0 && k < len(f.subrs) {
					if run(f.subrs[k], depth+1) {
						return true
					}
				}
			case 11:
				return false
			case 14:
				if open {
					g.close()
					open = false
				}
				return true
			case 12:
				if i >= len(c) {
					return true
				}
				e := int(c[i])
				i++
				switch e {
				case 7: // sbw
					x, y = arg(0), arg(1)
					st = st[:0]
				case 6: // seac
					asb, adx, ady, bch, ach := arg(0), arg(1), arg(2), int(arg(3)), int(arg(4))
					st = st[:0]
					if open {
						g.close()
						open = false
					}
					for _, part := range []struct {
						ch     int
						dx, dy float64
					}{{bch, 0, 0}, {ach, adx - asb, ady}} {
						if part.ch < len(standardEncoding) {
							sub := f.glyph(standardEncoding[part.ch])
							k := 0
							for _, op := range sub.ops {
								cnt := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3}[op]
								g.ops = append(g.ops, op)
								for j := 0; j < cnt; j++ {
									g.pts = append(g.pts, sub.pts[k]+part.dx*m[0], sub.pts[k+1]+part.dy*m[3])
									k += 2
								}
							}
						}
					}
					return true
				case 12: // div
					if len(st) >= 2 {
						b, a := st[len(st)-1], st[len(st)-2]
						st = st[:len(st)-2]
						if b != 0 {
							st = append(st, a/b)
						} else {
							st = append(st, 0)
						}
					}
				case 16: // callothersubr
					if len(st) < 2 {
						st = st[:0]
						break
					}
					oth := int(st[len(st)-1])
					n := int(st[len(st)-2])
					st = st[:len(st)-2]
					args := []float64{}
					if n <= len(st) {
						args = append(args, st[len(st)-n:]...)
						st = st[:len(st)-n]
					}
					switch oth {
					case 1:
						flex = true
						flexPts = flexPts[:0]
					case 0:
						flex = false
						if len(flexPts) >= 14 {
							p := flexPts
							if open {
								// первая точка — опорная
							}
							curve(p[2], p[3], p[4], p[5], p[6], p[7])
							curve(p[8], p[9], p[10], p[11], p[12], p[13])
						}
						ps = append(ps, y, x)
					default:
						for k := len(args) - 1; k >= 0; k-- {
							ps = append(ps, args[k])
						}
					}
				case 17: // pop
					if len(ps) > 0 {
						st = append(st, ps[len(ps)-1])
						ps = ps[:len(ps)-1]
					}
				case 33: // setcurrentpoint
					if len(st) >= 2 {
						x, y = st[0], st[1]
					}
					st = st[:0]
				default:
					st = st[:0]
				}
			default:
				st = st[:0]
			}
		}
		return false
	}
	run(code, 0)
	if open {
		g.close()
	}
	return g
}

// ---------------------------------------------------------------- шрифт PDF
type pdfFont struct {
	kind     string // tt cff t1 sys t3
	name     string
	tt       *sfnt
	cff      *cffFont
	t1       *type1Font
	sysTT    *sfnt
	widths   map[int]float64
	defW     float64
	cid      bool
	codeLens []codeRange
	cidMap   map[int]int // код → CID (если не тождественно)
	cidToGid []int
	enc      [256]string
	hasEnc   bool
	symbolic bool
	toUni    map[int]string
	glyphs   map[int]*gpath
	vertical bool
	t3procs  pdfDict
	t3matrix [6]float64
	t3res    pdfDict
	bold     bool
	mu       sync.Mutex
}

type codeRange struct {
	n      int
	lo, hi uint32
}

var sysFontCache sync.Map

func systemFontDirs() []string {
	if runtime.GOOS == "windows" {
		w := os.Getenv("WINDIR")
		if w == "" {
			w = `C:\Windows`
		}
		return []string{filepath.Join(w, "Fonts"), filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\Windows\Fonts`)}
	}
	return []string{"/usr/share/fonts", "/usr/local/share/fonts"}
}

// findSystemFont — файл системного шрифта для нестандартного/невстроенного шрифта PDF.
func findSystemFont(base string, bold, italic bool) *sfnt {
	key := base + "|" + strconv.FormatBool(bold) + strconv.FormatBool(italic)
	if v, ok := sysFontCache.Load(key); ok {
		s, _ := v.(*sfnt)
		return s
	}
	lb := strings.ToLower(base)
	if i := strings.IndexByte(lb, '+'); i == 6 {
		lb = lb[7:]
	}
	fam := "sans"
	switch {
	case strings.Contains(lb, "times") || strings.Contains(lb, "serif") || strings.Contains(lb, "roman") || strings.Contains(lb, "georgia") || strings.Contains(lb, "cambria"):
		fam = "serif"
	case strings.Contains(lb, "courier") || strings.Contains(lb, "mono") || strings.Contains(lb, "consol"):
		fam = "mono"
	case strings.Contains(lb, "symbol"):
		fam = "symbol"
	}
	style := ""
	switch {
	case bold && italic:
		style = "bi"
	case bold:
		style = "b"
	case italic:
		style = "i"
	}
	var cands []string
	if runtime.GOOS == "windows" {
		clean := strings.NewReplacer("-", "", ",", "", " ", "", "bolditalic", "", "bold", "", "italic", "", "mt", "", "ps", "", "regular", "").Replace(lb)
		cands = append(cands, clean+".ttf")
		win := map[string][]string{
			"serif":  {"times.ttf", "timesbd.ttf", "timesi.ttf", "timesbi.ttf"},
			"sans":   {"arial.ttf", "arialbd.ttf", "ariali.ttf", "arialbi.ttf"},
			"mono":   {"cour.ttf", "courbd.ttf", "couri.ttf", "courbi.ttf"},
			"symbol": {"symbol.ttf", "symbol.ttf", "symbol.ttf", "symbol.ttf"},
		}[fam]
		if strings.Contains(lb, "calibri") {
			win = []string{"calibri.ttf", "calibrib.ttf", "calibrii.ttf", "calibriz.ttf"}
		} else if strings.Contains(lb, "cambria") {
			win = []string{"cambria.ttc", "cambriab.ttf", "cambriai.ttf", "cambriaz.ttf"}
		} else if strings.Contains(lb, "verdana") {
			win = []string{"verdana.ttf", "verdanab.ttf", "verdanai.ttf", "verdanaz.ttf"}
		} else if strings.Contains(lb, "tahoma") {
			win = []string{"tahoma.ttf", "tahomabd.ttf", "tahoma.ttf", "tahomabd.ttf"}
		}
		idx := map[string]int{"": 0, "b": 1, "i": 2, "bi": 3}[style]
		cands = append(cands, win[idx], win[0])
	} else {
		lin := map[string][]string{
			"serif":  {"LiberationSerif-Regular.ttf", "LiberationSerif-Bold.ttf", "LiberationSerif-Italic.ttf", "LiberationSerif-BoldItalic.ttf", "DejaVuSerif.ttf"},
			"sans":   {"LiberationSans-Regular.ttf", "LiberationSans-Bold.ttf", "LiberationSans-Italic.ttf", "LiberationSans-BoldItalic.ttf", "DejaVuSans.ttf"},
			"mono":   {"LiberationMono-Regular.ttf", "LiberationMono-Bold.ttf", "LiberationMono-Italic.ttf", "LiberationMono-BoldItalic.ttf", "DejaVuSansMono.ttf"},
			"symbol": {"DejaVuSans.ttf", "DejaVuSans.ttf", "DejaVuSans.ttf", "DejaVuSans.ttf", "DejaVuSans.ttf"},
		}[fam]
		idx := map[string]int{"": 0, "b": 1, "i": 2, "bi": 3}[style]
		cands = append(cands, lin[idx], lin[0], lin[4])
	}
	var found *sfnt
	for _, c := range cands {
		for _, dir := range systemFontDirs() {
			var path string
			if runtime.GOOS == "windows" {
				path = filepath.Join(dir, c)
				if _, err := os.Stat(path); err != nil {
					continue
				}
			} else {
				filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
					if err == nil && path == "" && !info.IsDir() && strings.EqualFold(info.Name(), c) {
						path = p
					}
					return nil
				})
				if path == "" {
					continue
				}
			}
			if b, err := os.ReadFile(path); err == nil {
				found = parseSFNT(b, 0)
				if found != nil {
					break
				}
			}
		}
		if found != nil {
			break
		}
	}
	sysFontCache.Store(key, found)
	return found
}

func (r *pdfRenderer) loadFont(v any) *pdfFont {
	d := r.f.dict(v)
	if d == nil {
		return nil
	}
	key := ""
	if ref, ok := v.(pdfRef); ok {
		key = strconv.Itoa(ref.N)
	} else {
		key = "p" + strconv.Itoa(len(r.doc.fonts))
	}
	r.doc.fontMu.Lock()
	if ft, ok := r.doc.fonts[key]; ok {
		r.doc.fontMu.Unlock()
		return ft
	}
	r.doc.fontMu.Unlock()
	f := r.f
	ft := &pdfFont{widths: map[int]float64{}, glyphs: map[int]*gpath{}, defW: 1000}
	sub := f.name(d["Subtype"])
	ft.name = f.name(d["BaseFont"])
	ln := strings.ToLower(ft.name)
	ft.bold = strings.Contains(ln, "bold") || strings.Contains(ln, "black") || strings.Contains(ln, "heavy")
	italic := strings.Contains(ln, "italic") || strings.Contains(ln, "oblique")
	desc := f.dict(d["FontDescriptor"])
	if sub == "Type0" {
		ft.cid = true
		descs := f.arr(d["DescendantFonts"])
		var cf pdfDict
		if len(descs) > 0 {
			cf = f.dict(descs[0])
		}
		if cf != nil {
			desc = f.dict(cf["FontDescriptor"])
			if dw := f.num(cf["DW"]); dw > 0 {
				ft.defW = dw
			}
			w := f.arr(cf["W"])
			for i := 0; i < len(w); {
				c0 := int(f.num(w[i]))
				if i+1 >= len(w) {
					break
				}
				if a, ok := f.Resolve(w[i+1]).(pdfArray); ok {
					for k, x := range a {
						ft.widths[c0+k] = f.num(x)
					}
					i += 2
				} else if i+2 < len(w) {
					c1 := int(f.num(w[i+1]))
					wv := f.num(w[i+2])
					for c := c0; c <= c1 && c-c0 < 70000; c++ {
						ft.widths[c] = wv
					}
					i += 3
				} else {
					break
				}
			}
			if m := f.Resolve(cf["CIDToGIDMap"]); m != nil {
				if s, ok := m.(*pdfStream); ok {
					b, _ := f.decodeStream(s, false)
					ft.cidToGid = make([]int, len(b)/2)
					for i := range ft.cidToGid {
						ft.cidToGid[i] = int(binary.BigEndian.Uint16(b[2*i:]))
					}
				}
			}
		}
		switch enc := f.Resolve(d["Encoding"]).(type) {
		case pdfName:
			ft.vertical = strings.HasSuffix(string(enc), "-V")
			ft.codeLens = []codeRange{{2, 0, 0xffff}}
		case *pdfStream:
			b, _ := f.decodeStream(enc, false)
			ft.parseCMap(b, false)
		}
		if len(ft.codeLens) == 0 {
			ft.codeLens = []codeRange{{2, 0, 0xffff}}
		}
	} else {
		ft.codeLens = []codeRange{{1, 0, 255}}
		fc := int(f.num(d["FirstChar"]))
		for i, x := range f.arr(d["Widths"]) {
			ft.widths[fc+i] = f.num(x)
		}
		if desc != nil {
			if mw := f.num(desc["MissingWidth"]); mw > 0 {
				ft.defW = mw
			}
		}
		if sub == "Type3" {
			ft.kind = "t3"
			ft.t3procs = f.dict(d["CharProcs"])
			ft.t3res = f.dict(d["Resources"])
			m := f.numArray(d["FontMatrix"])
			if len(m) == 6 {
				copy(ft.t3matrix[:], m)
			} else {
				ft.t3matrix = [6]float64{0.001, 0, 0, 0.001, 0, 0}
			}
		}
	}
	if desc != nil {
		flags := int(f.num(desc["Flags"]))
		ft.symbolic = flags&4 != 0
		if flags&0x40000 != 0 {
			ft.bold = true
		}
		if flags&64 != 0 {
			italic = true
		}
		if s := f.stream(desc["FontFile2"]); s != nil {
			b, _ := f.decodeStream(s, false)
			ft.tt = parseSFNT(b, 0)
			if ft.tt != nil {
				ft.kind = "tt"
			}
		} else if s := f.stream(desc["FontFile3"]); s != nil {
			b, _ := f.decodeStream(s, false)
			if f.name(s.Dict["Subtype"]) == "OpenType" {
				if t := parseSFNT(b, 0); t != nil {
					if c := t.tables["CFF "]; c != nil {
						ft.cff = parseCFF(c)
						ft.tt = t
					} else {
						ft.tt = t
						ft.kind = "tt"
					}
				}
			} else {
				ft.cff = parseCFF(b)
			}
			if ft.cff != nil {
				ft.kind = "cff"
			}
		} else if s := f.stream(desc["FontFile"]); s != nil {
			b, _ := f.decodeStream(s, false)
			ft.t1 = parseType1(b, int(f.num(s.Dict["Length1"])), int(f.num(s.Dict["Length2"])))
			if ft.t1 != nil && len(ft.t1.chars) > 0 {
				ft.kind = "t1"
			}
		}
	}
	// кодировка простых шрифтов
	if !ft.cid {
		base := standardEncoding
		builtin := false
		if ft.t1 != nil && len(ft.t1.enc) > 0 {
			for c, n := range ft.t1.enc {
				if c < 256 {
					ft.enc[c] = n
				}
			}
			builtin = true
		} else if ft.cff != nil && len(ft.cff.encoding) > 0 {
			for c, g := range ft.cff.encoding {
				if c < 256 && g < len(ft.cff.charset) {
					ft.enc[c] = ft.cff.sidName(ft.cff.charset[g])
				}
			}
			builtin = true
		}
		if strings.Contains(ln, "symbol") || strings.Contains(ln, "dingbat") {
			builtin = true
		}
		apply := func(tbl [256]string) {
			for c, n := range tbl {
				if n != "" {
					ft.enc[c] = n
				}
			}
		}
		switch e := f.Resolve(d["Encoding"]).(type) {
		case pdfName:
			ft.hasEnc = true
			switch e {
			case "WinAnsiEncoding":
				apply(winAnsiEncoding)
			case "MacRomanEncoding":
				apply(macRomanEncoding)
			default:
				apply(base)
			}
		case pdfDict:
			ft.hasEnc = true
			switch f.name(e["BaseEncoding"]) {
			case "WinAnsiEncoding":
				apply(winAnsiEncoding)
			case "MacRomanEncoding":
				apply(macRomanEncoding)
			case "StandardEncoding":
				apply(base)
			default:
				if !builtin {
					if ft.kind == "tt" && ft.symbolic {
					} else {
						apply(base)
					}
				}
			}
			code := 0
			for _, x := range f.arr(e["Differences"]) {
				switch v := f.Resolve(x).(type) {
				case float64:
					code = int(v)
				case pdfName:
					if code >= 0 && code < 256 {
						ft.enc[code] = string(v)
					}
					code++
				}
			}
		default:
			if !builtin && ft.kind != "tt" {
				apply(base)
			}
		}
	}
	if tu := f.stream(d["ToUnicode"]); tu != nil {
		b, _ := f.decodeStream(tu, false)
		tmp := &pdfFont{}
		tmp.parseCMap(b, true)
		ft.toUni = tmp.toUni
	}
	if ft.kind == "" {
		ft.kind = "sys"
		ft.sysTT = findSystemFont(ft.name, ft.bold, italic)
	}
	r.doc.fontMu.Lock()
	r.doc.fonts[key] = ft
	r.doc.fontMu.Unlock()
	return ft
}

// parseCMap — кодовые пространства и CID (uni=false) или ToUnicode (uni=true).
func (ft *pdfFont) parseCMap(b []byte, uni bool) {
	l := &pdfLexer{b: b}
	var stack []any
	hexInt := func(v any) (uint32, int) {
		s, ok := v.(pdfString)
		if !ok {
			return 0, 0
		}
		var x uint32
		for _, c := range s {
			x = x<<8 | uint32(c)
		}
		return x, len(s)
	}
	dst := func(v any) string {
		s, _ := v.(pdfString)
		var u []uint16
		for i := 0; i+1 < len(s); i += 2 {
			u = append(u, binary.BigEndian.Uint16(s[i:]))
		}
		if len(s) == 1 {
			return string(rune(s[0]))
		}
		return string(utf16.Decode(u))
	}
	if uni && ft.toUni == nil {
		ft.toUni = map[int]string{}
	}
	if !uni && ft.cidMap == nil {
		ft.cidMap = map[int]int{}
	}
	mode := ""
	for {
		t, err := l.object()
		if err != nil {
			break
		}
		kw, isKw := t.(pdfKeyword)
		if !isKw {
			if mode != "" {
				stack = append(stack, t)
			}
			continue
		}
		switch kw {
		case "begincodespacerange", "beginbfchar", "beginbfrange", "begincidrange", "begincidchar":
			mode = string(kw)
			stack = nil
		case "endcodespacerange":
			for i := 0; i+1 < len(stack); i += 2 {
				lo, n := hexInt(stack[i])
				hi, _ := hexInt(stack[i+1])
				if n > 0 {
					ft.codeLens = append(ft.codeLens, codeRange{n, lo, hi})
				}
			}
			mode = ""
		case "endbfchar":
			for i := 0; i+1 < len(stack); i += 2 {
				c, _ := hexInt(stack[i])
				if uni {
					ft.toUni[int(c)] = dst(stack[i+1])
				}
			}
			mode = ""
		case "endbfrange":
			for i := 0; i+2 < len(stack); i += 3 {
				lo, _ := hexInt(stack[i])
				hi, _ := hexInt(stack[i+1])
				if hi-lo > 65535 {
					continue
				}
				switch d := stack[i+2].(type) {
				case pdfString:
					base := []rune(dst(d))
					for c := lo; c <= hi; c++ {
						if uni && len(base) > 0 {
							rr := append([]rune{}, base...)
							rr[len(rr)-1] += rune(c - lo)
							ft.toUni[int(c)] = string(rr)
						}
					}
				case pdfArray:
					for k, x := range d {
						if uni && lo+uint32(k) <= hi {
							ft.toUni[int(lo)+k] = dst(x)
						}
					}
				}
			}
			mode = ""
		case "endcidrange":
			for i := 0; i+2 < len(stack); i += 3 {
				lo, _ := hexInt(stack[i])
				hi, _ := hexInt(stack[i+1])
				cid, _ := stack[i+2].(float64)
				if !uni && hi-lo < 70000 {
					for c := lo; c <= hi; c++ {
						ft.cidMap[int(c)] = int(cid) + int(c-lo)
					}
				}
			}
			mode = ""
		case "endcidchar":
			for i := 0; i+1 < len(stack); i += 2 {
				c, _ := hexInt(stack[i])
				cid, _ := stack[i+1].(float64)
				if !uni {
					ft.cidMap[int(c)] = int(cid)
				}
			}
			mode = ""
		}
	}
	if !uni && len(ft.cidMap) == 0 {
		ft.cidMap = nil
	}
}

// codes разбивает строку на коды символов.
func (ft *pdfFont) codes(s []byte) []int {
	var out []int
	for i := 0; i < len(s); {
		matched := false
		for n := 1; n <= 4 && !matched; n++ {
			if i+n > len(s) {
				break
			}
			var v uint32
			for k := 0; k < n; k++ {
				v = v<<8 | uint32(s[i+k])
			}
			for _, r := range ft.codeLens {
				if r.n == n && v >= r.lo && v <= r.hi {
					out = append(out, int(v))
					i += n
					matched = true
					break
				}
			}
		}
		if !matched {
			n := 1
			if ft.cid {
				n = 2
			}
			var v int
			for k := 0; k < n && i < len(s); k++ {
				v = v<<8 | int(s[i])
				i++
			}
			out = append(out, v)
		}
	}
	return out
}

func (ft *pdfFont) width(code int) float64 {
	c := code
	if ft.cid && ft.cidMap != nil {
		if v, ok := ft.cidMap[code]; ok {
			c = v
		}
	}
	if w, ok := ft.widths[c]; ok {
		return w
	}
	if !ft.cid && ft.sysTT != nil {
		return 0 // посчитаем по системному шрифту
	}
	return ft.defW
}

func (ft *pdfFont) unicode(code int) string {
	if s, ok := ft.toUni[code]; ok {
		return s
	}
	if !ft.cid && code < 256 {
		if n := ft.enc[code]; n != "" {
			if r := glyphNameToRune(n); r != 0 {
				return string(r)
			}
		}
		if code >= 32 && code < 127 {
			return string(rune(code))
		}
	}
	return ""
}

func glyphNameToRune(n string) rune {
	if r, ok := aglMap[n]; ok {
		return r
	}
	if i := strings.IndexByte(n, '.'); i > 0 {
		n = n[:i]
		if r, ok := aglMap[n]; ok {
			return r
		}
	}
	if strings.HasPrefix(n, "uni") && len(n) >= 7 {
		if v, err := strconv.ParseUint(n[3:7], 16, 32); err == nil {
			return rune(v)
		}
	}
	if strings.HasPrefix(n, "u") && len(n) >= 5 && len(n) <= 7 {
		if v, err := strconv.ParseUint(n[1:], 16, 32); err == nil {
			return rune(v)
		}
	}
	if strings.HasPrefix(n, "afii") {
		if v, err := strconv.Atoi(n[4:]); err == nil {
			switch {
			case v >= 10017 && v <= 10022:
				return rune(0x410 + v - 10017)
			case v == 10023:
				return 'Ё'
			case v >= 10024 && v <= 10049:
				return rune(0x416 + v - 10024)
			case v >= 10065 && v <= 10070:
				return rune(0x430 + v - 10065)
			case v == 10071:
				return 'ё'
			case v >= 10072 && v <= 10097:
				return rune(0x436 + v - 10072)
			case v == 61352:
				return '№'
			}
		}
	}
	if len(n) == 1 {
		return rune(n[0])
	}
	return 0
}

// glyphFor — контур символа (в долях em, без учёта FontMatrix Type3).
func (ft *pdfFont) glyphFor(code int) *gpath {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if g, ok := ft.glyphs[code]; ok {
		return g
	}
	var g *gpath
	switch ft.kind {
	case "tt":
		gid := 0
		if ft.cid {
			cid := code
			if ft.cidMap != nil {
				if v, ok := ft.cidMap[code]; ok {
					cid = v
				}
			}
			gid = cid
			if ft.cidToGid != nil {
				if cid < len(ft.cidToGid) {
					gid = ft.cidToGid[cid]
				}
			}
		} else {
			gid = ft.ttSimpleGID(code)
		}
		g = ft.tt.glyph(gid, 0)
	case "cff":
		gid := 0
		c := ft.cff
		if ft.cid {
			cid := code
			if ft.cidMap != nil {
				if v, ok := ft.cidMap[code]; ok {
					cid = v
				}
			}
			if c.isCID {
				gid = c.cidToGid[cid]
			} else {
				gid = cid
			}
		} else {
			name := ft.enc[code]
			if gi, ok := c.names[name]; ok && name != "" {
				gid = gi
			} else if gi, ok := c.encoding[code]; ok {
				gid = gi
			} else if c.isCID {
				gid = c.cidToGid[code]
			}
		}
		g = c.glyph(gid)
	case "t1":
		name := ft.enc[code]
		if name == "" {
			name = standardEncoding[code&255]
		}
		g = ft.t1.glyph(name)
	case "sys":
		if ft.sysTT != nil {
			r := []rune(ft.unicode(code))
			gid := 0
			if len(r) > 0 {
				for _, k := range [][2]int{{3, 1}, {3, 10}, {0, 3}, {0, 4}} {
					if t := ft.sysTT.cmaps[k]; t != nil {
						if gid = cmapLookup(t, int(r[0])); gid != 0 {
							break
						}
					}
				}
			}
			if gid == 0 {
				if t := ft.sysTT.cmaps[[2]int{3, 0}]; t != nil {
					gid = cmapLookup(t, 0xf000+code)
				}
			}
			g = ft.sysTT.glyph(gid, 0)
		}
	}
	if g == nil {
		g = &gpath{}
	}
	ft.glyphs[code] = g
	return g
}

// sysAdvance — ширина символа по системному шрифту (если в PDF ширин нет).
func (ft *pdfFont) sysAdvance(code int) float64 {
	if ft.sysTT == nil {
		return 0.5
	}
	h := ft.sysTT.tables["hhea"]
	hm := ft.sysTT.tables["hmtx"]
	if len(h) < 36 || hm == nil {
		return 0.5
	}
	r := []rune(ft.unicode(code))
	if len(r) == 0 {
		return 0.5
	}
	gid := 0
	if t := ft.sysTT.cmaps[[2]int{3, 1}]; t != nil {
		gid = cmapLookup(t, int(r[0]))
	}
	nh := int(binary.BigEndian.Uint16(h[34:]))
	if gid >= nh {
		gid = nh - 1
	}
	if 4*gid+2 > len(hm) || gid < 0 {
		return 0.5
	}
	return float64(binary.BigEndian.Uint16(hm[4*gid:])) / ft.sysTT.upem
}

func (ft *pdfFont) ttSimpleGID(code int) int {
	t := ft.tt
	if ms := t.cmaps[[2]int{3, 0}]; ms != nil && (ft.symbolic || !ft.hasEnc) {
		for _, c := range []int{code, 0xf000 + code, 0xf100 + code, 0xf200 + code} {
			if g := cmapLookup(ms, c); g != 0 {
				return g
			}
		}
	}
	if name := ft.enc[code]; name != "" {
		if r := glyphNameToRune(name); r != 0 {
			for _, k := range [][2]int{{3, 1}, {3, 10}, {0, 3}} {
				if u := t.cmaps[k]; u != nil {
					if g := cmapLookup(u, int(r)); g != 0 {
						return g
					}
				}
			}
		}
		if g, ok := t.postNames()[name]; ok {
			return g
		}
	}
	if u := t.cmaps[[2]int{3, 1}]; u != nil {
		if s := ft.toUni[code]; s != "" {
			if g := cmapLookup(u, int([]rune(s)[0])); g != 0 {
				return g
			}
		}
	}
	if m := t.cmaps[[2]int{1, 0}]; m != nil {
		if g := cmapLookup(m, code); g != 0 {
			return g
		}
	}
	if ms := t.cmaps[[2]int{3, 0}]; ms != nil {
		for _, c := range []int{code, 0xf000 + code} {
			if g := cmapLookup(ms, c); g != 0 {
				return g
			}
		}
	}
	if code < t.numG && ft.symbolic {
		return code
	}
	return 0
}
