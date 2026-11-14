package main

import (
	"path/filepath"
	"testing"
)

func TestIndexFontFile(t *testing.T) {
	files, _ := filepath.Glob("/usr/share/fonts/truetype/*/*.tt[fc]")
	if len(files) == 0 {
		t.Skip("нет системных шрифтов")
	}
	got := map[string][]sysFontFace{}
	for _, f := range files {
		indexFontFile(f, func(fam string, face sysFontFace) { got[fam] = append(got[fam], face) })
	}
	faces := got["liberation serif"]
	if len(faces) != 4 {
		t.Fatalf("Liberation Serif: ждали 4 начертания, нашли %v", faces)
	}
	styles := map[[2]bool]bool{}
	for _, fc := range faces {
		styles[[2]bool{fc.bold, fc.italic}] = true
		if parseTTF(mustRead(t, fc.path), fc.index) == nil {
			t.Errorf("%s не разбирается", fc.path)
		}
	}
	if len(styles) != 4 {
		t.Errorf("начертания не различаются: %v", faces)
	}
	// коллекция .ttc: у второго шрифта свой индекс
	for fam, fs := range got {
		for _, fc := range fs {
			if fc.index > 0 {
				if f := parseTTF(mustRead(t, fc.path), fc.index); f == nil || f.cmap('A') == 0 {
					t.Errorf("%s #%d (%s) не разбирается", fc.path, fc.index, fam)
				}
				return
			}
		}
	}
	t.Log("ttc с несколькими шрифтами не найден")
}
