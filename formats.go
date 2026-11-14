package main

// Открытие и сохранение во всех форматах.

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// otherSaveKinds — файлы, которые сохраняются не как документ (диалог показывает только их тип).
var otherSaveKinds = map[string]string{"bib": "BibTeX (*.bib)", "ris": "RIS (*.ris)"}

var SaveFormats = []struct{ Ext, Name string }{
	{"docx", "Документ Word (*.docx)"}, {"odt", "Документ OpenDocument (*.odt)"}, {"rtf", "Текст в формате RTF (*.rtf)"},
	{"txt", "Обычный текст (*.txt)"}, {"pdf", "PDF (*.pdf)"},
}

func extOf(name string) string { return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) }

func decodeText(b []byte) string {
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		u := make([]uint16, (len(b)-2)/2)
		for i := range u {
			u[i] = uint16(b[2+i*2]) | uint16(b[3+i*2])<<8
		}
		return string(utf16.Decode(u))
	}
	if len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff {
		u := make([]uint16, (len(b)-2)/2)
		for i := range u {
			u[i] = uint16(b[3+i*2]) | uint16(b[2+i*2])<<8
		}
		return string(utf16.Decode(u))
	}
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if utf8.Valid(b) {
		return string(b)
	}
	return decodeCP1251(b)
}

var cp1251 = []rune("ЂЃ‚ѓ„…†‡€‰Љ‹ЊЌЋЏђ‘’“”•–—�™љ›њќћџ ЎўЈ¤Ґ¦§Ё©Є«¬­®Ї°±Ііґµ¶·ё№є»јЅѕї")

func decodeCP1251(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c < 0x80:
			sb.WriteByte(c)
		case c >= 0xC0:
			sb.WriteRune(rune(0x410 + int(c) - 0xC0))
		default:
			sb.WriteRune(cp1251[c-0x80])
		}
	}
	return sb.String()
}

func OpenBytes(name string, data []byte) (*Doc, error) {
	ext := extOf(name)
	isZip := len(data) > 4 && data[0] == 'P' && data[1] == 'K'
	isCfb := len(data) > 8 && bytes.Equal(data[:4], []byte{0xd0, 0xcf, 0x11, 0xe0})
	isRtf := bytes.HasPrefix(bytes.TrimLeft(data, " \r\n\t"), []byte(`{\rtf`))
	var d *Doc
	var err error
	switch {
	case isRtf:
		d, err = ReadRTF(data)
	case isCfb:
		d, err = ReadDoc(data)
	case isZip && (ext == "odt" || ext == "ott"):
		d, err = ReadODT(data)
	case isZip:
		d, err = ReadDocx(data)
		if err != nil && ext != "docx" {
			d, err = ReadODT(data)
		}
	default:
		text := decodeText(data)
		d = NewDoc()
		if ext == "html" || ext == "htm" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "<!doctype html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "<html") {
			bs, media := HTMLToBlocks(text)
			d.Body = bs
			for k, v := range media {
				d.Media[k] = v
			}
		} else {
			d.Body = PlainToBlocks(text)
			d.Def.After, d.Def.Line = 0, 1
		}
	}
	if err != nil {
		return nil, err
	}
	if len(d.Body) == 0 || d.Body[len(d.Body)-1].P == nil {
		d.Body = append(d.Body, Block{P: NewPara()})
	}
	return d, nil
}

func SaveBytes(d *Doc, ext string) ([]byte, error) {
	switch ext {
	case "docx":
		return WriteDocx(d)
	case "odt":
		return WriteODT(d)
	case "rtf":
		return WriteRTF(d), nil
	case "txt":
		t := strings.ReplaceAll(BlocksToText(d.Body), "\n", "\r\n")
		return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(t, "￼", ""))...), nil
	}
	return nil, fmt.Errorf("сохранение в .%s не поддерживается", ext)
}
