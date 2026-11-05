// Package winapi holds the small helpers shared by the packages that talk to
// Windows DLLs and COM interfaces directly.
package winapi

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// vtableSlotCount is how many entries of a COM vtable are addressable. Every
// interface used in this scanner stays far below that.
const vtableSlotCount = 24

// Proc resolves an entry point of a DLL.
//
// The DLL is loaded on demand, so a missing optional driver library simply
// produces an error instead of stopping the program.
func Proc(dll *windows.LazyDLL, name string) (*windows.LazyProc, error) {
	proc := dll.NewProc(name)
	if err := proc.Find(); err != nil {
		return nil, err
	}
	return proc, nil
}

// OptionalProc resolves an entry point and reports whether it exists.
func OptionalProc(dll *windows.LazyDLL, name string) *windows.LazyProc {
	proc, err := Proc(dll, name)
	if err != nil {
		return nil
	}
	return proc
}

// VTableSlot returns the address of the n-th function pointer of the COM
// vtable behind obj.
//
// The result is a pointer to the slot rather than the function address so
// that callers can reinterpret it in place:
//
//	fn := *(*someFunc)(unsafe.Pointer(winapi.VTableSlot(obj, 3)))
func VTableSlot(obj unsafe.Pointer, index int) *uintptr {
	vt := *(**uintptr)(obj)
	slots := unsafe.Slice(vt, vtableSlotCount)
	return &slots[index]
}

// Call invokes a native function pointer with the Windows calling
// convention.
//
// A Go function value must not be used for this: Go passes arguments in its
// own register ABI (RAX, RBX, RCX, ...) while the Windows ABI expects them
// in RCX, RDX, R8, R9. Calling a Win32 or COM entry point through a Go func
// value therefore passes the arguments in the wrong registers.
func Call(fn uintptr, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(fn, args...)
	return r
}

// ntdll is used to copy native strings into Go memory.
var ntdll = windows.NewLazySystemDLL("ntdll.dll")

// CString reads a NUL terminated string from a native pointer.
//
// The memory belongs to the driver rather than to the Go heap, so it is
// copied out with RtlMoveMemory instead of being reinterpreted as a Go
// pointer.
func CString(p uintptr, max int) string {
	if p == 0 {
		return ""
	}

	memmove := ntdll.NewProc("RtlMoveMemory")
	if err := memmove.Find(); err != nil {
		return ""
	}

	buf := make([]byte, max)
	syscall.SyscallN(memmove.Addr(),
		uintptr(unsafe.Pointer(&buf[0])),
		p,
		uintptr(max))
	runtime.KeepAlive(buf)

	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
