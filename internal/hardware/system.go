package hardware

import (
	"context"
	"golang.org/x/sys/windows/registry"
	"os/exec"
	"strings"
	"time"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/model"
	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

// win32SystemEnclosure mirrors the Win32_SystemEnclosure WMI class.
type win32SystemEnclosure struct {
	ChassisTypes   []int32 `wmi:"ChassisTypes"`
	Manufacturer   string  `wmi:"Manufacturer"`
	Model          string  `wmi:"Model"`
	SMBIOSAssetTag string  `wmi:"SMBIOSAssetTag"`
}

// win32Battery mirrors the Win32_Battery WMI class.
type win32Battery struct {
	Name                     string `wmi:"Name"`
	DeviceID                 string `wmi:"DeviceID"`
	BatteryStatus            uint16 `wmi:"BatteryStatus"`
	EstimatedChargeRemaining uint16 `wmi:"EstimatedChargeRemaining"`
	Status                   string `wmi:"Status"`
}

// systemPowerStatus is the Win32 SYSTEM_POWER_STATUS structure.
type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// Power plan identifiers of the built-in Windows schemes.
const (
	planHighPerformance     = "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c"
	planBalanced            = "381b4222-f694-41f0-9685-ff5bb260df2e"
	planPowerSaver          = "a1841308-3541-4fab-bc81-f71556f20b4a"
	planUltimatePerformance = "e9a42b02-d5df-448d-aa00-03f14749eb61"
)

// collectSystem gathers the machine class and the power state.
func collectSystem() model.SystemInfo {
	system := model.SystemInfo{
		PowerMode:             "unknown",
		ThermalThrottlingRisk: "unknown",
	}

	computers := queryStructs[win32ComputerSystem]("SELECT * FROM Win32_ComputerSystem")
	enclosures := queryStructs[win32SystemEnclosure]("SELECT * FROM Win32_SystemEnclosure")
	batteries := queryStructs[win32Battery]("SELECT * FROM Win32_Battery")

	if len(computers) > 0 {
		if value := strings.TrimSpace(computers[0].Manufacturer); value != "" {
			system.Manufacturer = pString(value)
		}
		if value := strings.TrimSpace(computers[0].Model); value != "" {
			system.Model = pString(value)
		}
	}

	chassis := chassisName(enclosures)
	if chassis != "" {
		system.ChassisType = pString(chassis)
	}

	system.BatteryPresent = len(batteries) > 0
	if status, ok := getSystemPowerStatus(); ok {
		if status.ACLineStatus == 1 {
			system.PowerMode = "plugged-in"
			system.OnAcPower = pBool(true)
		} else if status.ACLineStatus == 0 {
			system.PowerMode = "on-battery"
			system.OnAcPower = pBool(false)
		}
		if status.BatteryFlag&0x80 != 0 {
			system.BatteryPresent = true
		}
	}

	if len(computers) > 0 {
		switch computers[0].PCSystemType {
		case 4:
			system.IsLaptop = true
		case 3:
			system.IsDesktop = true
		}
	}
	if chassis != "" {
		switch chassis {
		case "Laptop", "Notebook", "Tablet", "Convertible":
			system.IsLaptop = true
			system.IsDesktop = false
		case "Desktop", "Tower", "Mini PC", "All-in-One":
			system.IsDesktop = true
			system.IsLaptop = false
		}
	}
	if system.BatteryPresent && !system.IsDesktop {
		system.IsLaptop = true
	}

	if plan := activePowerPlan(); plan != "" {
		system.PowerPlan = pString(plan)
	}

	system.ThermalThrottlingRisk = throttlingRisk(system)
	return system
}

// chassisName maps the SMBIOS chassis types onto a readable name.
func chassisName(enclosures []win32SystemEnclosure) string {
	for _, enclosure := range enclosures {
		for _, chassis := range enclosure.ChassisTypes {
			switch chassis {
			case 3, 4, 6, 7:
				return "Desktop"
			case 5:
				return "Pizza Box"
			case 8:
				return "Portable"
			case 9, 10, 14:
				return "Laptop"
			case 13:
				return "All-in-One"
			case 15, 16, 30:
				return "Compact"
			case 23:
				return "Rack Mount"
			case 28:
				return "Tablet"
			case 29:
				return "Convertible"
			case 32:
				return "Embedded PC"
			case 33:
				return "Mini PC"
			case 34:
				return "Stick PC"
			}
		}
	}
	return ""
}

// getSystemPowerStatus reads the AC and battery state of the machine.
func getSystemPowerStatus() (systemPowerStatus, bool) {
	proc, err := winapi.Proc(kernel32, "GetSystemPowerStatus")
	if err != nil {
		return systemPowerStatus{}, false
	}
	status := systemPowerStatus{}
	ok, _, _ := proc.Call(uintptr(unsafe.Pointer(&status)))
	return status, ok != 0
}

// activePowerPlan returns the name of the power plan that is currently used.
func activePowerPlan() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powercfg", "/getactivescheme")
	// powercfg is a console program; hide its window.
	cmd.SysProcAttr = winapi.HiddenProcess()
	out, err := cmd.Output()
	if err == nil {
		line := strings.TrimSpace(string(out))
		if start := strings.Index(line, "("); start >= 0 {
			if end := strings.LastIndex(line, ")"); end > start {
				name := strings.TrimSpace(line[start+1 : end])
				if name != "" {
					return name
				}
			}
		}
	}
	if guid, ok := registryString(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Power\User\PowerSchemes`, "ActivePowerScheme"); ok {
		return powerPlanName(guid)
	}
	return ""
}

// powerPlanName maps a power scheme GUID onto its display name.
func powerPlanName(guid string) string {
	switch strings.ToLower(strings.TrimSpace(guid)) {
	case planHighPerformance:
		return "High performance"
	case planBalanced:
		return "Balanced"
	case planPowerSaver:
		return "Power saver"
	case planUltimatePerformance:
		return "Ultimate performance"
	default:
		return "Custom"
	}
}

// throttlingRisk estimates how likely the machine is to throttle.
func throttlingRisk(system model.SystemInfo) string {
	if system.IsLaptop {
		if system.PowerMode == "on-battery" {
			return "medium"
		}
		return "low"
	}
	if system.IsDesktop {
		return "low"
	}
	return "unknown"
}
