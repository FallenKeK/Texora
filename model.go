package main

// Модель документа. Все размеры — в пунктах (pt).

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

const CM = 72 / 2.54

// F — формат символов. Пустые поля означают «как у абзаца/документа».
type F struct {
	B, I, U, S, Sup, Sub, Caps bool
	DU                         bool // двойное подчёркивание
	Color, Bg                  string
	Font                       string
	Size                       float64
	Link                       string
	Ins, Del                   string // id правки (режим исправлений)
	Cmt                        string // id примечания
	Lang                       string // "en" / "ru" / "-" (не проверять)
}

func (f F) Key() string { return fmt.Sprintf("%v", f) }

// Opt — значение, которое может быть «не задано».
type Opt struct {
	V   float64
	Set bool
}

func O(v float64) Opt { return Opt{v, true} }

type List struct {
	ID    string
	Kind  string // ul | ol
	Level int
	Fmt   string // decimal, lowerLetter, ... (из docx)
	Text  string // шаблон вида %1. или символ маркера
}

type PF struct {
	Align         string // left center right justify
	IndL, IndF    Opt
	IndR          float64
	Before, After Opt
	Line          Opt // множитель
	LineExact     float64
	LineRule      string // exact | atLeast
	Heading       int
	Style         string // Title, Subtitle, Quote, NoSpacing
	List          *List
	PB, KeepNext  bool
	CB            bool   // с новой колонки (разрыв колонки перед абзацем)
	RevPara       string // абзац целиком вставлен ("ins") или удалён ("del") исправлением: принят/отклонён — абзац исчезает
	NoWidow       bool   // выключен запрет висячих строк (по умолчанию он включён, как в Word)
	CS            string // «не добавлять интервал между абзацами одного стиля»
	Shade         string
	Tabs          []TabStop
	Bookmark      string   // закладка на абзаце (для оглавления)
	BdrTop        *Border  // линия над абзацем
	BdrBottom     *Border  // линия под абзацем
	Region        string   // "toc" / "bib" — абзац сгенерирован полем
	Sect          *Section // разрыв раздела после абзаца (свойства раздела, который им заканчивается)
}

type TabStop struct {
	Pos    float64
	Align  string // left center right
	Leader string // "" dot
}

const (
	RText  = iota
	RImage // картинка
	RNote  // ссылка на сноску
	RField // поле PAGE / NUMPAGES / PAGEREF x / CITE x
	RBreak // разрыв строки
	REq    // формула, Text — линейная запись
)

type Run struct {
	T    int
	Text []rune
	F    F
	ID   string // картинка или сноска
	W, H float64
	Name string // поле
	Wrap string // картинка: "" в тексте | square | topbottom | behind | front
	Side string // left | right | center
}

func (r *Run) Len() int {
	if r.T == RText {
		return len(r.Text)
	}
	return 1
}

type Para struct {
	PF   PF
	Runs []Run
	Mark F // формат пустого абзаца
	ver  uint64
}

type Cell struct {
	Blocks []Block
	Span   int
	VMerge string // restart | continue
	Shade  string
	B      [4]*Border  // свои границы ячейки: верх, лево, низ, право (nil — как у таблицы)
	VAlign string      // "" (верх) | center | bottom
	Pad    *[4]float64 // свои поля ячейки: верх, лево, низ, право (nil — как у таблицы)
}

// CellPads — поля ячейки (верх, лево, низ, право): свои или общие для таблицы.
func (t *Table) CellPads(c *Cell) (top, left, bottom, right float64) {
	if c != nil && c.Pad != nil {
		return c.Pad[0], c.Pad[1], c.Pad[2], c.Pad[3]
	}
	return t.PadT(), t.PadL(), t.PadB(), t.PadR()
}

type Row struct {
	Cells  []*Cell
	Header bool
	H      float64 // высота строки (пт), 0 — авто
	HRule  string  // "" (не меньше) | exact
}
type Table struct {
	Border bool
	Cols   []float64
	Rows   []*Row
	TB     *TableBorders // границы из Word; nil — все линии по Border
	Pad    *[4]float64   // поля ячеек: верх, лево, низ, право (nil — 0 / 5,4 пт, как в Word)
	Align  string        // "" | center | right
	Ind    float64       // отступ таблицы слева
}

// Border — линия границы. W — толщина в пт; W == 0 — линии нет.
type Border struct {
	W     float64
	Color string
	Style string // single double dashed dotted
}

type TableBorders struct {
	Top, Left, Bottom, Right, InsideH, InsideV *Border
}

func (t *Table) PadT() float64 {
	if t.Pad != nil {
		return t.Pad[0]
	}
	return CellPadV
}
func (t *Table) PadL() float64 {
	if t.Pad != nil {
		return t.Pad[1]
	}
	return CellPad
}
func (t *Table) PadB() float64 {
	if t.Pad != nil {
		return t.Pad[2]
	}
	return CellPadV
}
func (t *Table) PadR() float64 {
	if t.Pad != nil {
		return t.Pad[3]
	}
	return CellPad
}

var thinBorder = &Border{W: 0.5}

// EdgeBorder — граница ячейки (ri, ci) с края side (0 верх, 1 лево, 2 низ, 3 право); nil — нет линии.
func (t *Table) EdgeBorder(ri, ci, nc int, c *Cell, side int) *Border {
	if b := c.B[side]; b != nil {
		if b.W <= 0 {
			return nil
		}
		return b
	}
	if t.TB == nil {
		if t.Border {
			return thinBorder
		}
		return nil
	}
	var b *Border
	switch side {
	case 0:
		b = t.TB.InsideH
		if ri == 0 {
			b = t.TB.Top
		}
	case 2:
		b = t.TB.InsideH
		if ri == len(t.Rows)-1 {
			b = t.TB.Bottom
		}
	case 1:
		b = t.TB.InsideV
		if ci == 0 {
			b = t.TB.Left
		}
	case 3:
		b = t.TB.InsideV
		if ci == nc-1 {
			b = t.TB.Right
		}
	}
	if b == nil || b.W <= 0 {
		return nil
	}
	return b
}

// Block — абзац или таблица.
type Block struct {
	P *Para
	T *Table
}

type Page struct {
	W, H, Top, Bottom, Left, Right, Header, Footer float64
}

type Defaults struct {
	Font  string
	Size  float64
	Line  float64
	After float64
}

type Media struct {
	Mime string
	Data []byte
	img  any // декодированная картинка (кэш отрисовки)
}

// Note — текст сноски.
type Note struct{ Blocks []Block }

// HF — набор колонтитулов раздела. Пустой — «как в предыдущем разделе».
type HF struct {
	Header, Footer, HeaderFirst, FooterFirst, HeaderEven, FooterEven []Block
}

var HFKinds = []string{"header", "footer", "header.first", "footer.first", "header.even", "footer.even"}

func (h *HF) Get(kind string) *[]Block {
	switch kind {
	case "header":
		return &h.Header
	case "footer":
		return &h.Footer
	case "header.first":
		return &h.HeaderFirst
	case "footer.first":
		return &h.FooterFirst
	case "header.even":
		return &h.HeaderEven
	case "footer.even":
		return &h.FooterEven
	}
	return nil
}

func (h HF) Clone() HF {
	return HF{CloneBlocks(h.Header), CloneBlocks(h.Footer), CloneBlocks(h.HeaderFirst), CloneBlocks(h.FooterFirst),
		CloneBlocks(h.HeaderEven), CloneBlocks(h.FooterEven)}
}

// Section — свойства раздела документа (кроме последнего — он хранится в самом Doc).
// Columns — колонки текста раздела (как w:cols в Word). N ≤ 1 — одна колонка.
type Columns struct {
	N   int
	Gap float64 // промежуток между колонками, пт (0 — 1,25 см, как в Word)
	Sep bool    // линия между колонками
}

// ColGap — промежуток между колонками.
func (c Columns) ColGap() float64 {
	if c.Gap > 0 {
		return c.Gap
	}
	return 1.25 * CM
}

// Count — число колонок (не меньше одной).
func (c Columns) Count() int { return max(1, min(c.N, 12)) }

// Width — ширина одной колонки при ширине текста W.
func (c Columns) Width(W float64) float64 {
	n := c.Count()
	if n == 1 {
		return W
	}
	return math.Max(36, (W-c.ColGap()*float64(n-1))/float64(n))
}

type Section struct {
	ID      string
	Page    Page
	Cols    Columns
	TitlePg bool
	Start   int    // начать нумерацию страниц с (0 — продолжить)
	Type    string // "" (со следующей страницы) | continuous | oddPage | evenPage
	HF
}

func (s *Section) Clone() *Section {
	if s == nil {
		return nil
	}
	c := *s
	c.HF = s.HF.Clone()
	return &c
}

type Comment struct {
	Author, Initials, Date string
	Text                   string
	Done                   bool
}

type Rev struct{ Author, Date string }

// Source — источник для списка литературы (ГОСТ Р 7.0.100-2018).
type Source struct {
	ID        string
	Type      string // book | article | web | thesis | law
	Authors   string // «Иванов И. И., Петров П. П.»
	Title     string
	Extra     string // сведения, относящиеся к заглавию: «учебник для вузов»
	City      string
	Publisher string
	Year      string
	Pages     string // число страниц книги или диапазон статьи
	Journal   string // для статьи
	Issue     string // № выпуска
	URL       string
	Accessed  string // дата обращения
	Number    string `json:",omitempty"` // номер патента или обозначение стандарта
}

type Doc struct {
	Page    Page
	Cols    Columns // колонки последнего раздела
	TitlePg bool
	Start   int
	Type    string // как начинается последний раздел
	Def     Defaults
	Body    []Block
	HF
	EvenOdd   bool // разные колонтитулы для чётных и нечётных
	Footnotes map[string]*Note
	Media     map[string]*Media
	Styles    map[string]headingStyle // переопределённые стили: h1..h6, Title, Subtitle, Quote, NoSpacing, TOC1..3
	Comments  map[string]*Comment
	Revs      map[string]*Rev
	Track     bool
	Sources   []*Source
	Hyphen    bool // автоматическая расстановка переносов
	Lang      string

	refPages  map[string]string // номер страницы закладок по последней вёрстке
	citeNums  map[string]int
	seqNums   map[string]string // id поля SEQ → номер
	seqRefs   map[string]seqRef // закладка названия → категория и номер
	activeCmt string            // подсвеченное примечание (на экране)
}

var DefaultPage = Page{W: 595.3, H: 841.9, Top: 2 * CM, Bottom: 2 * CM, Left: 3 * CM, Right: 1.5 * CM, Header: 1.25 * CM, Footer: 1.25 * CM}

type headingStyle struct {
	Size          float64
	B, I          bool
	Before, After float64
	Color         string
	Font          string
	Align         string
	Caps          bool
	IndF, IndL    float64
	Line          float64 // 0 — как в документе
	Set           bool    // есть в стиле явный отступ первой строки
}

var Headings = map[int]headingStyle{
	1: {Size: 16, B: true, Before: 12, After: 6, Color: "#1f3864"},
	2: {Size: 13, B: true, Before: 10, After: 4, Color: "#2f5496"},
	3: {Size: 12, B: true, Before: 8, After: 4, Color: "#1f3864"},
	4: {Size: 12, B: true, I: true, Before: 6, After: 2, Color: "#2f5496"},
	5: {Size: 12, Before: 6, After: 2, Color: "#2f5496"},
	6: {Size: 12, I: true, Before: 6, After: 2, Color: "#1f3864"},
}

// Именованные стили абзаца (как в коллекции стилей Word).
var NamedStyles = map[string]headingStyle{
	"Title":      {Size: 26, Before: 0, After: 6},
	"Subtitle":   {Size: 14, Before: 0, After: 8, Color: "#595959"},
	"Quote":      {Size: 0, I: true, Before: 8, After: 8, Color: "#404040"},
	"NoSpacing":  {Before: 0, After: 0},
	"TOC1":       {Before: 0, After: 3},
	"TOC2":       {Before: 0, After: 3, IndL: 11},
	"TOC3":       {Before: 0, After: 3, IndL: 22},
	"TOCHeading": {Size: 16, Before: 12, After: 12, Color: "#1f3864", Align: "center"},
}

// StyleKey — ключ стиля абзаца в Doc.Styles.
func StyleKey(p *Para) string {
	if p.PF.Heading > 0 {
		return "h" + strconv.Itoa(p.PF.Heading)
	}
	return p.PF.Style
}

func (d *Doc) StyleDef(key string) (headingStyle, bool) {
	if s, ok := d.Styles[key]; ok {
		return s, true
	}
	if strings.HasPrefix(key, "h") && len(key) == 2 {
		s, ok := Headings[int(key[1]-'0')]
		return s, ok
	}
	s, ok := NamedStyles[key]
	return s, ok
}

var idCounter int64

func NewID(prefix string) string {
	n := atomic.AddInt64(&idCounter, 1)
	return fmt.Sprintf("%s%x%x", prefix, time.Now().UnixNano()&0xffffff, n)
}

func NewPara() *Para { return &Para{} }

func NewDoc() *Doc {
	return &Doc{Page: DefaultPage, Def: Defaults{Font: "Calibri", Size: 11, Line: 1.08, After: 8},
		Body: []Block{{P: NewPara()}}, Footnotes: map[string]*Note{}, Media: map[string]*Media{},
		Styles: map[string]headingStyle{}, Comments: map[string]*Comment{}, Revs: map[string]*Rev{}}
}

// Clone — полная копия документа (для отмены). Картинки общие: они не меняются.
func (d *Doc) Clone() *Doc {
	c := *d
	c.Body = CloneBlocks(d.Body)
	c.HF = d.HF.Clone()
	c.Footnotes = map[string]*Note{}
	for k, n := range d.Footnotes {
		c.Footnotes[k] = &Note{Blocks: CloneBlocks(n.Blocks)}
	}
	c.Media = map[string]*Media{}
	for k, m := range d.Media {
		c.Media[k] = m
	}
	c.Styles = map[string]headingStyle{}
	for k, v := range d.Styles {
		c.Styles[k] = v
	}
	c.Comments = map[string]*Comment{}
	for k, v := range d.Comments {
		cc := *v
		c.Comments[k] = &cc
	}
	c.Revs = map[string]*Rev{}
	for k, v := range d.Revs {
		c.Revs[k] = v
	}
	c.Sources = nil
	for _, s := range d.Sources {
		ss := *s
		c.Sources = append(c.Sources, &ss)
	}
	return &c
}

func (d *Doc) ensure() {
	if d.Footnotes == nil {
		d.Footnotes = map[string]*Note{}
	}
	if d.Media == nil {
		d.Media = map[string]*Media{}
	}
	if d.Styles == nil {
		d.Styles = map[string]headingStyle{}
	}
	if d.Comments == nil {
		d.Comments = map[string]*Comment{}
	}
	if d.Revs == nil {
		d.Revs = map[string]*Rev{}
	}
}

// SecView — раздел в порядке следования; Blocks — индексы верхнего уровня в Body [From, To).
type SecView struct {
	Index    int
	Page     Page
	Cols     Columns
	TitlePg  bool
	Start    int
	Type     string
	HF       *HF
	Prefix   string // префикс историй колонтитулов: "" для последнего, "s<ID>/" для остальных
	From, To int
	Owner    *Section // nil — последний раздел (свойства в Doc)
}

func (d *Doc) Sections() []SecView {
	var out []SecView
	from := 0
	for i, b := range d.Body {
		if b.P != nil && b.P.PF.Sect != nil {
			s := b.P.PF.Sect
			if s.ID == "" {
				s.ID = NewID("s")
			}
			out = append(out, SecView{Index: len(out), Page: s.Page, Cols: s.Cols, TitlePg: s.TitlePg, Start: s.Start, Type: s.Type, HF: &s.HF,
				Prefix: "s" + s.ID + "/", From: from, To: i + 1, Owner: s})
			from = i + 1
		}
	}
	out = append(out, SecView{Index: len(out), Page: d.Page, Cols: d.Cols, TitlePg: d.TitlePg, Start: d.Start, Type: d.Type, HF: &d.HF, From: from, To: len(d.Body)})
	return out
}

// SectionAt — раздел, в который входит блок верхнего уровня с индексом i.
func (d *Doc) SectionAt(i int) SecView {
	ss := d.Sections()
	for _, s := range ss {
		if i < s.To {
			return s
		}
	}
	return ss[len(ss)-1]
}

// ResolveHF — какую историю колонтитула показывать в разделе si: kind = header|footer, variant = ""|first|even.
func (d *Doc) ResolveHF(ss []SecView, si int, kind, variant string) (string, []Block) {
	key := kind
	if variant != "" {
		key += "." + variant
	}
	for i := si; i >= 0; i-- {
		if bs := *ss[i].HF.Get(key); len(bs) > 0 {
			return ss[i].Prefix + key, bs
		}
	}
	return ss[si].Prefix + key, nil
}

func TextRun(s string, f F) Run { return Run{T: RText, Text: []rune(s), F: f} }

func (p *Para) Len() int {
	n := 0
	for i := range p.Runs {
		n += p.Runs[i].Len()
	}
	return n
}

func (p *Para) Touch() { p.ver++ }

func (p *Para) Normalize() {
	out := p.Runs[:0:0]
	for _, r := range p.Runs {
		if r.T == RText {
			if len(r.Text) == 0 {
				continue
			}
			if n := len(out); n > 0 && out[n-1].T == RText && out[n-1].F == r.F {
				out[n-1].Text = append(append([]rune{}, out[n-1].Text...), r.Text...)
				continue
			}
		}
		out = append(out, r)
	}
	p.Runs = out
	p.Touch()
}

// SplitAt разрезает текстовый ран так, чтобы ровно off символов было до возвращённого индекса.
func (p *Para) SplitAt(off int) int {
	pos := 0
	for i := 0; i < len(p.Runs); i++ {
		if pos == off {
			return i
		}
		r := p.Runs[i]
		L := r.Len()
		if off < pos+L {
			k := off - pos
			a := Run{T: RText, Text: append([]rune{}, r.Text[:k]...), F: r.F}
			b := Run{T: RText, Text: append([]rune{}, r.Text[k:]...), F: r.F}
			p.Runs = append(p.Runs[:i], append([]Run{a, b}, p.Runs[i+1:]...)...)
			return i + 1
		}
		pos += L
	}
	return len(p.Runs)
}

// FmtAt — формат символа слева от позиции (для продолжения ввода).
func (p *Para) FmtAt(off int) F {
	if len(p.Runs) == 0 {
		return p.Mark
	}
	pos, idx := 0, 0
	for i := range p.Runs {
		L := p.Runs[i].Len()
		if off > pos && off <= pos+L {
			idx = i
			break
		}
		pos += L
		idx = i
	}
	if off == 0 {
		idx = 0
	}
	for i := idx; i >= 0; i-- {
		if p.Runs[i].T == RText {
			return p.Runs[i].F
		}
	}
	for i := idx; i < len(p.Runs); i++ {
		if p.Runs[i].T == RText {
			return p.Runs[i].F
		}
	}
	return p.Mark
}

func (p *Para) Insert(off int, runs ...Run) int {
	i := p.SplitAt(off)
	p.Runs = append(p.Runs[:i], append(append([]Run{}, runs...), p.Runs[i:]...)...)
	p.Normalize()
	n := 0
	for i := range runs {
		n += runs[i].Len()
	}
	return off + n
}

func (p *Para) Delete(a, b int) []Run {
	if b <= a {
		return nil
	}
	keep := p.FmtAt(a + 1)
	i := p.SplitAt(a)
	j := p.SplitAt(b)
	removed := append([]Run{}, p.Runs[i:j]...)
	p.Runs = append(p.Runs[:i], p.Runs[j:]...)
	p.Normalize()
	if len(p.Runs) == 0 {
		p.Mark = keep
	}
	return removed
}

func (p *Para) Slice(a, b int) []Run {
	q := &Para{Runs: cloneRuns(p.Runs)}
	i := q.SplitAt(a)
	j := q.SplitAt(b)
	return q.Runs[i:j]
}

func (p *Para) Format(a, b int, fn func(F) F) {
	if a == b {
		if len(p.Runs) == 0 {
			p.Mark = fn(p.Mark)
			p.Touch()
		}
		return
	}
	i := p.SplitAt(a)
	j := p.SplitAt(b)
	for k := i; k < j; k++ {
		if p.Runs[k].T == RText || p.Runs[k].T == RField {
			p.Runs[k].F = fn(p.Runs[k].F)
		}
	}
	p.Normalize()
}

// Text — текст абзаца, объекты заменены символом U+FFFC (смещения совпадают).
func (p *Para) Text() string {
	var sb strings.Builder
	for _, r := range p.Runs {
		if r.T == RText {
			sb.WriteString(string(r.Text))
		} else {
			sb.WriteRune('￼')
		}
	}
	return sb.String()
}

func (p *Para) Runes() []rune { return []rune(p.Text()) }

// ---------------------------------------------------------------- копирование (для отмены)
func cloneRuns(rs []Run) []Run {
	out := make([]Run, len(rs))
	for i, r := range rs {
		out[i] = r
		if r.Text != nil {
			out[i].Text = append([]rune{}, r.Text...)
		}
	}
	return out
}

func (p *Para) Clone() *Para {
	q := *p
	q.Runs = cloneRuns(p.Runs)
	if p.PF.List != nil {
		l := *p.PF.List
		q.PF.List = &l
	}
	q.PF.Tabs = append([]TabStop(nil), p.PF.Tabs...)
	q.PF.Sect = p.PF.Sect.Clone()
	q.ver = 0
	return &q
}

func CloneBlocks(bs []Block) []Block {
	if bs == nil {
		return nil
	}
	out := make([]Block, len(bs))
	for i, b := range bs {
		if b.P != nil {
			out[i].P = b.P.Clone()
		} else if b.T != nil {
			t := &Table{Border: b.T.Border, Cols: append([]float64{}, b.T.Cols...), TB: b.T.TB, Pad: b.T.Pad, Align: b.T.Align, Ind: b.T.Ind}
			for _, r := range b.T.Rows {
				nr := &Row{Header: r.Header, H: r.H, HRule: r.HRule}
				for _, c := range r.Cells {
					nr.Cells = append(nr.Cells, &Cell{Blocks: CloneBlocks(c.Blocks), Span: c.Span, VMerge: c.VMerge, Shade: c.Shade, B: c.B, VAlign: c.VAlign, Pad: c.Pad})
				}
				t.Rows = append(t.Rows, nr)
			}
			out[i].T = t
		}
	}
	return out
}

// ---------------------------------------------------------------- плоский список абзацев
type CellCtx struct {
	Table *Table
	Arr   *[]Block // где лежит таблица
	Row   int
	Col   int
	Cell  *Cell
}

type Entry struct {
	P    *Para
	Arr  *[]Block
	Cell *CellCtx
}

func Flatten(arr *[]Block, out []Entry, cell *CellCtx) []Entry {
	for _, b := range *arr {
		if b.P != nil {
			out = append(out, Entry{P: b.P, Arr: arr, Cell: cell})
		} else if b.T != nil {
			for ri, r := range b.T.Rows {
				for ci, c := range r.Cells {
					out = Flatten(&c.Blocks, out, &CellCtx{Table: b.T, Arr: arr, Row: ri, Col: ci, Cell: c})
				}
			}
		}
	}
	return out
}

func IndexOfPara(arr []Block, p *Para) int {
	for i, b := range arr {
		if b.P == p {
			return i
		}
	}
	return -1
}

func IndexOfTable(arr []Block, t *Table) int {
	for i, b := range arr {
		if b.T == t {
			return i
		}
	}
	return -1
}

func InsertBlocks(arr *[]Block, at int, bs ...Block) {
	*arr = append((*arr)[:at], append(append([]Block{}, bs...), (*arr)[at:]...)...)
}

func RemoveBlocks(arr *[]Block, at, n int) {
	*arr = append((*arr)[:at], (*arr)[at+n:]...)
}

func (d *Doc) Story(name string) *[]Block {
	switch {
	case name == "body":
		return &d.Body
	case strings.HasPrefix(name, "fn:"):
		if n, ok := d.Footnotes[name[3:]]; ok {
			return &n.Blocks
		}
	case strings.HasPrefix(name, "s") && strings.Contains(name, "/"):
		id, kind, _ := strings.Cut(name[1:], "/")
		for _, b := range d.Body {
			if b.P != nil && b.P.PF.Sect != nil && b.P.PF.Sect.ID == id {
				return b.P.PF.Sect.HF.Get(kind)
			}
		}
	default:
		return d.HF.Get(name)
	}
	return nil
}

// IsHF — история колонтитула.
func IsHF(story string) bool {
	_, k, ok := strings.Cut(story, "/")
	if !ok {
		k = story
	}
	return strings.HasPrefix(k, "header") || strings.HasPrefix(k, "footer")
}

func (d *Doc) FootnoteOrder() []string {
	var order []string
	seen := map[string]bool{}
	for _, e := range Flatten(&d.Body, nil, nil) {
		for _, r := range e.P.Runs {
			if r.T == RNote && !seen[r.ID] {
				if _, ok := d.Footnotes[r.ID]; ok {
					seen[r.ID] = true
					order = append(order, r.ID)
				}
			}
		}
	}
	return order
}

func NewTable(rows, cols int, width float64) *Table {
	t := &Table{Border: true}
	for i := 0; i < cols; i++ {
		t.Cols = append(t.Cols, width/float64(cols))
	}
	for i := 0; i < rows; i++ {
		r := &Row{}
		for j := 0; j < cols; j++ {
			r.Cells = append(r.Cells, &Cell{Blocks: []Block{{P: NewPara()}}, Span: 1})
		}
		t.Rows = append(t.Rows, r)
	}
	return t
}

func PlainToBlocks(text string) []Block {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var out []Block
	for _, line := range strings.Split(text, "\n") {
		p := NewPara()
		if line != "" {
			p.Runs = []Run{TextRun(line, F{})}
		}
		out = append(out, Block{P: p})
	}
	return out
}

func BlocksToText(bs []Block) string {
	var lines []string
	for _, b := range bs {
		if b.P != nil {
			var sb strings.Builder
			for _, r := range b.P.Runs {
				switch r.T {
				case RText:
					sb.WriteString(string(r.Text))
				case RBreak:
					sb.WriteString("\n")
				}
			}
			lines = append(lines, sb.String())
		} else if b.T != nil {
			for _, r := range b.T.Rows {
				var cells []string
				for _, c := range r.Cells {
					cells = append(cells, strings.ReplaceAll(BlocksToText(c.Blocks), "\n", " "))
				}
				lines = append(lines, strings.Join(cells, "\t"))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func IsWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func sortedKeys[M ~map[string]V, V any](m M) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
