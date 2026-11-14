package main

// Формулы: линейная запись в духе LaTeX (\frac{a}{b}, x^2, \sqrt{x}, \sum_{i=1}^n …),
// своя раскладка и отрисовка, перевод в OMML (Word), MathML (ODT, HTML) и обратно.

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

const eqFont = "Cambria Math"

type eqNode struct {
	K              string // row id num op fn text frac sup sub subsup sqrt nary delim acc space
	T              string
	Kids           []*eqNode
	Base, Sub, Sup *eqNode
	Num, Den       *eqNode
	Body           *eqNode
	Deg            *eqNode
	Open, Close    string
	Rows           [][]*eqNode // matrix: строки × ячейки
	Cols           string      // matrix: выравнивание столбцов (l c r), повторяется
	Sty            string      // id: b (полужирный), bb (ℝ), cal, rm
}

// eqCell — признак «читаем ячейку окружения» (до &, \\ или \end).
const eqCell rune = 2

var eqSymbols = map[string]string{
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε", "zeta": "ζ", "eta": "η",
	"theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π",
	"rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ", "phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π", "Sigma": "Σ", "Phi": "Φ", "Psi": "Ψ",
	"Omega": "Ω", "infty": "∞", "partial": "∂", "nabla": "∇", "degree": "°", "hbar": "ℏ", "ell": "ℓ", "emptyset": "∅",
	"forall": "∀", "exists": "∃", "angle": "∠", "triangle": "△", "prime": "′", "dots": "…", "ldots": "…", "cdots": "⋯",
	"vdots": "⋮", "ddots": "⋱", "dotsc": "…", "dotsb": "⋯", "aleph": "ℵ", "Re": "ℜ", "Im": "ℑ", "wp": "℘", "nexists": "∄",
	"varnothing": "∅", "Upsilon": "Υ", "varrho": "ϱ", "varsigma": "ς", "varkappa": "ϰ", "varpi": "ϖ", "complement": "∁",
	"langle": "⟨", "rangle": "⟩", "lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉", "square": "□", "blacksquare": "■",
	"lbrace": "{", "rbrace": "}", "backslash": "∖", "top": "⊤", "bot": "⊥", "flat": "♭", "sharp": "♯", "checkmark": "✓",
}

var eqOps = map[string]string{
	"cdot": "·", "times": "×", "div": "÷", "pm": "±", "mp": "∓", "le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "ne": "≠",
	"neq": "≠", "approx": "≈", "equiv": "≡", "sim": "∼", "propto": "∝", "to": "→", "rightarrow": "→", "leftarrow": "←",
	"Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "leftrightarrow": "↔", "in": "∈", "notin": "∉",
	"subset": "⊂", "supset": "⊃", "subseteq": "⊆", "cup": "∪", "cap": "∩", "land": "∧", "lor": "∨", "neg": "¬",
	"perp": "⊥", "parallel": "∥", "ll": "≪", "gg": "≫", "circ": "∘", "ast": "∗", "mid": "∣",
	"iff": "⟺", "implies": "⟹", "impliedby": "⟸", "mapsto": "↦", "longrightarrow": "⟶", "longleftarrow": "⟵",
	"Longrightarrow": "⟹", "Longleftarrow": "⟸", "Longleftrightarrow": "⟺", "longmapsto": "⟼", "uparrow": "↑",
	"downarrow": "↓", "Uparrow": "⇑", "Downarrow": "⇓", "updownarrow": "↕", "nearrow": "↗", "searrow": "↘",
	"rightleftharpoons": "⇌", "hookrightarrow": "↪", "gets": "←", "ni": "∋", "owns": "∋", "supseteq": "⊇",
	"subsetneq": "⊊", "supsetneq": "⊋", "nsubseteq": "⊈", "setminus": "∖", "cong": "≅", "simeq": "≃", "asymp": "≍",
	"doteq": "≐", "nleq": "≰", "ngeq": "≱", "leqslant": "⩽", "geqslant": "⩾", "nless": "≮", "ngtr": "≯", "prec": "≺",
	"succ": "≻", "nmid": "∤", "oplus": "⊕", "ominus": "⊖", "otimes": "⊗", "odot": "⊙", "bullet": "•", "star": "⋆",
	"wedge": "∧", "vee": "∨", "sqcup": "⊔", "sqcap": "⊓", "uplus": "⊎", "dagger": "†", "lnot": "¬", "vdash": "⊢",
	"models": "⊨", "therefore": "∴", "because": "∵", "colon": ":", "approxeq": "≊", "coloneqq": "≔", "triangleq": "≜",
	"leftrightarrows": "⇆", "rightrightarrows": "⇉", "Rrightarrow": "⇛", "lessgtr": "≶",
}

var eqNary = map[string]string{"sum": "∑", "prod": "∏", "int": "∫", "iint": "∬", "iiint": "∭", "oint": "∮", "coprod": "∐", "bigcup": "⋃", "bigcap": "⋂"}

var eqFuncs = map[string]bool{"sin": true, "cos": true, "tan": true, "tg": true, "cot": true, "ctg": true, "sec": true, "csc": true,
	"arcsin": true, "arccos": true, "arctan": true, "arctg": true, "arcctg": true, "sinh": true, "cosh": true, "tanh": true,
	"sh": true, "ch": true, "th": true, "log": true, "ln": true, "lg": true, "exp": true, "lim": true, "max": true, "min": true,
	"sup": true, "inf": true, "det": true, "dim": true, "deg": true, "gcd": true, "arg": true, "mod": true}

var eqAccents = map[string]string{"vec": "→", "bar": "¯", "overline": "¯", "hat": "^", "widehat": "^", "tilde": "~", "widetilde": "~",
	"dot": "˙", "ddot": "¨", "dddot": "⃛", "check": "ˇ", "breve": "˘", "acute": "´", "grave": "`", "overrightarrow": "→",
	"overleftarrow": "←", "underline": "_", "mathring": "˚"}

const eqOpChars = "+-=<>±×÷·≤≥≠≈≡∼→←⇒⇔↔∈∉⊂⊃⊆∪∩∧∨¬⊥∥,;:!∘∗∣/"

// ---------------------------------------------------------------- разбор
type eqParser struct {
	s         []rune
	i         int
	lastRight string
	gotRight  bool
}

func ParseEq(src string) *eqNode {
	p := &eqParser{s: []rune(src)}
	if topLevelAlign(p.s) { // a &= b \\ &= c — многострочный вывод без \begin
		return &eqNode{K: "row", Kids: []*eqNode{p.env("aligned")}}
	}
	return p.row(0)
}

// topLevelAlign — есть ли & или \\ вне скобок и окружений.
func topLevelAlign(s []rune) bool {
	depth, env := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case '&':
			if depth == 0 && env == 0 {
				return true
			}
		case '\\':
			if i+1 < len(s) && s[i+1] == '\\' {
				if depth == 0 && env == 0 {
					return true
				}
				i++
			} else if hasWord(s, i+1, "begin") {
				env++
			} else if hasWord(s, i+1, "end") {
				env--
			}
		}
	}
	return false
}

func hasWord(s []rune, i int, w string) bool {
	rw := []rune(w)
	if i+len(rw) > len(s) {
		return false
	}
	for j, r := range rw {
		if s[i+j] != r {
			return false
		}
	}
	return i+len(rw) == len(s) || !unicode.IsLetter(s[i+len(rw)])
}

// envName — {имя} после \begin / \end.
func (p *eqParser) envName() string {
	p.skipSpace()
	if p.peek() != '{' {
		return ""
	}
	j := p.i + 1
	for p.i < len(p.s) && p.s[p.i] != '}' {
		p.i++
	}
	name := string(p.s[j:min(p.i, len(p.s))])
	if p.i < len(p.s) {
		p.i++
	}
	return strings.TrimSpace(name)
}

// env — окружение (matrix, pmatrix, cases, aligned…) до \end или конца строки.
func (p *eqParser) env(name string) *eqNode {
	m := &eqNode{K: "matrix", T: "matrix", Cols: "c"}
	open, cl := "", ""
	switch strings.TrimSuffix(name, "*") {
	case "pmatrix":
		open, cl = "(", ")"
	case "bmatrix":
		open, cl = "[", "]"
	case "Bmatrix":
		open, cl = "{", "}"
	case "vmatrix":
		open, cl = "|", "|"
	case "Vmatrix":
		open, cl = "‖", "‖"
	case "smallmatrix":
		m.T = "small"
	case "cases", "system", "sistema":
		m.T, m.Cols, open = "cases", "l", "{"
	case "rcases":
		m.T, m.Cols, cl = "cases", "l", "}"
	case "aligned", "align", "split", "eqnarray", "alignat", "alignedat", "flalign":
		m.T, m.Cols = "aligned", "rl"
		if strings.HasPrefix(name, "alignat") {
			p.envName() // число пар столбцов
		}
	case "gathered", "gather", "multline":
		m.T = "gathered"
	case "array", "tabular":
		spec := p.envName()
		m.Cols = ""
		for _, r := range spec {
			if r == 'l' || r == 'c' || r == 'r' {
				m.Cols += string(r)
			}
		}
		if m.Cols == "" {
			m.Cols = "c"
		}
	}
	var row []*eqNode
	for {
		cell := p.row(eqCell)
		row = append(row, cell)
		if p.i >= len(p.s) {
			break
		}
		if p.s[p.i] == '&' {
			p.i++
			continue
		}
		if p.s[p.i] == '\\' && p.i+1 < len(p.s) && p.s[p.i+1] == '\\' {
			p.i += 2
			p.skipSpace()
			if p.peek() == '[' { // \\[2mm]
				for p.i < len(p.s) && p.s[p.i] != ']' {
					p.i++
				}
				p.i++
			}
			m.Rows = append(m.Rows, row)
			row = nil
			continue
		}
		// \end{…}
		p.i++
		p.command()
		p.envName()
		break
	}
	if len(row) > 0 && !(len(row) == 1 && len(row[0].Kids) == 0 && len(m.Rows) > 0) {
		m.Rows = append(m.Rows, row)
	}
	if len(m.Rows) == 0 {
		m.Rows = [][]*eqNode{{{K: "row"}}}
	}
	if open != "" || cl != "" {
		return &eqNode{K: "delim", Open: open, Close: cl, Body: &eqNode{K: "row", Kids: []*eqNode{m}}}
	}
	return m
}

func (p *eqParser) peek() rune {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *eqParser) skipSpace() {
	for p.i < len(p.s) && unicode.IsSpace(p.s[p.i]) {
		p.i++
	}
}

func (p *eqParser) command() string {
	j := p.i
	for p.i < len(p.s) && unicode.IsLetter(p.s[p.i]) {
		p.i++
	}
	if p.i == j && p.i < len(p.s) { // \, \; \{ \}
		p.i++
	}
	return string(p.s[j:p.i])
}

// row читает последовательность до закрывающего символа end (0 — до конца).
func (p *eqParser) row(end rune) *eqNode {
	r := &eqNode{K: "row"}
	for p.i < len(p.s) {
		c := p.peek()
		if end == eqCell {
			if c == '&' || (c == '\\' && p.i+1 < len(p.s) && p.s[p.i+1] == '\\') || (c == '\\' && hasWord(p.s, p.i+1, "end")) {
				return r
			}
		}
		if c == '}' && end == '}' {
			p.i++
			return r
		}
		if end != 0 && end != '}' && end != eqCell && c == end {
			p.i++
			return r
		}
		if c == '}' || ((c == ')' || c == ']') && (end == 0 || end == eqCell)) {
			p.i++ // лишняя скобка — пропускаем
			continue
		}
		if unicode.IsSpace(c) {
			p.i++
			continue
		}
		if c == '\\' {
			save := p.i
			p.i++
			name := p.command()
			if name == "right" {
				p.lastRight, p.gotRight = p.delimTok(), true
				if end != 0 && end != '}' {
					return r
				}
				continue
			}
			p.i = save
		}
		it := p.item()
		if it == nil {
			continue
		}
		p.skipSpace()
		if p.peek() == '/' {
			p.i++
			p.skipSpace()
			den := p.item()
			if den == nil {
				den = &eqNode{K: "row"}
			}
			it = &eqNode{K: "frac", Num: unwrapDelim(it), Den: unwrapDelim(den)}
		}
		if it.K == "nary" && it.Body == nil {
			p.skipSpace()
			if p.i < len(p.s) && !strings.ContainsRune("})]", p.peek()) {
				it.Body = p.item()
			}
		}
		r.Kids = append(r.Kids, it)
	}
	return r
}

func unwrapDelim(n *eqNode) *eqNode {
	if n.K == "delim" && n.Open == "(" && n.Close == ")" {
		return n.Body
	}
	return n
}

func (p *eqParser) arg() *eqNode {
	p.skipSpace()
	if p.peek() == '{' {
		p.i++
		return p.row('}')
	}
	a := p.atom()
	if a == nil {
		return &eqNode{K: "row"}
	}
	return a
}

func (p *eqParser) item() *eqNode {
	a := p.atom()
	if a == nil {
		return nil
	}
	for {
		p.skipSpace()
		c := p.peek()
		if c != '^' && c != '_' {
			break
		}
		p.i++
		s := p.arg()
		if a.K == "nary" {
			if c == '^' {
				a.Sup = s
			} else {
				a.Sub = s
			}
			continue
		}
		if c == '_' && a.K == "fn" && (a.T == "lim" || a.T == "max" || a.T == "min" || a.T == "sup" || a.T == "inf") {
			a = &eqNode{K: "limlow", Base: a, Sub: s}
			continue
		}
		switch {
		case c == '^' && a.K == "sub":
			a = &eqNode{K: "subsup", Base: a.Base, Sub: a.Sub, Sup: s}
		case c == '_' && a.K == "sup":
			a = &eqNode{K: "subsup", Base: a.Base, Sub: s, Sup: a.Sup}
		case c == '^':
			a = &eqNode{K: "sup", Base: a, Sup: s}
		default:
			a = &eqNode{K: "sub", Base: a, Sub: s}
		}
	}
	return a
}

var closeOf = map[rune]rune{'(': ')', '[': ']', '{': '}', '|': '|'}

var closeDelim = map[string]string{"(": ")", "[": "]", "{": "}", "|": "|", "‖": "‖", "⟨": "⟩", "⌊": "⌋", "⌈": "⌉", "": ""}

var eqIgnore = map[string]bool{"displaystyle": true, "textstyle": true, "scriptstyle": true, "limits": true, "nolimits": true,
	"big": true, "Big": true, "bigg": true, "Bigg": true, "bigl": true, "bigr": true, "Bigl": true, "Bigr": true, "biggl": true,
	"biggr": true, "left": false, "nonumber": true, "notag": true, "label": false, "qquad": false}

// delimTok — скобка после \left / \right: ( [ \{ | \langle . …
func (p *eqParser) delimTok() string {
	p.skipSpace()
	if p.i >= len(p.s) {
		return ""
	}
	c := p.s[p.i]
	p.i++
	if c == '.' {
		return ""
	}
	if c != '\\' {
		if c == '<' {
			return "⟨"
		}
		if c == '>' {
			return "⟩"
		}
		return string(c)
	}
	name := p.command()
	if v, ok := map[string]string{"{": "{", "}": "}", "lbrace": "{", "rbrace": "}", "langle": "⟨", "rangle": "⟩", "|": "‖", "Vert": "‖",
		"lVert": "‖", "rVert": "‖", "vert": "|", "lvert": "|", "rvert": "|", "lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉"}[name]; ok {
		return v
	}
	return ""
}

// eqStyle — начертание букв (\mathbb, \mathbf…).
func eqStyle(n *eqNode, sty string) {
	if n == nil {
		return
	}
	if n.K == "id" || n.K == "num" {
		n.Sty = sty
		if sty == "bb" || sty == "cal" {
			n.T = mathAlpha(n.T, sty)
		}
	}
	for _, k := range n.Kids {
		eqStyle(k, sty)
	}
	for _, k := range []*eqNode{n.Base, n.Sub, n.Sup, n.Num, n.Den, n.Body} {
		eqStyle(k, sty)
	}
}

// mathAlpha — буквы ℝ, ℕ… и рукописные 𝒜.
func mathAlpha(s, sty string) string {
	var sb strings.Builder
	for _, r := range s {
		switch sty {
		case "bb":
			if v, ok := map[rune]rune{'C': 'ℂ', 'H': 'ℍ', 'N': 'ℕ', 'P': 'ℙ', 'Q': 'ℚ', 'R': 'ℝ', 'Z': 'ℤ'}[r]; ok {
				r = v
			} else if r >= 'A' && r <= 'Z' {
				r = 0x1D538 + (r - 'A')
			} else if r >= 'a' && r <= 'z' {
				r = 0x1D552 + (r - 'a')
			} else if r >= '0' && r <= '9' {
				r = 0x1D7D8 + (r - '0')
			}
		case "cal":
			if v, ok := map[rune]rune{'B': 'ℬ', 'E': 'ℰ', 'F': 'ℱ', 'H': 'ℋ', 'I': 'ℐ', 'L': 'ℒ', 'M': 'ℳ', 'R': 'ℛ'}[r]; ok {
				r = v
			} else if r >= 'A' && r <= 'Z' {
				r = 0x1D49C + (r - 'A')
			}
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func (p *eqParser) atom() *eqNode {
	p.skipSpace()
	if p.i >= len(p.s) {
		return nil
	}
	c := p.s[p.i]
	switch {
	case c == '{':
		p.i++
		return p.row('}')
	case c == '(' || c == '[':
		p.i++
		body := p.row(closeOf[c])
		return &eqNode{K: "delim", Open: string(c), Close: string(closeOf[c]), Body: body}
	case c == '\\':
		p.i++
		name := p.command()
		switch {
		case name == "frac" || name == "dfrac" || name == "tfrac":
			return &eqNode{K: "frac", Num: p.arg(), Den: p.arg()}
		case name == "sqrt":
			n := &eqNode{K: "sqrt"}
			p.skipSpace()
			if p.peek() == '[' {
				p.i++
				n.Deg = p.row(']')
			}
			n.Body = p.arg()
			return n
		case name == "left":
			o := p.delimTok()
			p.gotRight = false
			body := p.row(1) // до \right
			cl := closeDelim[o]
			if p.gotRight {
				cl = p.lastRight
			}
			p.gotRight = false
			return &eqNode{K: "delim", Open: o, Close: cl, Body: body}
		case name == "begin":
			return p.env(p.envName())
		case name == "end":
			p.envName()
			return nil
		case name == "tag":
			t := p.arg()
			return &eqNode{K: "tag", T: eqPlain(t)}
		case name == "binom" || name == "dbinom" || name == "tbinom":
			f := &eqNode{K: "frac", T: "noline", Num: p.arg(), Den: p.arg()}
			return &eqNode{K: "delim", Open: "(", Close: ")", Body: &eqNode{K: "row", Kids: []*eqNode{f}}}
		case name == "overset" || name == "stackrel":
			top := p.arg()
			return &eqNode{K: "over", Sup: top, Base: p.arg()}
		case name == "underset":
			bot := p.arg()
			return &eqNode{K: "under", Sub: bot, Base: p.arg()}
		case name == "overbrace" || name == "underbrace":
			n := &eqNode{K: "brace", T: strings.TrimSuffix(name, "brace"), Body: p.arg()}
			p.skipSpace()
			if c := p.peek(); (c == '^' && n.T == "over") || (c == '_' && n.T == "under") {
				p.i++
				n.Sub = p.arg()
			}
			return n
		case name == "mathbb" || name == "mathbf" || name == "boldsymbol" || name == "mathcal" || name == "mathit" || name == "bm" || name == "mathup":
			body := p.arg()
			sty := map[string]string{"mathbb": "bb", "mathbf": "b", "boldsymbol": "b", "bm": "b", "mathcal": "cal", "mathit": "it", "mathup": "rm"}[name]
			eqStyle(body, sty)
			return body
		case name == "dv" || name == "pdv" || name == "odv":
			d := "d"
			if name == "pdv" {
				d = "∂"
			}
			p.skipSpace()
			var ord *eqNode
			if p.peek() == '[' {
				p.i++
				ord = p.row(']')
			}
			f := p.arg()
			x := p.arg()
			dn := &eqNode{K: "id", T: d, Sty: "rm"}
			var num, den *eqNode
			if ord != nil && len(ord.Kids) > 0 {
				num = &eqNode{K: "row", Kids: []*eqNode{{K: "sup", Base: dn, Sup: ord}, f}}
				den = &eqNode{K: "row", Kids: []*eqNode{dn, {K: "sup", Base: x, Sup: ord}}}
			} else {
				num = &eqNode{K: "row", Kids: []*eqNode{dn, f}}
				den = &eqNode{K: "row", Kids: []*eqNode{dn, x}}
			}
			return &eqNode{K: "frac", Num: num, Den: den}
		case name == "dd" || name == "diff":
			return &eqNode{K: "id", T: "d", Sty: "rm"}
		case name == "not":
			p.skipSpace()
			nx := p.atom()
			if nx != nil {
				if v, ok := map[string]string{"=": "≠", "∈": "∉", "⊂": "⊄", "⊆": "⊈", "≡": "≢", "∣": "∤", "∼": "≁", "<": "≮", ">": "≯"}[nx.T]; ok {
					return &eqNode{K: "op", T: v}
				}
				return nx
			}
			return nil
		case name == "|" || name == "Vert" || name == "lVert" || name == "rVert":
			return &eqNode{K: "op", T: "‖"}
		case eqIgnore[name]:
			return nil
		case name == "text" || name == "mathrm" || name == "textrm" || name == "operatorname":
			p.skipSpace()
			if p.peek() == '{' {
				p.i++
				j := p.i
				depth := 1
				for p.i < len(p.s) && depth > 0 {
					if p.s[p.i] == '{' {
						depth++
					} else if p.s[p.i] == '}' {
						depth--
					}
					p.i++
				}
				t := string(p.s[j : p.i-1])
				if name == "operatorname" {
					return &eqNode{K: "fn", T: t}
				}
				return &eqNode{K: "text", T: t}
			}
			return nil
		case eqNary[name] != "":
			return &eqNode{K: "nary", T: eqNary[name]}
		case eqAccents[name] != "":
			return &eqNode{K: "acc", T: eqAccents[name], Body: p.arg()}
		case eqFuncs[name]:
			return &eqNode{K: "fn", T: name}
		case eqSymbols[name] != "":
			return &eqNode{K: "id", T: eqSymbols[name]}
		case eqOps[name] != "":
			return &eqNode{K: "op", T: eqOps[name]}
		case name == "," || name == ";" || name == "quad" || name == " ":
			return &eqNode{K: "space", T: name}
		case name == "{" || name == "}":
			return &eqNode{K: "op", T: name}
		}
		return &eqNode{K: "text", T: "\\" + name}
	case unicode.IsDigit(c) || (c == '.' && p.i+1 < len(p.s) && unicode.IsDigit(p.s[p.i+1])):
		j := p.i
		for p.i < len(p.s) && (unicode.IsDigit(p.s[p.i]) || ((p.s[p.i] == '.' || p.s[p.i] == ',') && p.i+1 < len(p.s) && unicode.IsDigit(p.s[p.i+1]))) {
			p.i++
		}
		return &eqNode{K: "num", T: string(p.s[j:p.i])}
	case unicode.IsLetter(c):
		// подряд идущие латинские буквы, совпадающие с функцией, — функция (sin x)
		j := p.i
		for j < len(p.s) && unicode.IsLetter(p.s[j]) && p.s[j] < 128 {
			j++
		}
		if w := string(p.s[p.i:j]); eqFuncs[w] && j-p.i > 1 {
			p.i = j
			return &eqNode{K: "fn", T: w}
		}
		p.i++
		for k, v := range eqNary {
			_ = k
			if string(c) == v {
				return &eqNode{K: "nary", T: v}
			}
		}
		return &eqNode{K: "id", T: string(c)}
	case strings.ContainsRune("∑∏∫∬∭∮∐⋃⋂", c):
		p.i++
		return &eqNode{K: "nary", T: string(c)}
	case c == '√':
		p.i++
		return &eqNode{K: "sqrt", Body: p.arg()}
	case c == '\'':
		p.i++
		return &eqNode{K: "id", T: "′"}
	case strings.ContainsRune(eqOpChars, c) || c == '*':
		p.i++
		t := string(c)
		if c == '*' {
			t = "·"
		}
		if c == '-' {
			t = "−"
		}
		if c == '<' && p.peek() == '=' {
			p.i++
			t = "≤"
		} else if c == '>' && p.peek() == '=' {
			p.i++
			t = "≥"
		} else if c == '!' && p.peek() == '=' {
			p.i++
			t = "≠"
		} else if c == '-' && p.peek() == '>' {
			p.i++
			t = "→"
		}
		return &eqNode{K: "op", T: t}
	}
	p.i++
	return &eqNode{K: "id", T: string(c)}
}

// ---------------------------------------------------------------- раскладка
type eqOp struct {
	K          int // 0 текст, 1 линия
	X, Y       float64
	S          string
	Font       Font
	X2, Y2, LW float64
}

type EqBox struct {
	W, Asc, Desc float64
	Ops          []eqOp
}

func (b *EqBox) put(c *EqBox, dx, dy float64) {
	for _, o := range c.Ops {
		o.X += dx
		o.Y += dy
		o.X2 += dx
		o.Y2 += dy
		b.Ops = append(b.Ops, o)
	}
}

func (m *Measurer) EqLayout(src string, f F) *EqBox {
	size := f.Size
	if size <= 0 {
		size = 11
	}
	key := "\x02eq" + src + "|" + fmt.Sprint(size, f.B)
	if m.eq == nil {
		m.eq = map[string]*EqBox{}
	}
	if b, ok := m.eq[key]; ok {
		return b
	}
	b := m.eqLay(ParseEq(src), size, 0)
	if b.W == 0 {
		b = m.eqText("□", size, false)
	}
	m.eq[key] = b
	return b
}

func (m *Measurer) eqText(s string, size float64, italic bool) *EqBox {
	return m.eqTextB(s, size, italic, false)
}

func (m *Measurer) eqTextB(s string, size float64, italic, bold bool) *EqBox {
	font := Font{Family: eqFont, Px: size, Italic: italic, Bold: bold}
	w := m.Width(s, font)
	if italic {
		w += size * 0.04
	}
	return &EqBox{W: w, Asc: size * 0.72, Desc: size * 0.22, Ops: []eqOp{{K: 0, S: s, Font: font}}}
}

func (m *Measurer) eqLay(n *eqNode, size float64, lvl int) *EqBox {
	if n == nil {
		return &EqBox{}
	}
	axis := size * 0.25
	switch n.K {
	case "row":
		b := &EqBox{}
		for i, k := range n.Kids {
			c := m.eqLay(k, size, lvl)
			gap := 0.0
			unary := (k.T == "−" || k.T == "+" || k.T == "±") && (i == 0 || n.Kids[i-1].K == "op")
			if k.K == "op" && !unary && k.T != "," && k.T != "/" && k.T != "!" && k.T != "′" {
				gap = size * 0.2
			}
			if k.K == "op" && (k.T == "," || k.T == ";") {
				b.put(c, b.W, 0)
				b.W += c.W + size*0.15
			} else {
				b.put(c, b.W+gap, 0)
				b.W += c.W + 2*gap
			}
			if k.K == "fn" && i+1 < len(n.Kids) {
				b.W += size * 0.15
			}
			b.Asc, b.Desc = math.Max(b.Asc, c.Asc), math.Max(b.Desc, c.Desc)
		}
		return b
	case "id":
		it := n.Sty != "b" && n.Sty != "bb" && n.Sty != "cal" && n.Sty != "rm"
		for _, r := range n.T {
			if !unicode.IsLetter(r) || unicode.Is(unicode.Greek, r) && unicode.IsUpper(r) {
				it = false
			}
		}
		return m.eqTextB(n.T, size, it, n.Sty == "b")
	case "num", "op":
		return m.eqTextB(n.T, size, false, n.Sty == "b")
	case "tag":
		b := m.eqText("("+n.T+")", size, false)
		b.Ops[0].X += size * 1.5
		b.W += size * 1.5
		return b
	case "matrix":
		return m.eqMatrix(n, size, lvl)
	case "over", "under":
		base := m.eqLay(n.Base, size, lvl)
		lim := n.Sup
		if n.K == "under" {
			lim = n.Sub
		}
		lb := m.eqLay(lim, math.Max(size*0.7, 5), lvl+1)
		w := math.Max(base.W, lb.W)
		b := &EqBox{W: w + size*0.06}
		b.put(base, (w-base.W)/2, 0)
		if n.K == "over" {
			y := -base.Asc - size*0.08 - lb.Desc
			b.put(lb, (w-lb.W)/2, y)
			b.Asc, b.Desc = -y+lb.Asc, base.Desc
		} else {
			y := base.Desc + size*0.06 + lb.Asc
			b.put(lb, (w-lb.W)/2, y)
			b.Asc, b.Desc = base.Asc, y+lb.Desc
		}
		return b
	case "brace":
		c := m.eqLay(n.Body, size, lvl)
		b := &EqBox{W: c.W, Asc: c.Asc, Desc: c.Desc}
		b.put(c, 0, 0)
		t := math.Max(0.5, size*0.05)
		bh := size * 0.32
		var y0, y1 float64 // y0 — край у формулы, y1 — острие
		if n.T == "over" {
			y0 = -c.Asc - size*0.08
			y1 = y0 - bh
		} else {
			y0 = c.Desc + size*0.08
			y1 = y0 + bh
		}
		ym := (y0 + y1) / 2
		w := c.W
		seg := func(x1, ya, x2, yb float64) { b.Ops = append(b.Ops, eqOp{K: 1, X: x1, Y: ya, X2: x2, Y2: yb, LW: t}) }
		seg(0, y0, w*0.06, ym)
		seg(w*0.06, ym, w*0.44, ym)
		seg(w*0.44, ym, w*0.5, y1)
		seg(w*0.5, y1, w*0.56, ym)
		seg(w*0.56, ym, w*0.94, ym)
		seg(w*0.94, ym, w, y0)
		if n.T == "over" {
			b.Asc = -y1
		} else {
			b.Desc = y1
		}
		if n.Sub != nil {
			lb := m.eqLay(n.Sub, math.Max(size*0.7, 5), lvl+1)
			if n.T == "over" {
				y := y1 - size*0.08 - lb.Desc
				b.put(lb, (w-lb.W)/2, y)
				b.Asc = -y + lb.Asc
			} else {
				y := y1 + size*0.06 + lb.Asc
				b.put(lb, (w-lb.W)/2, y)
				b.Desc = y + lb.Desc
			}
			if lb.W > w {
				// подпись шире — сдвигаем всё вправо
				d := (lb.W - w) / 2
				for i := range b.Ops {
					b.Ops[i].X += d
					b.Ops[i].X2 += d
				}
				b.W = lb.W
			}
		}
		return b
	case "fn", "text":
		return m.eqText(n.T, size, false)
	case "space":
		w := size * 0.17
		if n.T == ";" {
			w = size * 0.28
		} else if n.T == "quad" {
			w = size
		}
		return &EqBox{W: w}
	case "frac":
		sc := 0.9
		if lvl > 0 {
			sc = 0.78
		}
		s := math.Max(size*sc, 5)
		nb, db := m.eqLay(n.Num, s, lvl+1), m.eqLay(n.Den, s, lvl+1)
		t := math.Max(0.5, size*0.055)
		gap := size * 0.12
		pad := size * 0.12
		w := math.Max(nb.W, db.W) + 2*pad
		b := &EqBox{W: w + size*0.08}
		ny := -(axis + t/2 + gap + nb.Desc)
		dy := -axis + t/2 + gap + db.Asc
		b.put(nb, size*0.04+(w-nb.W)/2, ny)
		b.put(db, size*0.04+(w-db.W)/2, dy)
		if n.T != "noline" {
			b.Ops = append(b.Ops, eqOp{K: 1, X: size * 0.04, Y: -axis, X2: size*0.04 + w, Y2: -axis, LW: t})
		}
		b.Asc = -ny + nb.Asc
		b.Desc = dy + db.Desc
		return b
	case "sup", "sub", "subsup":
		base := m.eqLay(n.Base, size, lvl)
		s := math.Max(size*0.7, 5)
		b := &EqBox{}
		b.put(base, 0, 0)
		b.Asc, b.Desc = base.Asc, base.Desc
		x := base.W + size*0.03
		w := 0.0
		if n.Sup != nil {
			sb := m.eqLay(n.Sup, s, lvl+1)
			up := math.Max(size*0.38, base.Asc-sb.Asc*0.45)
			b.put(sb, x, -up)
			b.Asc = math.Max(b.Asc, up+sb.Asc)
			w = sb.W
		}
		if n.Sub != nil {
			sb := m.eqLay(n.Sub, s, lvl+1)
			down := math.Max(size*0.18, base.Desc+sb.Asc*0.35)
			if n.Sup == nil {
				down = size * 0.2
			}
			b.put(sb, x, down)
			b.Desc = math.Max(b.Desc, down+sb.Desc)
			w = math.Max(w, sb.W)
		}
		b.W = x + w + size*0.03
		return b
	case "sqrt":
		c := m.eqLay(n.Body, size, lvl)
		gap := size * 0.12
		t := math.Max(0.5, size*0.05)
		rw := size * 0.6
		b := &EqBox{}
		var dw float64
		if n.Deg != nil {
			db := m.eqLay(n.Deg, math.Max(size*0.5, 5), lvl+2)
			dw = math.Max(0, db.W-rw*0.45)
			b.put(db, 0, -size*0.45)
		}
		x0 := dw
		yt := -(c.Asc + gap + t/2)
		yb := c.Desc
		b.Ops = append(b.Ops,
			eqOp{K: 1, X: x0 + rw*0.05, Y: -axis * 0.8, X2: x0 + rw*0.3, Y2: -axis * 1.1, LW: t},
			eqOp{K: 1, X: x0 + rw*0.3, Y: -axis * 1.1, X2: x0 + rw*0.58, Y2: yb, LW: t * 1.8},
			eqOp{K: 1, X: x0 + rw*0.58, Y: yb, X2: x0 + rw, Y2: yt, LW: t},
			eqOp{K: 1, X: x0 + rw, Y: yt, X2: x0 + rw + c.W + size*0.08, Y2: yt, LW: t})
		b.put(c, x0+rw+size*0.04, 0)
		b.W = x0 + rw + c.W + size*0.14
		b.Asc = c.Asc + gap + t
		b.Desc = c.Desc + size*0.05
		return b
	case "limlow":
		base := m.eqLay(n.Base, size, lvl)
		sb := m.eqLay(n.Sub, math.Max(size*0.7, 5), lvl+1)
		w := math.Max(base.W, sb.W)
		b := &EqBox{W: w + size*0.1}
		b.put(base, (w-base.W)/2, 0)
		y := base.Desc + size*0.05 + sb.Asc
		b.put(sb, (w-sb.W)/2, y)
		b.Asc, b.Desc = base.Asc, y+sb.Desc
		return b
	case "nary":
		integral := strings.ContainsRune("∫∬∭∮", []rune(n.T)[0])
		S := size * 1.45
		if integral {
			S = size * 1.6
		}
		sym := m.eqText(n.T, S, false)
		symY := S * 0.22
		sym.Asc, sym.Desc = S*0.72-symY, S*0.22+symY
		s := math.Max(size*0.68, 5)
		var sub, sup *EqBox
		if n.Sub != nil {
			sub = m.eqLay(n.Sub, s, lvl+1)
		}
		if n.Sup != nil {
			sup = m.eqLay(n.Sup, s, lvl+1)
		}
		b := &EqBox{}
		if integral {
			b.put(sym, 0, symY)
			b.Asc, b.Desc = sym.Asc, sym.Desc
			x := sym.W
			w := 0.0
			if sup != nil {
				b.put(sup, x, -sym.Asc+sup.Asc*0.6)
				w = sup.W
			}
			if sub != nil {
				b.put(sub, x-size*0.25, sym.Desc-sub.Desc*0.3)
				w = math.Max(w, sub.W-size*0.25)
			}
			b.W = x + w + size*0.12
		} else {
			w := sym.W
			if sub != nil {
				w = math.Max(w, sub.W)
			}
			if sup != nil {
				w = math.Max(w, sup.W)
			}
			b.put(sym, (w-sym.W)/2, symY)
			b.Asc, b.Desc = sym.Asc, sym.Desc
			if sup != nil {
				y := -sym.Asc - size*0.08 - sup.Desc
				b.put(sup, (w-sup.W)/2, y)
				b.Asc = -y + sup.Asc
			}
			if sub != nil {
				y := sym.Desc + size*0.06 + sub.Asc
				b.put(sub, (w-sub.W)/2, y)
				b.Desc = y + sub.Desc
			}
			b.W = w + size*0.12
		}
		if n.Body != nil {
			bb := m.eqLay(n.Body, size, lvl)
			b.put(bb, b.W, 0)
			b.W += bb.W
			b.Asc, b.Desc = math.Max(b.Asc, bb.Asc), math.Max(b.Desc, bb.Desc)
		}
		return b
	case "delim":
		c := m.eqLay(n.Body, size, lvl)
		asc := math.Max(c.Asc, size*0.72) + size*0.04
		desc := math.Max(c.Desc, size*0.22) + size*0.04
		h := asc + desc
		dw := size * 0.3
		if h > size*1.3 {
			dw = size * (0.3 + 0.06*(h/size-1.3))
		}
		t := math.Max(0.5, size*0.06)
		b := &EqBox{Asc: asc, Desc: desc}
		x := 0.0
		if h <= size*1.3 && n.Open != "{" && n.Open != "⟨" && n.Open != "‖" && !(n.Body != nil && len(n.Body.Kids) == 1 && n.Body.Kids[0].K == "matrix") {
			// обычная высота — рисуем символы шрифтом, а не линиями
			if n.Open != "" {
				o := m.eqText(n.Open, size, false)
				b.put(o, 0, 0)
				x += o.W
			}
			b.put(c, x, 0)
			x += c.W
			if n.Close != "" {
				cl := m.eqText(n.Close, size, false)
				b.put(cl, x, 0)
				x += cl.W
			}
			b.W = x
			b.Asc, b.Desc = math.Max(c.Asc, size*0.72), math.Max(c.Desc, size*0.22)
			return b
		}
		if n.Open != "" {
			b.Ops = append(b.Ops, delimOps(n.Open, x, -asc, dw, h, t, false)...)
			x += dw + size*0.04
		}
		b.put(c, x, 0)
		x += c.W + size*0.04
		if n.Close != "" {
			b.Ops = append(b.Ops, delimOps(n.Close, x, -asc, dw, h, t, true)...)
			x += dw
		}
		b.W = x
		return b
	case "acc":
		c := m.eqLay(n.Body, size, lvl)
		b := &EqBox{W: c.W, Desc: c.Desc}
		b.put(c, 0, 0)
		y := -c.Asc - size*0.12
		t := math.Max(0.5, size*0.05)
		switch n.T {
		case "→":
			b.Ops = append(b.Ops, eqOp{K: 1, X: size * 0.05, Y: y, X2: c.W, Y2: y, LW: t},
				eqOp{K: 1, X: c.W - size*0.18, Y: y - size*0.1, X2: c.W, Y2: y, LW: t},
				eqOp{K: 1, X: c.W - size*0.18, Y: y + size*0.1, X2: c.W, Y2: y, LW: t})
		case "¯":
			b.Ops = append(b.Ops, eqOp{K: 1, X: size * 0.05, Y: y, X2: c.W, Y2: y, LW: t})
		case "_":
			yb := c.Desc + size*0.1
			b.Ops = append(b.Ops, eqOp{K: 1, X: size * 0.05, Y: yb, X2: c.W, Y2: yb, LW: t})
			b.Asc, b.Desc = c.Asc, yb+t
			return b
		case "←":
			b.Ops = append(b.Ops, eqOp{K: 1, X: size * 0.05, Y: y, X2: c.W, Y2: y, LW: t},
				eqOp{K: 1, X: size*0.05 + size*0.18, Y: y - size*0.1, X2: size * 0.05, Y2: y, LW: t},
				eqOp{K: 1, X: size*0.05 + size*0.18, Y: y + size*0.1, X2: size * 0.05, Y2: y, LW: t})
		case "^":
			b.Ops = append(b.Ops, eqOp{K: 1, X: c.W*0.2 + size*0.05, Y: y, X2: c.W*0.5 + size*0.05, Y2: y - size*0.15, LW: t},
				eqOp{K: 1, X: c.W*0.5 + size*0.05, Y: y - size*0.15, X2: c.W*0.8 + size*0.05, Y2: y, LW: t})
		default:
			ac := m.eqText(n.T, size, false)
			b.put(ac, (c.W-ac.W)/2+size*0.05, y+size*0.3)
		}
		b.Asc = -y + size*0.2
		return b
	}
	return &EqBox{}
}

// eqMatrix — матрица, система, многострочный вывод.
func (m *Measurer) eqMatrix(n *eqNode, size float64, lvl int) *EqBox {
	axis := size * 0.25
	s := size
	if n.T == "small" {
		s = math.Max(size*0.7, 5)
	}
	nc := 0
	for _, r := range n.Rows {
		nc = max(nc, len(r))
	}
	cells := make([][]*EqBox, len(n.Rows))
	colW := make([]float64, nc)
	rowA := make([]float64, len(n.Rows))
	rowD := make([]float64, len(n.Rows))
	for i, r := range n.Rows {
		rowA[i], rowD[i] = s*0.72, s*0.22
		for j, c := range r {
			b := m.eqLay(c, s, lvl)
			cells[i] = append(cells[i], b)
			colW[j] = math.Max(colW[j], b.W)
			rowA[i], rowD[i] = math.Max(rowA[i], b.Asc), math.Max(rowD[i], b.Desc)
		}
	}
	colGap := s * 0.9
	rowGap := s * 0.32
	switch n.T {
	case "cases":
		colGap = s * 1.2
		rowGap = s * 0.25
	case "aligned":
		rowGap = s * 0.38
	case "small":
		colGap = s * 0.6
	}
	xs := make([]float64, nc+1)
	for j := 0; j < nc; j++ {
		g := colGap
		if n.T == "aligned" {
			g = 0
			if j%2 == 1 {
				g = s * 1.6
			}
		}
		xs[j+1] = xs[j] + colW[j]
		if j+1 < nc {
			xs[j+1] += g
		}
	}
	H := 0.0
	for i := range n.Rows {
		H += rowA[i] + rowD[i]
		if i > 0 {
			H += rowGap
		}
	}
	top := -axis - H/2
	pad := s * 0.08
	b := &EqBox{W: xs[nc] + 2*pad, Asc: -top, Desc: top + H}
	y := top
	for i := range n.Rows {
		base := y + rowA[i]
		for j, c := range cells[i] {
			al := byte('c')
			if n.Cols != "" {
				al = n.Cols[j%len(n.Cols)]
			}
			x := xs[j]
			switch al {
			case 'l':
			case 'r':
				x += colW[j] - c.W
			default:
				x += (colW[j] - c.W) / 2
			}
			b.put(c, pad+x, base)
		}
		y = base + rowD[i] + rowGap
	}
	return b
}

// delimOps — скобка как набор отрезков (масштабируется на любую высоту).
func delimOps(ch string, x, top, w, h, t float64, closing bool) []eqOp {
	var ops []eqOp
	seg := func(x1, y1, x2, y2 float64) { ops = append(ops, eqOp{K: 1, X: x1, Y: y1, X2: x2, Y2: y2, LW: t}) }
	flip := func(v float64) float64 {
		if closing {
			return x + w - (v - x)
		}
		return v
	}
	switch ch {
	case "(", ")":
		const n = 12
		px, py := 0.0, 0.0
		for i := 0; i <= n; i++ {
			a := math.Pi * (0.25 + 0.5*float64(i)/n)
			cx := x + w*0.95 - math.Sin(a)*w*0.75
			cy := top + h/2 - math.Cos(a)*h/2*1.35
			cx = flip(cx)
			if i > 0 {
				seg(px, py, cx, cy)
			}
			px, py = cx, cy
		}
	case "[", "]":
		seg(flip(x+w*0.35), top, flip(x+w*0.35), top+h)
		seg(flip(x+w*0.35), top, flip(x+w*0.9), top)
		seg(flip(x+w*0.35), top+h, flip(x+w*0.9), top+h)
	case "{", "}":
		mid := top + h/2
		xm := x + w*0.5
		seg(flip(x+w*0.9), top, flip(xm), top+h*0.08)
		seg(flip(xm), top+h*0.08, flip(xm), mid-h*0.08)
		seg(flip(xm), mid-h*0.08, flip(x+w*0.1), mid)
		seg(flip(x+w*0.1), mid, flip(xm), mid+h*0.08)
		seg(flip(xm), mid+h*0.08, flip(xm), top+h*0.92)
		seg(flip(xm), top+h*0.92, flip(x+w*0.9), top+h)
	case "|":
		seg(x+w/2, top, x+w/2, top+h)
	case "‖":
		seg(x+w*0.3, top, x+w*0.3, top+h)
		seg(x+w*0.7, top, x+w*0.7, top+h)
	case "⟨", "⟩":
		seg(flip(x+w*0.85), top, flip(x+w*0.2), top+h/2)
		seg(flip(x+w*0.2), top+h/2, flip(x+w*0.85), top+h)
	case "⌊", "⌋", "⌈", "⌉":
		seg(flip(x+w*0.35), top, flip(x+w*0.35), top+h)
		if ch == "⌊" || ch == "⌋" {
			seg(flip(x+w*0.35), top+h, flip(x+w*0.9), top+h)
		} else {
			seg(flip(x+w*0.35), top, flip(x+w*0.9), top)
		}
	}
	return ops
}

func (pp *PagePainter) drawEq(b *EqBox, x, base float64, col Color) {
	for _, o := range b.Ops {
		switch o.K {
		case 0:
			pp.text(o.S, x+o.X, base+o.Y, F{}, o.Font, col)
		case 1:
			pp.line(pp.X(x+o.X), pp.Y(base+o.Y), pp.X(x+o.X2), pp.Y(base+o.Y2), math.Max(1, o.LW*pp.k), pp.col(col))
		}
	}
}

// ---------------------------------------------------------------- OMML (Word)
func eqOMML(src string) string { return "<m:oMath>" + omml(ParseEq(src)) + "</m:oMath>" }

func ommlRun(t string, plain bool) string {
	rp := ""
	if plain {
		rp = `<m:rPr><m:sty m:val="p"/></m:rPr>`
	}
	return `<m:r>` + rp + `<w:rPr><w:rFonts w:ascii="Cambria Math" w:hAnsi="Cambria Math"/></w:rPr><m:t xml:space="preserve">` + xesc(t) + `</m:t></m:r>`
}

func omml(n *eqNode) string {
	if n == nil {
		return ""
	}
	e := func(x *eqNode) string { return "<m:e>" + omml(x) + "</m:e>" }
	switch n.K {
	case "row":
		var sb strings.Builder
		for _, k := range n.Kids {
			sb.WriteString(omml(k))
		}
		return sb.String()
	case "id", "num", "op":
		if n.Sty == "b" {
			return `<m:r><m:rPr><m:sty m:val="` + map[bool]string{true: "bi", false: "b"}[n.K == "id"] + `"/></m:rPr><w:rPr><w:rFonts w:ascii="Cambria Math" w:hAnsi="Cambria Math"/><w:b/></w:rPr><m:t xml:space="preserve">` + xesc(n.T) + `</m:t></m:r>`
		}
		return ommlRun(n.T, n.K != "id" || n.Sty == "rm")
	case "tag":
		return ommlRun("    ("+n.T+")", true)
	case "matrix":
		return ommlMatrix(n)
	case "over":
		return "<m:limUpp>" + e(n.Base) + "<m:lim>" + omml(n.Sup) + "</m:lim></m:limUpp>"
	case "under":
		return "<m:limLow>" + e(n.Base) + "<m:lim>" + omml(n.Sub) + "</m:lim></m:limLow>"
	case "brace":
		pos, ch, tag := "top", "⏞", "limUpp"
		if n.T == "under" {
			pos, ch, tag = "bot", "⏟", "limLow"
		}
		g := `<m:groupChr><m:groupChrPr><m:chr m:val="` + ch + `"/><m:pos m:val="` + pos + `"/><m:vertJc m:val="` +
			map[string]string{"top": "bot", "bot": "top"}[pos] + `"/></m:groupChrPr>` + e(n.Body) + `</m:groupChr>`
		if n.Sub == nil {
			return g
		}
		return "<m:" + tag + "><m:e>" + g + "</m:e><m:lim>" + omml(n.Sub) + "</m:lim></m:" + tag + ">"
	case "fn":
		return ommlRun(n.T, true)
	case "text":
		return `<m:r><m:rPr><m:nor/></m:rPr><w:rPr><w:rFonts w:ascii="Cambria Math" w:hAnsi="Cambria Math"/></w:rPr><m:t xml:space="preserve">` + xesc(n.T) + `</m:t></m:r>`
	case "space":
		return ommlRun(eqSpaceChar[n.T], true)
	case "frac":
		if n.T == "noline" {
			return `<m:f><m:fPr><m:type m:val="noBar"/></m:fPr><m:num>` + omml(n.Num) + "</m:num><m:den>" + omml(n.Den) + "</m:den></m:f>"
		}
		return "<m:f><m:num>" + omml(n.Num) + "</m:num><m:den>" + omml(n.Den) + "</m:den></m:f>"
	case "sup":
		return "<m:sSup>" + e(n.Base) + "<m:sup>" + omml(n.Sup) + "</m:sup></m:sSup>"
	case "sub":
		return "<m:sSub>" + e(n.Base) + "<m:sub>" + omml(n.Sub) + "</m:sub></m:sSub>"
	case "subsup":
		return "<m:sSubSup>" + e(n.Base) + "<m:sub>" + omml(n.Sub) + "</m:sub><m:sup>" + omml(n.Sup) + "</m:sup></m:sSubSup>"
	case "limlow":
		return "<m:limLow>" + e(n.Base) + "<m:lim>" + omml(n.Sub) + "</m:lim></m:limLow>"
	case "sqrt":
		if n.Deg != nil {
			return "<m:rad><m:deg>" + omml(n.Deg) + "</m:deg>" + e(n.Body) + "</m:rad>"
		}
		return `<m:rad><m:radPr><m:degHide m:val="1"/></m:radPr><m:deg/>` + e(n.Body) + "</m:rad>"
	case "nary":
		pr := `<m:naryPr><m:chr m:val="` + n.T + `"/>`
		if !strings.ContainsRune("∫∬∭∮", []rune(n.T)[0]) {
			pr += `<m:limLoc m:val="undOvr"/>`
		} else {
			pr += `<m:limLoc m:val="subSup"/>`
		}
		if n.Sub == nil {
			pr += `<m:subHide m:val="1"/>`
		}
		if n.Sup == nil {
			pr += `<m:supHide m:val="1"/>`
		}
		pr += "</m:naryPr>"
		return "<m:nary>" + pr + "<m:sub>" + omml(n.Sub) + "</m:sub><m:sup>" + omml(n.Sup) + "</m:sup>" + e(n.Body) + "</m:nary>"
	case "delim":
		pr := "<m:dPr>"
		if n.Open != "(" {
			pr += `<m:begChr m:val="` + xesc(n.Open) + `"/>`
		}
		if n.Close != ")" {
			pr += `<m:endChr m:val="` + xesc(n.Close) + `"/>`
		}
		pr += "</m:dPr>"
		return "<m:d>" + pr + e(n.Body) + "</m:d>"
	case "acc":
		if n.T == "¯" || n.T == "_" {
			return `<m:bar><m:barPr><m:pos m:val="` + map[bool]string{true: "top", false: "bot"}[n.T == "¯"] + `"/></m:barPr>` + e(n.Body) + "</m:bar>"
		}
		ch := accentComb[n.T]
		return `<m:acc><m:accPr><m:chr m:val="` + ch + `"/></m:accPr>` + e(n.Body) + "</m:acc>"
	}
	return ""
}

// Пробелы формулы при обмене: в OMML — юникодные пробелы своей ширины, в MathML — mspace.
// При чтении они превращаются обратно в \, \; \quad (иначе парсер их просто пропустит).
var eqSpaceChar = map[string]string{",": "\u2009", ";": "\u2005", "quad": "\u2003", " ": " "}
var eqSpaceWidth = map[string]string{",": "0.17em", ";": "0.28em", "quad": "1em", " ": "0.25em"}

var eqSpaceCmd = map[rune]string{'\u2009': "\\,", '\u2005': "\\;", '\u2003': "\\quad "}

// eqTagRun — номер формулы, записанный при экспорте как «    (1.3)».
func eqTagRun(t string) (string, bool) {
	if !strings.HasPrefix(t, "  ") {
		return "", false
	}
	t = strings.TrimSpace(t)
	if len(t) > 2 && t[0] == '(' && t[len(t)-1] == ')' && !strings.ContainsAny(t[1:len(t)-1], "()") {
		return t[1 : len(t)-1], true
	}
	return "", false
}

var accentComb = map[string]string{"→": "⃗", "^": "̂", "~": "̃", "˙": "̇", "¨": "̈", "⃛": "⃛", "ˇ": "̌", "˘": "̆", "´": "́", "`": "̀", "˚": "̊", "←": "⃖"}

// ommlMatrix — матрица (m:m) или многострочный вывод (m:eqArr) для Word.
func ommlMatrix(n *eqNode) string {
	var sb strings.Builder
	if n.T == "aligned" || n.T == "gathered" {
		sb.WriteString(`<m:eqArr>`)
		for _, row := range n.Rows {
			sb.WriteString("<m:e>")
			for j, c := range row {
				x := omml(c)
				if j > 0 && n.T == "aligned" && j%2 == 1 {
					// точка выравнивания — m:aln на первом фрагменте ячейки
					if i := strings.Index(x, "<m:r>"); i >= 0 {
						rest := x[i+5:]
						if strings.HasPrefix(rest, "<m:rPr>") {
							x = x[:i+5] + "<m:rPr><m:aln/>" + rest[7:]
						} else {
							x = x[:i+5] + "<m:rPr><m:aln/></m:rPr>" + rest
						}
					} else {
						x = `<m:r><m:rPr><m:aln/></m:rPr><m:t></m:t></m:r>` + x
					}
				}
				sb.WriteString(x)
			}
			sb.WriteString("</m:e>")
		}
		sb.WriteString(`</m:eqArr>`)
		return sb.String()
	}
	nc := 0
	for _, r := range n.Rows {
		nc = max(nc, len(r))
	}
	sb.WriteString(`<m:m><m:mPr><m:mcs>`)
	for j := 0; j < nc; j++ {
		al := byte('c')
		if n.Cols != "" {
			al = n.Cols[j%len(n.Cols)]
		}
		fmt.Fprintf(&sb, `<m:mc><m:mcPr><m:count m:val="1"/><m:mcJc m:val="%s"/></m:mcPr></m:mc>`, map[byte]string{'l': "left", 'r': "right", 'c': "center"}[al])
	}
	sb.WriteString(`</m:mcs></m:mPr>`)
	for _, row := range n.Rows {
		sb.WriteString("<m:mr>")
		for j := 0; j < nc; j++ {
			sb.WriteString("<m:e>")
			if j < len(row) {
				sb.WriteString(omml(row[j]))
			}
			sb.WriteString("</m:e>")
		}
		sb.WriteString("</m:mr>")
	}
	sb.WriteString(`</m:m>`)
	return sb.String()
}

var linearOf = func() map[string]string {
	m := map[string]string{}
	for k, v := range eqSymbols {
		if _, ok := m[v]; !ok || len(k) < len(m[v]) {
			m[v] = k
		}
	}
	for k, v := range eqOps {
		if _, ok := m[v]; !ok || len(k) < len(m[v]) {
			m[v] = k
		}
	}
	return m
}()

func linearText(t string, fn bool) string {
	if fn && eqFuncs[t] {
		return "\\" + t + " "
	}
	var sb strings.Builder
	for _, r := range t {
		switch {
		case r == '−':
			sb.WriteRune('-')
		case r == '{' || r == '}':
			sb.WriteString("\\" + string(r))
		case eqSpaceCmd[r] != "":
			sb.WriteString(eqSpaceCmd[r])
		default:
			if name, ok := linearOf[string(r)]; ok && r > 127 && !unicode.Is(unicode.Greek, r) {
				sb.WriteString("\\" + name + " ")
			} else {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}

// OMMLToLinear — обратный перевод формулы Word в линейную запись.
func OMMLToLinear(n *Node) string {
	if n == nil {
		return ""
	}
	g := func(name string) string { return "{" + OMMLToLinear(n.Kid(name)) + "}" }
	kids := func() string {
		var sb strings.Builder
		for _, k := range n.Elems() {
			sb.WriteString(OMMLToLinear(k))
		}
		return sb.String()
	}
	val := func(pr, name, def string) string {
		if p := n.Kid(pr); p != nil {
			if c := p.Kid(name); c != nil {
				if v, ok := c.A("val"); ok {
					return v
				}
			}
		}
		return def
	}
	switch n.Name {
	case "r":
		t := ""
		for _, x := range n.KidsNamed("t") {
			t += x.TextContent()
		}
		pre := ""
		if rp := n.Kid("rPr"); rp != nil && rp.Kid("aln") != nil {
			pre = " & "
		}
		if rp := n.Kid("rPr"); rp != nil && rp.Kid("nor") != nil {
			return pre + "\\text{" + t + "}"
		}
		if tag, ok := eqTagRun(t); ok {
			return pre + "\\tag{" + tag + "}"
		}
		if rp := n.Kid("rPr"); rp != nil && rp.Kid("sty") != nil {
			if v, _ := rp.Kid("sty").A("val"); (v == "b" || v == "bi") && strings.TrimSpace(t) != "" {
				return pre + "\\mathbf{" + linearText(t, true) + "}"
			}
			if v, _ := rp.Kid("sty").A("val"); v == "p" && eqUprightLetters(t) {
				return pre + "\\mathup{" + t + "}"
			}
		}
		return pre + linearText(t, true)
	case "f":
		if val("fPr", "type", "bar") == "noBar" {
			return "\\binom" + g("num") + g("den")
		}
		return "\\frac" + g("num") + g("den")
	case "sSup":
		return g("e") + "^" + g("sup")
	case "sSub":
		return g("e") + "_" + g("sub")
	case "sSubSup":
		return g("e") + "_" + g("sub") + "^" + g("sup")
	case "sPre":
		return "{}_" + g("sub") + "^" + g("sup") + g("e")
	case "rad":
		if val("radPr", "degHide", "0") == "1" || OMMLToLinear(n.Kid("deg")) == "" {
			return "\\sqrt" + g("e")
		}
		return "\\sqrt[" + OMMLToLinear(n.Kid("deg")) + "]" + g("e")
	case "nary":
		ch := val("naryPr", "chr", "∫")
		name := "int"
		for k, v := range eqNary {
			if v == ch {
				name = k
			}
		}
		s := "\\" + name
		if x := OMMLToLinear(n.Kid("sub")); x != "" {
			s += "_{" + x + "}"
		}
		if x := OMMLToLinear(n.Kid("sup")); x != "" {
			s += "^{" + x + "}"
		}
		return s + " " + g("e")
	case "d":
		o, c := val("dPr", "begChr", "("), val("dPr", "endChr", ")")
		var parts []string
		for _, e := range n.KidsNamed("e") {
			parts = append(parts, OMMLToLinear(e))
		}
		inner := strings.Join(parts, ",")
		if o == "(" && c == ")" && strings.HasPrefix(inner, "\\binom") {
			return inner
		}
		if len(parts) == 1 && strings.HasPrefix(strings.TrimSpace(inner), "\\begin{matrix}") {
			env := map[string]string{"()": "pmatrix", "[]": "bmatrix", "{}": "Bmatrix", "||": "vmatrix", "‖‖": "Vmatrix", "{": "cases"}[o+c]
			if env != "" {
				return strings.ReplaceAll(strings.TrimSpace(inner), "{matrix}", "{"+env+"}")
			}
		}
		if o == "{" && c == "" && len(parts) == 1 && strings.HasPrefix(strings.TrimSpace(inner), "\\begin{aligned}") {
			return strings.ReplaceAll(strings.TrimSpace(inner), "{aligned}", "{cases}")
		}
		if o == "(" && c == ")" {
			return "(" + inner + ")"
		}
		if o == "[" && c == "]" {
			return "[" + inner + "]"
		}
		lo, lc := o, c
		if lo == "{" {
			lo = "\\{"
		}
		if lc == "}" {
			lc = "\\}"
		}
		if lo == "" {
			lo = "."
		}
		if lc == "" {
			lc = "."
		}
		dl := map[string]string{"⟨": "\\langle", "⟩": "\\rangle"}
		if v, ok := dl[lo]; ok {
			lo = v
		}
		if v, ok := dl[lc]; ok {
			lc = v
		}
		return "\\left" + lo + " " + inner + " \\right" + lc
	case "func":
		return OMMLToLinear(n.Kid("fName")) + " " + OMMLToLinear(n.Kid("e"))
	case "limLow":
		return strings.TrimSpace(OMMLToLinear(n.Kid("e"))) + "_" + g("lim")
	case "acc":
		ch := val("accPr", "chr", "̂")
		name := map[string]string{"⃗": "vec", "̂": "hat", "̃": "tilde", "̇": "dot", "̈": "ddot", "̅": "bar"}[ch]
		if name == "" {
			name = "hat"
		}
		return "\\" + name + g("e")
	case "bar":
		return "\\bar" + g("e")
	case "limUpp":
		if e := n.Kid("e"); e != nil {
			if gc := e.Kid("groupChr"); gc != nil {
				return "\\overbrace{" + OMMLToLinear(gc.Kid("e")) + "}^" + g("lim")
			}
		}
		return "\\overset" + g("lim") + g("e")
	case "groupChr":
		ch := val("groupChrPr", "chr", "⏟")
		pos := val("groupChrPr", "pos", "bot")
		if ch == "⏞" || pos == "top" {
			return "\\overbrace" + g("e")
		}
		return "\\underbrace" + g("e")
	case "m":
		var rows []string
		for _, mr := range n.KidsNamed("mr") {
			var cs []string
			for _, e := range mr.KidsNamed("e") {
				cs = append(cs, OMMLToLinear(e))
			}
			rows = append(rows, strings.Join(cs, " & "))
		}
		return "\\begin{matrix}" + strings.Join(rows, " \\\\ ") + "\\end{matrix}"
	case "eqArr":
		var cs []string
		for _, e := range n.KidsNamed("e") {
			cs = append(cs, strings.TrimSpace(OMMLToLinear(e)))
		}
		return "\\begin{aligned}" + strings.Join(cs, " \\\\ ") + "\\end{aligned}"

	case "rPr", "fPr", "sSupPr", "sSubPr", "radPr", "naryPr", "dPr", "accPr", "barPr", "funcPr", "ctrlPr", "mPr", "oMathParaPr", "eqArrPr", "limLowPr", "limUppPr", "groupChrPr", "mcs", "mcPr", "mc":
		return ""
	}
	return kids()
}

// ---------------------------------------------------------------- MathML (ODT, HTML)
func EqMathML(src string) string {
	return `<math xmlns="http://www.w3.org/1998/Math/MathML"><semantics><mrow>` + mathml(ParseEq(src)) +
		`</mrow><annotation encoding="application/x-texora">` + xesc(src) + `</annotation></semantics></math>`
}

func mathml(n *eqNode) string {
	if n == nil {
		return "<mrow/>"
	}
	r := func(x *eqNode) string { return "<mrow>" + mathml(x) + "</mrow>" }
	switch n.K {
	case "row":
		var sb strings.Builder
		for _, k := range n.Kids {
			sb.WriteString(mathml(k))
		}
		return sb.String()
	case "id":
		if n.Sty == "b" {
			return `<mi mathvariant="bold-italic">` + xesc(n.T) + "</mi>"
		}
		if n.Sty == "rm" {
			return `<mi mathvariant="normal">` + xesc(n.T) + "</mi>"
		}
		return "<mi>" + xesc(n.T) + "</mi>"
	case "num":
		return "<mn>" + xesc(n.T) + "</mn>"
	case "tag":
		return `<mspace width="1em"/><mtext>(` + xesc(n.T) + ")</mtext>"
	case "matrix":
		var sb strings.Builder
		al := ""
		if n.Cols != "" {
			nc := 0
			for _, rw := range n.Rows {
				nc = max(nc, len(rw))
			}
			var cs []string
			for j := 0; j < nc; j++ {
				cs = append(cs, map[byte]string{'l': "left", 'r': "right", 'c': "center"}[n.Cols[j%len(n.Cols)]])
			}
			al = ` columnalign="` + strings.Join(cs, " ") + `"`
		}
		if n.T == "aligned" {
			al += ` columnspacing="0 1em"`
		}
		sb.WriteString("<mtable" + al + ">")
		for _, rw := range n.Rows {
			sb.WriteString("<mtr>")
			for _, c := range rw {
				sb.WriteString("<mtd>" + r(c) + "</mtd>")
			}
			sb.WriteString("</mtr>")
		}
		sb.WriteString("</mtable>")
		return sb.String()
	case "over":
		return "<mover>" + r(n.Base) + r(n.Sup) + "</mover>"
	case "under":
		return "<munder>" + r(n.Base) + r(n.Sub) + "</munder>"
	case "brace":
		tag, ch := "mover", "⏞"
		if n.T == "under" {
			tag, ch = "munder", "⏟"
		}
		g := "<" + tag + ">" + r(n.Body) + `<mo stretchy="true">` + ch + "</mo></" + tag + ">"
		if n.Sub != nil {
			g = "<" + tag + ">" + g + r(n.Sub) + "</" + tag + ">"
		}
		return g
	case "op":
		return "<mo>" + xesc(n.T) + "</mo>"
	case "fn":
		return `<mi mathvariant="normal">` + xesc(n.T) + "</mi>"
	case "text":
		return "<mtext>" + xesc(n.T) + "</mtext>"
	case "space":
		return `<mspace width="` + eqSpaceWidth[n.T] + `"/>`
	case "frac":
		if n.T == "noline" {
			return `<mfrac linethickness="0">` + r(n.Num) + r(n.Den) + "</mfrac>"
		}
		return "<mfrac>" + r(n.Num) + r(n.Den) + "</mfrac>"
	case "sup":
		return "<msup>" + r(n.Base) + r(n.Sup) + "</msup>"
	case "sub":
		return "<msub>" + r(n.Base) + r(n.Sub) + "</msub>"
	case "subsup":
		return "<msubsup>" + r(n.Base) + r(n.Sub) + r(n.Sup) + "</msubsup>"
	case "limlow":
		return "<munder>" + r(n.Base) + r(n.Sub) + "</munder>"
	case "sqrt":
		if n.Deg != nil {
			return "<mroot>" + r(n.Body) + r(n.Deg) + "</mroot>"
		}
		return "<msqrt>" + mathml(n.Body) + "</msqrt>"
	case "nary":
		op := "<mo stretchy=\"false\">" + n.T + "</mo>"
		integral := strings.ContainsRune("∫∬∭∮", []rune(n.T)[0])
		var s string
		switch {
		case n.Sub != nil && n.Sup != nil:
			tag := "munderover"
			if integral {
				tag = "msubsup"
			}
			s = "<" + tag + ">" + op + r(n.Sub) + r(n.Sup) + "</" + tag + ">"
		case n.Sub != nil:
			tag := "munder"
			if integral {
				tag = "msub"
			}
			s = "<" + tag + ">" + op + r(n.Sub) + "</" + tag + ">"
		default:
			s = op
		}
		return s + mathml(n.Body)
	case "delim":
		return "<mrow><mo fence=\"true\">" + xesc(n.Open) + "</mo>" + mathml(n.Body) + "<mo fence=\"true\">" + xesc(n.Close) + "</mo></mrow>"
	case "acc":
		return `<mover accent="true">` + r(n.Body) + "<mo>" + xesc(n.T) + "</mo></mover>"
	}
	return ""
}

// eqTableBody — строки таблицы из \\begin{array}{…}…\\end{array} или \\begin{matrix}…\\end{matrix}.
func eqTableBody(s string) (string, bool) {
	for _, env := range []string{"array", "matrix"} {
		b, e := "\\begin{"+env+"}", "\\end{"+env+"}"
		if !strings.HasPrefix(s, b) || !strings.HasSuffix(s, e) {
			continue
		}
		s = s[len(b) : len(s)-len(e)]
		if env == "array" && strings.HasPrefix(s, "{") {
			if i := strings.IndexByte(s, '}'); i >= 0 {
				s = s[i+1:]
			}
		}
		return s, true
	}
	return "", false
}

// eqUprightLetters — прямые (не курсивные) буквы вроде «d» в производной; имена функций сюда не относятся.
func eqUprightLetters(t string) bool {
	if t == "" || eqFuncs[t] {
		return false
	}
	for _, r := range t {
		if !unicode.IsLetter(r) && r != '∂' {
			return false
		}
	}
	return true
}

// eqNaryName — имя команды для знака ∑, ∫ и т. п.
func eqNaryName(ch string) (string, bool) {
	for k, v := range eqNary {
		if v == ch {
			return k, true
		}
	}
	return "", false
}

// MathMLToLinear — для чтения формул из ODT.
func MathMLToLinear(n *Node) string {
	if n == nil {
		return ""
	}
	kids := n.Elems()
	all := func() string {
		var sb strings.Builder
		for i := 0; i < len(kids); i++ {
			k := kids[i]
			// номер формулы: <mspace width="1em"/><mtext>(1.3)</mtext>
			if w, _ := k.A("width"); k.Name == "mspace" && w == "1em" && i+1 < len(kids) && kids[i+1].Name == "mtext" {
				if tag, ok := eqTagRun("  " + kids[i+1].TextContent()); ok {
					sb.WriteString("\\tag{" + tag + "}")
					i++
					continue
				}
			}
			sb.WriteString(MathMLToLinear(k))
		}
		return sb.String()
	}
	arg := func(i int) string {
		if i < len(kids) {
			return "{" + MathMLToLinear(kids[i]) + "}"
		}
		return "{}"
	}
	// base — основание индекса: у ∑, ∫ и lim без фигурных скобок, иначе это уже не оператор, а обычный символ с индексом
	base := func() string {
		if len(kids) > 0 {
			k := kids[0]
			for k.Name == "mrow" && len(k.Elems()) == 1 {
				k = k.Elems()[0]
			}
			t := strings.TrimSpace(k.TextContent())
			_, nary := eqNaryName(t)
			if (k.Name == "mo" && nary) || (k.Name == "mi" && eqFuncs[t]) {
				return strings.TrimSpace(MathMLToLinear(k))
			}
		}
		return arg(0)
	}
	switch n.Name {
	case "annotation", "annotation-xml":
		return ""
	case "semantics":
		if len(kids) > 0 {
			return MathMLToLinear(kids[0])
		}
	case "mi", "mn", "mo":
		t := strings.TrimSpace(n.TextContent())
		if v, _ := n.A("mathvariant"); n.Name == "mi" && (v == "bold" || v == "bold-italic") && t != "" {
			return "\\mathbf{" + linearText(t, false) + "}"
		}
		if v, _ := n.A("mathvariant"); n.Name == "mi" && v == "normal" && eqUprightLetters(t) {
			return "\\mathup{" + t + "}"
		}
		if n.Name == "mi" && len([]rune(t)) > 1 {
			if eqFuncs[t] {
				return "\\" + t + " "
			}
			return "\\text{" + t + "}"
		}
		for k, v := range eqNary {
			if v == t {
				return "\\" + k + " "
			}
		}
		return linearText(t, false)
	case "mtext":
		return "\\text{" + n.TextContent() + "}"
	case "mfrac":
		if v, _ := n.A("linethickness"); v == "0" || v == "0pt" {
			return "\\binom" + arg(0) + arg(1)
		}
		return "\\frac" + arg(0) + arg(1)
	case "mtable":
		var rows []string
		for _, tr := range n.KidsNamed("mtr") {
			var cs []string
			for _, td := range tr.KidsNamed("mtd") {
				cs = append(cs, MathMLToLinear(td))
			}
			rows = append(rows, strings.Join(cs, " & "))
		}
		env := "matrix"
		if v, _ := n.A("columnspacing"); strings.HasPrefix(v, "0 ") {
			env = "aligned"
		} else if v, _ := n.A("columnalign"); strings.HasPrefix(v, "left") {
			env = "array}{" + strings.Map(func(r rune) rune {
				switch r {
				case 'l', 'c', 'r':
					return r
				}
				return -1
			}, strings.NewReplacer("left", "l", "center", "c", "right", "r").Replace(v))
			return "\\begin{" + env + "}" + strings.Join(rows, " \\\\ ") + "\\end{array}"
		}
		return "\\begin{" + env + "}" + strings.Join(rows, " \\\\ ") + "\\end{" + env + "}"
	case "mover":
		if len(kids) > 1 && strings.TrimSpace(kids[1].TextContent()) == "⏞" {
			return "\\overbrace" + arg(0)
		}
		if len(kids) > 1 && len([]rune(strings.TrimSpace(kids[1].TextContent()))) == 1 {
			if nm, ok := map[string]string{"→": "vec", "¯": "bar", "^": "hat", "~": "tilde", "˙": "dot", "¨": "ddot"}[strings.TrimSpace(kids[1].TextContent())]; ok {
				return "\\" + nm + arg(0)
			}
		}
		return "\\overset" + arg(1) + arg(0)
	case "munder":
		if len(kids) > 1 && strings.TrimSpace(kids[1].TextContent()) == "⏟" {
			return "\\underbrace" + arg(0)
		}
		if len(kids) > 0 && kids[0].Name == "mo" {
			return base() + "_" + arg(1)
		}
		if t := strings.TrimSpace(kids[0].TextContent()); eqFuncs[t] {
			return base() + "_" + arg(1)
		}
		return "\\underset" + arg(1) + arg(0)
	case "msup":
		return base() + "^" + arg(1)
	case "msub":
		return base() + "_" + arg(1)
	case "msubsup", "munderover":
		return base() + "_" + arg(1) + "^" + arg(2)
	case "mspace":
		w, _ := n.A("width")
		for k, v := range eqSpaceWidth {
			if v == w && k != " " {
				return "\\" + k + " "
			}
		}
		return "\\ "
	case "mrow":
		if len(kids) >= 2 && kids[0].Name == "mo" && kids[len(kids)-1].Name == "mo" {
			if f, _ := kids[0].A("fence"); f == "true" {
				o, c := strings.TrimSpace(kids[0].TextContent()), strings.TrimSpace(kids[len(kids)-1].TextContent())
				var sb strings.Builder
				for _, k := range kids[1 : len(kids)-1] {
					sb.WriteString(MathMLToLinear(k))
				}
				inner := strings.TrimSpace(sb.String())
				if o == "(" && c == ")" && strings.HasPrefix(inner, "\\binom") {
					return inner
				}
				if env := map[string]string{"{": "cases", "}": "rcases"}[o+c]; env != "" {
					if body, ok := eqTableBody(inner); ok {
						return "\\begin{" + env + "}" + body + "\\end{" + env + "}"
					}
				}
				d := func(x string) string {
					switch x {
					case "":
						return "."
					case "{", "}":
						return "\\" + x
					case "⟨":
						return "\\langle"
					case "⟩":
						return "\\rangle"
					}
					return x
				}
				return "\\left" + d(o) + " " + inner + " \\right" + d(c)
			}
		}
		return all()
	case "msqrt":
		return "\\sqrt{" + all() + "}"
	case "mroot":
		return "\\sqrt[" + MathMLToLinear(kids[min(1, len(kids)-1)]) + "]" + arg(0)
	}
	return all()
}

// EqPlain — формула одной строкой в Юникоде (для TXT, RTF и буфера обмена): (−b ± √(b²−4ac))/(2a).
func EqPlain(src string) string { return eqPlain(ParseEq(src)) }

var supDigits = map[rune]rune{'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹', '+': '⁺', '−': '⁻', 'n': 'ⁿ'}

func eqPlain(n *eqNode) string {
	if n == nil {
		return ""
	}
	grp := func(x *eqNode) string {
		for x != nil && x.K == "row" && len(x.Kids) == 1 { // {(a+b)}^2 — фигурные скобки не добавляют круглых
			x = x.Kids[0]
		}
		s := eqPlain(x)
		if len([]rune(s)) > 1 && !(x.K == "delim") {
			return "(" + s + ")"
		}
		return s
	}
	switch n.K {
	case "row":
		var sb strings.Builder
		for i, k := range n.Kids {
			t := eqPlain(k)
			unary := (k.T == "−" || k.T == "+" || k.T == "±") && (i == 0 || n.Kids[i-1].K == "op")
			if k.K == "op" && !unary && k.T != "," && k.T != "!" {
				t = " " + t + " "
			} else if k.K == "op" && k.T == "," {
				t += " "
			}
			if k.K == "fn" && i+1 < len(n.Kids) {
				t += " "
			}
			sb.WriteString(t)
		}
		return sb.String()
	case "id", "num", "op", "fn", "text":
		return n.T
	case "space":
		return " "
	case "frac":
		return grp(n.Num) + "/" + grp(n.Den)
	case "sup":
		s := eqPlain(n.Sup)
		all := true
		var out []rune
		for _, r := range s {
			if v, ok := supDigits[r]; ok {
				out = append(out, v)
			} else {
				all = false
			}
		}
		if all && len(out) > 0 {
			return grp(n.Base) + string(out)
		}
		return grp(n.Base) + "^" + grp(n.Sup)
	case "sub":
		return eqPlain(n.Base) + "_" + grp(n.Sub)
	case "subsup":
		return eqPlain(n.Base) + "_" + grp(n.Sub) + "^" + grp(n.Sup)
	case "limlow":
		return eqPlain(n.Base) + "_" + grp(n.Sub) + " "
	case "sqrt":
		pre := "√"
		if n.Deg != nil {
			pre = map[string]string{"3": "∛", "4": "∜"}[eqPlain(n.Deg)]
			if pre == "" {
				pre = eqPlain(n.Deg) + "√"
			}
		}
		return pre + "(" + eqPlain(n.Body) + ")"
	case "nary":
		s := n.T
		if n.Sub != nil {
			s += "_" + grp(n.Sub)
		}
		if n.Sup != nil {
			s += "^" + grp(n.Sup)
		}
		return s + " " + eqPlain(n.Body)
	case "delim":
		return n.Open + eqPlain(n.Body) + n.Close
	case "tag":
		return "   (" + n.T + ")"
	case "matrix":
		var rows []string
		for _, rw := range n.Rows {
			var cs []string
			for _, c := range rw {
				cs = append(cs, eqPlain(c))
			}
			sep := " "
			if n.T == "aligned" {
				sep = ""
			} else if n.T == "cases" {
				sep = ", "
			}
			rows = append(rows, strings.Join(cs, sep))
		}
		if n.T == "matrix" || n.T == "small" {
			return "[" + strings.Join(rows, "; ") + "]"
		}
		return strings.Join(rows, "; ")
	case "over", "under", "brace":
		if n.Body != nil {
			return eqPlain(n.Body)
		}
		return eqPlain(n.Base)
	case "acc":
		return eqPlain(n.Body) + map[string]string{"→": "⃗", "¯": "̄", "^": "̂", "~": "̃", "˙": "̇", "¨": "̈"}[n.T]
	}
	return ""
}
