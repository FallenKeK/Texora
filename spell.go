package main

// Проверка орфографии: свой движок для словарей Hunspell (ru_RU, en_US из LibreOffice).
// Аффиксы PFX/SFX, перекрёстные формы, REP и TRY для подсказок, регистр, слова через дефис.

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

type affRule struct {
	flag  rune
	strip []rune
	add   []rune
	cond  []condEl
	cross bool
	cont  string
}

type condEl struct {
	any bool
	neg bool
	set string
}

type spellDict struct {
	words    map[string]string // слово → флаги
	sfx      map[string][]*affRule
	pfx      map[string][]*affRule
	rep      [][2]string
	try      []rune
	needAff  rune
	forbid   rune
	noSugg   rune
	onlyComp rune
	iconv    [][2]string
}

func parseCond(s string) []condEl {
	if s == "." || s == "" {
		return nil
	}
	var out []condEl
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		switch r[i] {
		case '.':
			out = append(out, condEl{any: true})
		case '[':
			j := i + 1
			neg := false
			if j < len(r) && r[j] == '^' {
				neg = true
				j++
			}
			k := j
			for k < len(r) && r[k] != ']' {
				k++
			}
			out = append(out, condEl{neg: neg, set: string(r[j:k])})
			i = k
		default:
			out = append(out, condEl{set: string(r[i])})
		}
	}
	return out
}

func condMatch(c []condEl, s []rune, atEnd bool) bool {
	if len(c) > len(s) {
		return false
	}
	off := 0
	if atEnd {
		off = len(s) - len(c)
	}
	for i, e := range c {
		ch := s[off+i]
		if e.any {
			continue
		}
		in := strings.ContainsRune(e.set, ch)
		if in == e.neg {
			return false
		}
	}
	return true
}

func loadSpell(aff, dic []byte) *spellDict {
	d := &spellDict{words: map[string]string{}, sfx: map[string][]*affRule{}, pfx: map[string][]*affRule{}}
	sc := bufio.NewScanner(bytes.NewReader(aff))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	cross := map[string]bool{}
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		flag := func(i int) rune {
			if i < len(f) {
				return []rune(f[i])[0]
			}
			return 0
		}
		switch f[0] {
		case "TRY":
			if len(f) > 1 {
				for _, r := range f[1] {
					if !unicode.IsUpper(r) {
						d.try = append(d.try, r)
					}
				}
			}
		case "REP":
			if len(f) == 3 {
				d.rep = append(d.rep, [2]string{strings.ReplaceAll(f[1], "_", " "), strings.ReplaceAll(f[2], "_", " ")})
			}
		case "ICONV":
			if len(f) == 3 {
				d.iconv = append(d.iconv, [2]string{f[1], f[2]})
			}
		case "NEEDAFFIX":
			d.needAff = flag(1)
		case "FORBIDDENWORD":
			d.forbid = flag(1)
		case "NOSUGGEST":
			d.noSugg = flag(1)
		case "ONLYINCOMPOUND":
			d.onlyComp = flag(1)
		case "PFX", "SFX":
			if len(f) == 4 {
				if _, err := strconv.Atoi(f[3]); err == nil {
					cross[f[0]+f[1]] = f[2] == "Y"
					continue
				}
			}
			if len(f) < 5 {
				continue
			}
			r := &affRule{flag: flag(1), cross: cross[f[0]+f[1]]}
			if f[2] != "0" {
				r.strip = []rune(f[2])
			}
			add := f[3]
			if i := strings.IndexByte(add, '/'); i >= 0 {
				r.cont = add[i+1:]
				add = add[:i]
			}
			if add != "0" {
				r.add = []rune(add)
			}
			r.cond = parseCond(f[4])
			if f[0] == "SFX" {
				d.sfx[string(r.add)] = append(d.sfx[string(r.add)], r)
			} else {
				d.pfx[string(r.add)] = append(d.pfx[string(r.add)], r)
			}
		}
	}
	sc = bufio.NewScanner(bytes.NewReader(dic))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if _, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
				continue
			}
		}
		if i := strings.IndexAny(line, "\t "); i >= 0 {
			line = line[:i]
		}
		if line == "" {
			continue
		}
		w, fl := line, ""
		if i := strings.IndexByte(line, '/'); i > 0 {
			w, fl = line[:i], line[i+1:]
		}
		if old, ok := d.words[w]; ok {
			fl = old + fl
		}
		d.words[w] = fl
	}
	return d
}

func (d *spellDict) has(stem string, flag rune) (string, bool) {
	fl, ok := d.words[stem]
	if !ok {
		return "", false
	}
	if flag != 0 && !strings.ContainsRune(fl, flag) {
		return "", false
	}
	return fl, true
}

// lookup — слово в словаре как есть (с учётом служебных флагов).
func (d *spellDict) lookup(w string) bool {
	fl, ok := d.words[w]
	if !ok {
		return false
	}
	if d.forbid != 0 && strings.ContainsRune(fl, d.forbid) {
		return false
	}
	if d.needAff != 0 && strings.ContainsRune(fl, d.needAff) {
		return false
	}
	if d.onlyComp != 0 && strings.ContainsRune(fl, d.onlyComp) {
		return false
	}
	return true
}

func (d *spellDict) sfxCheck(w []rune, needCross bool, pflag rune) bool {
	for i := 0; i <= len(w); i++ {
		rules := d.sfx[string(w[i:])]
		if len(rules) == 0 {
			continue
		}
		base := w[:i]
		if len(base) == 0 {
			continue
		}
		for _, r := range rules {
			if needCross && !r.cross {
				continue
			}
			stem := append(append([]rune{}, base...), r.strip...)
			if !condMatch(r.cond, stem, true) {
				continue
			}
			fl, ok := d.has(string(stem), r.flag)
			if !ok {
				continue
			}
			if pflag != 0 && !strings.ContainsRune(fl, pflag) {
				continue
			}
			if d.forbid != 0 && strings.ContainsRune(fl, d.forbid) {
				continue
			}
			return true
		}
	}
	return false
}

func (d *spellDict) pfxCheck(w []rune) bool {
	for i := 1; i <= len(w); i++ {
		rules := d.pfx[string(w[:i])]
		for _, r := range rules {
			stem := append(append([]rune{}, r.strip...), w[i:]...)
			if len(stem) == 0 || !condMatch(r.cond, stem, false) {
				continue
			}
			if fl, ok := d.has(string(stem), r.flag); ok && !(d.forbid != 0 && strings.ContainsRune(fl, d.forbid)) {
				return true
			}
			if r.cross && d.sfxCheck(stem, true, r.flag) {
				return true
			}
		}
	}
	return false
}

func (d *spellDict) checkExact(w string) bool {
	if d.lookup(w) {
		return true
	}
	rw := []rune(w)
	return d.sfxCheck(rw, false, 0) || d.pfxCheck(rw)
}

func caseKind(w string) int { // 0 нижний, 1 С заглавной, 2 ВСЕ, 3 сМеШаНнЫй
	up, low := 0, 0
	first := true
	firstUp := false
	for _, r := range w {
		if !unicode.IsLetter(r) {
			continue
		}
		if unicode.IsUpper(r) {
			up++
			if first {
				firstUp = true
			}
		} else {
			low++
		}
		first = false
	}
	switch {
	case up == 0:
		return 0
	case low == 0:
		return 2
	case up == 1 && firstUp:
		return 1
	}
	return 3
}

func lowerFirst(w string) string {
	r := []rune(w)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func titleCase(w string) string {
	r := []rune(strings.ToLower(w))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func (d *spellDict) Check(w string) bool {
	for _, c := range d.iconv {
		w = strings.ReplaceAll(w, c[0], c[1])
	}
	try := func(s string) bool {
		if d.checkExact(s) {
			return true
		}
		if strings.ContainsAny(s, "ёЁ") {
			return d.checkExact(strings.NewReplacer("ё", "е", "Ё", "Е").Replace(s))
		}
		return false
	}
	if try(w) {
		return true
	}
	switch caseKind(w) {
	case 1:
		return try(lowerFirst(w))
	case 2:
		return try(strings.ToLower(w)) || try(titleCase(w))
	case 3:
		return try(strings.ToLower(w))
	}
	return false
}

// ---------------------------------------------------------------- общий интерфейс
type Speller struct {
	ru, en *spellDict
	ready  bool
	mu     sync.RWMutex
	user   map[string]bool
	ignore map[string]bool
	userF  string
	gen    int // меняется, когда меняется словарь пользователя
}

var spell = &Speller{user: map[string]bool{}, ignore: map[string]bool{}}

func (s *Speller) Load(cfgDir string) {
	ru := loadSpell(dictData("ru.aff.gz"), dictData("ru.dic.gz"))
	en := loadSpell(dictData("en.aff.gz"), dictData("en.dic.gz"))
	s.mu.Lock()
	s.ru, s.en = ru, en
	if cfgDir != "" {
		s.userF = filepath.Join(cfgDir, "dict.txt")
		if b, err := os.ReadFile(s.userF); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					s.user[strings.ToLower(l)] = true
				}
			}
		}
	}
	s.ready = true
	s.gen++
	s.mu.Unlock()
}

func (s *Speller) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

func scriptOf(w string) string {
	cyr, lat := false, false
	for _, r := range w {
		if unicode.Is(unicode.Cyrillic, r) {
			cyr = true
		} else if r < 128 && unicode.IsLetter(r) {
			lat = true
		} else if unicode.IsLetter(r) {
			return ""
		}
	}
	switch {
	case cyr && !lat:
		return "ru"
	case lat && !cyr:
		return "en"
	}
	return ""
}

// Correct — правильно ли слово (непроверяемые слова считаются правильными).
func (s *Speller) Correct(w string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return true
	}
	if len([]rune(w)) < 2 || s.user[strings.ToLower(w)] || s.ignore[w] {
		return true
	}
	if caseKind(w) == 2 && len([]rune(w)) <= 6 { // аббревиатуры
		return true
	}
	d := s.ru
	switch scriptOf(w) {
	case "ru":
	case "en":
		d = s.en
	default:
		return true
	}
	if d.Check(w) {
		return true
	}
	if strings.Contains(w, "-") {
		for _, part := range strings.Split(w, "-") {
			if len([]rune(part)) > 1 && !d.Check(part) && !s.user[strings.ToLower(part)] {
				return false
			}
		}
		return true
	}
	return false
}

func (s *Speller) AddUser(w string) {
	s.mu.Lock()
	s.user[strings.ToLower(w)] = true
	s.gen++
	f := s.userF
	var all []string
	for k := range s.user {
		all = append(all, k)
	}
	s.mu.Unlock()
	if f != "" {
		sort.Strings(all)
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte(strings.Join(all, "\n")+"\n"), 0o644)
	}
}

func (s *Speller) Ignore(w string) {
	s.mu.Lock()
	s.ignore[w] = true
	s.gen++
	s.mu.Unlock()
}

func (s *Speller) Gen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gen
}

func applyCase(src, w string) string {
	switch caseKind(src) {
	case 1:
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		return string(r)
	case 2:
		return strings.ToUpper(w)
	}
	return w
}

// Suggest — варианты исправления (до max штук).
func (s *Speller) Suggest(w string, max int) []string {
	raw := s.suggest(w, max*3)
	seen := map[string]bool{}
	var out []string
	for _, c := range raw {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	n := len([]rune(w))
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := len([]rune(out[i]))-n, len([]rune(out[j]))-n
		if di < 0 {
			di = -di
		}
		if dj < 0 {
			dj = -dj
		}
		return di < dj
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func (s *Speller) suggest(w string, max int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return nil
	}
	d := s.ru
	if scriptOf(w) == "en" {
		d = s.en
	}
	low := strings.ToLower(w)
	seen := map[string]bool{low: true}
	var out []string
	add := func(c string) bool {
		if seen[c] || c == "" {
			return false
		}
		seen[c] = true
		ok := true
		for _, part := range strings.Fields(c) {
			if !d.Check(part) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, applyCase(w, c))
		}
		return len(out) >= max
	}
	for _, r := range d.rep {
		if i := strings.Index(low, r[0]); i >= 0 {
			if add(low[:i] + r[1] + low[i+len(r[0]):]) {
				return out
			}
		}
	}
	rw := []rune(low)
	n := len(rw)
	cand := func(r []rune) bool { return add(string(r)) }
	// перестановка соседних
	for i := 0; i+1 < n; i++ {
		c := append([]rune{}, rw...)
		c[i], c[i+1] = c[i+1], c[i]
		if cand(c) {
			return out
		}
	}
	// замена одной буквы
	for i := 0; i < n; i++ {
		for _, t := range d.try {
			if t == rw[i] {
				continue
			}
			c := append([]rune{}, rw...)
			c[i] = t
			if cand(c) {
				return out
			}
		}
	}
	// лишняя буква
	for i := 0; i < n; i++ {
		c := append(append([]rune{}, rw[:i]...), rw[i+1:]...)
		if cand(c) {
			return out
		}
	}
	// пропущенная буква
	for i := 0; i <= n; i++ {
		for _, t := range d.try {
			c := append(append(append([]rune{}, rw[:i]...), t), rw[i:]...)
			if cand(c) {
				return out
			}
		}
	}
	// две безударные гласные (типичная ошибка в русском)
	if d == s.ru && len(out) < max {
		swap := map[rune][]rune{'а': {'о'}, 'о': {'а'}, 'е': {'и', 'я'}, 'и': {'е'}, 'я': {'е'}}
		var vi []int
		for i, r := range rw {
			if _, ok := swap[r]; ok {
				vi = append(vi, i)
			}
		}
		for x := 0; x < len(vi); x++ {
			for y := x + 1; y < len(vi); y++ {
				for _, a1 := range swap[rw[vi[x]]] {
					for _, b1 := range swap[rw[vi[y]]] {
						c := append([]rune{}, rw...)
						c[vi[x]], c[vi[y]] = a1, b1
						if cand(c) {
							return out
						}
					}
				}
			}
		}
	}
	// слитно написанные слова
	for i := 3; i < n-2; i++ {
		if add(string(rw[:i]) + " " + string(rw[i:])) {
			return out
		}
	}
	return out
}

// SpellWords — слова абзаца с границами [начало, конец) в смещениях абзаца.
func SpellWords(p *Para) [][2]int {
	t := p.Runes()
	skip := make([]bool, len(t))
	off := 0
	for _, r := range p.Runs {
		L := r.Len()
		if r.T == RText && (r.F.Lang == "-" || (r.F.Link != "" && !strings.HasPrefix(r.F.Link, "#")) || r.F.Del != "") {
			for k := off; k < off+L && k < len(skip); k++ {
				skip[k] = true
			}
		}
		off += L
	}
	var out [][2]int
	i := 0
	for i < len(t) {
		if !unicode.IsLetter(t[i]) {
			i++
			continue
		}
		j := i
		bad := false
		for j < len(t) && (unicode.IsLetter(t[j]) || unicode.IsDigit(t[j]) || ((t[j] == '-' || t[j] == '\'' || t[j] == '’') && j+1 < len(t) && unicode.IsLetter(t[j+1]))) {
			if unicode.IsDigit(t[j]) || skip[j] {
				bad = true
			}
			j++
		}
		// адреса сайтов и почты не проверяем
		if i > 0 && strings.ContainsRune("./@:\\_", t[i-1]) {
			bad = true
		}
		if j+1 < len(t) && (t[j] == '.' || t[j] == '@' || t[j] == '_') && (unicode.IsLetter(t[j+1]) || unicode.IsDigit(t[j+1])) {
			bad = true
		}
		if j+1 < len(t) && t[j] == ':' && t[j+1] == '/' {
			bad = true
		}
		if !bad {
			out = append(out, [2]int{i, j})
		}
		i = j
	}
	return out
}
