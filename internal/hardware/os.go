package hardware

import (
	"fmt"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"runtime"
	"strings"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/dxdiag"
	"github.com/unp4tched/cypri-scanner/internal/model"
	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

// win32OperatingSystem mirrors the Win32_OperatingSystem WMI class.
type win32OperatingSystem struct {
	Caption                 string `wmi:"Caption"`
	Version                 string `wmi:"Version"`
	BuildNumber             string `wmi:"BuildNumber"`
	OSArchitecture          string `wmi:"OSArchitecture"`
	InstallDate             string `wmi:"InstallDate"`
	FreePhysicalMemory      uint64 `wmi:"FreePhysicalMemory"`
	TotalVisibleMemorySize  uint64 `wmi:"TotalVisibleMemorySize"`
	ServicePackMajorVersion uint32 `wmi:"ServicePackMajorVersion"`
	Locale                  string `wmi:"Locale"`
}

// osVersionInfoExW is the Win32 layout of RTL_OSVERSIONINFOEXW.
type osVersionInfoExW struct {
	OSVersionInfoSize uint32
	MajorVersion      uint32
	MinorVersion      uint32
	BuildNumber       uint32
	PlatformID        uint32
	CSDVersion        [128]uint16
	ServicePackMajor  uint16
	ServicePackMinor  uint16
	SuiteMask         uint16
	ProductType       uint8
	Reserved          uint8
}

const windowsVersionKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

// collectOS gathers the operating system description.
func collectOS(report *dxdiag.Report) model.OsInfo {
	os := model.OsInfo{
		Is64Bit: runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64",
	}

	systems := queryStructs[win32OperatingSystem]("SELECT * FROM Win32_OperatingSystem")
	if len(systems) > 0 {
		if caption := strings.TrimSpace(systems[0].Caption); caption != "" {
			os.Caption = pString(caption)
		}
		if version := strings.TrimSpace(systems[0].Version); version != "" {
			os.Version = pString(version)
		}
		if build := strings.TrimSpace(systems[0].BuildNumber); build != "" {
			os.Build = pString(build)
		}
		if arch := strings.TrimSpace(systems[0].OSArchitecture); arch != "" {
			os.Architecture = pString(arch)
			if strings.Contains(arch, "64") {
				os.Is64Bit = true
			}
		}
		if date := wmiDateString(systems[0].InstallDate); date != nil {
			os.InstallDate = date
		}
	}

	if version, ok := registryString(registry.LOCAL_MACHINE, windowsVersionKey, "CurrentVersion"); ok && version != "" {
		os.Version = pString(version)
	}
	if build, ok := registryString(registry.LOCAL_MACHINE, windowsVersionKey, "CurrentBuildNumber"); ok && build != "" {
		os.Build = pString(build)
	}
	if display, ok := registryString(registry.LOCAL_MACHINE, windowsVersionKey, "DisplayVersion"); ok && display != "" {
		os.DisplayVersion = pString(display)
	} else if release, ok := registryString(registry.LOCAL_MACHINE, windowsVersionKey, "ReleaseId"); ok && release != "" {
		os.DisplayVersion = pString(release)
	}
	if os.Caption == nil {
		if product, ok := registryString(registry.LOCAL_MACHINE, windowsVersionKey, "ProductName"); ok && product != "" {
			os.Caption = pString(product)
		}
	}
	if os.Architecture == nil {
		os.Architecture = pString(runtime.GOARCH)
	}

	if version := directXVersion(report); version != "" {
		os.DirectXVersion = pString(version)
	}
	if kernel := kernelVersion(); kernel != "" {
		os.KernelVersion = pString(kernel)
	}
	return os
}

// directXVersion reads the DirectX version from the registry and falls back
// to what dxdiag reports.
func directXVersion(report *dxdiag.Report) string {
	if version, ok := registryString(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\DirectX`, "Version"); ok {
		if fields := strings.Fields(version); len(fields) > 0 {
			return fields[0]
		}
	}
	if report != nil {
		for _, section := range []string{"System Information", "Display Devices"} {
			for _, pair := range report.Section(section) {
				if strings.EqualFold(pair.Key, "DirectX Version") {
					fields := strings.Fields(pair.Value)
					if len(fields) > 0 {
						return fields[len(fields)-1]
					}
				}
			}
		}
	}
	return ""
}

// kernelVersion reports the exact kernel version including the update
// revision number of the running Windows build.
func kernelVersion() string {
	ntdll := windows.NewLazySystemDLL("ntdll.dll")
	proc, err := winapi.Proc(ntdll, "RtlGetVersion")
	if err != nil {
		return ""
	}
	info := osVersionInfoExW{OSVersionInfoSize: uint32(unsafe.Sizeof(osVersionInfoExW{}))}
	if status, _, _ := proc.Call(uintptr(unsafe.Pointer(&info))); int32(status) < 0 {
		return ""
	}
	version := fmt.Sprintf("%d.%d.%d", info.MajorVersion, info.MinorVersion, info.BuildNumber)
	if ubr, ok := registryString(registry.LOCAL_MACHINE, windowsVersionKey, "UBR"); ok && ubr != "" {
		version += "." + ubr
	}
	return version
}
