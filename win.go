//go:build windows

package main

// Окно Windows без чужих библиотек: только системные user32/gdi32/dwmapi через syscall.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

func init() { runtime.LockOSThread() }

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")

	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pInvalidateRect      = user32.NewProc("InvalidateRect")
	pBeginPaint          = user32.NewProc("BeginPaint")
	pEndPaint            = user32.NewProc("EndPaint")
	pLoadCursorW         = user32.NewProc("LoadCursorW")
	pSetCursor           = user32.NewProc("SetCursor")
	pSetTimer            = user32.NewProc("SetTimer")
	pKillTimer           = user32.NewProc("KillTimer")
	pTrackMouseEvent     = user32.NewProc("TrackMouseEvent")
	pSetCapture          = user32.NewProc("SetCapture")
	pReleaseCapture      = user32.NewProc("ReleaseCapture")
	pGetDpiForWindow     = user32.NewProc("GetDpiForWindow")
	pSetDpiAwarenessCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware  = user32.NewProc("SetProcessDPIAware")
	pIsZoomed            = user32.NewProc("IsZoomed")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pGetSysMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
	pSetWindowPos        = user32.NewProc("SetWindowPos")
	pGetKeyState         = user32.NewProc("GetKeyState")
	pOpenClipboard       = user32.NewProc("OpenClipboard")
	pEmptyClipboard      = user32.NewProc("EmptyClipboard")
	pSetClipboardData    = user32.NewProc("SetClipboardData")
	pCloseClipboard      = user32.NewProc("CloseClipboard")
	pMessageBoxW         = user32.NewProc("MessageBoxW")
	pCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	pSendMessageW        = user32.NewProc("SendMessageW")
	pScreenToClient      = user32.NewProc("ScreenToClient")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pGetClientRect       = user32.NewProc("GetClientRect")

	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pBitBlt             = gdi32.NewProc("BitBlt")
	pCreateFontW        = gdi32.NewProc("CreateFontW")
	pSetBkMode          = gdi32.NewProc("SetBkMode")
	pSetTextColor       = gdi32.NewProc("SetTextColor")
	pSetTextAlign       = gdi32.NewProc("SetTextAlign")
	pExtTextOutW        = gdi32.NewProc("ExtTextOutW")
	pGetTextExtentW     = gdi32.NewProc("GetTextExtentPoint32W")
	pGetTextExtentExW   = gdi32.NewProc("GetTextExtentExPointW")
	pGetTextMetricsW    = gdi32.NewProc("GetTextMetricsW")
	pGetFontData        = gdi32.NewProc("GetFontData")
	pGdiFlush           = gdi32.NewProc("GdiFlush")
	pGetTextFaceW       = gdi32.NewProc("GetTextFaceW")
	pCreateBitmap       = gdi32.NewProc("CreateBitmap")
	pSelectClipRgn      = gdi32.NewProc("SelectClipRgn")
	pIntersectClipRect  = gdi32.NewProc("IntersectClipRect")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	pGlobalLock       = kernel32.NewProc("GlobalLock")
	pGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	pRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")

	pDwmExtendFrame = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	pDwmSetAttr     = dwmapi.NewProc("DwmSetWindowAttribute")
	pRegGetValueW   = advapi32.NewProc("RegGetValueW")
	pRtlGetVersion  = ntdll.NewProc("RtlGetVersion")
)

func call(p *syscall.LazyProc, a ...uintptr) uintptr {
	if p.Find() != nil {
		return 0
	}
	r, _, _ := p.Call(a...)
	return r
}

func wstr(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

type rect32 struct{ L, T, R, B int32 }
type point32 struct{ X, Y int32 }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msgT struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point32
	Private uint32
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	RcPaint   rect32
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type bitmapInfo struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
	Colors        [4]byte
}

type textMetric struct {
	Height, Ascent, Descent, InternalLeading, ExternalLeading, AveCharWidth, MaxCharWidth int32
	Weight, Overhang, DigitizedAspectX, DigitizedAspectY                                  int32
	FirstChar, LastChar, DefaultChar, BreakChar                                           uint16
	Italic, Underlined, StruckOut, PitchAndFamily, CharSet                                byte
	_                                                                                     [3]byte
}

type nccalcParams struct {
	Rgrc [3]rect32
	Pos  uintptr
}

type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point32
}

type trackMouse struct {
	Size  uint32
	Flags uint32
	Hwnd  uintptr
	Hover uint32
	_     uint32
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

type osVersion struct {
	Size, Major, Minor, Build, Platform uint32
	CSD                                 [128]uint16
}

// ---------------------------------------------------------------- вывод текста через GDI
type fontKey struct {
	family          string
	px              int
	bold, it, under bool
}

type extKey struct {
	s, fam string
	px     float64
	b, i   bool
}

type gdiText struct {
	ext     map[extKey][]int32
	meas    map[measKey]float64
	dc      uintptr
	fonts   map[fontKey]uintptr
	metrics map[fontKey][3]float64 // asc, desc, внешний интерлиньяж
	cur     uintptr
	target  uintptr // HBITMAP, выбранный сейчас в dc
	winBmp  uintptr
	clipKey [4]int32
	clipOK  bool
}

func newGdiText() *gdiText {
	return &gdiText{dc: call(pCreateCompatibleDC, 0), fonts: map[fontKey]uintptr{}, metrics: map[fontKey][3]float64{}}
}

func (g *gdiText) key(f Font) fontKey {
	return fontKey{f.Family, int(f.Px*4 + 0.5), f.Bold, f.Italic, f.Underline}
}

func (g *gdiText) use(f Font) fontKey {
	k := g.key(f)
	h, ok := g.fonts[k]
	if !ok && len(g.fonts) > 300 {
		// при смене масштаба копятся шрифты всех размеров — чистим, чтобы не кончились объекты GDI
		for fk, fh := range g.fonts {
			if fh != g.cur {
				call(pDeleteObject, fh)
				delete(g.fonts, fk)
			}
		}
	}
	if !ok {
		w, it, un := uintptr(400), uintptr(0), uintptr(0)
		if f.Bold {
			w = 700
		}
		if f.Italic {
			it = 1
		}
		if f.Underline {
			un = 1
		}
		px := int32(-(f.Px + 0.5))
		h = call(pCreateFontW, uintptr(px), 0, 0, 0, w, it, un, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wstr(f.Family))))
		g.fonts[k] = h
	}
	if g.cur != h {
		call(pSelectObject, g.dc, h)
		g.cur = h
	}
	return k
}

func utf(s string) []uint16 { return utf16.Encode([]rune(s)) }

func (g *gdiText) Measure(s string, f Font) float64 {
	if s == "" {
		return 0
	}
	mk := measKey{s: s, k: g.key(f)}
	if v, ok := g.meas[mk]; ok {
		return v
	}
	g.use(f)
	u := utf(s)
	var sz point32
	call(pGetTextExtentW, g.dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&sz)))
	if g.meas == nil || len(g.meas) > 60000 {
		g.meas = map[measKey]float64{}
	}
	g.meas[mk] = float64(sz.X)
	return float64(sz.X)
}

type measKey struct {
	s string
	k fontKey
}

func (g *gdiText) Metrics(f Font) (float64, float64) {
	m := g.lineMetrics(f)
	return m[0], m[1]
}

// Leading — внешний интерлиньяж: добавка к высоте строки сверх asc+desc, как её считает Word.
func (g *gdiText) Leading(f Font) float64 { return g.lineMetrics(f)[2] }

// lineMetrics — метрики строки по правилам Word (см. sfntLineMetrics): обычно это метрики GDI
// (winAscent, winDescent, tmExternalLeading), а у шрифтов с флагом USE_TYPO_METRICS — типографские из OS/2.
func (g *gdiText) lineMetrics(f Font) [3]float64 {
	k := g.use(f)
	if m, ok := g.metrics[k]; ok {
		return m
	}
	var tm textMetric
	call(pGetTextMetricsW, g.dc, uintptr(unsafe.Pointer(&tm)))
	m := [3]float64{float64(tm.Ascent), float64(tm.Descent), float64(tm.ExternalLeading)}
	// таблицы шрифта: 'OS/2' и 'head' (имя таблицы — четыре байта в порядке памяти)
	var os2 [78]byte
	var head [54]byte
	const gdiError = 0xFFFFFFFF
	if n := uint32(call(pGetFontData, g.dc, 0x322F534F, 0, uintptr(unsafe.Pointer(&os2[0])), uintptr(len(os2)))); n != gdiError && n >= 78 && u16(os2[:], 62)&0x80 != 0 {
		if n := uint32(call(pGetFontData, g.dc, 0x64616568, 0, uintptr(unsafe.Pointer(&head[0])), uintptr(len(head)))); n != gdiError && n >= 54 {
			if upem := float64(u16(head[:], 18)); upem > 0 {
				em := math.Floor(f.Px + 0.5) // так шрифт и создаётся: высота кегля в пикселях
				asc, desc, gap := sfntLineMetrics(os2[:], nil)
				m = [3]float64{math.Round(asc * em / upem), math.Round(desc * em / upem), gap * em / upem}
			}
		}
	}
	g.metrics[k] = m
	return m
}

func (g *gdiText) Draw(cv *Canvas, s string, x, base float64, f Font, c Color) {
	g.draw(cv, s, x, base, f, c, 0)
}

// DrawScaled — вывод с шириной букв, как при вёрстке (эталонный размер refPx).
func (g *gdiText) DrawScaled(cv *Canvas, s string, x, base float64, f Font, c Color, refPx float64) {
	g.draw(cv, s, x, base, f, c, refPx)
}

func (g *gdiText) draw(cv *Canvas, s string, x, base float64, f Font, c Color, refPx float64) {
	if s == "" {
		return
	}
	if cv.Target != 0 && cv.Target != g.target {
		call(pSelectObject, g.dc, cv.Target)
		g.target = cv.Target
		g.clipOK = false
	}
	// системный вывод текста не знает про наш клип — передаём его в GDI
	c0 := cv.clip
	k := [4]int32{int32(math.Floor(c0.X)), int32(math.Floor(c0.Y)), int32(math.Ceil(c0.X + c0.W)), int32(math.Ceil(c0.Y + c0.H))}
	if k[2] <= k[0] || k[3] <= k[1] || base-f.Px*1.6 > float64(k[3]) || base+f.Px*0.8 < float64(k[1]) {
		return
	}
	if !g.clipOK || k != g.clipKey {
		call(pSelectClipRgn, g.dc, 0)
		call(pIntersectClipRect, g.dc, uintptr(k[0]), uintptr(k[1]), uintptr(k[2]), uintptr(k[3]))
		g.clipKey, g.clipOK = k, true
	}
	u := utf(s)
	var dx []int32
	if refPx > 0 && len(u) > 0 {
		ck := extKey{s: s, fam: f.Family, px: refPx, b: f.Bold, i: f.Italic}
		cum, ok := g.ext[ck]
		if !ok {
			ref := Font{Family: f.Family, Px: refPx, Bold: f.Bold, Italic: f.Italic}
			g.use(ref)
			cum = make([]int32, len(u))
			var sz point32
			call(pGetTextExtentExW, g.dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), 0x7fffffff, 0,
				uintptr(unsafe.Pointer(&cum[0])), uintptr(unsafe.Pointer(&sz)))
			if g.ext == nil || len(g.ext) > 40000 {
				g.ext = map[extKey][]int32{}
			}
			g.ext[ck] = cum
		}
		sc := f.Px / refPx
		dx = make([]int32, len(u))
		prev := math.Round(x)
		for i := range u {
			pos := math.Round(x + float64(cum[i])*sc)
			dx[i] = int32(pos - prev)
			prev = pos
		}
	}
	g.use(f)
	col := uint32(c)
	call(pSetTextColor, g.dc, uintptr((col>>16)&0xff|(col&0xff00)|(col&0xff)<<16))
	var pdx uintptr
	if len(dx) > 0 {
		pdx = uintptr(unsafe.Pointer(&dx[0]))
	}
	call(pExtTextOutW, g.dc, uintptr(int32(math.Round(x))), uintptr(int32(base+0.5)), 0, 0,
		uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), pdx)
}

func (g *gdiText) faceOf(family string) string {
	g.use(Font{Family: family, Px: 16})
	buf := make([]uint16, 64)
	call(pGetTextFaceW, g.dc, 64, uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

// ---------------------------------------------------------------- окно
type window struct {
	hwnd         uintptr
	app          *App
	tx           *gdiText
	bmp          uintptr
	cv           Canvas
	dpi          uint32
	tracking     bool
	animOn       bool
	hiSurr       uint16
	suppressChar bool
	title        string
	quitting     bool
}

var win window

func (w *window) invalidate() { call(pInvalidateRect, w.hwnd, 0, 0) }

func (w *window) ensureCanvas(cw, ch int) {
	if cw < 1 || ch < 1 || (cw == w.cv.W && ch == w.cv.H && w.bmp != 0) {
		return
	}
	bi := bitmapInfo{Size: 40, Width: int32(cw), Height: -int32(ch), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	hbm := call(pCreateDIBSection, w.tx.dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == nil {
		return
	}
	call(pSelectObject, w.tx.dc, hbm)
	w.tx.target = hbm
	w.tx.clipOK = false
	if w.bmp != 0 {
		call(pDeleteObject, w.bmp)
	}
	w.bmp = hbm
	w.tx.winBmp = hbm
	w.cv = Canvas{W: cw, H: ch, Pix: unsafe.Slice((*uint32)(bits), cw*ch), Target: hbm, Sync: func() { call(pGdiFlush) }}
}

// newPageCanvas — поверхность для печати и PDF: пиксели в DIB, чтобы системный текст рисовался прямо в них.
func newPageCanvas(cv *Canvas) {
	tx := win.tx
	bi := bitmapInfo{Size: 40, Width: int32(cv.W), Height: -int32(cv.H), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	hbm := call(pCreateDIBSection, tx.dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == nil {
		return
	}
	cv.Pix = unsafe.Slice((*uint32)(bits), cv.W*cv.H)
	cv.Target = hbm
	cv.Sync = func() { call(pGdiFlush) }
	cv.Release = func() {
		call(pGdiFlush)
		if tx.target == hbm {
			call(pSelectObject, tx.dc, tx.winBmp)
			tx.target = tx.winBmp
			tx.clipOK = false
		}
		cv.Pix = nil
		call(pDeleteObject, hbm)
	}
}

func (w *window) startAnim() {
	if !w.animOn {
		w.animOn = true
		call(pSetTimer, w.hwnd, 2, 15, 0)
	}
}

func (w *window) scale() float64 { return float64(w.dpi) / 96 }

func lo(v uintptr) int { return int(int16(v & 0xffff)) }
func hi(v uintptr) int { return int(int16((v >> 16) & 0xffff)) }

func (w *window) frameSize() int {
	f := call(pGetSysMetricsForDpi, 32, uintptr(w.dpi)) + call(pGetSysMetricsForDpi, 92, uintptr(w.dpi))
	if f == 0 {
		f = call(pGetSystemMetrics, 32) + call(pGetSystemMetrics, 92)
	}
	return int(f)
}

func keyDown(vk uintptr) bool { return int16(call(pGetKeyState, vk)) < 0 }

func mods() Mods { return Mods{Ctrl: keyDown(0x11), Shift: keyDown(0x10), Alt: keyDown(0x12)} }

func (w *window) after() {
	w.startAnim()
	w.invalidate()
	call(pSetTimer, w.hwnd, 1, 530, 0) // курсор снова видим сразу после ввода
}

func (w *window) proc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	a := w.app
	switch msg {
	case 0x0083: // WM_NCCALCSIZE — убираем системную рамку, оставляя тень и прилипание к краям
		if wp != 0 {
			if call(pIsZoomed, hwnd) != 0 {
				p := (*nccalcParams)(unsafe.Pointer(lp))
				f := int32(w.frameSize())
				p.Rgrc[0].L += f
				p.Rgrc[0].T += f
				p.Rgrc[0].R -= f
				p.Rgrc[0].B -= f
			}
			return 0
		}
	case 0x0084: // WM_NCHITTEST
		pt := point32{int32(lo(lp)), int32(hi(lp))}
		call(pScreenToClient, hwnd, uintptr(unsafe.Pointer(&pt)))
		x, y := int(pt.X), int(pt.Y)
		if w.cv.W == 0 {
			return 1
		}
		if call(pIsZoomed, hwnd) == 0 {
			b := int(6 * w.scale())
			l, r, t, bt := x < b, x >= w.cv.W-b, y < b, y >= w.cv.H-b
			switch {
			case t && l:
				return 13
			case t && r:
				return 14
			case bt && l:
				return 16
			case bt && r:
				return 17
			case l:
				return 10
			case r:
				return 11
			case t:
				return 12
			case bt:
				return 15
			}
		}
		if a.dialog == nil && a.IsCaption(float64(x), float64(y)) {
			return 2 // HTCAPTION
		}
		return 1
	case 0x00A0: // WM_NCMOUSEMOVE
		if a.hover != "" {
			a.MouseLeave()
			w.after()
		}
	case 0x0024: // WM_GETMINMAXINFO
		mm := (*minMaxInfo)(unsafe.Pointer(lp))
		mm.MinTrackSize = point32{int32(760 * w.scale()), int32(500 * w.scale())}
		return 0
	case 0x0005: // WM_SIZE
		a.Maximized = call(pIsZoomed, hwnd) != 0
		w.invalidate()
		return 0
	case 0x0014: // WM_ERASEBKGND
		return 1
	case 0x000F: // WM_PAINT
		var ps paintStruct
		hdc := call(pBeginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		cw, ch := w.clientSize()
		w.ensureCanvas(cw, ch)
		if w.cv.W > 0 {
			a.BaseS = w.scale()
			a.W, a.H = float64(w.cv.W), float64(w.cv.H)
			if a.Tick() {
				w.startAnim()
			}
			if w.tx.target != w.bmp {
				call(pSelectObject, w.tx.dc, w.bmp)
				w.tx.target = w.bmp
				w.tx.clipOK = false
			}
			a.Paint(&w.cv)
			call(pGdiFlush)
			if w.tx.target != w.bmp { // копируем на экран именно кадр окна, а не холст листа
				call(pSelectObject, w.tx.dc, w.bmp)
				w.tx.target = w.bmp
				w.tx.clipOK = false
			}
			call(pBitBlt, hdc, 0, 0, uintptr(w.cv.W), uintptr(w.cv.H), w.tx.dc, 0, 0, 0x00CC0020)
			if t := a.WindowTitle(); t != w.title {
				w.title = t
				call(pSetWindowTextW, hwnd, uintptr(unsafe.Pointer(wstr(t))))
			}
		}
		call(pEndPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case 0x0113: // WM_TIMER
		switch wp {
		case 1:
			a.CaretOn = !a.CaretOn
			w.invalidate()
		case 2:
			if !a.Tick() {
				call(pKillTimer, hwnd, 2)
				w.animOn = false
			}
			w.invalidate()
		case 3:
			a.Autosave()
		}
		return 0
	case 0x0020: // WM_SETCURSOR
		if lp&0xffff == 1 {
			var pt point32
			call(pGetCursorPos, uintptr(unsafe.Pointer(&pt)))
			call(pScreenToClient, hwnd, uintptr(unsafe.Pointer(&pt)))
			id := uintptr(32512)
			switch a.Cursor(float64(pt.X), float64(pt.Y)) {
			case 1:
				id = 32513
			case 2:
				id = 32649
			case 3:
				id = 32642
			}
			call(pSetCursor, call(pLoadCursorW, 0, id))
			return 1
		}
	case 0x0200: // WM_MOUSEMOVE
		if !w.tracking {
			tm := trackMouse{Size: uint32(unsafe.Sizeof(trackMouse{})), Flags: 2, Hwnd: hwnd}
			call(pTrackMouseEvent, uintptr(unsafe.Pointer(&tm)))
			w.tracking = true
		}
		before := a.hover
		a.MouseMove(float64(lo(lp)), float64(hi(lp)), wp&1 != 0)
		if a.hover != before || wp&1 != 0 || a.popup != nil {
			w.after()
		}
		return 0
	case 0x02A3: // WM_MOUSELEAVE
		w.tracking = false
		a.MouseLeave()
		w.after()
		return 0
	case 0x0201, 0x0204: // WM_LBUTTONDOWN / WM_RBUTTONDOWN
		call(pSetCapture, hwnd)
		a.MouseDown(float64(lo(lp)), float64(hi(lp)), mods(), msg == 0x0204)
		w.after()
		return 0
	case 0x0202, 0x0205: // WM_LBUTTONUP / WM_RBUTTONUP
		call(pReleaseCapture)
		a.MouseUp(float64(lo(lp)), float64(hi(lp)))
		w.after()
		return 0
	case 0x020A: // WM_MOUSEWHEEL
		m := mods()
		a.Wheel(float64(int16(wp>>16)), m)
		w.after()
		return 0
	case 0x020E: // WM_MOUSEHWHEEL
		a.Wheel(-float64(int16(wp>>16)), Mods{Shift: true})
		w.after()
		return 0
	case 0x0100, 0x0104: // WM_KEYDOWN / WM_SYSKEYDOWN
		m := mods()
		if msg == 0x0104 && (wp == 0x73 || wp == 0x12) && !m.Ctrl { // Alt+F4 и сам Alt — системе
			break
		}
		handled := a.KeyDown(int(wp), m)
		w.suppressChar = handled && (m.Ctrl || m.Alt)
		w.after()
		if handled || msg == 0x0100 {
			return 0
		}
	case 0x0102: // WM_CHAR
		if w.suppressChar {
			w.suppressChar = false
			return 0
		}
		c := uint16(wp)
		if c >= 0xD800 && c < 0xDC00 {
			w.hiSurr = c
			return 0
		}
		r := rune(c)
		if c >= 0xDC00 && c < 0xE000 && w.hiSurr != 0 {
			r = utf16.DecodeRune(rune(w.hiSurr), rune(c))
			w.hiSurr = 0
		}
		a.Char(r)
		w.after()
		return 0
	case 0x0106: // WM_SYSCHAR — без системного «бип» на Alt+буква
		return 0
	case 0x0233: // WM_DROPFILES
		n := call(pDragQueryFileW, wp, 0xFFFFFFFF, 0, 0)
		for i := uintptr(0); i < n; i++ {
			buf := make([]uint16, 1024)
			call(pDragQueryFileW, wp, i, uintptr(unsafe.Pointer(&buf[0])), 1024)
			p := syscall.UTF16ToString(buf)
			switch strings.ToLower(filepath.Ext(p)) {
			case ".png", ".jpg", ".jpeg", ".gif":
				a.InsertImageFile(p)
			default:
				if i == 0 {
					a.withSaved(func() { a.OpenPath(p) })
				}
			}
		}
		call(pDragFinish, wp)
		w.after()
		return 0
	case 0x02E0: // WM_DPICHANGED
		w.dpi = uint32(wp & 0xffff)
		r := (*rect32)(unsafe.Pointer(lp))
		call(pSetWindowPos, hwnd, 0, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), 0x14)
		w.invalidate()
		return 0
	case 0x001A: // WM_SETTINGCHANGE — пользователь сменил тему Windows
		a.applyTheme()
		w.invalidate()
	case 0x0006, 0x0007, 0x0008: // активация, фокус
		a.CaretOn = true
		w.invalidate()
	case 0x0011: // WM_QUERYENDSESSION
		a.AutosaveAll()
		return 1
	case 0x0010: // WM_CLOSE
		if w.quitting {
			break
		}
		a.RequestClose()
		w.after()
		return 0
	case 0x0002: // WM_DESTROY
		call(pPostQuitMessage, 0)
		return 0
	}
	return call(pDefWindowProcW, hwnd, uintptr(msg), wp, lp)
}

func (w *window) clientSize() (int, int) {
	var rc rect32
	call(pGetClientRect, w.hwnd, uintptr(unsafe.Pointer(&rc)))
	return int(rc.R - rc.L), int(rc.B - rc.T)
}

func systemDark() bool {
	var v, size uint32 = 1, 4
	r := call(pRegGetValueW, 0x80000001, uintptr(unsafe.Pointer(wstr(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`))),
		uintptr(unsafe.Pointer(wstr("AppsUseLightTheme"))), 0x10, 0, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size)))
	return r == 0 && v == 0
}

// makeIcon — HICON из нашего значка (если ресурс exe недоступен).
func makeIcon(size int) uintptr {
	src := AppIcon(size)
	bi := bitmapInfo{Size: 40, Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	hbm := call(pCreateDIBSection, 0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 {
		return 0
	}
	copy(unsafe.Slice((*uint32)(bits), size*size), src)
	hmask := call(pCreateBitmap, uintptr(size), uintptr(size), 1, 1, 0)
	ii := iconInfo{FIcon: 1, HbmMask: hmask, HbmColor: hbm}
	ic := call(pCreateIconIndirect, uintptr(unsafe.Pointer(&ii)))
	// CreateIconIndirect делает свои копии: наши битмапы надо освободить
	call(pDeleteObject, hbm)
	if hmask != 0 {
		call(pDeleteObject, hmask)
	}
	return ic
}

func crashLog(v any) {
	path := filepath.Join(os.TempDir(), "Texora-crash.txt")
	msg := fmt.Sprintf("%v\n\n%s", v, debug.Stack())
	_ = os.WriteFile(path, []byte(msg), 0o644)
	if win.app != nil {
		func() {
			defer func() { recover() }()
			win.app.AutosaveAll()
		}()
	}
	call(pMessageBoxW, 0, uintptr(unsafe.Pointer(wstr("В Texora произошла ошибка, программа будет закрыта:\n"+fmt.Sprint(v)+
		"\n\nНесохранённый документ можно будет восстановить при следующем запуске.\nПодробности: "+path))),
		uintptr(unsafe.Pointer(wstr("Texora"))), 0x10)
	os.Exit(1)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			crashLog(r)
		}
	}()
	args := os.Args[1:]
	if platformPreRun(args) {
		return
	}
	if call(pSetDpiAwarenessCtx, ^uintptr(3)) == 0 { // PER_MONITOR_AWARE_V2 = -4
		call(pSetProcessDPIAware)
	}
	w := &win
	w.tx = newGdiText()
	call(pSetBkMode, w.tx.dc, 1)     // прозрачный фон текста
	call(pSetTextAlign, w.tx.dc, 24) // y — базовая линия
	plat := &winPlat{w: w}
	PrepareCanvas = newPageCanvas
	w.app = NewApp(w.tx, plat)
	a := w.app
	if a.applyPendingUpdate() { // обновление, отложенное в прошлый раз: новая версия уже запущена
		return
	}
	cleanupOldExe()
	if f := w.tx.faceOf("Segoe UI Variable Text"); f == "Segoe UI Variable Text" {
		a.UIFont = "Segoe UI Variable Text"
	}
	if f := w.tx.faceOf("Cascadia Mono"); f == "Cascadia Mono" { // Windows 11; иначе Consolas
		a.MonoFont = "Cascadia Mono"
	}
	a.SysFonts = plat.Fonts()
	a.applyTheme()

	inst := call(pGetModuleHandleW, 0)
	iconBig := call(pLoadImageW, inst, 1, 1, 0, 0, 0x40)  // ресурс 1, IMAGE_ICON, размер по умолчанию
	iconSmall := call(pLoadImageW, inst, 1, 1, 16, 16, 0) // маленький
	if iconBig == 0 {
		iconBig = makeIcon(32)
	}
	if iconSmall == 0 {
		iconSmall = makeIcon(16)
	}
	wc := wndClassEx{Style: 0x3, Instance: inst, Cursor: call(pLoadCursorW, 0, 32512), ClassName: wstr("TexoraWindow"),
		Icon: iconBig, IconSm: iconSmall,
		WndProc: syscall.NewCallback(func(h uintptr, m uint32, wp, lp uintptr) (ret uintptr) {
			defer func() {
				if r := recover(); r != nil {
					crashLog(r)
				}
			}()
			if w.hwnd == 0 {
				w.hwnd = h
			}
			return w.proc(h, m, wp, lp)
		})}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if call(pRegisterClassExW, uintptr(unsafe.Pointer(&wc))) == 0 {
		crashLog("RegisterClassEx не сработал")
	}
	const style = 0x00CF0000 | 0x02000000 // WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN
	cw := uintptr(0x80000000)
	hwnd := call(pCreateWindowExW, 0x40000|0x10, uintptr(unsafe.Pointer(wc.ClassName)), uintptr(unsafe.Pointer(wstr("Texora"))),
		style, cw, cw, 1280, 860, 0, 0, inst, 0) // WS_EX_APPWINDOW | WS_EX_ACCEPTFILES
	if hwnd == 0 {
		crashLog("Не удалось создать окно")
	}
	w.hwnd = hwnd
	w.dpi = uint32(call(pGetDpiForWindow, hwnd))
	if w.dpi == 0 {
		w.dpi = 96
	}
	s := w.scale()
	// окно — по размеру рабочей области экрана, но не больше разумного
	var wa rect32
	call(pSystemParametersInfoW, 0x30, 0, uintptr(unsafe.Pointer(&wa)), 0) // SPI_GETWORKAREA
	ww, wh := int32(1320*s), int32(900*s)
	if aw := wa.R - wa.L; aw > 0 && ww > aw-int32(40*s) {
		ww = aw - int32(40*s)
	}
	if ah := wa.B - wa.T; ah > 0 && wh > ah-int32(40*s) {
		wh = ah - int32(40*s)
	}
	x, y := wa.L+(wa.R-wa.L-ww)/2, wa.T+(wa.B-wa.T-wh)/2
	call(pSetWindowPos, hwnd, 0, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0x14|0x20)
	margins := [4]int32{0, 0, 1, 0}
	call(pDwmExtendFrame, hwnd, uintptr(unsafe.Pointer(&margins)))
	round := uint32(2)
	call(pDwmSetAttr, hwnd, 33, uintptr(unsafe.Pointer(&round)), 4) // скруглённые углы (Windows 11)
	call(pDragAcceptFiles, hwnd, 1)

	var ov osVersion
	ov.Size = uint32(unsafe.Sizeof(ov))
	call(pRtlGetVersion, uintptr(unsafe.Pointer(&ov)))
	a.Diag = fmt.Sprintf("Windows %d.%d.%d · %d dpi · шрифтов в системе: %d", ov.Major, ov.Minor, ov.Build, w.dpi, len(a.SysFonts))

	opened := false
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			if _, err := os.Stat(arg); err == nil {
				a.OpenPath(arg)
				opened = true
				break
			}
		}
	}
	if !opened {
		a.OfferRecovery()
	}
	a.autoCheckUpdates()

	call(pShowWindow, hwnd, 1)
	call(pSetTimer, hwnd, 1, 530, 0)
	call(pSetTimer, hwnd, 3, 60000, 0)
	var m msgT
	for {
		r := call(pGetMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		call(pTranslateMessage, uintptr(unsafe.Pointer(&m)))
		call(pDispatchMessageW, uintptr(unsafe.Pointer(&m)))
	}
}
