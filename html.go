package main

// HTML буфера обмена <-> блоки (вставка из Word/браузера и копирование в них).

import (
	"encoding/base64"
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type hnode struct {
	tag   string
	attrs map[string]string
	kids  []*hnode
	text  string
	isTxt bool
}

var voidTags = map[string]bool{"br": true, "img": true, "hr": true, "meta": true, "link": true, "input": true, "col": true, "wbr": true}
var autoClose = map[string][]string{"p": {"p", "li"}, "li": {"li"}, "tr": {"tr", "td", "th"}, "td": {"td", "th"}, "th": {"td", "th"}}
var tagRe = regexp.MustCompile(`^<\s*(/?)\s*([a-zA-Z][\w:.-]*)((?:\s+[^\s=>/]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+))?)*)\s*(/?)\s*>`)
var attrRe = regexp.MustCompile(`([^\s=>/]+)(?:\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+)))?`)

func parseHTML(src string) *hnode {
	root := &hnode{tag: "#root"}
	stack := []*hnode{root}
	top := func() *hnode { return stack[len(stack)-1] }
	i := 0
	for i < len(src) {
		if src[i] == '<' {
			if strings.HasPrefix(src[i:], "<!--") {
				e := strings.Index(src[i:], "-->")
				if e < 0 {
					break
				}
				i += e + 3
				continue
			}
			if strings.HasPrefix(src[i:], "<!") || strings.HasPrefix(src[i:], "<?") {
				e := strings.IndexByte(src[i:], '>')
				if e < 0 {
					break
				}
				i += e + 1
				continue
			}
			m := tagRe.FindStringSubmatch(src[i:])
			if m == nil {
				top().kids = append(top().kids, &hnode{isTxt: true, text: "<"})
				i++
				continue
			}
			i += len(m[0])
			tag := strings.ToLower(m[2])
			if m[1] == "/" {
				for k := len(stack) - 1; k > 0; k-- {
					if stack[k].tag == tag {
						stack = stack[:k]
						break
					}
				}
				continue
			}
			n := &hnode{tag: tag, attrs: map[string]string{}}
			for _, a := range attrRe.FindAllStringSubmatch(m[3], -1) {
				v := a[3] + a[4] + a[5]
				n.attrs[strings.ToLower(a[1])] = html.UnescapeString(v)
			}
			if closers, ok := autoClose[tag]; ok {
				for k := len(stack) - 1; k > 0; k-- {
					t := stack[k].tag
					if t == "table" || t == "ul" || t == "ol" || t == "body" {
						break
					}
					hit := false
					for _, c := range closers {
						if t == c {
							hit = true
						}
					}
					if hit {
						stack = stack[:k]
						break
					}
				}
			}
			top().kids = append(top().kids, n)
			if !voidTags[tag] && m[4] != "/" {
				stack = append(stack, n)
			}
			if tag == "style" || tag == "script" {
				e := strings.Index(strings.ToLower(src[i:]), "</"+tag)
				if e < 0 {
					break
				}
				i += e
			}
			continue
		}
		e := strings.IndexByte(src[i:], '<')
		if e < 0 {
			e = len(src) - i
		}
		top().kids = append(top().kids, &hnode{isTxt: true, text: html.UnescapeString(src[i : i+e])})
		i += e
	}
	return root
}

func cssMap(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) == 2 {
			out[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.TrimSpace(kv[1])
		}
	}
	return out
}

var namedColors = map[string]string{"black": "#000000", "white": "#ffffff", "red": "#ff0000", "green": "#008000", "blue": "#0000ff",
	"yellow": "#ffff00", "gray": "#808080", "grey": "#808080", "silver": "#c0c0c0", "maroon": "#800000", "navy": "#000080",
	"purple": "#800080", "teal": "#008080", "olive": "#808000", "lime": "#00ff00", "aqua": "#00ffff", "fuchsia": "#ff00ff", "orange": "#ffa500"}
var rgbRe = regexp.MustCompile(`rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)`)

func cssColor(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if c, ok := namedColors[v]; ok {
		return c
	}
	if strings.HasPrefix(v, "#") {
		if len(v) == 4 {
			return "#" + string([]byte{v[1], v[1], v[2], v[2], v[3], v[3]})
		}
		if len(v) == 7 {
			return v
		}
	}
	if m := rgbRe.FindStringSubmatch(v); m != nil {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		return fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return ""
}

func cssSize(v string) float64 {
	m := regexp.MustCompile(`([\d.]+)\s*(pt|px|em|rem|%)?`).FindStringSubmatch(v)
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "px":
		return n * 0.75
	case "em", "rem":
		return n * 12
	case "%":
		return n / 100 * 12
	}
	return n
}

func styleF(n *hnode, f F) F {
	switch n.tag {
	case "b", "strong":
		f.B = true
	case "i", "em":
		f.I = true
	case "u", "ins":
		f.U = true
	case "s", "strike", "del":
		f.S = true
	case "sup":
		f.Sup = true
	case "sub":
		f.Sub = true
	case "font":
		if c := cssColor(n.attrs["color"]); c != "" {
			f.Color = c
		}
		if fc := n.attrs["face"]; fc != "" {
			f.Font = strings.TrimSpace(strings.Split(fc, ",")[0])
		}
	}
	st := cssMap(n.attrs["style"])
	if w, ok := st["font-weight"]; ok {
		v, _ := strconv.Atoi(w)
		f.B = w == "bold" || w == "bolder" || v >= 600
	}
	if s, ok := st["font-style"]; ok {
		f.I = s == "italic" || s == "oblique"
	}
	deco := st["text-decoration"] + " " + st["text-decoration-line"] + " " + st["text-underline"]
	if strings.Contains(deco, "underline") || strings.Contains(deco, "single") {
		f.U = true
	}
	if strings.Contains(deco, "line-through") {
		f.S = true
	}
	if strings.Contains(deco, "none") && !strings.Contains(deco, "underline") {
		f.U = false
	}
	if va := st["vertical-align"]; va == "super" {
		f.Sup = true
	} else if va == "sub" {
		f.Sub = true
	}
	if c := cssColor(st["color"]); c != "" {
		if c == "#000000" {
			f.Color = ""
		} else {
			f.Color = c
		}
	}
	bg := st["background-color"]
	if bg == "" {
		bg = st["background"]
	}
	if hl := st["mso-highlight"]; hl != "" {
		bg = hl
	}
	if c := cssColor(bg); c != "" && c != "#ffffff" {
		f.Bg = c
	}
	if sz := cssSize(st["font-size"]); sz >= 4 && sz <= 200 {
		f.Size = math.Round(sz*2) / 2
	}
	if fam := st["font-family"]; fam != "" {
		f.Font = strings.Trim(strings.Split(fam, ",")[0], `"' `)
	}
	return f
}

func pfOf(n *hnode) PF {
	var pf PF
	st := cssMap(n.attrs["style"])
	al := st["text-align"]
	if al == "" {
		al = n.attrs["align"]
	}
	switch {
	case strings.Contains(al, "center"):
		pf.Align = "center"
	case strings.Contains(al, "right"):
		pf.Align = "right"
	case strings.Contains(al, "justify"):
		pf.Align = "justify"
	}
	if ml := cssSize(st["margin-left"]); ml > 0 && ml < 400 {
		pf.IndL = O(ml)
	}
	if ti, ok := st["text-indent"]; ok {
		v := cssSize(ti)
		if strings.HasPrefix(strings.TrimSpace(ti), "-") {
			v = -v
		}
		if v != 0 {
			pf.IndF = O(v)
		}
	}
	return pf
}

type htok struct {
	k   string // text open close br img table
	t   string
	f   F
	pf  PF
	pre bool
	img Run
	tbl *Table
}

var blockTags = map[string]bool{"p": true, "div": true, "blockquote": true, "pre": true, "section": true, "article": true, "header": true,
	"footer": true, "main": true, "center": true, "address": true, "figure": true, "figcaption": true, "dt": true, "dd": true, "dl": true,
	"nav": true, "aside": true, "body": true, "html": true}

func htmlTokens(nodes []*hnode, f F, pre bool, out *[]htok, media map[string]*Media, level int) {
	for _, n := range nodes {
		if n.isTxt {
			*out = append(*out, htok{k: "text", t: n.text, f: f, pre: pre})
			continue
		}
		if n.tag == "style" || n.tag == "script" || n.tag == "head" || n.tag == "title" || strings.Contains(n.tag, ":") {
			continue
		}
		if strings.Contains(strings.ReplaceAll(n.attrs["style"], " ", ""), "display:none") {
			continue
		}
		nf := styleF(n, f)
		switch {
		case n.tag == "br":
			*out = append(*out, htok{k: "br"})
		case n.tag == "img":
			src := n.attrs["src"]
			if strings.HasPrefix(src, "data:image/") {
				if i := strings.Index(src, ","); i > 0 {
					data, err := base64.StdEncoding.DecodeString(src[i+1:])
					if err == nil {
						id := NewID("m")
						media[id] = &Media{Mime: src[5:strings.Index(src, ";")], Data: data}
						w, _ := strconv.ParseFloat(n.attrs["width"], 64)
						h, _ := strconv.ParseFloat(n.attrs["height"], 64)
						if w == 0 {
							w = 100
						}
						if h == 0 {
							h = 100
						}
						*out = append(*out, htok{k: "img", img: Run{T: RImage, ID: id, W: w * 0.75, H: h * 0.75}})
					}
				}
			}
		case n.tag == "table":
			*out = append(*out, htok{k: "table", tbl: htmlTable(n, nf, media)})
		case n.tag == "ul" || n.tag == "ol":
			kind := "ul"
			if n.tag == "ol" {
				kind = "ol"
			}
			id := NewID("l")
			for _, li := range n.kids {
				if li.isTxt {
					continue
				}
				pf := pfOf(li)
				pf.List = &List{ID: id, Kind: kind, Level: level + 1}
				pf.IndL, pf.IndF = Opt{}, Opt{}
				*out = append(*out, htok{k: "open", pf: pf})
				htmlTokens(li.kids, styleF(li, nf), pre, out, media, level+1)
				*out = append(*out, htok{k: "close"})
			}
		case len(n.tag) == 2 && n.tag[0] == 'h' && n.tag[1] >= '1' && n.tag[1] <= '6':
			pf := pfOf(n)
			pf.Heading = int(n.tag[1] - '0')
			*out = append(*out, htok{k: "open", pf: pf})
			htmlTokens(n.kids, nf, pre, out, media, level)
			*out = append(*out, htok{k: "close"})
		case blockTags[n.tag] || n.tag == "li":
			pf := pfOf(n)
			*out = append(*out, htok{k: "open", pf: pf})
			htmlTokens(n.kids, nf, pre || n.tag == "pre", out, media, level)
			*out = append(*out, htok{k: "close"})
		default:
			if n.tag == "a" {
				if h := n.attrs["href"]; h != "" && !strings.HasPrefix(h, "#") && !strings.HasPrefix(strings.ToLower(h), "javascript") {
					nf.Link = h
				}
			}
			htmlTokens(n.kids, nf, pre, out, media, level)
		}
	}
}

var wsRe = regexp.MustCompile(`[\r\n\t ]+`)

func assembleHTML(toks []htok) []Block {
	var out []Block
	var cur *Para
	stack := []PF{{}}
	flush := func() {
		if cur != nil {
			cur.Normalize()
			if len(cur.Runs) > 0 && cur.Runs[0].T == RText {
				cur.Runs[0].Text = []rune(strings.TrimLeft(string(cur.Runs[0].Text), " "))
			}
			if n := len(cur.Runs); n > 0 && cur.Runs[n-1].T == RText {
				cur.Runs[n-1].Text = []rune(strings.TrimRight(string(cur.Runs[n-1].Text), " "))
			}
			cur.Normalize()
			if len(cur.Runs) > 0 {
				out = append(out, Block{P: cur})
			}
		}
		cur = nil
	}
	para := func() *Para {
		if cur == nil {
			pf := stack[len(stack)-1]
			if pf.List != nil {
				l := *pf.List
				pf.List = &l
			}
			cur = &Para{PF: pf}
		}
		return cur
	}
	for _, t := range toks {
		switch t.k {
		case "open":
			flush()
			stack = append(stack, t.pf)
		case "close":
			flush()
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case "table":
			flush()
			out = append(out, Block{T: t.tbl})
		case "br":
			para().Runs = append(para().Runs, Run{T: RBreak})
		case "img":
			para().Runs = append(para().Runs, t.img)
		case "text":
			text := t.t
			if !t.pre {
				text = wsRe.ReplaceAllString(text, " ")
			}
			if cur == nil && strings.TrimSpace(text) == "" {
				continue
			}
			for i, part := range strings.Split(text, "\n") {
				if i > 0 {
					para().Runs = append(para().Runs, Run{T: RBreak})
				}
				if part != "" {
					para().Runs = append(para().Runs, Run{T: RText, Text: []rune(strings.ReplaceAll(part, " ", " ")), F: t.f})
				}
			}
		}
	}
	flush()
	return out
}

func htmlTable(n *hnode, f F, media map[string]*Media) *Table {
	t := &Table{}
	if b, _ := strconv.Atoi(n.attrs["border"]); b > 0 || strings.Contains(n.attrs["style"], "solid") {
		t.Border = true
	}
	var rows []*hnode
	var walk func(x *hnode)
	walk = func(x *hnode) {
		for _, k := range x.kids {
			switch k.tag {
			case "tr":
				rows = append(rows, k)
			case "tbody", "thead", "tfoot":
				walk(k)
			}
		}
	}
	walk(n)
	for _, tr := range rows {
		row := &Row{}
		for _, td := range tr.kids {
			if td.tag != "td" && td.tag != "th" {
				continue
			}
			if strings.Contains(td.attrs["style"], "solid") {
				t.Border = true
			}
			cf := styleF(td, f)
			if td.tag == "th" {
				cf.B = true
			}
			var toks []htok
			htmlTokens(td.kids, cf, false, &toks, media, -1)
			bs := assembleHTML(toks)
			if len(bs) == 0 {
				bs = []Block{{P: NewPara()}}
			}
			span, _ := strconv.Atoi(td.attrs["colspan"])
			row.Cells = append(row.Cells, &Cell{Blocks: bs, Span: max(1, span)})
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

// HTMLToBlocks разбирает HTML (в том числе формат буфера Windows CF_HTML).
func HTMLToBlocks(src string) ([]Block, map[string]*Media) {
	if i := strings.Index(src, "<!--StartFragment-->"); i >= 0 {
		if j := strings.Index(src, "<!--EndFragment-->"); j > i {
			src = src[i+len("<!--StartFragment-->") : j]
		}
	} else if i := strings.Index(strings.ToLower(src), "<html"); i > 0 {
		src = src[i:]
	}
	media := map[string]*Media{}
	root := parseHTML(src)
	var toks []htok
	htmlTokens(root.kids, F{}, false, &toks, media, -1)
	return assembleHTML(toks), media
}

// BlocksToHTML — HTML для буфера обмена (вставка в Word, браузер, почту).
func BlocksToHTML(bs []Block, d *Doc) string {
	var sb strings.Builder
	openList := ""
	closeList := func() {
		if openList != "" {
			sb.WriteString("</" + openList + ">")
			openList = ""
		}
	}
	for _, b := range bs {
		if b.T != nil {
			closeList()
			sb.WriteString(`<table border="1" cellspacing="0" cellpadding="4" style="border-collapse:collapse">`)
			for _, r := range b.T.Rows {
				sb.WriteString("<tr>")
				for _, c := range r.Cells {
					span := ""
					if c.Span > 1 {
						span = fmt.Sprintf(` colspan="%d"`, c.Span)
					}
					sb.WriteString("<td" + span + ">" + BlocksToHTML(c.Blocks, d) + "</td>")
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</table>")
			continue
		}
		p := b.P
		var inner strings.Builder
		for _, r := range p.Runs {
			switch r.T {
			case RBreak:
				inner.WriteString("<br>")
			case REq:
				inner.WriteString(strings.Replace(EqMathML(string(r.Text)), "<math ", `<math display="inline" `, 1))
			case RField:
				if d != nil {
					inner.WriteString(htmlEsc(d.FieldValue(r.Name, "1", "1")))
				}
			case RImage:
				if m := d.Media[r.ID]; m != nil {
					fmt.Fprintf(&inner, `<img src="data:%s;base64,%s" width="%d" height="%d">`, m.Mime, base64.StdEncoding.EncodeToString(m.Data),
						int(r.W/0.75), int(r.H/0.75))
				}
			case RText:
				if r.F.Del != "" {
					continue
				}
				var st []string
				f := r.F
				if f.B {
					st = append(st, "font-weight:bold")
				}
				if f.I {
					st = append(st, "font-style:italic")
				}
				var deco []string
				if f.U || f.DU {
					deco = append(deco, "underline")
				}
				if f.S {
					deco = append(deco, "line-through")
				}
				if len(deco) > 0 {
					st = append(st, "text-decoration:"+strings.Join(deco, " "))
				}
				if f.Color != "" {
					st = append(st, "color:"+f.Color)
				}
				if f.Bg != "" {
					st = append(st, "background:"+f.Bg)
				}
				size := f.Size
				if size == 0 {
					size = EffF(d, p, f).Size
				}
				st = append(st, fmt.Sprintf("font-size:%gpt", size))
				font := f.Font
				if font == "" {
					font = EffF(d, p, f).Font
				}
				st = append(st, fmt.Sprintf("font-family:'%s'", font))
				if f.Sup {
					st = append(st, "vertical-align:super")
				}
				if f.Sub {
					st = append(st, "vertical-align:sub")
				}
				s := html.EscapeString(string(r.Text))
				s = strings.ReplaceAll(s, "\t", "&emsp;")
				s = `<span style="` + strings.Join(st, ";") + `">` + s + `</span>`
				if f.Link != "" {
					s = `<a href="` + html.EscapeString(f.Link) + `">` + s + `</a>`
				}
				inner.WriteString(s)
			}
		}
		if inner.Len() == 0 {
			inner.WriteString("&nbsp;")
		}
		al := ""
		if p.PF.Align != "" {
			al = ` style="text-align:` + p.PF.Align + `"`
		}
		if p.PF.List != nil {
			lt := "ul"
			if p.PF.List.Kind == "ol" {
				lt = "ol"
			}
			if openList != lt {
				closeList()
				sb.WriteString("<" + lt + ">")
				openList = lt
			}
			sb.WriteString("<li" + al + ">" + inner.String() + "</li>")
			continue
		}
		closeList()
		tag := "p"
		if p.PF.Heading > 0 {
			tag = "h" + strconv.Itoa(p.PF.Heading)
		}
		sb.WriteString("<" + tag + al + ">" + inner.String() + "</" + tag + ">")
	}
	closeList()
	return sb.String()
}

// CFHTML — обёртка Windows для формата "HTML Format".
func CFHTML(fragment string) string {
	head := "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
	pre := "<html><head><meta charset=\"utf-8\"></head><body><!--StartFragment-->"
	post := "<!--EndFragment--></body></html>"
	hl := len(fmt.Sprintf(head, 0, 0, 0, 0))
	sh := hl
	sf := sh + len(pre)
	ef := sf + len(fragment)
	eh := ef + len(post)
	return fmt.Sprintf(head, sh, eh, sf, ef) + pre + fragment + post
}

func htmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
