package main

// Названия с автонумерацией (Рисунок 1.2, Таблица А.1, (3.4) у формул),
// перекрёстные ссылки на них и списки иллюстраций.
//
// Поля в модели:
//   SEQ <категория> [s] #<id>   — номер (s — нумерация в пределах главы)
//   REF <закладка> <l|n|s|p>    — ссылка на название: «Рисунок 1.2», «1.2», «рис. 1.2», «(1.2)»
// Категория без пробелов (пробел хранится как «_»).

import (
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
)

var seqCounter int64 = 4000

func newSeqID() string {
	return strconv.FormatInt(atomic.AddInt64(&seqCounter, 1)*7919%1000003+100000, 36)
}

func newRefBM() string {
	return "_Ref" + strconv.FormatInt(200000000+atomic.AddInt64(&tocCounter, 1)*7919%700000000, 10)
}

// CaptionCats — встроенные категории.
var CaptionCats = []string{"Рисунок", "Таблица", "Формула", "Листинг", "Схема", "График", "Диаграмма", "Осциллограмма"}

var captionShort = map[string]string{"Рисунок": "рис.", "Таблица": "табл.", "Листинг": "лист.", "Схема": "схема", "График": "график",
	"Диаграмма": "диаграмма", "Осциллограмма": "осциллограмма"}

// буквы приложений по ГОСТ (без Ё З Й О Ч Ъ Ы Ь)
var appendixLetters = []rune("АБВГДЕЖИКЛМНПРСТУФХЦШЩЭЮЯ")

type seqRef struct{ Cat, Num string }

// SeqField разбирает имя поля SEQ.
func SeqField(name string) (cat string, chapter bool, id string) {
	fs := strings.Fields(name)
	if len(fs) < 2 || fs[0] != "SEQ" {
		return
	}
	cat = strings.ReplaceAll(fs[1], "_", " ")
	for _, f := range fs[2:] {
		switch {
		case f == "s":
			chapter = true
		case strings.HasPrefix(f, "#"):
			id = f[1:]
		}
	}
	return
}

func seqName(cat string, chapter bool, id string) string {
	s := "SEQ " + strings.ReplaceAll(strings.TrimSpace(cat), " ", "_")
	if chapter {
		s += " s"
	}
	return s + " #" + id
}

// chapterOf — номер главы по заголовку 1 уровня: «2», «А» (приложение) или "" (Введение и т. п.).
func chapterOf(p *Para, numbered, appx *int) (string, bool) {
	t := strings.TrimSpace(headingText(p))
	low := strings.ToLower(t)
	if strings.HasPrefix(low, "приложение") {
		*appx++
		rest := []rune(strings.TrimSpace(t[len("приложение"):]))
		if len(rest) >= 1 && unicode.IsUpper(rest[0]) && (len(rest) == 1 || !unicode.IsLetter(rest[1])) {
			return string(rest[0]), true
		}
		if *appx-1 < len(appendixLetters) {
			return string(appendixLetters[*appx-1]), true
		}
		return strconv.Itoa(*appx), true
	}
	if p.PF.List != nil && p.PF.List.Kind == "ol" {
		*numbered++
		return strconv.Itoa(*numbered), false
	}
	for _, pre := range []string{"глава ", "раздел ", "часть "} {
		if strings.HasPrefix(low, pre) {
			t = strings.TrimSpace(t[len(pre):])
			break
		}
	}
	j := 0
	rs := []rune(t)
	for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
		j++
	}
	if j > 0 {
		n, _ := strconv.Atoi(string(rs[:j]))
		*numbered = n
		return string(rs[:j]), false
	}
	return "", false
}

// computeSeq — номера всех SEQ (вызывается перед вёрсткой). Повторы id (после копирования) получают новый id.
func (d *Doc) computeSeq() {
	d.seqNums = map[string]string{}
	d.seqRefs = map[string]seqRef{}
	chap, inAppx := "", false
	numbered, appx := 0, 0
	counters := map[string]int{}
	seen := map[string]bool{}
	for _, en := range Flatten(&d.Body, nil, nil) {
		p := en.P
		if p.PF.Heading == 1 && p.PF.Region == "" && en.Cell == nil {
			chap, inAppx = chapterOf(p, &numbered, &appx)
		}
		for i := range p.Runs {
			r := &p.Runs[i]
			if r.T != RField || !strings.HasPrefix(r.Name, "SEQ ") {
				continue
			}
			cat, byCh, id := SeqField(r.Name)
			if id == "" || seen[id] {
				id = newSeqID()
				r.Name = seqName(cat, byCh, id)
				p.Touch()
			}
			seen[id] = true
			key, prefix := cat, ""
			if (byCh || inAppx) && chap != "" {
				key, prefix = cat+"|"+chap, chap+"."
			}
			counters[key]++
			num := prefix + strconv.Itoa(counters[key])
			d.seqNums[id] = num
			if bm := p.PF.Bookmark; bm != "" {
				if _, ok := d.seqRefs[bm]; !ok {
					d.seqRefs[bm] = seqRef{cat, num}
				}
			}
		}
	}
}

func (d *Doc) seqValue(name string) string {
	_, _, id := SeqField(name)
	if v, ok := d.seqNums[id]; ok {
		return v
	}
	return "?"
}

// refValue — текст перекрёстной ссылки.
func (d *Doc) refValue(name string) string {
	fs := strings.Fields(name)
	if len(fs) < 2 {
		return "?"
	}
	r, ok := d.seqRefs[fs[1]]
	if !ok {
		return "Ошибка! Источник ссылки не найден."
	}
	mode := "l"
	if len(fs) > 2 {
		mode = fs[2]
	}
	switch mode {
	case "n":
		return r.Num
	case "p":
		return "(" + r.Num + ")"
	case "s":
		if sh, ok := captionShort[r.Cat]; ok {
			return sh + " " + r.Num
		}
		if r.Cat == "Формула" {
			return "(" + r.Num + ")"
		}
		return strings.ToLower(r.Cat) + " " + r.Num
	}
	if r.Cat == "Формула" {
		return "формула (" + r.Num + ")"
	}
	return r.Cat + " " + r.Num
}

// Caption — найденное название (для диалога ссылок и списков иллюстраций).
type Caption struct {
	P        *Para
	Cat, Num string
	Text     string // всё название целиком
	BM       string
}

// Captions — названия документа по порядку (category "" — все).
func Captions(d *Doc, category string) []Caption {
	if d.seqNums == nil {
		d.computeSeq()
	}
	var out []Caption
	for _, en := range Flatten(&d.Body, nil, nil) {
		p := en.P
		if p.PF.Region != "" {
			continue
		}
		for _, r := range p.Runs {
			if r.T != RField || !strings.HasPrefix(r.Name, "SEQ ") {
				continue
			}
			cat, _, _ := SeqField(r.Name)
			if category != "" && cat != category {
				break
			}
			var sb strings.Builder
			for _, x := range p.Runs {
				switch {
				case x.T == RText && x.F.Del == "":
					sb.WriteString(string(x.Text))
				case x.T == RField:
					sb.WriteString(d.FieldValue(x.Name, "", ""))
				case x.T == REq:
					sb.WriteString(EqPlain(string(x.Text)))
				}
			}
			out = append(out, Caption{P: p, Cat: cat, Num: d.seqValue(r.Name), Text: strings.Join(strings.Fields(sb.String()), " "), BM: p.PF.Bookmark})
			break
		}
	}
	return out
}

// ---------------------------------------------------------------- вставка
// InsertCaption — абзац «Рисунок 1 – текст» после текущего абзаца (у таблицы — перед ней).
func (e *Editor) InsertCaption(cat, text string, chapter bool) {
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	e.Checkpoint("")
	d := e.Doc
	p := e.Sel.F.P
	en := e.entry(p)
	arr, idx := en.Arr, IndexOfPara(*en.Arr, p)+1
	align := "center"
	if cat == "Таблица" {
		align = "left"
	}
	if en.Cell != nil { // в таблице: название ставим над таблицей
		arr = en.Cell.Arr
		idx = IndexOfTable(*arr, en.Cell.Table)
	}
	f := F{}
	cp := &Para{PF: PF{Align: align, Bookmark: newRefBM(), KeepNext: cat == "Таблица"}}
	if align == "left" {
		cp.PF.IndF = Opt{V: 0, Set: true}
	}
	cp.Runs = []Run{TextRun(cat+" ", f), {T: RField, Name: seqName(cat, chapter, newSeqID()), F: f}}
	if strings.TrimSpace(text) != "" {
		cp.Runs = append(cp.Runs, TextRun(" – "+strings.TrimSpace(text), f))
	}
	InsertBlocks(arr, max(0, idx), Block{P: cp})
	e.refresh()
	e.Sel = Sel{Pos{P: cp, O: cp.Len()}, Pos{P: cp, O: cp.Len()}}
	d.seqNums = nil
	e.Changed()
}

// InsertNumberedEquation — формула отдельной строкой: по центру, номер (1.2) у правого поля.
func (e *Editor) InsertNumberedEquation(src string, chapter bool) {
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	e.Checkpoint("")
	e.deleteSel()
	pg := e.Doc.Page
	w := pg.W - pg.Left - pg.Right
	if e.L != nil && e.L.W > 0 {
		w = e.L.W
	}
	f := e.TypingFormat(e.Sel.F)
	f.Link = ""
	p := &Para{PF: PF{Align: "left", IndF: Opt{V: 0, Set: true}, IndL: Opt{V: 0, Set: true}, Bookmark: newRefBM(),
		Tabs: []TabStop{{Pos: w / 2, Align: "center"}, {Pos: w, Align: "right"}}}}
	p.Runs = []Run{TextRun("\t", f), {T: REq, Text: []rune(src), F: f}, TextRun("\t(", f),
		{T: RField, Name: seqName("Формула", chapter, newSeqID()), F: f}, TextRun(")", f)}
	if cur := e.Sel.F.P; cur.Len() == 0 && e.entry(cur).Cell == nil {
		// пустой абзац — формула встаёт прямо в него
		cur.PF, cur.Runs = p.PF, p.Runs
		cur.Touch()
		p = cur
		e.refresh()
	} else {
		e.insertBlocksAtCaret([]Block{{P: p}})
	}
	e.Sel = Sel{Pos{P: p, O: p.Len()}, Pos{P: p, O: p.Len()}}
	e.Doc.seqNums = nil
	e.Changed()
}

// InsertCrossRef — ссылка на название по закладке.
func (e *Editor) InsertCrossRef(bm, mode string) {
	f := e.TypingFormat(e.Sel.F)
	f.Link = "#" + bm
	e.InsertInline(Run{T: RField, Name: "REF " + bm + " " + mode, F: f})
}

// ---------------------------------------------------------------- списки иллюстраций
func lofRegion(cat string) string { return "lof:" + strings.ReplaceAll(cat, " ", "_") }

func LOFEntries(d *Doc, cat string) []Block {
	var out []Block
	for _, c := range Captions(d, cat) {
		if c.BM == "" {
			c.P.PF.Bookmark = newRefBM()
			c.P.Touch()
			c.BM = c.P.PF.Bookmark
		}
		f := F{Link: "#" + c.BM}
		out = append(out, Block{P: &Para{PF: PF{Style: "TOC1", Region: lofRegion(cat)},
			Runs: []Run{TextRun(c.Text, f), TextRun("\t", f), {T: RField, Name: "PAGEREF " + c.BM, F: f}}}})
	}
	if len(out) == 0 {
		out = []Block{{P: &Para{PF: PF{Region: lofRegion(cat)}, Runs: []Run{TextRun("Названия категории «"+cat+"» не найдены.", F{I: true})}}}}
	}
	return out
}

// UpdateLOF — пересобрать все списки иллюстраций; возвращает их число.
func UpdateLOF(d *Doc) int {
	d.computeSeq()
	n := 0
	for i := 0; i < len(d.Body); i++ {
		b := d.Body[i]
		if b.P == nil || !strings.HasPrefix(b.P.PF.Region, "lof:") {
			continue
		}
		kind := b.P.PF.Region
		cat := strings.ReplaceAll(strings.TrimPrefix(kind, "lof:"), "_", " ")
		from, to := RegionSpan(d, kind)
		es := LOFEntries(d, cat)
		if last := d.Body[to-1].P; last.PF.Sect != nil {
			es[len(es)-1].P.PF.Sect = last.PF.Sect
		}
		d.Body = append(d.Body[:from], append(es, d.Body[to:]...)...)
		i = from + len(es) - 1
		n++
	}
	return n
}

func (e *Editor) InsertLOF(cat string) {
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	e.Checkpoint("")
	e.deleteSel()
	title := map[string]string{"Рисунок": "Перечень рисунков", "Таблица": "Перечень таблиц", "Формула": "Перечень формул"}[cat]
	if title == "" {
		title = "Перечень: " + strings.ToLower(cat)
	}
	t := &Para{PF: PF{Style: "TOCHeading"}, Runs: []Run{TextRun(title, F{})}}
	e.insertBlocksAtCaret(append([]Block{{P: t}}, LOFEntries(e.Doc, cat)...))
	e.Changed()
}

// fixSeqPrefixes — при чтении из Word номер главы перед полем SEQ («1.» или результат STYLEREF) убираем:
// его посчитаем сами.
func fixSeqPrefixes(d *Doc) {
	for _, en := range Flatten(&d.Body, nil, nil) {
		p := en.P
		for i := 1; i < len(p.Runs); i++ {
			if p.Runs[i].T != RField || !strings.HasPrefix(p.Runs[i].Name, "SEQ ") || p.Runs[i-1].T != RText {
				continue
			}
			t := p.Runs[i-1].Text
			n := len(t)
			if n < 2 || (t[n-1] != '.' && t[n-1] != '-' && t[n-1] != '–') {
				continue
			}
			j := n - 1
			for j > 0 && (unicode.IsDigit(t[j-1]) || unicode.IsUpper(t[j-1])) {
				j--
			}
			if j == n-1 || (j > 0 && t[j-1] != ' ' && t[j-1] != '\u00a0') {
				continue
			}
			p.Runs[i-1].Text = t[:j]
			if !strings.Contains(p.Runs[i].Name, " s") {
				cat, _, id := SeqField(p.Runs[i].Name)
				if _, err := strconv.Atoi(string(t[j : n-1])); err == nil {
					p.Runs[i].Name = seqName(cat, true, id)
				}
			}
			p.Touch()
		}
		// «рис. » + REF …_n → краткая ссылка; «(» REF «)» → в скобках
		for i := 0; i < len(p.Runs); i++ {
			r := &p.Runs[i]
			if r.T != RField || !strings.HasPrefix(r.Name, "REF ") || !strings.HasSuffix(r.Name, " n") {
				continue
			}
			base := strings.TrimSuffix(r.Name, " n")
			if i > 0 && p.Runs[i-1].T == RText {
				t := string(p.Runs[i-1].Text)
				done := false
				for _, sh := range captionShort {
					if strings.HasSuffix(t, sh+" ") {
						p.Runs[i-1].Text = []rune(strings.TrimSuffix(t, sh+" "))
						r.Name, done = base+" s", true
						break
					}
				}
				if !done && strings.HasSuffix(t, "(") && i+1 < len(p.Runs) && p.Runs[i+1].T == RText && strings.HasPrefix(string(p.Runs[i+1].Text), ")") {
					p.Runs[i-1].Text = []rune(strings.TrimSuffix(t, "("))
					p.Runs[i+1].Text = p.Runs[i+1].Text[1:]
					r.Name = base + " p"
				}
				p.Touch()
			}
		}
	}
}
