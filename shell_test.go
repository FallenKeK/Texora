//go:build !windows

package main

import "testing"

func newShellApp(t *testing.T) *App {
	a := NewApp(newTTFText(), &fakePlatform{})
	a.cfgDir = ""
	a.Set = defaultSettings()
	a.BaseS, a.W, a.H = 1, 1600, 900
	a.backstage = false
	a.Paint(newCanvas(1600, 900))
	return a
}

func TestActionsUnique(t *testing.T) {
	a := newShellApp(t)
	seen := map[string]bool{}
	for _, ac := range a.buildActions() {
		if seen[ac.id] {
			t.Errorf("команда %q описана дважды", ac.id)
		}
		seen[ac.id] = true
		if ac.act == nil || ac.label == "" || ac.icon == "" {
			t.Errorf("команда %q без действия, названия или значка", ac.id)
		}
	}
}

func TestInspectorButtonsExist(t *testing.T) {
	a := newShellApp(t)
	var missing []string
	inspMissing = func(id string) { missing = append(missing, id) }
	defer func() { inspMissing = nil }()
	cv := newCanvas(1600, 900)
	for _, tab := range inspTabs {
		a.tab = tab.id
		a.Paint(cv)
	}
	// контекст таблицы и рисунка тоже
	a.tab = "format"
	a.E.InsertTable(2, 2)
	a.Paint(cv)
	if len(missing) > 0 {
		t.Errorf("кнопки инспектора без команды: %v", missing)
	}
}

func TestPaletteRunsCommand(t *testing.T) {
	a := newShellApp(t)
	a.E.InsertText("слово")
	a.E.SelectAll()
	if a.E.EffectiveF().B {
		t.Fatal("текст уже жирный")
	}
	a.KeyDown(75, Mods{Ctrl: true}) // Ctrl+K
	if a.popup == nil || a.popup.Owner != "palette" {
		t.Fatal("Ctrl+K не открыл палитру")
	}
	for _, r := range "жирн" {
		a.Char(r)
	}
	a.KeyDown(13, Mods{})
	if a.popup != nil {
		t.Error("палитра не закрылась после Enter")
	}
	if !a.E.EffectiveF().B {
		t.Error("«жирн» + Enter не включил полужирный")
	}
	// команда, открывающая своё меню, не должна закрывать его сразу
	a.KeyDown(75, Mods{Ctrl: true})
	for _, r := range "шрифт…" {
		a.Char(r)
	}
	a.KeyDown(13, Mods{})
	if a.popup == nil || a.popup.Owner != "font" {
		t.Errorf("меню шрифта не открылось из палитры: %+v", a.popup)
	}
	// Ctrl+Shift+K — по-прежнему гиперссылка
	a.popup = nil
	a.KeyDown(75, Mods{Ctrl: true, Shift: true})
	if a.dialog == nil {
		t.Error("Ctrl+Shift+K не открыл диалог гиперссылки")
	}
}

func TestInspectorCollapses(t *testing.T) {
	a := newShellApp(t)
	cv := newCanvas(1600, 900)
	a.Paint(cv)
	if a.inspStrip {
		t.Error("на широком окне инспектор свернулся")
	}
	a.W = 1000
	a.Set.ShowNav = false // миниатюры занимают своё место и раньше; проверяем, что инспектор уступает листу
	a.Paint(newCanvas(1000, 900))
	if !a.inspStrip {
		t.Error("на узком окне инспектор не свернулся в полосу")
	}
	if a.docRect.W < a.maxPageW() {
		t.Errorf("лист не помещается: %v < %v", a.docRect.W, a.maxPageW())
	}
}

// Тот же путь, что у ci/scenario.ps1: всё через палитру команд.
func TestPaletteTableScenario(t *testing.T) {
	a := newShellApp(t)
	cmd := func(q string) {
		a.KeyDown(75, Mods{Ctrl: true})
		for _, r := range q {
			a.Char(r)
		}
		a.KeyDown(13, Mods{})
		a.Paint(newCanvas(1600, 900))
	}
	for _, r := range "Привет мир." {
		a.Char(r)
	}
	a.KeyDown(13, Mods{})
	cmd("Таблица с параметрами")
	if a.dialog == nil {
		t.Fatal("диалог таблицы не открылся")
	}
	a.KeyDown(13, Mods{})
	if a.E.TableCtx() == nil {
		t.Fatal("таблица не вставилась или курсор не в ней")
	}
	rows := func() int { return len(a.E.TableCtx().Table.Rows) }
	n := rows()
	cmd("Вставить строку снизу")
	if rows() != n+1 {
		t.Errorf("строк %d, ждали %d", rows(), n+1)
	}
	for _, q := range []string{"Вставить строку сверху", "Вставить столбец справа", "Вставить столбец слева", "Объединить с ячейкой",
		"Удалить строку", "Удалить столбец", "Границы", "Заливка ячейки"} {
		cmd(q)
	}
	cmd("Удалить таблицу")
	if a.E.TableCtx() != nil {
		t.Error("таблица не удалилась")
	}
	cmd("Тема: Светлая")
	if a.th.Dark {
		t.Error("тема не переключилась на светлую")
	}
}
