package gui

import (
	"syscall"
	"unsafe"
)

// Win32, GDI and DWM entry points.
//
// Only the handful of calls the scanner window needs are declared. Keeping
// them here (instead of pulling in a UI toolkit) keeps the executable small
// and dependency free, and lets the window be painted exactly as designed.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	msimg32  = syscall.NewLazyDLL("msimg32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
)

var (
	procRegisterClassExW          = user32.NewProc("RegisterClassExW")
	procCreateWindowExW           = user32.NewProc("CreateWindowExW")
	procDefWindowProcW            = user32.NewProc("DefWindowProcW")
	procDestroyWindow             = user32.NewProc("DestroyWindow")
	procShowWindow                = user32.NewProc("ShowWindow")
	procUpdateWindow              = user32.NewProc("UpdateWindow")
	procGetMessageW               = user32.NewProc("GetMessageW")
	procTranslateMessage          = user32.NewProc("TranslateMessage")
	procDispatchMessageW          = user32.NewProc("DispatchMessageW")
	procPostQuitMessage           = user32.NewProc("PostQuitMessage")
	procPostMessageW              = user32.NewProc("PostMessageW")
	procSendMessageW              = user32.NewProc("SendMessageW")
	procInvalidateRect            = user32.NewProc("InvalidateRect")
	procBeginPaint                = user32.NewProc("BeginPaint")
	procEndPaint                  = user32.NewProc("EndPaint")
	procGetClientRect             = user32.NewProc("GetClientRect")
	procGetDC                     = user32.NewProc("GetDC")
	procReleaseDC                 = user32.NewProc("ReleaseDC")
	procSetTimer                  = user32.NewProc("SetTimer")
	procKillTimer                 = user32.NewProc("KillTimer")
	procLoadCursorW               = user32.NewProc("LoadCursorW")
	procSetCursor                 = user32.NewProc("SetCursor")
	procSetFocus                  = user32.NewProc("SetFocus")
	procGetSystemMetrics          = user32.NewProc("GetSystemMetrics")
	procSetProcessDpiAwareness    = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForWindow           = user32.NewProc("GetDpiForWindow")
	procDwmSetWindowAttribute     = dwmapi.NewProc("DwmSetWindowAttribute")
	procOpenClipboard             = user32.NewProc("OpenClipboard")
	procCloseClipboard            = user32.NewProc("CloseClipboard")
	procGetClipboardData          = user32.NewProc("GetClipboardData")
	procEmptyClipboard            = user32.NewProc("EmptyClipboard")
	procSetClipboardData          = user32.NewProc("SetClipboardData")
	procGlobalAlloc               = kernel32.NewProc("GlobalAlloc")
	procGlobalLock                = kernel32.NewProc("GlobalLock")
	procGlobalUnlock              = kernel32.NewProc("GlobalUnlock")
	procGlobalFree                = kernel32.NewProc("GlobalFree")
	procSetWindowTextW            = user32.NewProc("SetWindowTextW")
	procGetWindowRect             = user32.NewProc("GetWindowRect")
	procDrawTextW                 = user32.NewProc("DrawTextW")
	procSetBkMode                 = gdi32.NewProc("SetBkMode")
	procSetTextColor              = gdi32.NewProc("SetTextColor")
	procCreateFontW               = gdi32.NewProc("CreateFontW")
	procCreateSolidBrush          = gdi32.NewProc("CreateSolidBrush")
	procCreatePen                 = gdi32.NewProc("CreatePen")
	procSelectObject              = gdi32.NewProc("SelectObject")
	procDeleteObject              = gdi32.NewProc("DeleteObject")
	procDeleteDC                  = gdi32.NewProc("DeleteDC")
	procCreateCompatibleDC        = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection          = gdi32.NewProc("CreateDIBSection")
	procRoundRect                 = gdi32.NewProc("RoundRect")
	procEllipse                   = gdi32.NewProc("Ellipse")
	procArc                       = gdi32.NewProc("Arc")
	procMoveToEx                  = gdi32.NewProc("MoveToEx")
	procLineTo                    = gdi32.NewProc("LineTo")
	procPolyline                  = gdi32.NewProc("Polyline")
	procBitBlt                    = gdi32.NewProc("BitBlt")
	procGradientFill              = msimg32.NewProc("GradientFill")
	procSetTextAlign              = gdi32.NewProc("SetTextAlign")
	procRectangle                 = gdi32.NewProc("Rectangle")
	procSetPixel                  = gdi32.NewProc("SetPixel")
	procGetStockObject            = gdi32.NewProc("GetStockObject")
	procGetDeviceCaps             = gdi32.NewProc("GetDeviceCaps")
	procScreenToClient            = user32.NewProc("ScreenToClient")
)

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type rect struct {
	Left, Top, Right, Bottom int32
}

type point struct {
	X, Y int32
}

type msgStruct struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

const logPixelsX = 88

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type triVertex struct {
	X, Y  int32
	Red   uint16
	Green uint16
	Blue  uint16
	Alpha uint16
}

type gradientRect struct {
	UpperLeft  uint32
	LowerRight uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// Window styles and messages.
const (
	wsPopup     = 0x80000000
	wsVisible   = 0x10000000
	csDropShadow = 0x00020000
	csHRedraw    = 0x0002
	csVRedraw    = 0x0001

	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmPaint       = 0x000F
	wmEraseBkgnd  = 0x0014
	wmSetCursor   = 0x0020
	wmKeyDown     = 0x0100
	wmChar        = 0x0102
	wmTimer       = 0x0113
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmNCHitTest   = 0x0084
	wmActivate    = 0x0006
	wmAppRefresh  = 0x8000 + 1

	htClient  = 1
	htCaption = 2

	swShow = 5

	idcArrow = 32512
	idcHand  = 32649

	dtLeft        = 0x0000
	dtCenter      = 0x0001
	dtRight       = 0x0002
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtNoPrefix    = 0x0800
	dtWordBreak   = 0x0010
	dtEndEllipsis = 0x8000

	transparent = 1

	fwNormal   = 400
	fwSemibold = 600
	fwBold     = 700

	cleartypeQuality = 5
	defaultCharset   = 1
	outTTPrecis      = 4
	clipDefault      = 0
	defaultPitch     = 0
	ffDontCare       = 0

	psSolid = 0
	srcCopy = 0x00CC0020
	dibRgb  = 0
	biRgb   = 0

	gradientFillRectV = 1

	dwmwaWindowCornerPreference = 33
	dwmcpRound                  = 2

	vkBack    = 0x08
	vkReturn  = 0x0D
	vkEscape  = 0x1B
	vkControl = 0x11
	vkA       = 0x41
	vkC       = 0x43
	vkV       = 0x56
	vkX       = 0x58
	mkLButton = 0x0001

	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

func loWord(v uintptr) int32  { return int32(v & 0xFFFF) }
func hiWord(v uintptr) int32  { return int32((v >> 16) & 0xFFFF) }
func makeColor(r, g, b uint32) uint32 {
	return (b << 16) | (g << 8) | r
}

func ptrString(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty, _ := syscall.UTF16PtrFromString("")
		return empty
	}
	return p
}

// enableDPIAwareness opts into per-monitor DPI so the window is crisp instead
// of being bitmap-scaled by the system.
func enableDPIAwareness() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is the handle value -4.
	procSetProcessDpiAwareness.Call(^uintptr(3))
}

// systemDPI reports the DPI of the primary monitor, used to size the window
// before it exists.
func systemDPI() int {
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return 96
	}
	defer procReleaseDC.Call(0, screen)
	dpi, _, _ := procGetDeviceCaps.Call(screen, logPixelsX)
	if dpi < 96 {
		return 96
	}
	return int(dpi)
}

// clipboardText returns the text on the clipboard, if any.
//
// The handle from GlobalLock is a real pointer into the clipboard's shared
// memory and stays valid until GlobalUnlock, which is why the uintptr ->
// pointer conversion is safe here (go vet's unsafeptr check cannot see that,
// so it is run with -unsafeptr=false for this package).
func clipboardText() string {
	if ok, _, _ := procOpenClipboard.Call(0); ok == 0 {
		return ""
	}
	defer procCloseClipboard.Call()

	handle, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		return ""
	}
	pointer, _, _ := procGlobalLock.Call(handle)
	if pointer == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(handle)

	characters := unsafe.Slice((*uint16)(unsafe.Pointer(pointer)), 256)
	for index, value := range characters {
		if value == 0 {
			return syscall.UTF16ToString(characters[:index])
		}
	}
	return syscall.UTF16ToString(characters)
}
