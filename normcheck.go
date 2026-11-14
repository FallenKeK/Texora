package main

// Нормоконтроль по ГОСТ 7.32-2017 («Отчёт о научно-исследовательской работе»), на который
// ссылаются требования к курсовым и дипломам: поля, шрифт, интервал, отступ, выравнивание,
// заголовки, подписи рисунков и таблиц, ссылки на рисунки, таблицы и источники, номера страниц.
//
// Замечания группируются по видам: в панели — «вид — сколько раз», щелчок ведёт к следующему месту,
// часть видов исправляется одной кнопкой (с отменой).

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Требования ГОСТ 7.32-2017, раздел 6.
const (
	normFont   = "Times New Roman"
	normMinSz  = 12.0
	normSize   = 14.0 // так требует большинство вузов (ГОСТ — «не менее 12»)
	normLine   = 1.5
	normIndent = 1.25 * CM
	normLeft   = 3 * CM
	normRight  = 15.0 / 10 * CM
	normTop    = 20.0 / 10 * CM
	normBottom = 20.0 / 10 * CM
	normTol    = 1.0 / 10 * CM // допуск 1 мм
)

// NormIssue — одно замечание.
type NormIssue struct {
	Kind string // вид (ключ группы)
	Msg  string // пояснение к месту
	P    *Para  // где (nil — документ целиком)
	Sec  int    // раздел для замечаний о листе (-1 — нет)
}

// NormKind — описание вида замечаний.
type NormKind struct {
	ID, Title string
	Err       bool // нарушение (иначе — совет)
	Fix       bool // исправляется автоматически
}

var normKinds = []NormKind{
	{"page", "Формат листа не A4", true, false},
	{"margins", "Поля не 30 / 15 / 20 / 20 мм", true, true},
	{"pagenum", "Нет номеров страниц внизу", true, false},
	{"titlepg", "Номер страницы на титульном листе", false, true},
	{"font", "Шрифт не Times New Roman", true, true},
	{"size", "Кегль меньше 12 пт", true, true},
	{"sizemix", "Кегль текста не 14 пт", false, true},
	{"line", "Интервал не полуторный", true, true},
	{"indent", "Абзацный отступ не 1,25 см", true, true},
	{"align", "Текст не выровнен по ширине", true, true},
	{"color", "Цветной текст", true, true},
	{"hdot", "Точка в конце заголовка", true, true},
	{"hcase", "Структурный элемент не прописными или не по центру", true, true},
	{"hpage", "Раздел не с новой страницы", true, true},
	{"figcap", "Рисунок без подписи «Рисунок N – …» под ним", true, false},
	{"figfmt", "Подпись рисунка оформлена не по ГОСТ", true, true},
	{"tabcap", "Таблица без названия «Таблица N – …» над ней", true, false},
	{"tabfmt", "Название таблицы оформлено не по ГОСТ", true, true},
	{"figref", "Нет ссылки в тексте на рисунок", true, false},
	{"tabref", "Нет ссылки в тексте на таблицу", true, false},
	{"srcref", "Нет ссылки в тексте на источник", true, false},
	{"srcbad", "Ссылка на несуществующий источник", true, false},
	{"srcorder", "Источники пронумерованы не в порядке упоминания", false, false},
	{"empty", "Пустые абзацы вместо разрыва страницы", false, true},
	{"spaces", "Двойные пробелы", false, true},
	{"dash", "Дефис вместо тире", false, true},
}

func normKind(id string) NormKind {
	for _, k := range normKinds {
		if k.ID == id {
			return k
		}
	}
	return NormKind{ID: id, Title: id}
}

// названия структурных элементов (ГОСТ 7.32-2017, 6.2.1): по центру, прописными, без точки
var normStruct = []string{"РЕФЕРАТ", "СОДЕРЖАНИЕ", "ОГЛАВЛЕНИЕ", "ТЕРМИНЫ И ОПРЕДЕЛЕНИЯ", "ПЕРЕЧЕНЬ СОКРАЩЕНИЙ", "ОПРЕДЕЛЕНИЯ",
	"ОБОЗНАЧЕНИЯ И СОКРАЩЕНИЯ", "СПИСОК СОКРАЩЕНИЙ", "ВВЕДЕНИЕ", "ЗАКЛЮЧЕНИЕ", "СПИСОК ИСПОЛЬЗОВАННЫХ ИСТОЧНИКОВ",
	"СПИСОК ЛИТЕРАТУРЫ", "СПИСОК ИСПОЛЬЗОВАННОЙ ЛИТЕРАТУРЫ", "БИБЛИОГРАФИЧЕСКИЙ СПИСОК", "ПРИЛОЖЕНИЕ", "СПИСОК ИСПОЛНИТЕЛЕЙ"}

func normStructName(s string) (string, bool) {
	u := strings.ToUpper(strings.TrimRight(strings.TrimSpace(s), ". "))
	u = strings.Join(strings.Fields(u), " ")
	for _, n := range normStruct {
		if u == n || (n == "ПРИЛОЖЕНИЕ" && strings.HasPrefix(u, "ПРИЛОЖЕНИЕ ")) {
			return n, true
		}
	}
	return "", false
}

func isBibTitle(s string) bool {
	n, ok := normStructName(s)
	return ok && (strings.HasPrefix(n, "СПИСОК ИСПОЛЬЗ") || n == "СПИСОК ЛИТЕРАТУРЫ" || n == "БИБЛИОГРАФИЧЕСКИЙ СПИСОК")
}

// normText — текст абзаца, как он виден: поля подставлены, объекты убраны.
func normText(d *Doc, p *Para) string {
	var sb strings.Builder
	for _, r := range p.Runs {
		switch r.T {
		case RText:
			if r.F.Del == "" {
				sb.WriteString(string(r.Text))
			}
		case RField:
			if !strings.HasPrefix(r.Name, "PAGE") && r.Name != "NUMPAGES" {
				sb.WriteString(d.FieldValue(r.Name, "", ""))
			}
		case RBreak:
			sb.WriteByte(' ')
		}
	}
	return sb.String()
}

func hasLetters(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func paraHas(p *Para, t int) bool {
	for _, r := range p.Runs {
		if r.T == t {
			return true
		}
	}
	return false
}

var reTocLine = regexp.MustCompile(`\t\s*\d+\s*$`)
var reSecNum = regexp.MustCompile(`^(?:[А-ЯA-Z]\.)?\d+(?:\.\d+)*\.?\s+\S`)

// looksHeading — заголовок, оформленный вручную: короткий полужирный абзац с номером раздела
// или с запретом отрыва от следующего.
func looksHeading(d *Doc, p *Para, txt string) bool {
	if txt == "" || len([]rune(txt)) > 300 || strings.HasSuffix(txt, ":") || p.PF.List != nil {
		return false
	}
	any := false
	for _, r := range p.Runs {
		if r.T != RText || strings.TrimSpace(string(r.Text)) == "" {
			continue
		}
		if !EffF(d, p, r.F).B {
			return false
		}
		any = true
	}
	return any && (p.PF.KeepNext || reSecNum.MatchString(txt))
}

// normEntry — абзац документа по порядку.
type normEntry struct {
	p     *Para
	top   int    // индекс блока верхнего уровня
	cell  bool   // в ячейке таблицы
	table *Table // таблица, которая идёт сразу после (для абзаца перед таблицей) — заполняется отдельно
	text  string
}

var (
	reFigCap = regexp.MustCompile(`^(Рисунок|Рис\.?)\s*([А-ЯA-Z]?\.?\d+(?:[.\-]\d+)*)\s*(.?)`)
	reTabCap = regexp.MustCompile(`^(Таблица|Табл\.?)\s*([А-ЯA-Z]?\.?\d+(?:[.\-]\d+)*)\s*(.?)`)
	reFigRef = regexp.MustCompile(`(?i)(?:рис\.|рисун[а-яё]*)\s*\(?((?:[А-ЯA-Z]\.)?\d+(?:\.\d+)*(?:\s*(?:,|и|–|—|-|\.\.)\s*(?:[А-ЯA-Z]\.)?\d+(?:\.\d+)*)*)`)
	reTabRef = regexp.MustCompile(`(?i)(?:табл\.|таблиц[а-яё]*)\s*\(?((?:[А-ЯA-Z]\.)?\d+(?:\.\d+)*(?:\s*(?:,|и|–|—|-|\.\.)\s*(?:[А-ЯA-Z]\.)?\d+(?:\.\d+)*)*)`)
	reCite   = regexp.MustCompile(`\[([^\[\]]{1,80})\]`)
	reNumID  = regexp.MustCompile(`^(?:[А-ЯA-Z]\.)?\d+(?:\.\d+)*$`)
)

// refNums — номера из «1, 2 и 4–6» (диапазон раскрывается, если номера простые или с одним префиксом).
func refNums(s string) []string {
	var out []string
	parts := regexp.MustCompile(`\s*(,|\sи\s|;)\s*`).Split(s, -1)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		rng := regexp.MustCompile(`^(.*?)\s*(?:–|—|-|\.\.)\s*(.*)$`).FindStringSubmatch(part)
		if rng != nil && reNumID.MatchString(rng[1]) && reNumID.MatchString(rng[2]) {
			a, b := rng[1], rng[2]
			pa, pb := "", ""
			if i := strings.LastIndex(a, "."); i >= 0 {
				pa, a = a[:i+1], a[i+1:]
			}
			if i := strings.LastIndex(b, "."); i >= 0 {
				pb, b = b[:i+1], b[i+1:]
			}
			x, e1 := strconv.Atoi(a)
			y, e2 := strconv.Atoi(b)
			if e1 == nil && e2 == nil && pa == pb && y >= x && y-x < 200 {
				for k := x; k <= y; k++ {
					out = append(out, pa+strconv.Itoa(k))
				}
				continue
			}
			out = append(out, rng[1], rng[2])
			continue
		}
		if reNumID.MatchString(part) {
			out = append(out, part)
		}
	}
	return out
}

// citeNums — номера источников из содержимого квадратных скобок: «1, 3–5, с. 12» → 1 3 4 5.
func citeNumsOf(s string) ([]int, bool) {
	var out []int
	for _, part := range regexp.MustCompile(`[,;]`).Split(s, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		low := strings.ToLower(part)
		if strings.HasPrefix(low, "с.") || strings.HasPrefix(low, "c.") || strings.HasPrefix(low, "p.") || strings.HasPrefix(low, "стр") || strings.HasPrefix(low, "л.") {
			continue
		}
		if m := regexp.MustCompile(`^(\d+)\s*(?:–|—|-)\s*(\d+)$`).FindStringSubmatch(part); m != nil {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			if b >= a && b-a < 200 {
				for k := a; k <= b; k++ {
					out = append(out, k)
				}
				continue
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, false // [Иванов, 2020], [x] — не номерная ссылка
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

// NormCheck — все замечания по документу.
func NormCheck(d *Doc) []NormIssue {
	d.citeNums = CiteOrder(d)
	d.computeSeq()
	var out []NormIssue
	add := func(kind string, p *Para, msg string) {
		out = append(out, NormIssue{Kind: kind, P: p, Msg: msg, Sec: -1})
	}
	// ---- лист
	secs := d.Sections()
	for i, s := range secs {
		pg := s.Page
		w, h := math.Min(pg.W, pg.H), math.Max(pg.W, pg.H)
		if math.Abs(w-21*CM) > 2*normTol || math.Abs(h-29.7*CM) > 2*normTol {
			out = append(out, NormIssue{Kind: "page", Sec: i, Msg: fmt.Sprintf("%s × %s мм", fmtNum(math.Round(pg.W/(CM/10))), fmtNum(math.Round(pg.H/(CM/10))))})
		}
		if pg.W > pg.H {
			continue // альбомный лист: поля по повёрнутой странице не проверяем
		}
		if math.Abs(pg.Left-normLeft) > normTol || math.Abs(pg.Right-normRight) > normTol || math.Abs(pg.Top-normTop) > normTol || math.Abs(pg.Bottom-normBottom) > normTol {
			mm := func(v float64) string { return fmtNum(math.Round(v / (CM / 10))) }
			out = append(out, NormIssue{Kind: "margins", Sec: i, Msg: fmt.Sprintf("лев %s · прав %s · верх %s · низ %s мм", mm(pg.Left), mm(pg.Right), mm(pg.Top), mm(pg.Bottom))})
		}
	}
	// номера страниц: поле PAGE в нижнем колонтитуле
	hasNum := false
	for _, s := range secs {
		for _, k := range []string{"footer", "footer.first", "footer.even"} {
			for _, b := range *s.HF.Get(k) {
				walkParas([]Block{b}, func(p *Para, _ bool) {
					for _, r := range p.Runs {
						if r.T == RField && r.Name == "PAGE" {
							hasNum = true
						}
					}
				})
			}
		}
	}
	if !hasNum {
		add("pagenum", nil, "ГОСТ: арабские цифры внизу по центру, без точки")
	} else if len(d.Body) > 5 {
		// на титульном листе (первая страница первого раздела) номера быть не должно
		v := ""
		if secs[0].TitlePg {
			v = "first"
		}
		_, bs := d.ResolveHF(secs, 0, "footer", v)
		has := false
		walkParas(bs, func(p *Para, _ bool) {
			for _, r := range p.Runs {
				if r.T == RField && r.Name == "PAGE" {
					has = true
				}
			}
		})
		if has {
			add("titlepg", nil, "титульный лист считается, но номер на нём не ставят")
		}
	}
	// ---- абзацы по порядку
	var es []normEntry
	for i, b := range d.Body {
		walkParas([]Block{b}, func(p *Para, cell bool) {
			es = append(es, normEntry{p: p, top: i, cell: cell, text: normText(d, p)})
		})
	}
	// начало основной части: первый заголовок, оглавление или структурный элемент (титульный лист не проверяем)
	start := len(es)
	for i, e := range es {
		if e.cell {
			continue
		}
		if n, ok := normStructName(e.text); ok && n != "РЕФЕРАТ" || e.p.PF.Heading > 0 || e.p.PF.Region == "toc" || strings.HasPrefix(e.p.PF.Style, "TOC") {
			start = i
			break
		}
	}
	if start == len(es) {
		start = 0
	}
	inBib, inToc := false, false
	var bibEntries []*Para
	var figCaps, tabCaps []normCap
	prevText := -1 // последний непустой абзац (индекс в es)
	emptyRun := 0
	firstH := true
	for i := start; i < len(es); i++ {
		e := es[i]
		p, txt := e.p, strings.TrimSpace(e.text)
		if p.PF.RevPara == "del" {
			continue
		}
		if p.PF.Region == "toc" || strings.HasPrefix(p.PF.Style, "TOC") && p.PF.Style != "TOCHeading" {
			continue
		}
		pf := EffPF(d, p)
		_, isStruct := normStructName(txt)
		isHeading := !e.cell && (p.PF.Heading > 0 || (isStruct && len(txt) < 60) || looksHeading(d, p, txt))
		// оглавление, набранное вручную: от «Содержание» до следующего заголовка, строки с номером страницы через табуляцию
		if isHeading {
			n, _ := normStructName(txt)
			inToc = n == "СОДЕРЖАНИЕ" || n == "ОГЛАВЛЕНИЕ"
			if inToc {
				firstH = true // оглавление обычно на одной странице с титулом-заданием не стоит, но разрыв перед ним не требуем
			}
		} else if inToc || reTocLine.MatchString(e.text) {
			prevText = i
			continue
		}
		// пустые абзацы подряд
		if !e.cell && txt == "" && !paraHas(p, RImage) && !paraHas(p, REq) {
			emptyRun++
			if emptyRun == 4 {
				add("empty", p, "")
			}
			continue
		}
		emptyRun = 0
		// список литературы
		if isHeading {
			inBib = isBibTitle(txt)
		}
		if inBib && !isHeading && !e.cell && txt != "" {
			bibEntries = append(bibEntries, p)
		}
		// заголовки
		if isHeading {
			if strings.HasSuffix(txt, ".") && !strings.HasSuffix(txt, "..") {
				add("hdot", p, txt)
			}
			if isStruct {
				if txt != strings.ToUpper(txt) && !EffF(d, p, firstTextF(p)).Caps || pf.Align != "center" {
					add("hcase", p, txt)
				}
			}
			if (p.PF.Heading == 1 || isStruct) && !firstH && !p.PF.PB && !e.cell {
				if !breaksBefore(d, es, i) {
					add("hpage", p, txt)
				}
			}
			firstH = false
			prevText = i
			continue
		}
		if p.PF.Region == "toc" || p.PF.Region == "bib" || strings.HasPrefix(p.PF.Style, "TOC") {
			prevText = i
			continue
		}
		// рисунки и таблицы
		if m := reFigCap.FindStringSubmatch(txt); m != nil && !e.cell {
			figCaps = append(figCaps, normCap{p: p, num: m[2]})
			if msg := capFmtProblem(m, txt); msg != "" {
				add("figfmt", p, msg)
			} else if pf.Align != "center" {
				add("figfmt", p, "подпись рисунка — по центру")
			}
			prevText = i
			continue
		}
		if m := reTabCap.FindStringSubmatch(txt); m != nil && !e.cell {
			tabCaps = append(tabCaps, normCap{p: p, num: m[2]})
			if msg := capFmtProblem(m, txt); msg != "" {
				add("tabfmt", p, msg)
			} else if pf.Align == "center" || pf.Align == "right" || math.Abs(pf.IndF) > normTol || math.Abs(pf.IndL) > normTol {
				add("tabfmt", p, "название таблицы — слева, без абзацного отступа")
			}
			prevText = i
			continue
		}
		if !e.cell && paraHas(p, RImage) && !hasLetters(txt) {
			// подпись — в одном из двух следующих непустых абзацев
			found := false
			for j, n := i+1, 0; j < len(es) && n < 2; j++ {
				t := strings.TrimSpace(es[j].text)
				if t == "" && !paraHas(es[j].p, RImage) {
					continue
				}
				n++
				if reFigCap.MatchString(t) {
					found = true
					break
				}
				if paraHas(es[j].p, RImage) {
					continue // несколько картинок одним рисунком
				}
			}
			if !found {
				add("figcap", p, "")
			}
			prevText = i
			continue
		}
		if e.cell || paraHas(p, REq) && !hasLetters(txt) {
			continue
		}
		// обычный текст основной части
		checkBodyPara(d, p, pf, inBib, add)
		if strings.Contains(txt, "  ") {
			add("spaces", p, "")
		}
		if strings.Contains(txt, " - ") {
			add("dash", p, "")
		}
		prevText = i
	}
	_ = prevText
	// таблицы: перед каждой — название
	for i, b := range d.Body {
		if b.T == nil || i < firstTop(es, start) {
			continue
		}
		ok := false
		for j := i - 1; j >= 0 && j >= i-2; j-- {
			if d.Body[j].P == nil {
				break
			}
			t := strings.TrimSpace(normText(d, d.Body[j].P))
			if t == "" {
				continue
			}
			ok = reTabCap.MatchString(t) || strings.HasPrefix(t, "Продолжение таблицы") || strings.HasPrefix(t, "Окончание таблицы")
			break
		}
		if !ok && len(b.T.Rows) > 1 && len(b.T.Cols) > 1 {
			p := firstParaIn(b.T)
			add("tabcap", p, "")
		}
	}
	// ссылки на рисунки и таблицы
	var body strings.Builder
	capSet := map[*Para]bool{}
	for _, c := range figCaps {
		capSet[c.p] = true
	}
	for _, c := range tabCaps {
		capSet[c.p] = true
	}
	for _, e := range es {
		if capSet[e.p] || e.p.PF.Region == "toc" || strings.HasPrefix(e.p.PF.Style, "TOC") {
			continue
		}
		body.WriteString(e.text)
		body.WriteByte('\n')
	}
	all := body.String()
	refsOf := func(re *regexp.Regexp) map[string]bool {
		m := map[string]bool{}
		for _, g := range re.FindAllStringSubmatch(all, -1) {
			for _, n := range refNums(g[1]) {
				m[strings.ReplaceAll(n, "-", ".")] = true
			}
		}
		return m
	}
	figRefs, tabRefs := refsOf(reFigRef), refsOf(reTabRef)
	for _, c := range figCaps {
		if !figRefs[strings.ReplaceAll(c.num, "-", ".")] {
			add("figref", c.p, "Рисунок "+c.num)
		}
	}
	for _, c := range tabCaps {
		if !tabRefs[strings.ReplaceAll(c.num, "-", ".")] {
			add("tabref", c.p, "Таблица "+c.num)
		}
	}
	// источники
	if len(d.Sources) > 0 {
		for _, s := range d.Sources {
			if _, ok := d.citeNums[s.ID]; !ok {
				add("srcref", bibParaFor(es, s), normSrcName(s))
			}
		}
	} else if len(bibEntries) > 0 {
		cited := map[int]bool{}
		var order []int
		for _, e := range es {
			if inList(bibEntries, e.p) {
				continue
			}
			for _, m := range reCite.FindAllStringSubmatch(e.text, -1) {
				ns, ok := citeNumsOf(m[1])
				if !ok {
					continue
				}
				for _, n := range ns {
					if n < 1 || n > len(bibEntries) {
						add("srcbad", e.p, fmt.Sprintf("[%d], а источников %d", n, len(bibEntries)))
						continue
					}
					if !cited[n] {
						order = append(order, n)
					}
					cited[n] = true
				}
			}
		}
		for i, p := range bibEntries {
			if !cited[i+1] {
				t := strings.TrimSpace(normText(d, p))
				if r := []rune(t); len(r) > 60 {
					t = string(r[:60]) + "…"
				}
				add("srcref", p, fmt.Sprintf("%d. %s", i+1, t))
			}
		}
		// порядок первого упоминания (ГОСТ 7.32: в порядке появления ссылок в тексте)
		if len(order) > 2 && !sort.IntsAreSorted(order) {
			bad := 0
			for k := 1; k < len(order); k++ {
				if order[k] < order[k-1] {
					bad++
				}
			}
			if bad*5 > len(order) {
				add("srcorder", bibEntries[0], "")
			}
		}
	}
	return out
}

type normCap struct {
	p   *Para
	num string
}

func inList(ps []*Para, p *Para) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}

func firstTop(es []normEntry, start int) int {
	if start < len(es) {
		return es[start].top
	}
	return 0
}

func firstParaIn(t *Table) *Para {
	var out *Para
	walkParas([]Block{{T: t}}, func(p *Para, _ bool) {
		if out == nil {
			out = p
		}
	})
	return out
}

func bibParaFor(es []normEntry, s *Source) *Para {
	for _, e := range es {
		if e.p.PF.Region == "bib" && strings.Contains(e.text, s.Title) && s.Title != "" {
			return e.p
		}
	}
	return nil
}

func normSrcName(s *Source) string {
	t := strings.TrimSpace(s.Authors)
	if t != "" {
		t += ". "
	}
	t += s.Title
	if r := []rune(t); len(r) > 60 {
		t = string(r[:60]) + "…"
	}
	return t
}

func firstTextF(p *Para) F {
	for _, r := range p.Runs {
		if r.T == RText && strings.TrimSpace(string(r.Text)) != "" {
			return r.F
		}
	}
	return p.Mark
}

// capFmtProblem — «Рисунок 1 – Название»: полное слово, тире, без точки в конце.
func capFmtProblem(m []string, txt string) string {
	if !strings.HasSuffix(m[1], "ок") && !strings.HasSuffix(m[1], "ца") {
		return "сокращение «" + m[1] + "» — пишут полностью"
	}
	rest := strings.TrimSpace(txt[len(m[0])-len(m[3]):])
	if rest == "" {
		return "нет названия"
	}
	switch m[3] {
	case "–", "—":
	case "-":
		return "дефис вместо тире: «" + m[1] + " " + m[2] + " – …»"
	case ".", ":":
		return "после номера ставят тире, а не «" + m[3] + "»"
	default:
		return "номер и название разделяют тире: «" + m[1] + " " + m[2] + " – …»"
	}
	if strings.HasSuffix(strings.TrimSpace(txt), ".") && !strings.HasSuffix(strings.TrimSpace(txt), "..") {
		return "точку в конце названия не ставят"
	}
	return ""
}

// breaksBefore — начинается ли абзац es[i] с новой страницы: разрыв страницы, разрыв раздела или
// пустой хвост предыдущей страницы не считаем — только явные разрывы.
func breaksBefore(d *Doc, es []normEntry, i int) bool {
	top := es[i].top
	for j := top - 1; j >= 0; j-- {
		b := d.Body[j]
		if b.P == nil {
			return false
		}
		if s := b.P.PF.Sect; s != nil && s.Type != "continuous" {
			return true
		}
		if strings.TrimSpace(b.P.Text()) != "" || paraHas(b.P, RImage) {
			return false
		}
		if b.P.PF.PB {
			return true
		}
	}
	return true
}

// checkBodyPara — шрифт, кегль, цвет, интервал, отступ и выравнивание обычного абзаца.
func checkBodyPara(d *Doc, p *Para, pf EPF, inBib bool, add func(kind string, p *Para, msg string)) {
	font, size, color := "", 0.0, ""
	small := false
	for _, r := range p.Runs {
		if r.T != RText || strings.TrimSpace(string(r.Text)) == "" || r.F.Del != "" {
			continue
		}
		f := EffF(d, p, r.F)
		if f.Sup || f.Sub {
			continue
		}
		if !strings.EqualFold(f.Font, normFont) && font == "" {
			font = f.Font
		}
		if f.Size < normMinSz-0.01 {
			small = true
		}
		if size == 0 || math.Abs(f.Size-normSize) > math.Abs(size-normSize) {
			size = f.Size
		}
		if c := strings.ToLower(strings.TrimPrefix(f.Color, "#")); c != "" && c != "000000" && c != "auto" && f.Link == "" && color == "" {
			color = f.Color
		}
	}
	if font != "" {
		add("font", p, font)
	}
	if small {
		add("size", p, fmtNum(size)+" пт")
	} else if size > 0 && math.Abs(size-normSize) > 0.01 {
		add("sizemix", p, fmtNum(size)+" пт")
	}
	if color != "" {
		add("color", p, color)
	}
	lineOK := pf.LineExact == 0 && math.Abs(pf.Line-normLine) < 0.06
	if pf.LineExact > 0 && pf.LineRule != "atLeast" {
		lineOK = math.Abs(pf.LineExact-normLine*size*1.15) < 2 // точно 21 пт при 14 — тоже «полуторный»
	}
	if !lineOK {
		v := fmtNum(math.Round(pf.Line*100) / 100)
		if pf.LineExact > 0 {
			v = "точно " + fmtNum(pf.LineExact) + " пт"
		}
		add("line", p, v)
	}
	if inBib || pf.List != nil || pf.Align == "center" {
		return
	}
	if math.Abs(pf.IndF-normIndent) > normTol/2 && pf.IndF >= 0 {
		add("indent", p, fmtNum(math.Round(pf.IndF/CM*100)/100)+" см")
	}
	if pf.Align != "justify" && len([]rune(strings.TrimSpace(p.Text()))) > 80 {
		add("align", p, map[string]string{"left": "по левому краю", "center": "по центру", "right": "по правому краю"}[pf.Align])
	}
}

// walkParas — все абзацы блоков, включая ячейки таблиц.
func walkParas(bs []Block, fn func(p *Para, cell bool)) {
	var rec func(bs []Block, cell bool)
	rec = func(bs []Block, cell bool) {
		for _, b := range bs {
			if b.P != nil {
				fn(b.P, cell)
			}
			if b.T != nil {
				for _, r := range b.T.Rows {
					for _, c := range r.Cells {
						rec(c.Blocks, true)
					}
				}
			}
		}
	}
	rec(bs, false)
}

// NormFix — исправить все замечания вида kind. Возвращает число исправленных мест.
func NormFix(d *Doc, issues []NormIssue, kind string) int {
	n := 0
	done := map[*Para]bool{}
	for _, is := range issues {
		if is.Kind != kind {
			continue
		}
		p := is.P
		if p != nil {
			if done[p] {
				continue
			}
			done[p] = true
		}
		switch kind {
		case "margins":
			setSecPage(d, is.Sec, func(pg *Page) { pg.Left, pg.Right, pg.Top, pg.Bottom = normLeft, normRight, normTop, normBottom })
		case "titlepg":
			if ss := d.Sections(); ss[0].Owner != nil {
				ss[0].Owner.TitlePg = true
			} else {
				d.TitlePg = true
			}
		case "font", "size", "sizemix", "color":
			for i := range p.Runs {
				r := &p.Runs[i]
				switch kind {
				case "font":
					r.F.Font = normFont
				case "size", "sizemix":
					if !r.F.Sup && !r.F.Sub {
						r.F.Size = normSize
					}
				case "color":
					if r.F.Link == "" {
						r.F.Color = ""
					}
				}
			}
			switch kind {
			case "font":
				p.Mark.Font = normFont
			case "size", "sizemix":
				p.Mark.Size = normSize
			}
		case "line":
			p.PF.Line, p.PF.LineExact, p.PF.LineRule = O(normLine), 0, ""
		case "indent":
			p.PF.IndF = O(normIndent)
		case "align":
			p.PF.Align = "justify"
		case "hdot":
			trimTail(p, ".")
		case "hcase":
			p.PF.Align = "center"
			for i := range p.Runs {
				if p.Runs[i].T == RText {
					p.Runs[i].Text = []rune(strings.ToUpper(string(p.Runs[i].Text)))
				}
			}
		case "hpage":
			p.PF.PB = true
		case "figfmt", "tabfmt":
			fixCaption(p, kind == "figfmt")
		case "empty":
			// лишние пустые абзацы перед разрывом — убрать, следующий непустой — с новой страницы
			n += removeEmptyRun(d, p)
			p.Touch()
			continue
		case "spaces", "dash":
			for i := range p.Runs {
				if p.Runs[i].T != RText {
					continue
				}
				s := string(p.Runs[i].Text)
				if kind == "spaces" {
					for strings.Contains(s, "  ") {
						s = strings.ReplaceAll(s, "  ", " ")
					}
				} else {
					s = strings.ReplaceAll(s, " - ", " – ")
				}
				p.Runs[i].Text = []rune(s)
			}
		default:
			continue
		}
		if p != nil {
			p.Touch()
		}
		n++
	}
	return n
}

func setSecPage(d *Doc, si int, fn func(pg *Page)) {
	ss := d.Sections()
	if si < 0 || si >= len(ss) {
		return
	}
	if o := ss[si].Owner; o != nil {
		fn(&o.Page)
	} else {
		fn(&d.Page)
	}
}

func trimTail(p *Para, suffix string) {
	for i := len(p.Runs) - 1; i >= 0; i-- {
		r := &p.Runs[i]
		if r.T != RText {
			continue
		}
		s := strings.TrimRight(string(r.Text), " ")
		if s == "" {
			continue
		}
		r.Text = []rune(strings.TrimSuffix(s, suffix))
		return
	}
}

// fixCaption — полное слово, тире после номера, без точки в конце; рисунок — по центру, таблица — слева.
func fixCaption(p *Para, fig bool) {
	re := reTabCap
	word := "Таблица"
	if fig {
		re, word = reFigCap, "Рисунок"
	}
	if len(p.Runs) > 0 && p.Runs[0].T == RText {
		s := string(p.Runs[0].Text)
		if m := re.FindStringSubmatchIndex(s); m != nil {
			num := s[m[4]:m[5]]
			rest := strings.TrimLeft(s[m[6]:], " –—-.:")
			if m[7] > m[6] {
				switch s[m[6]:m[7]] {
				case "–", "—", "-", ".", ":":
				default:
					rest = strings.TrimLeft(s[m[6]:], " ")
				}
			}
			p.Runs[0].Text = []rune(word + " " + num + " – " + rest)
		} else if m := regexp.MustCompile(`^(Рисунок|Рис\.?|Таблица|Табл\.?)\s*`).FindStringIndex(s); m != nil {
			// номер — поле SEQ в следующем куске
			p.Runs[0].Text = []rune(word + " " + s[m[1]:])
			for i := 1; i+1 < len(p.Runs); i++ {
				if p.Runs[i].T == RField && p.Runs[i+1].T == RText {
					t := strings.TrimLeft(string(p.Runs[i+1].Text), " –—-.:")
					p.Runs[i+1].Text = []rune(" – " + t)
					break
				}
			}
		}
	}
	trimTail(p, ".")
	if fig {
		p.PF.Align = "center"
		p.PF.IndF, p.PF.IndL = O(0), O(0)
	} else {
		p.PF.Align = ""
		p.PF.IndF, p.PF.IndL = O(0), O(0)
	}
}

// removeEmptyRun — убрать пустые абзацы подряд, начиная с p (оставив один), и поставить разрыв страницы
// перед следующим непустым абзацем.
func removeEmptyRun(d *Doc, p *Para) int {
	start := -1
	for i, b := range d.Body {
		if b.P == p {
			start = i
			break
		}
	}
	if start < 0 {
		return 0
	}
	// начало серии — назад до непустого
	for start > 0 && d.Body[start-1].P != nil && isEmptyPara(d.Body[start-1].P) {
		start--
	}
	end := start
	for end < len(d.Body) && d.Body[end].P != nil && isEmptyPara(d.Body[end].P) && d.Body[end].P.PF.Sect == nil {
		end++
	}
	if end >= len(d.Body) || end-start < 2 {
		return 0
	}
	next := d.Body[end]
	if next.P == nil {
		return 0
	}
	next.P.PF.PB = true
	next.P.Touch()
	d.Body = append(d.Body[:start], d.Body[end:]...)
	return end - start
}

func isEmptyPara(p *Para) bool {
	if p.PF.PB {
		return false
	}
	for _, r := range p.Runs {
		if r.T != RText || strings.TrimSpace(string(r.Text)) != "" {
			return false
		}
	}
	return true
}
