// Package dxgi enumerates the graphics adapters of the machine through the
// DXGI 1.1 factory and probes the D3D11 feature level of every adapter.
//
// The COM interfaces are driven manually through their vtables so that the
// binary stays pure Go (no cgo) and therefore portable.
package dxgi

import (
	"fmt"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/winapi"
	"golang.org/x/sys/windows"
)

var (
	dxgiDll  = windows.NewLazySystemDLL("dxgi.dll")
	d3d11Dll = windows.NewLazySystemDLL("d3d11.dll")
)

// HRESULT values used by this package.
const (
	hresultSuccess        = 0
	dxgiErrorNotFound     = 0x887A0002 // DXGI_ERROR_NOT_FOUND
	dxgiAdapterFlagSoftw  = 2          // DXGI_ADAPTER_FLAG_SOFTWARE
	dxgiAdapterFlagRemote = 1          // DXGI_ADAPTER_FLAG_REMOTE
)

// iidIDXGIFactory1 is {770AAE78-F26F-4DBA-A829-253C83D1B387}.
var iidIDXGIFactory1 = windows.GUID{
	Data1: 0x770AAE78,
	Data2: 0xF26F,
	Data3: 0x4DBA,
	Data4: [8]byte{0xA8, 0x29, 0x25, 0x3C, 0x83, 0xD1, 0xB3, 0x87},
}

// AdapterDesc1 is the Win32 layout of DXGI_ADAPTER_DESC1.
type AdapterDesc1 struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	AdapterLuid           int32
	AdapterLuidReserved   uint32
	Flags                 uint32
}

// Adapter is the flattened description of one enumerated adapter.
type Adapter struct {
	Description           string
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uint64
	DedicatedSystemMemory uint64
	SharedSystemMemory    uint64
	LUID                  uint64
	Flags                 uint32
	IsSoftware            bool
	IsRemote              bool
	FeatureLevel          string
}

// VendorID values of the common PCIH vendors.
const (
	VendorNVIDIA    = 0x10DE
	VendorAMD       = 0x1002
	VendorATI       = 0x1022
	VendorIntel     = 0x8086
	VendorMicrosoft = 0x1414
	VendorMatrox    = 0x102B
	VendorASPEED    = 0x1A03
	VendorQualcomm  = 0x17CB
)

// Enumerate returns every adapter the DXGI factory reports, including
// software adapters such as the Microsoft Basic Render Driver.
func Enumerate() ([]Adapter, error) {
	factory, hr := createFactory1()
	if hr != hresultSuccess {
		return nil, fmt.Errorf("CreateDXGIFactory1 failed: 0x%08x", uint32(hr))
	}
	defer release(factory)

	var adapters []Adapter
	for index := uint32(0); index < 64; index++ {
		adapter, hr := enumAdapters1(factory, index)
		if uint32(hr) == dxgiErrorNotFound {
			break
		}
		if hr != hresultSuccess || adapter == nil {
			break
		}
		desc, err := getDesc1(adapter)
		if err != nil {
			release(adapter)
			continue
		}
		level := ""
		if desc.Flags&dxgiAdapterFlagSoftw == 0 {
			level = featureLevelFor(adapter)
		}
		release(adapter)

		adapters = append(adapters, Adapter{
			Description:           utf16ToString(desc.Description[:]),
			VendorID:              desc.VendorID,
			DeviceID:              desc.DeviceID,
			SubSysID:              desc.SubSysID,
			Revision:              desc.Revision,
			DedicatedVideoMemory:  uint64(desc.DedicatedVideoMemory),
			DedicatedSystemMemory: uint64(desc.DedicatedSystemMemory),
			SharedSystemMemory:    uint64(desc.SharedSystemMemory),
			LUID:                  uint64(uint32(desc.AdapterLuid)) | uint64(uint32(desc.AdapterLuidReserved))<<32,
			Flags:                 desc.Flags,
			IsSoftware:            desc.Flags&dxgiAdapterFlagSoftw != 0,
			IsRemote:              desc.Flags&dxgiAdapterFlagRemote != 0,
			FeatureLevel:          level,
		})
	}
	return adapters, nil
}

// DefaultFeatureLevel returns the feature level of the adapter the system
// picks for a plain hardware device, i.e. the active display adapter.
func DefaultFeatureLevel() string {
	return featureLevelFor(nil)
}

func createFactory1() (unsafe.Pointer, int32) {
	proc, err := winapi.Proc(dxgiDll, "CreateDXGIFactory1")
	if err != nil {
		return nil, -1
	}
	var factory unsafe.Pointer
	r, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&iidIDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)))
	hr := int32(r)
	if hr != hresultSuccess || factory == nil {
		return nil, hr
	}
	return factory, hresultSuccess
}

func enumAdapters1(factory unsafe.Pointer, index uint32) (unsafe.Pointer, int32) {
	// IDXGIFactory1::EnumAdapters1 sits behind the three IUnknown slots, the
	// four IDXGIObject slots and the five IDXGIFactory slots.
	var adapter unsafe.Pointer
	r := winapi.Call(*winapi.VTableSlot(factory, 12),
		uintptr(factory),
		uintptr(index),
		uintptr(unsafe.Pointer(&adapter)))
	return adapter, int32(r)
}

func getDesc1(adapter unsafe.Pointer) (*AdapterDesc1, error) {
	// IDXGIAdapter1::GetDesc1 sits behind the three IUnknown slots, the four
	// IDXGIObject slots and the three IDXGIAdapter slots.
	desc := &AdapterDesc1{}
	r := winapi.Call(*winapi.VTableSlot(adapter, 10),
		uintptr(adapter),
		uintptr(unsafe.Pointer(desc)))
	if hr := int32(r); hr != hresultSuccess {
		return nil, fmt.Errorf("IDXGIAdapter1::GetDesc1 failed: 0x%08x", uint32(hr))
	}
	return desc, nil
}

func release(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	winapi.Call(*winapi.VTableSlot(ptr, 2), uintptr(ptr))
}

func utf16ToString(buf []uint16) string {
	for i, c := range buf {
		if c == 0 {
			buf = buf[:i]
			break
		}
	}
	return windows.UTF16ToString(buf)
}
