package winapi

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNoWindow keeps a console program from flashing a window.
const createNoWindow = 0x08000000

// swHide hides a window.
const swHide = 0

// HiddenProcess returns the start attributes that suppress a child process's
// console window.
//
// The scanner shells out to dxdiag and powercfg. Each of those is a console
// program, so without these flags Windows opens a black window next to the
// scanner for every call, which looks like the app is spraying junk across the
// desktop.
func HiddenProcess() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// HideConsole suppresses a child process's window.
func HideConsole(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = HiddenProcess()
	return cmd
}

// HideStartupConsole hides the console window that Windows allocates when the
// scanner is launched by double-clicking it.
//
// A console shared with cmd.exe or Windows Terminal belongs to the user's
// terminal, so it is left alone: GetConsoleProcessList reports every process on
// the console, and more than one means it is not ours to hide. That keeps
// `-headless`, `-out` and `-version` printing into the window they were run
// from while a double-click shows only the scanner itself.
func HideStartupConsole() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	getConsoleProcessList := kernel32.NewProc("GetConsoleProcessList")
	showWindow := windows.NewLazySystemDLL("user32.dll").NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}

	// Two slots are enough: we only need to know whether ours is the only
	// process attached.
	var processes [2]uint32
	count, _, _ := getConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&processes[0])),
		uintptr(len(processes)),
	)
	if count > 1 {
		return
	}

	showWindow.Call(hwnd, uintptr(swHide))
}
