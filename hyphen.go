package main

// Автоматические переносы: алгоритм Лианга по шаблонам LibreOffice (hyph_ru_RU, hyph_en_US).

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

//go:embed assets/dict/*.gz
var dictFS embed.FS

func dictData(name string) []byte {
	b, err := dictFS.ReadFile("assets/dict/" + name)
	if err != nil {
		return nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	out, _ := io.ReadAll(zr)
	return out
}

type hyphenator struct {
	pats        map[string][]uint8
	maxLen      int
	left, right int
}

var (
	hyphOnce sync.Once
	hyphRu   *hyphenator
	hyphEn   *hyphenator
)

func loadHyph(name string, left, right int) *hyphenator {
	h := &hyphenator{pats: map[string][]uint8{}, left: left, right: right}
	sc := bufio.NewScanner(bytes.NewReader(dictData(name)))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			continue // кодировка
		}
		if line == "" || strings.HasPrefix(line, "%") || strings.HasPrefix(line, "#") || strings.Contains(line, "/") {
			continue
		}
		if f := strings.Fields(line); len(f) == 2 {
			v, err := strconv.Atoi(f[1])
			if err == nil {
				switch f[0] {
				case "LEFTHYPHENMIN":
					h.left = v
				case "RIGHTHYPHENMIN":
					h.right = v
				}
				continue
			}
		}
		if strings.HasPrefix(line, "NEXTLEVEL") || strings.HasPrefix(line, "COMPOUND") || strings.HasPrefix(line, "NOHYPHEN") {
			continue
		}
		var letters []rune
		digits := []uint8{0}
		for _, r := range line {
			if r >= '0' && r <= '9' {
				digits[len(digits)-1] = uint8(r - '0')
			} else {
				letters = append(letters, unicode.ToLower(r))
				digits = append(digits, 0)
			}
		}
		key := string(letters)
		h.pats[key] = digits
		if len(letters) > h.maxLen {
			h.maxLen = len(letters)
		}
	}
	return h
}

func hyphLoad() {
	hyphOnce.Do(func() {
		hyphRu = loadHyph("hyph_ru.gz", 2, 2)
		hyphEn = loadHyph("hyph_en.gz", 2, 3)
	})
}

// Hyphenate возвращает позиции, где слово можно разорвать (перенос перед символом с этим индексом).
func Hyphenate(w []rune) []int {
	if len(w) < 4 {
		return nil
	}
	hyphLoad()
	h := hyphEn
	for _, r := range w {
		if unicode.Is(unicode.Cyrillic, r) {
			h = hyphRu
			break
		}
	}
	if h == nil || len(h.pats) == 0 {
		return nil
	}
	low := make([]rune, 0, len(w)+2)
	low = append(low, '.')
	for _, r := range w {
		low = append(low, unicode.ToLower(r))
	}
	low = append(low, '.')
	pts := make([]uint8, len(low)+1)
	for i := 0; i < len(low); i++ {
		for l := 1; l <= h.maxLen && i+l <= len(low); l++ {
			if d, ok := h.pats[string(low[i:i+l])]; ok {
				for k, v := range d {
					if v > pts[i+k] {
						pts[i+k] = v
					}
				}
			}
		}
	}
	var out []int
	for i := h.left; i <= len(w)-h.right; i++ {
		// pts[i+1] — значение между low[i] и low[i+1], т.е. между w[i-1] и w[i]
		if pts[i+1]%2 == 1 {
			// не отрываем мягкий/твёрдый знак и «й» от предыдущей буквы
			if strings.ContainsRune("ьъйЬЪЙ", w[i]) {
				continue
			}
			out = append(out, i)
		}
	}
	return out
}
