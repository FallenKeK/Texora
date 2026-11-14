//go:build !windows

package main

import "testing"

// Сценарии те же, что `go run . interactive|features|captions|eqtest`, но с проверкой результата.
// Нужны шрифты из ttf.go, поэтому только не на Windows. Картинки сценарии пишут в /tmp.

func TestInteractive(t *testing.T) {
	if n := runInteractive(newTTFText()); n != 0 {
		t.Errorf("провалено проверок правки: %d (подробности выше)", n)
	}
}

func TestFeaturesRoundTrip(t *testing.T) {
	if n := runFeatures(newTTFText()); n != 0 {
		t.Errorf("docx/odt не пережили круговое сохранение: %d", n)
	}
}

func TestCaptionsRoundTrip(t *testing.T) {
	if n := runCaptions(newTTFText()); n != 0 {
		t.Errorf("номера названий или ссылки разошлись после чтения: %d", n)
	}
}

func TestEqLayoutAndExchange(t *testing.T) {
	if n := runEqTest(newTTFText()); n != 0 {
		t.Errorf("формул с потерями при обмене: %d", n)
	}
}
