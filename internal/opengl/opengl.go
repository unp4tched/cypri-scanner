// Package opengl queries the OpenGL implementation of the active display
// driver by creating a short lived context on a hidden window.
package opengl

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/winapi"
	"golang.org/x/sys/windows"
)

var (
	opengl32Dll = windows.NewLazySystemDLL("opengl32.dll")
	gdi32Dll    = windows.NewLazySystemDLL("gdi32.dll")
	user32Dll   = windows.NewLazySystemDLL("user32.dll")
	kernel32Dll = windows.NewLazySystemDLL("kernel32.dll")
)

// GL string enumerants.
const (
	glVendor                 = 0x1F00
	glRenderer               = 0x1F01
	glVersion                = 0x1F02
	glShadingLanguageVersion = 0x8B8C
)

// Info holds the OpenGL strings reported by the driver.
type Info struct {
	Version                string
	Renderer               string
	Vendor                 string
	ShadingLanguageVersion string
}

// wndClassW is the Win32 layout of WNDCLASSW.
type wndClassW struct {
	style         uint32
	_             uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName uintptr
}

const (
	wsOverlappedWindow = 0x00CF0000
	wmNCCreate         = 0x0081
	swHide             = 0
)

var (
	classOnce    sync.Once
	classNamePtr uintptr
	instancePtr  uintptr
	classErr     error
)

// Query creates an OpenGL context and reads the driver strings.
//
// The whole context lifetime runs with the OS thread pinned: wglMakeCurrent
// binds a context to a specific thread, but the Go scheduler is otherwise
// free to move this goroutine to another thread between two syscalls. If that
// happens, glGetString runs with no current context and the driver dereferences
// a null pointer - a native access violation that recover() cannot catch.
func Query() (Info, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var info Info
	if err := opengl32Dll.Load(); err != nil {
		return info, err
	}
	if err := user32Dll.Load(); err != nil {
		return info, err
	}
	if err := gdi32Dll.Load(); err != nil {
		return info, err
	}

	hdc, release, err := acquireDeviceContext()
	if err != nil {
		return info, err
	}
	defer release()

	ctx, err := createContext(hdc)
	if err != nil {
		return info, err
	}
	defer destroyContext(hdc, ctx)

	// Resolve glGetString only after a context is current: wglGetProcAddress
	// requires one and can return a bogus non-null address without it.
	if err := makeCurrent(hdc, ctx); err != nil {
		return info, err
	}
	defer makeCurrent(hdc, 0)

	glGetString, err := resolveGetString()
	if err != nil {
		return info, err
	}

	info.Version = glString(glGetString, glVersion)
	info.Renderer = glString(glGetString, glRenderer)
	info.Vendor = glString(glGetString, glVendor)
	info.ShadingLanguageVersion = glString(glGetString, glShadingLanguageVersion)
	if info.Version == "" {
		return info, fmt.Errorf("opengl32 returned no GL_VERSION")
	}
	return info, nil
}

func resolveGetString() (uintptr, error) {
	if wglGetProcAddress := winapi.OptionalProc(opengl32Dll, "wglGetProcAddress"); wglGetProcAddress != nil {
		// wglGetProcAddress expects a plain null terminated byte string.
		name := []byte("glGetString\x00")
		r, _, _ := wglGetProcAddress.Call(uintptr(unsafe.Pointer(&name[0])))
		if r != 0 {
			return r, nil
		}
	}
	proc, err := winapi.Proc(opengl32Dll, "glGetString")
	if err != nil {
		return 0, err
	}
	return proc.Addr(), nil
}

// glGetStringFunc is the signature of the OpenGL glGetString entry point.
type glGetStringFunc func(name uint32) *uint8

func glString(fn uintptr, name uint32) string {
	return winapi.CString(winapi.Call(fn, uintptr(name)), 512)
}

// acquireDeviceContext returns a device context that can back an OpenGL
// context. A hidden window is preferred because only a window DC is handled
// by the vendor ICD; a memory DC is used as a fallback.
func acquireDeviceContext() (uintptr, func(), error) {
	if hwnd, err := createHiddenWindow(); err == nil {
		hdc, _, _ := user32Dll.NewProc("GetDC").Call(hwnd)
		if hdc != 0 {
			return hdc, func() {
				user32Dll.NewProc("ReleaseDC").Call(hwnd, hdc)
				user32Dll.NewProc("DestroyWindow").Call(hwnd)
			}, nil
		}
	}

	hdc, _, _ := gdi32Dll.NewProc("CreateCompatibleDC").Call(0)
	if hdc == 0 {
		return 0, func() {}, fmt.Errorf("no usable device context")
	}
	return hdc, func() {
		gdi32Dll.NewProc("DeleteDC").Call(hdc)
	}, nil
}

func createHiddenWindow() (uintptr, error) {
	classOnce.Do(func() {
		hinst, _, _ := kernel32Dll.NewProc("GetModuleHandleW").Call(0)
		instancePtr = hinst
		className, err := windows.UTF16PtrFromString("CanYourPCRunItOpenGLProbe")
		if err != nil {
			classErr = err
			return
		}
		classNamePtr = uintptr(unsafe.Pointer(className))

		wndProc := windows.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
			if msg == wmNCCreate {
				return 1
			}
			r, _, _ := user32Dll.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
			return r
		})

		wc := wndClassW{
			lpfnWndProc:   wndProc,
			hInstance:     hinst,
			lpszClassName: classNamePtr,
		}
		user32Dll.NewProc("RegisterClassW").Call(uintptr(unsafe.Pointer(&wc)))
	})
	if classErr != nil {
		return 0, classErr
	}
	if classNamePtr == 0 {
		return 0, fmt.Errorf("window class registration failed")
	}

	hwnd, _, _ := user32Dll.NewProc("CreateWindowExW").Call(
		0,
		classNamePtr,
		classNamePtr,
		wsOverlappedWindow,
		0, 0, 1, 1,
		0, 0, instancePtr, 0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowExW failed")
	}
	user32Dll.NewProc("ShowWindow").Call(hwnd, swHide)
	return hwnd, nil
}

// pixelFormatDescriptor is the Win32 layout of PIXELFORMATDESCRIPTOR.
type pixelFormatDescriptor struct {
	Size            uint16
	Version         uint16
	Flags           uint32
	PixelType       uint8
	ColorBits       uint8
	RedBits         uint8
	GreenBits       uint8
	BlueBits        uint8
	RedShift        uint8
	GreenShift      uint8
	BlueShift       uint8
	AlphaBits       uint8
	AlphaShift      uint8
	AccumBits       uint8
	AccumRedBits    uint8
	AccumGreenBits  uint8
	AccumBlueBits   uint8
	AccumAlphaBits  uint8
	AccumAlphaShift uint8
	DepthBits       uint8
	StencilBits     uint8
	AuxBuffers      uint8
	LayerType       uint8
	Reserved        uint8
	_               [3]uint8
	LayerMask       uint32
	VisibleMask     uint32
	DamageMask      uint32
	_               [4]uint8
}

const (
	pfdTypeRGBA      = 0
	pfdDoubleBuffer  = 0x00000001
	pfdDrawToWindow  = 0x00000004
	pfdSupportOpenGL = 0x00000020
)

// setPixelFormat gives the device context a pixel format that OpenGL can
// render into. wglCreateContext fails without one.
//
// The API compares the size in the structure with the size it was compiled
// with, so the known variants are tried until one is accepted.
func setPixelFormat(hdc uintptr) error {
	choose, err := winapi.Proc(gdi32Dll, "ChoosePixelFormat")
	if err != nil {
		return err
	}
	set, err := winapi.Proc(gdi32Dll, "SetPixelFormat")
	if err != nil {
		return err
	}

	descriptor := pixelFormatDescriptor{
		Version:     1,
		Flags:       pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer,
		PixelType:   pfdTypeRGBA,
		ColorBits:   32,
		RedBits:     8,
		GreenBits:   8,
		BlueBits:    8,
		AlphaBits:   8,
		DepthBits:   24,
		StencilBits: 8,
	}

	var format uintptr
	for _, size := range []uint16{44, 41, 48} {
		descriptor.Size = size
		chosen, _, _ := choose.Call(hdc, uintptr(unsafe.Pointer(&descriptor)))
		if chosen != 0 {
			format = chosen
			break
		}
	}
	if format == 0 {
		return fmt.Errorf("ChoosePixelFormat failed")
	}

	ok, _, _ := set.Call(hdc, format, uintptr(unsafe.Pointer(&descriptor)))
	if ok == 0 {
		return fmt.Errorf("SetPixelFormat failed")
	}
	return nil
}

// createContext creates an OpenGL rendering context on a device context.
func createContext(hdc uintptr) (uintptr, error) {
	if err := setPixelFormat(hdc); err != nil {
		return 0, err
	}
	proc, err := winapi.Proc(opengl32Dll, "wglCreateContext")
	if err != nil {
		return 0, err
	}
	ctx, _, _ := proc.Call(hdc)
	if ctx == 0 {
		return 0, fmt.Errorf("wglCreateContext failed")
	}
	return ctx, nil
}

func destroyContext(hdc, ctx uintptr) {
	opengl32Dll.NewProc("wglMakeCurrent").Call(hdc, 0)
	opengl32Dll.NewProc("wglDeleteContext").Call(ctx)
}

func makeCurrent(hdc, ctx uintptr) error {
	r, _, _ := opengl32Dll.NewProc("wglMakeCurrent").Call(hdc, ctx)
	if r == 0 {
		return fmt.Errorf("wglMakeCurrent failed")
	}
	return nil
}
