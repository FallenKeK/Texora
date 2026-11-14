//go:build !windows

package main

import (
	"strings"
	"testing"
)

func TestBibImportFromClipboard(t *testing.T) {
	fp := &fakePlatform{}
	a := NewApp(newTTFText(), fp)
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.BaseS, a.W, a.H = 1, 1366, 860
	a.backstage = false
	fp.clip = ClipData{Text: sampleBib + sampleBib} // дважды: повторы не добавляются
	a.refreshActions()
	a.actionsByID()["bibpaste"].act()
	if n := len(a.E.Doc.Sources); n != 4 {
		t.Fatalf("источников %d, ждали 4", n)
	}
	a.E.InsertBibliography()
	text := BlocksToText(a.E.Doc.Body)
	for _, want := range []string{"Сидоров, А. А. Основы радиоэлектроники / А. А. Сидоров. – Москва : Наука, 2018. – 320 с.",
		"ГОСТ Р 7.0.100–2018. Система стандартов"} {
		if !strings.Contains(text, want) {
			t.Errorf("в списке литературы нет %q:\n%s", want, text)
		}
	}
	a.Paint(newCanvas(1366, 860))
}
