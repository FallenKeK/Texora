//go:build windows

package main

// Поиск файла шрифта по имени семейства — для вшивания шрифтов в PDF.
// Читаются только заголовки файлов (каталог таблиц, name, OS/2, head), а не файлы целиком.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	sysFontsOnce sync.Once
	sysFonts     map[string][]sysFontFace // семейство в нижнем регистре → начертания
)

func loadSysFonts() {
	sysFonts = map[string][]sysFontFace{}
	dirs := []string{filepath.Join(os.Getenv("WINDIR"), "Fonts")}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		dirs = append(dirs, filepath.Join(la, "Microsoft", "Windows", "Fonts"))
	}
	for _, dir := range dirs {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case ".ttf", ".ttc", ".otf":
				indexFontFile(filepath.Join(dir, e.Name()), func(fam string, face sysFontFace) {
					sysFonts[fam] = append(sysFonts[fam], face)
				})
			}
		}
	}
}

// systemFont — файл начертания; если нужного начертания нет, берётся обычное, а жирность или курсив
// дорисовываются в PDF (synB, synI), как это делает и Windows на экране.
func systemFont(family string, bold, italic bool) (path string, index int, synB, synI, ok bool) {
	sysFontsOnce.Do(loadSysFonts)
	faces := sysFonts[strings.ToLower(strings.TrimSpace(family))]
	if len(faces) == 0 {
		return "", 0, false, false, false
	}
	best, bestScore := faces[0], -1
	for _, fc := range faces {
		score := 0
		if fc.bold == bold {
			score += 2
		} else if fc.bold {
			score -= 3 // жирный вместо обычного хуже, чем дорисованный жирный
		}
		if fc.italic == italic {
			score++
		} else if fc.italic {
			score -= 3
		}
		if score > bestScore {
			best, bestScore = fc, score
		}
	}
	return best.path, best.index, bold && !best.bold, italic && !best.italic, true
}
