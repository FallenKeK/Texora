package main

// RTF <-> модель. Также используется для обмена через буфер с Word.

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

var rtfCharsetCP = map[int]int{0: 1252, 1: 1252, 204: 1251, 238: 1250, 161: 1253, 162: 1254, 186: 1257}

var rtfSkip = map[string]bool{"stylesheet": true, "info": true, "listtable": true, "listoverridetable": true, "revtbl": true,
	"rsidtbl": true, "generator": true, "xmlnstbl": true, "themedata": true, "colorschememapping": true, "latentstyles": true,
	"datastore": true, "mmathPr": true, "pgdsctbl": true, "fldtype": true, "nonshppict": true, "object": true, "bkmkstart": true,
	"bkmkend": true, "xe": true, "tc": true, "filetbl": true, "shpinst": true, "pnseclvl": true, "pntext": true, "userprops": true,
	"defchp": true, "defpap": true, "wgrffmtfilter": true, "aftncn": true, "aftnsep": true, "aftnsepc": true, "ftnsepc": true, "ftnsep": true, "ftncn": true}

type rtfState struct {
	f          F
	pf         PF
	dest       string
	uc         int
	font       int
	intbl      bool
	ignorable  bool
	storyStart string
	prevStory  *[]Block
	prevCur    *Para
	ls, ilvl   int
}

func decodeCP(b []byte, cp int) string {
	if cp == 1251 {
		return decodeCP1251(b)
	}
	var sb strings.Builder
	for _, c := range b {
		if c >= 0x80 && c < 0xa0 && cp == 1252 {
			sb.WriteRune([]rune("€\uFFFD‚ƒ„…†‡ˆ‰Š‹Œ\uFFFDŽ\uFFFD\uFFFD‘’“”•–—˜™š›œ\uFFFDžŸ")[c-0x80])
		} else {
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

var rtfDebug = false

func ReadRTF(data []byte) (*Doc, error) {
	src := string(data)
	doc := NewDoc()
	doc.Body = nil
	doc.Def.After, doc.Def.Line = 0, 1
	type font struct {
		name string
		cp   int
	}
	fonts := map[int]*font{}
	var colors []string
	ansiCp := 1252
	st := rtfState{uc: 1, font: -1, dest: "body"}
	var stack []rtfState
	storyArr := &doc.Body
	var cur *Para
	skip := 0
	pendingPB := false
	var rowCells []*Cell
	var cellBlocks []Block
	var table *Table
	var cellX []float64
	var fontTbl *font
	var cr, cg, cb int
	cset := false
	type pict struct {
		hex                  strings.Builder
		typ                  string
		wg, hg, w, h, sx, sy float64
	}
	var pc *pict
	type fld struct {
		inst    string
		special bool
		link    string
	}
	var fl *fld
	listText := ""

	para := func() *Para {
		if cur == nil {
			cur = &Para{PF: st.pf}
			if st.pf.List != nil {
				l := *st.pf.List
				cur.PF.List = &l
			}
			if pendingPB && storyArr == &doc.Body {
				cur.PF.PB = true
				pendingPB = false
			}
		}
		return cur
	}
	target := func() *[]Block {
		if st.intbl && storyArr == &doc.Body {
			return &cellBlocks
		}
		return storyArr
	}
	endPara := func() {
		p := para()
		if st.ls > 0 && p.PF.List == nil {
			kind := "ul"
			if strings.IndexFunc(listText, unicode.IsDigit) >= 0 || strings.IndexFunc(listText, unicode.IsLetter) >= 0 {
				kind = "ol"
			}
			p.PF.List = &List{ID: "r" + strconv.Itoa(st.ls), Kind: kind, Level: st.ilvl}
			p.PF.IndL, p.PF.IndF = Opt{}, Opt{}
		}
		listText = ""
		p.Normalize()
		if len(p.Runs) == 0 {
			p.Mark = st.f
		}
		if !st.intbl && table != nil && storyArr == &doc.Body {
			table = nil
		}
		t := target()
		*t = append(*t, Block{P: p})
		cur = nil
	}
	addText := func(t string) {
		if t == "" {
			return
		}
		switch st.dest {
		case "fldinst":
			if fl != nil {
				fl.inst += t
			}
			return
		case "listtext":
			listText += t
			return
		case "body", "fldrslt":
		default:
			return
		}
		if fl != nil && st.dest == "fldrslt" && fl.special {
			return
		}
		f := st.f
		para().Runs = append(para().Runs, Run{T: RText, Text: []rune(t), F: f})
	}
	var bytesBuf []byte
	flush := func() {
		if len(bytesBuf) > 0 {
			cp := ansiCp
			if fo, ok := fonts[st.font]; ok && fo.cp != 0 {
				cp = fo.cp
			}
			addText(decodeCP(bytesBuf, cp))
			bytesBuf = nil
		}
	}

	i, n := 0, len(src)
	for i < n {
		ch := src[i]
		switch ch {
		case '{':
			flush()
			stack = append(stack, st)
			st.ignorable = false
			i++
			continue
		case '}':
			flush()
			was := st
			if len(stack) > 0 {
				st = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			if was.dest == "pict" && st.dest != "pict" && pc != nil {
				hx := strings.Map(func(r rune) rune {
					if strings.ContainsRune("0123456789abcdefABCDEF", r) {
						return r
					}
					return -1
				}, pc.hex.String())
				if pc.typ != "" && len(hx) > 8 {
					b, err := hex.DecodeString(hx[:len(hx)/2*2])
					if err == nil {
						id := NewID("m")
						doc.Media[id] = &Media{Mime: pc.typ, Data: b}
						w, h := 100.0, 100.0
						if pc.wg > 0 {
							w = pc.wg / 20
						} else if pc.w > 0 {
							w = pc.w * 0.75
						}
						if pc.hg > 0 {
							h = pc.hg / 20
						} else if pc.h > 0 {
							h = pc.h * 0.75
						}
						sx, sy := 1.0, 1.0
						if pc.sx > 0 {
							sx = pc.sx / 100
						}
						if pc.sy > 0 {
							sy = pc.sy / 100
						}
						if st.dest == "body" || st.dest == "fldrslt" {
							para().Runs = append(para().Runs, Run{T: RImage, ID: id, W: w * sx, H: h * sy})
						}
					}
				}
				pc = nil
			}
			if was.dest == "fldinst" && st.dest != "fldinst" && fl != nil {
				fs := strings.Fields(fl.inst)
				name := ""
				if len(fs) > 0 {
					name = strings.ToUpper(fs[0])
				}
				switch name {
				case "PAGE", "NUMPAGES", "SECTIONPAGES":
					fl.special = true
					if name != "PAGE" {
						name = "NUMPAGES"
					}
					para().Runs = append(para().Runs, Run{T: RField, Name: name, F: st.f})
				case "HYPERLINK":
					if a := strings.Index(fl.inst, `"`); a >= 0 {
						if b := strings.Index(fl.inst[a+1:], `"`); b >= 0 {
							fl.link = fl.inst[a+1 : a+1+b]
						}
					}
				}
			}
			if was.dest == "field" && st.dest != "field" {
				fl = nil
			}
			if was.storyStart != "" && was.storyStart != st.storyStart {
				if cur != nil {
					endPara()
				}
				storyArr = was.prevStory
				cur = was.prevCur
			}
			i++
			continue
		case '\\':
			if i+1 >= n {
				i++
				continue
			}
			nx := src[i+1]
			if nx == '\'' && i+3 < n {
				v, err := strconv.ParseUint(src[i+2:i+4], 16, 8)
				i += 4
				if skip > 0 {
					skip--
					continue
				}
				if err == nil && (st.dest == "body" || st.dest == "fldrslt" || st.dest == "fldinst" || st.dest == "listtext") {
					bytesBuf = append(bytesBuf, byte(v))
				}
				continue
			}
			flush()
			switch nx {
			case '\\', '{', '}':
				if st.dest == "fontname" && fontTbl != nil {
					fontTbl.name += string(nx)
				} else {
					addText(string(nx))
				}
				i += 2
				continue
			case '~':
				addText("\u00a0")
				i += 2
				continue
			case '-':
				i += 2
				continue
			case '_':
				addText("-")
				i += 2
				continue
			case '*':
				st.ignorable = true
				i += 2
				continue
			case '\n', '\r':
				if st.dest == "body" {
					endPara()
				}
				i += 2
				continue
			}
			j := i + 1
			for j < n && ((src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z')) {
				j++
			}
			word := src[i+1 : j]
			hasP := false
			p := 0
			k := j
			if k < n && (src[k] == '-' || (src[k] >= '0' && src[k] <= '9')) {
				k++
				for k < n && src[k] >= '0' && src[k] <= '9' {
					k++
				}
				p, _ = strconv.Atoi(src[j:k])
				hasP = true
			}
			if k < n && src[k] == ' ' {
				k++
			}
			i = k
			if word == "" {
				continue
			}
			if st.dest == "skip" {
				continue
			}
			if st.dest == "pict" {
				switch word {
				case "pngblip":
					pc.typ = "image/png"
				case "jpegblip":
					pc.typ = "image/jpeg"
				case "picwgoal":
					pc.wg = float64(p)
				case "pichgoal":
					pc.hg = float64(p)
				case "picw":
					pc.w = float64(p)
				case "pich":
					pc.h = float64(p)
				case "picscalex":
					pc.sx = float64(p)
				case "picscaley":
					pc.sy = float64(p)
				case "bin":
					i += p
				}
				continue
			}
			on := !hasP || p != 0
			switch word {
			case "fonttbl":
				st.dest = "fonttbl"
			case "colortbl":
				st.dest = "colortbl"
				cr, cg, cb, cset = 0, 0, 0, false
			case "pict":
				st.dest = "pict"
				pc = &pict{}
			case "field":
				if st.dest == "body" {
					st.dest = "field"
				}
				fl = &fld{}
			case "fldinst":
				st.dest = "fldinst"
			case "fldrslt":
				st.dest = "fldrslt"
				if fl != nil && fl.link != "" {
					st.f.Link = fl.link
				}
			case "listtext":
				st.dest = "listtext"
			case "header", "headerr", "footer", "footerr", "headerf", "footerf", "headerl", "footerl":
				if strings.HasSuffix(word, "f") || strings.HasSuffix(word, "l") {
					st.dest = "skip"
					continue
				}
				arr := &doc.Header
				if strings.HasPrefix(word, "footer") {
					arr = &doc.Footer
				}
				*arr = nil
				st.storyStart, st.prevStory, st.prevCur = word, storyArr, cur
				storyArr, cur = arr, nil
				st.dest, st.intbl, st.pf = "body", false, PF{}
			case "footnote":
				id := NewID("f")
				note := &Note{}
				doc.Footnotes[id] = note
				para().Runs = append(para().Runs, Run{T: RNote, ID: id})
				st.storyStart, st.prevStory, st.prevCur = "fn"+id, storyArr, cur
				storyArr, cur = &note.Blocks, nil
				st.dest, st.intbl, st.pf = "body", false, PF{}
			case "chftn":
			case "ansicpg":
				ansiCp = p
			case "paperw":
				doc.Page.W = float64(p) / 20
			case "paperh":
				doc.Page.H = float64(p) / 20
			case "margl":
				doc.Page.Left = float64(p) / 20
			case "margr":
				doc.Page.Right = float64(p) / 20
			case "margt":
				doc.Page.Top = float64(p) / 20
			case "margb":
				doc.Page.Bottom = float64(p) / 20
			case "headery":
				doc.Page.Header = float64(p) / 20
			case "footery":
				doc.Page.Footer = float64(p) / 20
			case "titlepg":
				doc.TitlePg = true
			case "plain":
				st.f = F{}
				if fo, ok := fonts[0]; ok {
					_ = fo
				}
			case "b":
				st.f.B = on
			case "i":
				st.f.I = on
			case "ul", "uld", "ulw", "ulth", "uldash":
				st.f.U = on
			case "uldb":
				st.f.DU = on
			case "ulnone":
				st.f.U, st.f.DU = false, false
			case "strike", "striked":
				st.f.S = on
			case "caps":
				st.f.Caps = on
			case "super":
				st.f.Sup, st.f.Sub = true, false
			case "sub":
				st.f.Sub, st.f.Sup = true, false
			case "nosupersub":
				st.f.Sup, st.f.Sub = false, false
			case "fs":
				st.f.Size = float64(p) / 2
			case "f":
				if st.dest == "fonttbl" || st.dest == "fonttbl-entry" || st.dest == "fontname" {
					fontTbl = &font{}
					fonts[p] = fontTbl
					st.dest = "fontname"
				} else {
					st.font = p
					if fo, ok := fonts[p]; ok {
						st.f.Font = fo.name
					}
				}
			case "fcharset":
				if fontTbl != nil {
					fontTbl.cp = rtfCharsetCP[p]
				}
			case "cf":
				st.f.Color = ""
				if p > 0 && p < len(colors) {
					st.f.Color = colors[p]
				}
			case "highlight", "cb", "chcbpat":
				st.f.Bg = ""
				if p > 0 && p < len(colors) {
					st.f.Bg = colors[p]
				}
			case "red":
				cr, cset = p, true
			case "green":
				cg, cset = p, true
			case "blue":
				cb, cset = p, true
			case "u":
				c := p
				if c < 0 {
					c += 65536
				}
				addText(string(rune(c)))
				skip = st.uc
			case "uc":
				st.uc = p
			case "pard":
				st.pf, st.intbl, st.ls, st.ilvl = PF{}, false, 0, 0
			case "ql":
				st.pf.Align = ""
			case "qc":
				st.pf.Align = "center"
			case "qr":
				st.pf.Align = "right"
			case "qj":
				st.pf.Align = "justify"
			case "li":
				if p != 0 {
					st.pf.IndL = O(float64(p) / 20)
				}
			case "ri":
				st.pf.IndR = float64(p) / 20
			case "fi":
				if p != 0 {
					st.pf.IndF = O(float64(p) / 20)
				}
			case "sb":
				st.pf.Before = O(float64(p) / 20)
			case "sa":
				st.pf.After = O(float64(p) / 20)
			case "sl":
				if p > 0 {
					st.pf.Line = O(float64(p) / 240)
				} else if p < 0 {
					st.pf.LineExact, st.pf.LineRule = float64(-p)/20, "exact"
				}
			case "pagebb":
				st.pf.PB = true
			case "contextualspace":
				st.pf.CS = "x"
			case "keepn":
				st.pf.KeepNext = true
			case "outlinelevel":
				if p < 6 {
					st.pf.Heading = p + 1
				}
			case "ls":
				st.ls = p
			case "ilvl":
				st.ilvl = p
			case "intbl":
				st.intbl = true
			case "par":
				if st.dest == "body" || st.dest == "fldrslt" {
					endPara()
				}
			case "sect":
				if st.dest == "body" {
					endPara()
				}
			case "line":
				if st.dest == "body" || st.dest == "fldrslt" {
					para().Runs = append(para().Runs, Run{T: RBreak})
				}
			case "page":
				if st.dest == "body" {
					if cur != nil && len(cur.Runs) > 0 {
						endPara()
					}
					if cur != nil {
						cur.PF.PB = true
					} else {
						pendingPB = true
					}
				}
			case "tab":
				addText("\t")
			case "emdash":
				addText("—")
			case "endash":
				addText("–")
			case "bullet":
				addText("•")
			case "lquote":
				addText("‘")
			case "rquote":
				addText("’")
			case "ldblquote":
				addText("“")
			case "rdblquote":
				addText("”")
			case "trowd":
				cellX = nil
			case "cellx":
				cellX = append(cellX, float64(p)/20)
			case "cell":
				if cur != nil || len(cellBlocks) == 0 {
					endPara()
				}
				bs := cellBlocks
				if len(bs) == 0 {
					bs = []Block{{P: NewPara()}}
				}
				rowCells = append(rowCells, &Cell{Blocks: bs, Span: 1})
				cellBlocks = nil
			case "row":
				if table == nil {
					table = &Table{Border: true}
					for k, x := range cellX {
						prev := 0.0
						if k > 0 {
							prev = cellX[k-1]
						}
						if x-prev > 0 {
							table.Cols = append(table.Cols, x-prev)
						}
					}
					*storyArr = append(*storyArr, Block{T: table})
				}
				if len(rowCells) > 0 {
					table.Rows = append(table.Rows, &Row{Cells: rowCells})
				}
				rowCells = nil
			default:
				if st.ignorable || rtfSkip[word] {
					st.dest = "skip"
				}
			}
			continue
		}
		if ch == '\r' || ch == '\n' {
			i++
			continue
		}
		j := i
		for j < n && src[j] != '\\' && src[j] != '{' && src[j] != '}' && src[j] != '\r' && src[j] != '\n' {
			j++
		}
		text := src[i:j]
		i = j
		switch st.dest {
		case "fontname":
			fontTbl.name += text
			if k := strings.Index(fontTbl.name, ";"); k >= 0 {
				fontTbl.name = strings.TrimSpace(fontTbl.name[:k])
				st.dest = "fonttbl-entry"
			}
			continue
		case "colortbl":
			for _, c := range text {
				if c == ';' {
					if cset {
						colors = append(colors, fmt.Sprintf("#%02x%02x%02x", cr, cg, cb))
					} else {
						colors = append(colors, "")
					}
					cr, cg, cb, cset = 0, 0, 0, false
				}
			}
			continue
		case "pict":
			pc.hex.WriteString(text)
			continue
		case "skip", "fonttbl", "fonttbl-entry", "field":
			continue
		}
		if rtfDebug {
			fmt.Printf("TEXT dest=%s %q\n", st.dest, text[:min(30, len(text))])
		}
		if skip > 0 {
			k := min(skip, len(text))
			text = text[k:]
			skip -= k
		}
		bytesBuf = append(bytesBuf, []byte(text)...)
	}
	flush()
	if cur != nil && len(cur.Runs) > 0 {
		endPara()
	}
	if fo, ok := fonts[0]; ok && fo.name != "" {
		doc.Def.Font = fo.name
	}
	strip := func(bs []Block) {
		for _, en := range Flatten(&bs, nil, nil) {
			for k := range en.P.Runs {
				if en.P.Runs[k].F.Font == doc.Def.Font {
					en.P.Runs[k].F.Font = ""
				}
				if en.P.Runs[k].F.Size == 12 && doc.Def.Size == 12 {
					en.P.Runs[k].F.Size = 0
				}
			}
			en.P.Normalize()
		}
	}
	doc.Def.Size = 12
	strip(doc.Body)
	strip(doc.Header)
	strip(doc.Footer)
	if len(doc.Body) == 0 || doc.Body[len(doc.Body)-1].P == nil {
		doc.Body = append(doc.Body, Block{P: NewPara()})
	}
	return doc, nil
}

// ======================================================================== запись
func WriteRTF(d *Doc) []byte { return []byte(RTFString(d, d.Body, true)) }

func RTFString(d *Doc, body []Block, withPage bool) string {
	fonts := []string{d.Def.Font}
	var colors []string
	fontIdx := func(name string) int {
		for i, f := range fonts {
			if f == name {
				return i
			}
		}
		fonts = append(fonts, name)
		return len(fonts) - 1
	}
	colorIdx := func(c string) int {
		c = strings.ToLower(c)
		for i, x := range colors {
			if x == c {
				return i + 1
			}
		}
		colors = append(colors, c)
		return len(colors)
	}
	fnNum := map[string]int{}
	for i, id := range d.FootnoteOrder() {
		fnNum[id] = i + 1
	}
	twp := func(pt float64) int { return int(math.Round(pt * 20)) }
	esc := func(s string) string {
		var sb strings.Builder
		for _, ch := range s {
			switch {
			case ch == '\\' || ch == '{' || ch == '}':
				sb.WriteByte('\\')
				sb.WriteRune(ch)
			case ch == '\t':
				sb.WriteString(`\tab `)
			case ch < 128:
				sb.WriteRune(ch)
			case ch < 0x10000:
				v := int(ch)
				if v > 32767 {
					v -= 65536
				}
				fmt.Fprintf(&sb, `\u%d?`, v)
			default:
				for _, u := range []rune(string(ch)) {
					r1, r2 := 0xD800+((u-0x10000)>>10), 0xDC00+((u-0x10000)&0x3ff)
					fmt.Fprintf(&sb, `\u%d?\u%d?`, int(r1)-65536, int(r2)-65536)
				}
			}
		}
		return sb.String()
	}
	fmtF := func(f F) string {
		var sb strings.Builder
		if f.Font != "" {
			fmt.Fprintf(&sb, `\f%d`, fontIdx(f.Font))
		}
		size := f.Size
		if size == 0 {
			size = d.Def.Size
		}
		fmt.Fprintf(&sb, `\fs%d`, int(math.Round(size*2)))
		if f.B {
			sb.WriteString(`\b`)
		}
		if f.I {
			sb.WriteString(`\i`)
		}
		if f.DU {
			sb.WriteString(`\uldb`)
		} else if f.U || f.Link != "" {
			sb.WriteString(`\ul`)
		}
		if f.S {
			sb.WriteString(`\strike`)
		}
		if f.Caps {
			sb.WriteString(`\caps`)
		}
		if f.Sup {
			sb.WriteString(`\super`)
		}
		if f.Sub {
			sb.WriteString(`\sub`)
		}
		color := f.Color
		if color == "" && f.Link != "" {
			color = "#0563c1"
		}
		if color != "" {
			fmt.Fprintf(&sb, `\cf%d`, colorIdx(color))
		}
		if f.Bg != "" {
			fmt.Fprintf(&sb, `\chcbpat%d`, colorIdx(f.Bg))
		}
		return sb.String()
	}
	authors := []string{"Unknown"}
	authIdx := func(id string) int {
		a := "Texora"
		if rv := d.Revs[id]; rv != nil {
			a = rv.Author
		}
		for i, x := range authors {
			if x == a {
				return i
			}
		}
		authors = append(authors, a)
		return len(authors) - 1
	}
	cmtN := map[string]int{}
	cmtLast := map[string]cmtKey{}
	for _, en := range Flatten(&d.Body, nil, nil) {
		for i, r := range en.P.Runs {
			if r.F.Cmt != "" && d.Comments[r.F.Cmt] != nil {
				if _, ok := cmtN[r.F.Cmt]; !ok {
					cmtN[r.F.Cmt] = len(cmtN) + 1
				}
				cmtLast[r.F.Cmt] = cmtKey{en.P, i}
			}
		}
	}
	cmtOpen := map[string]bool{}
	var runs func(p *Para, inNote bool) string
	runs = func(p *Para, inNote bool) string {
		var sb strings.Builder
		if p.PF.Bookmark != "" {
			fmt.Fprintf(&sb, `{\*\bkmkstart %s}{\*\bkmkend %s}`, esc(p.PF.Bookmark), esc(p.PF.Bookmark))
		}
		for ri, r := range p.Runs {
			if c := r.F.Cmt; c != "" && cmtN[c] > 0 && !cmtOpen[c] {
				cmtOpen[c] = true
				fmt.Fprintf(&sb, `{\*\atrfstart %d}`, cmtN[c])
			}
			switch r.T {
			case RText:
				rev := ""
				if r.F.Ins != "" {
					rev += fmt.Sprintf(`\revised\revauth%d`, authIdx(r.F.Ins))
				}
				if r.F.Del != "" {
					rev += fmt.Sprintf(`\deleted\revauthdel%d`, authIdx(r.F.Del))
				}
				body := "{" + fmtF(r.F) + rev + " " + esc(string(r.Text)) + "}"
				if strings.HasPrefix(r.F.Link, "#") {
					body = "{" + fmtF(F{Size: r.F.Size, Font: r.F.Font, B: r.F.B, I: r.F.I, Color: r.F.Color}) + rev + " " + esc(string(r.Text)) + "}"
					fmt.Fprintf(&sb, `{\field{\*\fldinst HYPERLINK \\l "%s"}{\fldrslt %s}}`, esc(r.F.Link[1:]), body)
				} else if r.F.Link != "" {
					fmt.Fprintf(&sb, `{\field{\*\fldinst HYPERLINK "%s"}{\fldrslt %s}}`, esc(r.F.Link), body)
				} else {
					sb.WriteString(body)
				}
			case RBreak:
				sb.WriteString(`\line `)
			case RField:
				instr, res := r.Name, "1"
				if strings.HasPrefix(r.Name, "PAGEREF ") {
					instr += ` \h`
					res = d.FieldValue(r.Name, "1", "1")
				} else if strings.HasPrefix(r.Name, "CITE ") {
					instr = "ADDIN TEXORA_" + r.Name
					res = d.FieldValue(r.Name, "1", "1")
				} else if strings.HasPrefix(r.Name, "SEQ ") || strings.HasPrefix(r.Name, "REF ") {
					instr, res = "", d.FieldValue(r.Name, "1", "1")
				}
				ff := r.F
				ff.Link = ""
				if instr == "" {
					fmt.Fprintf(&sb, `{%s %s}`, fmtF(ff), esc(res))
				} else {
					fmt.Fprintf(&sb, `{\field{\*\fldinst %s}{\fldrslt {%s %s}}}`, esc(instr), fmtF(ff), esc(res))
				}
			case REq:
				fmt.Fprintf(&sb, `{%s\i %s}`, fmtF(F{Font: "Cambria Math", Size: r.F.Size}), esc(EqPlain(string(r.Text))))
			case RNote:
				if n, ok := d.Footnotes[r.ID]; ok && !inNote {
					sb.WriteString(`{\super\chftn}{\footnote\pard\plain\fs20 {\super\chftn} `)
					for k, b := range n.Blocks {
						if b.P != nil {
							if k > 0 {
								sb.WriteString(`\par `)
							}
							sb.WriteString(runs(b.P, true))
						}
					}
					sb.WriteString("}")
				}
			case RImage:
				m := d.Media[r.ID]
				if m == nil {
					continue
				}
				blip := ""
				switch m.Mime {
				case "image/png":
					blip = `\pngblip`
				case "image/jpeg":
					blip = `\jpegblip`
				}
				if blip == "" {
					continue
				}
				fmt.Fprintf(&sb, `{\pict%s\picwgoal%d\pichgoal%d `, blip, twp(r.W), twp(r.H))
				h := hex.EncodeToString(m.Data)
				for k := 0; k < len(h); k += 128 {
					sb.WriteString(h[k:min(len(h), k+128)])
					sb.WriteString("\n")
				}
				sb.WriteString("}")
			}
			if c := r.F.Cmt; c != "" && cmtLast[c] == (cmtKey{p, ri}) {
				cm := d.Comments[c]
				fmt.Fprintf(&sb, `{\*\atrfend %d}{\*\atnid %s}{\*\atnauthor %s}\chatn{\*\annotation{\*\atnref %d}\pard\plain %s}`,
					cmtN[c], esc(cm.Initials), esc(cm.Author), cmtN[c], esc(cm.Text))
			}
		}
		return sb.String()
	}
	lists := NewListState()
	listIdx := map[string]int{}
	var listOrder []*List
	pfmt := func(p *Para, intbl bool) string {
		pf := EffPF(d, p)
		if intbl {
			pf = CellPF(p, pf)
		}
		var sb strings.Builder
		sb.WriteString(`\pard\plain`)
		if intbl {
			sb.WriteString(`\intbl`)
		}
		sb.WriteString(map[string]string{"center": `\qc`, "right": `\qr`, "justify": `\qj`}[pf.Align])
		if pf.Align == "left" || pf.Align == "" {
			sb.WriteString(`\ql`)
		}
		if pf.IndL != 0 {
			fmt.Fprintf(&sb, `\li%d`, twp(pf.IndL))
		}
		if pf.IndR != 0 {
			fmt.Fprintf(&sb, `\ri%d`, twp(pf.IndR))
		}
		if pf.IndF != 0 {
			fmt.Fprintf(&sb, `\fi%d`, twp(pf.IndF))
		}
		fmt.Fprintf(&sb, `\sb%d\sa%d`, twp(pf.Before), twp(pf.After))
		if pf.LineExact > 0 {
			fmt.Fprintf(&sb, `\sl-%d\slmult0`, twp(pf.LineExact))
		} else {
			fmt.Fprintf(&sb, `\sl%d\slmult1`, int(math.Round(pf.Line*240)))
		}
		if pf.PB {
			sb.WriteString(`\pagebb`)
		}
		if pf.Keep {
			sb.WriteString(`\keepn`)
		}
		if CSKey(p) != "" {
			sb.WriteString(`\contextualspace`)
		}
		if p.PF.Heading > 0 {
			fmt.Fprintf(&sb, `\outlinelevel%d`, p.PF.Heading-1)
		}
		tabs := pf.Tabs
		if len(tabs) == 0 && strings.HasPrefix(p.PF.Style, "TOC") && p.PF.Style != "TOCHeading" {
			tabs = []TabStop{{Pos: d.Page.W - d.Page.Left - d.Page.Right, Align: "right", Leader: "dot"}}
		}
		for _, t := range tabs {
			sb.WriteString(map[string]string{"right": `\tqr`, "center": `\tqc`}[t.Align])
			if t.Leader == "dot" {
				sb.WriteString(`\tldot`)
			}
			fmt.Fprintf(&sb, `\tx%d`, twp(t.Pos))
		}
		sb.WriteString(" ")
		if l := p.PF.List; l != nil {
			key := l.Kind + ":" + l.ID
			n, ok := listIdx[key]
			if !ok {
				n = len(listOrder) + 1
				listIdx[key] = n
				listOrder = append(listOrder, l)
			}
			s := sb.String()
			sb.Reset()
			sb.WriteString(strings.TrimSuffix(s, " "))
			fmt.Fprintf(&sb, `\ls%d\ilvl%d {\listtext %s\tab }`, n, l.Level, esc(lists.Marker(l)))
		}
		return sb.String()
	}
	headF := func(p *Para) string {
		ef := EffF(d, p, F{})
		if p.PF.Heading == 0 && p.PF.Style == "" {
			return ""
		}
		g := F{Size: ef.Size, B: ef.B, I: ef.I, Color: ef.Color}
		if ef.Font != d.Def.Font {
			g.Font = ef.Font
		}
		return fmtF(g)
	}
	var sectAfter func(p *Para) string
	var blocks func(bs []Block, intbl bool, width float64) string
	blocks = func(bs []Block, intbl bool, width float64) string {
		var sb strings.Builder
		for _, b := range bs {
			if b.P != nil {
				h := headF(b.P)
				sb.WriteString(pfmt(b.P, intbl))
				if h != "" {
					sb.WriteString("{" + h + " ")
				}
				sb.WriteString(runs(b.P, false))
				if h != "" {
					sb.WriteString("}")
				}
				sb.WriteString("\\par\n")
				if b.P.PF.Sect != nil && !intbl && sectAfter != nil {
					sb.WriteString(sectAfter(b.P))
				}
				continue
			}
			t := b.T
			cols := TableCols(t, width)
			for _, row := range t.Rows {
				var def strings.Builder
				def.WriteString(`\trowd\trgaph108\trleft0`)
				x, gi := 0.0, 0
				for _, c := range row.Cells {
					span := max(1, c.Span)
					for k := gi; k < gi+span && k < len(cols); k++ {
						x += cols[k]
					}
					gi += span
					if t.Border {
						def.WriteString(`\clbrdrt\brdrs\brdrw10\clbrdrl\brdrs\brdrw10\clbrdrb\brdrs\brdrw10\clbrdrr\brdrs\brdrw10`)
					}
					if c.Shade != "" {
						fmt.Fprintf(&def, `\clcbpat%d`, colorIdx(c.Shade))
					}
					fmt.Fprintf(&def, `\cellx%d`, twp(x))
				}
				sb.WriteString(def.String() + "\n")
				for _, c := range row.Cells {
					ps := Flatten(&c.Blocks, nil, nil)
					for k, en := range ps {
						h := headF(en.P)
						sb.WriteString(pfmt(en.P, true))
						if h != "" {
							sb.WriteString("{" + h + " ")
						}
						sb.WriteString(runs(en.P, false))
						if h != "" {
							sb.WriteString("}")
						}
						if k < len(ps)-1 {
							sb.WriteString("\\par\n")
						} else {
							sb.WriteString("\\cell\n")
						}
					}
					if len(ps) == 0 {
						sb.WriteString("\\pard\\intbl\\cell\n")
					}
				}
				sb.WriteString(def.String() + "\\row\n")
			}
			sb.WriteString("\\pard\n")
		}
		return sb.String()
	}
	pg := d.Page
	ss := d.Sections()
	if withPage {
		pg = ss[0].Page
	}
	width := pg.W - pg.Left - pg.Right
	// свойства раздела i с колонтитулами
	sectProps := func(i int) string {
		sv := ss[i]
		sp := sv.Page
		var sb strings.Builder
		sb.WriteString(`\sectd`)
		switch sv.Type {
		case "continuous":
			sb.WriteString(`\sbknone`)
		case "oddPage":
			sb.WriteString(`\sbkodd`)
		case "evenPage":
			sb.WriteString(`\sbkeven`)
		}
		fmt.Fprintf(&sb, `\pgwsxn%d\pghsxn%d\marglsxn%d\margrsxn%d\margtsxn%d\margbsxn%d\headery%d\footery%d`,
			twp(sp.W), twp(sp.H), twp(sp.Left), twp(sp.Right), twp(sp.Top), twp(sp.Bottom), twp(sp.Header), twp(sp.Footer))
		if sp.W > sp.H {
			sb.WriteString(`\lndscpsxn`)
		}
		if sv.TitlePg {
			sb.WriteString(`\titlepg`)
		}
		if sv.Start > 0 {
			fmt.Fprintf(&sb, `\pgnrestart\pgnstarts%d`, sv.Start)
		}
		sb.WriteString("\n")
		w := sp.W - sp.Left - sp.Right
		for _, kind := range []string{"header", "footer"} {
			_, def := d.ResolveHF(ss, i, kind, "")
			if len(def) > 0 {
				tag := kind
				if d.EvenOdd {
					tag += "r"
				}
				sb.WriteString(`{\` + tag + ` ` + blocks(def, false, w) + "}")
			}
			if d.EvenOdd {
				if _, ev := d.ResolveHF(ss, i, kind, "even"); len(ev) > 0 {
					sb.WriteString(`{\` + kind + `l ` + blocks(ev, false, w) + "}")
				}
			}
			if sv.TitlePg {
				_, fi := d.ResolveHF(ss, i, kind, "first")
				if len(fi) == 0 {
					sb.WriteString(`{\` + kind + `f \pard\par}`)
				} else {
					sb.WriteString(`{\` + kind + `f ` + blocks(fi, false, w) + "}")
				}
			}
		}
		return sb.String()
	}
	if withPage {
		sectAfter = func(p *Para) string {
			for i, sv := range ss {
				if sv.Owner == p.PF.Sect && i+1 < len(ss) {
					return `\sect` + sectProps(i+1)
				}
			}
			return ""
		}
	}
	hf := ""
	if withPage {
		hf = sectProps(0)
	}
	bodyS := blocks(body, false, width)
	var head strings.Builder
	head.WriteString(`{\rtf1\ansi\ansicpg1251\deff0\uc1{\fonttbl`)
	for i, f := range fonts {
		fmt.Fprintf(&head, `{\f%d\fnil\fcharset204 %s;}`, i, esc(f))
	}
	head.WriteString(`}{\colortbl;`)
	for _, c := range colors {
		v := parseHexColor(c, 0)
		fmt.Fprintf(&head, `\red%d\green%d\blue%d;`, (v>>16)&0xff, (v>>8)&0xff, v&0xff)
	}
	head.WriteString("}\n")
	if len(authors) > 1 {
		head.WriteString(`{\*\revtbl `)
		for _, a := range authors {
			head.WriteString("{" + esc(a) + ";}")
		}
		head.WriteString("}\n")
	}
	if len(listOrder) > 0 {
		head.WriteString(rtfListTable(listOrder))
	}
	if withPage {
		land := ""
		if pg.W > pg.H {
			land = `\landscape`
		}
		tp := ""
		if d.EvenOdd {
			tp += `\facingp`
		}
		if d.Track {
			tp += `\revisions`
		}
		if d.Hyphen {
			tp += `\hyphauto1`
		}
		fmt.Fprintf(&head, `\paperw%d\paperh%d\margl%d\margr%d\margt%d\margb%d%s%s`,
			twp(pg.W), twp(pg.H), twp(pg.Left), twp(pg.Right), twp(pg.Top), twp(pg.Bottom), land, tp)
	}
	fmt.Fprintf(&head, `\deflang1049\plain\f0\fs%d`+"\n", int(math.Round(d.Def.Size*2)))
	return head.String() + hf + bodyS + "}"
}

// rtfListTable — таблица списков RTF, чтобы Word и LibreOffice видели настоящие списки, а не текст.
func rtfListTable(ls []*List) string {
	nfc := map[string]int{"decimal": 0, "upperRoman": 1, "lowerRoman": 2, "upperLetter": 3, "lowerLetter": 4, "bullet": 23}
	var sb strings.Builder
	sb.WriteString(`{\*\listtable`)
	for i, l := range ls {
		fmt.Fprintf(&sb, `{\list\listtemplateid%d\listhybrid`, 1000+i)
		for lv := 0; lv < 9; lv++ {
			ind := 36 * float64(lv+1)
			if l.Kind == "ul" {
				b := bullets[lv%3]
				if lv == l.Level && l.Text != "" && !strings.Contains(l.Text, "%") {
					b = l.Text
				}
				r := []rune(b)[0]
				fmt.Fprintf(&sb, `{\listlevel\levelnfc23\levelnfcn23\leveljc0\levelfollow0\levelstartat1{\leveltext\'01\u%d ?;}{\levelnumbers;}\fi-360\li%d}`, r, int(math.Round(ind*20)))
				continue
			}
			f := olDefault[lv%3]
			if lv == l.Level && l.Fmt != "" && l.Fmt != "bullet" {
				f = l.Fmt
			}
			code, ok := nfc[f]
			if !ok {
				code = 0
			}
			tpl := "%" + strconv.Itoa(lv+1) + "."
			if lv == l.Level && strings.Contains(l.Text, "%") {
				tpl = l.Text
			}
			var txt, nums strings.Builder
			cnt := 0
			t := []rune(tpl)
			for k := 0; k < len(t); k++ {
				if t[k] == '%' && k+1 < len(t) && t[k+1] >= '1' && t[k+1] <= '9' {
					cnt++
					fmt.Fprintf(&nums, `\'%02x`, cnt)
					fmt.Fprintf(&txt, `\'%02x`, int(t[k+1]-'1'))
					k++
					continue
				}
				cnt++
				if t[k] < 128 {
					if t[k] == '\\' || t[k] == '{' || t[k] == '}' {
						txt.WriteByte('\\')
					}
					txt.WriteRune(t[k])
				} else {
					fmt.Fprintf(&txt, `\u%d ?`, t[k])
				}
			}
			fmt.Fprintf(&sb, `{\listlevel\levelnfc%d\levelnfcn%d\leveljc0\levelfollow0\levelstartat1{\leveltext\'%02x%s;}{\levelnumbers%s;}\fi-360\li%d}`,
				code, code, cnt, txt.String(), nums.String(), int(math.Round(ind*20)))
		}
		fmt.Fprintf(&sb, `\listid%d}`, 2000+i)
	}
	sb.WriteString(`}{\*\listoverridetable`)
	for i := range ls {
		fmt.Fprintf(&sb, `{\listoverride\listid%d\listoverridecount0\ls%d}`, 2000+i, i+1)
	}
	sb.WriteString("}\n")
	return sb.String()
}
