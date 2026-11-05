package dxgi

import (
	"fmt"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

// D3D11 constants.
const (
	d3dDriverTypeUnknown  = 0
	d3dDriverTypeHardware = 1
	d3d11SDKVersion       = 7

	featureLevel11_1 = 0xb100
	featureLevel11_0 = 0xb000
	featureLevel10_1 = 0xa100
	featureLevel10_0 = 0xa000
	featureLevel9_3  = 0x9300
	featureLevel9_2  = 0x9200
	featureLevel9_1  = 0x9100
)

// featureLevelNames maps a D3D feature level to its "major_minor" name.
var featureLevelNames = map[uint32]string{
	featureLevel11_1: "11_1",
	featureLevel11_0: "11_0",
	featureLevel10_1: "10_1",
	featureLevel10_0: "10_0",
	featureLevel9_3:  "9_3",
	featureLevel9_2:  "9_2",
	featureLevel9_1:  "9_1",
}

// FeatureLevelName converts a raw D3D feature level value to its name.
func FeatureLevelName(level uint32) string {
	if name, ok := featureLevelNames[level]; ok {
		return name
	}
	return ""
}

// featureLevelFor creates a throw-away D3D11 device and reads back the feature
// level the driver settled on. A nil adapter asks for the system default
// (the adapter that currently drives the desktop).
func featureLevelFor(adapter unsafe.Pointer) string {
	level, err := createDeviceFeatureLevel(adapter)
	if err != nil {
		return ""
	}
	return level
}

func createDeviceFeatureLevel(adapter unsafe.Pointer) (string, error) {
	proc, err := winapi.Proc(d3d11Dll, "D3D11CreateDevice")
	if err != nil {
		return "", err
	}
	levels := []uint32{
		featureLevel11_1, featureLevel11_0, featureLevel10_1,
		featureLevel10_0, featureLevel9_3, featureLevel9_2, featureLevel9_1,
	}
	driverType := uintptr(d3dDriverTypeUnknown)
	if adapter == nil {
		driverType = d3dDriverTypeHardware
	}

	var device, context unsafe.Pointer
	var gotLevel uint32

	r, _, _ := proc.Call(
		uintptr(adapter),
		driverType,
		0,
		0,
		uintptr(unsafe.Pointer(&levels[0])),
		uintptr(len(levels)),
		uintptr(d3d11SDKVersion),
		uintptr(unsafe.Pointer(&device)),
		uintptr(unsafe.Pointer(&gotLevel)),
		uintptr(unsafe.Pointer(&context)),
	)
	if int32(r) < 0 {
		return "", fmt.Errorf("D3D11CreateDevice failed: 0x%08x", uint32(r))
	}
	defer release(device)
	if context != nil {
		defer release(context)
	}
	return FeatureLevelName(gotLevel), nil
}
