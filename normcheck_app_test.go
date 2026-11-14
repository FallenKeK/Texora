//go:build !windows

package main

import "testing"

func TestNormPanel(t *testing.T) {
	a := NewApp(newTTFText(), &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.BaseS, a.W, a.H = 1, 1366, 860
	d := normTestDoc()
	findPara(d, "Работа").PF.Line = O(1.15)
	findPara(d, "По результатам").PF.Line = O(1)
	a.newTab(d, "Курсовая.docx", "")
	a.backstage = false
	a.refreshActions()
	a.actionsByID()["normcheck"].act()
	if a.inspTab() != "review" {
		t.Errorf("вкладка %q, ждали «Проверка»", a.inspTab())
	}
	cv := newCanvas(1366, 860)
	a.Paint(cv)
	found := false
	for _, h := range a.hits {
		if h.id == "x:norm:line" {
			found = true
		}
	}
	if !found {
		t.Fatalf("в панели нет группы «Интервал»")
	}
	a.normGoto("line")
	if a.E.Sel.F.P != findPara(a.E.Doc, "Работа") {
		t.Errorf("курсор не перешёл к первому месту")
	}
	a.normGoto("line")
	if a.E.Sel.F.P != findPara(a.E.Doc, "По результатам") {
		t.Errorf("курсор не перешёл ко второму месту")
	}
	a.normFix("line")
	a.Paint(cv)
	if gs := normGroups(a.normIssues()); len(gs) != 0 {
		t.Errorf("после исправления осталось %v", gs)
	}
	a.E.Undo()
	a.Paint(cv)
	if gs := normGroups(a.normIssues()); len(gs) != 1 || gs[0].n != 2 {
		t.Errorf("после отмены %v, ждали интервал ×2", gs)
	}
}
