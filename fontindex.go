package main

// Каталог шрифтов по заголовкам файлов (общий код; на Windows по нему ищутся шрифты для PDF).

import (
	"os"
	"strings"
)

type sysFontFace struct {
	path         string
	index        int
	bold, italic bool
}

func readAtN(f *os.File, off, n int) []byte {
	if n <= 0 || n > 4<<20 {
		return nil
	}
	b := make([]byte, n)
	if _, err := f.ReadAt(b, int64(off)); err != nil {
		return nil
	}
	return b
}

func indexFontFile(path string, add func(fam string, face sysFontFace)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	defer func() { recover() }()
	hdr := readAtN(f, 0, 12)
	if hdr == nil {
		return
	}
	bases := []int{0}
	if string(hdr[:4]) == "ttcf" {
		n := u32(hdr, 8)
		if n > 64 {
			return
		}
		offs := readAtN(f, 12, n*4)
		bases = bases[:0]
		for i := 0; i < n; i++ {
			bases = append(bases, u32(offs, i*4))
		}
	}
	for idx, base := range bases {
		dir := readAtN(f, base, 12)
		if dir == nil {
			continue
		}
		nt := u16(dir, 4)
		recs := readAtN(f, base+12, nt*16)
		tables := map[string][]byte{}
		for i := 0; i < nt && recs != nil; i++ {
			tag := string(recs[i*16 : i*16+4])
			if tag == "name" || tag == "OS/2" || tag == "head" {
				tables[tag] = readAtN(f, u32(recs, i*16+8), u32(recs, i*16+12))
			}
		}
		best, all := sfntNames(tables["name"])
		bold, italic := false, false
		if os2 := tables["OS/2"]; len(os2) >= 64 {
			sel := u16(os2, 62)
			italic = sel&1 != 0
			bold = sel&32 != 0 || u16(os2, 4) >= 600
		} else if head := tables["head"]; len(head) >= 46 {
			ms := u16(head, 44)
			bold, italic = ms&1 != 0, ms&2 != 0
		}
		face := sysFontFace{path: path, index: idx, bold: bold, italic: italic}
		seen := map[string]bool{}
		for _, id := range []int{1, 16} {
			names := append([]string{best[id]}, all[id]...)
			for _, nm := range names {
				nm = strings.ToLower(strings.TrimSpace(nm))
				if nm != "" && !seen[nm] {
					seen[nm] = true
					add(nm, face)
				}
			}
		}
	}
}
