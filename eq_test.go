package main

import "testing"

// Формула должна пережить экспорт в OMML (Word) и MathML (ODT) и чтение обратно без потерь:
// сравниваем не текст, а то, что из формулы снова получится в OMML (начертание, пробелы, номер, окружения).
func TestEqRoundTrip(t *testing.T) {
	for i, src := range eqTestSet {
		want := omml(ParseEq(src))

		root, err := ParseXML([]byte(`<m:oMath xmlns:m="m" xmlns:w="w">` + want + `</m:oMath>`))
		if err != nil {
			t.Fatalf("%d: OMML не разбирается: %v", i+1, err)
		}
		if back := OMMLToLinear(root); omml(ParseEq(back)) != want {
			t.Errorf("%d: OMML теряет данные\n было: %s\nстало: %s", i+1, src, back)
		}

		mm, err := ParseXML([]byte(EqMathML(src)))
		if err != nil {
			t.Fatalf("%d: MathML не разбирается: %v", i+1, err)
		}
		if back := MathMLToLinear(mm); omml(ParseEq(back)) != want {
			t.Errorf("%d: MathML теряет данные\n было: %s\nстало: %s", i+1, src, back)
		}
	}
}

func TestEqPlainNoDoubleParens(t *testing.T) {
	for _, src := range []string{`(a+b)^2`, `{(a+b)}^{2}`} {
		if got := EqPlain(src); got != "(a + b)²" {
			t.Errorf("EqPlain(%q) = %q, want %q", src, got, "(a + b)²")
		}
	}
}
