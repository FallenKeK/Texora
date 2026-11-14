package main

// Панель нормоконтроля во вкладке «Проверка» инспектора: группы замечаний со счётчиком,
// щелчок — к следующему месту, «Исправить» — исправить всю группу (одним шагом отмены).

import (
	"math"
	"strconv"
)

type normState struct {
	on     bool
	doc    *Doc
	lay    *Layout // вёрстка, по которой считали (после правки — пересчёт)
	issues []NormIssue
	next   map[string]int
}

// runNormCheck — команда «Нормоконтроль».
func (a *App) runNormCheck() {
	e := a.E
	a.norm = normState{on: true, doc: e.Doc, lay: e.L, issues: NormCheck(e.Doc), next: map[string]int{}}
	a.tab = "review"
	if a.Set.HideInspector {
		a.Set.HideInspector = false
		a.saveSettings()
	}
	if n := len(a.norm.issues); n == 0 {
		a.msg(a.tr("Нормоконтроль: замечаний нет"))
	} else {
		a.msg(a.trf("Нормоконтроль: замечаний — %d", n))
	}
	a.invalidate()
}

// normIssues — замечания по текущему документу (после правок пересчитываются).
func (a *App) normIssues() []NormIssue {
	n := &a.norm
	if !n.on || a.E == nil || n.doc != a.E.Doc {
		return nil
	}
	if n.lay != a.E.L {
		n.issues, n.lay = NormCheck(a.E.Doc), a.E.L
	}
	return n.issues
}

type normGroup struct {
	k NormKind
	n int
}

func normGroups(is []NormIssue) []normGroup {
	cnt := map[string]int{}
	for _, i := range is {
		cnt[i.Kind]++
	}
	var out []normGroup
	for _, err := range []bool{true, false} { // сначала нарушения, потом советы
		for _, k := range normKinds {
			if k.Err == err && cnt[k.ID] > 0 {
				out = append(out, normGroup{k, cnt[k.ID]})
			}
		}
	}
	return out
}

// normGoto — к следующему месту группы kind.
func (a *App) normGoto(kind string) {
	is := a.normIssues()
	var list []NormIssue
	for _, i := range is {
		if i.Kind == kind {
			list = append(list, i)
		}
	}
	if len(list) == 0 {
		return
	}
	k := a.norm.next[kind] % len(list)
	a.norm.next[kind] = k + 1
	it := list[k]
	tip := a.tr(normKind(kind).Title)
	if it.Msg != "" {
		tip += ": " + a.tr(it.Msg)
	}
	if len(list) > 1 {
		tip += " (" + strconv.Itoa(k+1) + "/" + strconv.Itoa(len(list)) + ")"
	}
	p := it.P
	if p == nil && it.Sec >= 0 {
		// замечание о листе — к началу раздела
		ss := a.E.Doc.Sections()
		if it.Sec < len(ss) {
			for j := ss[it.Sec].From; j < ss[it.Sec].To && p == nil; j++ {
				p = a.E.Doc.Body[j].P
			}
		}
	}
	if p != nil {
		e := a.E
		if e.Story != "body" {
			e.SetStory("body", 0)
		}
		e.SetCaret(Pos{P: p}, false)
		e.SelectPara(Pos{P: p})
		a.ScrollToCaret()
		a.after()
	}
	a.msg(tip)
}

// normFix — исправить всю группу.
func (a *App) normFix(kind string) {
	e := a.E
	is := a.normIssues()
	e.Checkpoint("")
	n := NormFix(e.Doc, is, kind)
	e.Changed()
	a.norm.issues, a.norm.lay = NormCheck(e.Doc), e.L
	a.msg(a.trf("Исправлено мест: %d", n))
	a.after()
}

// normPanel — список групп в инспекторе.
func (a *App) normPanel(s *inspector) {
	if !a.norm.on || a.norm.doc != a.E.Doc {
		return
	}
	th := a.th
	gs := normGroups(a.normIssues())
	if len(gs) == 0 {
		s.custom(a.d(24), func(r Rect) {
			a.icon(s.cv, "check", Rect{r.X, r.Y + a.d(2), a.d(18), a.d(18)}, th.Accent2)
			a.textAt(s.cv, "Замечаний нет", r.X+a.d(26), r.Y+a.d(3), a.uiFont(12.5), th.Text)
		})
		return
	}
	for _, g := range gs {
		g := g
		s.custom(a.d(40), func(r Rect) {
			r = Rect{r.X - a.d(6), r.Y, r.W + a.d(12), r.H}
			fixW := 0.0
			if g.k.Fix {
				fixW = a.d(74)
			}
			main := Rect{r.X, r.Y, r.W - fixW, r.H}
			id := "x:norm:" + g.k.ID
			a.hits = append(a.hits, hit{id: id, r: main, act: func() { a.normGoto(g.k.ID) }, tip: a.tr("К следующему месту")})
			a.hoverBg(s.cv, id, main, a.d(5))
			dot := th.TextDim
			if g.k.Err {
				dot = th.Accent2
			}
			s.cv.FillCircle(r.X+a.d(11), r.Y+a.d(13), a.d(3.5), dot)
			f := a.uiFont(12)
			clip := s.cv.clip
			s.cv.SetClip(intersect(Rect{main.X, main.Y, main.W - a.d(4), main.H}, clip))
			a.textAt(s.cv, g.k.Title, r.X+a.d(22), r.Y+a.d(4), f, th.Text)
			sub := a.trf("мест: %d", g.n)
			if !g.k.Err {
				sub += " · " + a.tr("совет")
			}
			a.textAt(s.cv, sub, r.X+a.d(22), r.Y+a.d(21), a.uiFont(10.5), th.TextDim)
			s.cv.SetClip(clip)
			if g.k.Fix {
				br := Rect{r.X + r.W - fixW + a.d(4), r.Y + a.d(7), fixW - a.d(10), r.H - a.d(14)}
				fid := "x:normfix:" + g.k.ID
				a.hits = append(a.hits, hit{id: fid, r: br, act: func() { a.normFix(g.k.ID) }, tip: a.tr("Исправить все места этой группы")})
				s.cv.StrokeRound(br, a.d(5), math.Max(1, a.S), th.Border)
				a.hoverBg(s.cv, fid, br, a.d(5))
				a.centerText(s.cv, "Исправить", br, a.uiFont(11), th.Text)
			}
		})
	}
}
