//go:build !windows

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"runtime/debug"
	"testing"
	"time"
)

// Порченые файлы: чтение должно вернуть документ или ошибку, но не упасть и не зависнуть.
func mutate(rng *rand.Rand, b []byte) []byte {
	out := append([]byte(nil), b...)
	if len(out) == 0 {
		return out
	}
	for n := 1 + rng.Intn(6); n > 0; n-- {
		i := rng.Intn(len(out))
		switch rng.Intn(6) {
		case 0:
			out[i] = byte(rng.Intn(256))
		case 1: // вырезать кусок
			j := min(len(out), i+1+rng.Intn(200))
			out = append(out[:i], out[j:]...)
		case 2: // повторить кусок
			j := min(len(out), i+1+rng.Intn(200))
			out = append(out[:j], append(append([]byte(nil), out[i:j]...), out[j:]...)...)
		case 3: // обрезать хвост
			out = out[:i]
		case 4: // испортить число
			for k := i; k < len(out) && k < i+40; k++ {
				if out[k] >= '0' && out[k] <= '9' {
					out[k] = "0999999999"[rng.Intn(10)]
					if rng.Intn(2) == 0 {
						out = append(out[:k], append([]byte("-99999999999"), out[k:]...)...)
					}
					break
				}
			}
		case 5: // сломать тег
			for k := i; k < len(out) && k < i+60; k++ {
				if out[k] == '<' || out[k] == '>' || out[k] == '"' || out[k] == '{' || out[k] == '}' || out[k] == '\\' {
					out[k] = ' '
					break
				}
			}
		}
		if len(out) == 0 {
			break
		}
	}
	return out
}

func rezip(t *testing.T, data []byte, rng *rand.Rand) []byte {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return mutate(rng, data)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	pick := rng.Intn(len(zr.File))
	for i, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if i == pick || (rng.Intn(4) == 0 && (bytes.HasSuffix([]byte(f.Name), []byte(".xml")) || bytes.HasSuffix([]byte(f.Name), []byte(".rels")))) {
			b = mutate(rng, b)
		}
		if rng.Intn(40) == 0 {
			continue // часть пропала
		}
		w, _ := zw.Create(f.Name)
		w.Write(b)
	}
	zw.Close()
	return buf.Bytes()
}

func tryOpen(name string, data []byte) (msg string) {
	done := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Sprintf("паника: %v\n%s", r, debug.Stack())
			}
		}()
		if len(name) > 4 && name[len(name)-4:] == ".pdf" {
			d, err := LoadPDF(data)
			if err == nil {
				for i := 0; i < len(d.Pages) && i < 2; i++ {
					d.RenderPage(i, 0.4, false)
				}
			}
		} else {
			d, err := OpenBytes(name, data)
			if err == nil && d != nil {
				tb := newTTFText()
				L := NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{})
				_ = L
				for _, ext := range []string{"docx", "odt", "rtf"} {
					SaveBytes(d, ext)
				}
				ExportPDFVector(d, L, tb, nil)
			}
		}
		done <- ""
	}()
	select {
	case m := <-done:
		return m
	case <-time.After(20 * time.Second):
		return "зависание больше 20 с"
	}
}

func TestCorruptFiles(t *testing.T) {
	n := 15
	if v := os.Getenv("FUZZN"); v != "" {
		fmt.Sscan(v, &n)
	}
	type src struct {
		name string
		data []byte
		zip  bool
	}
	var srcs []src
	for _, f := range []string{"testdata/cap.docx", "testdata/form.docx", "testdata/feat.docx"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		srcs = append(srcs, src{f, data, true})
		if d, err := OpenBytes(f, data); err == nil {
			if b, err := SaveBytes(d, "odt"); err == nil {
				srcs = append(srcs, src{f + ".odt", b, true})
			}
			if b, err := SaveBytes(d, "rtf"); err == nil {
				srcs = append(srcs, src{f + ".rtf", b, false})
			}
			tb := newTTFText()
			if b, err := ExportPDFVector(d, NewMeasurer(tb).Paginate(d, map[*Para]*ParaL{}), tb, nil); err == nil {
				srcs = append(srcs, src{f + ".pdf", b, false})
			}
		}
	}
	if data, err := os.ReadFile("testdata/scan600.pdf"); err == nil {
		srcs = append(srcs, src{"scan600.pdf", data, false})
	}
	fails := 0
	for _, s := range srcs {
		rng := rand.New(rand.NewSource(fuzzSeed))
		for i := 0; i < n && fails < 10; i++ {
			var b []byte
			if s.zip {
				b = rezip(t, s.data, rng)
			} else {
				b = mutate(rng, s.data)
			}
			if msg := tryOpen(s.name, b); msg != "" {
				fails++
				os.MkdirAll("/tmp/claude-0/mk", 0o755)
				p := fmt.Sprintf("/tmp/claude-0/mk/fail_%d_%s", fails, baseName(s.name))
				os.WriteFile(p, b, 0o644)
				t.Errorf("%s #%d (%s): %s", s.name, i, p, firstLines(msg, 14))
			}
		}
	}
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func firstLines(s string, n int) string {
	out, c := "", 0
	for _, ch := range s {
		if ch == '\n' {
			c++
			if c >= n {
				break
			}
		}
		out += string(ch)
	}
	return out
}

func TestOpenSavedFailures(t *testing.T) {
	files, _ := os.ReadDir("/tmp/claude-0/mk")
	for _, f := range files {
		data, _ := os.ReadFile("/tmp/claude-0/mk/" + f.Name())
		if msg := tryOpen(f.Name(), data); msg != "" {
			t.Errorf("%s: %s", f.Name(), firstLines(msg, 12))
		}
	}
}

var fuzzSeed = func() int64 {
	var s int64 = 1
	fmt.Sscan(os.Getenv("FUZZSEED"), &s)
	return s
}()
