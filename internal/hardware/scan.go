package hardware

import (
	"fmt"
	"strings"
	"time"

	"github.com/unp4tched/cypri-scanner/internal/dxdiag"
	"github.com/unp4tched/cypri-scanner/internal/model"
)

// dxdiagTimeout caps the runtime of the dxdiag report.
const dxdiagTimeout = 45 * time.Second

// Logger prints the progress of the scan.
type Logger func(format string, args ...any)

// Scan collects the complete hardware inventory of the machine.
//
// Every subsystem is probed in isolation so that a missing driver or an
// absent device never aborts the scan.
func Scan(log Logger) model.HardwareProfile {
	if log == nil {
		log = func(string, ...any) {}
	}

	profile := model.HardwareProfile{
		SchemaVersion:  model.SchemaVersion,
		CapturedAt:     time.Now().UnixMilli(),
		ScannerVersion: model.ScannerVersion,
		ScannerSource:  model.ScannerSource,
		Storage:        []model.DriveInfo{},
	}

	log("Collecting hardware inventory...")

	report := ProbeValue(func() (*dxdiag.Report, error) { return dxdiag.Run(dxdiagTimeout) })
	if report == nil {
		log("  dxdiag unavailable - continuing with the remaining probes")
	}

	profile.CPU = ProbeValue(func() (model.CpuInfo, error) { return collectCPU(), nil })
	log("CPU: %s", describeCPU(profile.CPU))

	profile.GPU = ProbeValue(func() (model.GpuInfo, error) { return collectGPU(report), nil })
	log("GPU: %s", describeGPU(profile.GPU))

	profile.RAM = ProbeValue(func() (model.RamInfo, error) { return collectRAM(), nil })
	log("RAM: %s", describeRAM(profile.RAM))

	profile.Storage = ProbeValue(func() ([]model.DriveInfo, error) { return collectStorage(), nil })
	if profile.Storage == nil {
		profile.Storage = []model.DriveInfo{}
	}
	for _, drive := range profile.Storage {
		log("Storage: %s", describeDrive(drive))
	}

	profile.OS = ProbeValue(func() (model.OsInfo, error) { return collectOS(report), nil })
	log("OS: %s", describeOS(profile.OS))

	profile.Display = ProbeValue(func() (model.DisplayInfo, error) { return collectDisplay(), nil })
	log("Display: %s", describeDisplay(profile.Display))

	profile.Motherboard = ProbeValue(func() (*model.MotherboardInfo, error) { return collectMotherboard(), nil })
	log("Motherboard: %s", describeMotherboard(profile.Motherboard))

	profile.System = ProbeValue(func() (model.SystemInfo, error) { return collectSystem(), nil })
	log("System: %s", describeSystem(profile.System))

	profile.Network = ProbeValue(func() (*model.NetworkInfo, error) { return collectNetwork(), nil })
	log("Network: %s", describeNetworkAdapter(profile.Network))

	profile.Audio = ProbeValue(func() (*model.AudioInfo, error) { return collectAudio(), nil })
	log("Audio: %s", describeAudio(profile.Audio))

	return profile
}

func describeCPU(cpu model.CpuInfo) string {
	if cpu.Name == "" {
		return "unknown"
	}
	return cpu.Name
}

func describeGPU(gpu model.GpuInfo) string {
	details := []string{}
	if gpu.DedicatedVramGb != nil && *gpu.DedicatedVramGb > 0 {
		details = append(details, fmt.Sprintf("%.0f GB", *gpu.DedicatedVramGb))
	}
	if gpu.FeatureLevel != nil {
		details = append(details, "DX"+*gpu.FeatureLevel)
	}
	if len(details) == 0 {
		return gpu.Name
	}
	return gpu.Name + " (" + strings.Join(details, ", ") + ")"
}

func describeRAM(ram model.RamInfo) string {
	parts := []string{fmt.Sprintf("%.0f GB", ram.TotalGb)}
	if ram.ModuleCount != nil {
		parts = append(parts, fmt.Sprintf("%d module(s)", *ram.ModuleCount))
	}
	if ram.Type != nil {
		parts = append(parts, *ram.Type)
	}
	return strings.Join(parts, ", ")
}

func describeDrive(drive model.DriveInfo) string {
	name := "unknown drive"
	if drive.Model != nil {
		name = *drive.Model
	}
	parts := []string{name, drive.MediaType, drive.BusType}
	return strings.Join(parts, " - ")
}

func describeOS(os model.OsInfo) string {
	if os.Caption != nil {
		return *os.Caption
	}
	return "unknown"
}

func describeDisplay(display model.DisplayInfo) string {
	if display.ResolutionWidth == nil || display.ResolutionHeight == nil {
		return fmt.Sprintf("%d monitor(s)", display.MonitorCount)
	}
	parts := []string{fmt.Sprintf("%dx%d", *display.ResolutionWidth, *display.ResolutionHeight)}
	if display.RefreshRateHz != nil {
		parts = append(parts, fmt.Sprintf("%d Hz", *display.RefreshRateHz))
	}
	parts = append(parts, fmt.Sprintf("%d monitor(s)", display.MonitorCount))
	return strings.Join(parts, " - ")
}

func describeMotherboard(board *model.MotherboardInfo) string {
	if board == nil {
		return "unknown"
	}
	manufacturer := ""
	product := ""
	if board.Manufacturer != nil {
		manufacturer = *board.Manufacturer
	}
	if board.Product != nil {
		product = *board.Product
	}
	if product == "" {
		return "unknown"
	}
	return strings.TrimSpace(manufacturer + " " + product)
}

func describeSystem(system model.SystemInfo) string {
	kind := "unknown"
	if system.IsLaptop {
		kind = "Laptop"
	} else if system.IsDesktop {
		kind = "Desktop"
	}
	name := ""
	if system.Model != nil {
		name = *system.Model
	}
	return strings.TrimSpace(name + " (" + kind + ", " + system.PowerMode + ")")
}

func describeAudio(audio *model.AudioInfo) string {
	if audio == nil {
		return "none"
	}
	name := "unknown device"
	if audio.DeviceName != nil {
		name = *audio.DeviceName
	}
	if audio.Channels != nil {
		return fmt.Sprintf("%s (%d channels)", name, *audio.Channels)
	}
	return name
}
