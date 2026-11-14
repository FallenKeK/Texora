package main

// Разбор шрифтов TrueType/OpenType (общий для всех ОС): метрики, cmap, глифы.
// Растеризация для Linux — в ttf.go, вшивание в PDF — в pdfout.go.

import (
	"encoding/binary"
	"math"
	"os"
)

func u16(b []byte, o int) int { return int(binary.BigEndian.Uint16(b[o:])) }
func s16(b []byte, o int) int { return int(int16(binary.BigEndian.Uint16(b[o:]))) }
func u32(b []byte, o int) int { return int(binary.BigEndian.Uint32(b[o:])) }

type ttFont struct {
	data             []byte
	tables           map[string][]byte
	upem             float64
	asc, desc        float64
	gap              float64 // внешний интерлиньяж: добавка к высоте строки сверх asc+desc
	numHMetrics      int
	hmtx, loca, glyf []byte
	longLoca         bool
	cmap             func(r rune) int
	cache            map[glyphKey]*glyphBmp
}

type glyphKey struct {
	gid int
	px  int // размер * 8
}

type glyphBmp struct {
	x0, y0, w, h int
	cov          []float32
}

func loadTTF(path string) *ttFont {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseTTF(data, 0)
}

// parseTTF разбирает TrueType/OpenType; index — номер шрифта в коллекции .ttc (в обычном файле игнорируется).
func parseTTF(data []byte, index int) (f *ttFont) {
	defer func() {
		if recover() != nil { // битый файл шрифта не должен ронять программу
			f = nil
		}
	}()
	if len(data) < 12 {
		return nil
	}
	base := 0
	if string(data[:4]) == "ttcf" {
		if index >= u32(data, 8) {
			return nil
		}
		base = u32(data, 12+index*4)
	}
	f = &ttFont{data: data, tables: map[string][]byte{}, cache: map[glyphKey]*glyphBmp{}}
	n := u16(data, base+4)
	for i := 0; i < n; i++ {
		o := base + 12 + i*16
		tag := string(data[o : o+4])
		off, ln := u32(data, o+8), u32(data, o+12)
		if off+ln <= len(data) {
			f.tables[tag] = data[off : off+ln]
		}
	}
	head, hhea, os2 := f.tables["head"], f.tables["hhea"], f.tables["OS/2"]
	if len(head) < 54 || len(hhea) < 36 || f.tables["cmap"] == nil {
		return nil
	}
	f.upem = float64(u16(head, 18))
	f.longLoca = s16(head, 50) == 1
	f.numHMetrics = u16(hhea, 34)
	f.asc, f.desc, f.gap = sfntLineMetrics(os2, hhea)
	f.hmtx, f.loca, f.glyf = f.tables["hmtx"], f.tables["loca"], f.tables["glyf"]
	f.cmap = f.parseCmap()
	return f
}

func (f *ttFont) parseCmap() func(rune) int {
	c := f.tables["cmap"]
	n := u16(c, 2)
	var best []byte
	for i := 0; i < n; i++ {
		pid, eid, off := u16(c, 4+i*8), u16(c, 6+i*8), u32(c, 8+i*8)
		sub := c[off:]
		format := u16(sub, 0)
		if pid == 3 && eid == 10 && format == 12 {
			best = sub
			break
		}
		if pid == 3 && eid == 1 && format == 4 && best == nil {
			best = sub
		}
		if pid == 0 && best == nil && (format == 4 || format == 12) {
			best = sub
		}
	}
	if best == nil {
		return func(rune) int { return 0 }
	}
	if u16(best, 0) == 12 {
		ng := u32(best, 12)
		return func(r rune) int {
			lo, hi := 0, ng-1
			for lo <= hi {
				m := (lo + hi) / 2
				g := 16 + m*12
				s, e := u32(best, g), u32(best, g+4)
				if int(r) < s {
					hi = m - 1
				} else if int(r) > e {
					lo = m + 1
				} else {
					return u32(best, g+8) + int(r) - s
				}
			}
			return 0
		}
	}
	segX2 := u16(best, 6)
	ends, starts, deltas, offs := 14, 16+segX2, 16+2*segX2, 16+3*segX2
	return func(r rune) int {
		if r > 0xffff {
			return 0
		}
		c := int(r)
		for i := 0; i < segX2/2; i++ {
			if c <= u16(best, ends+i*2) {
				s := u16(best, starts+i*2)
				if c < s {
					return 0
				}
				ro := u16(best, offs+i*2)
				if ro == 0 {
					return (c + s16(best, deltas+i*2)) & 0xffff
				}
				p := offs + i*2 + ro + (c-s)*2
				if p+2 > len(best) {
					return 0
				}
				g := u16(best, p)
				if g == 0 {
					return 0
				}
				return (g + s16(best, deltas+i*2)) & 0xffff
			}
		}
		return 0
	}
}

func (f *ttFont) advance(gid int) float64 {
	if gid >= f.numHMetrics {
		gid = f.numHMetrics - 1
	}
	return float64(u16(f.hmtx, gid*4))
}

func (f *ttFont) glyphData(gid int) []byte {
	var a, b int
	if f.longLoca {
		a, b = u32(f.loca, gid*4), u32(f.loca, gid*4+4)
	} else {
		a, b = u16(f.loca, gid*2)*2, u16(f.loca, gid*2+2)*2
	}
	if b <= a || b > len(f.glyf) {
		return nil
	}
	return f.glyf[a:b]
}

// sfntNames — строки таблицы name (платформа Windows, UTF-16BE): id → текст.
// Английские (0x409) важнее остальных, остальные языки складываются в all (для поиска по русскому имени семейства).
func sfntNames(name []byte) (best map[int]string, all map[int][]string) {
	best, all = map[int]string{}, map[int][]string{}
	defer func() { recover() }()
	if len(name) < 6 {
		return
	}
	n, strOff := u16(name, 2), u16(name, 4)
	for i := 0; i < n; i++ {
		o := 6 + i*12
		pid, eid, lang, id, ln, off := u16(name, o), u16(name, o+2), u16(name, o+4), u16(name, o+6), u16(name, o+8), u16(name, o+10)
		if pid != 3 || (eid != 1 && eid != 0 && eid != 10) {
			continue
		}
		b := name[strOff+off : strOff+off+ln]
		rs := make([]rune, 0, len(b)/2)
		for j := 0; j+1 < len(b); j += 2 {
			rs = append(rs, rune(u16(b, j)))
		}
		s := string(rs)
		all[id] = append(all[id], s)
		if _, ok := best[id]; !ok || lang == 0x409 {
			best[id] = s
		}
	}
	return
}

// sfntLineMetrics — высота строки шрифта так, как её считает Word (в единицах шрифта):
//   - шрифт с флагом USE_TYPO_METRICS (OS/2 fsSelection, бит 7): typoAscender, typoDescender, typoLineGap;
//   - иначе winAscent и winDescent плюс внешний интерлиньяж, как у GDI:
//     max(0, hhea.lineGap − ((winAscent + winDescent) − (hhea.ascender − hhea.descender))).
//
// Для Times New Roman это 1825 + 443 + 87 из 2048, то есть 1,15 кегля; без интерлиньяжа строки
// выходили на 4 % плотнее, чем в Word, и страницы расходились.
func sfntLineMetrics(os2, hhea []byte) (asc, desc, gap float64) {
	hAsc, hDesc, hGap := 0.0, 0.0, 0.0
	if len(hhea) >= 10 {
		hAsc, hDesc, hGap = float64(s16(hhea, 4)), -float64(s16(hhea, 6)), float64(s16(hhea, 8))
	}
	if len(os2) < 78 {
		return hAsc, hDesc, math.Max(0, hGap)
	}
	if u16(os2, 62)&0x80 != 0 {
		return float64(s16(os2, 68)), -float64(s16(os2, 70)), math.Max(0, float64(s16(os2, 72)))
	}
	asc, desc = float64(u16(os2, 74)), float64(u16(os2, 76))
	return asc, desc, math.Max(0, hGap-((asc+desc)-(hAsc+hDesc)))
}
