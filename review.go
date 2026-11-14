package main

// Рецензирование в редакторе: режим исправлений (вставки/удаления с автором), примечания.

import (
	"strings"
	"time"
	"unicode"
)

func nowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// trackRev — id правки текущего сеанса (создаётся при первой правке в режиме исправлений).
func (e *Editor) trackRev() string {
	if !e.Doc.Track {
		return ""
	}
	if e.rev == "" || e.Doc.Revs[e.rev] == nil {
		e.Doc.ensure()
		e.rev = NewID("r")
		a := e.Author
		if a == "" {
			a = "Автор"
		}
		e.Doc.Revs[e.rev] = &Rev{Author: a, Date: nowISO()}
	}
	return e.rev
}

// eachRange вызывает fn для отрезка [a,b) каждого абзаца выделения s..en (в порядке документа).
func (e *Editor) eachRange(s, en Pos, fn func(p *Para, a, b int)) {
	i0, i1 := e.fidx[s.P], e.fidx[en.P]
	for i := i0; i <= i1 && i < len(e.flat); i++ {
		p := e.flat[i].P
		a, b := 0, p.Len()
		if i == i0 {
			a = s.O
		}
		if i == i1 {
			b = en.O
		}
		if a < b {
			fn(p, a, b)
		}
	}
}

// trackDeleteIn помечает [a,b) как удалённое; свои вставки удаляются сразу. Возвращает, сколько символов осталось.
func (e *Editor) trackDeleteIn(p *Para, a, b int) int {
	rev := e.trackRev()
	p.Format(a, b, func(f F) F {
		if f.Del == "" {
			f.Del = rev
		}
		return f
	})
	removed := 0
	out := p.Runs[:0:0]
	pos := 0
	for _, r := range p.Runs {
		L := r.Len()
		if r.T == RText && pos >= a && pos+L <= b && r.F.Ins != "" && r.F.Ins == rev && r.F.Del != "" {
			removed += L
			pos += L
			continue
		}
		out = append(out, r)
		pos += L
	}
	p.Runs = out
	p.Normalize()
	return (b - a) - removed
}

func (e *Editor) trackDeleteSel() {
	s, en := e.Range()
	e.eachRange(s, en, func(p *Para, a, b int) { e.trackDeleteIn(p, a, b) })
	e.refresh()
	e.Sel = Sel{s, s}
}

// isDel — символ с индексом o абзаца p помечен как удалённый.
func isDel(p *Para, o int) bool {
	pos := 0
	for _, r := range p.Runs {
		L := r.Len()
		if o >= pos && o < pos+L {
			return r.T == RText && r.F.Del != ""
		}
		pos += L
	}
	return false
}

func (e *Editor) trackBackspace(word bool) {
	p, o := e.Sel.F.P, e.Sel.F.O
	for o > 0 && isDel(p, o-1) {
		o--
	}
	if o == 0 {
		e.SetCaret(e.prev(Pos{P: p, O: 0}), false)
		return
	}
	e.Checkpoint("del")
	a := o - 1
	if word {
		a = e.wordPrev(Pos{P: p, O: o}).O
	}
	e.trackDeleteIn(p, a, o)
	e.Sel = Sel{Pos{P: p, O: a}, Pos{P: p, O: a}}
	e.Changed()
}

func (e *Editor) trackDelete(word bool) {
	p, o := e.Sel.F.P, e.Sel.F.O
	for o < p.Len() && isDel(p, o) {
		o++
	}
	if o >= p.Len() {
		e.SetCaret(e.next(Pos{P: p, O: p.Len()}), false)
		return
	}
	e.Checkpoint("del")
	b := o + 1
	if word {
		b = max(b, e.wordNext(Pos{P: p, O: o}).O)
	}
	left := e.trackDeleteIn(p, o, b)
	e.Sel = Sel{Pos{P: p, O: o + left}, Pos{P: p, O: o + left}}
	e.Changed()
}

// markIns помечает вставленный кусок как исправление.
func (e *Editor) markIns(s, en Pos) {
	rev := e.trackRev()
	if rev == "" {
		return
	}
	e.eachRange(s, en, func(p *Para, a, b int) {
		p.Format(a, b, func(f F) F { f.Ins = rev; return f })
	})
}

// Revision — исправление (для навигации и списка).
type RevSpan struct {
	P    *Para
	A, B int
	Ins  bool
	ID   string
}

func (e *Editor) revSpans() []RevSpan {
	var out []RevSpan
	for _, en := range e.flat {
		pos := 0
		for _, r := range en.P.Runs {
			L := r.Len()
			if r.F.Ins != "" || r.F.Del != "" {
				id, ins := r.F.Ins, true
				if r.F.Del != "" {
					id, ins = r.F.Del, false
				}
				if n := len(out); n > 0 && out[n-1].P == en.P && out[n-1].B == pos && out[n-1].ID == id && out[n-1].Ins == ins {
					out[n-1].B = pos + L
				} else {
					out = append(out, RevSpan{P: en.P, A: pos, B: pos + L, Ins: ins, ID: id})
				}
			}
			pos += L
		}
	}
	return out
}

func (e *Editor) HasRevisions() bool { return len(e.revSpans()) > 0 }

// NextRevision выделяет следующее (dir>0) или предыдущее исправление.
func (e *Editor) NextRevision(dir int) bool {
	spans := e.revSpans()
	if len(spans) == 0 {
		return false
	}
	_, cur := e.Range()
	if dir < 0 {
		cur, _ = e.Range()
	}
	pick := -1
	for i, s := range spans {
		c := e.cmp(Pos{P: s.P, O: s.A}, cur)
		if dir > 0 && c >= 0 {
			pick = i
			break
		}
		if dir < 0 && e.cmp(Pos{P: s.P, O: s.B}, cur) <= 0 {
			pick = i
		}
	}
	if pick < 0 {
		if dir > 0 {
			pick = 0
		} else {
			pick = len(spans) - 1
		}
	}
	s := spans[pick]
	e.Sel = Sel{Pos{P: s.P, O: s.A}, Pos{P: s.P, O: s.B}}
	return true
}

// ResolveRevisions принимает (accept) или отклоняет исправления: all — во всём документе, иначе в выделении/под курсором.
func (e *Editor) ResolveRevisions(accept, all bool) int {
	spans := e.revSpans()
	s, en := e.Range()
	if !all && e.Collapsed() {
		for _, sp := range spans {
			if sp.P == s.P && sp.A <= s.O && s.O <= sp.B {
				s, en = Pos{P: sp.P, O: sp.A}, Pos{P: sp.P, O: sp.B}
				break
			}
		}
	}
	n := 0
	e.Checkpoint("")
	gone := map[*Para]bool{} // абзацы, исчезающие целиком (вставка отклонена или удаление принято)
	for _, en2 := range e.flat {
		p := en2.P
		a, b := 0, p.Len()
		if !all {
			i, i0, i1 := e.fidx[p], e.fidx[s.P], e.fidx[en.P]
			if i < i0 || i > i1 {
				continue
			}
			if i == i0 {
				a = s.O
			}
			if i == i1 {
				b = en.O
			}
			if a >= b {
				continue
			}
		}
		if p.PF.RevPara != "" && a == 0 && b == p.Len() {
			if (p.PF.RevPara == "del") == accept {
				gone[p] = true
			}
			p.PF.RevPara = ""
			p.Touch()
		}
		i := p.SplitAt(a)
		j := p.SplitAt(b)
		var out []Run
		for k, r := range p.Runs {
			if k >= i && k < j && (r.F.Ins != "" || r.F.Del != "") {
				n++
				drop := (r.F.Del != "" && accept) || (r.F.Ins != "" && r.F.Del == "" && !accept)
				if r.F.Ins != "" && r.F.Del != "" {
					drop = true
				}
				if drop {
					continue
				}
				r.F.Ins, r.F.Del = "", ""
			}
			out = append(out, r)
		}
		p.Runs = out
		p.Normalize()
	}
	// убираем исчезнувшие абзацы из их списков (в каждом списке остаётся хотя бы один абзац)
	if len(gone) > 0 {
		seen := map[*[]Block]bool{}
		for _, en2 := range e.flat {
			arr := en2.Arr
			if seen[arr] || !gone[en2.P] {
				continue
			}
			seen[arr] = true
			var keep []Block
			for _, b := range *arr {
				if b.P != nil && gone[b.P] && len(b.P.Runs) == 0 && b.P.PF.Sect == nil {
					continue
				}
				keep = append(keep, b)
			}
			if len(keep) == 0 {
				keep = []Block{{P: NewPara()}}
			}
			*arr = keep
		}
		e.refresh()
		s = e.clampPos(s)
	}
	e.Sel = Sel{s, s}
	e.Changed()
	return n
}

// ---------------------------------------------------------------- примечания
func (e *Editor) AddComment(text, author string) string {
	if e.Collapsed() {
		e.SelectWord(e.Sel.F)
	}
	if e.Collapsed() {
		return ""
	}
	e.Checkpoint("")
	e.Doc.ensure()
	id := NewID("c")
	ini := ""
	for _, w := range strings.Fields(author) {
		ini += string(unicode.ToUpper([]rune(w)[0]))
	}
	e.Doc.Comments[id] = &Comment{Author: author, Initials: ini, Date: nowISO(), Text: text}
	s, en := e.Range()
	e.eachRange(s, en, func(p *Para, a, b int) {
		p.Format(a, b, func(f F) F { f.Cmt = id; return f })
	})
	e.Doc.activeCmt = id
	e.Sel = Sel{en, en}
	e.Changed()
	return id
}

func clearCmt(bs []Block, id string) {
	for _, en := range Flatten(&bs, nil, nil) {
		changed := false
		for i := range en.P.Runs {
			if en.P.Runs[i].F.Cmt == id {
				en.P.Runs[i].F.Cmt = ""
				changed = true
			}
		}
		if changed {
			en.P.Normalize()
		}
	}
}

func (e *Editor) DeleteComment(id string) {
	e.Checkpoint("")
	delete(e.Doc.Comments, id)
	clearCmt(e.Doc.Body, id)
	for _, n := range e.Doc.Footnotes {
		clearCmt(n.Blocks, id)
	}
	if e.Doc.activeCmt == id {
		e.Doc.activeCmt = ""
	}
	e.Changed()
}

// CommentAt — примечание под курсором.
func (e *Editor) CommentAt() string {
	p := e.Sel.F
	f := p.P.FmtAt(p.O)
	if f.Cmt == "" && p.O < p.P.Len() {
		f = p.P.FmtAt(p.O + 1)
	}
	return f.Cmt
}

// CommentOrder — примечания в порядке появления в тексте.
func (d *Doc) CommentOrder() []string {
	var out []string
	seen := map[string]bool{}
	for _, en := range Flatten(&d.Body, nil, nil) {
		for _, r := range en.P.Runs {
			if r.F.Cmt != "" && !seen[r.F.Cmt] && d.Comments[r.F.Cmt] != nil {
				seen[r.F.Cmt] = true
				out = append(out, r.F.Cmt)
			}
		}
	}
	return out
}

// SelectComment ставит курсор в начало примечания.
func (e *Editor) SelectComment(id string) bool {
	for _, en := range e.flat {
		pos := 0
		for _, r := range en.P.Runs {
			if r.F.Cmt == id {
				e.Sel = Sel{Pos{P: en.P, O: pos}, Pos{P: en.P, O: pos}}
				e.Doc.activeCmt = id
				return true
			}
			pos += r.Len()
		}
	}
	return false
}
