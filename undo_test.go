//go:build !windows

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"testing"
)

func docSig(t *testing.T, d *Doc) string {
	b, err := WriteDocx(d)
	if err != nil {
		t.Fatalf("WriteDocx: %v", err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	var sb strings.Builder
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".xml") && !strings.Contains(f.Name, "docProps") {
			rc, _ := f.Open()
			x, _ := io.ReadAll(rc)
			rc.Close()
			sb.WriteString("\n== " + f.Name + "\n")
			sb.Write(x)
		}
	}
	return sb.String()
}

func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return fmt.Sprintf("…%s\n   ≠\n…%s", a[max(0, i-200):min(len(a), i+200)], b[max(0, i-200):min(len(b), i+200)])
}

// Любая цепочка правок, отменённая до конца, возвращает документ в исходное состояние,
// а сохранённый после правок docx читается обратно с тем же текстом.
func TestUndoRestoresDocument(t *testing.T) {
	seeds := 60
	if v := os.Getenv("UNDON"); v != "" {
		fmt.Sscan(v, &seeds)
	}
	groups := map[string]bool{"Текст": true, "Абзац": true, "Стиль": true, "Таблица": true, "Вставка": true, "Колонтитулы": true,
		"Страница": true, "Ссылки": true, "Проверка": true}
	skip := map[string]bool{"picture": true, "spellon": true, "markup": true, "stats": true, "painter": true}
	keys := []int{37, 38, 39, 40, 36, 35, 8, 46, 13, 9, 27}
	ctrl := []int{66, 73, 85, 65, 88, 86, 67, 69, 76, 82, 74, 77, 49, 50, 53, 13, 8, 46, 187, 32}
	letters := []rune("абв гд.Fx1 ")
	fails := 0
	for s := 0; s < seeds && fails < 6; s++ {
		rng := rand.New(rand.NewSource(int64(s)))
		a := NewApp(newTTFText(), &fakePlatform{})
		a.cfgDir = ""
		a.Set = defaultSettings()
		a.BaseS, a.W, a.H = 1, 1366, 860
		if s%2 == 0 {
			a.newTab(featuresDoc(), "t.docx", "")
		}
		a.backstage = false
		cv := newCanvas(1366, 860)
		a.Paint(cv)
		e := a.E
		// случайное место курсора
		for i := rng.Intn(40); i > 0; i-- {
			a.KeyDown(40, Mods{})
		}
		e.undo, e.redo = nil, nil
		track0 := e.Doc.Track
		sig0 := docSig(t, e.Doc)
		var log []string
		n := 3 + rng.Intn(25)
		for i := 0; i < n; i++ {
			switch k := rng.Intn(10); {
			case k < 3:
				r := letters[rng.Intn(len(letters))]
				log = append(log, fmt.Sprintf("ch %q", r))
				a.Char(r)
			case k < 5:
				vk := keys[rng.Intn(len(keys))]
				m := Mods{Shift: rng.Intn(3) == 0}
				log = append(log, fmt.Sprintf("key %d %v", vk, m.Shift))
				a.KeyDown(vk, m)
			case k < 6:
				vk := ctrl[rng.Intn(len(ctrl))]
				m := Mods{Ctrl: true, Shift: rng.Intn(4) == 0}
				log = append(log, fmt.Sprintf("ctrl %d %v", vk, m.Shift))
				a.KeyDown(vk, m)
			default:
				a.refreshActions()
				ac := a.acts[rng.Intn(len(a.acts))]
				if !groups[ac.group] || skip[ac.id] || ac.off != "" {
					continue
				}
				log = append(log, "act "+ac.id)
				ac.act()
			}
			if a.popup != nil {
				a.popup = nil
			}
			if a.dialog != nil {
				vk := []int{27, 13}[rng.Intn(2)]
				log = append(log, fmt.Sprintf("dlg %d", vk))
				a.KeyDown(vk, Mods{})
				a.dialog = nil
			}
			if a.E != e {
				break
			}
		}
		if a.E != e || len(e.undo) >= 90 {
			continue
		}
		// сохранение и чтение
		for _, ext := range strings.Fields(saveExts) {
			if out, err := SaveBytes(e.Doc, ext); err != nil {
				fails++
				t.Errorf("seed %d: не сохраняется в %s: %v\n%s", s, ext, err, strings.Join(log, " | "))
			} else if d2, err := OpenBytes("x."+ext, out); err != nil {
				fails++
				t.Errorf("seed %d: сохранённый %s не читается: %v\n%s", s, ext, err, strings.Join(log, " | "))
			} else {
				norm := func(d *Doc) string { return strings.Join(strings.Fields(BlocksToText(d.Body)), " ") }
				if x, y := norm(e.Doc), norm(d2); x != y {
					fails++
					t.Errorf("seed %d: текст после %s другой\n%s\n%s", s, ext, firstDiff(x, y), strings.Join(log, " | "))
				}
			}
		}
		if e.Story != "body" {
			e.SetStory("body", 0)
		}
		for guard := 0; len(e.undo) > 0 && guard < 500; guard++ {
			e.Undo()
		}
		if e.Story != "body" { // отмена могла вернуть в колонтитул — выходим из него, как сделал бы человек
			e.SetStory("body", 0)
		}
		e.Doc.Track = track0 // режим исправлений не отменяется (как и в Word)
		if sig := docSig(t, e.Doc); sig != sig0 {
			fails++
			t.Errorf("seed %d: после полной отмены документ другой\n%s\nдействия: %s", s, firstDiff(sig0, sig), strings.Join(log, " | "))
		}
	}
}

// saveExts — форматы, в которых текст обязан пережить сохранение (RTF теряет поля и формулы — см. ARCHITECTURE.md).
var saveExts = func() string {
	if v := os.Getenv("SAVEEXTS"); v != "" {
		return v
	}
	return "docx odt"
}()
