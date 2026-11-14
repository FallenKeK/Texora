package main

// Чтение старого .doc (Word 97–2003): контейнер CFB + таблица кусков.
// Извлекаются текст, абзацы, разрывы страниц и таблицы.

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

type cfbFile struct {
	data       []byte
	ss, ms     int
	fat, mfat  []uint32
	entries    []cfbEntry
	mini       []byte
	miniCutoff uint32
}

type cfbEntry struct {
	name  string
	typ   byte
	start uint32
	size  uint32
}

func le32(b []byte, o int) uint32 { return binary.LittleEndian.Uint32(b[o:]) }
func le16(b []byte, o int) uint16 { return binary.LittleEndian.Uint16(b[o:]) }

func (c *cfbFile) chain(start uint32, table []uint32) []uint32 {
	var out []uint32
	seen := map[uint32]bool{}
	for s := start; s < 0xfffffffa && int(s) < len(table) && !seen[s]; s = table[s] {
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (c *cfbFile) sector(s uint32) []byte {
	o := (int(s) + 1) * c.ss
	if o+c.ss > len(c.data) {
		if o >= len(c.data) {
			return nil
		}
		return c.data[o:]
	}
	return c.data[o : o+c.ss]
}

func (c *cfbFile) readBig(start, size uint32) []byte {
	out := make([]byte, 0, size)
	for _, s := range c.chain(start, c.fat) {
		out = append(out, c.sector(s)...)
		if uint32(len(out)) >= size {
			break
		}
	}
	if uint32(len(out)) > size {
		out = out[:size]
	}
	return out
}

func openCFB(b []byte) (*cfbFile, error) {
	if len(b) < 512 || le32(b, 0) != 0xe011cfd0 {
		return nil, fmt.Errorf("это не файл Word 97–2003")
	}
	c := &cfbFile{data: b, ss: 1 << le16(b, 30), ms: 1 << le16(b, 32), miniCutoff: le32(b, 56)}
	nFat := int(le32(b, 44))
	var fatSecs []uint32
	for i := 0; i < 109 && len(fatSecs) < nFat; i++ {
		fatSecs = append(fatSecs, le32(b, 76+i*4))
	}
	difat := le32(b, 68)
	for len(fatSecs) < nFat && difat < 0xfffffffa {
		sec := c.sector(difat)
		if sec == nil {
			break
		}
		for i := 0; i < c.ss/4-1 && len(fatSecs) < nFat; i++ {
			fatSecs = append(fatSecs, le32(sec, i*4))
		}
		difat = le32(sec, c.ss-4)
	}
	for _, s := range fatSecs {
		sec := c.sector(s)
		for i := 0; i+4 <= len(sec); i += 4 {
			c.fat = append(c.fat, le32(sec, i))
		}
	}
	for _, s := range c.chain(le32(b, 48), c.fat) {
		sec := c.sector(s)
		for k := 0; k+128 <= len(sec); k += 128 {
			e := sec[k:]
			nl := int(le16(e, 64))
			var u []uint16
			for i := 0; i < nl/2-1 && i < 32; i++ {
				u = append(u, le16(e, i*2))
			}
			c.entries = append(c.entries, cfbEntry{name: string(utf16.Decode(u)), typ: e[66], start: le32(e, 116), size: le32(e, 120)})
		}
	}
	if len(c.entries) > 0 {
		root := c.entries[0]
		c.mini = c.readBig(root.start, root.size)
	}
	for _, s := range c.chain(le32(b, 60), c.fat) {
		sec := c.sector(s)
		for i := 0; i+4 <= len(sec); i += 4 {
			c.mfat = append(c.mfat, le32(sec, i))
		}
	}
	return c, nil
}

func (c *cfbFile) stream(name string) []byte {
	for _, e := range c.entries {
		if e.name != name || e.typ != 2 {
			continue
		}
		if e.size >= c.miniCutoff {
			return c.readBig(e.start, e.size)
		}
		out := make([]byte, 0, e.size)
		for _, s := range c.chain(e.start, c.mfat) {
			o := int(s) * c.ms
			if o+c.ms > len(c.mini) {
				break
			}
			out = append(out, c.mini[o:o+c.ms]...)
			if uint32(len(out)) >= e.size {
				break
			}
		}
		if uint32(len(out)) > e.size {
			out = out[:e.size]
		}
		return out
	}
	return nil
}

func ReadDoc(b []byte) (*Doc, error) {
	c, err := openCFB(b)
	if err != nil {
		return nil, err
	}
	wd := c.stream("WordDocument")
	if len(wd) < 0x200 {
		return nil, fmt.Errorf("в файле нет потока WordDocument")
	}
	if le16(wd, 0) != 0xa5ec {
		return nil, fmt.Errorf("неизвестная версия формата .doc")
	}
	flags := le16(wd, 0x0a)
	if flags&0x0100 != 0 {
		return nil, fmt.Errorf("документ .doc защищён паролем")
	}
	tname := "0Table"
	if flags&0x0200 != 0 {
		tname = "1Table"
	}
	tbl := c.stream(tname)
	if tbl == nil {
		return nil, fmt.Errorf("в файле нет таблицы документа")
	}
	p := 32
	csw := int(le16(wd, p))
	p += 2 + csw*2
	cslw := int(le16(wd, p))
	lw := p + 2
	ccpText := int(le32(wd, lw+3*4))
	p += 2 + cslw*4 + 2
	fcClx, lcbClx := int(le32(wd, p+33*8)), int(le32(wd, p+33*8+4))
	q := fcClx
	for q < fcClx+lcbClx && q < len(tbl) && tbl[q] == 0x01 {
		q += 3 + int(le16(tbl, q+1))
	}
	if q >= len(tbl) || tbl[q] != 0x02 {
		return nil, fmt.Errorf("не удалось прочитать структуру документа .doc")
	}
	lcb := int(le32(tbl, q+1))
	plc := q + 5
	n := (lcb - 4) / 12
	var text []rune
	for k := 0; k < n && len(text) < ccpText; k++ {
		cpA, cpB := int(le32(tbl, plc+k*4)), int(le32(tbl, plc+(k+1)*4))
		pcd := plc + (n+1)*4 + k*8
		fc := le32(tbl, pcd+2)
		ln := cpB - cpA
		if fc&0x40000000 != 0 {
			off := int(fc&^0x40000000) / 2
			if off+ln <= len(wd) {
				text = append(text, []rune(decodeCP(wd[off:off+ln], 1252))...)
			}
		} else {
			off := int(fc)
			var u []uint16
			for i := 0; i < ln && off+i*2+2 <= len(wd); i++ {
				u = append(u, le16(wd, off+i*2))
			}
			text = append(text, utf16.Decode(u)...)
		}
	}
	if len(text) > ccpText {
		text = text[:ccpText]
	}
	// поля: \x13 код \x14 результат \x15 — оставляем результат
	var clean []rune
	var mode []bool
	for _, ch := range text {
		switch ch {
		case 0x13:
			mode = append(mode, true)
			continue
		case 0x14:
			if len(mode) > 0 {
				mode[len(mode)-1] = false
			}
			continue
		case 0x15:
			if len(mode) > 0 {
				mode = mode[:len(mode)-1]
			}
			continue
		}
		inInstr := false
		for _, m := range mode {
			inInstr = inInstr || m
		}
		if !inInstr {
			clean = append(clean, ch)
		}
	}
	d := NewDoc()
	d.Def = Defaults{Font: "Times New Roman", Size: 12, Line: 1, After: 0}
	d.Body = nil
	pb := false
	mk := func(s []rune) *Para {
		p := NewPara()
		var cur []rune
		for _, ch := range s {
			switch ch {
			case 0x0b:
				if len(cur) > 0 {
					p.Runs = append(p.Runs, Run{T: RText, Text: cur})
					cur = nil
				}
				p.Runs = append(p.Runs, Run{T: RBreak})
			case 0x01, 0x02, 0x08, 0x1f:
			case 0x1e:
				cur = append(cur, '-')
			default:
				cur = append(cur, ch)
			}
		}
		if len(cur) > 0 {
			p.Runs = append(p.Runs, Run{T: RText, Text: cur})
		}
		p.PF.PB = pb
		pb = false
		p.Normalize()
		return p
	}
	var table *Table
	ncols := -1
	var row []*Cell
	var cell []Block
	var buf []rune
	for _, ch := range clean {
		switch ch {
		case '\r':
			if table != nil && len(row) > 0 {
				cell = append(cell, Block{P: mk(buf)})
			} else {
				table, ncols = nil, -1
				d.Body = append(d.Body, Block{P: mk(buf)})
			}
			buf = nil
		case 0x07:
			cell = append(cell, Block{P: mk(buf)})
			buf = nil
			empty := len(cell) == 1 && cell[0].P.Len() == 0
			if table == nil {
				table = &Table{Border: true}
				d.Body = append(d.Body, Block{T: table})
			}
			rowEnd := false
			if ncols < 0 {
				rowEnd = empty && len(row) > 0
			} else {
				rowEnd = len(row) >= ncols
			}
			if rowEnd {
				if ncols < 0 {
					ncols = len(row)
				}
				table.Rows = append(table.Rows, &Row{Cells: row})
				row = nil
			} else {
				row = append(row, &Cell{Blocks: cell, Span: 1})
			}
			cell = nil
		case 0x0c:
			if len(buf) > 0 {
				d.Body = append(d.Body, Block{P: mk(buf)})
				buf = nil
			}
			pb = true
		default:
			buf = append(buf, ch)
		}
	}
	if table != nil && len(row) > 0 {
		table.Rows = append(table.Rows, &Row{Cells: row})
	}
	if len(buf) > 0 {
		d.Body = append(d.Body, Block{P: mk(buf)})
	}
	for _, b := range d.Body {
		if b.T == nil {
			continue
		}
		m := 0
		for _, r := range b.T.Rows {
			m = max(m, len(r.Cells))
		}
		for _, r := range b.T.Rows {
			for len(r.Cells) < m {
				r.Cells = append(r.Cells, &Cell{Blocks: []Block{{P: NewPara()}}, Span: 1})
			}
		}
	}
	if len(d.Body) == 0 || d.Body[len(d.Body)-1].P == nil {
		d.Body = append(d.Body, Block{P: NewPara()})
	}
	return d, nil
}
