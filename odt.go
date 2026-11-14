package main

// ODT (OpenDocument) <-> модель.

import (
	"encoding/json"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	nsOffice = "urn:oasis:names:tc:opendocument:xmlns:office:1.0"
	nsStyle  = "urn:oasis:names:tc:opendocument:xmlns:style:1.0"
	nsText   = "urn:oasis:names:tc:opendocument:xmlns:text:1.0"
	nsTable  = "urn:oasis:names:tc:opendocument:xmlns:table:1.0"
	nsFo     = "urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0"
	nsSvg    = "urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0"
	nsDraw   = "urn:oasis:names:tc:opendocument:xmlns:drawing:1.0"
	nsXlink  = "http://www.w3.org/1999/xlink"
)

var lenRe = regexp.MustCompile(`^(-?[\d.]+)\s*(cm|mm|in|pt|pc|px)?$`)

func lenPt(v string) (float64, bool) {
	m := lenRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, false
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "cm":
		return n * 72 / 2.54, true
	case "mm":
		return n * 72 / 25.4, true
	case "in":
		return n * 72, true
	case "pc":
		return n * 12, true
	case "px":
		return n * 0.75, true
	}
	return n, true
}

type odStyle struct {
	parent, display string
	tp, pp, tcp, cp *Node
	gp, trp         *Node
	master, pageNum string
}

type odMaster struct {
	page    Page
	cols    Columns
	titlePg bool
	hf      HF
	even    bool
}

type odtReader struct {
	files    map[string][]byte
	doc      *Doc
	styles   map[string]*odStyle
	lists    map[string][]List
	fonts    map[string]string
	media    map[string]string
	masters  map[string]*odMaster
	secStart map[*Para][2]string // абзац начинает раздел: мастер-страница, номер
	cmt      string
	cmtNoEnd bool
	ins      string
	changes  map[string]*odChange
	inBody   bool
	skipTo   string
}

type odChange struct {
	kind, author, date string
	deleted            *Node
}

func (r *odtReader) collect(root *Node) {
	if root == nil {
		return
	}
	for _, ff := range root.Find("font-face") {
		fam := strings.Trim(ff.AttrNS(nsSvg, "font-family"), `'"`)
		if fam == "" {
			fam = ff.AttrNS(nsStyle, "name")
		}
		r.fonts[ff.AttrNS(nsStyle, "name")] = fam
	}
	add := func(s *Node, name string) {
		fam := s.AttrNS(nsStyle, "family")
		parent := s.AttrNS(nsStyle, "parent-style-name")
		if parent == "" && name != "__default" {
			parent = "__default"
		}
		disp := s.AttrNS(nsStyle, "display-name")
		if disp == "" {
			disp = name
		}
		r.styles[fam+":"+name] = &odStyle{parent: parent, display: disp, tp: s.First("text-properties"), pp: s.First("paragraph-properties"),
			tcp: s.First("table-column-properties"), cp: s.First("table-cell-properties"), gp: s.First("graphic-properties"), trp: s.First("table-row-properties"),
			master: s.AttrNS(nsStyle, "master-page-name")}
		if pp := s.First("paragraph-properties"); pp != nil {
			r.styles[fam+":"+name].pageNum = pp.AttrNS(nsStyle, "page-number")
		}
	}
	for _, s := range root.Find("style") {
		if s.NS == nsStyle {
			add(s, s.AttrNS(nsStyle, "name"))
		}
	}
	for _, s := range root.Find("default-style") {
		add(s, "__default")
	}
	for _, ls := range root.Find("list-style") {
		var levels []List
		for _, l := range ls.Elems() {
			lv, _ := strconv.Atoi(l.AttrNS(nsText, "level"))
			for len(levels) < lv {
				levels = append(levels, List{Kind: "ul"})
			}
			if lv < 1 {
				continue
			}
			switch l.Name {
			case "list-level-style-bullet":
				levels[lv-1] = List{Kind: "ul", Text: l.AttrNS(nsText, "bullet-char")}
			case "list-level-style-number":
				nf := l.AttrNS(nsStyle, "num-format")
				f := map[string]string{"1": "decimal", "a": "lowerLetter", "A": "upperLetter", "i": "lowerRoman", "I": "upperRoman", "а": "russianLower", "А": "russianUpper"}[nf]
				if f == "" {
					f = "decimal"
				}
				suf := l.AttrNS(nsStyle, "num-suffix")
				text := ""
				if suf != "" && suf != "." {
					text = "%" + strconv.Itoa(lv) + suf
				}
				if nf == "" {
					levels[lv-1] = List{Kind: "ul", Text: " "}
				} else {
					levels[lv-1] = List{Kind: "ol", Fmt: f, Text: text}
				}
			}
		}
		r.lists[ls.AttrNS(nsStyle, "name")] = levels
	}
}

func (r *odtReader) chain(fam, name string) []*odStyle {
	var out []*odStyle
	seen := map[string]bool{}
	key := fam + ":" + name
	for !seen[key] {
		s, ok := r.styles[key]
		if !ok {
			break
		}
		seen[key] = true
		out = append([]*odStyle{s}, out...)
		if s.parent == "" {
			break
		}
		key = fam + ":" + s.parent
	}
	return out
}

func (r *odtReader) tp(n *Node, f F) F {
	if n == nil {
		return f
	}
	g := func(a string) string { return n.AttrNS(nsFo, a) }
	if w := g("font-weight"); w != "" {
		v, _ := strconv.Atoi(w)
		f.B = w == "bold" || v >= 600
	}
	if s := g("font-style"); s != "" {
		f.I = s == "italic" || s == "oblique"
	}
	if u := n.AttrNS(nsStyle, "text-underline-style"); u != "" {
		f.U = u != "none"
		f.DU = f.U && n.AttrNS(nsStyle, "text-underline-type") == "double"
		if f.DU {
			f.U = false
		}
	}
	if s := n.AttrNS(nsStyle, "text-line-through-style"); s != "" {
		f.S = s != "none"
	}
	if c := g("color"); c != "" {
		if strings.EqualFold(c, "#000000") {
			f.Color = ""
		} else {
			f.Color = strings.ToLower(c)
		}
	}
	if bg := g("background-color"); bg != "" {
		if bg == "transparent" {
			f.Bg = ""
		} else {
			f.Bg = strings.ToLower(bg)
		}
	}
	if sz := g("font-size"); sz != "" && !strings.HasSuffix(sz, "%") {
		if v, ok := lenPt(sz); ok {
			f.Size = v
		}
	}
	if fn := n.AttrNS(nsStyle, "font-name"); fn != "" {
		if v, ok := r.fonts[fn]; ok {
			f.Font = v
		} else {
			f.Font = fn
		}
	}
	if ff := g("font-family"); ff != "" {
		f.Font = strings.Trim(strings.Split(ff, ",")[0], `'" `)
	}
	if pos := n.AttrNS(nsStyle, "text-position"); pos != "" {
		f.Sup = strings.HasPrefix(pos, "super") || (pos[0] >= '1' && pos[0] <= '9')
		f.Sub = strings.HasPrefix(pos, "sub") || strings.HasPrefix(pos, "-")
	}
	if tt := g("text-transform"); tt == "uppercase" {
		f.Caps = true
	}
	return f
}

func (r *odtReader) pp(n *Node, pf PF) PF {
	if n == nil {
		return pf
	}
	g := func(a string) string { return n.AttrNS(nsFo, a) }
	if al := g("text-align"); al != "" {
		pf.Align = map[string]string{"center": "center", "end": "right", "right": "right", "justify": "justify"}[al]
	}
	setOpt := func(dst *Opt, a string) {
		if v := g(a); v != "" && !strings.HasSuffix(v, "%") {
			if x, ok := lenPt(v); ok {
				*dst = O(x)
			}
		}
	}
	for _, at := range n.Attr {
		if at.Name.Local == "contextual-spacing" {
			if at.Value == "true" {
				pf.CS = "x"
			} else {
				pf.CS = ""
			}
		}
	}
	setOpt(&pf.IndL, "margin-left")
	setOpt(&pf.IndF, "text-indent")
	setOpt(&pf.Before, "margin-top")
	setOpt(&pf.After, "margin-bottom")
	if v := g("margin-right"); v != "" {
		if x, ok := lenPt(v); ok {
			pf.IndR = x
		}
	}
	if lh := g("line-height"); lh != "" {
		if strings.HasSuffix(lh, "%") {
			v, _ := strconv.ParseFloat(strings.TrimSuffix(lh, "%"), 64)
			pf.Line, pf.LineExact = O(v/100), 0
		} else if lh != "normal" {
			if x, ok := lenPt(lh); ok {
				pf.LineExact, pf.LineRule = x, "exact"
			}
		}
	}
	if al := n.AttrNS(nsStyle, "line-height-at-least"); al != "" {
		if x, ok := lenPt(al); ok {
			pf.LineExact, pf.LineRule = x, "atLeast"
		}
	}
	switch g("break-before") {
	case "page":
		pf.PB = true
	case "column":
		pf.CB = true
	}
	if ts := n.First("tab-stops"); ts != nil {
		pf.Tabs = nil
		base := 0.0
		if pf.IndL.Set {
			base = pf.IndL.V
		}
		for _, t := range ts.KidsNamed("tab-stop") {
			pos, _ := lenPt(t.AttrNS(nsStyle, "position"))
			al := map[string]string{"right": "right", "center": "center", "char": "right"}[t.AttrNS(nsStyle, "type")]
			ld := ""
			if l := t.AttrNS(nsStyle, "leader-style"); l != "" && l != "none" {
				ld = "dot"
			} else if l := t.AttrNS(nsStyle, "leader-text"); l != "" && l != " " {
				ld = "dot"
			}
			pf.Tabs = append(pf.Tabs, TabStop{Pos: pos + base, Align: al, Leader: ld})
		}
	}
	if g("keep-with-next") == "always" {
		pf.KeepNext = true
	}
	return pf
}

func (r *odtReader) paraStyle(name string) (F, PF, int, string) {
	var f F
	var pf PF
	heading, named := 0, ""
	re := regexp.MustCompile(`(?i)^heading[ _]?(?:_20_)?(\d)$`)
	for _, s := range r.chain("paragraph", name) {
		f = r.tp(s.tp, f)
		pf = r.pp(s.pp, pf)
		d := strings.ReplaceAll(s.display, " ", "_")
		if m := re.FindStringSubmatch(d); m != nil {
			heading, _ = strconv.Atoi(m[1])
		}
		switch strings.ToLower(strings.ReplaceAll(s.display, "_20_", " ")) {
		case "title":
			named = "Title"
		case "subtitle":
			named = "Subtitle"
		case "quotations", "quote":
			named = "Quote"
		case "contents 1":
			named = "TOC1"
		case "contents 2":
			named = "TOC2"
		case "contents 3":
			named = "TOC3"
		case "contents heading":
			named = "TOCHeading"
		}
	}
	return f, pf, heading, named
}

func (r *odtReader) textF(name string, base F) F {
	f := base
	for _, s := range r.chain("text", name) {
		f = r.tp(s.tp, f)
	}
	return f
}

func (r *odtReader) trimF(f F) F {
	if f.Font == r.doc.Def.Font {
		f.Font = ""
	}
	if f.Size == r.doc.Def.Size {
		f.Size = 0
	}
	return f
}

func (r *odtReader) mediaID(href string) string {
	if id, ok := r.media[href]; ok {
		return id
	}
	data, ok := r.files[href]
	if !ok {
		return ""
	}
	id := NewID("m")
	r.doc.Media[id] = &Media{Mime: mimeByExt[strings.TrimPrefix(strings.ToLower(path.Ext(href)), ".")], Data: data}
	r.media[href] = id
	return id
}

var spaceRe = regexp.MustCompile(`[\r\n\t]+`)
var multiSpace = regexp.MustCompile(` {2,}`)

func (r *odtReader) inline(n *Node, p *Para, f F, link string) {
	for _, k := range n.Kids {
		if r.skipTo != "" && !(k.NS == nsText && k.Name == "reference-mark-end") {
			continue
		}
		if k.Name == "" {
			t := multiSpace.ReplaceAllString(spaceRe.ReplaceAllString(k.Text, " "), " ")
			if len(p.Runs) == 0 {
				t = strings.TrimLeft(t, " ") // ODF: пробелы в начале абзаца схлопываются; явные text:s сохраняются
			}
			if t != "" {
				ff := f
				if link != "" {
					ff.Link = link
				}
				ff = r.trimF(ff)
				ff.Cmt, ff.Ins = r.cmt, r.ins
				p.Runs = append(p.Runs, Run{T: RText, Text: []rune(t), F: ff})
				if r.cmtNoEnd {
					r.cmt, r.cmtNoEnd = "", false
				}
			}
			continue
		}
		if k.NS == nsText {
			switch k.Name {
			case "span":
				r.inline(k, p, r.textF(k.AttrNS(nsText, "style-name"), f), link)
			case "s":
				c, _ := strconv.Atoi(k.AttrNS(nsText, "c"))
				p.Runs = append(p.Runs, Run{T: RText, Text: []rune(strings.Repeat(" ", max(1, c))), F: r.trimF(f)})
			case "tab":
				p.Runs = append(p.Runs, Run{T: RText, Text: []rune{'\t'}, F: r.trimF(f)})
			case "line-break":
				p.Runs = append(p.Runs, Run{T: RBreak})
			case "a":
				r.inline(k, p, f, k.AttrNS(nsXlink, "href"))
			case "page-number":
				p.Runs = append(p.Runs, Run{T: RField, Name: "PAGE", F: r.trimF(f)})
			case "page-count":
				p.Runs = append(p.Runs, Run{T: RField, Name: "NUMPAGES", F: r.trimF(f)})
			case "sequence":
				if nm := k.AttrNS(nsText, "name"); nm != "" {
					if rn := k.AttrNS(nsText, "ref-name"); rn != "" && p.PF.Bookmark == "" {
						p.PF.Bookmark = rn
					}
					p.Runs = append(p.Runs, Run{T: RField, Name: seqName(nm, strings.Contains(k.TextContent(), "."), ""), F: r.trimF(f)})
				}
			case "sequence-ref":
				mode := "l"
				if k.AttrNS(nsText, "reference-format") == "value" {
					mode = "n"
				}
				if rn := k.AttrNS(nsText, "ref-name"); rn != "" {
					ff := r.trimF(f)
					ff.Link = "#" + rn
					p.Runs = append(p.Runs, Run{T: RField, Name: "REF " + rn + " " + mode, F: ff})
				}
			case "bookmark", "bookmark-start":
				if n := k.AttrNS(nsText, "name"); n != "" && p.PF.Bookmark == "" && !strings.HasPrefix(n, "__") {
					p.PF.Bookmark = n
				}
			case "bookmark-ref":
				if k.AttrNS(nsText, "reference-format") == "page" {
					ff := r.trimF(f)
					ff.Link = link
					p.Runs = append(p.Runs, Run{T: RField, Name: "PAGEREF " + k.AttrNS(nsText, "ref-name"), F: ff})
				} else {
					r.inline(k, p, f, link)
				}
			case "bibliography-mark":
				if id := k.AttrNS(nsText, "identifier"); id != "" && r.doc.SourceByID(strings.Fields(id + " x")[0]) != nil {
					p.Runs = append(p.Runs, Run{T: RField, Name: "CITE " + id, F: r.trimF(f)})
				} else {
					r.inline(k, p, f, link)
				}
			case "reference-mark-start":
				if nm := k.AttrNS(nsText, "name"); strings.HasPrefix(nm, "TexoraCite:") {
					id := strings.TrimPrefix(nm, "TexoraCite:")
					if i := strings.LastIndex(id, "|"); i >= 0 {
						id = id[:i]
					}
					p.Runs = append(p.Runs, Run{T: RField, Name: "CITE " + id, F: r.trimF(f)})
					r.skipTo = nm
				}
			case "reference-mark-end":
				if k.AttrNS(nsText, "name") == r.skipTo {
					r.skipTo = ""
				}
			case "change-start":
				r.ins = r.changeRev(k.AttrNS(nsText, "change-id"))
			case "change-end":
				r.ins = ""
			case "change":
				id := k.AttrNS(nsText, "change-id")
				if ch := r.changes[id]; ch != nil && ch.deleted != nil {
					before := len(p.Runs)
					for _, dp := range ch.deleted.Elems() {
						if dp.Name == "p" || dp.Name == "h" {
							r.inline(dp, p, f, link)
						}
					}
					rid := r.changeRev(id)
					for i := before; i < len(p.Runs); i++ {
						p.Runs[i].F.Del = rid
					}
				}
			case "note":
				id := NewID("f")
				var bs []Block
				if body := k.First("note-body"); body != nil {
					bs = r.blocks(body)
				}
				if len(bs) == 0 {
					bs = []Block{{P: NewPara()}}
				}
				r.doc.Footnotes[id] = &Note{Blocks: bs}
				p.Runs = append(p.Runs, Run{T: RNote, ID: id})
			case "bookmark-end", "soft-page-break", "alphabetical-index-mark":
			default:
				r.inline(k, p, f, link)
			}
		} else if k.NS == nsOffice && k.Name == "annotation" {
			name := k.AttrNS(nsOffice, "name")
			id := "c" + strings.TrimPrefix(name, "__Annotation__")
			if name == "" {
				id = NewID("c")
			}
			var lines []string
			for _, tp := range k.KidsNamed("p") {
				lines = append(lines, tp.TextContent())
			}
			c := &Comment{Text: strings.Join(lines, "\n")}
			if cr := k.Kid("creator"); cr != nil {
				c.Author = cr.TextContent()
			}
			if dt := k.Kid("date"); dt != nil {
				c.Date = dt.TextContent()
				if !strings.HasSuffix(c.Date, "Z") && len(c.Date) >= 19 {
					c.Date = c.Date[:19] + "Z"
				}
			}
			for _, at := range k.Attr {
				if at.Name.Local == "resolved" && at.Value == "true" {
					c.Done = true
				}
			}
			r.doc.Comments[id] = c
			r.cmt, r.cmtNoEnd = id, name == ""
		} else if k.NS == nsOffice && k.Name == "annotation-end" {
			r.cmt = ""
		} else if k.NS == nsDraw && k.Name == "frame" {
			if obj := k.First("object"); obj != nil {
				dir := strings.TrimSuffix(strings.TrimPrefix(obj.AttrNS(nsXlink, "href"), "./"), "/")
				src := ""
				if mx, ok := r.files[dir+"/content.xml"]; ok {
					if mr, err := ParseXML(mx); err == nil {
						for _, an := range mr.Find("annotation") {
							if an.Av("encoding") == "application/x-texora" {
								src = an.TextContent()
							}
						}
						if src == "" {
							if m := mr.First("math"); m != nil {
								src = strings.TrimSpace(MathMLToLinear(m))
							}
						}
					}
				}
				if src != "" {
					ff := r.trimF(f)
					ff.Cmt, ff.Ins = r.cmt, r.ins
					p.Runs = append(p.Runs, Run{T: REq, Text: []rune(src), F: ff})
				}
				continue
			}
			img := k.First("image")
			if img == nil {
				continue
			}
			id := r.mediaID(img.AttrNS(nsXlink, "href"))
			if id == "" {
				continue
			}
			w, ok1 := lenPt(k.AttrNS(nsSvg, "width"))
			h, ok2 := lenPt(k.AttrNS(nsSvg, "height"))
			if !ok1 || !ok2 {
				w, h = 100, 100
			}
			run := Run{T: RImage, ID: id, W: w, H: h}
			if at := k.AttrNS(nsText, "anchor-type"); at == "paragraph" || at == "char" {
				run.Wrap, run.Side = "square", "left"
				for _, st := range r.chain("graphic", k.AttrNS(nsDraw, "style-name")) {
					if st.gp == nil {
						continue
					}
					switch st.gp.AttrNS(nsStyle, "wrap") {
					case "none":
						run.Wrap = "topbottom"
					case "run-through":
						run.Wrap = "front"
						if st.gp.AttrNS(nsStyle, "run-through") == "background" {
							run.Wrap = "behind"
						}
					case "left":
						run.Wrap, run.Side = "square", "right"
					case "right", "parallel", "dynamic", "biggest":
						run.Wrap = "square"
					}
					if hp := st.gp.AttrNS(nsStyle, "horizontal-pos"); hp == "right" || hp == "center" {
						run.Side = hp
					} else if hp == "left" {
						run.Side = "left"
					}
				}
				if run.Wrap == "square" && run.Side == "center" {
					run.Side = "left"
				}
			}
			p.Runs = append(p.Runs, run)
		}
	}
}

func (r *odtReader) para(n *Node, list *List) *Para {
	f, pf, heading, named := r.paraStyle(n.AttrNS(nsText, "style-name"))
	if n.Name == "h" {
		lv, _ := strconv.Atoi(n.AttrNS(nsText, "outline-level"))
		if lv == 0 {
			lv = max(heading, 1)
		}
		heading = lv
	}
	pf.Heading = min(heading, 6)
	if heading == 0 {
		pf.Style = named
	}
	if list != nil {
		l := *list
		pf.List = &l
		// отступы списка задаёт стиль списка; свои отступы абзаца действуют, только если записаны
		// прямо в его стиле (а не унаследованы, например, красная строка от «Обычного»)
		own := false
		if st := r.styles["paragraph:"+n.AttrNS(nsText, "style-name")]; st != nil && st.pp != nil {
			own = st.pp.AttrNS(nsFo, "margin-left") != "" && st.pp.AttrNS(nsFo, "text-indent") != ""
		}
		if !own {
			pf.IndL, pf.IndF = Opt{}, Opt{}
		}
	}
	d := r.doc.Def
	if pf.After.Set && pf.After.V == d.After && heading == 0 {
		pf.After = Opt{}
	}
	if pf.Before.Set && pf.Before.V == 0 && heading == 0 {
		pf.Before = Opt{}
	}
	if pf.Line.Set && math.Abs(pf.Line.V-d.Line) < 0.001 {
		pf.Line = Opt{}
	}
	p := &Para{PF: pf}
	if r.inBody {
		for _, st := range r.chain("paragraph", n.AttrNS(nsText, "style-name")) {
			if st.master != "" {
				r.secStart[p] = [2]string{st.master, st.pageNum}
			}
		}
	}
	r.inline(n, p, f, "")
	p.Normalize()
	if len(p.Runs) == 0 {
		p.Mark = r.trimF(f)
	}
	return p
}

func (r *odtReader) list(n *Node, level int, style, id string, out *[]Block) {
	sn := n.AttrNS(nsText, "style-name")
	if sn == "" {
		sn = style
	}
	lv := List{Kind: "ul"}
	if ls, ok := r.lists[sn]; ok && level < len(ls) {
		lv = ls[level]
	}
	for _, item := range n.Elems() {
		if item.Name != "list-item" && item.Name != "list-header" {
			continue
		}
		for _, c := range item.Elems() {
			switch c.Name {
			case "p", "h":
				l := lv
				l.ID, l.Level = id, level
				if l.Kind == "ul" && (l.Text == "•" || l.Text == " ") {
					l.Text = ""
				}
				var lp *List
				if item.Name == "list-item" {
					lp = &l
				}
				*out = append(*out, Block{P: r.para(c, lp)})
			case "list":
				r.list(c, level+1, sn, id, out)
			case "table":
				*out = append(*out, Block{T: r.table(c)})
			}
		}
	}
}

func (r *odtReader) table(n *Node) *Table {
	t := &Table{}
	var cols []float64
	addCols := func(c *Node) {
		w := 0.0
		if st, ok := r.styles["table-column:"+c.AttrNS(nsTable, "style-name")]; ok && st.tcp != nil {
			w, _ = lenPt(st.tcp.AttrNS(nsStyle, "column-width"))
		}
		rep, _ := strconv.Atoi(c.AttrNS(nsTable, "number-columns-repeated"))
		for k := 0; k < max(1, rep); k++ {
			cols = append(cols, w)
		}
	}
	var rows []*Node
	hdrRows := map[*Node]bool{}
	for _, c := range n.Elems() {
		switch c.Name {
		case "table-column":
			addCols(c)
		case "table-columns", "table-header-columns":
			for _, cc := range c.KidsNamed("table-column") {
				addCols(cc)
			}
		case "table-row":
			rows = append(rows, c)
		case "table-rows":
			rows = append(rows, c.KidsNamed("table-row")...)
		case "table-header-rows":
			for _, hr := range c.KidsNamed("table-row") {
				rows = append(rows, hr)
				hdrRows[hr] = true
			}
		}
	}
	ok := len(cols) > 0
	for _, c := range cols {
		if c <= 0 {
			ok = false
		}
	}
	if ok {
		t.Cols = cols
	}
	// odBorder — "0.50pt solid #000000" → граница; "none" → явное «нет линии»
	odBorder := func(v string) *Border {
		f := strings.Fields(v)
		if len(f) == 0 {
			return nil
		}
		if f[0] == "none" || f[0] == "hidden" {
			return &Border{}
		}
		w, ok := lenPt(f[0])
		if !ok {
			return nil
		}
		b := &Border{W: w}
		for _, x := range f[1:] {
			switch {
			case x == "double":
				b.Style = "double"
			case strings.HasPrefix(x, "#") && !strings.EqualFold(x, "#000000"):
				b.Color = x
			}
		}
		return b
	}
	for _, tr := range rows {
		row := &Row{Header: hdrRows[tr]}
		if st, ok := r.styles["table-row:"+tr.AttrNS(nsTable, "style-name")]; ok && st.trp != nil {
			if h, ok := lenPt(st.trp.AttrNS(nsStyle, "row-height")); ok && h > 0 {
				row.H, row.HRule = h, "exact"
			} else if h, ok := lenPt(st.trp.AttrNS(nsStyle, "min-row-height")); ok && h > 0 {
				row.H = h
			}
		}
		for _, tc := range tr.KidsNamed("table-cell") {
			cell := &Cell{}
			if st, ok := r.styles["table-cell:"+tc.AttrNS(nsTable, "style-name")]; ok && st.cp != nil {
				g := func(a string) string { return st.cp.AttrNS(nsFo, a) }
				b := g("border") + g("border-top") + g("border-left")
				if strings.Contains(b, "solid") {
					t.Border = true
				}
				for i, side := range []string{"top", "left", "bottom", "right"} {
					v := g("border-" + side)
					if v == "" {
						v = g("border")
					}
					cell.B[i] = odBorder(v)
				}
				// поля ячейки: общее fo:padding и по сторонам
				pad := [4]float64{CellPadV, CellPad, CellPadV, CellPad}
				anyPad := false
				if v, ok := lenPt(g("padding")); ok {
					pad, anyPad = [4]float64{v, v, v, v}, true
				}
				for i, side := range []string{"top", "left", "bottom", "right"} {
					if v, ok := lenPt(g("padding-" + side)); ok {
						pad[i], anyPad = v, true
					}
				}
				if anyPad {
					cp := pad
					cell.Pad = &cp
				}
				if bg := g("background-color"); strings.HasPrefix(bg, "#") {
					cell.Shade = bg
				}
				switch st.cp.AttrNS(nsStyle, "vertical-align") {
				case "middle":
					cell.VAlign = "center"
				case "bottom":
					cell.VAlign = "bottom"
				}
			}
			span, _ := strconv.Atoi(tc.AttrNS(nsTable, "number-columns-spanned"))
			bs := r.blocks(tc)
			if len(bs) == 0 {
				bs = []Block{{P: NewPara()}}
			}
			cell.Blocks, cell.Span = bs, max(1, span)
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

func (r *odtReader) blocks(n *Node) []Block {
	var out []Block
	for _, el := range n.Elems() {
		switch el.Name {
		case "p", "h":
			out = append(out, Block{P: r.para(el, nil)})
		case "list":
			r.list(el, 0, "", NewID("l"), &out)
		case "table":
			out = append(out, Block{T: r.table(el)})
		case "table-of-content":
			if ib := el.Kid("index-body"); ib != nil {
				for _, b := range r.blocks(ib) {
					if b.P != nil && r.inBody {
						b.P.PF.Region = "toc"
					}
					out = append(out, b)
				}
			}
		case "section":
			bs := r.blocks(el)
			if nm := el.AttrNS(nsText, "name"); (nm == "TexoraBib" || strings.HasPrefix(nm, "TexoraLOF:")) && r.inBody {
				reg := "bib"
				if nm != "TexoraBib" {
					reg = "lof:" + strings.TrimPrefix(nm, "TexoraLOF:")
				}
				for _, b := range bs {
					if b.P != nil {
						b.P.PF.Region = reg
					}
				}
			}
			out = append(out, bs...)
		case "index-body", "alphabetical-index", "illustration-index", "user-index", "bibliography":
			out = append(out, r.blocks(el)...)
		}
	}
	return out
}

func ReadODT(data []byte) (*Doc, error) {
	files, err := unzipAll(data)
	if err != nil {
		return nil, err
	}
	content, ok := files["content.xml"]
	if !ok {
		return nil, fmt.Errorf("в архиве нет content.xml — это не документ OpenDocument")
	}
	r := &odtReader{files: files, doc: NewDoc(), styles: map[string]*odStyle{}, lists: map[string][]List{}, fonts: map[string]string{}, media: map[string]string{},
		masters: map[string]*odMaster{}, secStart: map[*Para][2]string{}, changes: map[string]*odChange{}}
	if mx, ok := files["meta.xml"]; ok {
		if mr, err := ParseXML(mx); err == nil {
			for _, ud := range mr.Find("user-defined") {
				if ud.AttrNS("urn:oasis:names:tc:opendocument:xmlns:meta:1.0", "name") == "TexoraSources" {
					var src []*Source
					if json.Unmarshal([]byte(ud.TextContent()), &src) == nil {
						r.doc.Sources = src
					}
				}
			}
		}
	}
	var styles *Node
	if s, ok := files["styles.xml"]; ok {
		styles, _ = ParseXML(s)
		r.collect(styles)
	}
	croot, _ := ParseXML(content)
	r.collect(croot)
	f, pf, _, _ := r.paraStyle("Standard")
	d := &r.doc.Def
	d.Font, d.Size = "Times New Roman", 12
	if f.Font != "" {
		d.Font = f.Font
	}
	if f.Size > 0 {
		d.Size = f.Size
	}
	d.After, d.Line = 0, 1
	if pf.After.Set {
		d.After = pf.After.V
	}
	if pf.Line.Set {
		d.Line = pf.Line.V
	}
	text := croot.First("text")
	r.doc.Body = nil
	if text != nil {
		if tc := text.Kid("tracked-changes"); tc != nil {
			r.doc.Track = tc.AttrNS(nsText, "track-changes") == "true"
			for _, cr := range tc.KidsNamed("changed-region") {
				id := cr.AttrNS(nsText, "id")
				for _, ch := range cr.Elems() {
					c := &odChange{kind: ch.Name}
					if ci := ch.Kid("change-info"); ci != nil {
						if a := ci.Kid("creator"); a != nil {
							c.author = a.TextContent()
						}
						if d := ci.Kid("date"); d != nil {
							c.date = d.TextContent()
						}
					}
					if ch.Name == "deletion" {
						c.deleted = ch
					}
					r.changes[id] = c
				}
			}
		}
		if styles != nil {
			r.readMasters(styles)
		}
		r.inBody = true
		r.doc.Body = r.blocks(text)
		r.inBody = false
	}
	if len(r.doc.Body) == 0 || r.doc.Body[len(r.doc.Body)-1].P == nil {
		r.doc.Body = append(r.doc.Body, Block{P: NewPara()})
	}
	if len(r.masters) > 0 {
		r.applySections()
	} else if styles != nil {
		mp := styles.First("master-page")
		if mp != nil {
			pl := mp.AttrNS(nsStyle, "page-layout-name")
			for _, l := range styles.Find("page-layout") {
				if l.AttrNS(nsStyle, "name") != pl {
					continue
				}
				pp := l.First("page-layout-properties")
				pg := &r.doc.Page
				g := func(a string, dst *float64) {
					if v, ok := lenPt(pp.AttrNS(nsFo, a)); ok {
						*dst = v
					}
				}
				if pp != nil {
					g("page-width", &pg.W)
					g("page-height", &pg.H)
					g("margin-top", &pg.Top)
					g("margin-bottom", &pg.Bottom)
					g("margin-left", &pg.Left)
					g("margin-right", &pg.Right)
				}
				if hs := l.First("header-style"); hs != nil {
					if hp := hs.First("header-footer-properties"); hp != nil {
						mh, _ := lenPt(hp.AttrNS(nsFo, "min-height"))
						mb, _ := lenPt(hp.AttrNS(nsFo, "margin-bottom"))
						pg.Header = pg.Top
						pg.Top += mh + mb
					}
				}
				if fs := l.First("footer-style"); fs != nil {
					if fp := fs.First("header-footer-properties"); fp != nil {
						mh, _ := lenPt(fp.AttrNS(nsFo, "min-height"))
						mt, _ := lenPt(fp.AttrNS(nsFo, "margin-top"))
						pg.Footer = pg.Bottom
						pg.Bottom += mh + mt
					}
				}
			}
			for _, key := range []string{"header", "footer"} {
				if el := mp.Kid(key); el != nil && el.AttrNS(nsStyle, "display") != "false" {
					bs := r.blocks(el)
					empty := true
					for _, b := range bs {
						if b.T != nil || len(b.P.Runs) > 0 {
							empty = false
						}
					}
					if !empty {
						if key == "header" {
							r.doc.Header = bs
						} else {
							r.doc.Footer = bs
						}
					}
				}
			}
			if mp.Kid("header-first") != nil || mp.Kid("footer-first") != nil {
				r.doc.TitlePg = true
			}
		}
	}
	fixSeqPrefixes(r.doc)
	return r.doc, nil
}

func (r *odtReader) changeRev(id string) string {
	c := r.changes[id]
	if c == nil {
		return ""
	}
	rid := "r" + id
	if _, ok := r.doc.Revs[rid]; !ok {
		date := c.date
		if len(date) >= 19 && !strings.HasSuffix(date, "Z") {
			date = date[:19] + "Z"
		}
		r.doc.Revs[rid] = &Rev{Author: c.author, Date: date}
	}
	return rid
}

// readMasters — мастер-страницы: размер, поля, колонтитулы (обычные, первой и чётных страниц).
func (r *odtReader) readMasters(styles *Node) {
	layouts := map[string]*Node{}
	for _, l := range styles.Find("page-layout") {
		layouts[l.AttrNS(nsStyle, "name")] = l
	}
	for _, mp := range styles.Find("master-page") {
		m := &odMaster{page: DefaultPage}
		pg := &m.page
		if l := layouts[mp.AttrNS(nsStyle, "page-layout-name")]; l != nil {
			if pp := l.First("page-layout-properties"); pp != nil {
				g := func(a string, dst *float64) {
					if v, ok := lenPt(pp.AttrNS(nsFo, a)); ok {
						*dst = v
					}
				}
				g("page-width", &pg.W)
				g("page-height", &pg.H)
				g("margin-top", &pg.Top)
				g("margin-bottom", &pg.Bottom)
				if c := pp.First("columns"); c != nil {
					if n, _ := strconv.Atoi(c.AttrNS(nsFo, "column-count")); n > 1 {
						m.cols.N = min(n, 12)
						if v, ok := lenPt(c.AttrNS(nsFo, "column-gap")); ok {
							m.cols.Gap = v
						}
						if sp := c.First("column-sep"); sp != nil && sp.AttrNS(nsStyle, "style") != "none" {
							m.cols.Sep = true
						}
					}
				}
				g("margin-left", &pg.Left)
				g("margin-right", &pg.Right)
			}
			if hs := l.First("header-style"); hs != nil {
				if hp := hs.First("header-footer-properties"); hp != nil {
					mh, _ := lenPt(hp.AttrNS(nsFo, "min-height"))
					mb, _ := lenPt(hp.AttrNS(nsFo, "margin-bottom"))
					pg.Header = pg.Top
					pg.Top += mh + mb
				}
			}
			if fs := l.First("footer-style"); fs != nil {
				if fp := fs.First("header-footer-properties"); fp != nil {
					mh, _ := lenPt(fp.AttrNS(nsFo, "min-height"))
					mt, _ := lenPt(fp.AttrNS(nsFo, "margin-top"))
					pg.Footer = pg.Bottom
					pg.Bottom += mh + mt
				}
			}
		}
		for _, kind := range []string{"header", "footer"} {
			for _, v := range []string{"", "-left", "-first"} {
				el := mp.Kid(kind + v)
				if el == nil {
					continue
				}
				key := kind
				switch v {
				case "-left":
					key += ".even"
					m.even = true
				case "-first":
					key += ".first"
					m.titlePg = true
				}
				var bs []Block
				if el.AttrNS(nsStyle, "display") != "false" {
					bs = r.blocks(el)
				}
				empty := true
				for _, b := range bs {
					if b.T != nil || len(b.P.Runs) > 0 {
						empty = false
					}
				}
				if empty && v != "" {
					bs, empty = []Block{{P: NewPara()}}, false
				}
				if !empty {
					*m.hf.Get(key) = bs
				}
			}
		}
		r.masters[mp.AttrNS(nsStyle, "name")] = m
	}
}

// applySections превращает смены мастер-страниц в разделы модели.
func (r *odtReader) applySections() {
	d := r.doc
	type mark struct {
		idx    int
		master string
		num    int
	}
	var marks []mark
	for i, b := range d.Body {
		if b.P == nil {
			continue
		}
		if sm, ok := r.secStart[b.P]; ok {
			n, _ := strconv.Atoi(sm[1])
			marks = append(marks, mark{i, sm[0], n})
		}
	}
	first := "Standard"
	if len(marks) > 0 && marks[0].idx == 0 {
		first = marks[0].master
	} else {
		marks = append([]mark{{0, "Standard", 0}}, marks...)
	}
	_ = first
	set := func(m mark) (*odMaster, int) {
		if om := r.masters[m.master]; om != nil {
			return om, m.num
		}
		return r.masters["Standard"], m.num
	}
	for k := 0; k < len(marks); k++ {
		om, num := set(marks[k])
		if om == nil {
			continue
		}
		if k == len(marks)-1 {
			d.Page, d.Cols, d.TitlePg, d.Start, d.HF = om.page, om.cols, om.titlePg, num, om.hf.Clone()
			continue
		}
		end := marks[k+1].idx - 1
		if end < 0 || d.Body[end].P == nil {
			continue
		}
		d.Body[end].P.PF.Sect = &Section{ID: NewID("s"), Page: om.page, Cols: om.cols, TitlePg: om.titlePg, Start: num, HF: om.hf.Clone()}
	}
	for _, om := range r.masters {
		if om.even {
			d.EvenOdd = true
		}
	}
	// одинаковые колонтитулы подряд — «как в предыдущем»
	ss := d.Sections()
	for i := len(ss) - 1; i > 0; i-- {
		for _, k := range HFKinds {
			if BlocksToText(*ss[i].HF.Get(k)) == BlocksToText(*ss[i-1].HF.Get(k)) && len(*ss[i].HF.Get(k)) > 0 {
				same := true
				a, b := *ss[i].HF.Get(k), *ss[i-1].HF.Get(k)
				if len(a) != len(b) {
					same = false
				}
				if same {
					*ss[i].HF.Get(k) = nil
				}
			}
		}
	}
}

// ======================================================================== запись
type odReg struct {
	prefix string
	text   map[string]string
	para   map[string]string
	fonts  map[string]bool
	xml    strings.Builder
}

func newOdReg(prefix string) *odReg {
	return &odReg{prefix: prefix, text: map[string]string{}, para: map[string]string{}, fonts: map[string]bool{}}
}

func ptS(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) + "pt" }

func (g *odReg) textProps(f F) string {
	var ps []string
	if f.B {
		ps = append(ps, `fo:font-weight="bold" style:font-weight-asian="bold" style:font-weight-complex="bold"`)
	}
	if f.I {
		ps = append(ps, `fo:font-style="italic" style:font-style-asian="italic" style:font-style-complex="italic"`)
	}
	if f.DU {
		ps = append(ps, `style:text-underline-style="solid" style:text-underline-type="double" style:text-underline-width="auto" style:text-underline-color="font-color"`)
	} else if f.U || f.Link != "" {
		ps = append(ps, `style:text-underline-style="solid" style:text-underline-width="auto" style:text-underline-color="font-color"`)
	}
	if f.S {
		ps = append(ps, `style:text-line-through-style="solid"`)
	}
	if f.Caps {
		ps = append(ps, `fo:text-transform="uppercase"`)
	}
	c := f.Color
	if c == "" && f.Link != "" {
		c = "#0563c1"
	}
	if c != "" {
		ps = append(ps, `fo:color="`+c+`"`)
	}
	if f.Bg != "" {
		ps = append(ps, `fo:background-color="`+f.Bg+`"`)
	}
	if f.Size > 0 {
		s := ptS(f.Size)
		ps = append(ps, `fo:font-size="`+s+`" style:font-size-asian="`+s+`" style:font-size-complex="`+s+`"`)
	}
	if f.Font != "" {
		g.fonts[f.Font] = true
		ps = append(ps, `style:font-name="`+xesc(f.Font)+`"`)
	}
	if f.Sup {
		ps = append(ps, `style:text-position="super 58%"`)
	}
	if f.Sub {
		ps = append(ps, `style:text-position="sub 58%"`)
	}
	return strings.Join(ps, " ")
}

func (g *odReg) t(f F) string {
	key := g.textProps(f)
	if key == "" {
		return ""
	}
	if n, ok := g.text[key]; ok {
		return n
	}
	n := fmt.Sprintf("%sT%d", g.prefix, len(g.text)+1)
	g.text[key] = n
	fmt.Fprintf(&g.xml, `<style:style style:name="%s" style:family="text"><style:text-properties %s/></style:style>`, n, key)
	return n
}

func (g *odReg) p(pf PF, mark F) string { return g.pEx(pf, mark, "", 0, 0) }

var odStyleNames = map[string]string{"Title": "Title", "Subtitle": "Subtitle", "Quote": "Quotations", "NoSpacing": "No_20_Spacing",
	"TOC1": "Contents_20_1", "TOC2": "Contents_20_2", "TOC3": "Contents_20_3", "TOCHeading": "Contents_20_Heading"}

// pEx — стиль абзаца; master — смена мастер-страницы (начало раздела), indL — отступ для пересчёта табуляций.
func (g *odReg) pEx(pf PF, mark F, master string, pageNum int, indL float64) string {
	parent := "Standard"
	if pf.Heading > 0 {
		parent = "Heading_20_" + strconv.Itoa(pf.Heading)
	} else if pf.Style != "" {
		parent = odStyleNames[pf.Style]
		if parent == "" {
			parent = "Standard"
		}
	} else if pf.List != nil {
		parent = "List_20_Paragraph"
	}
	var ps []string
	if pf.Align != "" && pf.Align != "left" {
		ps = append(ps, `fo:text-align="`+map[string]string{"center": "center", "right": "end", "justify": "justify"}[pf.Align]+`"`)
	}
	if pf.IndL.Set {
		ps = append(ps, `fo:margin-left="`+ptS(pf.IndL.V)+`"`)
	}
	if pf.IndR != 0 {
		ps = append(ps, `fo:margin-right="`+ptS(pf.IndR)+`"`)
	}
	if pf.IndF.Set {
		ps = append(ps, `fo:text-indent="`+ptS(pf.IndF.V)+`"`)
	}
	if pf.Before.Set {
		ps = append(ps, `fo:margin-top="`+ptS(pf.Before.V)+`"`)
	}
	if pf.After.Set {
		ps = append(ps, `fo:margin-bottom="`+ptS(pf.After.V)+`"`)
	}
	if pf.LineExact > 0 {
		if pf.LineRule == "atLeast" {
			ps = append(ps, `style:line-height-at-least="`+ptS(pf.LineExact)+`"`)
		} else {
			ps = append(ps, `fo:line-height="`+ptS(pf.LineExact)+`"`)
		}
	} else if pf.Line.Set {
		ps = append(ps, fmt.Sprintf(`fo:line-height="%d%%"`, int(math.Round(pf.Line.V*100))))
	}
	if pf.PB {
		ps = append(ps, `fo:break-before="page"`)
	} else if pf.CB {
		ps = append(ps, `fo:break-before="column"`)
	}
	if pf.KeepNext {
		ps = append(ps, `fo:keep-with-next="always"`)
	}
	if pf.CS != "" {
		ps = append(ps, `loext:contextual-spacing="true"`)
	}
	if pf.Shade != "" {
		ps = append(ps, `fo:background-color="`+pf.Shade+`"`)
	}
	if pageNum > 0 {
		ps = append(ps, fmt.Sprintf(`style:page-number="%d"`, pageNum))
	}
	tabs := ""
	if len(pf.Tabs) > 0 {
		var tb strings.Builder
		tb.WriteString("<style:tab-stops>")
		for _, t := range pf.Tabs {
			typ := ""
			if t.Align == "right" || t.Align == "center" {
				typ = ` style:type="` + t.Align + `"`
			}
			ld := ""
			if t.Leader == "dot" {
				ld = ` style:leader-style="dotted" style:leader-text="."`
			}
			fmt.Fprintf(&tb, `<style:tab-stop style:position="%s"%s%s/>`, ptS(math.Max(0, t.Pos-indL)), typ, ld)
		}
		tb.WriteString("</style:tab-stops>")
		tabs = tb.String()
	}
	tprops := ""
	if mark != (F{}) {
		tprops = g.textProps(mark)
	}
	key := parent + "|" + strings.Join(ps, " ") + "|" + tprops + "|" + master + "|" + tabs
	if n, ok := g.para[key]; ok {
		return n
	}
	n := fmt.Sprintf("%sP%d", g.prefix, len(g.para)+1)
	g.para[key] = n
	mp := ""
	if master != "" {
		mp = ` style:master-page-name="` + master + `"`
	}
	fmt.Fprintf(&g.xml, `<style:style style:name="%s" style:family="paragraph" style:parent-style-name="%s"%s>`, n, parent, mp)
	if len(ps) > 0 || tabs != "" {
		g.xml.WriteString(`<style:paragraph-properties ` + strings.Join(ps, " ") + `>` + tabs + `</style:paragraph-properties>`)
	}
	if tprops != "" {
		g.xml.WriteString(`<style:text-properties ` + tprops + `/>`)
	}
	g.xml.WriteString(`</style:style>`)
	return n
}

type odtWriter struct {
	doc        *Doc
	pics       map[string]string
	files      []wfile
	mimes      map[string]string
	fnNum      map[string]int
	tables     int
	frames     int
	cell       int
	bullets    map[string]string // свой знак маркера («–» в отчётах по ГОСТ) → имя стиля списка
	master     string            // мастер-страница для следующего абзаца
	masterNum  int
	width      float64
	cmtLast    map[string]cmtKey
	cmtStarted map[string]bool
	changes    strings.Builder
	chN        int
	objs       int
	gstyles    map[string]bool
}

// eqMeasure — измеритель для размеров формул при сохранении (задаётся приложением).
var eqMeasure *Measurer

func eqSizePt(src string, size float64) (float64, float64) {
	if eqMeasure != nil {
		b := eqMeasure.EqLayout(src, F{Size: size})
		return b.W + 2, b.Asc + b.Desc + 2
	}
	return float64(len([]rune(src))) * size * 0.45, size * 1.6
}

func odDate(iso string) string { return strings.TrimSuffix(iso, "Z") }

func (w *odtWriter) change(kind, revID, deleted string) string {
	w.chN++
	id := fmt.Sprintf("ct%d", w.chN)
	author, date := "Texora", nowISO()
	if rv := w.doc.Revs[revID]; rv != nil {
		author, date = rv.Author, rv.Date
	}
	fmt.Fprintf(&w.changes, `<text:changed-region text:id="%s" xml:id="%s"><text:%s><office:change-info><dc:creator>%s</dc:creator><dc:date>%s</dc:date></office:change-info>%s</text:%s></text:changed-region>`,
		id, id, kind, xesc(author), xesc(odDate(date)), deleted, kind)
	return id
}

func (w *odtWriter) graphicStyle(g *odReg, wrap, side string) string {
	name := g.prefix + "fr_" + wrap + "_" + side
	if wrap == "eq" {
		name = g.prefix + "fr_eq"
	}
	if w.gstyles[name] {
		return name
	}
	w.gstyles[name] = true
	if wrap == "eq" {
		fmt.Fprintf(&g.xml, `<style:style style:name="%s" style:family="graphic" style:parent-style-name="Formula"><style:graphic-properties style:vertical-pos="middle" style:vertical-rel="text" draw:ole-draw-aspect="1"/></style:style>`, name)
		return name
	}
	hp := side
	if hp == "" {
		hp = "left"
	}
	wr := "right"
	switch {
	case wrap == "topbottom":
		wr = "none"
	case wrap == "behind" || wrap == "front":
		wr = "run-through"
	case side == "right":
		wr = "left"
	}
	extra := ""
	if wrap == "behind" {
		extra = ` style:run-through="background"`
	} else if wrap == "front" {
		extra = ` style:run-through="foreground"`
	}
	g2 := ptS(floatGap)
	fmt.Fprintf(&g.xml, `<style:style style:name="%s" style:family="graphic"><style:graphic-properties style:wrap="%s"%s style:number-wrapped-paragraphs="no-limit" fo:margin-left="%s" fo:margin-right="%s" fo:margin-top="0pt" fo:margin-bottom="%s" style:vertical-pos="top" style:vertical-rel="paragraph" style:horizontal-pos="%s" style:horizontal-rel="paragraph"/></style:style>`,
		name, wr, extra, g2, g2, ptS(floatGap/2), hp)
	return name
}

func odText(s string) string {
	var sb strings.Builder
	spaces := 0
	flush := func() {
		if spaces == 0 {
			return
		}
		if spaces == 1 && sb.Len() > 0 && !strings.HasSuffix(sb.String(), ">") {
			sb.WriteString(" ")
		} else {
			fmt.Fprintf(&sb, `<text:s text:c="%d"/>`, spaces)
		}
		spaces = 0
	}
	for _, ch := range s {
		if ch == ' ' {
			spaces++
			continue
		}
		flush()
		if ch == '\t' {
			sb.WriteString("<text:tab/>")
		} else {
			sb.WriteString(xesc(string(ch)))
		}
	}
	if spaces > 0 {
		fmt.Fprintf(&sb, `<text:s text:c="%d"/>`, spaces)
	}
	return sb.String()
}

func (w *odtWriter) runs(g *odReg, p *Para) string {
	var sb strings.Builder
	if p.PF.Bookmark != "" {
		sb.WriteString(`<text:bookmark text:name="` + xesc(p.PF.Bookmark) + `"/>`)
	}
	for ri, r := range p.Runs {
		if r.F.Cmt != "" && !w.cmtStarted[r.F.Cmt] {
			if c := w.doc.Comments[r.F.Cmt]; c != nil {
				w.cmtStarted[r.F.Cmt] = true
				res := ""
				if c.Done {
					res = ` loext:resolved="true"`
				}
				fmt.Fprintf(&sb, `<office:annotation office:name="__Annotation__%s"%s><dc:creator>%s</dc:creator><dc:date>%s</dc:date>`, xesc(r.F.Cmt), res, xesc(c.Author), xesc(odDate(c.Date)))
				for _, l := range strings.Split(c.Text, "\n") {
					sb.WriteString("<text:p>" + odText(l) + "</text:p>")
				}
				sb.WriteString("</office:annotation>")
			}
		}
		var item string
		switch r.T {
		case RText:
			inner := odText(string(r.Text))
			ff := r.F
			if strings.HasPrefix(ff.Link, "#_Toc") {
				ff.Link = ""
			}
			if n := g.t(ff); n != "" {
				inner = `<text:span text:style-name="` + n + `">` + inner + `</text:span>`
			}
			if r.F.Link != "" {
				inner = `<text:a xlink:type="simple" xlink:href="` + xesc(r.F.Link) + `">` + inner + `</text:a>`
			}
			if r.F.Del != "" {
				id := w.change("deletion", r.F.Del, "<text:p>"+inner+"</text:p>")
				inner = `<text:change text:change-id="` + id + `"/>`
			} else if r.F.Ins != "" {
				id := w.change("insertion", r.F.Ins, "")
				inner = `<text:change-start text:change-id="` + id + `"/>` + inner + `<text:change-end text:change-id="` + id + `"/>`
			}
			item = inner
		case RBreak:
			item = "<text:line-break/>"
		case RField:
			f := `<text:page-number text:select-page="current">1</text:page-number>`
			switch {
			case r.Name == "NUMPAGES":
				f = `<text:page-count>1</text:page-count>`
			case strings.HasPrefix(r.Name, "PAGEREF "):
				bm := strings.Fields(r.Name)[1]
				f = `<text:bookmark-ref text:reference-format="page" text:ref-name="` + xesc(bm) + `">` + xesc(w.doc.FieldValue(r.Name, "1", "1")) + `</text:bookmark-ref>`
			case strings.HasPrefix(r.Name, "SEQ "):
				cat, _, _ := SeqField(r.Name)
				ref := ""
				if p.PF.Bookmark != "" {
					ref = ` text:ref-name="` + xesc(p.PF.Bookmark) + `"`
				}
				f = `<text:sequence text:name="` + xesc(cat) + `" text:formula="ooow:` + xesc(cat) + `+1" style:num-format="1"` + ref + `>` +
					xesc(w.doc.FieldValue(r.Name, "1", "1")) + `</text:sequence>`
			case strings.HasPrefix(r.Name, "REF "):
				fs := strings.Fields(r.Name)
				val := xesc(w.doc.FieldValue(r.Name, "1", "1"))
				mode := "l"
				if len(fs) > 2 {
					mode = fs[2]
				}
				pre, post := "", ""
				switch mode {
				case "p":
					pre, post, val = "(", ")", strings.TrimSuffix(strings.TrimPrefix(val, "("), ")")
				case "s":
					if i := strings.LastIndex(val, " "); i >= 0 {
						pre, val = val[:i+1], val[i+1:]
					}
				}
				if mode == "l" {
					f = `<text:sequence-ref text:reference-format="category-and-value" text:ref-name="` + xesc(fs[1]) + `">` + val + `</text:sequence-ref>`
				} else {
					f = xesc(pre) + `<text:sequence-ref text:reference-format="value" text:ref-name="` + xesc(fs[1]) + `">` + val + `</text:sequence-ref>` + xesc(post)
				}
			case strings.HasPrefix(r.Name, "CITE "):
				w.frames++
				nm := xesc(fmt.Sprintf("TexoraCite:%s|%d", strings.TrimPrefix(r.Name, "CITE "), w.frames))
				f = `<text:reference-mark-start text:name="` + nm + `"/>` + xesc(w.doc.FieldValue(r.Name, "1", "1")) + `<text:reference-mark-end text:name="` + nm + `"/>`
			}
			ff := r.F
			ff.Link = ""
			if n := g.t(ff); n != "" {
				f = `<text:span text:style-name="` + n + `">` + f + `</text:span>`
			}
			item = f
		case RNote:
			if n, ok := w.fnNum[r.ID]; ok {
				item = fmt.Sprintf(`<text:note text:id="ftn%d" text:note-class="footnote"><text:note-citation>%d</text:note-citation><text:note-body>`, n, n) +
					w.blocks(g, w.doc.Footnotes[r.ID].Blocks, true) + `</text:note-body></text:note>`
			}
		case REq:
			w.objs++
			dir := fmt.Sprintf("Object %d", w.objs)
			size := r.F.Size
			if size == 0 {
				size = EffF(w.doc, p, r.F).Size
			}
			ew, eh := eqSizePt(string(r.Text), size)
			math := `<?xml version="1.0" encoding="UTF-8"?>` + EqMathML(string(r.Text))
			w.files = append(w.files, wfile{name: dir + "/content.xml", data: []byte(math)})
			w.mimes[dir+"/content.xml"] = "text/xml"
			w.mimes[dir+"/"] = "application/vnd.oasis.opendocument.formula"
			w.frames++
			item = fmt.Sprintf(`<draw:frame draw:style-name="%s" draw:name="Formula%d" text:anchor-type="as-char" svg:width="%s" svg:height="%s" draw:z-index="%d"><draw:object xlink:href="./%s" xlink:type="simple" xlink:show="embed" xlink:actuate="onLoad"/></draw:frame>`,
				w.graphicStyle(g, "eq", ""), w.frames, ptS(ew), ptS(eh), w.frames, dir)
		case RImage:
			m := w.doc.Media[r.ID]
			if m == nil {
				continue
			}
			pth, ok := w.pics[r.ID]
			if !ok {
				pth = fmt.Sprintf("Pictures/img%d.%s", len(w.pics)+1, extByMime(m.Mime))
				w.pics[r.ID] = pth
				w.files = append(w.files, wfile{name: pth, data: m.Data, store: true})
				w.mimes[pth] = m.Mime
			}
			w.frames++
			if r.Wrap != "" {
				item = fmt.Sprintf(`<draw:frame draw:style-name="%s" draw:name="Image%d" text:anchor-type="paragraph" svg:width="%s" svg:height="%s" draw:z-index="%d"><draw:image xlink:href="%s" xlink:type="simple" xlink:show="embed" xlink:actuate="onLoad"/></draw:frame>`,
					w.graphicStyle(g, r.Wrap, r.Side), w.frames, ptS(r.W), ptS(r.H), w.frames, pth)
			} else {
				item = fmt.Sprintf(`<draw:frame draw:name="Image%d" text:anchor-type="as-char" svg:width="%s" svg:height="%s" draw:z-index="0"><draw:image xlink:href="%s" xlink:type="simple" xlink:show="embed" xlink:actuate="onLoad"/></draw:frame>`,
					w.frames, ptS(r.W), ptS(r.H), pth)
			}
		}
		sb.WriteString(item)
		if r.F.Cmt != "" && w.cmtLast[r.F.Cmt] == (cmtKey{p, ri}) {
			sb.WriteString(`<office:annotation-end office:name="__Annotation__` + xesc(r.F.Cmt) + `"/>`)
		}
	}
	return sb.String()
}

func (w *odtWriter) para(g *odReg, p *Para, inNote bool) string {
	pf := p.PF
	if inNote {
		pf.Heading, pf.Style = 0, ""
	}
	var mark F
	if len(p.Runs) == 0 {
		mark = p.Mark
	}
	if w.cell > 0 {
		if !pf.After.Set {
			pf.After = O(0)
		}
		if !pf.Line.Set && pf.LineExact == 0 {
			pf.Line = O(1)
		}
	}
	if pf.CS == "" {
		pf.CS = CSKey(p)
	}
	indL := 0.0
	if len(pf.Tabs) > 0 {
		indL = EffPF(w.doc, p).IndL
	}
	master, num := "", 0
	if w.master != "" && w.cell == 0 && !inNote {
		master, num = w.master, w.masterNum
		w.master, w.masterNum = "", 0
	}
	name := g.pEx(pf, mark, master, num, indL)
	if pf.Heading > 0 {
		return fmt.Sprintf(`<text:h text:style-name="%s" text:outline-level="%d">%s</text:h>`, name, pf.Heading, w.runs(g, p))
	}
	return `<text:p text:style-name="` + name + `">` + w.runs(g, p) + `</text:p>`
}

func (w *odtWriter) blocks(g *odReg, bs []Block, inNote bool) string {
	var sb strings.Builder
	for i := 0; i < len(bs); i++ {
		b := bs[i]
		if b.P != nil && b.P.PF.Region != "" && !inNote && w.cell == 0 {
			kind := b.P.PF.Region
			j := i
			for j < len(bs) && bs[j].P != nil && bs[j].P.PF.Region == kind {
				j++
			}
			region := make([]Block, j-i)
			for k := i; k < j; k++ {
				c := *bs[k].P
				c.PF.Region = ""
				region[k-i] = Block{P: &c}
			}
			inner := w.blocks(g, region, false)
			if kind == "toc" {
				var tpl strings.Builder
				for l := 1; l <= 3; l++ {
					fmt.Fprintf(&tpl, `<text:table-of-content-entry-template text:outline-level="%d" text:style-name="Contents_20_%d"><text:index-entry-link-start text:style-name="Index_20_Link"/><text:index-entry-chapter/><text:index-entry-text/><text:index-entry-tab-stop style:type="right" style:leader-char="."/><text:index-entry-page-number/><text:index-entry-link-end/></text:table-of-content-entry-template>`, l, l)
				}
				sb.WriteString(`<text:table-of-content text:protected="true" text:name="Оглавление1"><text:table-of-content-source text:outline-level="3"><text:index-title-template/>` + tpl.String() + `</text:table-of-content-source><text:index-body>` + inner + `</text:index-body></text:table-of-content>`)
			} else if strings.HasPrefix(kind, "lof:") {
				sb.WriteString(`<text:section text:name="TexoraLOF:` + xesc(strings.TrimPrefix(kind, "lof:")) + `">` + inner + `</text:section>`)
			} else {
				sb.WriteString(`<text:section text:name="TexoraBib">` + inner + `</text:section>`)
			}
			i = j - 1
			continue
		}
		if b.T != nil {
			sb.WriteString(w.table(g, b.T))
			continue
		}
		if b.P.PF.List == nil || inNote {
			sb.WriteString(w.para(g, b.P, inNote))
			continue
		}
		id := b.P.PF.List.ID
		j := i
		for j < len(bs) && bs[j].P != nil && bs[j].P.PF.List != nil && bs[j].P.PF.List.ID == id {
			j++
		}
		style := "LUL"
		if b.P.PF.List.Kind == "ol" {
			style = "LOL"
		} else if ch := strings.TrimSpace(b.P.PF.List.Text); ch != "" && ch != "•" && len([]rune(ch)) <= 2 {
			if w.bullets == nil {
				w.bullets = map[string]string{}
			}
			if _, ok := w.bullets[ch]; !ok {
				w.bullets[ch] = fmt.Sprintf("LUL%d", len(w.bullets)+1)
			}
			style = w.bullets[ch]
		}
		sb.WriteString(`<text:list text:style-name="` + style + `">`)
		depth := 0
		for _, it := range bs[i:j] {
			L := min(9, it.P.PF.List.Level)
			for depth < L {
				sb.WriteString("<text:list-item><text:list>")
				depth++
			}
			for depth > L {
				sb.WriteString("</text:list></text:list-item>")
				depth--
			}
			sb.WriteString("<text:list-item>" + w.para(g, it.P, false) + "</text:list-item>")
		}
		for depth > 0 {
			sb.WriteString("</text:list></text:list-item>")
			depth--
		}
		sb.WriteString("</text:list>")
		i = j - 1
	}
	return sb.String()
}

func (w *odtWriter) table(g *odReg, t *Table) string {
	pg := w.doc.Page
	cols := TableCols(t, pg.W-pg.Left-pg.Right)
	w.tables++
	name := fmt.Sprintf("Tbl%d", w.tables)
	total := 0.0
	for _, c := range cols {
		total += c
	}
	fmt.Fprintf(&g.xml, `<style:style style:name="%s" style:family="table"><style:table-properties style:width="%s" table:align="left"/></style:style>`, name, ptS(total))
	for k, c := range cols {
		fmt.Fprintf(&g.xml, `<style:style style:name="%s.C%d" style:family="table-column"><style:table-column-properties style:column-width="%s"/></style:style>`, name, k, ptS(c))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<table:table table:name="Таблица%d" table:style-name="%s">`, w.tables, name)
	for k := range cols {
		fmt.Fprintf(&sb, `<table:table-column table:style-name="%s.C%d"/>`, name, k)
	}
	cellStyles := map[string]string{}
	edge := func(b *Border) string {
		if b == nil {
			return "none"
		}
		col := "#000000"
		if b.Color != "" {
			col = b.Color
		}
		st := "solid"
		if b.Style == "double" {
			st = "double"
		}
		return fmt.Sprintf("%spt %s %s", strconv.FormatFloat(b.W, 'f', 2, 64), st, col)
	}
	for ri, row := range t.Rows {
		if row.Header {
			sb.WriteString("<table:table-header-rows>")
		}
		if row.H > 0 {
			rs := fmt.Sprintf("%s.r%d", name, ri)
			prop := "style:min-row-height"
			if row.HRule == "exact" {
				prop = "style:row-height"
			}
			fmt.Fprintf(&g.xml, `<style:style style:name="%s" style:family="table-row"><style:table-row-properties %s="%s"/></style:style>`, rs, prop, ptS(row.H))
			fmt.Fprintf(&sb, `<table:table-row table:style-name="%s">`, rs)
		} else {
			sb.WriteString("<table:table-row>")
		}
		for ci, c := range row.Cells {
			span := max(1, c.Span)
			cpt, cpl, cpb, cpr := t.CellPads(c)
			props := fmt.Sprintf(`fo:padding-top="%s" fo:padding-bottom="%s" fo:padding-left="%s" fo:padding-right="%s" fo:border-top="%s" fo:border-left="%s" fo:border-bottom="%s" fo:border-right="%s"`,
				ptS(cpt), ptS(cpb), ptS(cpl), ptS(cpr),
				edge(t.EdgeBorder(ri, ci, len(row.Cells), c, 0)), edge(t.EdgeBorder(ri, ci, len(row.Cells), c, 1)),
				edge(t.EdgeBorder(ri, ci, len(row.Cells), c, 2)), edge(t.EdgeBorder(ri, ci, len(row.Cells), c, 3)))
			if c.Shade != "" {
				props += ` fo:background-color="` + c.Shade + `"`
			}
			if c.VAlign != "" {
				props += ` style:vertical-align="` + map[string]string{"center": "middle", "bottom": "bottom"}[c.VAlign] + `"`
			}
			cs, ok := cellStyles[props]
			if !ok {
				cs = fmt.Sprintf("%s.c%d", name, len(cellStyles))
				cellStyles[props] = cs
				fmt.Fprintf(&g.xml, `<style:style style:name="%s" style:family="table-cell"><style:table-cell-properties %s/></style:style>`, cs, props)
			}
			fmt.Fprintf(&sb, `<table:table-cell table:style-name="%s" office:value-type="string"`, cs)
			if span > 1 {
				fmt.Fprintf(&sb, ` table:number-columns-spanned="%d"`, span)
			}
			sb.WriteString(">")
			w.cell++
			inner := w.blocks(g, c.Blocks, false)
			w.cell--
			if inner == "" {
				inner = "<text:p/>"
			}
			sb.WriteString(inner + "</table:table-cell>")
			for k := 1; k < span; k++ {
				sb.WriteString("<table:covered-table-cell/>")
			}
		}
		sb.WriteString("</table:table-row>")
		if row.Header {
			sb.WriteString("</table:table-header-rows>")
		}
	}
	sb.WriteString("</table:table>")
	return sb.String()
}

const odNS = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
	`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" ` +
	`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" xmlns:xlink="http://www.w3.org/1999/xlink" ` +
	`xmlns:meta="urn:oasis:names:tc:opendocument:xmlns:meta:1.0" xmlns:dc="http://purl.org/dc/elements/1.1/" ` +
	`xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0" ` +
	`xmlns:math="http://www.w3.org/1998/Math/MathML"`

func listStylesODT(bullets map[string]string) string {
	var ul, ol, custom strings.Builder
	bl := []string{"•", "◦", "▪"}
	nf := []string{"1", "a", "i"}
	for l := 1; l <= 10; l++ {
		pos := fmt.Sprintf(`<style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="%s" fo:text-indent="-18pt" fo:margin-left="%s"/></style:list-level-properties>`, ptS(36*float64(l)), ptS(36*float64(l)))
		fmt.Fprintf(&ul, `<text:list-level-style-bullet text:level="%d" text:bullet-char="%s">%s</text:list-level-style-bullet>`, l, bl[(l-1)%3], pos)
		_ = pos
		suf := ")"
		if (l-1)%3 == 0 {
			suf = "."
		}
		fmt.Fprintf(&ol, `<text:list-level-style-number text:level="%d" style:num-format="%s" style:num-suffix="%s">%s</text:list-level-style-number>`, l, nf[(l-1)%3], suf, pos)
	}
	for _, ch := range sortedKeys(bullets) {
		custom.WriteString(`<text:list-style style:name="` + bullets[ch] + `">`)
		for l := 1; l <= 10; l++ {
			fmt.Fprintf(&custom, `<text:list-level-style-bullet text:level="%d" text:bullet-char="%s"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="%s" fo:text-indent="-18pt" fo:margin-left="%s"/></style:list-level-properties></text:list-level-style-bullet>`,
				l, xesc(ch), ptS(36*float64(l)), ptS(36*float64(l)))
		}
		custom.WriteString(`</text:list-style>`)
	}
	return `<text:list-style style:name="LUL">` + ul.String() + `</text:list-style><text:list-style style:name="LOL">` + ol.String() + `</text:list-style>` + custom.String()
}

// odHeadingStyles — общие стили заголовков, оглавления и формул.
func odStyleProps(h headingStyle, font string, d *Doc) (string, string) {
	pp := fmt.Sprintf(`fo:margin-top="%s" fo:margin-bottom="%s"`, ptS(math.Max(0, h.Before)), ptS(math.Max(0, h.After)))
	if h.Align != "" && h.Align != "left" {
		pp += ` fo:text-align="` + map[string]string{"center": "center", "right": "end", "justify": "justify"}[h.Align] + `"`
	}
	if h.IndL != 0 {
		pp += ` fo:margin-left="` + ptS(h.IndL) + `"`
	}
	if h.Set {
		pp += ` fo:text-indent="` + ptS(h.IndF) + `"`
	}
	if h.Line > 0 {
		pp += fmt.Sprintf(` fo:line-height="%d%%"`, int(math.Round(h.Line*100)))
	}
	var tp []string
	if h.Size > 0 {
		tp = append(tp, `fo:font-size="`+ptS(h.Size)+`"`)
	}
	if h.Color != "" {
		tp = append(tp, `fo:color="`+h.Color+`"`)
	}
	if h.B {
		tp = append(tp, `fo:font-weight="bold"`)
	}
	if h.I {
		tp = append(tp, `fo:font-style="italic"`)
	}
	if h.Caps {
		tp = append(tp, `fo:text-transform="uppercase"`)
	}
	if font != "" {
		tp = append(tp, `style:font-name="`+xesc(font)+`"`)
	}
	return pp, strings.Join(tp, " ")
}

func WriteODT(d *Doc) ([]byte, error) {
	d.ensure()
	w := &odtWriter{doc: d, pics: map[string]string{}, mimes: map[string]string{}, fnNum: map[string]int{},
		cmtLast: map[string]cmtKey{}, cmtStarted: map[string]bool{}, gstyles: map[string]bool{}}
	for i, id := range d.FootnoteOrder() {
		w.fnNum[id] = i + 1
	}
	for _, en := range Flatten(&d.Body, nil, nil) {
		for i, r := range en.P.Runs {
			if r.F.Cmt != "" {
				w.cmtLast[r.F.Cmt] = cmtKey{en.P, i}
			}
		}
	}
	pg, def := d.Page, d.Def
	w.width = pg.W - pg.Left - pg.Right
	ss := d.Sections()
	cg := newOdReg("")
	var bodyB strings.Builder
	for i, sv := range ss {
		w.master, w.masterNum = fmt.Sprintf("MP%d", i), sv.Start
		bodyB.WriteString(w.blocks(cg, d.Body[sv.From:sv.To], false))
	}
	body := bodyB.String()
	if w.chN > 0 || d.Track {
		tc := "false"
		if d.Track {
			tc = "true"
		}
		body = `<text:tracked-changes text:track-changes="` + tc + `">` + w.changes.String() + `</text:tracked-changes>` + body
	}
	sg := newOdReg("M")
	// мастер-страницы разделов
	var layouts strings.Builder
	var masters []string
	for i, sv := range ss {
		var mps strings.Builder
		spg := sv.Page
		get := func(kind, v string) string {
			_, bs := d.ResolveHF(ss, i, kind, v)
			if len(bs) == 0 {
				return ""
			}
			return w.blocks(sg, bs, false)
		}
		header, footer := get("header", ""), get("footer", "")
		topM, botM := spg.Top, spg.Bottom
		hstyle, fstyle := "", ""
		if header != "" || (sv.TitlePg && get("header", "first") != "") {
			topM = spg.Header
			hstyle = fmt.Sprintf(`<style:header-style><style:header-footer-properties fo:min-height="0pt" fo:margin-bottom="%s"/></style:header-style>`, ptS(math.Max(4, spg.Top-spg.Header-14)))
		}
		if footer != "" || (sv.TitlePg && get("footer", "first") != "") {
			botM = spg.Footer
			fstyle = fmt.Sprintf(`<style:footer-style><style:header-footer-properties fo:min-height="0pt" fo:margin-top="%s"/></style:footer-style>`, ptS(math.Max(4, spg.Bottom-spg.Footer-14)))
		}
		orient := "portrait"
		if spg.W > spg.H {
			orient = "landscape"
		}
		fmt.Fprintf(&layouts, `<style:page-layout style:name="pm%d"><style:page-layout-properties fo:page-width="%s" fo:page-height="%s" style:print-orientation="%s" fo:margin-top="%s" fo:margin-bottom="%s" fo:margin-left="%s" fo:margin-right="%s">%s</style:page-layout-properties>%s%s</style:page-layout>`,
			i, ptS(spg.W), ptS(spg.H), orient, ptS(topM), ptS(botM), ptS(spg.Left), ptS(spg.Right), odColumns(sv.Cols), hstyle, fstyle)
		fmt.Fprintf(&mps, `<style:master-page style:name="MP%d" style:display-name="Раздел %d" style:page-layout-name="pm%d">`, i, i+1, i)
		for _, kind := range []string{"header", "footer"} {
			main := header
			if kind == "footer" {
				main = footer
			}
			if main != "" {
				mps.WriteString("<style:" + kind + ">" + main + "</style:" + kind + ">")
			}
			if d.EvenOdd {
				if ev := get(kind, "even"); ev != "" {
					mps.WriteString("<style:" + kind + "-left>" + ev + "</style:" + kind + "-left>")
				} else if main != "" {
					mps.WriteString("<style:" + kind + `-left style:display="false"/>`)
				}
			}
			if sv.TitlePg {
				if fi := get(kind, "first"); fi != "" {
					mps.WriteString("<style:" + kind + "-first>" + fi + "</style:" + kind + "-first>")
				} else if main != "" {
					mps.WriteString("<style:" + kind + `-first style:display="false"/>`)
				}
			}
		}
		mps.WriteString("</style:master-page>")
		masters = append(masters, mps.String())
	}
	fonts := map[string]bool{def.Font: true, headingFont(d): true}
	for f := range cg.fonts {
		fonts[f] = true
	}
	for f := range sg.fonts {
		fonts[f] = true
	}
	for _, k := range []string{"h1", "h2", "h3", "h4", "h5", "h6", "TOCHeading"} {
		if h, _ := d.StyleDef(k); h.Font != "" {
			fonts[h.Font] = true
		}
	}
	var fd strings.Builder
	fd.WriteString("<office:font-face-decls>")
	for _, f := range sortedKeys(fonts) {
		fmt.Fprintf(&fd, `<style:font-face style:name="%s" svg:font-family="'%s'"/>`, xesc(f), xesc(f))
	}
	fd.WriteString("</office:font-face-decls>")
	content := `<?xml version="1.0" encoding="UTF-8"?><office:document-content ` + odNS + ` office:version="1.3">` + fd.String() +
		`<office:automatic-styles>` + cg.xml.String() + listStylesODT(w.bullets) + `</office:automatic-styles><office:body><office:text>` + body + `</office:text></office:body></office:document-content>`
	var hs strings.Builder
	for n := 1; n <= 6; n++ {
		h, _ := d.StyleDef("h" + strconv.Itoa(n))
		font := h.Font
		if font == "" && n <= 2 {
			font = headingFont(d)
		}
		pp, tp := odStyleProps(h, font, d)
		fmt.Fprintf(&hs, `<style:style style:name="Heading_20_%d" style:display-name="Heading %d" style:family="paragraph" style:parent-style-name="Standard" style:next-style-name="Standard" style:default-outline-level="%d" style:class="text">`+
			`<style:paragraph-properties %s fo:keep-with-next="always"/><style:text-properties %s/></style:style>`, n, n, n, pp, tp)
	}
	t, _ := d.StyleDef("Title")
	fmt.Fprintf(&hs, `<style:style style:name="Title" style:family="paragraph" style:parent-style-name="Standard" style:class="chapter"><style:paragraph-properties fo:margin-bottom="%s" fo:line-height="100%%"/><style:text-properties fo:font-size="%s" style:font-name="%s"/></style:style>`, ptS(t.After), ptS(t.Size), xesc(headingFont(d)))
	fmt.Fprintf(&hs, `<style:style style:name="Subtitle" style:family="paragraph" style:parent-style-name="Standard" style:class="chapter"><style:paragraph-properties fo:margin-bottom="8pt"/><style:text-properties fo:font-size="14pt" fo:color="#595959"/></style:style>`)
	hs.WriteString(`<style:style style:name="Quotations" style:family="paragraph" style:parent-style-name="Standard" style:class="html"><style:paragraph-properties fo:margin-left="43pt" fo:margin-right="43pt" fo:margin-top="8pt" fo:text-align="center"/><style:text-properties fo:font-style="italic" fo:color="#404040"/></style:style>`)
	hs.WriteString(`<style:style style:name="List_20_Paragraph" style:display-name="List Paragraph" style:family="paragraph" style:parent-style-name="Standard"><style:paragraph-properties loext:contextual-spacing="true"/></style:style>`)
	hs.WriteString(`<style:style style:name="No_20_Spacing" style:display-name="No Spacing" style:family="paragraph" style:parent-style-name="Standard"><style:paragraph-properties fo:margin-bottom="0pt" fo:line-height="100%"/></style:style>`)
	{
		h, _ := d.StyleDef("TOCHeading")
		font := h.Font
		if font == "" {
			font = headingFont(d)
		}
		pp, tp := odStyleProps(h, font, d)
		fmt.Fprintf(&hs, `<style:style style:name="Contents_20_Heading" style:display-name="Contents Heading" style:family="paragraph" style:parent-style-name="Standard" style:class="index"><style:paragraph-properties %s/><style:text-properties %s/></style:style>`, pp, tp)
	}
	for l := 1; l <= 3; l++ {
		h, _ := d.StyleDef("TOC" + strconv.Itoa(l))
		pp, tp := odStyleProps(h, h.Font, d)
		fmt.Fprintf(&hs, `<style:style style:name="Contents_20_%d" style:display-name="Contents %d" style:family="paragraph" style:parent-style-name="Standard" style:class="index"><style:paragraph-properties %s><style:tab-stops><style:tab-stop style:position="%s" style:type="right" style:leader-style="dotted" style:leader-text="."/></style:tab-stops></style:paragraph-properties><style:text-properties %s/></style:style>`,
			l, l, pp, ptS(math.Max(0, w.width-h.IndL)), tp)
	}
	hs.WriteString(`<style:style style:name="Index_20_Link" style:display-name="Index Link" style:family="text"/>`)
	hs.WriteString(`<style:style style:name="Formula" style:family="graphic"><style:graphic-properties text:anchor-type="as-char" svg:y="0cm" fo:margin-left="0.07cm" fo:margin-right="0.07cm" style:vertical-pos="middle" style:vertical-rel="text"/></style:style>`)
	hyph := ""
	if d.Hyphen {
		hyph = ` fo:hyphenate="true" fo:hyphenation-remain-char-count="2" fo:hyphenation-push-char-count="2"`
	}
	styles := `<?xml version="1.0" encoding="UTF-8"?><office:document-styles ` + odNS + ` office:version="1.3">` + fd.String() + `<office:styles>` +
		fmt.Sprintf(`<style:default-style style:family="paragraph"><style:paragraph-properties fo:margin-bottom="%s" fo:line-height="%d%%"/><style:text-properties style:font-name="%s" fo:font-size="%s" fo:language="ru" fo:country="RU"%s/></style:default-style>`,
			ptS(def.After), int(math.Round(def.Line*100)), xesc(def.Font), ptS(def.Size), hyph) +
		`<style:style style:name="Standard" style:family="paragraph" style:class="text"/>` + hs.String() + `</office:styles><office:automatic-styles>` + sg.xml.String() +
		layouts.String() + `</office:automatic-styles><office:master-styles>` +
		strings.Replace(masters[0], `style:name="MP0" style:display-name="Раздел 1"`, `style:name="Standard" style:display-name="Обычный"`, 1) +
		strings.Join(masters, "") + `</office:master-styles></office:document-styles>`
	// «Standard» — копия первого раздела (для абзацев без явной мастер-страницы)
	var man strings.Builder
	man.WriteString(`<?xml version="1.0" encoding="UTF-8"?><manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">` +
		`<manifest:file-entry manifest:full-path="/" manifest:version="1.3" manifest:media-type="application/vnd.oasis.opendocument.text"/>`)
	for _, f := range []string{"content.xml", "styles.xml", "meta.xml"} {
		fmt.Fprintf(&man, `<manifest:file-entry manifest:full-path="%s" manifest:media-type="text/xml"/>`, f)
	}
	for i := 1; i <= w.objs; i++ {
		fmt.Fprintf(&man, `<manifest:file-entry manifest:full-path="Object %d/" manifest:version="1.3" manifest:media-type="application/vnd.oasis.opendocument.formula"/>`, i)
	}
	for _, f := range w.files {
		fmt.Fprintf(&man, `<manifest:file-entry manifest:full-path="%s" manifest:media-type="%s"/>`, f.name, w.mimes[f.name])
	}
	man.WriteString("</manifest:manifest>")
	ud := ""
	if len(d.Sources) > 0 {
		if b, err := json.Marshal(d.Sources); err == nil {
			ud = `<meta:user-defined meta:name="TexoraSources" meta:value-type="string">` + xesc(string(b)) + `</meta:user-defined>`
		}
	}
	meta := `<?xml version="1.0" encoding="UTF-8"?><office:document-meta ` + odNS + ` office:version="1.3"><office:meta><meta:generator>Texora</meta:generator>` + ud + `</office:meta></office:document-meta>`
	files := []wfile{{name: "mimetype", data: []byte("application/vnd.oasis.opendocument.text"), store: true},
		{name: "content.xml", data: []byte(content)}, {name: "styles.xml", data: []byte(styles)}, {name: "meta.xml", data: []byte(meta)}}
	files = append(files, w.files...)
	files = append(files, wfile{name: "META-INF/manifest.xml", data: []byte(man.String())})
	return zipFiles(files)
}

// odColumns — колонки страницы (style:columns).
func odColumns(c Columns) string {
	if c.Count() <= 1 {
		return ""
	}
	sep := ""
	if c.Sep {
		sep = `<style:column-sep style:width="0.5pt" style:color="#000000" style:height="100%" style:vertical-align="top"/>`
	}
	return fmt.Sprintf(`<style:columns fo:column-count="%d" fo:column-gap="%s">%s</style:columns>`, c.Count(), ptS(c.ColGap()), sep)
}
