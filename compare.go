package main

// Сравнение двух версий документа (как «Сравнить» в Word): результат — новая версия, в которой
// отличия от старой отмечены исправлениями: вставки (F.Ins) и удаления (F.Del) с автором «Сравнение».
// Их можно листать, принимать и отклонять обычными командами рецензирования.
//
// Как: абзацы верхнего уровня выравниваются по совпадению текста (наибольшая общая подпоследовательность),
// между совпавшими похожие абзацы сравниваются по словам, непохожие считаются удалёнными и вставленными.
// Таблицы одинаковой формы сравниваются по ячейкам; изменения оформления без изменения текста не показываются.

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// CompareStats — сколько найдено отличий.
type CompareStats struct {
	InsWords, DelWords int
	InsParas, DelParas int
}

// CompareDocs строит документ «новая версия с отмеченными отличиями от старой».
func CompareDocs(oldD, newD *Doc, author string) (*Doc, CompareStats) {
	// исправления, которые уже были в документах, считаются принятыми (как в Word)
	oldD = oldD.Clone()
	acceptRevisions(oldD)
	out := newD.Clone()
	acceptRevisions(out)
	out.ensure()
	rev := NewID("r")
	out.Revs[rev] = &Rev{Author: author, Date: nowISO()}
	c := &comparer{old: oldD, out: out, rev: rev, media: map[string]string{}}
	out.Body = c.blocks(oldD.Body, out.Body)
	return out, c.st
}

type comparer struct {
	old, out *Doc
	rev      string
	media    map[string]string // id картинки в старом → id в результате
	st       CompareStats
}

func blockKey(b Block) string {
	if b.P != nil {
		return "p:" + paraKey(b.P)
	}
	if b.T != nil {
		var sb strings.Builder
		sb.WriteString("t:")
		for _, r := range b.T.Rows {
			for _, c := range r.Cells {
				sb.WriteString(strings.TrimSpace(BlocksToText(c.Blocks)))
				sb.WriteString("\x1f")
			}
			sb.WriteString("\x1e")
		}
		return sb.String()
	}
	return ""
}

// paraKey — текст абзаца со всеми объектами (картинка, формула, поле, сноска считаются своими знаками).
func paraKey(p *Para) string {
	var sb strings.Builder
	for _, r := range p.Runs {
		switch r.T {
		case RText:
			sb.WriteString(string(r.Text))
		default:
			sb.WriteString("\x00" + atomKey(r) + "\x00")
		}
	}
	return sb.String()
}

func atomKey(r Run) string {
	switch r.T {
	case REq:
		return "eq:" + string(r.Text)
	case RField:
		return "f:" + r.Name
	case RBreak:
		return "br"
	case RNote:
		return "fn"
	case RImage:
		return "img:" + r.ID
	}
	return "?"
}

// lcsPairs — пары совпавших индексов (наибольшая общая подпоследовательность).
func lcsPairs(a, b []string) [][2]int {
	n, m := len(a), len(b)
	// общие начало и конец — без таблицы
	pre := 0
	for pre < n && pre < m && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && a[n-1-suf] == b[m-1-suf] {
		suf++
	}
	var out [][2]int
	for i := 0; i < pre; i++ {
		out = append(out, [2]int{i, i})
	}
	a2, b2 := a[pre:n-suf], b[pre:m-suf]
	if len(a2) > 0 && len(b2) > 0 {
		if len(a2)*len(b2) > 25_000_000 {
			// слишком большой кусок: без выравнивания (всё старое удалено, всё новое вставлено)
		} else {
			na, nb := len(a2), len(b2)
			dp := make([][]int32, na+1)
			for i := range dp {
				dp[i] = make([]int32, nb+1)
			}
			for i := na - 1; i >= 0; i-- {
				for j := nb - 1; j >= 0; j-- {
					if a2[i] == b2[j] {
						dp[i][j] = dp[i+1][j+1] + 1
					} else if dp[i+1][j] >= dp[i][j+1] {
						dp[i][j] = dp[i+1][j]
					} else {
						dp[i][j] = dp[i][j+1]
					}
				}
			}
			for i, j := 0, 0; i < na && j < nb; {
				switch {
				case a2[i] == b2[j]:
					out = append(out, [2]int{pre + i, pre + j})
					i++
					j++
				case dp[i+1][j] >= dp[i][j+1]:
					i++
				default:
					j++
				}
			}
		}
	}
	for k := 0; k < suf; k++ {
		out = append(out, [2]int{n - suf + k, m - suf + k})
	}
	return out
}

// blocks сравнивает два списка блоков и возвращает новый с отметками.
func (c *comparer) blocks(oldB, newB []Block) []Block {
	ka := make([]string, len(oldB))
	kb := make([]string, len(newB))
	for i, b := range oldB {
		ka[i] = blockKey(b)
	}
	for i, b := range newB {
		kb[i] = blockKey(b)
	}
	pairs := lcsPairs(ka, kb)
	var out []Block
	i, j := 0, 0
	flush := func(i1, j1 int) {
		c.gap(oldB[i:i1], newB[j:j1], &out)
	}
	for _, pr := range pairs {
		flush(pr[0], pr[1])
		out = append(out, newB[pr[1]])
		i, j = pr[0]+1, pr[1]+1
	}
	flush(len(oldB), len(newB))
	return out
}

// gap — несовпавшие отрезки: похожие абзацы сравниваем по словам, остальное — удалено и вставлено.
func (c *comparer) gap(oldB, newB []Block, out *[]Block) {
	used := make([]bool, len(oldB))
	oi := 0
	for _, nb := range newB {
		// ищем дальше по старым похожий блок того же вида
		match := -1
		for k := oi; k < len(oldB); k++ {
			if !used[k] && c.similar(oldB[k], nb) {
				match = k
				break
			}
		}
		if match >= 0 {
			for k := oi; k < match; k++ { // всё старое до него — удалено
				c.emitDeleted(oldB[k], out)
				used[k] = true
			}
			used[match] = true
			oi = match + 1
			*out = append(*out, c.pair(oldB[match], nb))
			continue
		}
		c.emitInserted(nb, out)
	}
	for k := oi; k < len(oldB); k++ {
		if !used[k] {
			c.emitDeleted(oldB[k], out)
		}
	}
}

func (c *comparer) similar(a, b Block) bool {
	if a.P != nil && b.P != nil {
		wa, wb := words(paraKey(a.P)), words(paraKey(b.P))
		if len(wa) == 0 || len(wb) == 0 {
			return len(wa) == len(wb)
		}
		common := len(lcsPairs(wa, wb))
		return float64(common) >= 0.4*float64(max(len(wa), len(wb)))
	}
	if a.T != nil && b.T != nil {
		return sameShape(a.T, b.T)
	}
	return false
}

func sameShape(a, b *Table) bool {
	if len(a.Rows) != len(b.Rows) {
		return false
	}
	for i := range a.Rows {
		if len(a.Rows[i].Cells) != len(b.Rows[i].Cells) {
			return false
		}
	}
	return true
}

func words(s string) []string {
	var out []string
	for _, t := range tokenizeDiff([]rune(s)) {
		if strings.TrimSpace(t) != "" {
			out = append(out, t)
		}
	}
	return out
}

// pair — два похожих блока: абзацы по словам, таблицы по ячейкам.
func (c *comparer) pair(a, b Block) Block {
	if a.P != nil && b.P != nil {
		return Block{P: c.diffPara(a.P, b.P)}
	}
	t := cloneTable(b.T)
	for ri, row := range t.Rows {
		for ci, cell := range row.Cells {
			cell.Blocks = c.blocks(a.T.Rows[ri].Cells[ci].Blocks, cell.Blocks)
		}
	}
	return Block{T: t}
}

func cloneTable(t *Table) *Table {
	bs := CloneBlocks([]Block{{T: t}})
	return bs[0].T
}

// atom — один знак абзаца с оформлением: буква или объект.
type atom struct {
	r   Run
	key string
}

func atomsOf(p *Para) []atom {
	var out []atom
	for _, r := range p.Runs {
		if r.T == RText {
			for _, ch := range r.Text {
				x := r
				x.Text = []rune{ch}
				out = append(out, atom{x, string(ch)})
			}
			continue
		}
		out = append(out, atom{r, "\x00" + atomKey(r) + "\x00"})
	}
	return out
}

// tokenizeDiff — слова, пробелы и знаки по отдельности (сравнение идёт по таким кусочкам).
func tokenizeDiff(rs []rune) []string {
	var out []string
	cls := func(r rune) int {
		switch {
		case r == 0:
			return 3
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return 1
		case unicode.IsSpace(r):
			return 2
		}
		return 0
	}
	for i := 0; i < len(rs); {
		j := i + 1
		if rs[i] == 0 { // объект: от \x00 до \x00
			for j < len(rs) && rs[j] != 0 {
				j++
			}
			j = min(j+1, len(rs))
		} else if k := cls(rs[i]); k == 1 || k == 2 {
			for j < len(rs) && cls(rs[j]) == k {
				j++
			}
		}
		out = append(out, string(rs[i:j]))
		i = j
	}
	return out
}

// groupAtoms — атомы, сгруппированные в те же кусочки, что tokenizeDiff.
func groupAtoms(as []atom) (keys []string, groups [][]atom) {
	var rs []rune
	var owner []int
	for i, a := range as {
		for range []rune(a.key) {
			owner = append(owner, i)
		}
		rs = append(rs, []rune(a.key)...)
	}
	pos := 0
	for _, t := range tokenizeDiff(rs) {
		n := len([]rune(t))
		first, last := owner[pos], owner[pos+n-1]
		keys = append(keys, t)
		groups = append(groups, as[first:last+1])
		pos += n
	}
	return
}

func (c *comparer) diffPara(op, np *Para) *Para {
	out := np.Clone()
	ka, ga := groupAtoms(atomsOf(op))
	kb, gb := groupAtoms(atomsOf(np))
	pairs := lcsPairs(ka, kb)
	var runs []Run
	add := func(g []atom, mark string, old bool) {
		for _, a := range g {
			r := a.r
			if old {
				r = c.fromOld(r)
			}
			switch mark {
			case "ins":
				r.F.Ins = c.rev
			case "del":
				r.F.Del = c.rev
			}
			runs = append(runs, r)
		}
		if mark != "" && isWordToken(keyOf(g)) {
			if mark == "ins" {
				c.st.InsWords++
			} else {
				c.st.DelWords++
			}
		}
	}
	i, j := 0, 0
	for _, pr := range append(pairs, [2]int{len(ka), len(kb)}) {
		for ; i < pr[0]; i++ {
			add(ga[i], "del", true)
		}
		for ; j < pr[1]; j++ {
			add(gb[j], "ins", false)
		}
		if pr[0] < len(ka) {
			add(gb[pr[1]], "", false)
			i, j = pr[0]+1, pr[1]+1
		}
	}
	out.Runs = runs
	out.Normalize()
	return out
}

func keyOf(g []atom) string {
	var sb strings.Builder
	for _, a := range g {
		sb.WriteString(a.key)
	}
	return sb.String()
}

// fromOld — объект из старого документа переносится в результат (картинка — вместе с данными).
func (c *comparer) fromOld(r Run) Run {
	if r.T == RImage {
		if id, ok := c.media[r.ID]; ok {
			r.ID = id
		} else if m := c.old.Media[r.ID]; m != nil {
			// та же картинка уже есть в новой версии — берём её
			h := sha256.Sum256(m.Data)
			for nid, nm := range c.out.Media {
				if sha256.Sum256(nm.Data) == h {
					c.media[r.ID] = nid
					r.ID = nid
					return r
				}
			}
			nid := NewID("m")
			c.out.Media[nid] = &Media{Mime: m.Mime, Data: m.Data}
			c.media[r.ID] = nid
			r.ID = nid
		}
	}
	if r.T == RNote {
		// сноска из удалённого текста: копия, чтобы не ссылаться на чужую
		if n := c.old.Footnotes[r.ID]; n != nil {
			nid := NewID("fn")
			c.out.Footnotes[nid] = &Note{Blocks: CloneBlocks(n.Blocks)}
			r.ID = nid
		}
	}
	r.F.Cmt = ""
	return r
}

func (c *comparer) markAll(p *Para, mark string, old bool) *Para {
	q := p.Clone()
	if old {
		q.PF.Sect = nil // разделы старой версии не переносим
		q.PF.Bookmark = ""
	}
	for i := range q.Runs {
		if old {
			q.Runs[i] = c.fromOld(q.Runs[i])
		}
		if mark == "ins" {
			q.Runs[i].F.Ins = c.rev
		} else {
			q.Runs[i].F.Del = c.rev
		}
	}
	n := 0
	for _, w := range words(paraKey(p)) {
		if isWordToken(w) {
			n++
		}
	}
	if n > 0 {
		if mark == "ins" {
			c.st.InsParas++
			c.st.InsWords += n
		} else {
			c.st.DelParas++
			c.st.DelWords += n
		}
	}
	q.PF.RevPara = mark
	return q
}

func (c *comparer) emitDeleted(b Block, out *[]Block) {
	if b.P != nil {
		*out = append(*out, Block{P: c.markAll(b.P, "del", true)})
		return
	}
	// удалённая таблица: её текст отмечается удалённым внутри ячеек
	t := cloneTable(b.T)
	for _, row := range t.Rows {
		for _, cell := range row.Cells {
			for k, cb := range cell.Blocks {
				if cb.P != nil {
					cell.Blocks[k].P = c.markAll(cb.P, "del", true)
				}
			}
		}
	}
	*out = append(*out, Block{T: t})
}

func (c *comparer) emitInserted(b Block, out *[]Block) {
	if b.P != nil {
		*out = append(*out, Block{P: c.markAll(b.P, "ins", false)})
		return
	}
	t := cloneTable(b.T)
	for _, row := range t.Rows {
		for _, cell := range row.Cells {
			for k, cb := range cell.Blocks {
				if cb.P != nil {
					cell.Blocks[k].P = c.markAll(cb.P, "ins", false)
				}
			}
		}
	}
	*out = append(*out, Block{T: t})
}

// ---------------------------------------------------------------- в приложении

// CompareWithFile — текущий документ считается новой версией, выбранный файл — старой.
// Результат открывается в новой вкладке с отличиями в виде исправлений.
func (a *App) CompareWithFile(p string) {
	if a.pdf != nil {
		a.toast(a.tr("Сравнивать можно документы, а не PDF"))
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		a.errorBox(a.tr("Не удалось открыть файл:") + "\n" + err.Error())
		return
	}
	oldD, err := OpenBytes(filepath.Base(p), data)
	if err != nil {
		a.errorBox(a.trf("Не удалось открыть «%s»:", filepath.Base(p)) + "\n" + err.Error())
		return
	}
	a.stash()
	newName := a.FileName
	res, st := CompareDocs(oldD, a.E.Doc, a.tr("Сравнение"))
	name := a.tr("Сравнение") + " — " + strings.TrimSuffix(newName, filepath.Ext(newName)) + ".docx"
	a.newTab(res, name, "")
	a.backstage = false
	a.Set.HideMarkup = false
	a.E.Dirty = true
	a.tab = "review"
	if st.InsWords+st.DelWords == 0 {
		a.toast(a.tr("Отличий в тексте нет"))
		return
	}
	a.E.NextRevision(1)
	a.toast(a.trf("Вставлено слов: %d, удалено: %d. Листайте правки и принимайте или отклоняйте их", st.InsWords, st.DelWords))
	a.after()
}

// isWordToken — кусок текста считается словом, если в нём есть буква или цифра (объекты тоже считаются).
func isWordToken(s string) bool {
	for _, r := range s {
		if r == 0 || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// acceptRevisions принимает все исправления документа: удалённое убирается, вставленное остаётся.
func acceptRevisions(d *Doc) {
	var fix func(arr *[]Block)
	fix = func(arr *[]Block) {
		var keep []Block
		for _, b := range *arr {
			if b.T != nil {
				for _, r := range b.T.Rows {
					for _, c := range r.Cells {
						fix(&c.Blocks)
					}
				}
				keep = append(keep, b)
				continue
			}
			p := b.P
			gone := p.PF.RevPara == "del"
			p.PF.RevPara = ""
			var runs []Run
			for _, r := range p.Runs {
				if r.F.Del != "" {
					continue
				}
				r.F.Ins = ""
				runs = append(runs, r)
			}
			p.Runs = runs
			p.Normalize()
			if gone && len(p.Runs) == 0 && p.PF.Sect == nil {
				continue
			}
			keep = append(keep, b)
		}
		if len(keep) == 0 {
			keep = []Block{{P: NewPara()}}
		}
		*arr = keep
	}
	fix(&d.Body)
	for _, n := range d.Footnotes {
		fix(&n.Blocks)
	}
}
