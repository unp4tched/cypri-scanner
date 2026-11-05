// Package nvml queries the NVIDIA management library for the details of the
// installed NVIDIA adapters. The library is optional: on machines without an
// NVIDIA driver the DLL simply does not exist and every probe fails softly.
package nvml

import (
	"fmt"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/winapi"
	"golang.org/x/sys/windows"
)

var nvmlDll = windows.NewLazySystemDLL("nvml.dll")

// NVML status codes.
const (
	statusSuccess       = 0
	statusNotSupported  = 3
	statusNotFound      = 6
	statusUninitialized = 1
)

// NVML clock identifiers.
const (
	clockGraphics = 0
	clockSM       = 1
	clockMem      = 2
)

// memoryInfo mirrors nvmlMemory_t.
type memoryInfo struct {
	total uint64
	free  uint64
	used  uint64
}

// Device describes one NVIDIA adapter.
type Device struct {
	Index                int
	Name                 string
	UUID                 string
	DriverVersion        string
	TotalMemoryBytes     uint64
	FreeMemoryBytes      uint64
	ComputeCapabilityMaj int32
	ComputeCapabilityMin int32
	GraphicsClockMhz     uint32
	SMClockMhz           uint32
	MemoryClockMhz       uint32
	MemoryBusWidthBits   uint32
	HasMemoryBusWidth    bool
	NvLinkActive         bool
	HasNvLink            bool
}

// Available reports whether the NVML runtime could be loaded.
func Available() bool {
	return nvmlDll.Load() == nil
}

// Query initialises NVML and returns every NVIDIA adapter it reports.
func Query() ([]Device, error) {
	if err := nvmlDll.Load(); err != nil {
		return nil, err
	}
	initProc, err := winapi.Proc(nvmlDll, "nvmlInit_v2")
	if err != nil {
		return nil, err
	}
	if status := status32(initProc.Call()); status != statusSuccess {
		return nil, fmt.Errorf("nvmlInit_v2 failed with status %d", status)
	}
	defer func() {
		if p, err := winapi.Proc(nvmlDll, "nvmlShutdown"); err == nil {
			p.Call()
		}
	}()

	countProc, err := winapi.Proc(nvmlDll, "nvmlDeviceGetCount_v2")
	if err != nil {
		return nil, err
	}
	var count uint32
	if status := status32(countProc.Call(uintptr(unsafe.Pointer(&count)))); status != statusSuccess {
		return nil, fmt.Errorf("nvmlDeviceGetCount_v2 failed with status %d", status)
	}

	handleProc, err := winapi.Proc(nvmlDll, "nvmlDeviceGetHandleByIndex_v2")
	if err != nil {
		return nil, err
	}

	devices := make([]Device, 0, count)
	for i := uint32(0); i < count; i++ {
		var handle uintptr
		if status := status32(handleProc.Call(uintptr(i), uintptr(unsafe.Pointer(&handle)))); status != statusSuccess {
			continue
		}
		devices = append(devices, readDevice(int(i), handle))
	}
	return devices, nil
}

func readDevice(index int, handle uintptr) Device {
	d := Device{Index: index}

	d.Name = readString(nvmlDll, "nvmlDeviceGetName", handle, 128)
	d.UUID = readString(nvmlDll, "nvmlDeviceGetUUID", handle, 96)
	d.DriverVersion = readString(nvmlDll, "nvmlDeviceGetDriverVersion", handle, 96)

	if p := winapi.OptionalProc(nvmlDll, "nvmlDeviceGetMemoryInfo"); p != nil {
		var mem memoryInfo
		if status32(p.Call(handle, uintptr(unsafe.Pointer(&mem)))) == statusSuccess {
			d.TotalMemoryBytes = mem.total
			d.FreeMemoryBytes = mem.free
		}
	}

	if p := winapi.OptionalProc(nvmlDll, "nvmlDeviceGetCudaComputeCapability"); p != nil {
		var major, minor int32
		if status32(p.Call(handle, uintptr(unsafe.Pointer(&major)), uintptr(unsafe.Pointer(&minor)))) == statusSuccess {
			d.ComputeCapabilityMaj = major
			d.ComputeCapabilityMin = minor
		}
	}

	if p := winapi.OptionalProc(nvmlDll, "nvmlDeviceGetClockInfo"); p != nil {
		var clock uint32
		if status32(p.Call(handle, clockGraphics, uintptr(unsafe.Pointer(&clock)))) == statusSuccess {
			d.GraphicsClockMhz = clock
		}
		if status32(p.Call(handle, clockSM, uintptr(unsafe.Pointer(&clock)))) == statusSuccess {
			d.SMClockMhz = clock
		}
		if status32(p.Call(handle, clockMem, uintptr(unsafe.Pointer(&clock)))) == statusSuccess {
			d.MemoryClockMhz = clock
		}
	}

	if p := winapi.OptionalProc(nvmlDll, "nvmlDeviceGetMemoryBusWidth"); p != nil {
		var width uint32
		if status32(p.Call(handle, uintptr(unsafe.Pointer(&width)))) == statusSuccess {
			d.MemoryBusWidthBits = width
			d.HasMemoryBusWidth = true
		}
	}

	if p := winapi.OptionalProc(nvmlDll, "nvmlDeviceGetNvLinkState"); p != nil {
		var state uint32
		if status32(p.Call(handle, uintptr(unsafe.Pointer(&state)))) == statusSuccess {
			d.NvLinkActive = state != 0
			d.HasNvLink = true
		}
	}
	return d
}

func readString(dll *windows.LazyDLL, name string, handle uintptr, size int) string {
	proc, err := winapi.Proc(dll, name)
	if err != nil {
		return ""
	}
	buf := make([]byte, size)
	if status32(proc.Call(handle, uintptr(unsafe.Pointer(&buf[0])), uintptr(size))) != statusSuccess {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// status32 truncates the raw return value of an NVML entry point to the
// 32 bit status code it actually is.
func status32(r1 uintptr, _ uintptr, _ error) int32 {
	return int32(uint32(r1))
}
