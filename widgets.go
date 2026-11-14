package main

// Всплывающие списки, диалоги и поле ввода — всё нарисовано своим кодом.

import (
	"math"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------- поле ввода
type TextEdit struct {
	text   []rune
	caret  int
	anchor int
}

func NewTextEdit(s string) *TextEdit {
	t := &TextEdit{text: []rune(s)}
	t.caret = len(t.text)
	t.anchor = 0
	return t
}

func (t *TextEdit) String() string { return string(t.text) }

func (t *TextEdit) selRange() (int, int) {
	if t.anchor < t.caret {
		return t.anchor, t.caret
	}
	return t.caret, t.anchor
}

func (t *TextEdit) insert(s string) {
	a, b := t.selRange()
	r := []rune(strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " "))
	t.text = append(t.text[:a], append(r, t.text[b:]...)...)
	t.caret = a + len(r)
	t.anchor = t.caret
}

func (t *TextEdit) Key(vk int, m Mods, clip func() string, setClip func(string)) bool {
	move := func(to int) {
		t.caret = max(0, min(len(t.text), to))
		if !m.Shift {
			t.anchor = t.caret
		}
	}
	switch vk {
	case 37:
		if !m.Shift && t.anchor != t.caret {
			a, _ := t.selRange()
			t.caret, t.anchor = a, a
		} else {
			move(t.caret - 1)
		}
	case 39:
		if !m.Shift && t.anchor != t.caret {
			_, b := t.selRange()
			t.caret, t.anchor = b, b
		} else {
			move(t.caret + 1)
		}
	case 36:
		move(0)
	case 35:
		move(len(t.text))
	case 8:
		a, b := t.selRange()
		if a == b && a > 0 {
			a--
		}
		t.text = append(t.text[:a], t.text[b:]...)
		t.caret, t.anchor = a, a
	case 46:
		a, b := t.selRange()
		if a == b && b < len(t.text) {
			b++
		}
		t.text = append(t.text[:a], t.text[b:]...)
		t.caret, t.anchor = a, a
	case 65:
		if m.Ctrl {
			t.anchor, t.caret = 0, len(t.text)
		} else {
			return false
		}
	case 67, 88:
		if !m.Ctrl {
			return false
		}
		a, b := t.selRange()
		if a < b && setClip != nil {
			setClip(string(t.text[a:b]))
			if vk == 88 {
				t.insert("")
			}
		}
	case 86:
		if !m.Ctrl {
			return false
		}
		if clip != nil {
			t.insert(clip())
		}
	default:
		return false
	}
	return true
}

func (t *TextEdit) Char(r rune) {
	if r >= 32 && r != 127 {
		t.insert(string(r))
	}
}

// ---------------------------------------------------------------- всплывающее окно
type Popup struct {
	R      Rect
	Draw   func(cv *Canvas, p *Popup)
	Click  func(x, y float64) bool // true — закрыть
	Move   func(x, y float64)
	Wheel  func(d float64)
	Key    func(vk int, m Mods) bool
	Char   func(r rune)
	Owner  string
	hover  int
	scroll float64
	filter string
}

type PopItem struct {
	Label string
	Icon  string
	Font  string // показать название этим шрифтом
	On    bool
	Sep   bool
	Act   func()
	Right string
}

// listPopup — список пунктов (меню, шрифты, размеры, интервалы).
func (a *App) listPopup(owner string, x, y, w float64, items []PopItem, maxH float64, withFilter bool) *Popup {
	rowH := a.d(30)
	if withFilter {
		rowH = a.d(28)
	}
	sepH := a.d(9)
	total := 0.0
	for _, it := range items {
		if it.Sep {
			total += sepH
		} else {
			total += rowH
		}
	}
	top := a.d(6)
	if withFilter {
		top = a.d(42)
	}
	h := math.Min(total+top+a.d(6), maxH)
	if y+h > a.H-a.d(8) {
		y = math.Max(a.d(8), a.H-a.d(8)-h)
	}
	if x+w > a.W-a.d(8) {
		x = a.W - a.d(8) - w
	}
	p := &Popup{R: Rect{x, y, w, h}, Owner: owner, hover: -1}
	visible := func() []int {
		var idx []int
		for i, it := range items {
			if p.filter != "" && (it.Sep || !strings.Contains(strings.ToLower(it.Label), strings.ToLower(p.filter))) {
				continue
			}
			idx = append(idx, i)
		}
		return idx
	}
	itemAt := func(px, py float64) int {
		yy := p.R.Y + top - p.scroll
		for _, i := range visible() {
			ih := rowH
			if items[i].Sep {
				ih = sepH
			}
			if py >= yy && py < yy+ih && py >= p.R.Y+top && !items[i].Sep {
				return i
			}
			yy += ih
		}
		return -1
	}
	p.Draw = func(cv *Canvas, p *Popup) {
		th := a.th
		a.glass(cv, p.R, a.d(10))
		if withFilter {
			fr := Rect{p.R.X + a.d(8), p.R.Y + a.d(8), p.R.W - a.d(16), a.d(28)}
			cv.FillRound(fr, a.d(5), th.Field)
			cv.StrokeRound(fr, a.d(5), math.Max(1, a.S), th.Accent)
			t := p.filter
			col := th.Text
			if t == "" {
				t = "Поиск…"
				col = th.TextDim
			}
			a.textAt(cv, t, fr.X+a.d(10), fr.Y+(fr.H-a.d(17))/2, a.uiFont(13), col)
		}
		cv.SetClip(Rect{p.R.X, p.R.Y + top, p.R.W, p.R.H - top - a.d(4)})
		yy := p.R.Y + top - p.scroll
		for _, i := range visible() {
			it := items[i]
			if it.Sep {
				cv.FillRect(Rect{p.R.X + a.d(10), yy + sepH/2, p.R.W - a.d(20), math.Max(1, a.S)}, th.Divider)
				yy += sepH
				continue
			}
			r := Rect{p.R.X + a.d(4), yy, p.R.W - a.d(8), rowH}
			if yy+rowH >= p.R.Y && yy <= p.R.Y+p.R.H {
				if i == p.hover {
					cv.FillRound(r, a.d(5), th.Hover)
				}
				if it.On {
					cv.FillRound(Rect{r.X, r.Y + a.d(7), a.d(3), rowH - a.d(14)}, a.d(1.5), th.Accent)
				}
				tx := r.X + a.d(12)
				if it.Icon != "" {
					a.icon(cv, it.Icon, Rect{tx, yy, a.d(20), rowH}, th.Text)
					tx += a.d(28)
				}
				f := a.uiFont(13)
				if it.Font != "" {
					f = Font{Family: it.Font, Px: a.d(15)}
				}
				asc, desc := a.tb.Metrics(f)
				a.tb.Draw(cv, a.tr(it.Label), tx, yy+(rowH-asc-desc)/2+asc, f, th.Text)
				if it.Right != "" {
					rf := a.uiFont(12)
					rw := a.measure(it.Right, rf)
					a.textAt(cv, it.Right, r.X+r.W-rw-a.d(10), yy+(rowH-a.d(16))/2, rf, th.TextDim)
				}
			}
			yy += rowH
		}
		cv.ResetClip()
		if total+top > p.R.H {
			ratio := (p.R.H - top) / (total + a.d(6))
			th2 := math.Max(a.d(24), (p.R.H-top)*ratio)
			pos := p.scroll / math.Max(1, total+a.d(6)-(p.R.H-top)) * (p.R.H - top - th2)
			cv.FillRound(Rect{p.R.X + p.R.W - a.d(7), p.R.Y + top + pos, a.d(3), th2}, a.d(1.5), th.TextDim.WithA(0.5))
		}
	}
	p.Move = func(x, y float64) { p.hover = itemAt(x, y) }
	p.Click = func(x, y float64) bool {
		i := itemAt(x, y)
		if i < 0 {
			return false
		}
		if items[i].Act != nil {
			items[i].Act()
		}
		return true
	}
	p.Wheel = func(d float64) {
		p.scroll = math.Max(0, math.Min(p.scroll-d, total+a.d(12)+top-p.R.H))
	}
	p.Key = func(vk int, m Mods) bool {
		vis := visible()
		switch vk {
		case 38, 40:
			pos := -1
			for k, i := range vis {
				if i == p.hover {
					pos = k
				}
			}
			for step := 0; step < len(vis); step++ {
				if vk == 40 {
					pos = (pos + 1) % len(vis)
				} else {
					pos = (pos - 1 + len(vis)) % len(vis)
				}
				if !items[vis[pos]].Sep {
					break
				}
			}
			if len(vis) > 0 {
				p.hover = vis[pos]
				yy := 0.0
				for _, i := range vis[:pos] {
					if items[i].Sep {
						yy += sepH
					} else {
						yy += rowH
					}
				}
				if yy < p.scroll {
					p.scroll = yy
				} else if yy+rowH > p.scroll+p.R.H-top-a.d(6) {
					p.scroll = yy + rowH - (p.R.H - top - a.d(6))
				}
			}
			return true
		case 13:
			if p.hover >= 0 && items[p.hover].Act != nil {
				items[p.hover].Act()
			}
			return false
		case 8:
			if withFilter && len(p.filter) > 0 {
				r := []rune(p.filter)
				p.filter = string(r[:len(r)-1])
				p.scroll = 0
			}
			return true
		}
		return true
	}
	p.Char = func(r rune) {
		if withFilter && r >= 32 {
			p.filter += string(r)
			p.scroll = 0
			if v := visible(); len(v) > 0 {
				p.hover = v[0]
			}
		}
	}
	// прокрутить к отмеченному пункту
	yy := 0.0
	for _, it := range items {
		if it.On {
			p.scroll = math.Max(0, math.Min(yy-h/2+rowH, total+top-h))
			break
		}
		if it.Sep {
			yy += sepH
		} else {
			yy += rowH
		}
	}
	return p
}

// ---------------------------------------------------------------- диалоги
type Field struct {
	Label  string
	Kind   string // text, check, select, info, custom, chips
	Edit   *TextEdit
	On     bool
	Opts   []string
	Sel    int
	r      Rect
	Paint  func(cv *Canvas, r Rect) // custom
	H      float64                  // custom: высота
	Vals   []string                 // chips: что вставить
	Target int                      // chips: в какое поле
	chipR  []Rect
	OnSel  func(i int) // select: при выборе
}

type DlgBtn struct {
	Label   string
	Primary bool
	Act     func() bool // true — закрыть диалог
}

type Dialog struct {
	Title   string
	Text    string
	Fields  []*Field
	Buttons []DlgBtn
	Info    string
	W       float64
	focus   int
	r       Rect
	btnR    []Rect
}

func (d *Dialog) Val(i int) string { return d.Fields[i].Edit.String() }

func (a *App) openDialog(d *Dialog) {
	if d.W == 0 {
		d.W = 420
	}
	d.focus = -1
	for i, f := range d.Fields {
		if f.Kind == "text" {
			d.focus = i
			break
		}
	}
	a.dialog = d
	a.popup = nil
	a.invalidate()
}

func (a *App) paintDialog(cv *Canvas) {
	d := a.dialog
	th := a.th
	cv.FillRect(Rect{0, 0, a.W, a.H}, RGBA(0, 0, 0, 70))
	w := math.Min(a.d(d.W), a.W-a.d(24))
	lineH := a.d(34)
	h := a.d(56) + a.d(56)
	textLines := []string{}
	if d.Text != "" {
		textLines = a.wrapText(d.Text, w-a.d(48), a.uiFont(13))
		h += float64(len(textLines))*a.d(20) + a.d(8)
	}
	for _, f := range d.Fields {
		switch f.Kind {
		case "info":
			h += a.d(22)
		case "custom":
			h += a.d(f.H)
		case "chips":
			h += a.chipsHeight(f, w-a.d(48))
		default:
			h += lineH
		}
	}
	if d.Info != "" {
		h += a.d(24)
	}
	// окно ниже диалога — сжимаем крупные поля (предпросмотр), чтобы кнопки не уехали за край
	customK := 1.0
	if over := h - (a.H - a.d(16)); over > 0 {
		ch := 0.0
		for _, f := range d.Fields {
			if f.Kind == "custom" {
				ch += a.d(f.H)
			}
		}
		if ch > 0 {
			customK = math.Max(0.35, (ch-over)/ch)
			h -= ch * (1 - customK)
		}
	}
	d.r = Rect{math.Max(a.d(12), (a.W-w)/2), math.Max(a.d(8), math.Min((a.H-h)/2, a.H-h-a.d(8))), w, h}
	r := d.r
	a.glass(cv, r, a.d(14))
	a.textAt(cv, d.Title, r.X+a.d(24), r.Y+a.d(20), Font{Family: a.UIFont, Px: a.d(17), Bold: true}, th.Text)
	y := r.Y + a.d(56)
	for _, l := range textLines {
		a.textAt(cv, l, r.X+a.d(24), y, a.uiFont(13), th.Text)
		y += a.d(20)
	}
	if len(textLines) > 0 {
		y += a.d(8)
	}
	labelW := 0.0
	for _, f := range d.Fields {
		if f.Kind != "check" && f.Kind != "info" {
			labelW = math.Max(labelW, a.measure(f.Label, a.uiFont(13)))
		}
	}
	fx := r.X + a.d(24) + labelW + a.d(14)
	for i, f := range d.Fields {
		switch f.Kind {
		case "info":
			a.textAt(cv, f.Label, r.X+a.d(24), y, a.uiFont(13), th.TextDim)
			y += a.d(22)
			continue
		case "custom":
			fh := a.d(f.H) * customK
			fr := Rect{r.X + a.d(24), y, w - a.d(48), fh - a.d(6)}
			f.r = fr
			if f.Paint != nil {
				f.Paint(cv, fr)
			}
			y += fh
			continue
		case "chips":
			ch := a.chipsHeight(f, w-a.d(48))
			f.chipR = nil
			x0, cx, cy := r.X+a.d(24), r.X+a.d(24), y+a.d(2)
			cf := a.uiFont(12)
			for ci, lbl := range f.Opts {
				cwid := a.measure(lbl, cf) + a.d(16)
				if cx+cwid > r.X+w-a.d(24) && cx > x0 {
					cx, cy = x0, cy+a.d(28)
				}
				cr := Rect{cx, cy, cwid, a.d(24)}
				f.chipR = append(f.chipR, cr)
				id := "chip:" + lbl + ":" + string(rune('0'+ci%10))
				a.addHit(id, cr, 0, nil)
				cv.FillRound(cr, a.d(5), Mix(th.Field, th.Accent, 0.08+a.anim[id]*0.15))
				cv.StrokeRound(cr, a.d(5), math.Max(1, a.S), th.Border)
				a.centerText(cv, lbl, cr, cf, th.Text)
				cx += cwid + a.d(6)
			}
			y += ch
			continue
		case "check":
			box := Rect{r.X + a.d(24), y + a.d(8), a.d(18), a.d(18)}
			f.r = Rect{box.X, y, w - a.d(48), lineH}
			if f.On {
				cv.FillRound(box, a.d(4), th.Accent)
				a.icon(cv, "check", box, Hex(0xffffff))
			} else {
				cv.StrokeRound(box, a.d(4), math.Max(1, a.S), th.TextDim)
			}
			a.textAt(cv, f.Label, box.X+a.d(28), y+(lineH-a.d(17))/2, a.uiFont(13), th.Text)
		case "text", "select":
			a.textAt(cv, f.Label, r.X+a.d(24), y+(lineH-a.d(17))/2, a.uiFont(13), th.Text)
			fr := Rect{fx, y + a.d(3), r.X + w - a.d(24) - fx, lineH - a.d(6)}
			f.r = fr
			cv.FillRound(fr, a.d(5), th.Field)
			bc := th.Border
			if i == d.focus {
				bc = th.Accent
			}
			cv.StrokeRound(fr, a.d(5), math.Max(1, a.S)*map[bool]float64{true: 1.5, false: 1}[i == d.focus], bc)
			uf := a.uiFont(13)
			if f.Kind == "select" {
				a.textAt(cv, f.Opts[f.Sel], fr.X+a.d(10), fr.Y+(fr.H-a.d(17))/2, uf, th.Text)
				a.icon(cv, "chevron", Rect{fr.X + fr.W - a.d(26), fr.Y, a.d(20), fr.H}, th.TextDim)
			} else {
				t := f.Edit
				s, e := t.selRange()
				tx := fr.X + a.d(10)
				ty := fr.Y + (fr.H-a.d(17))/2
				if i == d.focus && s != e {
					x1 := tx + a.measure(string(t.text[:s]), uf)
					x2 := tx + a.measure(string(t.text[:e]), uf)
					cv.FillRect(Rect{x1, fr.Y + a.d(5), x2 - x1, fr.H - a.d(10)}, th.Accent.WithA(0.3))
				}
				a.textAt(cv, t.String(), tx, ty, uf, th.Text)
				if i == d.focus && a.CaretOn {
					cx := tx + a.measure(string(t.text[:t.caret]), uf)
					cv.FillRect(Rect{math.Round(cx), fr.Y + a.d(6), math.Max(1, math.Round(a.S)), fr.H - a.d(12)}, th.Text)
				}
			}
		}
		y += lineH
	}
	if d.Info != "" {
		a.textAt(cv, d.Info, r.X+a.d(24), y+a.d(4), a.uiFont(12), th.TextDim)
	}
	bx := r.X + w - a.d(24)
	d.btnR = make([]Rect, len(d.Buttons))
	for i := len(d.Buttons) - 1; i >= 0; i-- {
		b := d.Buttons[i]
		bw := math.Max(a.d(96), a.measure(b.Label, a.uiFont(13))+a.d(32))
		bx -= bw
		br := Rect{bx, r.Y + h - a.d(52), bw, a.d(32)}
		d.btnR[i] = br
		id := "dlgbtn:" + b.Label
		hv := a.anim[id]
		a.addHit(id, br, 0, nil)
		if b.Primary {
			cv.FillRound(br, a.d(6), Mix(th.Accent, Hex(0xffffff), hv*0.12))
			a.centerText(cv, b.Label, br, a.uiFont(13), th.AccentText)
		} else {
			cv.FillRound(br, a.d(6), Mix(th.Field, th.Text, 0.04+hv*0.06))
			cv.StrokeRound(br, a.d(6), math.Max(1, a.S), th.Border)
			a.centerText(cv, b.Label, br, a.uiFont(13), th.Text)
		}
		bx -= a.d(8)
	}
}

func (a *App) dialogClick(x, y float64) {
	d := a.dialog
	for i, br := range d.btnR {
		if br.Contains(x, y) {
			if d.Buttons[i].Act == nil || d.Buttons[i].Act() {
				if a.dialog == d {
					a.dialog = nil
				}
			}
			return
		}
	}
	for _, f := range d.Fields {
		if f.Kind != "chips" {
			continue
		}
		for ci, cr := range f.chipR {
			if cr.Contains(x, y) && f.Target < len(d.Fields) && d.Fields[f.Target].Edit != nil {
				t := d.Fields[f.Target].Edit
				v := f.Vals[ci]
				t.insert(strings.ReplaceAll(v, "|", ""))
				if k := strings.Index(v, "|"); k >= 0 { // курсор на место «|»
					t.caret -= len([]rune(v)) - 1 - len([]rune(v[:k]))
					t.anchor = t.caret
				}
				d.focus = f.Target
				return
			}
		}
	}
	for i, f := range d.Fields {
		if !f.r.Contains(x, y) {
			continue
		}
		switch f.Kind {
		case "check":
			f.On = !f.On
		case "select":
			d.focus = i
			var items []PopItem
			for k, o := range f.Opts {
				k := k
				items = append(items, PopItem{Label: o, On: k == f.Sel, Act: func() {
					f.Sel = k
					if f.OnSel != nil {
						f.OnSel(k)
					}
				}})
			}
			a.popup = a.listPopup("dlgsel", f.r.X, f.r.Y+f.r.H+a.d(2), f.r.W, items, a.d(320), false)
		case "text":
			d.focus = i
			uf := a.uiFont(13)
			rx := x - f.r.X - a.d(10)
			k := 0
			for k < len(f.Edit.text) && a.measure(string(f.Edit.text[:k+1]), uf) < rx+a.d(3) {
				k++
			}
			f.Edit.caret, f.Edit.anchor = k, k
		}
		return
	}
}

func (a *App) dialogKey(vk int, m Mods) {
	d := a.dialog
	switch vk {
	case 27:
		a.dialog = nil
		return
	case 13:
		for _, b := range d.Buttons {
			if b.Primary {
				if b.Act == nil || b.Act() {
					if a.dialog == d {
						a.dialog = nil
					}
				}
				return
			}
		}
		return
	case 9:
		n := len(d.Fields)
		for k := 1; k <= n; k++ {
			i := (d.focus + k) % n
			if m.Shift {
				i = (d.focus - k + n*2) % n
			}
			if d.Fields[i].Kind == "text" {
				d.focus = i
				d.Fields[i].Edit.anchor, d.Fields[i].Edit.caret = 0, len(d.Fields[i].Edit.text)
				break
			}
		}
		return
	}
	if d.focus >= 0 && d.Fields[d.focus].Kind == "text" {
		d.Fields[d.focus].Edit.Key(vk, m, func() string { return a.P.GetClipboard().Text }, func(s string) { a.P.SetClipboard(ClipData{Text: s}) })
	}
}

func (a *App) dialogChar(r rune) {
	d := a.dialog
	if d.focus >= 0 && d.Fields[d.focus].Kind == "text" {
		d.Fields[d.focus].Edit.Char(r)
	}
}

func (a *App) wrapText(s string, w float64, f Font) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.FieldsFunc(para, unicode.IsSpace)
		line := ""
		for _, wd := range words {
			try := strings.TrimSpace(line + " " + wd)
			if a.measure(try, f) > w && line != "" {
				out = append(out, line)
				line = wd
			} else {
				line = try
			}
		}
		out = append(out, line)
	}
	return out
}

func textField(label, val string) *Field {
	return &Field{Label: label, Kind: "text", Edit: NewTextEdit(val)}
}

func (a *App) chipsHeight(f *Field, w float64) float64 {
	cf := a.uiFont(12)
	rows, cx := 1, 0.0
	for _, lbl := range f.Opts {
		cw := a.measure(lbl, cf) + a.d(16)
		if cx+cw > w && cx > 0 {
			rows++
			cx = 0
		}
		cx += cw + a.d(6)
	}
	return float64(rows)*a.d(28) + a.d(6)
}
