package hardware

import (
	"strings"

	"github.com/unp4tched/cypri-scanner/internal/model"
)

// win32PhysicalMemory mirrors the Win32_PhysicalMemory WMI class.
type win32PhysicalMemory struct {
	BankLabel            string `wmi:"BankLabel"`
	DeviceLocator        string `wmi:"DeviceLocator"`
	Capacity             uint64 `wmi:"Capacity"`
	Speed                uint32 `wmi:"Speed"`
	ConfiguredClockSpeed uint32 `wmi:"ConfiguredClockSpeed"`
	SMBIOSMemoryType     uint16 `wmi:"SMBIOSMemoryType"`
	Manufacturer         string `wmi:"Manufacturer"`
	PartNumber           string `wmi:"PartNumber"`
	SerialNumber         string `wmi:"SerialNumber"`
	FormFactor           uint16 `wmi:"FormFactor"`
}

// win32PhysicalMemoryArray mirrors the Win32_PhysicalMemoryArray WMI class.
type win32PhysicalMemoryArray struct {
	MemoryDevices uint32 `wmi:"MemoryDevices"`
	MaxCapacity   uint64 `wmi:"MaxCapacity"`
}

// collectRAM gathers the system memory inventory.
func collectRAM() model.RamInfo {
	ram := model.RamInfo{
		TotalGb: 0,
		Modules: []model.RamModule{},
	}

	modules := queryStructs[win32PhysicalMemory]("SELECT * FROM Win32_PhysicalMemory")
	systems := queryStructs[win32ComputerSystem]("SELECT * FROM Win32_ComputerSystem")
	operatingSystems := queryStructs[win32OperatingSystem]("SELECT * FROM Win32_OperatingSystem")

	var totalBytes uint64
	for _, module := range modules {
		totalBytes += module.Capacity

		entry := model.RamModule{}
		slot := firstNonEmpty(module.DeviceLocator, module.BankLabel)
		if slot != "" {
			entry.Slot = pString(slot)
		}
		if module.Capacity > 0 {
			entry.CapacityGb = pFloat(roundTo(gib(module.Capacity), 2))
		}
		speed := int(module.ConfiguredClockSpeed)
		if speed == 0 {
			speed = int(module.Speed)
		}
		if speed > 0 {
			entry.SpeedMhz = pInt(speed)
		}
		if memoryType, ok := memoryTypeName(module.SMBIOSMemoryType); ok {
			entry.Type = pString(memoryType)
		}
		if manufacturer := strings.TrimSpace(module.Manufacturer); manufacturer != "" {
			entry.Manufacturer = pString(manufacturer)
		}
		if part := strings.TrimSpace(module.PartNumber); part != "" {
			entry.PartNumber = pString(part)
		}
		ram.Modules = append(ram.Modules, entry)
	}

	if totalBytes > 0 {
		ram.TotalGb = roundTo(gib(totalBytes), 2)
	} else if len(systems) > 0 && systems[0].TotalPhysicalMemory > 0 {
		ram.TotalGb = roundTo(gib(systems[0].TotalPhysicalMemory), 2)
	}

	if len(operatingSystems) > 0 {
		free := operatingSystems[0].FreePhysicalMemory
		if free > 0 {
			ram.AvailableGb = pFloat(roundTo(float64(free)*1024/bytesPerGiB, 2))
		}
	}

	if len(modules) > 0 {
		ram.ModuleCount = pInt(len(modules))

		var speed, rated int
		for _, module := range modules {
			if module.ConfiguredClockSpeed > 0 {
				speed = int(module.ConfiguredClockSpeed)
			} else if module.Speed > 0 {
				speed = int(module.Speed)
			}
			if module.Speed > 0 {
				rated = int(module.Speed)
			}
		}
		if speed > 0 {
			ram.SpeedMhz = pInt(speed)
		}
		if rated > 0 {
			ram.RatedSpeedMhz = pInt(rated)
		}

		if memoryType, ok := memoryTypeName(modules[0].SMBIOSMemoryType); ok {
			ram.Type = pString(memoryType)
		}
		ram.Channels = pInt(channelCount(len(modules)))
	}
	return ram
}

// channelCount estimates the number of memory channels from the number of
// populated modules. Consumer boards are dual channel, server boards
// quad channel, a single module is always single channel.
func channelCount(modules int) int {
	switch {
	case modules <= 0:
		return 0
	case modules == 1:
		return 1
	case modules <= 3:
		return 2
	default:
		return 4
	}
}

// memoryTypeName maps the SMBIOS memory type onto a marketing name.
func memoryTypeName(smBIOS uint16) (string, bool) {
	switch smBIOS {
	case 20:
		return "DDR", true
	case 21, 22:
		return "DDR2", true
	case 24:
		return "DDR3", true
	case 26:
		return "DDR4", true
	case 34:
		return "DDR5", true
	case 35:
		return "LPDDR4", true
	case 36:
		return "LPDDR5", true
	default:
		return "", false
	}
}
