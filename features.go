package main

// Оглавление-поле, список литературы по ГОСТ Р 7.0.100-2018, ссылки на источники, формулы, обтекание, стили.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

var tocCounter int64 = time.Now().Unix() % 100000000

func newTocBM() string {
	return "_Toc" + strconv.FormatInt(100000000+atomic.AddInt64(&tocCounter, 1)%900000000, 10)
}

// RegionSpan — первый и последний+1 индекс абзацев области kind в Body (или -1).
func RegionSpan(d *Doc, kind string) (int, int) {
	from := -1
	for i, b := range d.Body {
		if b.P != nil && b.P.PF.Region == kind {
			if from < 0 {
				from = i
			}
			to := i + 1
			for to < len(d.Body) && d.Body[to].P != nil && d.Body[to].P.PF.Region == kind {
				to++
			}
			return from, to
		}
	}
	return -1, -1
}

func headingText(p *Para) string {
	var sb strings.Builder
	for _, r := range p.Runs {
		if r.T == RText && r.F.Del == "" {
			sb.WriteString(string(r.Text))
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// TOCEntries — строки оглавления по заголовкам 1–levels; ставит закладки на заголовки.
func TOCEntries(d *Doc, levels int) []Block {
	var out []Block
	for _, b := range d.Body {
		p := b.P
		if p == nil || p.PF.Region != "" || p.PF.Heading < 1 || p.PF.Heading > levels {
			continue
		}
		t := headingText(p)
		if t == "" {
			continue
		}
		if p.PF.Bookmark == "" {
			p.PF.Bookmark = newTocBM()
			p.Touch()
		}
		link := "#" + p.PF.Bookmark
		f := F{Link: link}
		e := &Para{PF: PF{Style: "TOC" + strconv.Itoa(p.PF.Heading), Region: "toc"},
			Runs: []Run{TextRun(t, f), TextRun("\t", f), {T: RField, Name: "PAGEREF " + p.PF.Bookmark, F: f}}}
		out = append(out, Block{P: e})
	}
	return out
}

// UpdateTOC пересобирает оглавление на месте. false — оглавления нет.
func UpdateTOC(d *Doc) (bool, int) {
	from, to := RegionSpan(d, "toc")
	if from < 0 {
		return false, 0
	}
	es := TOCEntries(d, 3)
	n := len(es)
	if n == 0 {
		es = []Block{{P: &Para{PF: PF{Region: "toc"}, Runs: []Run{TextRun("Элементы оглавления не найдены: примените к заголовкам стили «Заголовок 1–3».", F{I: true})}}}}
	}
	// разрыв раздела, если он был на последней строке оглавления, переносим
	if last := d.Body[to-1].P; last.PF.Sect != nil {
		es[len(es)-1].P.PF.Sect = last.PF.Sect
	}
	d.Body = append(d.Body[:from], append(es, d.Body[to:]...)...)
	return true, n
}

func (e *Editor) InsertTOC() bool {
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	e.Checkpoint("")
	if ok, n := UpdateTOC(e.Doc); ok {
		e.Changed()
		return n > 0
	}
	es := TOCEntries(e.Doc, 3)
	if len(es) == 0 {
		return false
	}
	e.deleteSel()
	title := &Para{PF: PF{Style: "TOCHeading"}, Runs: []Run{TextRun("Содержание", F{})}}
	e.insertBlocksAtCaret(append([]Block{{P: title}}, es...))
	e.Changed()
	return true
}

// ---------------------------------------------------------------- список литературы
var SourceTypes = []struct{ ID, Name string }{
	{"book", "Книга"}, {"article", "Статья в журнале"}, {"web", "Электронный ресурс"}, {"thesis", "Диссертация"}, {"law", "Нормативный акт"},
	{"standard", "Стандарт"}, {"patent", "Патент"},
}

type author struct{ sur, ini string }

func parseAuthors(s string) []author {
	var out []author
	for _, part := range strings.Split(s, ",") {
		f := strings.Fields(part)
		if len(f) == 0 {
			continue
		}
		a := author{sur: f[0]}
		if len(f) > 1 {
			a.ini = strings.Join(f[1:], " ")
		}
		// «И. И. Иванов» → фамилия последней
		if len(f) > 1 && strings.HasSuffix(f[0], ".") && !strings.HasSuffix(f[len(f)-1], ".") {
			a = author{sur: f[len(f)-1], ini: strings.Join(f[:len(f)-1], " ")}
		}
		out = append(out, a)
	}
	return out
}

func (a author) inverted() string {
	if a.ini == "" {
		return a.sur
	}
	return a.sur + ", " + a.ini
}

func (a author) direct() string {
	if a.ini == "" {
		return a.sur
	}
	return a.ini + " " + a.sur
}

func endDot(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}

// FormatSource — описание источника по ГОСТ Р 7.0.100-2018 (упрощённо).
func FormatSource(s *Source) string {
	as := parseAuthors(s.Authors)
	const dash = " – "
	var b strings.Builder
	head := ""
	if len(as) > 0 && len(as) <= 3 && s.Type != "law" && s.Type != "standard" && s.Type != "patent" {
		head = as[0].inverted() + " "
	}
	resp := ""
	if len(as) > 0 && s.Type != "law" && s.Type != "thesis" {
		var ds []string
		for i, a := range as {
			if i == 3 && len(as) > 4 {
				ds = append(ds, "[и др.]")
				break
			}
			ds = append(ds, a.direct())
		}
		resp = " / " + strings.Join(ds, ", ")
	}
	switch s.Type {
	case "standard":
		// ГОСТ Р 7.0.100-2018, пример: ГОСТ Р 7.0.100–2018. Библиографическая запись… : национальный стандарт
		// Российской Федерации : издание официальное … – Москва : Стандартинформ, 2018. – 70 с.
		t := strings.TrimSpace(s.Title)
		if n := strings.TrimSpace(s.Number); n != "" && !strings.HasPrefix(t, n) {
			t = n + ". " + t
		}
		b.WriteString(t)
		if s.Extra != "" {
			b.WriteString(" : " + strings.TrimSpace(s.Extra))
		}
		s2 := *s
		s2.Title, s2.Extra, s2.Authors, s2.Type = "", "", "", "book"
		return endDot(b.String()) + " " + strings.TrimLeft(FormatSource(&s2), ". ")
	case "patent":
		// Патент № 1234567 Российская Федерация, МПК G01S 13/90. Способ … : № 2020100001 : заявл. 10.01.2020 :
		// опубл. 15.06.2021 / Иванов И. И., Петров П. П. ; заявитель … – 12 с.
		country := strings.TrimSpace(s.City)
		if country == "" {
			country = "Российская Федерация"
		}
		b.WriteString("Патент № " + strings.TrimSpace(strings.TrimPrefix(s.Number, "№")) + " " + country)
		if s.Journal != "" { // МПК
			b.WriteString(", " + strings.TrimSpace(s.Journal))
		}
		b.WriteString(". " + strings.TrimSpace(s.Title))
		if s.Extra != "" {
			b.WriteString(" : " + strings.TrimSpace(s.Extra))
		}
		if len(as) > 0 {
			var ds []string
			for _, a := range as {
				ds = append(ds, a.direct())
			}
			b.WriteString(" / " + strings.Join(ds, ", "))
		}
		if s.Publisher != "" {
			b.WriteString(" ; заявитель " + strings.TrimSpace(s.Publisher))
		}
		out := endDot(b.String())
		if s.Pages != "" {
			pg := strings.TrimSpace(s.Pages)
			if _, err := strconv.Atoi(pg); err == nil {
				pg += " с."
			}
			out += " – " + endDot(pg)
		}
		return out
	}
	b.WriteString(head)
	b.WriteString(strings.TrimSpace(s.Title))
	if s.Extra != "" {
		b.WriteString(" : " + strings.TrimSpace(s.Extra))
	}
	switch s.Type {
	case "article":
		b.WriteString(resp)
		if s.Journal != "" {
			b.WriteString(" // " + strings.TrimSpace(s.Journal))
		}
		if s.Year != "" {
			b.WriteString("." + dash + s.Year)
		}
		if s.Issue != "" {
			b.WriteString("." + dash + "№ " + strings.TrimPrefix(s.Issue, "№ "))
		}
		if s.Pages != "" {
			b.WriteString("." + dash + "С. " + strings.ReplaceAll(s.Pages, "-", "–"))
		}
		if s.URL != "" {
			b.WriteString("." + dash + "URL: " + s.URL)
			if s.Accessed != "" {
				b.WriteString(" (дата обращения: " + s.Accessed + ")")
			}
		}
		return endDot(b.String())
	case "web":
		if s.Extra == "" {
			b.WriteString(" : сайт")
		}
		b.WriteString(resp)
		if s.Publisher != "" {
			b.WriteString(dash + s.Publisher)
		}
		if s.Year != "" {
			b.WriteString(", " + s.Year)
		}
		b.WriteString("." + dash + "URL: " + s.URL)
		if s.Accessed != "" {
			b.WriteString(" (дата обращения: " + s.Accessed + ")")
		}
		return endDot(b.String())
	case "thesis":
		if len(as) > 0 {
			b.WriteString(" / " + as[0].direct())
		}
		if s.Publisher != "" {
			b.WriteString(" ; " + s.Publisher)
		}
	case "law":
		if s.Publisher != "" {
			b.WriteString(" // " + s.Publisher)
		}
		if s.Year != "" {
			b.WriteString("." + dash + s.Year)
		}
		if s.URL != "" {
			b.WriteString("." + dash + "URL: " + s.URL)
			if s.Accessed != "" {
				b.WriteString(" (дата обращения: " + s.Accessed + ")")
			}
		}
		return endDot(b.String())
	default:
		b.WriteString(resp)
	}
	out := endDot(b.String())
	place := ""
	if s.City != "" {
		place = s.City
	}
	if s.Publisher != "" && s.Type != "thesis" {
		if place != "" {
			place += " : "
		}
		place += s.Publisher
	}
	if s.Year != "" {
		if place != "" {
			place += ", "
		}
		place += s.Year
	}
	if place != "" {
		out += dash + endDot(place)
	}
	if s.Pages != "" {
		pg := strings.TrimSpace(s.Pages)
		if _, err := strconv.Atoi(pg); err == nil {
			pg += " с."
		}
		out += dash + endDot(pg)
	}
	if s.URL != "" {
		out += dash + "URL: " + s.URL
		if s.Accessed != "" {
			out += " (дата обращения: " + s.Accessed + ")"
		}
		out = endDot(out)
	}
	return out
}

func (d *Doc) SourceByID(id string) *Source {
	for _, s := range d.Sources {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// BibEntries — список литературы: сначала в порядке ссылок, потом остальные по алфавиту.
func BibEntries(d *Doc) []Block {
	nums := CiteOrder(d)
	srcs := append([]*Source{}, d.Sources...)
	sort.SliceStable(srcs, func(i, j int) bool {
		ni, oi := nums[srcs[i].ID]
		nj, oj := nums[srcs[j].ID]
		switch {
		case oi && oj:
			return ni < nj
		case oi != oj:
			return oi
		}
		return strings.ToLower(FormatSource(srcs[i])) < strings.ToLower(FormatSource(srcs[j]))
	})
	var out []Block
	for _, s := range srcs {
		p := &Para{PF: PF{Region: "bib", Align: "justify", List: &List{ID: "bib", Kind: "ol", Fmt: "decimal", Text: "%1."}},
			Runs: []Run{TextRun(FormatSource(s), F{})}}
		out = append(out, Block{P: p})
	}
	if len(out) == 0 {
		out = []Block{{P: &Para{PF: PF{Region: "bib"}, Runs: []Run{TextRun("Источников пока нет: добавьте их кнопкой «Источники».", F{I: true})}}}}
	}
	return out
}

func UpdateBib(d *Doc) bool {
	from, to := RegionSpan(d, "bib")
	if from < 0 {
		return false
	}
	es := BibEntries(d)
	if last := d.Body[to-1].P; last.PF.Sect != nil {
		es[len(es)-1].P.PF.Sect = last.PF.Sect
	}
	d.Body = append(d.Body[:from], append(es, d.Body[to:]...)...)
	return true
}

func (e *Editor) InsertBibliography() {
	if e.Story != "body" {
		e.SetStory("body", 0)
	}
	e.Checkpoint("")
	if UpdateBib(e.Doc) {
		e.Changed()
		return
	}
	e.deleteSel()
	title := &Para{PF: PF{Heading: 1, Align: "center"}, Runs: []Run{TextRun("Список использованных источников", F{})}}
	e.insertBlocksAtCaret(append([]Block{{P: title}}, BibEntries(e.Doc)...))
	e.Changed()
}

// InsertCitation — ссылка на источник [n] (pages — «с. 25» по желанию).
func (e *Editor) InsertCitation(id, pages string) {
	name := "CITE " + id
	if pages = strings.TrimSpace(pages); pages != "" {
		name += " " + pages
	}
	e.InsertInline(Run{T: RField, Name: name})
	UpdateBib(e.Doc)
	e.Changed()
}

func (e *Editor) SaveSource(s *Source) {
	e.Checkpoint("")
	if s.ID == "" {
		s.ID = NewID("src")
		e.Doc.Sources = append(e.Doc.Sources, s)
	} else if old := e.Doc.SourceByID(s.ID); old != nil {
		*old = *s
	}
	UpdateBib(e.Doc)
	e.Changed()
}

func (e *Editor) DeleteSource(id string) {
	e.Checkpoint("")
	var keep []*Source
	for _, s := range e.Doc.Sources {
		if s.ID != id {
			keep = append(keep, s)
		}
	}
	e.Doc.Sources = keep
	UpdateBib(e.Doc)
	e.Changed()
}

// ---------------------------------------------------------------- формулы и картинки
// RunAt — индекс объекта (картинка, формула) в позиции o абзаца p.
func RunAt(p *Para, o, typ int) int {
	pos := 0
	for i, r := range p.Runs {
		if r.T == typ && pos == o {
			return i
		}
		pos += r.Len()
	}
	return -1
}

// EqNear — формула рядом с позицией (слева или справа).
func (e *Editor) EqNear(pos Pos) (int, int) {
	if i := RunAt(pos.P, pos.O, REq); i >= 0 {
		return i, pos.O
	}
	if pos.O > 0 {
		if i := RunAt(pos.P, pos.O-1, REq); i >= 0 {
			return i, pos.O - 1
		}
	}
	return -1, 0
}

func (e *Editor) InsertEquation(src string) {
	e.InsertInline(Run{T: REq, Text: []rune(src), F: e.TypingFormat(e.Sel.F)})
}

func (e *Editor) ReplaceEquation(p *Para, idx int, src string) {
	e.Checkpoint("")
	p.Runs[idx].Text = []rune(src)
	p.Touch()
	e.Changed()
}

func (e *Editor) SetWrap(pos Pos, wrap, side string) bool {
	i := RunAt(pos.P, pos.O, RImage)
	if i < 0 {
		return false
	}
	e.Checkpoint("")
	pos.P.Runs[i].Wrap, pos.P.Runs[i].Side = wrap, side
	pos.P.Touch()
	e.Changed()
	return true
}

// ---------------------------------------------------------------- стили
// StyleNames — подписи стилей для интерфейса.
var StyleNames = map[string]string{"h1": "Заголовок 1", "h2": "Заголовок 2", "h3": "Заголовок 3", "h4": "Заголовок 4",
	"h5": "Заголовок 5", "h6": "Заголовок 6", "Title": "Название", "Subtitle": "Подзаголовок", "Quote": "Цитата",
	"NoSpacing": "Без интервала", "TOC1": "Оглавление 1", "TOC2": "Оглавление 2", "TOC3": "Оглавление 3", "TOCHeading": "Заголовок оглавления"}

// UpdateStyleFromPara — «Обновить стиль по выделенному фрагменту».
func (e *Editor) UpdateStyleFromPara(key string) {
	p := e.Sel.F.P
	e.Checkpoint("")
	ef := EffF(e.Doc, p, p.FmtAt(0))
	if len(p.Runs) > 0 {
		ef = EffF(e.Doc, p, p.Runs[0].F)
	}
	pf := EffPF(e.Doc, p)
	s, _ := e.Doc.StyleDef(key)
	s.Font, s.Size, s.B, s.I, s.Color, s.Caps = ef.Font, ef.Size, ef.B, ef.I, ef.Color, ef.Caps
	s.Align, s.Before, s.After, s.IndL, s.IndF, s.Set, s.Line = pf.Align, pf.Before, pf.After, pf.IndL, pf.IndF, true, pf.Line
	e.Doc.ensure()
	e.Doc.Styles[key] = s
	// прямое форматирование абзаца теперь лишнее
	for i := range p.Runs {
		f := &p.Runs[i].F
		f.Font, f.Size, f.Color = "", 0, ""
		f.B, f.I, f.Caps = false, false, false
	}
	p.PF.Align, p.PF.Before, p.PF.After, p.PF.IndL, p.PF.IndF, p.PF.Line = "", Opt{}, Opt{}, Opt{}, Opt{}, Opt{}
	p.Normalize()
	e.touchStyle(key)
	e.Changed()
}

func (e *Editor) SetStyleDef(key string, s headingStyle) {
	e.Checkpoint("")
	e.Doc.ensure()
	e.Doc.Styles[key] = s
	e.touchStyle(key)
	e.Changed()
}

func (e *Editor) touchStyle(key string) {
	for _, en := range Flatten(&e.Doc.Body, nil, nil) {
		if StyleKey(en.P) == key || strings.HasPrefix(key, "TOC") {
			en.P.Touch()
		}
	}
}

// ---------------------------------------------------------------- поиск с регулярными выражениями
// uniRegex — \w, \W и \b понимают кириллицу (в Go они только для латиницы).
func uniRegex(q string) string {
	r := strings.NewReplacer(`\\`, `\\`, `\w`, `[\p{L}\p{N}_]`, `\W`, `[^\p{L}\p{N}_]`)
	return r.Replace(q)
}

func (e *Editor) findRegex(q string, o FindOpts, from Pos) (Pos, Pos, bool) {
	pat := uniRegex(q)
	if !o.Case {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return Pos{}, Pos{}, false
	}
	start := e.fidx[from.P]
	n := len(e.flat)
	for k := 0; k <= n; k++ {
		i := (start + k) % n
		p := e.flat[i].P
		t := p.Runes()
		s := string(t)
		off := 0
		if k == 0 {
			off = len(string(t[:min(from.O, len(t))]))
		}
		for off <= len(s) {
			loc := re.FindStringIndex(s[off:])
			if loc == nil {
				break
			}
			a, b := off+loc[0], off+loc[1]
			if a == b {
				off = b + 1
				continue
			}
			ra := len([]rune(s[:a]))
			rb := ra + len([]rune(s[a:b]))
			if o.Whole && !wholeOK(t, ra, rb-ra) {
				off = b
				continue
			}
			return Pos{P: p, O: ra}, Pos{P: p, O: rb}, true
		}
	}
	return Pos{}, Pos{}, false
}

// regexReplacement — текст замены с $1, $2…
func regexReplacement(q, r, matched string, cs bool) string {
	pat := uniRegex(q)
	if !cs {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return r
	}
	return re.ReplaceAllString(matched, r)
}

func isUpperWord(s string) bool {
	has := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			has = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return has
}
