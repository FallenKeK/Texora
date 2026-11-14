//go:build windows

package main

// Системные службы Windows для Texora: диалоги файлов, буфер обмена, печать, шрифты, ссылки.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
	pGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")
	pPrintDlgW        = comdlg32.NewProc("PrintDlgW")
	pCommDlgExtError  = comdlg32.NewProc("CommDlgExtendedError")

	pShellExecuteW   = shell32.NewProc("ShellExecuteW")
	pDragQueryFileW  = shell32.NewProc("DragQueryFileW")
	pDragFinish      = shell32.NewProc("DragFinish")
	pDragAcceptFiles = shell32.NewProc("DragAcceptFiles")

	pSetWindowTextW        = user32.NewProc("SetWindowTextW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pLoadImageW            = user32.NewProc("LoadImageW")
	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pGetClipboardData      = user32.NewProc("GetClipboardData")
	pRegisterClipboardFmt  = user32.NewProc("RegisterClipboardFormatW")
	pIsClipboardFmtAvail   = user32.NewProc("IsClipboardFormatAvailable")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")

	pGlobalSize         = kernel32.NewProc("GlobalSize")
	pGlobalFree         = kernel32.NewProc("GlobalFree")
	pEnumFontFamiliesEx = gdi32.NewProc("EnumFontFamiliesExW")
	pStartDocW          = gdi32.NewProc("StartDocW")
	pEndDoc             = gdi32.NewProc("EndDoc")
	pAbortDoc           = gdi32.NewProc("AbortDoc")
	pStartPage          = gdi32.NewProc("StartPage")
	pEndPage            = gdi32.NewProc("EndPage")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
	pGetDeviceCaps      = gdi32.NewProc("GetDeviceCaps")
	pStretchDIBits      = gdi32.NewProc("StretchDIBits")
	pSetStretchBltMode  = gdi32.NewProc("SetStretchBltMode")
)

type winPlat struct{ w *window }

func (p *winPlat) Invalidate() { p.w.invalidate() }
func (p *winPlat) Minimize()   { call(pShowWindow, p.w.hwnd, 6) }
func (p *winPlat) Close()      { call(pPostMessageW, p.w.hwnd, 0x10, 0, 0) }
func (p *winPlat) SetTitle(s string) {
	call(pSetWindowTextW, p.w.hwnd, uintptr(unsafe.Pointer(wstr(s))))
}
func (p *winPlat) SystemDark() bool { return systemDark() }
func (p *winPlat) ToggleMaximize() {
	if call(pIsZoomed, p.w.hwnd) != 0 {
		call(pShowWindow, p.w.hwnd, 9)
	} else {
		call(pShowWindow, p.w.hwnd, 3)
	}
}
func (p *winPlat) Quit() {
	p.w.quitting = true
	call(pDestroyWindow, p.w.hwnd)
}
func (p *winPlat) OpenURL(u string) {
	low := strings.ToLower(u)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") && !strings.HasPrefix(low, "mailto:") {
		return // не запускаем произвольные файлы и команды из документа
	}
	call(pShellExecuteW, p.w.hwnd, uintptr(unsafe.Pointer(wstr("open"))), uintptr(unsafe.Pointer(wstr(u))), 0, 0, 1)
}

// ---------------------------------------------------------------- диалоги файлов
type openFileName struct {
	StructSize    uint32
	Owner         uintptr
	Instance      uintptr
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	Hook          uintptr
	TemplateName  *uint16
	Reserved      uintptr
	Reserved2     uint32
	FlagsEx       uint32
}

func filterStr(pairs ...string) *uint16 {
	u := utf16.Encode([]rune(strings.Join(pairs, "\x00") + "\x00\x00"))
	return &u[0]
}

func (p *winPlat) OpenDialog(kind string) string {
	buf := make([]uint16, 4096)
	var filter *uint16
	title := "Открыть документ"
	if kind == "image" {
		title = "Вставка рисунка"
		filter = filterStr("Рисунки (PNG, JPEG, GIF)", "*.png;*.jpg;*.jpeg;*.gif", "Все файлы", "*.*")
	} else if kind == "bib" {
		title = "Импорт литературы"
		filter = filterStr("BibTeX и RIS (*.bib, *.ris)", "*.bib;*.ris;*.txt", "Все файлы", "*.*")
	} else {
		filter = filterStr("Все документы", "*.docx;*.doc;*.odt;*.rtf;*.txt;*.htm;*.html;*.pdf;*.pptx",
			"Документ Word (*.docx)", "*.docx", "PDF — только чтение (*.pdf)", "*.pdf", "Презентация PowerPoint — просмотр (*.pptx)", "*.pptx", "Документ Word 97–2003 (*.doc)", "*.doc", "OpenDocument (*.odt)", "*.odt",
			"Текст в формате RTF (*.rtf)", "*.rtf", "Обычный текст (*.txt)", "*.txt", "Все файлы", "*.*")
	}
	of := openFileName{Owner: p.w.hwnd, Filter: filter, FilterIndex: 1, File: &buf[0], MaxFile: uint32(len(buf)),
		Title: wstr(title), Flags: 0x00001000 | 0x00000800 | 0x00080000 | 0x00000004} // FILEMUSTEXIST|PATHMUSTEXIST|EXPLORER|HIDEREADONLY
	of.StructSize = uint32(unsafe.Sizeof(of))
	if call(pGetOpenFileNameW, uintptr(unsafe.Pointer(&of))) == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func (p *winPlat) SaveDialog(name, ext string) (string, string) {
	buf := make([]uint16, 4096)
	copy(buf, utf16.Encode([]rune(name)))
	var pairs []string
	idx := uint32(1)
	formats := SaveFormats
	if desc, ok := otherSaveKinds[ext]; ok { // не документ (литература): только свой тип
		formats = []struct{ Ext, Name string }{{ext, desc}}
	}
	for i, f := range formats {
		pairs = append(pairs, f.Name, "*."+f.Ext)
		if f.Ext == ext {
			idx = uint32(i + 1)
		}
	}
	of := openFileName{Owner: p.w.hwnd, Filter: filterStr(pairs...), FilterIndex: idx, File: &buf[0], MaxFile: uint32(len(buf)),
		Title: wstr("Сохранить как"), DefExt: wstr(ext), Flags: 0x00000002 | 0x00000800 | 0x00080000 | 0x00000004} // OVERWRITEPROMPT
	of.StructSize = uint32(unsafe.Sizeof(of))
	if call(pGetSaveFileNameW, uintptr(unsafe.Pointer(&of))) == 0 {
		return "", ""
	}
	chosen := ext
	if i := int(of.FilterIndex) - 1; i >= 0 && i < len(formats) {
		chosen = formats[i].Ext
	}
	path := syscall.UTF16ToString(buf)
	// пользователь сменил тип файла, а расширение осталось старым — поправим
	if e := extOf(path); e != chosen {
		known := false
		for _, f := range formats {
			if f.Ext == e {
				known = true
			}
		}
		if known {
			path = path[:len(path)-len(e)] + chosen
		} else {
			path += "." + chosen
		}
	}
	return path, chosen
}

// ---------------------------------------------------------------- буфер обмена
var cfRTF, cfHTML, cfInternal, cfPNG uintptr

func clipFormats() {
	if cfRTF == 0 {
		cfRTF = call(pRegisterClipboardFmt, uintptr(unsafe.Pointer(wstr("Rich Text Format"))))
		cfHTML = call(pRegisterClipboardFmt, uintptr(unsafe.Pointer(wstr("HTML Format"))))
		cfInternal = call(pRegisterClipboardFmt, uintptr(unsafe.Pointer(wstr("Texora.Blocks"))))
		cfPNG = call(pRegisterClipboardFmt, uintptr(unsafe.Pointer(wstr("PNG"))))
	}
}

func globalBytes(b []byte) uintptr {
	h := call(pGlobalAlloc, 2, uintptr(len(b)+1))
	ptr := call(pGlobalLock, h)
	if ptr == 0 {
		return 0
	}
	if len(b) > 0 {
		call(pRtlMoveMemory, ptr, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	}
	*(*byte)(unsafe.Pointer(ptr + uintptr(len(b)))) = 0
	call(pGlobalUnlock, h)
	return h
}

func (p *winPlat) SetClipboard(c ClipData) {
	clipFormats()
	if call(pOpenClipboard, p.w.hwnd) == 0 {
		return
	}
	call(pEmptyClipboard)
	if c.Text != "" {
		u := utf16.Encode([]rune(strings.ReplaceAll(c.Text, "\n", "\r\n") + "\x00"))
		b := unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*2)
		call(pSetClipboardData, 13, globalBytes(b))
	}
	if c.RTF != "" {
		call(pSetClipboardData, cfRTF, globalBytes([]byte(c.RTF)))
	}
	if c.HTML != "" {
		call(pSetClipboardData, cfHTML, globalBytes([]byte(c.HTML)))
	}
	if len(c.Internal) > 0 {
		var hdr [8]byte
		binary.LittleEndian.PutUint64(hdr[:], uint64(len(c.Internal)))
		call(pSetClipboardData, cfInternal, globalBytes(append(hdr[:], c.Internal...)))
	}
	call(pCloseClipboard)
}

func clipRaw(f uintptr) []byte {
	h := call(pGetClipboardData, f)
	if h == 0 {
		return nil
	}
	n := call(pGlobalSize, h)
	ptr := call(pGlobalLock, h)
	if ptr == 0 || n == 0 {
		return nil
	}
	out := make([]byte, n)
	copy(out, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n))
	call(pGlobalUnlock, h)
	return out
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func (p *winPlat) GetClipboard() ClipData {
	clipFormats()
	var c ClipData
	if call(pOpenClipboard, p.w.hwnd) == 0 {
		return c
	}
	defer call(pCloseClipboard)
	if b := clipRaw(cfInternal); len(b) > 8 {
		n := binary.LittleEndian.Uint64(b[:8])
		if int(n) <= len(b)-8 {
			c.Internal = b[8 : 8+n]
		}
	}
	if b := clipRaw(cfRTF); b != nil {
		c.RTF = cstr(b)
	}
	if b := clipRaw(cfHTML); b != nil {
		c.HTML = cstr(b)
	}
	if b := clipRaw(13); len(b) >= 2 {
		u := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
		c.Text = strings.ReplaceAll(syscall.UTF16ToString(u), "\r\n", "\n")
	}
	if b := clipRaw(cfPNG); len(b) > 8 {
		c.Image, c.ImageMime = b, "image/png"
	} else if b := clipRaw(8); len(b) > 40 { // CF_DIB
		if png := dibToPNG(b); png != nil {
			c.Image, c.ImageMime = png, "image/png"
		}
	}
	return c
}

func dibToPNG(b []byte) []byte {
	hs := int(binary.LittleEndian.Uint32(b[0:]))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:])))
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	comp := binary.LittleEndian.Uint32(b[16:])
	used := int(binary.LittleEndian.Uint32(b[32:]))
	if w <= 0 || h == 0 || (bpp != 24 && bpp != 32) || w > 20000 {
		return nil
	}
	off := hs
	if comp == 3 && hs == 40 {
		off += 12
	}
	off += used * 4
	topDown := h < 0
	if topDown {
		h = -h
	}
	stride := (w*bpp/8 + 3) &^ 3
	if off+stride*h > len(b) {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	allZeroA := true
	for y := 0; y < h; y++ {
		sy := h - 1 - y
		if topDown {
			sy = y
		}
		row := b[off+sy*stride:]
		for x := 0; x < w; x++ {
			k := x * bpp / 8
			a := uint8(255)
			if bpp == 32 {
				a = row[k+3]
				if a != 0 {
					allZeroA = false
				}
			}
			img.Pix[y*img.Stride+x*4+0] = row[k+2]
			img.Pix[y*img.Stride+x*4+1] = row[k+1]
			img.Pix[y*img.Stride+x*4+2] = row[k]
			img.Pix[y*img.Stride+x*4+3] = a
		}
	}
	if bpp == 32 && allZeroA {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	var out bytes.Buffer
	if png.Encode(&out, img) != nil {
		return nil
	}
	return out.Bytes()
}

// ---------------------------------------------------------------- шрифты
type logFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          byte
	OutPrecision, ClipPrecision, Quality, Pitch    byte
	FaceName                                       [32]uint16
}

var fontSet map[string]bool

var enumCb = syscall.NewCallback(func(lf *logFont, tm uintptr, ftype uint32, lp uintptr) uintptr {
	name := syscall.UTF16ToString(lf.FaceName[:])
	if name != "" && !strings.HasPrefix(name, "@") {
		fontSet[name] = true
	}
	return 1
})

func (p *winPlat) Fonts() []string {
	fontSet = map[string]bool{}
	dc := call(pGetDC, 0)
	lf := logFont{CharSet: 1}
	call(pEnumFontFamiliesEx, dc, uintptr(unsafe.Pointer(&lf)), enumCb, 0, 0)
	call(pReleaseDC, 0, dc)
	var out []string
	for k := range fontSet {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// ---------------------------------------------------------------- печать
type printDlg struct {
	StructSize        uint32
	Owner             uintptr
	DevMode           uintptr
	DevNames          uintptr
	DC                uintptr
	Flags             uint32
	FromPage, ToPage  uint16
	MinPage, MaxPage  uint16
	Copies            uint16
	Instance          uintptr
	CustData          uintptr
	PrintHook         uintptr
	SetupHook         uintptr
	PrintTemplateName *uint16
	SetupTemplateName *uint16
	PrintTemplate     uintptr
	SetupTemplate     uintptr
}

type docInfo struct {
	Size     int32
	DocName  *uint16
	Output   *uint16
	Datatype *uint16
	Type     uint32
}

func (p *winPlat) Print(pages int, render func(page int, dpi float64) *image.RGBA, wPt, hPt float64) error {
	pd := printDlg{Owner: p.w.hwnd, Flags: 0x100 | 0x4 | 0x40000, FromPage: 1, ToPage: uint16(pages), MinPage: 1,
		MaxPage: uint16(pages), Copies: 1} // RETURNDC | NOSELECTION | USEDEVMODECOPIESANDCOLLATE
	pd.StructSize = uint32(unsafe.Sizeof(pd))
	if call(pPrintDlgW, uintptr(unsafe.Pointer(&pd))) == 0 {
		if e := call(pCommDlgExtError); e != 0 {
			return fmt.Errorf("не удалось открыть окно печати (код %#x). Проверьте, что в Windows установлен принтер", e)
		}
		return nil
	}
	if pd.DevMode != 0 {
		defer call(pGlobalFree, pd.DevMode)
	}
	if pd.DevNames != 0 {
		defer call(pGlobalFree, pd.DevNames)
	}
	dc := pd.DC
	if dc == 0 {
		return fmt.Errorf("принтер не ответил")
	}
	defer call(pDeleteDC, dc)
	from, to := 1, pages
	if pd.Flags&0x2 != 0 { // PD_PAGENUMS
		from, to = int(pd.FromPage), int(pd.ToPage)
	}
	dpiX := float64(call(pGetDeviceCaps, dc, 88))
	dpiY := float64(call(pGetDeviceCaps, dc, 90))
	offX := float64(call(pGetDeviceCaps, dc, 112))
	offY := float64(call(pGetDeviceCaps, dc, 113))
	if dpiX <= 0 || dpiY <= 0 {
		return fmt.Errorf("принтер сообщил неверное разрешение")
	}
	di := docInfo{DocName: wstr(p.w.app.FileName)}
	di.Size = int32(unsafe.Sizeof(di))
	if int32(call(pStartDocW, dc, uintptr(unsafe.Pointer(&di)))) <= 0 {
		return fmt.Errorf("не удалось начать печать")
	}
	call(pSetStretchBltMode, dc, 4)
	rdpi := dpiX
	if rdpi > 300 {
		rdpi = 300
	}
	for i := from - 1; i < to && i < pages; i++ {
		if int32(call(pStartPage, dc)) <= 0 {
			call(pAbortDoc, dc)
			return fmt.Errorf("печать прервана")
		}
		img := render(i, rdpi)
		// альбомная страница на книжном листе (или наоборот) — поворачиваем
		physW := float64(call(pGetDeviceCaps, dc, 110))
		physH := float64(call(pGetDeviceCaps, dc, 111))
		if (img.Rect.Dx() > img.Rect.Dy()) != (physW > physH) && physW > 0 && physH > 0 {
			img = rotate90(img)
		}
		w, h := img.Rect.Dx(), img.Rect.Dy()
		bgra := make([]byte, len(img.Pix))
		for k := 0; k < len(img.Pix); k += 4 {
			bgra[k], bgra[k+1], bgra[k+2], bgra[k+3] = img.Pix[k+2], img.Pix[k+1], img.Pix[k], 255
		}
		bi := bitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
		dw, dh := int32(float64(w)/rdpi*dpiX+0.5), int32(float64(h)/rdpi*dpiY+0.5)
		call(pStretchDIBits, dc, uintptr(int32(-offX)), uintptr(int32(-offY)), uintptr(dw), uintptr(dh), 0, 0, uintptr(w), uintptr(h),
			uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&bi)), 0, 0x00CC0020)
		call(pEndPage, dc)
	}
	call(pEndDoc, dc)
	return nil
}
