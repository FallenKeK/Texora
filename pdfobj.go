package main

// Читалка PDF, часть 1: объекты, перекрёстные ссылки, потоки, фильтры, шифрование.

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

type pdfName string
type pdfRef struct{ N, G int }
type pdfDict map[string]any
type pdfArray []any
type pdfString []byte
type pdfKeyword string

type pdfStream struct {
	Dict pdfDict
	Raw  []byte
	ref  pdfRef
	dec  []byte
	err  error
	done bool
}

type xrefEntry struct {
	off    int64
	gen    int
	stream int // номер объектного потока (0 — обычный объект)
	idx    int
	free   bool
}

type PdfFile struct {
	data    []byte
	xref    map[int]xrefEntry
	cache   map[int]any
	trailer pdfDict
	crypt   *pdfCrypt
	objStm  map[int]map[int]any
	depth   int
}

// ---------------------------------------------------------------- лексер
type pdfLexer struct {
	b []byte
	p int
}

func isWS(c byte) bool    { return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0 }
func isDelim(c byte) bool { return strings.IndexByte("()<>[]{}/%", c) >= 0 }

func (l *pdfLexer) skip() {
	for l.p < len(l.b) {
		c := l.b[l.p]
		if isWS(c) {
			l.p++
		} else if c == '%' {
			for l.p < len(l.b) && l.b[l.p] != '\n' && l.b[l.p] != '\r' {
				l.p++
			}
		} else {
			return
		}
	}
}

func hexv(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

var errEOF = errors.New("конец данных")

// token читает один элемент (без сборки ссылок «N G R»).
func (l *pdfLexer) token() (any, error) {
	l.skip()
	if l.p >= len(l.b) {
		return nil, errEOF
	}
	c := l.b[l.p]
	switch {
	case c == '/':
		l.p++
		var sb []byte
		for l.p < len(l.b) && !isWS(l.b[l.p]) && !isDelim(l.b[l.p]) {
			ch := l.b[l.p]
			if ch == '#' && l.p+2 < len(l.b) && hexv(l.b[l.p+1]) >= 0 && hexv(l.b[l.p+2]) >= 0 {
				sb = append(sb, byte(hexv(l.b[l.p+1])<<4|hexv(l.b[l.p+2])))
				l.p += 3
				continue
			}
			sb = append(sb, ch)
			l.p++
		}
		return pdfName(sb), nil
	case c == '(':
		l.p++
		depth := 1
		var sb []byte
		for l.p < len(l.b) {
			ch := l.b[l.p]
			l.p++
			if ch == '\\' && l.p < len(l.b) {
				e := l.b[l.p]
				l.p++
				switch e {
				case 'n':
					sb = append(sb, '\n')
				case 'r':
					sb = append(sb, '\r')
				case 't':
					sb = append(sb, '\t')
				case 'b':
					sb = append(sb, '\b')
				case 'f':
					sb = append(sb, '\f')
				case '\r':
					if l.p < len(l.b) && l.b[l.p] == '\n' {
						l.p++
					}
				case '\n':
				default:
					if e >= '0' && e <= '7' {
						v := int(e - '0')
						for k := 0; k < 2 && l.p < len(l.b) && l.b[l.p] >= '0' && l.b[l.p] <= '7'; k++ {
							v = v*8 + int(l.b[l.p]-'0')
							l.p++
						}
						sb = append(sb, byte(v))
					} else {
						sb = append(sb, e)
					}
				}
				continue
			}
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
			sb = append(sb, ch)
		}
		return pdfString(sb), nil
	case c == '<':
		if l.p+1 < len(l.b) && l.b[l.p+1] == '<' {
			l.p += 2
			return pdfKeyword("<<"), nil
		}
		l.p++
		var sb []byte
		hi := -1
		for l.p < len(l.b) && l.b[l.p] != '>' {
			v := hexv(l.b[l.p])
			l.p++
			if v < 0 {
				continue
			}
			if hi < 0 {
				hi = v
			} else {
				sb = append(sb, byte(hi<<4|v))
				hi = -1
			}
		}
		if hi >= 0 {
			sb = append(sb, byte(hi<<4))
		}
		l.p++
		return pdfString(sb), nil
	case c == '>':
		if l.p+1 < len(l.b) && l.b[l.p+1] == '>' {
			l.p += 2
			return pdfKeyword(">>"), nil
		}
		l.p++
		return pdfKeyword(">"), nil
	case c == '[' || c == ']' || c == '{' || c == '}':
		l.p++
		return pdfKeyword(string(c)), nil
	case c == ')':
		l.p++
		return pdfKeyword(")"), nil
	}
	s := l.p
	for l.p < len(l.b) && !isWS(l.b[l.p]) && !isDelim(l.b[l.p]) {
		l.p++
	}
	w := string(l.b[s:l.p])
	if w == "" {
		l.p++
		return pdfKeyword(""), nil
	}
	if (w[0] >= '0' && w[0] <= '9') || w[0] == '-' || w[0] == '+' || w[0] == '.' {
		if v, err := strconv.ParseFloat(w, 64); err == nil {
			return v, nil
		}
		// «--5», «1.2.3» — пробуем то, что можно
		clean := strings.TrimLeft(w, "+-")
		if v, err := strconv.ParseFloat(clean, 64); err == nil {
			if strings.HasPrefix(w, "-") {
				v = -v
			}
			return v, nil
		}
		return 0.0, nil
	}
	switch w {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	return pdfKeyword(w), nil
}

// object читает объект целиком (массивы, словари, ссылки).
func (l *pdfLexer) object() (any, error) {
	t, err := l.token()
	if err != nil {
		return nil, err
	}
	return l.finish(t)
}

func (l *pdfLexer) finish(t any) (any, error) {
	switch v := t.(type) {
	case pdfKeyword:
		switch v {
		case "[":
			var arr pdfArray
			for {
				x, err := l.token()
				if err != nil {
					return arr, nil
				}
				if k, ok := x.(pdfKeyword); ok && k == "]" {
					return arr, nil
				}
				o, err := l.finish(x)
				if err != nil {
					return arr, nil
				}
				arr = append(arr, o)
				arr = foldRefs(arr)
			}
		case "<<":
			d := pdfDict{}
			for {
				k, err := l.token()
				if err != nil {
					return d, nil
				}
				if kw, ok := k.(pdfKeyword); ok && kw == ">>" {
					return d, nil
				}
				name, ok := k.(pdfName)
				if !ok {
					continue
				}
				val, err := l.object()
				if err != nil {
					return d, nil
				}
				if kw, ok := val.(pdfKeyword); ok && kw == ">>" {
					d[string(name)] = nil
					return d, nil
				}
				// ссылка «N G R»
				if n, ok := val.(float64); ok {
					save := l.p
					g, e1 := l.token()
					r, e2 := l.token()
					if gf, ok := g.(float64); ok && e1 == nil && e2 == nil {
						if rk, ok := r.(pdfKeyword); ok && rk == "R" {
							d[string(name)] = pdfRef{int(n), int(gf)}
							continue
						}
					}
					l.p = save
				}
				d[string(name)] = val
			}
		}
	}
	return t, nil
}

// foldRefs сворачивает «N G R» в конце массива в ссылку.
func foldRefs(a pdfArray) pdfArray {
	n := len(a)
	if n >= 3 {
		if k, ok := a[n-1].(pdfKeyword); ok && k == "R" {
			x, ok1 := a[n-3].(float64)
			g, ok2 := a[n-2].(float64)
			if ok1 && ok2 {
				return append(a[:n-3], pdfRef{int(x), int(g)})
			}
		}
	}
	return a
}

// ---------------------------------------------------------------- открытие
func OpenPDF(data []byte) (*PdfFile, error) {
	if !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF")) {
		return nil, fmt.Errorf("это не PDF")
	}
	f := &PdfFile{data: data, xref: map[int]xrefEntry{}, cache: map[int]any{}, objStm: map[int]map[int]any{}}
	ok := f.readXrefChain()
	if !ok || f.trailer == nil || f.trailer["Root"] == nil {
		f.reconstruct()
	}
	if f.trailer == nil || f.trailer["Root"] == nil {
		return nil, fmt.Errorf("не удалось разобрать структуру PDF")
	}
	if enc := f.trailer["Encrypt"]; enc != nil {
		c, err := f.newCrypt(f.dict(enc), "")
		if err != nil {
			return nil, err
		}
		f.crypt = c
		f.cache = map[int]any{}
		f.objStm = map[int]map[int]any{}
	}
	return f, nil
}

func (f *PdfFile) readXrefChain() bool {
	tail := f.data[max(0, len(f.data)-2048):]
	i := bytes.LastIndex(tail, []byte("startxref"))
	if i < 0 {
		return false
	}
	l := &pdfLexer{b: tail, p: i + 9}
	t, _ := l.token()
	off, ok := t.(float64)
	if !ok {
		return false
	}
	seen := map[int64]bool{}
	pos := int64(off)
	first := true
	for pos > 0 && pos < int64(len(f.data)) && !seen[pos] {
		seen[pos] = true
		tr, prev, ok := f.readXrefAt(pos)
		if !ok {
			return false
		}
		if first {
			f.trailer = tr
			first = false
		}
		// гибридные файлы: XRefStm
		if xs, ok := tr["XRefStm"].(float64); ok && !seen[int64(xs)] {
			seen[int64(xs)] = true
			f.readXrefAt(int64(xs))
		}
		pos = prev
	}
	return !first
}

func (f *PdfFile) readXrefAt(pos int64) (pdfDict, int64, bool) {
	l := &pdfLexer{b: f.data, p: int(pos)}
	l.skip()
	if bytes.HasPrefix(f.data[l.p:], []byte("xref")) {
		l.p += 4
		for {
			t, err := l.token()
			if err != nil {
				return nil, 0, false
			}
			if k, ok := t.(pdfKeyword); ok && k == "trailer" {
				break
			}
			start, ok := t.(float64)
			if !ok {
				return nil, 0, false
			}
			c, _ := l.token()
			count, _ := c.(float64)
			l.skip()
			for i := 0; i < int(count); i++ {
				l.skip()
				if l.p+18 > len(f.data) {
					break
				}
				line := string(f.data[l.p:min(len(f.data), l.p+20)])
				fs := strings.Fields(line)
				if len(fs) < 3 {
					break
				}
				o, _ := strconv.ParseInt(fs[0], 10, 64)
				g, _ := strconv.Atoi(fs[1])
				n := int(start) + i
				if _, exists := f.xref[n]; !exists {
					f.xref[n] = xrefEntry{off: o, gen: g, free: fs[2][0] == 'f'}
				}
				l.p += len(fs[0]) + 1 + len(fs[1]) + 1 + 1
				for l.p < len(f.data) && (f.data[l.p] == ' ' || f.data[l.p] == '\r' || f.data[l.p] == '\n') {
					l.p++
				}
			}
		}
		o, err := l.object()
		if err != nil {
			return nil, 0, false
		}
		tr, _ := o.(pdfDict)
		prev, _ := tr["Prev"].(float64)
		return tr, int64(prev), tr != nil
	}
	// поток перекрёстных ссылок
	obj, err := f.parseIndirect(int(pos))
	if err != nil {
		return nil, 0, false
	}
	s, ok := obj.(*pdfStream)
	if !ok || s.Dict["Type"] != pdfName("XRef") {
		return nil, 0, false
	}
	data, err := f.decodeStream(s, true)
	if err != nil && len(data) == 0 {
		return nil, 0, false
	}
	w := f.numArray(s.Dict["W"])
	if len(w) < 3 {
		return nil, 0, false
	}
	idx := f.numArray(s.Dict["Index"])
	if len(idx) == 0 {
		idx = []float64{0, f.num(s.Dict["Size"])}
	}
	rw := int(w[0] + w[1] + w[2])
	p := 0
	read := func(n int) int64 {
		var v int64
		for k := 0; k < n && p < len(data); k++ {
			v = v<<8 | int64(data[p])
			p++
		}
		return v
	}
	for i := 0; i+1 < len(idx); i += 2 {
		st, cnt := int(idx[i]), int(idx[i+1])
		for k := 0; k < cnt && p+rw <= len(data); k++ {
			typ := int64(1)
			if w[0] > 0 {
				typ = read(int(w[0]))
			}
			a := read(int(w[1]))
			b := read(int(w[2]))
			n := st + k
			if _, exists := f.xref[n]; exists {
				continue
			}
			switch typ {
			case 0:
				f.xref[n] = xrefEntry{free: true}
			case 1:
				f.xref[n] = xrefEntry{off: a, gen: int(b)}
			case 2:
				f.xref[n] = xrefEntry{stream: int(a), idx: int(b)}
			}
		}
	}
	prev, _ := s.Dict["Prev"].(float64)
	return s.Dict, int64(prev), true
}

// reconstruct — восстановление по «N G obj», если таблица ссылок испорчена.
func (f *PdfFile) reconstruct() {
	f.xref = map[int]xrefEntry{}
	d := f.data
	for i := 0; i < len(d)-4; i++ {
		if d[i] != 'o' || d[i+1] != 'b' || d[i+2] != 'j' {
			continue
		}
		// назад: пробел, число, пробел, число
		j := i - 1
		for j >= 0 && isWS(d[j]) {
			j--
		}
		ge := j + 1
		for j >= 0 && d[j] >= '0' && d[j] <= '9' {
			j--
		}
		gs := j + 1
		for j >= 0 && isWS(d[j]) {
			j--
		}
		ne := j + 1
		for j >= 0 && d[j] >= '0' && d[j] <= '9' {
			j--
		}
		ns := j + 1
		if gs >= ge || ns >= ne {
			continue
		}
		n, _ := strconv.Atoi(string(d[ns:ne]))
		g, _ := strconv.Atoi(string(d[gs:ge]))
		f.xref[n] = xrefEntry{off: int64(ns), gen: g}
	}
	// трейлер — последний, где есть Root; иначе ищем каталог
	for at := bytes.LastIndex(d, []byte("trailer")); at >= 0; {
		l := &pdfLexer{b: d, p: at + 7}
		if o, err := l.object(); err == nil {
			if t, ok := o.(pdfDict); ok && t["Root"] != nil {
				f.trailer = t
				return
			}
		}
		at = bytes.LastIndex(d[:at], []byte("trailer"))
	}
	f.trailer = pdfDict{}
	for n := range f.xref {
		if o, ok := f.get(n).(pdfDict); ok && o["Type"] == pdfName("Catalog") {
			f.trailer["Root"] = pdfRef{n, 0}
		}
		if s, ok := f.get(n).(*pdfStream); ok && s.Dict["Type"] == pdfName("XRef") && s.Dict["Root"] != nil {
			f.trailer["Root"] = s.Dict["Root"]
		}
	}
}

func (f *PdfFile) parseIndirect(pos int) (any, error) {
	l := &pdfLexer{b: f.data, p: pos}
	n, _ := l.token()
	_, _ = l.token()
	kw, _ := l.token()
	if k, ok := kw.(pdfKeyword); !ok || k != "obj" {
		return nil, fmt.Errorf("нет объекта")
	}
	o, err := l.object()
	if err != nil {
		return nil, err
	}
	if d, ok := o.(pdfDict); ok {
		save := l.p
		t, _ := l.token()
		if k, ok := t.(pdfKeyword); ok && k == "stream" {
			p := l.p
			if p < len(f.data) && f.data[p] == '\r' {
				p++
			}
			if p < len(f.data) && f.data[p] == '\n' {
				p++
			}
			ln := -1
			switch v := d["Length"].(type) {
			case float64:
				ln = int(v)
			case pdfRef:
				if f.depth < 4 {
					f.depth++
					if x, ok := f.Resolve(v).(float64); ok {
						ln = int(x)
					}
					f.depth--
				}
			}
			end := p + ln
			if ln < 0 || end > len(f.data) || !bytes.HasPrefix(bytes.TrimLeft(f.data[end:min(len(f.data), end+20)], "\r\n \t"), []byte("endstream")) {
				if k := bytes.Index(f.data[p:], []byte("endstream")); k >= 0 {
					end = p + k
					for end > p && (f.data[end-1] == '\n' || f.data[end-1] == '\r') {
						end--
					}
				} else {
					end = len(f.data)
				}
			}
			nn, _ := n.(float64)
			return &pdfStream{Dict: d, Raw: f.data[p:end], ref: pdfRef{N: int(nn)}}, nil
		}
		l.p = save
	}
	return o, nil
}

func (f *PdfFile) get(n int) any {
	if v, ok := f.cache[n]; ok {
		return v
	}
	e, ok := f.xref[n]
	if !ok || e.free {
		return nil
	}
	var v any
	if e.stream > 0 {
		v = f.fromObjStm(e.stream, n, e.idx)
	} else {
		o, err := f.parseIndirect(int(e.off))
		if err != nil {
			return nil
		}
		v = o
		if f.crypt != nil {
			v = f.crypt.decryptObj(v, n, e.gen)
		}
	}
	if s, ok := v.(*pdfStream); ok {
		s.ref = pdfRef{n, e.gen}
	}
	f.cache[n] = v
	return v
}

func (f *PdfFile) fromObjStm(sn, n, idx int) any {
	m, ok := f.objStm[sn]
	if !ok {
		m = map[int]any{}
		f.objStm[sn] = m
		s, ok := f.get(sn).(*pdfStream)
		if !ok {
			return nil
		}
		data, _ := f.decodeStream(s, false)
		cnt := int(f.num(s.Dict["N"]))
		first := int(f.num(s.Dict["First"]))
		l := &pdfLexer{b: data}
		type pr struct{ n, off int }
		var prs []pr
		for i := 0; i < cnt; i++ {
			a, _ := l.token()
			b, _ := l.token()
			an, _ := a.(float64)
			bn, _ := b.(float64)
			prs = append(prs, pr{int(an), int(bn)})
		}
		for _, p := range prs {
			if first+p.off >= len(data) {
				continue
			}
			ll := &pdfLexer{b: data, p: first + p.off}
			o, err := ll.object()
			if err == nil {
				m[p.n] = o
			}
		}
	}
	return m[n]
}

// Resolve раскрывает ссылки.
func (f *PdfFile) Resolve(v any) any {
	for i := 0; i < 16; i++ {
		r, ok := v.(pdfRef)
		if !ok {
			return v
		}
		v = f.get(r.N)
	}
	return v
}

func (f *PdfFile) dict(v any) pdfDict {
	switch x := f.Resolve(v).(type) {
	case pdfDict:
		return x
	case *pdfStream:
		return x.Dict
	}
	return nil
}

func (f *PdfFile) arr(v any) pdfArray {
	a, _ := f.Resolve(v).(pdfArray)
	return a
}

func (f *PdfFile) num(v any) float64 {
	switch x := f.Resolve(v).(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

func (f *PdfFile) numArray(v any) []float64 {
	var out []float64
	for _, x := range f.arr(v) {
		out = append(out, f.num(x))
	}
	return out
}

func (f *PdfFile) name(v any) string {
	n, _ := f.Resolve(v).(pdfName)
	return string(n)
}

func (f *PdfFile) stream(v any) *pdfStream {
	s, _ := f.Resolve(v).(*pdfStream)
	return s
}

// textString — строка документа (UTF-16 или PDFDocEncoding).
func textString(b []byte) string {
	if len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff {
		var u []uint16
		for i := 2; i+1 < len(b); i += 2 {
			u = append(u, binary.BigEndian.Uint16(b[i:]))
		}
		return string(utf16.Decode(u))
	}
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		return string(b[3:])
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = pdfDocEnc(c)
	}
	return string(r)
}

func pdfDocEnc(c byte) rune {
	switch c {
	case 0x80:
		return '•'
	case 0x84:
		return '—'
	case 0x85:
		return '–'
	case 0x8d:
		return '“'
	case 0x8e:
		return '”'
	case 0x8f:
		return '‘'
	case 0x90:
		return '’'
	case 0xa0:
		return '€'
	}
	return rune(c)
}

// ---------------------------------------------------------------- фильтры
func (f *PdfFile) decodeStream(s *pdfStream, raw bool) ([]byte, error) {
	if s.done {
		return s.dec, s.err
	}
	data := s.Raw
	filters := []string{}
	var parms []pdfDict
	switch fv := f.Resolve(s.Dict["Filter"]).(type) {
	case pdfName:
		filters = append(filters, string(fv))
		parms = append(parms, f.dict(s.Dict["DecodeParms"]))
	case pdfArray:
		pa := f.arr(s.Dict["DecodeParms"])
		for i, x := range fv {
			filters = append(filters, f.name(x))
			if i < len(pa) {
				parms = append(parms, f.dict(pa[i]))
			} else {
				parms = append(parms, nil)
			}
		}
	}
	var err error
	for i, name := range filters {
		var e error
		switch name {
		case "FlateDecode", "Fl":
			data, e = inflate(data)
			if parms[i] != nil {
				data = f.predict(data, parms[i])
			}
		case "LZWDecode", "LZW":
			early := 1
			if parms[i] != nil {
				if v, ok := parms[i]["EarlyChange"].(float64); ok {
					early = int(v)
				}
			}
			data = lzwDecode(data, early)
			if parms[i] != nil {
				data = f.predict(data, parms[i])
			}
		case "ASCIIHexDecode", "AHx":
			data = asciiHex(data)
		case "ASCII85Decode", "A85":
			data = ascii85(data)
		case "RunLengthDecode", "RL":
			data = runLength(data)
		case "Crypt":
		default:
			// DCT, JPX, CCITT, JBIG2 — разбирает тот, кто рисует картинку
			s.dec, s.err, s.done = data, nil, true
			return data, nil
		}
		if e != nil && err == nil {
			err = e
		}
	}
	s.dec, s.err, s.done = data, err, true
	return data, err
}

// filterParms — DecodeParms последнего фильтра.
func (f *PdfFile) filterParms(s *pdfStream) pdfDict {
	v := f.Resolve(s.Dict["DecodeParms"])
	if v == nil {
		v = f.Resolve(s.Dict["DP"])
	}
	switch x := v.(type) {
	case pdfDict:
		return x
	case pdfArray:
		if len(x) > 0 {
			if d, ok := f.Resolve(x[len(x)-1]).(pdfDict); ok {
				return d
			}
		}
	}
	return pdfDict{}
}

// imageFilter — последний фильтр картинки, который не раскрыт (DCTDecode и т. п.).
func (f *PdfFile) imageFilter(s *pdfStream) string {
	switch fv := f.Resolve(s.Dict["Filter"]).(type) {
	case pdfName:
		return string(fv)
	case pdfArray:
		if len(fv) > 0 {
			return f.name(fv[len(fv)-1])
		}
	}
	return ""
}

func inflate(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err == nil {
		out, err2 := io.ReadAll(io.LimitReader(r, 1<<30))
		if len(out) > 0 || err2 == nil {
			return out, nil
		}
	}
	// без заголовка zlib или битый поток — берём что вышло
	fr := flate.NewReader(bytes.NewReader(b[min(2, len(b)):]))
	out, err := io.ReadAll(io.LimitReader(fr, 1<<30))
	if len(out) > 0 {
		return out, nil
	}
	return nil, err
}

func (f *PdfFile) predict(b []byte, p pdfDict) []byte {
	pred := int(f.num(p["Predictor"]))
	if pred < 2 {
		return b
	}
	colors := max(1, int(f.num(p["Colors"])))
	bpc := int(f.num(p["BitsPerComponent"]))
	if bpc == 0 {
		bpc = 8
	}
	cols := max(1, int(f.num(p["Columns"])))
	bpp := max(1, (colors*bpc+7)/8)
	rowLen := (colors*bpc*cols + 7) / 8
	if pred == 2 {
		out := append([]byte{}, b...)
		if bpc == 8 {
			for r := 0; r+rowLen <= len(out); r += rowLen {
				for i := bpp; i < rowLen; i++ {
					out[r+i] += out[r+i-bpp]
				}
			}
		}
		return out
	}
	var out []byte
	prev := make([]byte, rowLen)
	for r := 0; r+1 <= len(b); r += rowLen + 1 {
		ft := b[r]
		end := min(len(b), r+1+rowLen)
		row := make([]byte, rowLen)
		copy(row, b[r+1:end])
		for i := 0; i < rowLen; i++ {
			var a, c byte
			if i >= bpp {
				a = row[i-bpp]
				c = prev[i-bpp]
			}
			up := prev[i]
			switch ft {
			case 1:
				row[i] += a
			case 2:
				row[i] += up
			case 3:
				row[i] += byte((int(a) + int(up)) / 2)
			case 4:
				pa := abs(int(up) - int(c))
				pb := abs(int(a) - int(c))
				pc := abs(int(a) + int(up) - 2*int(c))
				pr := a
				if pa > pb || pa > pc {
					if pb <= pc {
						pr = up
					} else {
						pr = c
					}
				}
				row[i] += pr
			}
		}
		out = append(out, row...)
		prev = row
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func asciiHex(b []byte) []byte {
	var out []byte
	hi := -1
	for _, c := range b {
		if c == '>' {
			break
		}
		v := hexv(c)
		if v < 0 {
			continue
		}
		if hi < 0 {
			hi = v
		} else {
			out = append(out, byte(hi<<4|v))
			hi = -1
		}
	}
	if hi >= 0 {
		out = append(out, byte(hi<<4))
	}
	return out
}

func ascii85(b []byte) []byte {
	var out []byte
	var tup uint32
	n := 0
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == '~' {
			break
		}
		if isWS(c) {
			continue
		}
		if c == 'z' && n == 0 {
			out = append(out, 0, 0, 0, 0)
			continue
		}
		if c < '!' || c > 'u' {
			continue
		}
		tup = tup*85 + uint32(c-'!')
		n++
		if n == 5 {
			out = append(out, byte(tup>>24), byte(tup>>16), byte(tup>>8), byte(tup))
			tup, n = 0, 0
		}
	}
	if n > 1 {
		for k := n; k < 5; k++ {
			tup = tup*85 + 84
		}
		for k := 0; k < n-1; k++ {
			out = append(out, byte(tup>>(24-8*k)))
		}
	}
	return out
}

func runLength(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		l := int(b[i])
		i++
		if l == 128 {
			break
		}
		if l < 128 {
			end := min(len(b), i+l+1)
			out = append(out, b[i:end]...)
			i = end
		} else if i < len(b) {
			for k := 0; k < 257-l; k++ {
				out = append(out, b[i])
			}
			i++
		}
	}
	return out
}

func lzwDecode(b []byte, early int) []byte {
	var out []byte
	dict := make([][]byte, 0, 4096)
	reset := func() {
		dict = dict[:0]
		for i := 0; i < 256; i++ {
			dict = append(dict, []byte{byte(i)})
		}
		dict = append(dict, nil, nil)
	}
	reset()
	bits, buf, nbits := 9, uint32(0), 0
	var prev []byte
	for i := 0; i < len(b); i++ {
		buf = buf<<8 | uint32(b[i])
		nbits += 8
		for nbits >= bits {
			code := int(buf >> uint(nbits-bits) & (1<<uint(bits) - 1))
			nbits -= bits
			if code == 256 {
				reset()
				bits, prev = 9, nil
				continue
			}
			if code == 257 {
				return out
			}
			var entry []byte
			if code < len(dict) && dict[code] != nil {
				entry = dict[code]
			} else if prev != nil {
				entry = append(append([]byte{}, prev...), prev[0])
			} else {
				continue
			}
			out = append(out, entry...)
			if prev != nil && len(dict) < 4096 {
				dict = append(dict, append(append([]byte{}, prev...), entry[0]))
			}
			prev = entry
			if len(dict)+early >= 1<<uint(bits) && bits < 12 {
				bits++
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- шифрование (стандартная защита)
type pdfCrypt struct {
	key    []byte
	v, r   int
	aes    bool
	aes256 bool
	strF   string
	stmF   string
}

var pdfPad = []byte{0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A}

var ErrPDFPassword = errors.New("PDF защищён паролем")

func (f *PdfFile) newCrypt(enc pdfDict, password string) (*pdfCrypt, error) {
	if enc == nil || f.name(enc["Filter"]) != "Standard" {
		return nil, fmt.Errorf("неподдерживаемая защита PDF")
	}
	c := &pdfCrypt{v: int(f.num(enc["V"])), r: int(f.num(enc["R"]))}
	bs := func(k string) []byte { b, _ := f.Resolve(enc[k]).(pdfString); return []byte(b) }
	O, U := bs("O"), bs("U")
	P := int32(int64(f.num(enc["P"])))
	length := int(f.num(enc["Length"]))
	if length == 0 {
		length = 40
	}
	if c.v >= 4 {
		cf := f.dict(enc["CF"])
		stdName := f.name(enc["StmF"])
		if std := f.dict(cf[stdName]); std != nil {
			switch f.name(std["CFM"]) {
			case "AESV2":
				c.aes = true
			case "AESV3":
				c.aes, c.aes256 = true, true
			}
			if l := f.num(std["Length"]); l > 0 && c.v == 4 {
				length = int(l)
				if length <= 32 {
					length *= 8
				}
			}
		}
		if stdName == "Identity" {
			return nil, nil
		}
	}
	if c.r >= 5 {
		// AES-256: ключ из UE по SHA-256 (R5) или хешу R6
		pw := []byte(password)
		if len(U) < 48 {
			return nil, ErrPDFPassword
		}
		h := pdfHashR6(pw, U[40:48], nil, c.r)
		UE := bs("UE")
		if len(UE) < 32 {
			return nil, ErrPDFPassword
		}
		blk, _ := aes.NewCipher(h)
		key := make([]byte, 32)
		cipher.NewCBCDecrypter(blk, make([]byte, 16)).CryptBlocks(key, UE[:32])
		c.key = key
		check := pdfHashR6(pw, U[32:40], nil, c.r)
		if !bytes.Equal(check, U[:32]) {
			return nil, ErrPDFPassword
		}
		return c, nil
	}
	n := length / 8
	if c.r == 2 {
		n = 5
	}
	pw := append([]byte(password), pdfPad...)[:32]
	h := md5.New()
	h.Write(pw)
	h.Write(O)
	binary.Write(h, binary.LittleEndian, P)
	id := f.arr(f.trailer["ID"])
	if len(id) > 0 {
		b, _ := f.Resolve(id[0]).(pdfString)
		h.Write(b)
	}
	if c.r >= 4 {
		if em, ok := enc["EncryptMetadata"].(bool); ok && !em {
			h.Write([]byte{0xff, 0xff, 0xff, 0xff})
		}
	}
	key := h.Sum(nil)
	if c.r >= 3 {
		for i := 0; i < 50; i++ {
			s := md5.Sum(key[:n])
			key = s[:]
		}
	}
	c.key = key[:n]
	return c, nil
}

func pdfHashR6(pw, salt, udata []byte, r int) []byte {
	h := sha256.New()
	h.Write(pw)
	h.Write(salt)
	h.Write(udata)
	k := h.Sum(nil)
	if r < 6 {
		return k
	}
	for i := 0; ; i++ {
		var k1 []byte
		for j := 0; j < 64; j++ {
			k1 = append(k1, pw...)
			k1 = append(k1, k...)
			k1 = append(k1, udata...)
		}
		blk, _ := aes.NewCipher(k[:16])
		e := make([]byte, len(k1))
		cipher.NewCBCEncrypter(blk, k[16:32]).CryptBlocks(e, k1)
		sum := 0
		for _, b := range e[:16] {
			sum += int(b)
		}
		switch sum % 3 {
		case 0:
			s := sha256.Sum256(e)
			k = s[:]
		case 1:
			s := sha512.Sum384(e)
			k = s[:]
		default:
			s := sha512.Sum512(e)
			k = s[:]
		}
		if i >= 63 && int(e[len(e)-1]) <= i-32 {
			break
		}
	}
	return k[:32]
}

func (c *pdfCrypt) objKey(n, g int) []byte {
	if c.aes256 {
		return c.key
	}
	k := append([]byte{}, c.key...)
	k = append(k, byte(n), byte(n>>8), byte(n>>16), byte(g), byte(g>>8))
	if c.aes {
		k = append(k, 's', 'A', 'l', 'T')
	}
	s := md5.Sum(k)
	return s[:min(len(c.key)+5, 16)]
}

func (c *pdfCrypt) decrypt(b []byte, n, g int) []byte {
	key := c.objKey(n, g)
	if c.aes {
		if len(b) < 32 || len(b)%16 != 0 {
			return b
		}
		blk, err := aes.NewCipher(key)
		if err != nil {
			return b
		}
		out := make([]byte, len(b)-16)
		cipher.NewCBCDecrypter(blk, b[:16]).CryptBlocks(out, b[16:])
		if p := int(out[len(out)-1]); p > 0 && p <= 16 && p <= len(out) {
			out = out[:len(out)-p]
		}
		return out
	}
	r, err := rc4.NewCipher(key)
	if err != nil {
		return b
	}
	out := make([]byte, len(b))
	r.XORKeyStream(out, b)
	return out
}

func (c *pdfCrypt) decryptObj(v any, n, g int) any {
	switch x := v.(type) {
	case pdfString:
		return pdfString(c.decrypt(x, n, g))
	case pdfArray:
		for i := range x {
			x[i] = c.decryptObj(x[i], n, g)
		}
	case pdfDict:
		for k, e := range x {
			x[k] = c.decryptObj(e, n, g)
		}
	case *pdfStream:
		if x.Dict["Type"] != pdfName("XRef") {
			x.Raw = c.decrypt(x.Raw, n, g)
		}
		for k, e := range x.Dict {
			x.Dict[k] = c.decryptObj(e, n, g)
		}
	}
	return v
}

// ---------------------------------------------------------------- страницы и оглавление
type PdfPage struct {
	Ref    int
	Dict   pdfDict
	Res    pdfDict
	Box    [4]float64 // видимая область (CropBox или MediaBox)
	Rotate int
}

func (p *PdfPage) W() float64 {
	if p.Rotate%180 != 0 {
		return p.Box[3] - p.Box[1]
	}
	return p.Box[2] - p.Box[0]
}

func (p *PdfPage) H() float64 {
	if p.Rotate%180 != 0 {
		return p.Box[2] - p.Box[0]
	}
	return p.Box[3] - p.Box[1]
}

func (f *PdfFile) Pages() []*PdfPage {
	var out []*PdfPage
	root := f.dict(f.trailer["Root"])
	seen := map[*pdfDict]bool{}
	_ = seen
	visited := map[int]bool{}
	var walk func(nv any, res pdfDict, media, crop []float64, rot int, depth int)
	walk = func(nv any, res pdfDict, media, crop []float64, rot int, depth int) {
		ref := 0
		if rf, ok := nv.(pdfRef); ok {
			ref = rf.N
			if visited[ref] {
				return
			}
			visited[ref] = true
		}
		node := f.dict(nv)
		if node == nil || depth > 64 {
			return
		}
		if r := f.dict(node["Resources"]); r != nil {
			res = r
		}
		if m := f.numArray(node["MediaBox"]); len(m) == 4 {
			media = m
		}
		if c := f.numArray(node["CropBox"]); len(c) == 4 {
			crop = c
		}
		if _, ok := node["Rotate"]; ok {
			rot = int(f.num(node["Rotate"]))
		}
		kids := f.arr(node["Kids"])
		if f.name(node["Type"]) == "Pages" || (kids != nil && node["Contents"] == nil) {
			for _, k := range kids {
				walk(k, res, media, crop, rot, depth+1)
			}
			return
		}
		box := media
		if len(box) != 4 {
			box = []float64{0, 0, 612, 792}
		}
		if len(crop) == 4 {
			// пересечение с MediaBox
			box = []float64{math.Max(math.Min(crop[0], crop[2]), math.Min(box[0], box[2])), math.Max(math.Min(crop[1], crop[3]), math.Min(box[1], box[3])),
				math.Min(math.Max(crop[0], crop[2]), math.Max(box[0], box[2])), math.Min(math.Max(crop[1], crop[3]), math.Max(box[1], box[3]))}
		}
		x0, y0, x1, y1 := math.Min(box[0], box[2]), math.Min(box[1], box[3]), math.Max(box[0], box[2]), math.Max(box[1], box[3])
		// битый или безумный размер (PDF допускает не больше 14400 пт по стороне) — лист Letter, как делают другие читалки
		if !(x1-x0 >= 1 && y1-y0 >= 1 && x1-x0 <= 14400 && y1-y0 <= 14400) || math.IsInf(x0, 0) || math.IsInf(y0, 0) {
			x0, y0, x1, y1 = 0, 0, 612, 792
		}
		rot = ((rot % 360) + 360) % 360
		out = append(out, &PdfPage{Ref: ref, Dict: node, Res: res, Box: [4]float64{x0, y0, x1, y1}, Rotate: rot})
	}
	walk(root["Pages"], nil, nil, nil, 0, 0)
	return out
}

// Content — объединённый поток содержимого страницы.
func (f *PdfFile) Content(p *PdfPage) []byte {
	var out []byte
	add := func(v any) {
		if s := f.stream(v); s != nil {
			b, _ := f.decodeStream(s, false)
			out = append(out, b...)
			out = append(out, '\n')
		}
	}
	switch c := f.Resolve(p.Dict["Contents"]).(type) {
	case pdfArray:
		for _, x := range c {
			add(x)
		}
	case *pdfStream:
		add(c)
	}
	return out
}

type PdfOutline struct {
	Title string
	Page  int
	Y     float64 // координата на странице (сверху), -1 — начало
	Level int
}

// destResolver — функция, превращающая назначение PDF (массив, имя, строку) в номер листа и Y сверху (в пунктах).
func (f *PdfFile) destResolver(pages []*PdfPage) func(d any) (int, float64) {
	pageIdx := map[int]int{}
	for i, p := range pages {
		if p.Ref > 0 {
			pageIdx[p.Ref] = i
		}
	}
	root := f.dict(f.trailer["Root"])
	var names map[string]any
	return func(d any) (int, float64) {
		d = f.Resolve(d)
		switch x := d.(type) {
		case pdfString, pdfName:
			if names == nil {
				names = f.namedDests(root)
			}
			if s, ok := x.(pdfString); ok {
				d = names[string(s)]
			} else {
				d = names[string(x.(pdfName))]
			}
		}
		if dd, ok := f.Resolve(d).(pdfDict); ok {
			d = dd["D"]
		}
		a := f.arr(d)
		if len(a) == 0 {
			return -1, -1
		}
		pg := -1
		switch r := a[0].(type) {
		case pdfRef:
			if i, ok := pageIdx[r.N]; ok {
				pg = i
			}
		case float64:
			pg = int(r)
		}
		if pg >= len(pages) {
			pg = -1
		}
		y := -1.0
		if pg >= 0 && len(a) >= 3 {
			var top any
			switch f.name(a[1]) {
			case "XYZ":
				if len(a) >= 4 {
					top = a[3]
				}
			case "FitH", "FitBH":
				top = a[2]
			}
			if v, ok := f.Resolve(top).(float64); ok {
				y = pages[pg].Box[3] - v
			}
		}
		return pg, y
	}
}

// PdfLink — ссылка на листе: прямоугольник в пунктах (сверху вниз) и куда ведёт.
type PdfLink struct {
	X, Y, W, H float64
	Page       int
	DestY      float64
	URI        string
}

// Links — ссылки (аннотации /Link) листа i.
func (f *PdfFile) Links(pages []*PdfPage, i int, dest func(any) (int, float64)) []PdfLink {
	p := pages[i]
	var out []PdfLink
	for _, av := range f.arr(p.Dict["Annots"]) {
		ad := f.dict(av)
		if ad == nil || f.name(ad["Subtype"]) != "Link" {
			continue
		}
		r := f.numArray(ad["Rect"])
		if len(r) < 4 {
			continue
		}
		l := PdfLink{Page: -1, DestY: -1}
		if ad["Dest"] != nil {
			l.Page, l.DestY = dest(ad["Dest"])
		}
		if a := f.dict(ad["A"]); a != nil {
			switch f.name(a["S"]) {
			case "GoTo":
				l.Page, l.DestY = dest(a["D"])
			case "URI":
				if s, ok := f.Resolve(a["URI"]).(pdfString); ok {
					l.URI = string(s)
				}
			}
		}
		if l.Page < 0 && l.URI == "" {
			continue
		}
		x0, y0 := p.ToView(r[0], r[1])
		x1, y1 := p.ToView(r[2], r[3])
		l.X, l.Y, l.W, l.H = math.Min(x0, x1), math.Min(y0, y1), math.Abs(x1-x0), math.Abs(y1-y0)
		out = append(out, l)
	}
	return out
}

// ToView — из координат PDF в пункты листа на экране (сверху вниз, с учётом поворота).
func (p *PdfPage) ToView(x, y float64) (float64, float64) {
	bx0, by0, bx1, by1 := p.Box[0], p.Box[1], p.Box[2], p.Box[3]
	switch p.Rotate {
	case 90:
		return y - by0, x - bx0
	case 180:
		return bx1 - x, y - by0
	case 270:
		return by1 - y, bx1 - x
	}
	return x - bx0, by1 - y
}

func (f *PdfFile) Outline(pages []*PdfPage) []PdfOutline {
	root := f.dict(f.trailer["Root"])
	var out []PdfOutline
	destOf := f.destResolver(pages)
	seen := map[pdfRef]bool{}
	var walk func(item any, lvl int)
	walk = func(item any, lvl int) {
		for n := 0; n < 5000; n++ {
			r, isRef := item.(pdfRef)
			if !isRef || seen[r] || lvl > 12 {
				return
			}
			seen[r] = true
			d := f.dict(item)
			if d == nil {
				return
			}
			title, _ := f.Resolve(d["Title"]).(pdfString)
			pg, y := destOf(d["Dest"])
			if a := f.dict(d["A"]); a != nil && pg < 0 && f.name(a["S"]) == "GoTo" {
				pg, y = destOf(a["D"])
			}
			out = append(out, PdfOutline{Title: strings.TrimSpace(textString(title)), Page: pg, Y: y, Level: lvl})
			if d["First"] != nil {
				walk(d["First"], lvl+1)
			}
			item = d["Next"]
		}
	}
	if ol := f.dict(root["Outlines"]); ol != nil {
		walk(ol["First"], 0)
	}
	return out
}

func (f *PdfFile) namedDests(root pdfDict) map[string]any {
	out := map[string]any{}
	if d := f.dict(root["Dests"]); d != nil {
		for k, v := range d {
			out[k] = v
		}
	}
	var tree func(n pdfDict, depth int)
	tree = func(n pdfDict, depth int) {
		if n == nil || depth > 20 {
			return
		}
		a := f.arr(n["Names"])
		for i := 0; i+1 < len(a); i += 2 {
			k, _ := f.Resolve(a[i]).(pdfString)
			out[string(k)] = a[i+1]
		}
		for _, k := range f.arr(n["Kids"]) {
			tree(f.dict(k), depth+1)
		}
	}
	if nm := f.dict(root["Names"]); nm != nil {
		tree(f.dict(nm["Dests"]), 0)
	}
	return out
}

// sortedInts — вспомогательное.
func sortedInts(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
