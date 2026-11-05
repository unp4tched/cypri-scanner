package gui

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Phase is what the window is currently showing.
type Phase int

const (
	// PhaseCode asks for the pairing code. This is the first thing the user
	// sees: nothing is read until a code is confirmed.
	PhaseCode Phase = iota
	// PhaseConnecting is the handshake with the website after the code is
	// entered, before any hardware is touched.
	PhaseConnecting
	// PhaseScanning shows the live hardware scan.
	PhaseScanning
	// PhaseSending is uploading the profile to the website.
	PhaseSending
	// PhaseDone is the success screen.
	PhaseDone
	// PhaseError explains why the last attempt failed.
	PhaseError
)

// StepState is the progress of one subsystem in the scan list.
type StepState int

const (
	stepPending StepState = iota
	stepActive
	stepDone
)

// Exported mirrors so callers outside the package can drive the scan list.
const (
	StepPending = stepPending
	StepActive  = stepActive
	StepDone    = stepDone
)

type step struct {
	label  string
	state  StepState
	detail string
}

// Options configures the window.
type Options struct {
	// Site is the website the scanner pairs with.
	Site string
	// Version is shown in the title bar area.
	Version string
	// PrefillCode skips the keyboard step when a code was passed on the
	// command line (used by scripts and by the maintainer's test suite).
	PrefillCode string
	// AutoClose is how long the success screen stays up. Zero keeps it open.
	AutoClose time.Duration
	// OnSubmit runs on the UI thread once the user confirms a code.
	OnSubmit func(code string)
}

const (
	windowWidth  = 480
	windowHeight = 620

	fontMark = iota
	fontBrand
	fontBrandSub
	fontVersion
	fontTitle
	fontSubtitle
	fontBody
	fontCaption
	fontTiny
	fontCode
	fontButton
	fontPercent
	fontStep
	fontCount
)

type fontSpec struct {
	size   int
	weight int
	family string
}

var fontSpecs = [fontCount]fontSpec{
	fontMark:      {15, fwBold, "Segoe UI"},
	fontBrand:     {13, fwSemibold, "Segoe UI"},
	fontBrandSub:  {9, fwNormal, "Segoe UI"},
	fontVersion:   {9, fwNormal, "Segoe UI"},
	fontTitle:     {24, fwSemibold, "Segoe UI"},
	fontSubtitle:  {11, fwNormal, "Segoe UI"},
	fontBody:      {11, fwNormal, "Segoe UI"},
	fontCaption:   {10, fwNormal, "Segoe UI"},
	fontTiny:      {9, fwNormal, "Segoe UI"},
	fontCode:      {20, fwBold, "Consolas"},
	fontButton:    {12, fwSemibold, "Segoe UI"},
	fontPercent:   {18, fwSemibold, "Segoe UI"},
	fontStep:      {10, fwNormal, "Segoe UI"},
}

// Colours are written as 0xRRGGBB and converted to GDI's BGR order.
func col(rgb uint32) uint32 {
	return makeColor((rgb>>16)&0xFF, (rgb>>8)&0xFF, rgb&0xFF)
}

var (
	colBgTop      = col(0x0C0F15)
	colBgBottom   = col(0x07090D)
	colSurface    = col(0x121722)
	colSurfaceAlt = col(0x1A2130)
	colBorder     = col(0x232B3A)
	colText       = col(0xF3F6FB)
	colMuted      = col(0x8B94A8)
	colDim        = col(0x5A6379)
	colAccent     = col(0x34D399)
	colAccentDim  = col(0x1D7F63)
	colOnAccent   = col(0x04140E)
	colDanger     = col(0xF87171)
	colDangerDim  = col(0x7F2B2B)
)

// App owns the window and everything it displays.
type App struct {
	mu sync.Mutex

	opts  Options
	hwnd  uintptr
	dpi   int
	scale float64

	phase   Phase
	phaseAt time.Time

	steps    []step
	progress float64
	detail   string
	// doneNote is shown under the success screen when the site has the profile
	// but the browser tab has not collected it yet.
	doneNote string
	hasScan  bool

	code  []rune
	errMsg string

	hover   int
	pressed int
	pressAt time.Time

	sentAt  time.Time
	closeAt time.Time

	fonts     [fontCount]uintptr
	fontScale float64
	back      *surface
	backW     int
	backH     int

	closing bool
}

const (
	hitNone = iota
	hitPrimary
	hitSecondary
	hitClose
)

// New builds an app with the scan list ready to fill in.
func New(opts Options) *App {
	if opts.AutoClose == 0 {
		opts.AutoClose = 8 * time.Second
	}
	app := &App{
		opts:  opts,
		dpi:   96,
		scale: 1,
		phase: PhaseCode,
		steps: []step{
			{label: "Processor"},
			{label: "Graphics"},
			{label: "Memory"},
			{label: "Drives"},
			{label: "System"},
		},
	}
	app.phaseAt = time.Now()
	return app
}

/* ── Updates from the scan and upload goroutines ─────────────────────────── */

// Step marks a list row as active or finished and shows its detail.
func (a *App) Step(index int, state StepState, detail string) {
	a.mu.Lock()
	if index >= 0 && index < len(a.steps) {
		a.steps[index].state = state
		if detail != "" {
			a.steps[index].detail = detail
		}
	}
	done := 0
	for _, item := range a.steps {
		if item.state == stepDone {
			done++
		}
	}
	a.progress = float64(done) / float64(len(a.steps))
	a.mu.Unlock()
	a.refresh()
}

// SetProgress overrides the completed-step ratio.
func (a *App) SetProgress(value float64) {
	a.mu.Lock()
	a.progress = clampFloat(value, 0, 1)
	a.mu.Unlock()
	a.refresh()
}

// SetDetail updates the small live line under the headline.
func (a *App) SetDetail(text string) {
	a.mu.Lock()
	a.detail = text
	a.mu.Unlock()
	a.refresh()
}

// StartScan switches to the live scan screen. It is called only after the
// website has accepted the pairing code, so the very first thing the scanner
// ever reads is already authorised by the user.
func (a *App) StartScan() {
	a.mu.Lock()
	for index := range a.steps {
		a.steps[index].state = stepPending
		a.steps[index].detail = ""
	}
	if len(a.steps) > 0 {
		a.steps[0].state = stepActive
	}
	a.progress = 0
	a.detail = "Starting the scan..."
	a.hasScan = true
	a.phase = PhaseScanning
	a.phaseAt = time.Now()
	a.mu.Unlock()
	a.refresh()
}

// Sending switches the window to the upload phase.
func (a *App) Sending() {
	a.mu.Lock()
	a.phase = PhaseSending
	a.phaseAt = time.Now()
	a.errMsg = ""
	a.pressed = hitNone
	a.mu.Unlock()
	a.refresh()
}

// Uploaded shows the success screen.
func (a *App) Uploaded() {
	a.mu.Lock()
	a.phase = PhaseDone
	a.phaseAt = time.Now()
	a.sentAt = a.phaseAt
	a.doneNote = ""
	a.closeAt = a.phaseAt.Add(a.opts.AutoClose)
	a.mu.Unlock()
	a.refresh()
}

// UploadedUnread shows the success screen when the site accepted the profile
// but no browser tab has collected it yet. The window stays open so the note
// can actually be read.
func (a *App) UploadedUnread(note string) {
	a.mu.Lock()
	a.phase = PhaseDone
	a.phaseAt = time.Now()
	a.sentAt = a.phaseAt
	a.doneNote = note
	a.closeAt = time.Time{}
	a.mu.Unlock()
	a.refresh()
}

// UploadFailed shows an error with the given explanation.
func (a *App) UploadFailed(message string) {
	a.mu.Lock()
	a.phase = PhaseError
	a.phaseAt = time.Now()
	a.errMsg = message
	a.mu.Unlock()
	a.refresh()
}

// CurrentCode returns the code the user has typed so far.
func (a *App) CurrentCode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return string(a.code)
}

// SubmitCode confirms the code when it is complete. The window moves to the
// connecting phase; the caller's OnSubmit runs the handshake with the website.
func (a *App) SubmitCode() {
	a.mu.Lock()
	code := strings.ToUpper(string(a.code))
	phase := a.phase
	a.mu.Unlock()

	if len(code) != 6 {
		return
	}
	switch phase {
	case PhaseConnecting, PhaseScanning, PhaseSending, PhaseDone:
		return
	}

	a.mu.Lock()
	a.phase = PhaseConnecting
	a.phaseAt = time.Now()
	a.errMsg = ""
	a.mu.Unlock()
	a.refresh()

	if a.opts.OnSubmit != nil {
		a.opts.OnSubmit(code)
	}
}

// Close asks the window to shut down.
func (a *App) Close() {
	if a.hwnd != 0 {
		procPostMessageW.Call(a.hwnd, wmClose, 0, 0)
	}
}

func (a *App) refresh() {
	if a.hwnd != 0 {
		procPostMessageW.Call(a.hwnd, wmAppRefresh, 0, 0)
	}
}

/* ── Window lifecycle ────────────────────────────────────────────────────── */

// callback keeps the Go window procedure alive for the lifetime of the app.
var (
	callbackApp *App
	wndProcPtr  uintptr
)

// Run creates the window and pumps messages until it closes.
func (a *App) Run() error {
	enableDPIAwareness()

	// Windows delivers a window's messages to the thread that created it. Go's
	// scheduler will happily move this goroutine to another thread between two
	// GetMessage calls, after which the loop goes deaf and the window reports
	// "not responding" - so pin the goroutine for the lifetime of the window.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	callbackApp = a
	wndProcPtr = syscall.NewCallback(windowProc)

	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)

	className := ptrString("CanYourPCRunItScannerWindow")
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)

	cls := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		Style:         csDropShadow,
		LpfnWndProc:   wndProcPtr,
		HInstance:     instance,
		HCursor:       cursor,
		HbrBackground: 0,
		LpszClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&cls)))

	dpi := systemDPI()
	scale := float64(dpi) / 96
	width := int(math.Round(windowWidth * scale))
	height := int(math.Round(windowHeight * scale))
	screenWidth, _, _ := procGetSystemMetrics.Call(0)
	screenHeight, _, _ := procGetSystemMetrics.Call(1)
	x := (int(screenWidth) - width) / 2
	y := (int(screenHeight) - height) / 2

	title := ptrString("CanYourPCRunIt Scanner")
	hwnd, _, _ := procCreateWindowExW.Call(
		0x00000008, // WS_EX_TOPMOST? no: 0
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup|wsVisible,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("could not create the scanner window")
	}

	a.hwnd = hwnd
	if windowDPI, _, _ := procGetDpiForWindow.Call(hwnd); windowDPI > 0 {
		dpi = int(windowDPI)
	}
	a.dpi = dpi
	a.scale = float64(dpi) / 96

	// Rounded corners with a real shadow on Windows 11; harmless elsewhere.
	preference := int32(dwmcpRound)
	procDwmSetWindowAttribute.Call(
		hwnd,
		dwmwaWindowCornerPreference,
		uintptr(unsafe.Pointer(&preference)),
		uintptr(unsafe.Sizeof(preference)),
	)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetFocus.Call(hwnd)
	procSetTimer.Call(hwnd, 1, 16, 0)

	// A code passed on the command line skips the typing step: fill the cells
	// and start the handshake as soon as the window exists.
	if a.opts.PrefillCode != "" {
		a.mu.Lock()
		a.code = []rune(a.opts.PrefillCode)
		a.mu.Unlock()
		a.SubmitCode()
	}

	var message msgStruct
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}

	a.releaseSurfaces()
	return nil
}

func windowProc(hwnd, message, wParam, lParam uintptr) uintptr {
	app := callbackApp
	if app == nil {
		result, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
		return result
	}
	app.hwnd = hwnd

	switch message {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		app.paintWindow()
		return 0
	case wmAppRefresh:
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmTimer:
		app.tick()
		return 0
	case wmNCHitTest:
		point := point{X: loWord(lParam), Y: hiWord(lParam)}
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&point)))
		if app.hitTitleBar(int(point.X), int(point.Y)) {
			return htCaption
		}
		return htClient
	case wmMouseMove:
		app.setHover(int(loWord(lParam)), int(hiWord(lParam)))
		return 0
	case wmLButtonDown:
		app.mouseDown(int(loWord(lParam)), int(hiWord(lParam)))
		return 0
	case wmLButtonUp:
		app.mouseUp(int(loWord(lParam)), int(hiWord(lParam)))
		return 0
	case wmSetCursor:
		app.updateCursor()
		return 1
	case wmKeyDown:
		app.keyDown(int(wParam))
		return 0
	case wmChar:
		app.keyChar(rune(wParam))
		return 0
	case wmClose:
		app.closing = true
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return result
}

func (a *App) tick() {
	a.mu.Lock()
	phase := a.phase
	closeAt := a.closeAt
	closing := a.closing
	a.mu.Unlock()

	if phase == PhaseDone && !closeAt.IsZero() && !closing && time.Now().After(closeAt) {
		a.Close()
		return
	}
	procInvalidateRect.Call(a.hwnd, 0, 0)
}

func (a *App) releaseSurfaces() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.back != nil {
		a.back.free()
		a.back = nil
	}
	for index := range a.fonts {
		if a.fonts[index] != 0 {
			procDeleteObject.Call(a.fonts[index])
			a.fonts[index] = 0
		}
	}
}

/* ── Input ───────────────────────────────────────────────────────────────── */

func (a *App) geom() *geometry {
	a.mu.Lock()
	scale := a.scale
	a.mu.Unlock()
	if scale <= 0 {
		scale = 1
	}
	return &geometry{scale: scale}
}

// hitTitleBar decides whether a screen point drags the window.
func (a *App) hitTitleBar(screenX, screenY int) bool {
	// The coordinates arriving in WM_NCHITTEST are already client-relative
	// once the window has no frame, which is the case here.
	a.mu.Lock()
	scale := a.scale
	a.mu.Unlock()
	if screenX < 0 || screenY < 0 || screenX > int(float64(windowWidth)*scale) || screenY > int(64*scale) {
		return false
	}
	// Leave the close button clickable.
	return !a.pointInClose(screenX, screenY)
}

func (a *App) pointInClose(x, y int) bool {
	g := a.geom()
	area := g.rect(424, 18, 30, 30)
	return int32(x) >= area.Left && int32(x) <= area.Right && int32(y) >= area.Top && int32(y) <= area.Bottom
}

func (a *App) buttons() (primary rect, secondary rect, primaryVisible bool, secondaryVisible bool) {
	g := a.geom()
	a.mu.Lock()
	phase := a.phase
	a.mu.Unlock()

	primary = g.rect(40, 466, 400, 46)
	secondary = g.rect(40, 532, 400, 26)

	switch phase {
	case PhaseScanning:
		return primary, secondary, true, true
	case PhaseCode:
		return primary, secondary, true, true
	case PhaseConnecting:
		return primary, secondary, true, false
	case PhaseSending:
		return primary, secondary, true, false
	case PhaseError:
		return primary, secondary, true, true
	case PhaseDone:
		return primary, secondary, false, true
	}
	return primary, secondary, false, false
}

func inside(r rect, x, y int32) bool {
	return x >= r.Left && x <= r.Right && y >= r.Top && y <= r.Bottom
}

func (a *App) hitTest(x, y int) int {
	if a.pointInClose(x, y) {
		return hitClose
	}
	primary, secondary, primaryVisible, secondaryVisible := a.buttons()
	px, py := int32(x), int32(y)
	if primaryVisible && inside(primary, px, py) {
		return hitPrimary
	}
	if secondaryVisible && inside(secondary, px, py) {
		return hitSecondary
	}
	return hitNone
}

func (a *App) setHover(x, y int) {
	hit := a.hitTest(x, y)
	a.mu.Lock()
	changed := a.hover != hit
	a.hover = hit
	a.mu.Unlock()
	if changed {
		procInvalidateRect.Call(a.hwnd, 0, 0)
	}
}

func (a *App) mouseDown(x, y int) {
	hit := a.hitTest(x, y)
	a.mu.Lock()
	a.pressed = hit
	a.pressAt = time.Now()
	a.mu.Unlock()
	procSetFocus.Call(a.hwnd)
	procInvalidateRect.Call(a.hwnd, 0, 0)
}

func (a *App) mouseUp(x, y int) {
	a.mu.Lock()
	pressed := a.pressed
	a.pressed = hitNone
	a.mu.Unlock()

	hit := a.hitTest(x, y)
	if pressed != hit || hit == hitNone {
		procInvalidateRect.Call(a.hwnd, 0, 0)
		return
	}
	a.activate(hit)
}

func (a *App) activate(hit int) {
	a.mu.Lock()
	phase := a.phase
	codeLength := len(a.code)
	a.mu.Unlock()

	switch hit {
	case hitClose:
		a.Close()
	case hitPrimary:
		switch phase {
		case PhaseCode:
			if codeLength == 6 {
				a.SubmitCode()
			}
		case PhaseError:
			a.mu.Lock()
			a.phase = PhaseCode
			a.phaseAt = time.Now()
			a.errMsg = ""
			a.mu.Unlock()
			a.refresh()
		}
	case hitSecondary:
		switch phase {
		case PhaseSending, PhaseConnecting, PhaseScanning, PhaseCode, PhaseError:
			a.Close()
		case PhaseDone:
			a.Close()
		}
	}
}

func (a *App) updateCursor() {
	// The system asks on every mouse move; recompute from the last position
	// by asking the window for the cursor position indirectly is overkill, so
	// keep the hand while a button is hovered.
	a.mu.Lock()
	hover := a.hover
	a.mu.Unlock()
	if hover != hitNone {
		procSetCursor.Call(pointerCursor())
		return
	}
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	procSetCursor.Call(cursor)
}

var (
	handCursor    uintptr
	handCursorSet bool
)

func pointerCursor() uintptr {
	if !handCursorSet {
		handCursor, _, _ = procLoadCursorW.Call(0, idcHand)
		handCursorSet = true
	}
	return handCursor
}

func (a *App) keyDown(key int) {
	a.mu.Lock()
	phase := a.phase
	a.mu.Unlock()

	if key == vkEscape {
		a.Close()
		return
	}

	if phase != PhaseCode {
		return
	}

	switch key {
	case vkBack:
		a.mu.Lock()
		if len(a.code) > 0 {
			a.code = a.code[:len(a.code)-1]
			a.errMsg = ""
		}
		a.mu.Unlock()
		a.refresh()
	case vkReturn:
		if len(a.code) == 6 {
			a.SubmitCode()
		}
	case vkV:
		if ctrlPressed() {
			a.pasteFromClipboard()
		}
	case vkC, vkX, vkA:
		// Ctrl+C / Ctrl+X / Ctrl+A have no meaning here; swallow them so no
		// system beep is produced.
	}
}

func (a *App) keyChar(character rune) {
	a.mu.Lock()
	phase := a.phase
	a.mu.Unlock()
	if phase != PhaseCode {
		return
	}
	upper := strings.ToUpper(string(character))
	if len(upper) == 0 || len(a.code) >= 6 {
		return
	}
	value := rune(upper[0])
	if !strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", value) {
		return
	}
	a.mu.Lock()
	a.code = append(a.code, value)
	a.errMsg = ""
	a.mu.Unlock()
	a.refresh()
}

func (a *App) pasteFromClipboard() {
	text := strings.ToUpper(clipboardText())
	cleaned := make([]rune, 0, 6)
	for _, character := range text {
		if strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", character) {
			cleaned = append(cleaned, character)
			if len(cleaned) == 6 {
				break
			}
		}
	}
	if len(cleaned) == 0 {
		return
	}
	a.mu.Lock()
	a.code = cleaned
	a.errMsg = ""
	a.mu.Unlock()
	a.refresh()
}

func ctrlPressed() bool {
	state, _, _ := user32.NewProc("GetKeyState").Call(vkControl)
	return int16(state) < 0
}

/* ── Painting ────────────────────────────────────────────────────────────── */

func (a *App) paintWindow() {
	var paint paintStruct
	hdc, _, _ := procBeginPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&paint)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&paint)))

	var client rect
	procGetClientRect.Call(a.hwnd, uintptr(unsafe.Pointer(&client)))
	width := int(client.Right)
	height := int(client.Bottom)
	if width <= 0 || height <= 0 {
		return
	}

	surface, err := a.ensureSurface(width, height)
	if err != nil {
		return
	}

	a.draw(surface)
	surface.blitTo(hdc)
}

func (a *App) ensureSurface(width, height int) (*surface, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.back != nil && a.backW == width && a.backH == height {
		return a.back, nil
	}
	if a.back != nil {
		a.back.free()
		a.back = nil
	}
	created, err := newSurface(width, height)
	if err != nil {
		return nil, err
	}
	a.back = created
	a.backW = width
	a.backH = height
	return created, nil
}

func (a *App) ensureFonts(scale float64) {
	if math.Abs(a.fontScale-scale) < 0.001 && a.fonts[fontTitle] != 0 {
		return
	}
	for index := range a.fonts {
		if a.fonts[index] != 0 {
			procDeleteObject.Call(a.fonts[index])
			a.fonts[index] = 0
		}
	}
	for index, spec := range fontSpecs {
		height := -int(math.Round(float64(spec.size) * scale))
		font, _, _ := procCreateFontW.Call(
			uintptr(height), 0, 0, 0,
			uintptr(spec.weight),
			0, 0, 0,
			defaultCharset, outTTPrecis, clipDefault, cleartypeQuality,
			defaultPitch|ffDontCare,
			uintptr(unsafe.Pointer(ptrString(spec.family))),
		)
		a.fonts[index] = font
	}
	a.fontScale = scale
}

type view struct {
	phase    Phase
	phaseAt  time.Time
	steps    []step
	progress float64
	code     string
	errMsg   string
	detail   string
	doneNote string
	hasScan  bool
	hover    int
	pressed  int
	pressAt  time.Time
	sentAt   time.Time
	now      time.Time
	site     string
	version  string
}

func (a *App) snapshot() view {
	a.mu.Lock()
	defer a.mu.Unlock()
	steps := make([]step, len(a.steps))
	copy(steps, a.steps)
	return view{
		phase:    a.phase,
		phaseAt:  a.phaseAt,
		steps:    steps,
		progress: a.progress,
		code:     string(a.code),
		errMsg:   a.errMsg,
		detail:   a.detail,
		doneNote: a.doneNote,
		hasScan:  a.hasScan,
		hover:    a.hover,
		pressed:  a.pressed,
		pressAt:  a.pressAt,
		sentAt:   a.sentAt,
		now:      time.Now(),
		site:     a.opts.Site,
		version:  a.opts.Version,
	}
}

type geometry struct {
	scale float64
}

func (g *geometry) i(value int) int {
	return int(math.Round(float64(value) * g.scale))
}

func (g *geometry) rect(x, y, width, height int) rect {
	return rect{
		Left:   int32(g.i(x)),
		Top:    int32(g.i(y)),
		Right:  int32(g.i(x + width)),
		Bottom: int32(g.i(y + height)),
	}
}

// draw paints the whole window for the current state.
func (a *App) draw(s *surface) {
	a.mu.Lock()
	a.ensureFonts(a.scale)
	fonts := a.fonts
	a.mu.Unlock()

	g := &geometry{scale: a.fontScale}
	v := a.snapshot()

	width := g.i(windowWidth)
	height := g.i(windowHeight)

	s.gradient(rect{0, 0, int32(width), int32(height)}, colBgTop, colBgBottom)
	// A faint band of accent light along the very top edge keeps the window
	// from looking like a flat black rectangle.
	s.gradient(g.rect(0, 0, windowWidth, 120), blend(colBgTop, colAccent, 0.06), colBgTop)

	a.drawBrand(s, g, fonts, &v)
	a.drawHeadline(s, g, fonts, &v)

	switch v.phase {
	case PhaseScanning:
		a.drawScanning(s, g, fonts, &v)
	case PhaseCode:
		a.drawCode(s, g, fonts, &v)
	case PhaseConnecting:
		a.drawConnecting(s, g, fonts, &v)
	case PhaseSending:
		a.drawSending(s, g, fonts, &v)
	case PhaseDone:
		a.drawDone(s, g, fonts, &v)
	case PhaseError:
		a.drawError(s, g, fonts, &v)
	}

	a.drawButtons(s, g, fonts, &v)
}

func (a *App) drawBrand(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	mark := g.rect(32, 22, 26, 26)
	s.fillRound(mark, g.i(8), colAccent)
	s.text("C", fonts[fontMark], colOnAccent, g.rect(32, 22, 26, 26), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)

	s.text("CanYourPCRunIt", fonts[fontBrand], colText, g.rect(66, 21, 220, 18), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	s.text("HARDWARE SCANNER", fonts[fontBrandSub], colDim, g.rect(66, 38, 220, 14), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)

	if v.version != "" {
		s.text("v"+v.version, fonts[fontVersion], colDim, g.rect(300, 21, 116, 16), dtRight|dtVCenter|dtSingleLine|dtNoPrefix)
	}

	// Close button.
	closeArea := g.rect(424, 18, 30, 30)
	if v.hover == hitClose {
		s.fillRound(closeArea, g.i(8), colSurfaceAlt)
	}
	inset := g.i(10)
	procX1 := int(closeArea.Left) + inset
	procY1 := int(closeArea.Top) + inset
	procX2 := int(closeArea.Right) - inset
	procY2 := int(closeArea.Bottom) - inset
	lineColor := colMuted
	if v.hover == hitClose {
		lineColor = colText
	}
	s.line(procX1, procY1, procX2, procY2, maxInt(1, g.i(2)-1), lineColor)
	s.line(procX2, procY1, procX1, procY2, maxInt(1, g.i(2)-1), lineColor)

	// Hairline under the header.
	s.fill(g.rect(0, 70, windowWidth, 1), blend(colBgTop, colBorder, 0.55))
}

func (a *App) drawHeadline(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	headline, subtitle := a.copy(v)

	// Fade and lift the text in when the phase changes.
	elapsed := v.now.Sub(v.phaseAt)
	fade := clampFloat(elapsed.Seconds()/0.22, 0, 1)
	lift := int(math.Round((1 - fade) * 6))
	titleColor := blend(colBgTop, colText, fade)

	s.text(headline, fonts[fontTitle], titleColor, g.rect(40, 88+lift, 400, 34), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	s.text(subtitle, fonts[fontSubtitle], blend(colBgTop, colMuted, fade), g.rect(46, 126+lift, 388, 40), dtCenter|dtWordBreak|dtNoPrefix)
}

func (a *App) copy(v *view) (string, string) {
	host := hostOf(v.site)
	switch v.phase {
	case PhaseCode:
		return "Enter your pairing code", "Generate a code on the website, then type\nthe six characters here. Nothing is read yet."
	case PhaseConnecting:
		return "Confirming your code", "Checking it with " + host + " before any\nhardware is read."
	case PhaseScanning:
		return "Reading your hardware", "CPU, graphics, memory, drives and display.\nSent automatically when the scan finishes."
	case PhaseSending:
		return "Sending your specs", "Handing the scan to the browser tab that\nis waiting for it."
	case PhaseDone:
		return "You're connected", "Your browser now shows this PC."
	default:
		if v.hasScan {
			return "That didn't work", "Your scan is safe on this PC. Fix the\nproblem below and send it again."
		}
		return "That didn't work", "The website did not accept the code. Fix\nthe problem below and try again."
	}
}

// hostOf trims a site URL down to the host shown in copy ("example.com").
func hostOf(site string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(site, "https://"), "http://")
	return strings.TrimSuffix(host, "/")
}

func (a *App) drawScanning(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	centerX, centerY := g.i(240), g.i(250)
	radius := g.i(52)
	width := g.i(8)
	s.ring(centerX, centerY, radius, width, 0, 359.6, colSurfaceAlt)

	spin := math.Mod(v.now.Sub(v.phaseAt).Seconds()*150, 360)
	s.ring(centerX, centerY, radius, width, spin-90, 70, blend(colBgTop, colAccentDim, 0.9))
	if v.progress > 0.001 {
		s.ring(centerX, centerY, radius, width, -90, 360*v.progress, colAccent)
	}

	percent := fmt.Sprintf("%d%%", int(math.Round(v.progress*100)))
	s.text(percent, fonts[fontPercent], colText, g.rect(190, 228, 100, 26), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	s.text("SCANNED", fonts[fontTiny], colDim, g.rect(190, 254, 100, 14), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)

	rowTop := 326
	for index, item := range v.steps {
		y := rowTop + index*24
		dotX := g.i(92)
		dotY := g.i(y + 10)
		switch item.state {
		case stepDone:
			s.dot(dotX, dotY, g.i(4), colAccent)
		case stepActive:
			pulse := 0.5 + 0.5*math.Sin(v.now.Sub(v.phaseAt).Seconds()*4)
			s.dot(dotX, dotY, g.i(8), blend(colBgTop, colAccent, 0.14+0.10*pulse))
			s.dot(dotX, dotY, g.i(4), colAccent)
		default:
			s.dot(dotX, dotY, g.i(3), colDim)
		}

		labelColor := colDim
		if item.state == stepDone {
			labelColor = colMuted
		} else if item.state == stepActive {
			labelColor = colText
		}
		s.text(item.label, fonts[fontStep], labelColor, g.rect(106, y, 100, 20), dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
		if item.detail != "" {
			s.text(item.detail, fonts[fontTiny], colDim, g.rect(200, y, 196, 20), dtRight|dtVCenter|dtSingleLine|dtNoPrefix|dtEndEllipsis)
		}
	}

	if v.detail != "" {
		s.text(v.detail, fonts[fontTiny], colDim, g.rect(60, 444, 360, 16), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix|dtEndEllipsis)
	}
}

func (a *App) drawCode(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	s.text("PAIRING CODE", fonts[fontTiny], colDim, g.rect(40, 190, 400, 14), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)

	cellWidth, cellHeight, gap := 46, 62, 10
	total := 6*cellWidth + 5*gap
	startX := (windowWidth - total) / 2
	top := 208

	for index := 0; index < 6; index++ {
		x := startX + index*(cellWidth+gap)
		cell := g.rect(x, top, cellWidth, cellHeight)
		filled := index < len(v.code)
		active := index == len(v.code) || (index == 5 && len(v.code) == 6)

		fill := colSurface
		if filled {
			fill = blend(colSurface, colAccent, 0.10)
		}
		s.fillRound(cell, g.i(12), fill)

		border := colBorder
		borderWidth := 1
		if active {
			border = colAccent
			borderWidth = 2
		} else if filled {
			border = blend(colBorder, colAccent, 0.45)
		}
		s.strokeRound(cell, g.i(12), maxInt(1, g.i(borderWidth)), border)

		if filled {
			s.text(string(v.code[index]), fonts[fontCode], colText, cell, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		} else if active {
			blink := math.Mod(float64(v.now.UnixMilli()), 1100) < 620
			if blink {
				caretX := int(cell.Left) + g.i(cellWidth/2)
				s.line(caretX, int(cell.Top)+g.i(16), caretX, int(cell.Bottom)-g.i(16), maxInt(2, g.i(2)), colAccent)
			}
		}
	}

	host := strings.TrimPrefix(strings.TrimPrefix(v.site, "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	s.text("Codes are generated at "+host+"/scanner", fonts[fontCaption], colMuted,
		g.rect(40, 292, 400, 18), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	s.text("The code expires 10 minutes after it is created.", fonts[fontTiny], colDim,
		g.rect(40, 312, 400, 16), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)

	if v.errMsg != "" {
		s.text(v.errMsg, fonts[fontCaption], colDanger, g.rect(56, 340, 368, 40), dtCenter|dtWordBreak|dtNoPrefix)
	} else {
		s.text("Tip: Ctrl+V pastes a code you copied.", fonts[fontTiny], colDim,
			g.rect(40, 340, 400, 16), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}
}

func (a *App) drawSending(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	centerX, centerY := g.i(240), g.i(246)
	radius := g.i(52)
	width := g.i(8)
	s.ring(centerX, centerY, radius, width, 0, 359.6, colSurfaceAlt)
	spin := math.Mod(v.now.Sub(v.phaseAt).Seconds()*240, 360)
	s.ring(centerX, centerY, radius, width, spin-90, 110, colAccent)
	s.text("SENDING", fonts[fontTiny], colDim, g.rect(180, 240, 120, 16), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)

	s.text("Uploading your profile to "+hostOf(v.site), fonts[fontCaption], colMuted, g.rect(40, 322, 400, 18), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
}

// drawConnecting shows the handshake that happens between entering the code
// and the first hardware query: a calm pulse while the website validates it.
func (a *App) drawConnecting(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	centerX, centerY := g.i(240), g.i(246)
	radius := g.i(52)
	width := g.i(8)
	s.ring(centerX, centerY, radius, width, 0, 359.6, colSurfaceAlt)

	// Two counter-rotating arcs: the outer one sweeps slowly, the inner dot
	// orbits fast, so it reads as "talking to the site" rather than "loading".
	spin := math.Mod(v.now.Sub(v.phaseAt).Seconds()*90, 360)
	s.ring(centerX, centerY, radius, width, spin-90, 80, colAccent)
	pulse := 0.5 + 0.5*math.Sin(v.now.Sub(v.phaseAt).Seconds()*3)
	s.ring(centerX, centerY, g.i(34), maxInt(2, g.i(3)), -90, 359.6, blend(colBgTop, colAccentDim, 0.35+0.35*pulse))
	s.dot(centerX, centerY, g.i(6), colAccent)

	s.text("CHECKING CODE", fonts[fontTiny], colDim, g.rect(160, 318, 160, 16), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	s.text("Nothing on this PC is read until it\naccepts.", fonts[fontCaption], colMuted,
		g.rect(40, 344, 400, 34), dtCenter|dtWordBreak|dtNoPrefix)
}

func (a *App) drawDone(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	centerX, centerY := g.i(240), g.i(246)
	radius := g.i(54)
	width := g.i(8)

	elapsed := v.now.Sub(v.phaseAt)
	reveal := clampFloat(elapsed.Seconds()/0.4, 0, 1)

	s.ring(centerX, centerY, radius, width, -90, 359.6*clampFloat(elapsed.Seconds()/0.5, 0, 1), colAccent)
	s.checkMark(centerX, centerY, g.i(46), reveal, colAccent)

	secondsLeft := 0
	if !v.sentAt.IsZero() {
		secondsLeft = int(math.Ceil(8 - v.now.Sub(v.sentAt).Seconds()))
		if secondsLeft < 0 {
			secondsLeft = 0
		}
	}

	s.text("Delivered to "+hostOf(v.site), fonts[fontCaption], colMuted, g.rect(40, 322, 400, 18), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	if secondsLeft > 0 {
		s.text(fmt.Sprintf("This window closes in %ds", secondsLeft), fonts[fontTiny], colDim,
			g.rect(40, 344, 400, 16), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}
	if v.doneNote != "" {
		s.text(v.doneNote, fonts[fontTiny], colMuted,
			g.rect(36, 344, 408, 40), dtCenter|dtWordBreak|dtNoPrefix)
	}
}

func (a *App) drawError(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	centerX, centerY := g.i(240), g.i(232)
	radius := g.i(48)
	width := g.i(8)

	elapsed := v.now.Sub(v.phaseAt)
	shake := 0.0
	if elapsed < 350*time.Millisecond {
		progress := elapsed.Seconds() / 0.35
		shake = math.Sin(progress*math.Pi*6) * 5 * (1 - progress)
	}
	centerX += int(math.Round(shake * g.scale))

	s.ring(centerX, centerY, radius, width, -90, 359.6, blend(colBgTop, colDangerDim, 0.55))
	s.line(centerX, centerY-g.i(18), centerX, centerY+g.i(6), maxInt(2, g.i(4)), colDanger)
	s.dot(centerX, centerY+g.i(18), maxInt(2, g.i(3)), colDanger)

	if v.errMsg != "" {
		s.text(v.errMsg, fonts[fontCaption], colMuted, g.rect(56, 316, 368, 64), dtCenter|dtWordBreak|dtNoPrefix)
	}
}

func (a *App) drawButtons(s *surface, g *geometry, fonts [fontCount]uintptr, v *view) {
	primary, secondary, primaryVisible, secondaryVisible := a.buttons()

	if primaryVisible {
		label := "Send to website"
		enabled := true
		switch v.phase {
		case PhaseScanning:
			label = "Scanning..."
			enabled = false
		case PhaseCode:
			label = "Connect & scan"
			enabled = len(v.code) == 6
		case PhaseConnecting:
			label = "Connecting..."
			enabled = false
		case PhaseSending:
			label = "Sending..."
			enabled = false
		case PhaseError:
			label = "Try again"
		}

		pressed := v.hover == hitPrimary && v.pressed == hitPrimary
		fill := colAccent
		if !enabled {
			fill = colSurfaceAlt
		} else if pressed {
			fill = blend(colAccent, colBgTop, 0.14)
		} else if v.hover == hitPrimary {
			fill = blend(colAccent, colText, 0.12)
		}

		area := primary
		if pressed {
			area.Top += int32(g.i(1))
			area.Bottom += int32(g.i(1))
		}
		s.fillRound(area, g.i(12), fill)
		textColor := colOnAccent
		if !enabled {
			textColor = colDim
		}
		s.text(label, fonts[fontButton], textColor, area, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}

	if secondaryVisible {
		label := "Quit"
		switch v.phase {
		case PhaseCode:
			label = "Cancel"
		case PhaseDone:
			label = "Close now"
		}
		color := colMuted
		if v.hover == hitSecondary {
			color = colText
		}
		s.text(label, fonts[fontButton], color, secondary, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}

	footer := "Nothing is read until the code is accepted."
	if v.phase == PhaseDone {
		footer = "You can close this window now."
	} else if v.phase == PhaseSending {
		footer = "One request, then the scanner forgets the code."
	} else if v.phase == PhaseConnecting {
		footer = "Checking the code before touching any hardware."
	} else if v.phase == PhaseScanning {
		footer = "Your specs are sent once the scan finishes."
	}
	s.text(footer, fonts[fontTiny], colDim, g.rect(40, 570, 400, 16), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
}

/* ── Preview rendering (used to eyeball the UI without a desktop) ────────── */

// PreviewOptions drives WritePreview.
type PreviewOptions struct {
	Phase    Phase
	Code     string
	Progress float64
	Error    string
	Steps    int
	Site     string
	Version  string
	// HasScan marks the error screen as a failed upload (scan already done)
	// rather than a failed handshake (nothing read yet).
	HasScan bool
}

// WritePreview renders one state to a PNG file.
func WritePreview(path string, options PreviewOptions) error {
	app := &App{
		opts:      Options{Site: options.Site, Version: options.Version},
		dpi:       96,
		scale:     1,
		phase:     options.Phase,
		phaseAt:   time.Now().Add(-2 * time.Second),
		progress:  options.Progress,
		errMsg:    options.Error,
		code:      []rune(options.Code),
		hasScan:   options.HasScan,
		fontScale: 1,
		steps: []step{
			{label: "Processor", state: stepDone, detail: "Ryzen 7 5800X"},
			{label: "Graphics", state: stepDone, detail: "RTX 4070 · 12 GB"},
			{label: "Memory", state: stepDone, detail: "32 GB DDR4"},
			{label: "Drives", state: stepDone, detail: "990 PRO 2 TB"},
			{label: "System", state: stepDone, detail: "Windows 11 Pro"},
		},
	}
	for index := 0; index < options.Steps && index < len(app.steps); index++ {
		app.steps[index].state = stepDone
	}

	w := windowWidth
	h := windowHeight
	created, err := newSurface(w, h)
	if err != nil {
		return err
	}
	defer created.free()

	app.draw(created)

	pixels := created.bgra()
	rgba := make([]byte, w*h*4)
	for index := 0; index < w*h; index++ {
		blue := pixels[index*4+0]
		green := pixels[index*4+1]
		red := pixels[index*4+2]
		rgba[index*4+0] = red
		rgba[index*4+1] = green
		rgba[index*4+2] = blue
		rgba[index*4+3] = 0xFF
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := writePNG(file, rgba, w, h); err != nil {
		return err
	}
	return nil
}
