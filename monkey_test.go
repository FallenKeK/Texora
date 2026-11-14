//go:build !windows

package main

import (
	"fmt"
	"math/rand"
	"os"
	"runtime/debug"
	"strings"
	"testing"
)

// «Обезьяна»: случайные клавиши, клики и команды. Паника или зависание — ошибка.
func monkeyRun(t *testing.T, seed int64, steps int, doc func() *Doc) (fail string) {
	rng := rand.New(rand.NewSource(seed))
	a := NewApp(newTTFText(), &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.BaseS, a.W, a.H = 1, 1366, 860
	if doc != nil {
		a.newTab(doc(), "t.docx", "")
	}
	a.backstage = false
	cv := newCanvas(1366, 860)
	a.Paint(cv)
	var log []string
	defer func() {
		if r := recover(); r != nil {
			n := len(log)
			fail = fmt.Sprintf("seed %d: паника: %v\nпоследние действия: %s\n%s", seed, r, strings.Join(log[max(0, n-25):], " | "), debug.Stack())
		}
	}()
	keys := []int{37, 38, 39, 40, 33, 34, 36, 35, 8, 46, 13, 9, 27, 120, 118}
	letters := []rune("абв гдеё ж.,-Fx12 \t")
	ctrl := []int{66, 73, 85, 90, 89, 65, 67, 88, 86, 69, 76, 82, 74, 77, 49, 50, 53, 13, 36, 35, 37, 39, 8, 46, 187, 32, 75}
	for i := 0; i < steps; i++ {
		switch k := rng.Intn(100); {
		case k < 30:
			r := letters[rng.Intn(len(letters))]
			log = append(log, fmt.Sprintf("ch %q", r))
			a.Char(r)
		case k < 50:
			vk := keys[rng.Intn(len(keys))]
			m := Mods{Shift: rng.Intn(4) == 0}
			log = append(log, fmt.Sprintf("key %d %v", vk, m))
			a.KeyDown(vk, m)
		case k < 60:
			vk := ctrl[rng.Intn(len(ctrl))]
			m := Mods{Ctrl: true, Shift: rng.Intn(4) == 0}
			log = append(log, fmt.Sprintf("ctrl %d %v", vk, m))
			a.KeyDown(vk, m)
		case k < 75:
			x, y := rng.Float64()*a.W, rng.Float64()*a.H
			right := rng.Intn(8) == 0
			log = append(log, fmt.Sprintf("click %.0f,%.0f r=%v", x, y, right))
			a.MouseMove(x, y, false)
			a.MouseDown(x, y, Mods{Shift: rng.Intn(6) == 0}, right)
			if rng.Intn(3) == 0 {
				x2, y2 := rng.Float64()*a.W, rng.Float64()*a.H
				a.MouseMove(x2, y2, true)
				a.MouseUp(x2, y2)
			} else {
				a.MouseUp(x, y)
			}
		case k < 80:
			d := float64(rng.Intn(7)-3) * 120
			log = append(log, fmt.Sprintf("wheel %.0f", d))
			a.Wheel(d, Mods{Ctrl: rng.Intn(5) == 0})
		case k < 97:
			a.refreshActions()
			ac := a.acts[rng.Intn(len(a.acts))]
			if ac.id == "closetab" && rng.Intn(3) != 0 {
				continue
			}
			log = append(log, "act "+ac.id)
			if ac.off == "" {
				ac.act()
			}
		default:
			log = append(log, "tick")
			a.Tick()
		}
		if a.dialog != nil && rng.Intn(3) == 0 {
			vk := []int{27, 13, 9}[rng.Intn(3)]
			log = append(log, fmt.Sprintf("dlg %d", vk))
			a.KeyDown(vk, Mods{})
		}
		if i%3 == 0 {
			a.Paint(cv)
		}
		if len(a.tabs) > 6 {
			a.tabs = a.tabs[:1]
			a.cur = -1
			a.activate(0)
		}
	}
	a.Paint(cv)
	return ""
}

func TestMonkey(t *testing.T) {
	seeds, steps := 4, 250
	if os.Getenv("MONKEY") != "" {
		fmt.Sscan(os.Getenv("MONKEY"), &seeds, &steps)
	}
	docs := map[string]func() *Doc{"пустой": nil, "features": featuresDoc}
	for _, f := range []string{"testdata/cap.docx", "testdata/form.docx", "testdata/feat.docx"} {
		if data, err := os.ReadFile(f); err == nil {
			docs[f] = func() *Doc { d, _ := OpenBytes(f, data); return d }
		}
	}
	fails := 0
	for name, mk := range docs {
		for s := 0; s < seeds && fails < 8; s++ {
			if msg := monkeyRun(t, int64(s), steps, mk); msg != "" {
				fails++
				t.Errorf("%s: %s", name, msg)
			}
		}
	}
}
