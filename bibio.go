package main

// Литература: импорт и экспорт BibTeX и RIS (их отдают eLibrary, КиберЛенинка, Google Scholar,
// Zotero, Mendeley), поиск дублей и их объединение.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------- имена авторов

// normAuthor — «Иванов, Иван Петрович», «Иван Петрович Иванов», «Ivanov, I. P.» → «Иванов И. П.».
func normAuthor(s string) string {
	s = strings.TrimSpace(strings.NewReplacer("{", "", "}", "").Replace(s))
	if s == "" {
		return ""
	}
	var sur string
	var given []string
	if i := strings.Index(s, ","); i >= 0 {
		sur = strings.TrimSpace(s[:i])
		given = strings.Fields(strings.ReplaceAll(s[i+1:], ".", ". "))
	} else {
		f := strings.Fields(strings.ReplaceAll(s, ".", ". "))
		if len(f) == 1 {
			return f[0]
		}
		// «И. П. Иванов» или «Иван Петрович Иванов» — фамилия последней; «Иванов И. П.» — первой
		if strings.HasSuffix(f[len(f)-1], ".") {
			sur, given = f[0], f[1:]
		} else {
			sur, given = f[len(f)-1], f[:len(f)-1]
		}
	}
	var ini []string
	for _, g := range given {
		for _, part := range strings.Split(g, "-") {
			r := []rune(strings.TrimSpace(strings.TrimSuffix(part, ".")))
			if len(r) > 0 && unicode.IsLetter(r[0]) {
				ini = append(ini, string(unicode.ToUpper(r[0]))+".")
			}
		}
	}
	if len(ini) == 0 {
		return sur
	}
	return sur + " " + strings.Join(ini, " ")
}

// ---------------------------------------------------------------- BibTeX

var bibTypes = map[string]string{"book": "book", "inbook": "book", "incollection": "article", "booklet": "book", "techreport": "book",
	"manual": "book", "article": "article", "inproceedings": "article", "conference": "article", "online": "web", "www": "web",
	"electronic": "web", "misc": "web", "phdthesis": "thesis", "mastersthesis": "thesis", "thesis": "thesis", "standard": "standard",
	"patent": "patent", "legislation": "law", "law": "law", "unpublished": "book", "proceedings": "book", "report": "book"}

// latexText — убирает фигурные скобки и простые команды LaTeX.
func latexText(s string) string {
	r := strings.NewReplacer(`\&`, "&", `\%`, "%", `\_`, "_", `\$`, "$", `\#`, "#", "--", "–", "~", " ",
		`\"o`, "ö", `\"a`, "ä", `\"u`, "ü", `\'e`, "é", `\'a`, "á", `\textendash`, "–", `\textemdash`, "—", `\guillemotleft`, "«", `\guillemotright`, "»")
	s = r.Replace(s)
	s = strings.NewReplacer("{", "", "}", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// ParseBibTeX разбирает записи @type{key, field = {…} | "…" | число, …}.
func ParseBibTeX(src string) []*Source {
	var out []*Source
	i := 0
	for {
		at := strings.IndexByte(src[i:], '@')
		if at < 0 {
			break
		}
		i += at + 1
		j := i
		for j < len(src) && (unicode.IsLetter(rune(src[j]))) {
			j++
		}
		typ := strings.ToLower(src[i:j])
		for j < len(src) && src[j] != '{' && src[j] != '(' {
			j++
		}
		if j >= len(src) {
			break
		}
		// конец записи — по балансу скобок
		depth, k := 0, j
		for ; k < len(src); k++ {
			switch src[k] {
			case '{', '(':
				depth++
			case '}', ')':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		body := src[j+1 : min(k, len(src))]
		i = min(k+1, len(src))
		if typ == "comment" || typ == "string" || typ == "preamble" {
			continue
		}
		fields := bibFields(body)
		if len(fields) == 0 {
			continue
		}
		out = append(out, sourceFromFields(typ, fields))
	}
	return out
}

// bibFields — поля одной записи (ключ записи пропускается).
func bibFields(body string) map[string]string {
	f := map[string]string{}
	if c := strings.IndexByte(body, ','); c >= 0 && !strings.Contains(body[:c], "=") {
		body = body[c+1:]
	}
	i := 0
	for i < len(body) {
		eq := strings.IndexByte(body[i:], '=')
		if eq < 0 {
			break
		}
		name := strings.ToLower(strings.TrimSpace(strings.Trim(body[i:i+eq], ", \n\r\t")))
		i += eq + 1
		for i < len(body) && unicode.IsSpace(rune(body[i])) {
			i++
		}
		var val strings.Builder
		for i < len(body) {
			c := body[i]
			switch {
			case c == '{':
				depth, k := 0, i
				for ; k < len(body); k++ {
					if body[k] == '{' {
						depth++
					} else if body[k] == '}' {
						depth--
						if depth == 0 {
							break
						}
					}
				}
				val.WriteString(body[i+1 : min(k, len(body))])
				i = k + 1
			case c == '"':
				k := strings.IndexByte(body[i+1:], '"')
				if k < 0 {
					k = len(body) - i - 1
				}
				val.WriteString(body[i+1 : i+1+k])
				i += k + 2
			default:
				k := i
				for k < len(body) && body[k] != ',' && body[k] != '#' && !unicode.IsSpace(rune(body[k])) {
					k++
				}
				val.WriteString(body[i:k])
				i = k
			}
			for i < len(body) && unicode.IsSpace(rune(body[i])) {
				i++
			}
			if i < len(body) && body[i] == '#' { // склейка строк
				i++
				for i < len(body) && unicode.IsSpace(rune(body[i])) {
					i++
				}
				continue
			}
			break
		}
		if name != "" {
			f[name] = latexText(val.String())
		}
		for i < len(body) && body[i] != ',' {
			i++
		}
		i++
	}
	return f
}

var isoDate = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})`)

// ruDate — 2024-03-05 → 05.03.2024 (так пишут дату обращения по ГОСТ).
func ruDate(s string) string {
	if m := isoDate.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		return m[3] + "." + m[2] + "." + m[1]
	}
	return strings.TrimSpace(s)
}

func sourceFromFields(typ string, f map[string]string) *Source {
	s := &Source{ID: NewID("src"), Type: bibTypes[typ]}
	if s.Type == "" {
		s.Type = "book"
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(f[k]); v != "" {
				return v
			}
		}
		return ""
	}
	var as []string
	for _, a := range regexp.MustCompile(`(?i)\s+and\s+`).Split(first("author", "editor", "inventor"), -1) {
		if n := normAuthor(a); n != "" && !strings.EqualFold(n, "others") {
			as = append(as, n)
		}
	}
	s.Authors = strings.Join(as, ", ")
	s.Title = first("title")
	s.Extra = first("subtitle", "type", "howpublished")
	s.City = first("address", "location")
	s.Publisher = first("publisher", "organization", "institution", "school", "holder")
	s.Year = first("year")
	if s.Year == "" {
		if d := first("date"); len(d) >= 4 {
			s.Year = d[:4]
		}
	}
	s.Pages = strings.ReplaceAll(first("pages", "pagetotal"), "--", "–")
	s.Journal = first("journal", "journaltitle", "booktitle")
	s.Issue = first("number", "issue")
	if s.Type == "standard" || s.Type == "patent" {
		s.Number, s.Issue = first("number"), ""
	}
	s.URL = first("url")
	if s.URL == "" {
		if doi := first("doi"); doi != "" {
			s.URL = "https://doi.org/" + strings.TrimPrefix(doi, "https://doi.org/")
		}
	}
	s.Accessed = ruDate(first("urldate"))
	if s.Type == "web" && s.URL == "" && s.Journal == "" {
		s.Type = "book" // @misc без ссылки — скорее печатное издание
	}
	return s
}

// bibAuthors — «Иванов И. П., Петров П.» → «Иванов, И. П. and Петров, П.».
func bibAuthors(s string) string {
	var out []string
	for _, a := range parseAuthors(s) {
		if a.ini != "" {
			out = append(out, a.sur+", "+a.ini)
		} else {
			out = append(out, a.sur)
		}
	}
	return strings.Join(out, " and ")
}

// ExportBibTeX — все источники в BibTeX.
func ExportBibTeX(ss []*Source) string {
	inv := map[string]string{"book": "book", "article": "article", "web": "online", "thesis": "phdthesis", "law": "legislation",
		"standard": "standard", "patent": "patent"}
	var b strings.Builder
	used := map[string]int{}
	for _, s := range ss {
		key := bibKey(s)
		used[key]++
		if used[key] > 1 {
			key += string(rune('a' + used[key] - 2))
		}
		fmt.Fprintf(&b, "@%s{%s,\n", inv[s.Type], key)
		put := func(k, v string) {
			if v = strings.TrimSpace(v); v != "" {
				fmt.Fprintf(&b, "  %s = {%s},\n", k, strings.NewReplacer("{", `\{`, "}", `\}`).Replace(v))
			}
		}
		put("author", bibAuthors(s.Authors))
		put("title", s.Title)
		put("subtitle", s.Extra)
		put("address", s.City)
		put("publisher", s.Publisher)
		put("year", s.Year)
		put("pages", strings.ReplaceAll(s.Pages, "–", "--"))
		put("journal", s.Journal)
		put("number", map[bool]string{true: s.Number, false: s.Issue}[s.Type == "standard" || s.Type == "patent"])
		put("url", s.URL)
		if m := regexp.MustCompile(`^(\d{2})\.(\d{2})\.(\d{4})$`).FindStringSubmatch(s.Accessed); m != nil {
			put("urldate", m[3]+"-"+m[2]+"-"+m[1])
		} else {
			put("urldate", s.Accessed)
		}
		b.WriteString("}\n\n")
	}
	return b.String()
}

var translit = map[rune]string{'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh",
	'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ы': "y", 'э': "e", 'ю': "yu", 'я': "ya"}

// bibKey — ключ записи: фамилия первого автора латиницей + год («ivanov2020»).
func bibKey(s *Source) string {
	base := "src"
	if as := parseAuthors(s.Authors); len(as) > 0 {
		base = as[0].sur
	} else if w := strings.Fields(s.Title); len(w) > 0 {
		base = w[0]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case translit[r] != "":
			b.WriteString(translit[r])
		}
	}
	k := b.String()
	if k == "" {
		k = "src"
	}
	return k + strings.TrimSpace(s.Year)
}

// ---------------------------------------------------------------- RIS

var risTypes = map[string]string{"BOOK": "book", "CHAP": "book", "EBOOK": "book", "EDBOOK": "book", "JOUR": "article", "EJOUR": "article",
	"MGZN": "article", "NEWS": "article", "CONF": "article", "CPAPER": "article", "ELEC": "web", "WEB": "web", "BLOG": "web",
	"THES": "thesis", "STAND": "standard", "PAT": "patent", "STAT": "law", "LEGAL": "law", "BILL": "law", "RPRT": "book", "GEN": "book"}

// ParseRIS разбирает записи «TY  - …» … «ER  - ».
func ParseRIS(src string) []*Source {
	var out []*Source
	var cur *Source
	var as []string
	var sp, ep string
	flush := func() {
		if cur == nil {
			return
		}
		cur.Authors = strings.Join(as, ", ")
		switch {
		case sp != "" && ep != "":
			cur.Pages = sp + "–" + ep
		case sp != "":
			cur.Pages = sp
		}
		if cur.Title != "" || cur.Authors != "" {
			out = append(out, cur)
		}
		cur, as, sp, ep = nil, nil, "", ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(src, "\r", ""), "\n") {
		if len(line) < 5 || line[2:5] != "  -" {
			continue
		}
		tag := strings.TrimSpace(line[:2])
		val := ""
		if len(line) > 6 {
			val = strings.TrimSpace(line[6:])
		}
		if tag == "TY" {
			flush()
			cur = &Source{ID: NewID("src"), Type: risTypes[strings.ToUpper(val)]}
			if cur.Type == "" {
				cur.Type = "book"
			}
			continue
		}
		if cur == nil {
			continue
		}
		set := func(dst *string) {
			if *dst == "" {
				*dst = val
			}
		}
		switch tag {
		case "ER":
			flush()
		case "AU", "A1", "A2":
			if n := normAuthor(val); n != "" {
				as = append(as, n)
			}
		case "TI", "T1":
			set(&cur.Title)
		case "T2", "JO", "JF", "JA", "BT":
			set(&cur.Journal)
		case "PY", "Y1", "DA":
			if len(val) >= 4 && cur.Year == "" {
				cur.Year = val[:4]
			}
		case "SP":
			sp = val
		case "EP":
			ep = val
		case "CY", "PP":
			set(&cur.City)
		case "PB":
			set(&cur.Publisher)
		case "IS":
			set(&cur.Issue)
		case "VL":
			if cur.Issue == "" {
				cur.Issue = val
			}
		case "UR", "L2":
			set(&cur.URL)
		case "DO":
			if cur.URL == "" && val != "" {
				cur.URL = "https://doi.org/" + strings.TrimPrefix(val, "https://doi.org/")
			}
		case "Y2":
			set(&cur.Accessed)
			cur.Accessed = ruDate(strings.ReplaceAll(cur.Accessed, "/", "-"))
		case "M3": // тип работы — у нас «сведения к заглавию»
			set(&cur.Extra)
		case "M1", "SN":
			if cur.Type == "patent" || cur.Type == "standard" {
				set(&cur.Number)
			}
		}
	}
	flush()
	return out
}

// ExportRIS — все источники в RIS.
func ExportRIS(ss []*Source) string {
	inv := map[string]string{"book": "BOOK", "article": "JOUR", "web": "ELEC", "thesis": "THES", "law": "STAT", "standard": "STAND", "patent": "PAT"}
	var b strings.Builder
	for _, s := range ss {
		put := func(tag, v string) {
			if v = strings.TrimSpace(v); v != "" {
				fmt.Fprintf(&b, "%s  - %s\r\n", tag, v)
			}
		}
		put("TY", inv[s.Type])
		for _, a := range parseAuthors(s.Authors) {
			if a.ini != "" {
				put("AU", a.sur+", "+a.ini)
			} else {
				put("AU", a.sur)
			}
		}
		put("TI", s.Title)
		put("T2", s.Journal)
		put("M3", s.Extra)
		put("PY", s.Year)
		pg := strings.Split(strings.NewReplacer("–", "-", "—", "-").Replace(s.Pages), "-")
		if len(pg) == 2 {
			put("SP", pg[0])
			put("EP", pg[1])
		} else {
			put("SP", s.Pages)
		}
		put("CY", s.City)
		put("PB", s.Publisher)
		put("IS", s.Issue)
		put("M1", s.Number)
		put("UR", s.URL)
		put("Y2", s.Accessed)
		b.WriteString("ER  - \r\n\r\n")
	}
	return b.String()
}

// ParseBibliography — BibTeX или RIS (определяется по содержимому).
func ParseBibliography(src string) []*Source {
	src = strings.TrimPrefix(src, "\ufeff")
	if regexp.MustCompile(`(?m)^TY  - `).MatchString(src) {
		return ParseRIS(src)
	}
	return ParseBibTeX(src)
}

// ---------------------------------------------------------------- дубли

func normTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r == 'ё' {
				r = 'е'
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstSurname(s *Source) string {
	if as := parseAuthors(s.Authors); len(as) > 0 {
		return strings.ToLower(strings.ReplaceAll(as[0].sur, "ё", "е"))
	}
	return ""
}

// IsDuplicate — один и тот же источник: совпадает заглавие (без регистра и знаков), а год и первый
// автор не противоречат друг другу (пустое поле не мешает).
func IsDuplicate(a, b *Source) bool {
	ta, tb := normTitle(a.Title), normTitle(b.Title)
	if ta == "" || ta != tb {
		if !(a.URL != "" && strings.TrimRight(a.URL, "/") == strings.TrimRight(b.URL, "/")) {
			return false
		}
	}
	if a.Year != "" && b.Year != "" && strings.TrimSpace(a.Year) != strings.TrimSpace(b.Year) {
		return false
	}
	if sa, sb := firstSurname(a), firstSurname(b); sa != "" && sb != "" && sa != sb {
		return false
	}
	return true
}

// FindDuplicates — группы одинаковых источников (в каждой первый — тот, что остаётся).
func FindDuplicates(ss []*Source) [][]*Source {
	var groups [][]*Source
	used := map[*Source]bool{}
	for i, a := range ss {
		if used[a] {
			continue
		}
		g := []*Source{a}
		for _, b := range ss[i+1:] {
			if !used[b] && IsDuplicate(a, b) {
				g = append(g, b)
				used[b] = true
			}
		}
		if len(g) > 1 {
			groups = append(groups, g)
		}
	}
	return groups
}

// mergeSource — недостающие поля первого берутся из второго.
func mergeSource(dst, src *Source) {
	fill := func(d *string, s string) {
		if strings.TrimSpace(*d) == "" {
			*d = s
		}
	}
	fill(&dst.Authors, src.Authors)
	fill(&dst.Extra, src.Extra)
	fill(&dst.City, src.City)
	fill(&dst.Publisher, src.Publisher)
	fill(&dst.Year, src.Year)
	fill(&dst.Pages, src.Pages)
	fill(&dst.Journal, src.Journal)
	fill(&dst.Issue, src.Issue)
	fill(&dst.URL, src.URL)
	fill(&dst.Accessed, src.Accessed)
	fill(&dst.Number, src.Number)
}

// citeIDs — список id в поле «CITE id1,id2 [страницы]».
var citeRe = regexp.MustCompile(`^CITE\s+(\S+)(.*)$`)

// MergeDuplicates объединяет дубли: ссылки в тексте переводятся на оставшийся источник. Возвращает,
// сколько источников убрано.
func (e *Editor) MergeDuplicates() int {
	d := e.Doc
	groups := FindDuplicates(d.Sources)
	if len(groups) == 0 {
		return 0
	}
	e.Checkpoint("")
	to := map[string]string{}
	drop := map[*Source]bool{}
	for _, g := range groups {
		for _, x := range g[1:] {
			mergeSource(g[0], x)
			to[x.ID] = g[0].ID
			drop[x] = true
		}
	}
	var keep []*Source
	for _, s := range d.Sources {
		if !drop[s] {
			keep = append(keep, s)
		}
	}
	d.Sources = keep
	fix := func(p *Para) {
		for i, r := range p.Runs {
			if r.T != RField {
				continue
			}
			m := citeRe.FindStringSubmatch(r.Name)
			if m == nil {
				continue
			}
			ids := strings.Split(m[1], ",")
			var out []string
			seen := map[string]bool{}
			for _, id := range ids {
				if n, ok := to[id]; ok {
					id = n
				}
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			p.Runs[i].Name = "CITE " + strings.Join(out, ",") + m[2]
			p.Touch()
		}
	}
	for _, en := range Flatten(&d.Body, nil, nil) {
		fix(en.P)
	}
	for _, n := range d.Footnotes {
		for _, en := range Flatten(&n.Blocks, nil, nil) {
			fix(en.P)
		}
	}
	UpdateBib(d)
	e.Changed()
	return len(drop)
}

// ImportSources добавляет источники, пропуская те, что уже есть. Возвращает: добавлено, пропущено.
func (e *Editor) ImportSources(ss []*Source) (int, int) {
	if len(ss) == 0 {
		return 0, 0
	}
	e.Checkpoint("")
	added, skipped := 0, 0
	for _, s := range ss {
		dup := false
		for _, x := range e.Doc.Sources {
			if IsDuplicate(x, s) {
				mergeSource(x, s)
				dup = true
				break
			}
		}
		if dup {
			skipped++
			continue
		}
		e.Doc.Sources = append(e.Doc.Sources, s)
		added++
	}
	UpdateBib(e.Doc)
	e.Changed()
	return added, skipped
}
