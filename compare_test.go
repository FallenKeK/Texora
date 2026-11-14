//go:build !windows

package main

import (
	"os"
	"strings"
	"testing"
)

func docText(d *Doc) string { return strings.Join(strings.Fields(BlocksToText(d.Body)), " ") }

// resolveAll принимает или отклоняет все исправления в копии документа.
func resolveAll(t *testing.T, d *Doc, accept bool) *Doc {
	t.Helper()
	a := NewApp(newTTFText(), &fakePlatform{})
	a.cfgDir = ""
	a.newTab(d.Clone(), "x.docx", "")
	a.E.ResolveRevisions(accept, true)
	return a.E.Doc
}

func checkCompare(t *testing.T, name string, oldD, newD *Doc) *Doc {
	t.Helper()
	// эталоны — документы с уже принятыми прежними исправлениями (так их и сравнивает Texora)
	oldD, newD = oldD.Clone(), newD.Clone()
	acceptRevisions(oldD)
	acceptRevisions(newD)
	res, st := CompareDocs(oldD, newD, "Сравнение")
	if got, want := docText(resolveAll(t, res, true)), docText(newD); got != want {
		t.Errorf("%s: «принять все» ≠ новая версия\n%s", name, firstDiff(want, got))
	}
	if got, want := docText(resolveAll(t, res, false)), docText(oldD); got != want {
		t.Errorf("%s: «отклонить все» ≠ старая версия\n%s", name, firstDiff(want, got))
	}
	t.Logf("%s: вставлено слов %d, удалено %d, абзацев +%d −%d", name, st.InsWords, st.DelWords, st.InsParas, st.DelParas)
	return res
}

func TestCompareWords(t *testing.T) {
	oldD := docOf(para("Мама мыла раму.", PF{}, tnr14), para("Второй абзац остаётся.", PF{}, tnr14), para("Этот абзац удалят целиком.", PF{}, tnr14))
	newD := docOf(para("Мама вымыла большую раму!", PF{}, tnr14), para("Новый абзац вставлен.", PF{}, tnr14), para("Второй абзац остаётся.", PF{}, tnr14))
	res := checkCompare(t, "простой", oldD, newD)
	var ins, del []string
	for _, b := range res.Body {
		for _, r := range b.P.Runs {
			switch {
			case r.F.Ins != "":
				ins = append(ins, string(r.Text))
			case r.F.Del != "":
				del = append(del, string(r.Text))
			}
		}
	}
	if j := strings.Join(ins, "|"); !strings.Contains(j, "вымыла") || !strings.Contains(j, "большую") || !strings.Contains(j, "Новый абзац вставлен.") {
		t.Errorf("вставки: %q", j)
	}
	if j := strings.Join(del, "|"); !strings.Contains(j, "мыла") || !strings.Contains(j, "Этот абзац удалят целиком.") || strings.Contains(j, "раму") {
		t.Errorf("удаления: %q", j)
	}
	// одинаковые документы — ни одной правки
	if _, st := CompareDocs(oldD, oldD, "x"); st.InsWords+st.DelWords != 0 {
		t.Errorf("одинаковые документы дали отличия: %+v", st)
	}
	// результат переживает docx и открывается с теми же правками
	out, err := SaveBytes(res, "docx")
	if err != nil {
		t.Fatal(err)
	}
	d2, err := OpenBytes("x.docx", out)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := docText(resolveAll(t, d2, false)), docText(oldD); got != want {
		t.Errorf("после docx «отклонить все» ≠ старая версия\n%s", firstDiff(want, got))
	}
	if got, want := docText(resolveAll(t, d2, true)), docText(newD); got != want {
		t.Errorf("после docx «принять все» ≠ новая версия\n%s", firstDiff(want, got))
	}
}

func TestCompareRealCourseworks(t *testing.T) {
	var docs []*Doc
	for _, f := range []string{"/tmp/claude-0/kurs/k2.docx", "/tmp/claude-0/kurs/k3.docx", "testdata/feat.docx", "testdata/cap.docx"} {
		if data, err := os.ReadFile(f); err == nil {
			if d, err := OpenBytes(f, data); err == nil {
				docs = append(docs, d)
			}
		}
	}
	if len(docs) < 2 {
		t.Skip("нет образцов")
	}
	for i := 0; i+1 < len(docs); i++ {
		checkCompare(t, "образцы", docs[i], docs[i+1])
	}
	// небольшая правка внутри большого документа находится точечно
	d := docs[0]
	edited := d.Clone()
	for _, b := range edited.Body {
		if b.P != nil && len(b.P.Text()) > 200 {
			b.P.Runs = append(b.P.Runs, Run{T: RText, Text: []rune(" Добавленная фраза."), F: b.P.Runs[0].F})
			break
		}
	}
	_, st := CompareDocs(d, edited, "x")
	if st.InsWords != 2 || st.DelWords != 0 {
		t.Errorf("одна вставка из двух слов найдена как %+v", st)
	}
}
