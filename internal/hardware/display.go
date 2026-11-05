package hardware

import (
	"golang.org/x/sys/windows"
	"strings"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/model"
	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

var user32 = windows.NewLazySystemDLL("user32.dll")

// Display device state flags.
const (
	displayDeviceAttachedToDesktop = 0x1
	displayDevicePrimaryDevice     = 0x4
	enumCurrentSettings            = 0xFFFFFFFF
)

// displayDeviceW is the Win32 layout of DISPLAY_DEVICEW.
type displayDeviceW struct {
	CB           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

// devModeW is the Win32 layout of DEVMODEW.
//
// The structure is 220 bytes long. The short members between dmFields and
// dmFormName are 16 bit wide, so declaring them as 32 bit values would shift
// every member behind them.
type devModeW struct {
	DeviceName       [32]uint16
	SpecVersion      uint16
	DriverVersion    uint16
	Size             uint16
	DriverExtra      uint16
	Fields           uint32
	Orientation      int16
	PaperSize        int16
	PaperLength      int16
	PaperWidth       int16
	Scale            int16
	Copies           int16
	DefaultSource    int16
	PrintQuality     int16
	Color            int16
	Duplex           int16
	YResolution      int16
	TTOption         int16
	Collate          int16
	FormName         [32]uint16
	LogPixels        uint16
	BitsPerPel       uint32
	PelsWidth        uint32
	PelsHeight       uint32
	DisplayFlags     uint32
	DisplayFrequency uint32
	ICMMethod        uint32
	ICMIntent        uint32
	MediaType        uint32
	DitherType       uint32
	Reserved1        uint32
	Reserved2        uint32
	PanningWidth     uint32
	PanningHeight    uint32
}

// collectDisplay gathers the display topology of the desktop.
func collectDisplay() model.DisplayInfo {
	display := model.DisplayInfo{
		Monitors: []model.MonitorInfo{},
	}

	adapters := enumerateDisplayDevices("")
	controllers := queryStructs[win32VideoController]("SELECT * FROM Win32_VideoController")

	var primaryMode *devModeW
	var primaryAdapterName string
	for _, adapter := range adapters {
		if adapter.StateFlags&displayDeviceAttachedToDesktop == 0 {
			continue
		}
		if adapter.StateFlags&displayDevicePrimaryDevice != 0 && primaryMode == nil {
			deviceName := utf16ToString(adapter.DeviceName[:])
			if mode, ok := enumerateDisplaySettings(deviceName); ok {
				copied := mode
				primaryMode = &copied
				primaryAdapterName = deviceName
			}
		}
	}

	if primaryMode == nil {
		// Fall back to the first attached adapter.
		for _, adapter := range adapters {
			if adapter.StateFlags&displayDeviceAttachedToDesktop == 0 {
				continue
			}
			deviceName := utf16ToString(adapter.DeviceName[:])
			if mode, ok := enumerateDisplaySettings(deviceName); ok {
				copied := mode
				primaryMode = &copied
				primaryAdapterName = deviceName
			}
			break
		}
	}

	for _, adapter := range adapters {
		if adapter.StateFlags&displayDeviceAttachedToDesktop == 0 {
			continue
		}
		monitors := enumerateDisplayDevices(utf16ToString(adapter.DeviceName[:]))
		if len(monitors) == 0 {
			monitors = []displayDeviceW{adapter}
		}
		for _, monitor := range monitors {
			if monitor.StateFlags&displayDeviceAttachedToDesktop == 0 {
				continue
			}
			entry := model.MonitorInfo{
				Primary: adapter.StateFlags&displayDevicePrimaryDevice != 0,
			}
			if name := strings.TrimSpace(utf16ToString(monitor.DeviceString[:])); name != "" {
				entry.Name = pString(name)
			}
			if primaryMode != nil && utf16ToString(adapter.DeviceName[:]) == primaryAdapterName {
				entry.Width = pInt(int(primaryMode.PelsWidth))
				entry.Height = pInt(int(primaryMode.PelsHeight))
				if primaryMode.DisplayFrequency > 0 {
					entry.RefreshRateHz = pInt(int(primaryMode.DisplayFrequency))
				}
			}
			display.Monitors = append(display.Monitors, entry)
		}
	}

	display.MonitorCount = len(display.Monitors)
	for i := range display.Monitors {
		if display.Monitors[i].Primary {
			copied := display.Monitors[i]
			display.Primary = &copied
			break
		}
	}

	if primaryMode != nil {
		display.ResolutionWidth = pInt(int(primaryMode.PelsWidth))
		display.ResolutionHeight = pInt(int(primaryMode.PelsHeight))
		if primaryMode.DisplayFrequency > 0 {
			display.RefreshRateHz = pInt(int(primaryMode.DisplayFrequency))
		}
		if primaryMode.BitsPerPel > 0 {
			display.ColorDepthBits = pInt(int(primaryMode.BitsPerPel))
		}
	}

	if display.ResolutionWidth == nil {
		for _, controller := range controllers {
			if controller.CurrentHorizontalResolution > 0 && controller.CurrentVerticalResolution > 0 {
				display.ResolutionWidth = pInt(int(controller.CurrentHorizontalResolution))
				display.ResolutionHeight = pInt(int(controller.CurrentVerticalResolution))
				if controller.CurrentRefreshRate > 0 {
					display.RefreshRateHz = pInt(int(controller.CurrentRefreshRate))
				}
				break
			}
		}
	}

	if hdr, ok := displaySupportsHDR(); ok {
		display.SupportsHdr = pBool(hdr)
		if display.Primary != nil {
			display.Primary.Hdr = pBool(hdr)
		}
	}
	return display
}

// enumerateDisplayDevices lists the display devices. An empty device name
// lists the adapters, a device name lists the monitors of that adapter.
func enumerateDisplayDevices(deviceName string) []displayDeviceW {
	proc, err := winapi.Proc(user32, "EnumDisplayDevicesW")
	if err != nil {
		return nil
	}
	var namePtr *uint16
	if deviceName != "" {
		namePtr, err = windows.UTF16PtrFromString(deviceName)
		if err != nil {
			return nil
		}
	}

	var devices []displayDeviceW
	for index := uint32(0); index < 64; index++ {
		device := displayDeviceW{CB: uint32(unsafe.Sizeof(displayDeviceW{}))}
		ok, _, _ := proc.Call(uintptr(unsafe.Pointer(namePtr)), uintptr(index),
			uintptr(unsafe.Pointer(&device)), 0)
		if ok == 0 {
			break
		}
		devices = append(devices, device)
	}
	return devices
}

// enumerateDisplaySettings reads a display mode of a device.
func enumerateDisplaySettings(deviceName string) (devModeW, bool) {
	proc, err := winapi.Proc(user32, "EnumDisplaySettingsW")
	if err != nil {
		return devModeW{}, false
	}
	namePtr, err := windows.UTF16PtrFromString(deviceName)
	if err != nil {
		return devModeW{}, false
	}
	mode := devModeW{Size: uint16(unsafe.Sizeof(devModeW{}))}
	ok, _, _ := proc.Call(uintptr(unsafe.Pointer(namePtr)), uintptr(enumCurrentSettings),
		uintptr(unsafe.Pointer(&mode)))
	if ok == 0 {
		return devModeW{}, false
	}
	return mode, true
}

// luid is the Win32 LUID structure.
type luid struct {
	LowPart  uint32
	HighPart int32
}

// displayConfigRational is the Win32 DISPLAYCONFIG_RATIONAL structure.
type displayConfigRational struct {
	Numerator   uint32
	Denominator uint32
}

// displayConfigPathSourceInfo describes the source of a display path.
type displayConfigPathSourceInfo struct {
	AdapterId   luid
	ID          uint32
	ModeInfoIdx uint32
	Status      uint32
}

// displayConfigPathTargetInfo describes the target of a display path.
type displayConfigPathTargetInfo struct {
	AdapterId        luid
	ID               uint32
	ModeInfoIdx      uint32
	OutputTechnology uint32
	Rotation         uint32
	Scaling          uint32
	RefreshRate      displayConfigRational
	ScanLineOrdering uint32
	TargetAvailable  uint32
	Status           uint32
}

// displayConfigPathInfo describes one active display path.
type displayConfigPathInfo struct {
	SourceInfo displayConfigPathSourceInfo
	TargetInfo displayConfigPathTargetInfo
	Flags      uint32
}

// displayConfigDeviceInfoHeader is the common header of the display config
// device info queries.
type displayConfigDeviceInfoHeader struct {
	Type      uint32
	Size      uint32
	AdapterId luid
	ID        uint32
}

// displayConfigTargetDeviceInfo carries the colour space of a target.
type displayConfigTargetDeviceInfo struct {
	Header     displayConfigDeviceInfoHeader
	ColorSpace uint32
}

// displaySupportsHDR reports whether an attached display uses an HDR colour
// space.
func displaySupportsHDR() (bool, bool) {
	query, err := winapi.Proc(user32, "QueryDisplayConfig")
	if err != nil {
		return false, false
	}
	getInfo, err := winapi.Proc(user32, "DisplayConfigGetDeviceInfo")
	if err != nil {
		return false, false
	}

	const (
		qdcOnlyActivePaths  = 0x2
		qdcOnlyCurrent      = 0x4
		errorSuccess        = 0
		errorInsufficient   = 122
		getTargetColorSpace = 5
		colorSpaceHDR2084   = 12
	)
	flags := uintptr(qdcOnlyActivePaths | qdcOnlyCurrent)

	var pathCount, modeCount uint32
	status, _, _ := query.Call(flags, uintptr(unsafe.Pointer(&pathCount)), 0,
		uintptr(unsafe.Pointer(&modeCount)), 0, 0)
	if status != errorInsufficient && status != errorSuccess {
		return false, false
	}
	if pathCount == 0 {
		return false, false
	}

	paths := make([]displayConfigPathInfo, pathCount)
	status, _, _ = query.Call(flags, uintptr(unsafe.Pointer(&pathCount)),
		uintptr(unsafe.Pointer(&paths[0])), uintptr(unsafe.Pointer(&modeCount)), 0, 0)
	if status != errorSuccess {
		return false, false
	}

	for i := range paths {
		info := displayConfigTargetDeviceInfo{
			Header: displayConfigDeviceInfoHeader{
				Type:      getTargetColorSpace,
				Size:      uint32(unsafe.Sizeof(displayConfigTargetDeviceInfo{})),
				AdapterId: paths[i].TargetInfo.AdapterId,
				ID:        paths[i].TargetInfo.ID,
			},
		}
		ok, _, _ := getInfo.Call(uintptr(unsafe.Pointer(&info.Header)))
		if ok == 0 {
			continue
		}
		if info.ColorSpace == colorSpaceHDR2084 {
			return true, true
		}
	}
	return false, true
}

// utf16ToString converts a UTF-16 buffer into a Go string.
func utf16ToString(buf []uint16) string {
	for i, c := range buf {
		if c == 0 {
			return windows.UTF16ToString(buf[:i])
		}
	}
	return windows.UTF16ToString(buf)
}
