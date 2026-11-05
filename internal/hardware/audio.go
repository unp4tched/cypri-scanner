package hardware

import (
	"golang.org/x/sys/windows"
	"strings"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/model"
	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

var winmm = windows.NewLazySystemDLL("winmm.dll")

// waveFormatEx is the Win32 WAVEFORMATEX structure.
type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	ExtraSize      uint16
}

// waveInCaps is the Win32 WAVEINCAPS structure.
type waveInCaps struct {
	Mid           uint16
	Pid           uint16
	DriverVersion uint32
	Product       [32]uint16
	Formats       uint32
	Channels      uint16
	Reserved      uint16
}

// win32SoundDevice mirrors the Win32_SoundDevice WMI class.
type win32SoundDevice struct {
	Name         string `wmi:"Name"`
	Manufacturer string `wmi:"Manufacturer"`
	ProductName  string `wmi:"ProductName"`
	Status       string `wmi:"Status"`
}

const (
	waveMapper      = 0xFFFFFFFF
	waveFormatQuery = 0x1
	mmsysErrNoError = 0
)

// collectAudio gathers the default audio endpoint.
func collectAudio() *model.AudioInfo {
	audio := &model.AudioInfo{}

	format, ok := defaultWaveFormat()
	if ok {
		if format.Channels > 0 {
			audio.Channels = pInt(int(format.Channels))
		}
		if format.SamplesPerSec > 0 {
			audio.SampleRateHz = pInt(int(format.SamplesPerSec))
		}
		if format.BitsPerSample > 0 {
			audio.BitDepth = pInt(int(format.BitsPerSample))
		}
	}

	name := defaultAudioDeviceName()
	if name == "" {
		name = fallbackSoundDeviceName()
	}
	if name != "" {
		audio.DeviceName = pString(name)
	}

	if audio.DeviceName == nil && audio.Channels == nil {
		return nil
	}
	return audio
}

// defaultWaveFormat queries the format of the default audio device.
func defaultWaveFormat() (waveFormatEx, bool) {
	openIn, err := winapi.Proc(winmm, "waveInOpen")
	if err != nil {
		return waveFormatEx{}, false
	}
	format := waveFormatEx{}
	status, _, _ := openIn.Call(0, waveMapper, uintptr(unsafe.Pointer(&format)), 0, 0, waveFormatQuery)
	if status != mmsysErrNoError {
		openOut, err := winapi.Proc(winmm, "waveOutOpen")
		if err != nil {
			return waveFormatEx{}, false
		}
		status, _, _ = openOut.Call(0, waveMapper, uintptr(unsafe.Pointer(&format)), 0, 0, waveFormatQuery)
		if status != mmsysErrNoError {
			return waveFormatEx{}, false
		}
	}
	return format, true
}

// defaultAudioDeviceName reads the friendly name of the default device.
func defaultAudioDeviceName() string {
	getCaps, err := winapi.Proc(winmm, "waveOutGetDevCapsW")
	if err != nil {
		return ""
	}
	caps := waveInCaps{Product: [32]uint16{}}
	if status, _, _ := getCaps.Call(waveMapper, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); status != mmsysErrNoError {
		getCapsIn, err := winapi.Proc(winmm, "waveInGetDevCapsW")
		if err != nil {
			return ""
		}
		if status, _, _ := getCapsIn.Call(waveMapper, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); status != mmsysErrNoError {
			return ""
		}
	}
	return strings.TrimSpace(utf16ToString(caps.Product[:]))
}

// fallbackSoundDeviceName picks a sound device from WMI.
func fallbackSoundDeviceName() string {
	for _, device := range queryStructs[win32SoundDevice]("SELECT * FROM Win32_SoundDevice") {
		name := strings.TrimSpace(device.Name)
		if name == "" {
			continue
		}
		if containsAny(name, "microsoft remote display", "rdp audio", "remote audio") {
			continue
		}
		return name
	}
	return ""
}
