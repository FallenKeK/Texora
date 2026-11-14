package main

// .docx <-> модель. Только стандартная библиотека.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"
)

var mimeByExt = map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif",
	"bmp": "image/bmp", "tif": "image/tiff", "tiff": "image/tiff", "emf": "image/x-emf", "wmf": "image/x-wmf",
	"svg": "image/svg+xml", "webp": "image/webp"}

func extByMime(m string) string {
	for _, e := range []string{"png", "jpeg", "gif", "bmp", "tiff", "emf", "wmf", "svg", "webp"} {
		if mimeByExt[e] == m {
			return e
		}
	}
	return "png"
}

var highlightColors = map[string]string{"yellow": "#ffff00", "green": "#00ff00", "cyan": "#00ffff", "magenta": "#ff00ff",
	"blue": "#0000ff", "red": "#ff0000", "darkBlue": "#000080", "darkCyan": "#008080", "darkGreen": "#008000",
	"darkMagenta": "#800080", "darkRed": "#800000", "darkYellow": "#808000", "darkGray": "#808080", "lightGray": "#c0c0c0",
	"black": "#000000", "white": "#ffffff"}

var puaMap = map[rune]string{0xf0b7: "•", 0xf0a7: "▪", 0xf0d8: "➢", 0xf0fc: "✓", 0xf076: "❖", 0xf0a8: "◦", 0xf06f: "o"}

func unzipAll(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("файл повреждён или это не документ Office: %v", err)
	}
	out := map[string][]byte{}
	// защита от «бомб»: часть, которая после распаковки раздувается на гигабайты, не должна съесть всю память
	const perFile, total = 300 << 20, 900 << 20
	used := int64(0)
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(rc, perFile))
		rc.Close()
		used += int64(len(b))
		if used > total {
			return nil, fmt.Errorf("документ слишком большой после распаковки")
		}
		out[f.Name] = b
	}
	return out, nil
}

// ======================================================================== чтение
type rel struct {
	typ, target string
	external    bool
}

func readRels(files map[string][]byte, part string) map[string]rel {
	dir := path.Dir(part)
	relPath := dir + "/_rels/" + path.Base(part) + ".rels"
	if dir == "." {
		relPath = "_rels/" + path.Base(part) + ".rels"
	}
	out := map[string]rel{}
	data, ok := files[relPath]
	if !ok {
		return out
	}
	root, _ := ParseXML(data)
	for _, r := range root.Find("Relationship") {
		t := r.Av("Target")
		ext := r.Av("TargetMode") == "External"
		if !ext {
			if strings.HasPrefix(t, "/") {
				t = t[1:]
			} else {
				t = path.Clean(dir + "/" + t)
			}
		}
		typ := r.Av("Type")
		out[r.Av("Id")] = rel{typ: typ[strings.LastIndex(typ, "/")+1:], target: t, external: ext}
	}
	return out
}

type dxStyle struct {
	id, typ, name, basedOn string
	pPr, rPr, tblPr        *Node
	def                    bool
}

// cellStyles — свойства абзацев из стиля таблицы (действуют на абзацы в её ячейках).
type cellStyles struct{ pPr, rPr []*Node }

type numLevel struct {
	fmt, text string
	pPr       *Node
}

type docxReader struct {
	files    map[string][]byte
	doc      *Doc
	styles   map[string]*dxStyle
	normalID string
	defR     *Node
	defP     *Node
	cellSt   []cellStyles
	major    string
	minor    string
	absNum   map[string][]numLevel
	nums     map[string]string
	mediaIDs map[string]string
	fld      []*fldState
	region   string // поле TOC / список литературы, внутри которого мы сейчас
	regHit   string
	cmt      string // открытое примечание
	ins, del string // текущие исправления
	rels0    map[string]rel
}

type fldState struct {
	instr string
	mode  string // instr | result
	done  bool
	link  string
}

func (r *docxReader) top() *fldState {
	if n := len(r.fld); n > 0 {
		return r.fld[n-1]
	}
	return nil
}

func fieldKind(instr string) (string, []string) {
	fs := strings.Fields(instr)
	if len(fs) == 0 {
		return "", nil
	}
	return strings.ToUpper(fs[0]), fs[1:]
}

// fieldRunName — во что превращается поле в нашей модели ("" — показываем результат как текст).
func fieldRunName(instr string) string {
	k, args := fieldKind(instr)
	switch k {
	case "PAGE":
		return "PAGE"
	case "NUMPAGES", "SECTIONPAGES":
		return "NUMPAGES"
	case "PAGEREF":
		if len(args) > 0 {
			return "PAGEREF " + strings.Trim(args[0], `"`)
		}
	case "ADDIN":
		if len(args) > 0 && strings.HasPrefix(args[0], "TEXORA_CITE") && len(args) > 1 {
			return "CITE " + strings.Join(args[1:], " ")
		}
	case "SEQ":
		if len(args) > 0 {
			chapter := false
			for i, a := range args {
				if a == `\s` && i+1 < len(args) {
					chapter = true
				}
			}
			return seqName(strings.Trim(args[0], `"`), chapter, "")
		}
	case "REF":
		if len(args) > 0 {
			bm := strings.Trim(args[0], `"`)
			mode := "l"
			if strings.HasSuffix(bm, "_n") {
				bm, mode = strings.TrimSuffix(bm, "_n"), "n"
			}
			if !strings.HasPrefix(bm, "_Ref") && !strings.HasPrefix(bm, "_Toc") {
				return ""
			}
			return "REF " + bm + " " + mode
		}
	}
	return ""
}

func onVal(n *Node) bool {
	if n == nil {
		return false
	}
	v, ok := n.A("val")
	return !ok || (v != "0" && v != "false" && v != "off")
}

func twip(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v / 20 }

func (r *docxReader) xml(p string) *Node {
	d, ok := r.files[p]
	if !ok {
		return nil
	}
	n, _ := ParseXML(d)
	return n
}

func (r *docxReader) chain(id string) []*dxStyle {
	var out []*dxStyle
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		s, ok := r.styles[id]
		if !ok {
			break
		}
		seen[id] = true
		out = append([]*dxStyle{s}, out...)
		id = s.basedOn
	}
	return out
}

func (r *docxReader) rpr(n *Node, f F) F {
	if n == nil {
		return f
	}
	for _, c := range n.Elems() {
		v := c.Av("val")
		switch c.Name {
		case "b":
			f.B = onVal(c)
		case "i":
			f.I = onVal(c)
		case "strike":
			f.S = onVal(c)
		case "dstrike":
			f.S = onVal(c)
		case "u":
			f.U = v != "none" && v != "0" && v != "double"
			f.DU = v == "double"
		case "vertAlign":
			f.Sup, f.Sub = v == "superscript", v == "subscript"
		case "color":
			if v != "" && v != "auto" {
				f.Color = "#" + strings.ToLower(v)
			} else {
				f.Color = ""
			}
		case "highlight":
			if c, ok := highlightColors[v]; ok {
				f.Bg = c
			}
		case "shd":
			if fill := c.Av("fill"); fill != "" && fill != "auto" {
				f.Bg = "#" + strings.ToLower(fill)
			}
		case "sz":
			if x, err := strconv.ParseFloat(v, 64); err == nil {
				f.Size = x / 2
			}
		case "rFonts":
			name := c.Av("ascii")
			if name == "" {
				name = c.Av("hAnsi")
			}
			if name == "" {
				name = c.Av("cs")
			}
			if name != "" {
				f.Font = name
			} else if t := c.Av("asciiTheme"); t != "" {
				if strings.HasPrefix(t, "major") {
					f.Font = r.major
				} else {
					f.Font = r.minor
				}
			}
		case "caps":
			f.Caps = onVal(c)
		case "vanish":
			if onVal(c) {
				f.Link = "\x00hidden"
			}
		}
	}
	return f
}

type rpf struct {
	PF
	num    string
	lvl    int
	hasNum bool
	outl   int
}

func (r *docxReader) ppr(n *Node, pf rpf) rpf {
	if n == nil {
		return pf
	}
	for _, c := range n.Elems() {
		v := c.Av("val")
		switch c.Name {
		case "pBdr":
			if b := c.Kid("top"); b != nil {
				pf.BdrTop = parseBorder(b)
			}
			if b := c.Kid("bottom"); b != nil {
				pf.BdrBottom = parseBorder(b)
			}
		case "jc":
			pf.Align = map[string]string{"center": "center", "right": "right", "end": "right", "both": "justify", "distribute": "justify"}[v]
		case "ind":
			if l, ok := c.A("left"); ok {
				pf.IndL = O(twip(l))
			} else if l, ok := c.A("start"); ok {
				pf.IndL = O(twip(l))
			}
			if rr, ok := c.A("right"); ok {
				pf.IndR = twip(rr)
			} else if rr, ok := c.A("end"); ok {
				pf.IndR = twip(rr)
			}
			if h, ok := c.A("hanging"); ok {
				pf.IndF = O(-twip(h))
			} else if f, ok := c.A("firstLine"); ok {
				pf.IndF = O(twip(f))
			}
		case "spacing":
			if b, ok := c.A("before"); ok {
				pf.Before = O(twip(b))
			}
			if a, ok := c.A("after"); ok {
				pf.After = O(twip(a))
			}
			if c.Av("beforeAutospacing") == "1" {
				pf.Before = O(14)
			}
			if c.Av("afterAutospacing") == "1" {
				pf.After = O(14)
			}
			if l, ok := c.A("line"); ok {
				rule := c.Av("lineRule")
				lv, _ := strconv.ParseFloat(l, 64)
				if rule == "" || rule == "auto" {
					pf.Line, pf.LineExact, pf.LineRule = O(lv/240), 0, ""
				} else {
					pf.LineExact, pf.LineRule = lv/20, rule
				}
			}
		case "pageBreakBefore":
			pf.PB = onVal(c)
		case "keepNext":
			pf.KeepNext = onVal(c)
		case "widowControl":
			pf.NoWidow = !onVal(c)
		case "contextualSpacing":
			if onVal(c) {
				pf.CS = "x"
			} else {
				pf.CS = ""
			}
		case "numPr":
			if id := c.Kid("numId"); id != nil {
				pf.num = id.Av("val")
				pf.hasNum = true
			}
			if l := c.Kid("ilvl"); l != nil {
				pf.lvl, _ = strconv.Atoi(l.Av("val"))
			}
		case "outlineLvl":
			pf.outl, _ = strconv.Atoi(v)
			pf.outl++
		case "shd":
			if fill := c.Av("fill"); fill != "" && fill != "auto" {
				pf.Shade = "#" + strings.ToLower(fill)
			}
		case "tabs":
			pf.Tabs = nil
			for _, t := range c.KidsNamed("tab") {
				v := t.Av("val")
				if v == "clear" {
					continue
				}
				al := map[string]string{"center": "center", "right": "right", "end": "right", "decimal": "right"}[v]
				ld := ""
				if l := t.Av("leader"); l != "" && l != "none" {
					ld = "dot"
				}
				pf.Tabs = append(pf.Tabs, TabStop{Pos: twip(t.Av("pos")), Align: al, Leader: ld})
			}
		}
	}
	return pf
}

func (r *docxReader) loadStyles(p string) {
	root := r.xml(p)
	if root == nil {
		return
	}
	if dd := root.First("docDefaults"); dd != nil {
		if x := dd.First("rPrDefault"); x != nil {
			r.defR = x.Kid("rPr")
		}
		if x := dd.First("pPrDefault"); x != nil {
			r.defP = x.Kid("pPr")
		}
	}
	for _, s := range root.Find("style") {
		st := &dxStyle{id: s.Av("styleId"), typ: s.Av("type"), name: s.Kid("name").Av("val"), pPr: s.Kid("pPr"), rPr: s.Kid("rPr"), tblPr: s.Kid("tblPr"), def: s.Av("default") == "1"}
		if b := s.Kid("basedOn"); b != nil {
			st.basedOn = b.Av("val")
		}
		if st.name == "" {
			st.name = st.id
		}
		r.styles[st.id] = st
		if s.Av("default") == "1" && st.typ == "paragraph" {
			r.normalID = st.id
		}
	}
}

func (r *docxReader) loadNumbering(p string) {
	root := r.xml(p)
	if root == nil {
		return
	}
	for _, a := range root.Find("abstractNum") {
		levels := make([]numLevel, 9)
		for _, l := range a.KidsNamed("lvl") {
			i, _ := strconv.Atoi(l.Av("ilvl"))
			if i < 0 || i > 8 {
				continue
			}
			text := l.Kid("lvlText").Av("val")
			var sb strings.Builder
			for _, ch := range text {
				if ch >= 0xf000 && ch <= 0xf0ff {
					if m, ok := puaMap[ch]; ok {
						sb.WriteString(m)
					} else {
						sb.WriteString("•")
					}
				} else {
					sb.WriteRune(ch)
				}
			}
			f := l.Kid("numFmt").Av("val")
			if f == "" {
				f = "decimal"
			}
			levels[i] = numLevel{fmt: f, text: sb.String(), pPr: l.Kid("pPr")}
		}
		r.absNum[a.Av("abstractNumId")] = levels
	}
	for _, n := range root.Root().KidsNamed("num") {
		r.nums[n.Av("numId")] = n.Kid("abstractNumId").Av("val")
	}
}

func (r *docxReader) trimF(f F) F {
	if f.Font == r.doc.Def.Font {
		f.Font = ""
	}
	if f.Size == r.doc.Def.Size {
		f.Size = 0
	}
	return f
}

type pctx struct {
	cur  *Para
	out  []*Para
	base F
	rels map[string]rel
	pf   PF
}

func (c *pctx) push(run Run) { c.cur.Runs = append(c.cur.Runs, run) }

func (r *docxReader) blocks(n *Node, rels map[string]rel) []Block {
	var out []Block
	for _, el := range n.Elems() {
		switch el.Name {
		case "p":
			for _, p := range r.para(el, rels) {
				out = append(out, Block{P: p})
			}
		case "tbl":
			out = append(out, Block{T: r.table(el, rels)})
		case "sdt":
			if c := el.Kid("sdtContent"); c != nil {
				out = append(out, r.blocks(c, rels)...)
			}
		case "customXml":
			out = append(out, r.blocks(el, rels)...)
		}
	}
	return out
}

var stylesByName = map[string]string{"title": "Title", "subtitle": "Subtitle", "quote": "Quote", "intense quote": "Quote", "no spacing": "NoSpacing",
	"toc 1": "TOC1", "toc 2": "TOC2", "toc 3": "TOC3", "toc heading": "TOCHeading"}

// styleDefFromDocx — определение нашего стиля по цепочке стилей Word.
func (r *docxReader) styleDefFromDocx(id string) headingStyle {
	ch := r.chain(id)
	f := r.rpr(r.defR, F{})
	pf := r.ppr(r.defP, rpf{})
	for _, s := range ch {
		f = r.rpr(s.rPr, f)
		pf = r.ppr(s.pPr, pf)
	}
	hs := headingStyle{Size: f.Size, B: f.B, I: f.I, Color: f.Color, Font: f.Font, Caps: f.Caps, Align: pf.Align}
	if pf.Before.Set {
		hs.Before = pf.Before.V
	}
	if pf.After.Set {
		hs.After = pf.After.V
	}
	if pf.Line.Set {
		hs.Line = pf.Line.V
	}
	if pf.IndL.Set {
		hs.IndL = pf.IndL.V
	}
	if pf.IndF.Set {
		hs.IndF, hs.Set = pf.IndF.V, true
	}
	return hs
}

// trimStyle убирает из прямого форматирования то, что совпадает со стилем.
func trimStyle(f F, hs headingStyle) F {
	if f.Font == hs.Font {
		f.Font = ""
	}
	if f.Size == hs.Size {
		f.Size = 0
	}
	if f.Color == hs.Color {
		f.Color = ""
	}
	if f.B == hs.B {
		f.B = false
	}
	if f.I == hs.I {
		f.I = false
	}
	if f.Caps == hs.Caps {
		f.Caps = false
	}
	return f
}

func (r *docxReader) para(el *Node, rels map[string]rel) []*Para {
	pPr := el.Kid("pPr")
	sid := r.normalID
	if ps := pPr.Kid("pStyle"); ps != nil {
		sid = ps.Av("val")
	}
	ch := r.chain(sid)
	pf := r.ppr(r.defP, rpf{})
	inCell := len(r.cellSt) > 0
	if inCell {
		for _, n := range r.cellSt[len(r.cellSt)-1].pPr {
			pf = r.ppr(n, pf)
		}
	}
	for _, s := range ch {
		pf = r.ppr(s.pPr, pf)
	}
	direct := r.ppr(pPr, rpf{})
	num, lvl, has := pf.num, pf.lvl, pf.hasNum
	if direct.hasNum {
		num, has = direct.num, true
		lvl = direct.lvl
	}
	var list *List
	if has && num != "" && num != "0" {
		if abs, ok := r.nums[num]; ok {
			levels := r.absNum[abs]
			if lvl < len(levels) {
				lv := levels[lvl]
				if lv.pPr != nil {
					pf = r.ppr(lv.pPr, pf)
				}
				if lv.fmt != "none" && lv.fmt != "" {
					kind := "ol"
					if lv.fmt == "bullet" {
						kind = "ul"
					}
					list = &List{ID: "n" + num, Kind: kind, Level: lvl, Fmt: lv.fmt, Text: lv.text}
				}
			}
		}
	}
	pf = r.ppr(pPr, pf)
	f0 := r.rpr(r.defR, F{})
	if inCell {
		for _, n := range r.cellSt[len(r.cellSt)-1].rPr {
			f0 = r.rpr(n, f0)
		}
	}
	for _, s := range ch {
		f0 = r.rpr(s.rPr, f0)
	}
	// в ячейке интервалы фиксируем явно: у Word они идут от стиля таблицы, а не от «по умолчанию для таблиц»
	cellAfter, cellLine := 0.0, 1.0
	if pf.After.Set {
		cellAfter = pf.After.V
	}
	if pf.Line.Set {
		cellLine = pf.Line.V
	}
	name := ""
	if len(ch) > 0 {
		name = strings.ToLower(ch[len(ch)-1].name)
	}
	out := pf.PF
	out.List = list
	heading := 0
	if strings.HasPrefix(name, "heading ") {
		heading, _ = strconv.Atoi(strings.TrimPrefix(name, "heading "))
	} else if st, ok := stylesByName[name]; ok {
		out.Style = st
	} else if pf.outl > 0 && pf.outl <= 6 && len(ch) > 0 && ch[len(ch)-1].id != r.normalID {
		heading = pf.outl
	}
	if heading > 6 {
		heading = 6
	}
	out.Heading = heading
	skey := StyleKey(&Para{PF: out})
	shs, hasStyle := r.doc.Styles[skey]
	if hasStyle && skey != "" {
		if out.Align == shs.Align {
			out.Align = ""
		}
		if out.Before.Set && out.Before.V == shs.Before {
			out.Before = Opt{}
		}
		if out.After.Set && out.After.V == shs.After {
			out.After = Opt{}
		}
		if out.Line.Set && out.Line.V == shs.Line {
			out.Line = Opt{}
		}
		if out.IndF.Set && shs.Set && out.IndF.V == shs.IndF {
			out.IndF = Opt{}
		}
		if out.IndL.Set && out.IndL.V == shs.IndL && list == nil {
			out.IndL = Opt{}
		}
		if strings.HasPrefix(skey, "TOC") && len(out.Tabs) > 0 && skey != "TOCHeading" {
			out.Tabs = nil // табуляцию с заполнителем добавим сами
		}
		f0 = trimStyle(f0, shs)
	}
	d := r.doc.Def
	if !out.Before.Set || (out.Before.V == 0 && heading == 0 && out.Style == "") {
		if !(heading > 0 || out.Style != "") {
			out.Before = Opt{}
		}
	}
	if out.After.Set && out.After.V == d.After && heading == 0 && out.Style == "" {
		out.After = Opt{}
	}
	if out.Line.Set && math.Abs(out.Line.V-d.Line) < 0.001 {
		out.Line = Opt{}
	}
	if inCell && heading == 0 {
		out.After = O(cellAfter)
		if out.LineExact == 0 {
			out.Line = O(cellLine)
		}
	}
	if out.KeepNext && heading > 0 {
		out.KeepNext = false
	}
	if out.CS != "" {
		out.CS = sid
	}
	if list != nil && out.IndL.Set && !direct.IndL.Set {
		// отступ из нумерации — оставляем явным, чтобы совпадал с Word
	}
	c := &pctx{cur: &Para{PF: out}, base: f0, rels: rels, pf: out}
	r.regHit = r.region
	r.inline(el, c, "")
	c.cur.Normalize()
	if r.regHit != "" && r.rels0 != nil {
		c.cur.PF.Region = r.regHit
	}
	if sp := pPr.Kid("sectPr"); sp != nil && r.rels0 != nil {
		sec := &Section{ID: NewID("s")}
		r.readSect(sp, &sec.Page, &sec.Cols, &sec.TitlePg, &sec.Start, &sec.Type, &sec.HF)
		c.cur.PF.Sect = sec
	}
	if len(c.cur.Runs) == 0 {
		var mk *Node
		if pPr != nil {
			mk = pPr.Kid("rPr")
		}
		c.cur.Mark = r.trimF(r.rpr(mk, f0))
	}
	c.out = append(c.out, c.cur)
	// знак абзаца вставлен или удалён исправлением, и весь текст тоже — абзац целиком вставлен/удалён
	if rp := pPr.Kid("rPr"); rp != nil && len(c.out) > 0 {
		for _, kind := range []string{"del", "ins"} {
			if rp.Kid(kind) == nil {
				continue
			}
			p := c.out[len(c.out)-1]
			all := len(p.Runs) > 0
			for _, run := range p.Runs {
				if (kind == "del" && run.F.Del == "") || (kind == "ins" && run.F.Ins == "") {
					all = false
				}
			}
			if all {
				p.PF.RevPara = kind
			}
		}
	}
	return c.out
}

func (r *docxReader) inline(el *Node, c *pctx, link string) {
	for _, k := range el.Elems() {
		switch k.Name {
		case "r":
			r.run(k, c, link)
		case "hyperlink":
			url := link
			if id := k.AttrNS("http://schemas.openxmlformats.org/officeDocument/2006/relationships", "id"); id != "" {
				if rl, ok := c.rels[id]; ok {
					url = rl.target
				}
			} else if a := k.Av("anchor"); a != "" {
				url = "#" + a
			}
			r.inline(k, c, url)
		case "fldSimple":
			if name := fieldRunName(k.Av("instr")); name != "" {
				f := r.trimF(c.base)
				f.Link = link
				f.Cmt, f.Ins, f.Del = r.cmt, r.ins, r.del
				c.push(Run{T: RField, Name: name, F: f})
			} else {
				r.inline(k, c, link)
			}
		case "sdt":
			if s := k.Kid("sdtContent"); s != nil {
				r.inline(s, c, link)
			}
		case "sdtContent", "smartTag", "customXml":
			r.inline(k, c, link)
		case "ins", "moveTo", "del", "moveFrom":
			id := "r" + k.Av("id") + k.Av("author")
			if _, ok := r.doc.Revs[id]; !ok {
				r.doc.Revs[id] = &Rev{Author: k.Av("author"), Date: k.Av("date")}
			}
			oi, od := r.ins, r.del
			if k.Name == "ins" || k.Name == "moveTo" {
				r.ins = id
			} else {
				r.del = id
			}
			r.inline(k, c, link)
			r.ins, r.del = oi, od
		case "bookmarkStart":
			if n := k.Av("name"); n != "" && n != "_GoBack" && c.cur.PF.Bookmark == "" {
				c.cur.PF.Bookmark = n
			}
		case "commentRangeStart":
			if r.rels0 != nil {
				r.cmt = "c" + k.Av("id")
			}
		case "commentRangeEnd":
			if r.cmt == "c"+k.Av("id") {
				r.cmt = ""
			}
		case "oMath":
			f := r.trimF(c.base)
			f.Cmt, f.Ins, f.Del = r.cmt, r.ins, r.del
			c.push(Run{T: REq, Text: []rune(OMMLToLinear(k)), F: f})
		case "oMathPara":
			for _, om := range k.KidsNamed("oMath") {
				f := r.trimF(c.base)
				f.Cmt = r.cmt
				c.push(Run{T: REq, Text: []rune(OMMLToLinear(om)), F: f})
			}
			if c.cur.PF.Align == "" {
				c.cur.PF.Align = "center"
				if pr := k.Kid("oMathParaPr"); pr != nil {
					if j := pr.Kid("jc"); j != nil {
						c.cur.PF.Align = map[string]string{"left": "left", "right": "right", "center": "center"}[j.Av("val")]
					}
				}
			}
		}
	}
}

func (r *docxReader) media(p string) string {
	if id, ok := r.mediaIDs[p]; ok {
		return id
	}
	data, ok := r.files[p]
	if !ok {
		return ""
	}
	id := NewID("m")
	ext := strings.ToLower(path.Ext(p))
	if len(ext) > 0 {
		ext = ext[1:]
	}
	r.doc.Media[id] = &Media{Mime: mimeByExt[ext], Data: data}
	r.mediaIDs[p] = id
	return id
}

func (r *docxReader) run(rn *Node, c *pctx, link string) {
	rPr := rn.Kid("rPr")
	f := c.base
	if rs := rPr.Kid("rStyle"); rs != nil {
		for _, s := range r.chain(rs.Av("val")) {
			f = r.rpr(s.rPr, f)
		}
	}
	f = r.rpr(rPr, f)
	if f.Link == "\x00hidden" {
		return
	}
	if link != "" {
		f.Link = link
	}
	for _, st := range r.fld {
		if st.link != "" && st.mode == "result" && f.Link == "" {
			f.Link = st.link
		}
	}
	ff := r.trimF(f)
	ff.Cmt, ff.Ins, ff.Del = r.cmt, r.ins, r.del
	var text []rune
	flush := func() {
		if len(text) > 0 {
			c.push(Run{T: RText, Text: text, F: ff})
			text = nil
		}
	}
	for _, k := range rn.Elems() {
		if k.Name == "fldChar" {
			flush()
			switch k.Av("fldCharType") {
			case "begin":
				r.fld = append(r.fld, &fldState{mode: "instr"})
			case "separate":
				if st := r.top(); st != nil {
					st.mode = "result"
					kind, args := fieldKind(st.instr)
					switch {
					case kind == "TOC" && strings.Contains(st.instr, `\c`):
						for i, a := range args {
							if a == `\c` && i+1 < len(args) {
								r.region = "lof:" + strings.Trim(args[i+1], `"`)
								r.regHit = r.region
							}
						}
					case kind == "TOC":
						r.region, r.regHit = "toc", "toc"
					case kind == "ADDIN" && len(args) > 0 && args[0] == "TEXORA_BIB":
						r.region, r.regHit = "bib", "bib"
					case kind == "HYPERLINK":
						for i := 0; i < len(args); i++ {
							if args[i] == "\\l" && i+1 < len(args) {
								st.link = "#" + strings.Trim(args[i+1], `"`)
							} else if !strings.HasPrefix(args[i], "\\") && st.link == "" {
								st.link = strings.Trim(args[i], `"`)
							}
						}
					}
				}
			case "end":
				if st := r.top(); st != nil {
					r.fld = r.fld[:len(r.fld)-1]
					kind, _ := fieldKind(st.instr)
					if name := fieldRunName(st.instr); name != "" && !st.done {
						fr := ff
						fr.Link = f.Link
						if st.link != "" {
							fr.Link = st.link
						}
						c.push(Run{T: RField, Name: name, F: fr})
					}
					if kind == "TOC" || (kind == "ADDIN" && strings.Contains(st.instr, "TEXORA_BIB")) {
						r.region = ""
					}
				}
			}
			continue
		}
		st := r.top()
		if st != nil && st.mode == "instr" {
			if k.Name == "instrText" || k.Name == "delInstrText" {
				st.instr += k.TextContent()
			}
			continue
		}
		if st != nil && st.mode == "result" {
			if name := fieldRunName(st.instr); name != "" {
				if !st.done {
					fr := ff
					if st.link != "" {
						fr.Link = st.link
					}
					c.push(Run{T: RField, Name: name, F: fr})
					st.done = true
				}
				continue
			}
		}
		switch k.Name {
		case "t", "delText":
			text = append(text, []rune(k.TextContent())...)
		case "tab", "ptab":
			text = append(text, '\t')
		case "noBreakHyphen":
			text = append(text, '-')
		case "softHyphen":
		case "sym":
			if code, err := strconv.ParseUint(k.Av("char"), 16, 32); err == nil {
				ch := rune(code)
				if m, ok := puaMap[ch]; ok {
					text = append(text, []rune(m)...)
				} else {
					if ch >= 0xf000 {
						ch -= 0xf000
					}
					text = append(text, ch)
				}
			}
		case "br", "cr":
			flush()
			if bt := k.Av("type"); bt == "page" || bt == "column" {
				if len(c.cur.Runs) > 0 {
					c.cur.Normalize()
					c.out = append(c.out, c.cur)
					np := &Para{PF: c.pf}
					np.PF.List = nil
					np.PF.Bookmark = ""
					c.cur = np
				}
				if bt == "page" {
					c.cur.PF.PB = true
				} else {
					c.cur.PF.CB = true
				}
			} else {
				c.push(Run{T: RBreak})
			}
		case "footnoteReference":
			flush()
			c.push(Run{T: RNote, ID: "f" + k.Av("id")})
		case "drawing":
			flush()
			r.drawing(k, c)
		case "pict", "object":
			flush()
			r.vml(k, c)
		}
	}
	flush()
}

const relNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

func (r *docxReader) drawing(el *Node, c *pctx) {
	blip := el.First("blip")
	ext := el.First("extent")
	if blip == nil {
		return
	}
	rid := blip.AttrNS(relNS, "embed")
	rl, ok := c.rels[rid]
	if !ok {
		return
	}
	id := r.media(rl.target)
	if id == "" {
		return
	}
	w, h := 100.0, 100.0
	if ext != nil {
		cx, _ := strconv.ParseFloat(ext.Av("cx"), 64)
		cy, _ := strconv.ParseFloat(ext.Av("cy"), 64)
		w, h = cx/12700, cy/12700
	}
	run := Run{T: RImage, ID: id, W: w, H: h}
	if an := el.First("anchor"); an != nil {
		switch {
		case an.Kid("wrapTopAndBottom") != nil:
			run.Wrap = "topbottom"
		case an.Kid("wrapNone") != nil:
			run.Wrap = "front"
			if an.Av("behindDoc") == "1" || an.Av("behindDoc") == "true" {
				run.Wrap = "behind"
			}
		default:
			run.Wrap = "square"
		}
		run.Side = "left"
		if ph := an.Kid("positionH"); ph != nil {
			if al := ph.Kid("align"); al != nil {
				switch strings.TrimSpace(al.TextContent()) {
				case "right", "outside":
					run.Side = "right"
				case "center":
					run.Side = "center"
				}
			} else if po := ph.Kid("posOffset"); po != nil {
				v, _ := strconv.ParseFloat(strings.TrimSpace(po.TextContent()), 64)
				if v/12700 > 150 {
					run.Side = "right"
				}
			}
		}
		if run.Wrap == "square" && run.Side == "center" {
			run.Side = "left"
		}
	}
	c.push(run)
}

func (r *docxReader) vml(el *Node, c *pctx) {
	im := el.First("imagedata")
	if im == nil {
		return
	}
	rl, ok := c.rels[im.AttrNS(relNS, "id")]
	if !ok {
		return
	}
	id := r.media(rl.target)
	if id == "" {
		return
	}
	w, h := 100.0, 100.0
	if sh := el.First("shape"); sh != nil {
		for _, part := range strings.Split(sh.Av("style"), ";") {
			kv := strings.SplitN(part, ":", 2)
			if len(kv) == 2 && strings.HasSuffix(kv[1], "pt") {
				v, _ := strconv.ParseFloat(strings.TrimSuffix(kv[1], "pt"), 64)
				if strings.TrimSpace(kv[0]) == "width" {
					w = v
				} else if strings.TrimSpace(kv[0]) == "height" {
					h = v
				}
			}
		}
	}
	c.push(Run{T: RImage, ID: id, W: w, H: h})
}

// parseBorder — w:top/w:bottom… → Border (W == 0 — явное «нет»).
func parseBorder(n *Node) *Border {
	v := n.Av("val")
	if v == "" || v == "none" || v == "nil" {
		return &Border{}
	}
	sz, _ := strconv.ParseFloat(n.Av("sz"), 64)
	w := sz / 8
	if w <= 0 {
		w = 0.5
	}
	b := &Border{W: w, Style: v}
	if c := n.Av("color"); c != "" && c != "auto" {
		b.Color = "#" + strings.ToLower(c)
	}
	return b
}

func mergeTblBorders(tb *TableBorders, n *Node) {
	if n == nil {
		return
	}
	for _, e := range n.Elems() {
		b := parseBorder(e)
		switch e.Name {
		case "top":
			tb.Top = b
		case "left", "start":
			tb.Left = b
		case "bottom":
			tb.Bottom = b
		case "right", "end":
			tb.Right = b
		case "insideH":
			tb.InsideH = b
		case "insideV":
			tb.InsideV = b
		}
	}
}

func mergeCellMar(pad *[4]float64, n *Node) bool {
	if n == nil {
		return false
	}
	any := false
	for _, e := range n.Elems() {
		w := twip(e.Av("w"))
		if t := e.Av("type"); t != "" && t != "dxa" {
			continue
		}
		switch e.Name {
		case "top":
			pad[0], any = w, true
		case "left", "start":
			pad[1], any = w, true
		case "bottom":
			pad[2], any = w, true
		case "right", "end":
			pad[3], any = w, true
		}
	}
	return any
}

func hasBorders(n *Node) bool {
	if n == nil {
		return false
	}
	for _, b := range n.Find("tblBorders") {
		for _, e := range b.Elems() {
			if v := e.Av("val"); v != "none" && v != "nil" {
				return true
			}
		}
	}
	return false
}

func (r *docxReader) table(el *Node, rels map[string]rel) *Table {
	tblPr := el.Kid("tblPr")
	t := &Table{}
	for _, g := range el.Kid("tblGrid").KidsNamed("gridCol") {
		t.Cols = append(t.Cols, twip(g.Av("w")))
	}
	// границы и поля: стиль таблицы (цепочка), затем свои свойства
	tb := &TableBorders{}
	pad := [4]float64{0, CellPad, 0, CellPad}
	sid := ""
	if s := tblPr.Kid("tblStyle"); s != nil {
		sid = s.Av("val")
	} else {
		for id, st := range r.styles {
			if st.typ == "table" && st.def {
				sid = id
			}
		}
	}
	cs := cellStyles{}
	for _, st := range r.chain(sid) {
		if st.tblPr != nil {
			mergeTblBorders(tb, st.tblPr.Kid("tblBorders"))
			mergeCellMar(&pad, st.tblPr.Kid("tblCellMar"))
		}
		if st.pPr != nil {
			cs.pPr = append(cs.pPr, st.pPr)
		}
		if st.rPr != nil {
			cs.rPr = append(cs.rPr, st.rPr)
		}
	}
	r.cellSt = append(r.cellSt, cs)
	defer func() { r.cellSt = r.cellSt[:len(r.cellSt)-1] }()
	if tblPr != nil {
		mergeTblBorders(tb, tblPr.Kid("tblBorders"))
		mergeCellMar(&pad, tblPr.Kid("tblCellMar"))
		if jc := tblPr.Kid("jc"); jc != nil {
			t.Align = map[string]string{"center": "center", "right": "right", "end": "right"}[jc.Av("val")]
		}
		if ind := tblPr.Kid("tblInd"); ind != nil && (ind.Av("type") == "" || ind.Av("type") == "dxa") {
			t.Ind = twip(ind.Av("w"))
		}
	}
	t.TB, t.Pad = tb, &pad
	for _, b := range []*Border{tb.Top, tb.Left, tb.Bottom, tb.Right, tb.InsideH, tb.InsideV} {
		if b != nil && b.W > 0 {
			t.Border = true
		}
	}
	for _, tr := range el.KidsNamed("tr") {
		row := &Row{}
		if trPr := tr.Kid("trPr"); trPr != nil {
			row.Header = onVal(trPr.Kid("tblHeader")) // <w:tblHeader w:val="off"/> — не шапка
			if h := trPr.Kid("trHeight"); h != nil {
				row.H = twip(h.Av("val"))
				if h.Av("hRule") == "exact" {
					row.HRule = "exact"
				}
			}
		}
		for _, tc := range tr.KidsNamed("tc") {
			tcPr := tc.Kid("tcPr")
			span := 1
			if gs := tcPr.Kid("gridSpan"); gs != nil {
				span, _ = strconv.Atoi(gs.Av("val"))
			}
			cell := &Cell{Blocks: r.blocks(tc, rels), Span: max(1, span)}
			if vm := tcPr.Kid("vMerge"); vm != nil {
				if vm.Av("val") == "restart" {
					cell.VMerge = "restart"
				} else {
					cell.VMerge = "continue"
				}
			}
			if sh := tcPr.Kid("shd"); sh != nil {
				if fill := sh.Av("fill"); fill != "" && fill != "auto" {
					cell.Shade = "#" + strings.ToLower(fill)
				}
			}
			if bs := tcPr.Kid("tcBorders"); bs != nil {
				for _, e := range bs.Elems() {
					side := map[string]int{"top": 0, "left": 1, "start": 1, "bottom": 2, "right": 3, "end": 3}
					if i, ok := side[e.Name]; ok {
						cell.B[i] = parseBorder(e)
					}
				}
			}
			var cp [4]float64
			if t.Pad != nil {
				cp = *t.Pad
			} else {
				cp = [4]float64{CellPadV, CellPad, CellPadV, CellPad}
			}
			if mergeCellMar(&cp, tcPr.Kid("tcMar")) {
				cell.Pad = &cp
			}
			if va := tcPr.Kid("vAlign"); va != nil {
				switch va.Av("val") {
				case "center":
					cell.VAlign = "center"
				case "bottom":
					cell.VAlign = "bottom"
				}
			}
			if len(cell.Blocks) == 0 {
				cell.Blocks = []Block{{P: NewPara()}}
			}
			row.Cells = append(row.Cells, cell)
		}
		if len(row.Cells) > 0 {
			t.Rows = append(t.Rows, row)
		}
	}
	if len(t.Rows) == 0 {
		t.Rows = []*Row{{Cells: []*Cell{{Blocks: []Block{{P: NewPara()}}, Span: 1}}}}
	}
	return t
}

// readSect — размер страницы, поля, колонтитулы, нумерация раздела.
func (r *docxReader) readSect(sect *Node, pg *Page, cols *Columns, titlePg *bool, start *int, typ *string, hf *HF) {
	*cols = Columns{}
	if c := sect.Kid("cols"); c != nil {
		n, _ := strconv.Atoi(c.Av("num"))
		if ks := c.KidsNamed("col"); len(ks) > 1 && c.Av("equalWidth") == "0" {
			n = len(ks) // колонки разной ширины — пока верстаем равными
		}
		if n > 1 {
			cols.N = min(n, 12)
			if v, ok := c.A("space"); ok {
				cols.Gap = twip(v)
			}
			if v, ok := c.A("sep"); ok {
				cols.Sep = v == "1" || v == "true" || v == "on"
			}
		}
	}
	if sz := sect.Kid("pgSz"); sz != nil {
		if v := twip(sz.Av("w")); v > 0 {
			pg.W = v
		}
		if v := twip(sz.Av("h")); v > 0 {
			pg.H = v
		}
	}
	if m := sect.Kid("pgMar"); m != nil {
		set := func(dst *float64, names ...string) {
			for _, n := range names {
				if v, ok := m.A(n); ok {
					*dst = math.Abs(twip(v))
					return
				}
			}
		}
		set(&pg.Top, "top")
		set(&pg.Bottom, "bottom")
		set(&pg.Left, "left", "start")
		set(&pg.Right, "right", "end")
		set(&pg.Header, "header")
		set(&pg.Footer, "footer")
	}
	*titlePg = onVal(sect.Kid("titlePg"))
	if t := sect.Kid("type"); t != nil {
		*typ = map[string]string{"continuous": "continuous", "evenPage": "evenPage", "oddPage": "oddPage"}[t.Av("val")]
	}
	if pn := sect.Kid("pgNumType"); pn != nil {
		if v, err := strconv.Atoi(pn.Av("start")); err == nil {
			*start = v
		}
	}
	saveFld, saveReg := r.fld, r.region
	for _, kind := range []string{"header", "footer"} {
		for _, ref := range sect.KidsNamed(kind + "Reference") {
			key := kind
			switch ref.Av("type") {
			case "first":
				key += ".first"
			case "even":
				key += ".even"
			}
			rl, ok := r.rels0[ref.AttrNS(relNS, "id")]
			if !ok {
				continue
			}
			hx := r.xml(rl.target)
			if hx == nil {
				continue
			}
			r.fld, r.region = nil, ""
			rels0 := r.rels0
			r.rels0 = nil
			bs := r.blocks(hx.Root(), readRels(r.files, rl.target))
			r.rels0 = rels0
			empty := true
			for _, b := range bs {
				if b.T != nil || len(b.P.Runs) > 0 {
					empty = false
				}
			}
			if empty && key != kind {
				// пустой колонтитул первой/чётной страницы — пустая строка, а не «как в прошлом разделе»
				bs = []Block{{P: NewPara()}}
				empty = false
			}
			if !empty {
				*hf.Get(key) = bs
			}
		}
	}
	r.fld, r.region = saveFld, saveReg
}

func ReadDocx(data []byte) (*Doc, error) {
	files, err := unzipAll(data)
	if err != nil {
		return nil, err
	}
	r := &docxReader{files: files, doc: NewDoc(), styles: map[string]*dxStyle{}, major: "Calibri Light", minor: "Calibri",
		absNum: map[string][]numLevel{}, nums: map[string]string{}, mediaIDs: map[string]string{}}
	r.doc.Body = nil
	main := "word/document.xml"
	if root, err := ParseXML(files["[Content_Types].xml"]); err == nil {
		for _, o := range root.Find("Override") {
			if strings.Contains(o.Av("ContentType"), "document.main+xml") {
				main = strings.TrimPrefix(o.Av("PartName"), "/")
			}
		}
	}
	rels := readRels(files, main)
	for _, rl := range rels {
		if rl.typ == "theme" {
			if th := r.xml(rl.target); th != nil {
				if mj := th.First("majorFont"); mj != nil {
					if l := mj.Kid("latin"); l != nil && l.Av("typeface") != "" {
						r.major = l.Av("typeface")
					}
				}
				if mn := th.First("minorFont"); mn != nil {
					if l := mn.Kid("latin"); l != nil && l.Av("typeface") != "" {
						r.minor = l.Av("typeface")
					}
				}
			}
		}
	}
	for _, rl := range rels {
		switch rl.typ {
		case "styles":
			r.loadStyles(rl.target)
		case "numbering":
			r.loadNumbering(rl.target)
		}
	}
	d := &r.doc.Def
	f := r.rpr(r.defR, F{})
	for _, s := range r.chain(r.normalID) {
		f = r.rpr(s.rPr, f)
	}
	if f.Font != "" {
		d.Font = f.Font
	} else {
		d.Font = "Times New Roman"
	}
	if f.Size > 0 {
		d.Size = f.Size
	} else {
		d.Size = 10
	}
	pf := r.ppr(r.defP, rpf{})
	for _, s := range r.chain(r.normalID) {
		pf = r.ppr(s.pPr, pf)
	}
	d.After, d.Line = 0, 1
	if pf.After.Set {
		d.After = pf.After.V
	}
	if pf.Line.Set {
		d.Line = pf.Line.V
	}
	for _, st := range r.styles {
		if st.typ != "paragraph" {
			continue
		}
		key := ""
		name := strings.ToLower(st.name)
		switch {
		case strings.HasPrefix(name, "heading ") && len(name) == 9 && name[8] >= '1' && name[8] <= '6':
			key = "h" + name[8:]
		default:
			key = stylesByName[name]
		}
		if key != "" {
			r.doc.Styles[key] = r.styleDefFromDocx(st.id)
		}
	}
	r.rels0 = rels
	root := r.xml(main)
	if root == nil {
		return nil, fmt.Errorf("в архиве нет %s — это не документ Word", main)
	}
	body := root.First("body")
	if body == nil {
		return nil, fmt.Errorf("документ без содержимого")
	}
	r.doc.Body = r.blocks(body, rels)
	if n := len(r.doc.Body); n == 0 || r.doc.Body[n-1].P == nil {
		r.doc.Body = append(r.doc.Body, Block{P: NewPara()})
	}
	// страница и колонтитулы последнего раздела
	var sect *Node
	for _, sp := range body.KidsNamed("sectPr") {
		sect = sp
	}
	if sect != nil {
		r.readSect(sect, &r.doc.Page, &r.doc.Cols, &r.doc.TitlePg, &r.doc.Start, &r.doc.Type, &r.doc.HF)
	} else if all := body.Find("sectPr"); len(all) > 0 && len(r.doc.Sections()) == 1 {
		r.readSect(all[len(all)-1], &r.doc.Page, &r.doc.Cols, &r.doc.TitlePg, &r.doc.Start, &r.doc.Type, &r.doc.HF)
	}
	// примечания
	for _, rl := range rels {
		if rl.typ != "comments" {
			continue
		}
		if cx := r.xml(rl.target); cx != nil {
			for _, cm := range cx.Root().KidsNamed("comment") {
				var lines []string
				for _, p := range cm.KidsNamed("p") {
					var sb strings.Builder
					for _, t := range p.Find("t") {
						sb.WriteString(t.TextContent())
					}
					lines = append(lines, sb.String())
				}
				r.doc.Comments["c"+cm.Av("id")] = &Comment{Author: cm.Av("author"), Initials: cm.Av("initials"), Date: cm.Av("date"),
					Text: strings.TrimSpace(strings.Join(lines, "\n"))}
			}
		}
	}
	// настройки документа
	for _, rl := range rels {
		if rl.typ != "settings" {
			continue
		}
		if sx := r.xml(rl.target); sx != nil {
			root := sx.Root()
			r.doc.EvenOdd = onVal(root.Kid("evenAndOddHeaders"))
			r.doc.Track = onVal(root.Kid("trackRevisions"))
			r.doc.Hyphen = onVal(root.Kid("autoHyphenation"))
			for _, v := range root.Find("docVar") {
				if v.Av("name") == "TexoraSources" {
					var src []*Source
					if json.Unmarshal([]byte(v.Av("val")), &src) == nil {
						r.doc.Sources = src
					}
				}
			}
		}
	}
	// сноски
	r.rels0 = nil
	for _, rl := range rels {
		if rl.typ != "footnotes" {
			continue
		}
		fx := r.xml(rl.target)
		if fx == nil {
			continue
		}
		frels := readRels(files, rl.target)
		for _, fn := range fx.Root().KidsNamed("footnote") {
			if t := fn.Av("type"); t != "" && t != "normal" {
				continue
			}
			r.fld = nil
			bs := r.blocks(fn, frels)
			for _, b := range bs {
				if b.P != nil {
					b.P.PF.List = nil
					b.P.PF.CS = ""
				}
			}
			if len(bs) > 0 && bs[0].P != nil && len(bs[0].P.Runs) > 0 && bs[0].P.Runs[0].T == RText {
				bs[0].P.Runs[0].Text = []rune(strings.TrimLeft(string(bs[0].P.Runs[0].Text), " \t"))
				bs[0].P.Normalize()
			}
			if len(bs) == 0 {
				bs = []Block{{P: NewPara()}}
			}
			r.doc.Footnotes["f"+fn.Av("id")] = &Note{Blocks: bs}
		}
	}
	fixSeqPrefixes(r.doc)
	return r.doc, nil
}

// ======================================================================== запись
const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"`
const xmlHead = "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n"
const relBase = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"

type wpart struct {
	name string
	rels []rel
	ids  []string
}

func (p *wpart) rel(typ, target string, ext bool) string {
	id := "rId" + strconv.Itoa(len(p.rels)+1)
	p.rels = append(p.rels, rel{typ: typ, target: target, external: ext})
	p.ids = append(p.ids, id)
	return id
}

func (p *wpart) xml() string {
	var sb strings.Builder
	sb.WriteString(xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, r := range p.rels {
		mode := ""
		if r.external {
			mode = ` TargetMode="External"`
		}
		fmt.Fprintf(&sb, `<Relationship Id="%s" Type="%s%s" Target="%s"%s/>`, p.ids[i], relBase, r.typ, xesc(r.target), mode)
	}
	sb.WriteString("</Relationships>")
	return sb.String()
}

type wfile struct {
	name  string
	data  []byte
	store bool
}

type docxWriter struct {
	seqBM      *seqBMState
	doc        *Doc
	cell       int
	files      []wfile
	mediaDone  map[string]string
	lists      map[string]*wlist
	listOrder  []string
	drawID     int
	fnIDs      map[string]int
	ctypes     []string
	cmtIDs     map[string]int
	cmtLast    map[string]cmtKey
	mainPart   *wpart
	cmtStarted map[string]bool
	revN       int
	bmN        int
	hfN        int
	width      float64
}

type cmtKey struct {
	p *Para
	i int
}

type wlist struct {
	num    int
	kind   string
	levels [9]*List
}

func tw(pt float64) int { return int(math.Round(pt * 20)) }

func colorHex(c string) string { return strings.ToUpper(strings.TrimPrefix(c, "#")) }

func (w *docxWriter) rPr(f F, extra string) string {
	d := w.doc.Def
	var sb strings.Builder
	sb.WriteString(extra)
	if f.Font != "" && f.Font != d.Font {
		n := xesc(f.Font)
		fmt.Fprintf(&sb, `<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, n, n, n, n)
	}
	if f.B {
		sb.WriteString("<w:b/><w:bCs/>")
	}
	if f.I {
		sb.WriteString("<w:i/><w:iCs/>")
	}
	if f.Caps {
		sb.WriteString("<w:caps/>")
	}
	if f.S {
		sb.WriteString("<w:strike/>")
	}
	color := f.Color
	if color == "" && f.Link != "" {
		color = "#0563c1"
	}
	if color != "" {
		fmt.Fprintf(&sb, `<w:color w:val="%s"/>`, colorHex(color))
	}
	if f.Size > 0 && f.Size != d.Size {
		fmt.Fprintf(&sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, int(math.Round(f.Size*2)), int(math.Round(f.Size*2)))
	}
	if f.DU {
		sb.WriteString(`<w:u w:val="double"/>`)
	} else if f.U || f.Link != "" {
		sb.WriteString(`<w:u w:val="single"/>`)
	}
	if f.Bg != "" {
		fmt.Fprintf(&sb, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, colorHex(f.Bg))
	}
	if f.Sup {
		sb.WriteString(`<w:vertAlign w:val="superscript"/>`)
	} else if f.Sub {
		sb.WriteString(`<w:vertAlign w:val="subscript"/>`)
	}
	if sb.Len() == 0 {
		return ""
	}
	return "<w:rPr>" + sb.String() + "</w:rPr>"
}

func (w *docxWriter) textRun(text []rune, f F) string {
	rpr := w.rPr(f, "")
	tag := "w:t"
	if f.Del != "" {
		tag = "w:delText"
	}
	var sb strings.Builder
	for i, part := range strings.Split(string(text), "\t") {
		if i > 0 {
			sb.WriteString("<w:r>" + rpr + "<w:tab/></w:r>")
		}
		if part != "" {
			sb.WriteString("<w:r>" + rpr + `<` + tag + ` xml:space="preserve">` + xesc(part) + "</" + tag + "></w:r>")
		}
	}
	return w.revWrap(f, sb.String())
}

// revWrap оборачивает разметку рана в w:ins / w:del.
func (w *docxWriter) revWrap(f F, x string) string {
	if x == "" {
		return x
	}
	wrap := func(tag, id string) {
		rv := w.doc.Revs[id]
		author, date := "Texora", nowISO()
		if rv != nil {
			author, date = rv.Author, rv.Date
		}
		w.revN++
		x = fmt.Sprintf(`<w:%s w:id="%d" w:author="%s" w:date="%s">`, tag, 1000+w.revN, xesc(author), xesc(date)) + x + "</w:" + tag + ">"
	}
	if f.Del != "" {
		wrap("del", f.Del)
	}
	if f.Ins != "" {
		wrap("ins", f.Ins)
	}
	return x
}

// fieldRun — сложное поле с готовым результатом.
func (w *docxWriter) fieldRun(r Run) string {
	rpr := w.rPr(r.F, "")
	instr, res := r.Name, "1"
	switch {
	case strings.HasPrefix(r.Name, "PAGEREF "):
		instr = r.Name + ` \h`
		res = w.doc.FieldValue(r.Name, "1", "1")
	case strings.HasPrefix(r.Name, "CITE "):
		instr = "ADDIN TEXORA_" + r.Name
		res = w.doc.FieldValue(r.Name, "1", "1")
	case strings.HasPrefix(r.Name, "SEQ "):
		cat, ch, _ := SeqField(r.Name)
		res = w.doc.FieldValue(r.Name, "1", "1")
		instr = `SEQ ` + strings.ReplaceAll(cat, " ", "_") + ` \* ARABIC`
		pre := ""
		if i := strings.LastIndex(res, "."); i >= 0 {
			// номер главы — обычным текстом, счётчик — полем со сбросом по заголовку 1
			pre, res = res[:i+1], res[i+1:]
			if ch {
				instr += ` \s 1`
			}
			if res == "1" {
				instr += ` \r 1` // явный сброс в начале главы (LibreOffice не понимает \s)
			}
		}
		if pre != "" {
			pre = `<w:r>` + rpr + `<w:t xml:space="preserve">` + xesc(pre) + `</w:t></w:r>`
		}
		return w.revWrap(r.F, pre+fmt.Sprintf(`<w:r>%s<w:fldChar w:fldCharType="begin"/></w:r><w:r>%s<w:instrText xml:space="preserve"> %s </w:instrText></w:r>`+
			`<w:r>%s<w:fldChar w:fldCharType="separate"/></w:r><w:r>%s<w:t xml:space="preserve">%s</w:t></w:r><w:r>%s<w:fldChar w:fldCharType="end"/></w:r>`,
			rpr, rpr, xesc(instr), rpr, rpr, xesc(res), rpr))
	case strings.HasPrefix(r.Name, "REF "):
		fs := strings.Fields(r.Name)
		res = w.doc.FieldValue(r.Name, "1", "1")
		bm := fs[1]
		mode := "l"
		if len(fs) > 2 {
			mode = fs[2]
		}
		pre, post := "", ""
		switch mode {
		case "n":
			bm += "_n"
		case "p":
			bm += "_n"
			pre, post, res = "(", ")", strings.TrimSuffix(strings.TrimPrefix(res, "("), ")")
		case "s":
			bm += "_n"
			if i := strings.LastIndex(res, " "); i >= 0 {
				pre, res = res[:i+1], res[i+1:]
			}
		}
		instr = "REF " + bm + ` \h`
		txt := func(t string) string {
			if t == "" {
				return ""
			}
			return `<w:r>` + rpr + `<w:t xml:space="preserve">` + xesc(t) + `</w:t></w:r>`
		}
		return w.revWrap(r.F, txt(pre)+fmt.Sprintf(`<w:r>%s<w:fldChar w:fldCharType="begin"/></w:r><w:r>%s<w:instrText xml:space="preserve"> %s </w:instrText></w:r>`+
			`<w:r>%s<w:fldChar w:fldCharType="separate"/></w:r><w:r>%s<w:t xml:space="preserve">%s</w:t></w:r><w:r>%s<w:fldChar w:fldCharType="end"/></w:r>`,
			rpr, rpr, xesc(instr), rpr, rpr, xesc(res), rpr)+txt(post))
	}
	return w.revWrap(r.F, fmt.Sprintf(`<w:r>%s<w:fldChar w:fldCharType="begin"/></w:r><w:r>%s<w:instrText xml:space="preserve"> %s </w:instrText></w:r>`+
		`<w:r>%s<w:fldChar w:fldCharType="separate"/></w:r><w:r>%s<w:t xml:space="preserve">%s</w:t></w:r><w:r>%s<w:fldChar w:fldCharType="end"/></w:r>`,
		rpr, rpr, xesc(instr), rpr, rpr, xesc(res), rpr))
}

func (w *docxWriter) cmtStart(f F) string {
	if f.Cmt == "" || w.cmtStarted[f.Cmt] {
		return ""
	}
	n, ok := w.cmtIDs[f.Cmt]
	if !ok {
		return ""
	}
	w.cmtStarted[f.Cmt] = true
	return fmt.Sprintf(`<w:commentRangeStart w:id="%d"/>`, n)
}

func (w *docxWriter) cmtEnd(p *Para, i int, f F) string {
	if f.Cmt == "" || w.cmtLast[f.Cmt] != (cmtKey{p, i}) {
		return ""
	}
	n := w.cmtIDs[f.Cmt]
	return fmt.Sprintf(`<w:commentRangeEnd w:id="%d"/><w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:commentReference w:id="%d"/></w:r>`, n, n)
}

func (w *docxWriter) image(p *wpart, r Run) string {
	m := w.doc.Media[r.ID]
	if m == nil {
		return ""
	}
	target, ok := w.mediaDone[r.ID]
	if !ok {
		target = fmt.Sprintf("media/image%d.%s", len(w.mediaDone)+1, extByMime(m.Mime))
		w.files = append(w.files, wfile{"word/" + target, m.Data, true})
		w.mediaDone[r.ID] = target
	}
	rid := p.rel("image", target, false)
	cx, cy := int(math.Round(r.W*12700)), int(math.Round(r.H*12700))
	w.drawID++
	id := w.drawID
	if r.Wrap != "" {
		side := r.Side
		if side == "" {
			side = "left"
		}
		wrap := `<wp:wrapSquare wrapText="bothSides"/>`
		behind := "0"
		switch r.Wrap {
		case "topbottom":
			wrap = `<wp:wrapTopAndBottom/>`
		case "behind":
			wrap, behind = `<wp:wrapNone/>`, "1"
		case "front":
			wrap = `<wp:wrapNone/>`
		}
		g := int(floatGap * 12700)
		return fmt.Sprintf(`<w:r><w:drawing><wp:anchor distT="%d" distB="%d" distL="%d" distR="%d" simplePos="0" relativeHeight="%d" behindDoc="%s" locked="0" layoutInCell="1" allowOverlap="1">`+
			`<wp:simplePos x="0" y="0"/><wp:positionH relativeFrom="column"><wp:align>%s</wp:align></wp:positionH><wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV>`+
			`<wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/>%s<wp:docPr id="%d" name="Рисунок %d"/>`+
			`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>`+
			`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>`+
			`<pic:nvPicPr><pic:cNvPr id="%d" name="image%d"/><pic:cNvPicPr/></pic:nvPicPr>`+
			`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
			`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
			`</pic:pic></a:graphicData></a:graphic></wp:anchor></w:drawing></w:r>`, g/2, g/2, g, g, 251658240+id, behind, side, cx, cy, wrap, id, id, id, id, rid, cx, cy)
	}
	return fmt.Sprintf(`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/>`+
		`<wp:effectExtent l="0" t="0" r="0" b="0"/><wp:docPr id="%d" name="Рисунок %d"/>`+
		`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>`+
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="image%d"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`, cx, cy, id, id, id, id, rid, cx, cy)
}

func (w *docxWriter) runs(p *wpart, para *Para) string {
	var sb strings.Builder
	rs := para.Runs
	// формула одна в абзаце — выключная (m:oMathPara)
	if len(rs) == 1 && rs[0].T == REq {
		return w.cmtStart(rs[0].F) + "<m:oMathPara>" + eqOMML(string(rs[0].Text)) + "</m:oMathPara>" + w.cmtEnd(para, 0, rs[0].F)
	}
	for i := 0; i < len(rs); {
		r := rs[i]
		if (r.T == RText || r.T == RField) && r.F.Link != "" {
			url := r.F.Link
			var inner strings.Builder
			for i < len(rs) && (rs[i].T == RText || rs[i].T == RField) && rs[i].F.Link == url {
				f := rs[i].F
				f.Link = ""
				if !strings.HasPrefix(url, "#_Toc") {
					if f.Color == "" {
						f.Color = "#0563c1"
					}
					f.U = true
				}
				inner.WriteString(w.cmtStart(f))
				if rs[i].T == RField {
					fr := rs[i]
					fr.F = f
					inner.WriteString(w.fieldRun(fr))
				} else {
					inner.WriteString(w.textRun(rs[i].Text, f))
				}
				inner.WriteString(w.cmtEnd(para, i, f))
				i++
			}
			if strings.HasPrefix(url, "#") {
				fmt.Fprintf(&sb, `<w:hyperlink w:anchor="%s" w:history="1">%s</w:hyperlink>`, xesc(url[1:]), inner.String())
			} else {
				fmt.Fprintf(&sb, `<w:hyperlink r:id="%s" w:history="1">%s</w:hyperlink>`, p.rel("hyperlink", url, true), inner.String())
			}
			continue
		}
		sb.WriteString(w.cmtStart(r.F))
		switch r.T {
		case RText:
			sb.WriteString(w.textRun(r.Text, r.F))
		case RBreak:
			sb.WriteString("<w:r><w:br/></w:r>")
		case RImage:
			sb.WriteString(w.image(p, r))
		case RNote:
			if n, ok := w.fnIDs[r.ID]; ok {
				fmt.Fprintf(&sb, `<w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteReference w:id="%d"/></w:r>`, n)
			}
		case RField:
			if st := w.seqBM; st != nil && strings.HasPrefix(r.Name, "SEQ ") && !st.done {
				st.done = true
				fmt.Fprintf(&sb, `<w:bookmarkStart w:id="%d" w:name="%s_n"/>`, st.n, xesc(st.name))
				sb.WriteString(w.fieldRun(r))
				fmt.Fprintf(&sb, `<w:bookmarkEnd w:id="%d"/><w:bookmarkEnd w:id="%d"/>`, st.n, st.a)
				break
			}
			sb.WriteString(w.fieldRun(r))
		case REq:
			sb.WriteString(eqOMML(string(r.Text)))
		}
		sb.WriteString(w.cmtEnd(para, i, r.F))
		i++
	}
	return sb.String()
}

type seqBMState struct {
	name string
	a, n int
	done bool
}

func paraHasSeq(p *Para) bool {
	for _, r := range p.Runs {
		if r.T == RField && strings.HasPrefix(r.Name, "SEQ ") {
			return true
		}
	}
	return false
}

func (w *docxWriter) listNum(l *List) int {
	L, ok := w.lists[l.ID]
	if !ok {
		L = &wlist{num: len(w.lists) + 1, kind: l.Kind}
		w.lists[l.ID] = L
		w.listOrder = append(w.listOrder, l.ID)
	}
	if lv := min(8, max(0, l.Level)); L.levels[lv] == nil {
		c := *l
		L.levels[lv] = &c
	}
	return L.num
}

var styleIDs = map[string]string{"Title": "Title", "Subtitle": "Subtitle", "Quote": "Quote", "NoSpacing": "NoSpacing",
	"TOC1": "TOC1", "TOC2": "TOC2", "TOC3": "TOC3", "TOCHeading": "TOCHeading"}

func (w *docxWriter) pPr(p *Para, forceStyle, extraRuns string) string {
	pf := p.PF
	if w.cell > 0 { // как у нас на экране: в таблице без интервала после и с одинарным междустрочным
		if !pf.After.Set {
			pf.After = O(0)
		}
		if !pf.Line.Set && pf.LineExact == 0 {
			pf.Line = O(1)
		}
	}
	var sb strings.Builder
	style := forceStyle
	if style == "" {
		if pf.Heading > 0 {
			style = "Heading" + strconv.Itoa(pf.Heading)
		} else if s, ok := styleIDs[pf.Style]; ok {
			style = s
		} else if pf.List != nil {
			style = "ListParagraph"
		}
	}
	if style != "" {
		fmt.Fprintf(&sb, `<w:pStyle w:val="%s"/>`, style)
	}
	if pf.KeepNext {
		sb.WriteString("<w:keepNext/>")
	}
	if pf.PB {
		sb.WriteString("<w:pageBreakBefore/>")
	}
	if pf.NoWidow {
		sb.WriteString(`<w:widowControl w:val="0"/>`)
	}
	if pf.List != nil {
		fmt.Fprintf(&sb, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, pf.List.Level, w.listNum(pf.List))
	}
	if hasBdr(pf.BdrTop) || hasBdr(pf.BdrBottom) {
		sb.WriteString("<w:pBdr>")
		if hasBdr(pf.BdrTop) {
			sb.WriteString(strings.Replace(borderXML("top", pf.BdrTop), `w:space="0"`, `w:space="1"`, 1))
		}
		if hasBdr(pf.BdrBottom) {
			sb.WriteString(strings.Replace(borderXML("bottom", pf.BdrBottom), `w:space="0"`, `w:space="1"`, 1))
		}
		sb.WriteString("</w:pBdr>")
	}
	if pf.Shade != "" {
		fmt.Fprintf(&sb, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, colorHex(pf.Shade))
	}
	tabs := pf.Tabs
	if len(tabs) == 0 && strings.HasPrefix(pf.Style, "TOC") && pf.Style != "TOCHeading" {
		tabs = []TabStop{{Pos: w.width, Align: "right", Leader: "dot"}}
	}
	if len(tabs) > 0 {
		sb.WriteString("<w:tabs>")
		for _, t := range tabs {
			al := t.Align
			if al == "" {
				al = "left"
			}
			ld := ""
			if t.Leader != "" {
				ld = ` w:leader="` + t.Leader + `"`
			}
			fmt.Fprintf(&sb, `<w:tab w:val="%s"%s w:pos="%d"/>`, al, ld, tw(t.Pos))
		}
		sb.WriteString("</w:tabs>")
	}
	var sp []string
	if pf.Before.Set {
		sp = append(sp, fmt.Sprintf(`w:before="%d"`, tw(pf.Before.V)))
	}
	if pf.After.Set {
		sp = append(sp, fmt.Sprintf(`w:after="%d"`, tw(pf.After.V)))
	}
	if pf.LineExact > 0 {
		rule := "exact"
		if pf.LineRule == "atLeast" {
			rule = "atLeast"
		}
		sp = append(sp, fmt.Sprintf(`w:line="%d" w:lineRule="%s"`, tw(pf.LineExact), rule))
	} else if pf.Line.Set {
		sp = append(sp, fmt.Sprintf(`w:line="%d" w:lineRule="auto"`, int(math.Round(pf.Line.V*240))))
	}
	if len(sp) > 0 {
		sb.WriteString("<w:spacing " + strings.Join(sp, " ") + "/>")
	}
	var ind []string
	if pf.IndL.Set {
		ind = append(ind, fmt.Sprintf(`w:left="%d"`, tw(pf.IndL.V)))
	}
	if pf.IndR != 0 {
		ind = append(ind, fmt.Sprintf(`w:right="%d"`, tw(pf.IndR)))
	}
	if pf.IndF.Set {
		if pf.IndF.V < 0 {
			ind = append(ind, fmt.Sprintf(`w:hanging="%d"`, tw(-pf.IndF.V)))
		} else {
			ind = append(ind, fmt.Sprintf(`w:firstLine="%d"`, tw(pf.IndF.V)))
		}
	}
	if len(ind) > 0 {
		sb.WriteString("<w:ind " + strings.Join(ind, " ") + "/>")
	}
	if pf.CS != "" {
		sb.WriteString("<w:contextualSpacing/>")
	}
	if pf.Align != "" && pf.Align != "left" {
		fmt.Fprintf(&sb, `<w:jc w:val="%s"/>`, map[string]string{"center": "center", "right": "right", "justify": "both"}[pf.Align])
	}
	if pf.RevPara != "" && len(p.Runs) > 0 {
		// знак абзаца — часть вставки/удаления (как пишет Word): при принятии абзац исчезает целиком
		f := p.Runs[0].F
		id := f.Del
		if pf.RevPara == "ins" {
			id = f.Ins
		}
		rv := w.doc.Revs[id]
		author, date := "Texora", nowISO()
		if rv != nil {
			author, date = rv.Author, rv.Date
		}
		w.revN++
		fmt.Fprintf(&sb, `<w:rPr><w:%s w:id="%d" w:author="%s" w:date="%s"/></w:rPr>`, pf.RevPara, 1000+w.revN, xesc(author), xesc(date))
	} else if len(p.Runs) == 0 && p.Mark != (F{}) {
		sb.WriteString(w.rPr(p.Mark, ""))
	}
	if pf.Sect != nil && w.cell == 0 {
		s := pf.Sect
		sb.WriteString(w.sectPr(s.Page, s.Cols, s.TitlePg, s.Start, s.Type, &s.HF, s.ID))
	}
	out := ""
	if sb.Len() > 0 {
		out = "<w:pPr>" + sb.String() + "</w:pPr>"
	}
	return out + extraRuns
}

func (w *docxWriter) para(part *wpart, p *Para, style, prefix string) string {
	bs, be := "", ""
	if p.PF.Bookmark != "" && paraHasSeq(p) {
		// название: закладка «Рисунок 1.2» (для REF) и «1.2» (для REF …_n)
		w.bmN += 2
		a, n := w.bmN-1, w.bmN
		w.seqBM = &seqBMState{name: p.PF.Bookmark, a: a, n: n}
		bs = fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="%s"/>`, a, xesc(p.PF.Bookmark))
		runs := w.runs(part, p)
		w.seqBM = nil
		return "<w:p>" + w.pPr(p, style, prefix) + bs + colBreakXML(p) + runs + "</w:p>"
	}
	if p.PF.Bookmark != "" {
		w.bmN++
		bs = fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="%s"/>`, w.bmN, xesc(p.PF.Bookmark))
		be = fmt.Sprintf(`<w:bookmarkEnd w:id="%d"/>`, w.bmN)
	}
	return "<w:p>" + w.pPr(p, style, prefix) + bs + colBreakXML(p) + w.runs(part, p) + be + "</w:p>"
}

// colBreakXML — разрыв колонки перед абзацем (в Word это символ разрыва в начале абзаца).
func colBreakXML(p *Para) string {
	if p.PF.CB {
		return `<w:r><w:br w:type="column"/></w:r>`
	}
	return ""
}

func borderXML(name string, b *Border) string {
	if b == nil || b.W <= 0 {
		return `<w:` + name + ` w:val="nil"/>`
	}
	st := b.Style
	if st == "" {
		st = "single"
	}
	col := "auto"
	if b.Color != "" {
		col = colorHex(b.Color)
	}
	return fmt.Sprintf(`<w:%s w:val="%s" w:sz="%d" w:space="0" w:color="%s"/>`, name, st, max(2, int(math.Round(b.W*8))), col)
}

func (w *docxWriter) table(part *wpart, t *Table, width float64) string {
	cols := TableCols(t, width)
	var sb strings.Builder
	sb.WriteString(`<w:tbl><w:tblPr>`)
	if t.Border {
		sb.WriteString(`<w:tblStyle w:val="TableGrid"/>`)
	}
	sb.WriteString(`<w:tblW w:w="0" w:type="auto"/>`)
	if t.Align != "" {
		fmt.Fprintf(&sb, `<w:jc w:val="%s"/>`, t.Align)
	}
	if t.Ind != 0 {
		fmt.Fprintf(&sb, `<w:tblInd w:w="%d" w:type="dxa"/>`, tw(t.Ind))
	}
	if t.TB != nil {
		tb := t.TB
		sb.WriteString("<w:tblBorders>")
		for i, b := range []*Border{tb.Top, tb.Left, tb.Bottom, tb.Right, tb.InsideH, tb.InsideV} {
			sb.WriteString(borderXML([]string{"top", "left", "bottom", "right", "insideH", "insideV"}[i], b))
		}
		sb.WriteString("</w:tblBorders>")
	} else if t.Border {
		sb.WriteString("<w:tblBorders>")
		for _, k := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
			fmt.Fprintf(&sb, `<w:%s w:val="single" w:sz="4" w:space="0" w:color="auto"/>`, k)
		}
		sb.WriteString("</w:tblBorders>")
	}
	fmt.Fprintf(&sb, `<w:tblCellMar><w:top w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:bottom w:w="%d" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:tblCellMar>`,
		tw(t.PadT()), tw(t.PadL()), tw(t.PadB()), tw(t.PadR()))
	sb.WriteString(`<w:tblLayout w:type="fixed"/><w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="1" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/></w:tblPr><w:tblGrid>`)
	for _, c := range cols {
		fmt.Fprintf(&sb, `<w:gridCol w:w="%d"/>`, tw(c))
	}
	sb.WriteString("</w:tblGrid>")
	for _, row := range t.Rows {
		sb.WriteString("<w:tr>")
		if row.Header || row.H > 0 {
			sb.WriteString("<w:trPr>")
			if row.H > 0 {
				rule := "atLeast"
				if row.HRule == "exact" {
					rule = "exact"
				}
				fmt.Fprintf(&sb, `<w:trHeight w:val="%d" w:hRule="%s"/>`, tw(row.H), rule)
			}
			if row.Header {
				sb.WriteString("<w:tblHeader/>")
			}
			sb.WriteString("</w:trPr>")
		}
		gi := 0
		for _, c := range row.Cells {
			span := max(1, c.Span)
			cw := 0.0
			for k := gi; k < gi+span && k < len(cols); k++ {
				cw += cols[k]
			}
			gi += span
			fmt.Fprintf(&sb, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, tw(cw))
			if span > 1 {
				fmt.Fprintf(&sb, `<w:gridSpan w:val="%d"/>`, span)
			}
			switch c.VMerge {
			case "restart":
				sb.WriteString(`<w:vMerge w:val="restart"/>`)
			case "continue":
				sb.WriteString(`<w:vMerge/>`)
			}
			if c.B != [4]*Border{} {
				sb.WriteString("<w:tcBorders>")
				for i, b := range c.B {
					if b != nil {
						sb.WriteString(borderXML([]string{"top", "left", "bottom", "right"}[i], b))
					}
				}
				sb.WriteString("</w:tcBorders>")
			}
			if c.Shade != "" {
				fmt.Fprintf(&sb, `<w:shd w:val="clear" w:color="auto" w:fill="%s"/>`, colorHex(c.Shade))
			}
			if c.Pad != nil {
				fmt.Fprintf(&sb, `<w:tcMar><w:top w:w="%d" w:type="dxa"/><w:left w:w="%d" w:type="dxa"/><w:bottom w:w="%d" w:type="dxa"/><w:right w:w="%d" w:type="dxa"/></w:tcMar>`,
					tw(c.Pad[0]), tw(c.Pad[1]), tw(c.Pad[2]), tw(c.Pad[3]))
			}
			if c.VAlign != "" {
				fmt.Fprintf(&sb, `<w:vAlign w:val="%s"/>`, c.VAlign)
			}
			sb.WriteString("</w:tcPr>")
			w.cell++
			sb.WriteString(w.blocks(part, c.Blocks, cw))
			w.cell--
			if len(c.Blocks) == 0 || c.Blocks[len(c.Blocks)-1].P == nil {
				sb.WriteString("<w:p/>")
			}
			sb.WriteString("</w:tc>")
		}
		sb.WriteString("</w:tr>")
	}
	sb.WriteString("</w:tbl>")
	return sb.String()
}

func (w *docxWriter) blocks(part *wpart, bs []Block, width float64) string {
	var sb strings.Builder
	for i := 0; i < len(bs); i++ {
		b := bs[i]
		if b.P != nil && b.P.PF.Region != "" && w.cell == 0 {
			// поле, занимающее несколько абзацев: TOC или список литературы
			kind := b.P.PF.Region
			j := i
			for j < len(bs) && bs[j].P != nil && bs[j].P.PF.Region == kind {
				j++
			}
			instr := ` TOC \o "1-3" \h \z \u `
			if kind == "bib" {
				instr = ` ADDIN TEXORA_BIB `
			} else if strings.HasPrefix(kind, "lof:") {
				instr = ` TOC \h \z \c "` + strings.TrimPrefix(kind, "lof:") + `" `
			}
			begin := `<w:r><w:fldChar w:fldCharType="begin" w:dirty="true"/></w:r><w:r><w:instrText xml:space="preserve">` + xesc(instr) + `</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>`
			if kind == "bib" {
				begin = strings.Replace(begin, ` w:dirty="true"`, "", 1)
			}
			end := `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
			for k := i; k < j; k++ {
				p := bs[k].P
				x := w.para(part, p, "", "")
				if k == i {
					// начало поля — сразу после свойств абзаца
					if at := strings.Index(x, "</w:pPr>"); at >= 0 {
						x = x[:at+8] + begin + x[at+8:]
					} else {
						x = "<w:p>" + begin + x[5:]
					}
				}
				if k == j-1 {
					x = x[:len(x)-6] + end + "</w:p>"
				}
				sb.WriteString(x)
			}
			i = j - 1
			continue
		}
		if b.P != nil {
			sb.WriteString(w.para(part, b.P, "", ""))
		} else if b.T != nil {
			sb.WriteString(w.table(part, b.T, width))
		}
	}
	return sb.String()
}

// sectPr — свойства раздела с колонтитулами.
func (w *docxWriter) sectPr(pg Page, cols Columns, titlePg bool, start int, typ string, hf *HF, id string) string {
	var refs strings.Builder
	width := pg.W - pg.Left - pg.Right
	for _, kind := range []string{"header", "footer"} {
		for _, v := range []string{"", "first", "even"} {
			key := kind
			wt := "default"
			if v != "" {
				key += "." + v
				wt = v
			}
			bs := *hf.Get(key)
			if len(bs) == 0 {
				continue
			}
			w.hfN++
			name := fmt.Sprintf("%s%d.xml", kind, w.hfN)
			root, style := "hdr", "Header"
			if kind == "footer" {
				root, style = "ftr", "Footer"
			}
			w.story(name, root, bs, width, kind, style)
			fmt.Fprintf(&refs, `<w:%sReference w:type="%s" r:id="%s"/>`, kind, wt, w.mainPart.rel(kind, name, false))
		}
	}
	orient := ""
	if pg.W > pg.H {
		orient = ` w:orient="landscape"`
	}
	tp := ""
	if titlePg {
		tp = "<w:titlePg/>"
	}
	ty := ""
	if typ != "" {
		ty = `<w:type w:val="` + typ + `"/>`
	}
	num := ""
	if start > 0 {
		num = fmt.Sprintf(`<w:pgNumType w:start="%d"/>`, start)
	}
	return fmt.Sprintf(`<w:sectPr>%s%s<w:pgSz w:w="%d" w:h="%d"%s/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="%d" w:footer="%d" w:gutter="0"/>%s%s%s<w:docGrid w:linePitch="360"/></w:sectPr>`,
		refs.String(), ty, tw(pg.W), tw(pg.H), orient, tw(pg.Top), tw(pg.Right), tw(pg.Bottom), tw(pg.Left), tw(pg.Header), tw(pg.Footer), num, colsXML(cols), tp)
}

func (w *docxWriter) story(name, root string, bs []Block, width float64, ctype, style string) {
	part := &wpart{name: name}
	var body string
	if len(bs) == 0 {
		body = `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr></w:p>`
	} else {
		var sb strings.Builder
		for _, b := range bs {
			if b.P != nil {
				st := ""
				if b.P.PF.Heading == 0 && b.P.PF.Style == "" && b.P.PF.List == nil {
					st = style
				}
				sb.WriteString(w.para(part, b.P, st, ""))
			} else {
				sb.WriteString(w.table(part, b.T, width))
			}
		}
		body = sb.String()
		if bs[len(bs)-1].P == nil {
			body += "<w:p/>"
		}
	}
	w.files = append(w.files, wfile{name: "word/" + name, data: []byte(xmlHead + "<w:" + root + " " + wNS + ">" + body + "</w:" + root + ">")})
	if len(part.rels) > 0 {
		w.files = append(w.files, wfile{name: "word/_rels/" + name + ".rels", data: []byte(part.xml())})
	}
	w.ctypes = append(w.ctypes, `<Override PartName="/word/`+name+`" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.`+ctype+`+xml"/>`)
}

func (w *docxWriter) stylesXML() string {
	d := w.doc.Def
	font := xesc(d.Font)
	var sb strings.Builder
	sb.WriteString(xmlHead + "<w:styles " + wNS + ">")
	fmt.Fprintf(&sb, `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`+
		`<w:sz w:val="%d"/><w:szCs w:val="%d"/><w:lang w:val="ru-RU" w:eastAsia="en-US" w:bidi="ar-SA"/></w:rPr></w:rPrDefault>`+
		`<w:pPrDefault><w:pPr><w:spacing w:after="%d" w:line="%d" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>`,
		font, font, font, font, int(math.Round(d.Size*2)), int(math.Round(d.Size*2)), tw(d.After), int(math.Round(d.Line*240)))
	sb.WriteString(`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`)
	hf := headingFont(w.doc)
	for n := 1; n <= 6; n++ {
		h, _ := w.doc.StyleDef("h" + strconv.Itoa(n))
		font := h.Font
		if font == "" && n <= 2 {
			font = hf
		}
		ppr, rpr := styleProps(h, font, d)
		fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/>`+
			`<w:next w:val="Normal"/><w:link w:val="Heading%dChar"/><w:uiPriority w:val="9"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/>`+
			`%s<w:outlineLvl w:val="%d"/></w:pPr><w:rPr>%s</w:rPr></w:style>`,
			n, n, n, ppr, n-1, rpr)
		fmt.Fprintf(&sb, `<w:style w:type="character" w:customStyle="1" w:styleId="Heading%dChar"><w:name w:val="Заголовок %d Знак"/><w:link w:val="Heading%d"/><w:uiPriority w:val="9"/><w:rPr>%s</w:rPr></w:style>`,
			n, n, n, rpr)
	}
	for k := 1; k <= 3; k++ {
		h, _ := w.doc.StyleDef("TOC" + strconv.Itoa(k))
		ppr, rpr := styleProps(h, h.Font, d)
		fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="TOC%d"><w:name w:val="toc %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:autoRedefine/><w:uiPriority w:val="39"/><w:unhideWhenUsed/><w:pPr>%s</w:pPr><w:rPr>%s</w:rPr></w:style>`, k, k, ppr, rpr)
	}
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="CommentText"><w:name w:val="annotation text"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/><w:pPr><w:spacing w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>`)
	sb.WriteString(`<w:style w:type="character" w:styleId="CommentReference"><w:name w:val="annotation reference"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/><w:rPr><w:sz w:val="16"/><w:szCs w:val="16"/></w:rPr></w:style>`)
	t, _ := w.doc.StyleDef("Title")
	fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="10"/><w:qFormat/>`+
		`<w:pPr><w:spacing w:after="%d" w:line="240" w:lineRule="auto"/><w:contextualSpacing/></w:pPr><w:rPr><w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/><w:spacing w:val="-10"/><w:kern w:val="28"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
		tw(t.After), xesc(hf), xesc(hf), xesc(hf), xesc(hf), int(t.Size*2), int(t.Size*2))
	st, _ := w.doc.StyleDef("Subtitle")
	fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="11"/><w:qFormat/>`+
		`<w:pPr><w:spacing w:after="%d"/></w:pPr><w:rPr><w:color w:val="595959"/><w:spacing w:val="15"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
		tw(st.After), int(st.Size*2), int(st.Size*2))
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="29"/><w:qFormat/>` +
		`<w:pPr><w:spacing w:before="160"/><w:ind w:left="864" w:right="864"/><w:jc w:val="center"/></w:pPr><w:rPr><w:i/><w:iCs/><w:color w:val="404040"/></w:rPr></w:style>`)
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="NoSpacing"><w:name w:val="No Spacing"/><w:uiPriority w:val="1"/><w:qFormat/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:style>`)
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="34"/><w:qFormat/><w:pPr><w:ind w:left="720"/><w:contextualSpacing/></w:pPr></w:style>`)
	sb.WriteString(`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/><w:uiPriority w:val="1"/><w:semiHidden/><w:unhideWhenUsed/></w:style>`)
	sb.WriteString(`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/><w:tblPr><w:tblInd w:w="0" w:type="dxa"/><w:tblCellMar><w:top w:w="0" w:type="dxa"/><w:left w:w="108" w:type="dxa"/><w:bottom w:w="0" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	sb.WriteString(`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:basedOn w:val="TableNormal"/><w:uiPriority w:val="39"/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:left w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:right w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="auto"/></w:tblBorders></w:tblPr></w:style>`)
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="FootnoteText"><w:name w:val="footnote text"/><w:basedOn w:val="Normal"/><w:link w:val="FootnoteTextChar"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>`)
	sb.WriteString(`<w:style w:type="character" w:customStyle="1" w:styleId="FootnoteTextChar"><w:name w:val="Текст сноски Знак"/><w:link w:val="FootnoteText"/><w:uiPriority w:val="99"/><w:semiHidden/><w:rPr><w:sz w:val="20"/><w:szCs w:val="20"/></w:rPr></w:style>`)
	sb.WriteString(`<w:style w:type="character" w:styleId="FootnoteReference"><w:name w:val="footnote reference"/><w:uiPriority w:val="99"/><w:semiHidden/><w:unhideWhenUsed/><w:rPr><w:vertAlign w:val="superscript"/></w:rPr></w:style>`)
	sb.WriteString(`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>`)
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="Header"><w:name w:val="header"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/><w:pPr><w:tabs><w:tab w:val="center" w:pos="4677"/><w:tab w:val="right" w:pos="9355"/></w:tabs><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:style>`)
	sb.WriteString(`<w:style w:type="paragraph" w:styleId="Footer"><w:name w:val="footer"/><w:basedOn w:val="Normal"/><w:uiPriority w:val="99"/><w:unhideWhenUsed/><w:pPr><w:tabs><w:tab w:val="center" w:pos="4677"/><w:tab w:val="right" w:pos="9355"/></w:tabs><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:style>`)
	{
		h, _ := w.doc.StyleDef("TOCHeading")
		font := h.Font
		if font == "" {
			font = hf
		}
		ppr, rpr := styleProps(h, font, d)
		fmt.Fprintf(&sb, `<w:style w:type="paragraph" w:styleId="TOCHeading"><w:name w:val="TOC Heading"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="39"/><w:unhideWhenUsed/><w:qFormat/><w:pPr><w:keepNext/>%s<w:outlineLvl w:val="9"/></w:pPr><w:rPr>%s</w:rPr></w:style>`, ppr, rpr)
	}
	sb.WriteString("</w:styles>")
	return sb.String()
}

// styleProps — pPr/rPr стиля абзаца (без обёрток).
func styleProps(h headingStyle, font string, d Defaults) (string, string) {
	var p, r strings.Builder
	fmt.Fprintf(&p, `<w:spacing w:before="%d" w:after="%d"`, tw(math.Max(0, h.Before)), tw(math.Max(0, h.After)))
	if h.Line > 0 {
		fmt.Fprintf(&p, ` w:line="%d" w:lineRule="auto"`, int(math.Round(h.Line*240)))
	}
	p.WriteString("/>")
	if h.IndL != 0 || h.Set {
		fmt.Fprintf(&p, `<w:ind w:left="%d"`, tw(h.IndL))
		if h.IndF < 0 {
			fmt.Fprintf(&p, ` w:hanging="%d"`, tw(-h.IndF))
		} else {
			fmt.Fprintf(&p, ` w:firstLine="%d"`, tw(h.IndF))
		}
		p.WriteString("/>")
	}
	if h.Align != "" && h.Align != "left" {
		fmt.Fprintf(&p, `<w:jc w:val="%s"/>`, map[string]string{"center": "center", "right": "right", "justify": "both"}[h.Align])
	}
	if font != "" && font != d.Font {
		fmt.Fprintf(&r, `<w:rFonts w:ascii="%s" w:hAnsi="%s" w:eastAsia="%s" w:cs="%s"/>`, xesc(font), xesc(font), xesc(font), xesc(font))
	}
	if h.B {
		r.WriteString("<w:b/><w:bCs/>")
	}
	if h.I {
		r.WriteString("<w:i/><w:iCs/>")
	}
	if h.Caps {
		r.WriteString("<w:caps/>")
	}
	if h.Color != "" {
		fmt.Fprintf(&r, `<w:color w:val="%s"/>`, colorHex(h.Color))
	}
	if h.Size > 0 {
		fmt.Fprintf(&r, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, int(math.Round(h.Size*2)), int(math.Round(h.Size*2)))
	}
	return p.String(), r.String()
}

func (w *docxWriter) numberingXML() string {
	if len(w.lists) == 0 {
		return ""
	}
	var abs, nums strings.Builder
	bul := []string{"•", "o", "▪"}
	def := []string{"decimal", "lowerLetter", "lowerRoman"}
	for _, id := range w.listOrder {
		L := w.lists[id]
		fmt.Fprintf(&abs, `<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/>`, L.num)
		for i := 0; i < 9; i++ {
			g := L.levels[i]
			var fmtS, text string
			if L.kind == "ul" {
				fmtS = "bullet"
				text = bul[i%3]
				if g != nil && g.Text != "" && !strings.Contains(g.Text, "%") {
					text = g.Text
				}
			} else {
				fmtS = def[i%3]
				if g != nil && g.Fmt != "" && g.Fmt != "bullet" {
					fmtS = g.Fmt
				}
				suf := ")"
				if fmtS == "decimal" {
					suf = "."
				}
				text = fmt.Sprintf("%%%d%s", i+1, suf)
				if g != nil && g.Text != "" {
					text = g.Text
				}
			}
			rpr := ""
			if L.kind == "ul" {
				switch text {
				case "o":
					rpr = `<w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:cs="Courier New" w:hint="default"/></w:rPr>`
				case "•":
					rpr = `<w:rPr><w:rFonts w:ascii="Symbol" w:hAnsi="Symbol" w:hint="default"/></w:rPr>`
					text = ""
				case "▪":
					rpr = `<w:rPr><w:rFonts w:ascii="Wingdings" w:hAnsi="Wingdings" w:hint="default"/></w:rPr>`
					text = ""
				}
			}
			fmt.Fprintf(&abs, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/>`+
				`<w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr>%s</w:lvl>`, i, fmtS, xesc(text), 720*(i+1), rpr)
		}
		abs.WriteString("</w:abstractNum>")
		fmt.Fprintf(&nums, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/></w:num>`, L.num, L.num)
	}
	return xmlHead + "<w:numbering " + wNS + ">" + abs.String() + nums.String() + "</w:numbering>"
}

func WriteDocx(d *Doc) ([]byte, error) {
	d.ensure()
	w := &docxWriter{doc: d, mediaDone: map[string]string{}, lists: map[string]*wlist{}, fnIDs: map[string]int{},
		cmtIDs: map[string]int{}, cmtLast: map[string]cmtKey{}, cmtStarted: map[string]bool{}}
	for i, id := range d.FootnoteOrder() {
		w.fnIDs[id] = i + 1
	}
	// примечания: номер и последний ран каждого
	scanCmt := func(bs []Block) {
		for _, en := range Flatten(&bs, nil, nil) {
			for i, r := range en.P.Runs {
				if r.F.Cmt != "" && d.Comments[r.F.Cmt] != nil {
					if _, ok := w.cmtIDs[r.F.Cmt]; !ok {
						w.cmtIDs[r.F.Cmt] = len(w.cmtIDs)
					}
					w.cmtLast[r.F.Cmt] = cmtKey{en.P, i}
				}
			}
		}
	}
	scanCmt(d.Body)
	for _, id := range d.FootnoteOrder() {
		scanCmt(d.Footnotes[id].Blocks)
	}
	pg := d.Page
	width := pg.W - pg.Left - pg.Right
	w.width = width
	main := &wpart{name: "document.xml"}
	w.mainPart = main
	main.rel("styles", "styles.xml", false)
	main.rel("settings", "settings.xml", false)
	main.rel("fontTable", "fontTable.xml", false)
	body := w.blocks(main, d.Body, width)
	if n := len(d.Body); n == 0 || d.Body[n-1].P == nil {
		body += "<w:p/>"
	}
	// последний раздел; если первая страница особая, а своего колонтитула нет — пустой, как в Word
	hf := d.HF
	if d.TitlePg {
		ss := d.Sections()
		last := len(ss) - 1
		if _, bs := d.ResolveHF(ss, last, "header", "first"); len(bs) == 0 {
			hf.HeaderFirst = []Block{{P: NewPara()}}
		}
		if _, bs := d.ResolveHF(ss, last, "footer", "first"); len(bs) == 0 {
			hf.FooterFirst = []Block{{P: NewPara()}}
		}
	}
	sect := w.sectPr(pg, d.Cols, d.TitlePg, d.Start, d.Type, &hf, "")
	if len(w.cmtIDs) > 0 {
		main.rel("comments", "comments.xml", false)
		var sb strings.Builder
		ids := make([]string, len(w.cmtIDs))
		for id, n := range w.cmtIDs {
			ids[n] = id
		}
		for n, id := range ids {
			c := d.Comments[id]
			fmt.Fprintf(&sb, `<w:comment w:id="%d" w:author="%s" w:date="%s" w:initials="%s">`, n, xesc(c.Author), xesc(c.Date), xesc(c.Initials))
			for li, line := range strings.Split(c.Text, "\n") {
				ref := ""
				if li == 0 {
					ref = `<w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:annotationRef/></w:r>`
				}
				fmt.Fprintf(&sb, `<w:p><w:pPr><w:pStyle w:val="CommentText"/></w:pPr>%s<w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, ref, xesc(line))
			}
			sb.WriteString("</w:comment>")
		}
		w.files = append(w.files, wfile{name: "word/comments.xml", data: []byte(xmlHead + "<w:comments " + wNS + ">" + sb.String() + "</w:comments>")})
		w.ctypes = append(w.ctypes, `<Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/>`)
	}
	if len(w.fnIDs) > 0 {
		main.rel("footnotes", "footnotes.xml", false)
		part := &wpart{name: "footnotes.xml"}
		sep := func(t string) string {
			return `<w:p><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:r><w:` + t + `/></w:r></w:p>`
		}
		var sb strings.Builder
		sb.WriteString(`<w:footnote w:type="separator" w:id="-1">` + sep("separator") + `</w:footnote>`)
		sb.WriteString(`<w:footnote w:type="continuationSeparator" w:id="0">` + sep("continuationSeparator") + `</w:footnote>`)
		order := d.FootnoteOrder()
		for _, id := range order {
			n := w.fnIDs[id]
			bs := d.Footnotes[id].Blocks
			fmt.Fprintf(&sb, `<w:footnote w:id="%d">`, n)
			ref := `<w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> </w:t></w:r>`
			if len(bs) == 0 {
				sb.WriteString(`<w:p><w:pPr><w:pStyle w:val="FootnoteText"/></w:pPr>` + ref + `</w:p>`)
			}
			for i, b := range bs {
				if b.P == nil {
					sb.WriteString(w.table(part, b.T, width))
					continue
				}
				pre := ""
				if i == 0 {
					pre = ref
				}
				sb.WriteString(w.para(part, b.P, "FootnoteText", pre))
			}
			sb.WriteString("</w:footnote>")
		}
		w.files = append(w.files, wfile{name: "word/footnotes.xml", data: []byte(xmlHead + "<w:footnotes " + wNS + ">" + sb.String() + "</w:footnotes>")})
		if len(part.rels) > 0 {
			w.files = append(w.files, wfile{name: "word/_rels/footnotes.xml.rels", data: []byte(part.xml())})
		}
		w.ctypes = append(w.ctypes, `<Override PartName="/word/footnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"/>`)
	}
	if num := w.numberingXML(); num != "" {
		main.rel("numbering", "numbering.xml", false)
		w.files = append(w.files, wfile{name: "word/numbering.xml", data: []byte(num)})
		w.ctypes = append(w.ctypes, `<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>`)
	}
	w.files = append(w.files,
		wfile{name: "word/document.xml", data: []byte(xmlHead + "<w:document " + wNS + "><w:body>" + body + sect + "</w:body></w:document>")},
		wfile{name: "word/_rels/document.xml.rels", data: []byte(main.xml())},
		wfile{name: "word/styles.xml", data: []byte(w.stylesXML())},
		wfile{name: "word/settings.xml", data: []byte(w.settingsXML())},
		wfile{name: "word/fontTable.xml", data: []byte(w.fontTableXML())},
	)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	w.files = append(w.files,
		wfile{name: "docProps/core.xml", data: []byte(xmlHead + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
			`<dc:creator>Texora</dc:creator><cp:lastModifiedBy>Texora</cp:lastModifiedBy><dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified></cp:coreProperties>`)},
		wfile{name: "docProps/app.xml", data: []byte(xmlHead + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>Texora</Application></Properties>`)},
		wfile{name: "_rels/.rels", data: []byte(xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + relBase + `officeDocument" Target="word/document.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
			`<Relationship Id="rId3" Type="` + relBase + `extended-properties" Target="docProps/app.xml"/></Relationships>`)},
	)
	var ct strings.Builder
	ct.WriteString(xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	exts := sortedKeys(mimeByExt)
	for _, e := range exts {
		fmt.Fprintf(&ct, `<Default Extension="%s" ContentType="%s"/>`, e, mimeByExt[e])
	}
	ct.WriteString(`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
		`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
		`<Override PartName="/word/fontTable.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"/>` +
		`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
		`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>`)
	for _, c := range w.ctypes {
		ct.WriteString(c)
	}
	ct.WriteString("</Types>")
	all := append([]wfile{{name: "[Content_Types].xml", data: []byte(ct.String())}}, w.files...)
	return zipFiles(all)
}

func (w *docxWriter) settingsXML() string {
	fn := ""
	if len(w.fnIDs) > 0 {
		fn = `<w:footnotePr><w:footnote w:id="-1"/><w:footnote w:id="0"/></w:footnotePr>`
	}
	d := w.doc
	pre := ""
	if d.Track {
		pre += "<w:trackRevisions/>"
	}
	pre += `<w:defaultTabStop w:val="709"/>`
	if d.Hyphen {
		pre += `<w:autoHyphenation/>`
	}
	if d.EvenOdd {
		pre += "<w:evenAndOddHeaders/>"
	}
	pre += `<w:characterSpacingControl w:val="doNotCompress"/>`
	vars := ""
	if len(d.Sources) > 0 {
		if b, err := json.Marshal(d.Sources); err == nil {
			vars = `<w:docVars><w:docVar w:name="TexoraSources" w:val="` + xesc(string(b)) + `"/></w:docVars>`
		}
	}
	return xmlHead + "<w:settings " + wNS + `><w:zoom w:percent="100"/>` + pre + fn +
		`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>` +
		vars + `<w:themeFontLang w:val="ru-RU"/><w:decimalSymbol w:val=","/><w:listSeparator w:val=";"/></w:settings>`
}

func (w *docxWriter) fontTableXML() string {
	fonts := map[string]bool{w.doc.Def.Font: true, headingFont(w.doc): true}
	var walk func(bs []Block)
	walk = func(bs []Block) {
		for _, b := range bs {
			if b.P != nil {
				for _, r := range b.P.Runs {
					if r.F.Font != "" {
						fonts[r.F.Font] = true
					}
				}
			} else if b.T != nil {
				for _, row := range b.T.Rows {
					for _, c := range row.Cells {
						walk(c.Blocks)
					}
				}
			}
		}
	}
	walk(w.doc.Body)
	walk(w.doc.Header)
	walk(w.doc.Footer)
	var sb strings.Builder
	sb.WriteString(xmlHead + "<w:fonts " + wNS + ">")
	names := sortedKeys(fonts)
	for _, f := range names {
		fmt.Fprintf(&sb, `<w:font w:name="%s"><w:charset w:val="CC"/><w:family w:val="auto"/><w:pitch w:val="variable"/></w:font>`, xesc(f))
	}
	sb.WriteString("</w:fonts>")
	return sb.String()
}

func zipFiles(files []wfile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		h.Modified = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var wr io.Writer
		var err error
		if f.store {
			// несжатые записи — с размерами и CRC в заголовке (так их понимают все программы)
			h.Method = zip.Store
			h.CRC32 = crc32.ChecksumIEEE(f.data)
			h.CompressedSize64 = uint64(len(f.data))
			h.UncompressedSize64 = uint64(len(f.data))
			wr, err = zw.CreateRaw(h)
		} else {
			wr, err = zw.CreateHeader(h)
		}
		if err != nil {
			return nil, err
		}
		if _, err := wr.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// colsXML — колонки раздела (w:cols).
func colsXML(c Columns) string {
	if c.Count() <= 1 {
		return `<w:cols w:space="708"/>`
	}
	sep := ""
	if c.Sep {
		sep = ` w:sep="1"`
	}
	return fmt.Sprintf(`<w:cols w:num="%d" w:space="%d"%s/>`, c.Count(), tw(c.ColGap()), sep)
}
