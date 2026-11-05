package hardware

import (
	"golang.org/x/sys/windows"
	"runtime"
	"strings"

	"github.com/unp4tched/cypri-scanner/internal/model"
	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

// Processor feature flags of IsProcessorFeaturePresent.
const (
	pfSSE3            = 13
	pfSSSE3           = 36
	pfSSE41           = 37
	pfSSE42           = 38
	pfAVX             = 39
	pfAVX2            = 40
	pfAVX512F         = 41
	pfVirtFirmware    = 21
	pfSecondLevelAddr = 20
)

// win32Processor mirrors the Win32_Processor WMI class.
type win32Processor struct {
	Name                             string `wmi:"Name"`
	Manufacturer                     string `wmi:"Manufacturer"`
	Description                      string `wmi:"Description"`
	NumberOfCores                    uint32 `wmi:"NumberOfCores"`
	NumberOfLogicalProcessors        uint32 `wmi:"NumberOfLogicalProcessors"`
	MaxClockSpeed                    uint32 `wmi:"MaxClockSpeed"`
	CurrentClockSpeed                uint32 `wmi:"CurrentClockSpeed"`
	L1CacheSize                      uint32 `wmi:"L1CacheSize"`
	L2CacheSize                      uint32 `wmi:"L2CacheSize"`
	L3CacheSize                      uint32 `wmi:"L3CacheSize"`
	SocketDesignation                string `wmi:"SocketDesignation"`
	AddressWidth                     uint16 `wmi:"AddressWidth"`
	VirtualizationFirmwareEnabled    bool   `wmi:"VirtualizationFirmwareEnabled"`
	SecondLevelAddressTranslationExt bool   `wmi:"SecondLevelAddressTranslationExtensions"`
	VMMonitorModeExtensions          bool   `wmi:"VMMonitorModeExtensions"`
}

// win32Cache mirrors the Win32_CacheMemory WMI class.
type win32Cache struct {
	Level         uint16 `wmi:"Level"`
	InstalledSize uint32 `wmi:"InstalledSize"`
	BlockSize     uint32 `wmi:"BlockSize"`
	Associativity uint16 `wmi:"Associativity"`
}

// win32ComputerSystem mirrors the Win32_ComputerSystem WMI class.
type win32ComputerSystem struct {
	Manufacturer        string `wmi:"Manufacturer"`
	Model               string `wmi:"Model"`
	TotalPhysicalMemory uint64 `wmi:"TotalPhysicalMemory"`
	HypervisorPresent   bool   `wmi:"HypervisorPresent"`
	PCSystemType        uint32 `wmi:"PCSystemType"`
}

// collectCPU gathers the processor description.
func collectCPU() model.CpuInfo {
	cpu := model.CpuInfo{
		Name:            "Unknown CPU",
		Manufacturer:    "Unknown",
		Architecture:    architectureName(),
		InstructionSets: []string{},
		ScoreSource:     "unknown",
	}

	processors := queryStructs[win32Processor]("SELECT * FROM Win32_Processor")
	if len(processors) == 0 {
		return cpu
	}
	p := processors[0]

	cpu.Name = firstNonEmpty(p.Name, p.Description, "Unknown CPU")
	cpu.Manufacturer = cpuManufacturer(p.Manufacturer, cpu.Name)
	cpu.Cores = int(p.NumberOfCores)
	cpu.Threads = int(p.NumberOfLogicalProcessors)
	if cpu.Threads == 0 {
		cpu.Threads = cpu.Cores
	}
	cpu.Socket = socketName(p.SocketDesignation)
	cpu.InstructionSets = instructionSets()
	cpu.Virtualization = virtualizationSupport(p)

	if p.MaxClockSpeed > 0 {
		cpu.BaseClockGhz = pFloat(roundTo(float64(p.MaxClockSpeed)/1000, 3))
	}
	if boost, ok := boostClockGhz(cpu.Name); ok {
		cpu.BoostClockGhz = pFloat(boost)
	}

	l1, l2, l3 := cacheSizes(p)
	cpu.CacheL1Kb = l1
	cpu.CacheL2Kb = l2
	cpu.CacheL3Kb = l3
	return cpu
}

func cacheSizes(p win32Processor) (*int, *int, *int) {
	l1, l2, l3 := p.L1CacheSize, p.L2CacheSize, p.L3CacheSize
	if l1 == 0 || l2 == 0 || l3 == 0 {
		sizes, maxLevel := cacheMemorySizes()
		// The cache level of Win32_CacheMemory is not the level itself
		// but a code that depends on the machine. The highest code that
		// occurs shows where the levels start.
		offset := 0
		switch {
		case maxLevel >= 5:
			offset = 2
		case maxLevel == 4:
			offset = 1
		}
		if value := sizes[1+offset]; value > 0 && l1 == 0 {
			l1 = value
		}
		if value := sizes[2+offset]; value > 0 && l2 == 0 {
			l2 = value
		}
		if value := sizes[3+offset]; value > 0 && l3 == 0 {
			l3 = value
		}
	}
	var out1, out2, out3 *int
	if l1 > 0 {
		out1 = pInt(int(l1))
	}
	if l2 > 0 {
		out2 = pInt(int(l2))
	}
	if l3 > 0 {
		out3 = pInt(int(l3))
	}
	return out1, out2, out3
}

// cacheMemorySizes reads the cache sizes of Win32_CacheMemory.
//
// A machine reports several caches per level, so the biggest one of a level
// is used.
func cacheMemorySizes() (map[int]uint32, uint16) {
	sizes := map[int]uint32{}
	var maxLevel uint16
	for _, cache := range queryStructs[win32Cache]("SELECT * FROM Win32_CacheMemory") {
		if cache.InstalledSize == 0 {
			continue
		}
		if cache.Level > maxLevel {
			maxLevel = cache.Level
		}
		level := int(cache.Level)
		if cache.InstalledSize > sizes[level] {
			sizes[level] = cache.InstalledSize
		}
	}
	return sizes, maxLevel
}

func cpuManufacturer(manufacturer, name string) string {
	switch {
	case strings.Contains(strings.ToLower(manufacturer), "genuineintel"),
		containsAny(name, "intel", "core i", "core 2", "pentium", "celeron", "xeon"):
		return "Intel"
	case strings.Contains(strings.ToLower(manufacturer), "authenticamd"),
		containsAny(name, "amd", "ryzen", "epyc", "athlon", "threadripper"):
		return "AMD"
	default:
		return "Unknown"
	}
}

func socketName(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(strings.ToLower(value), "unknown") ||
		strings.Contains(strings.ToLower(value), "to be filled") ||
		strings.Contains(strings.ToLower(value), "default string") {
		return nil
	}
	return pString(value)
}

func virtualizationSupport(p win32Processor) *bool {
	if p.VirtualizationFirmwareEnabled || p.SecondLevelAddressTranslationExt || p.VMMonitorModeExtensions {
		return pBool(true)
	}
	systems := queryStructs[win32ComputerSystem]("SELECT * FROM Win32_ComputerSystem")
	if len(systems) > 0 && systems[0].HypervisorPresent {
		return pBool(true)
	}
	return nil
}

// instructionSets reads the CPU feature flags Windows exposes.
func instructionSets() []string {
	sets := []string{}
	if runtime.GOARCH == "amd64" || runtime.GOARCH == "386" {
		sets = append(sets, "SSE2")
	}
	if processorFeaturePresent(pfSSE3) {
		sets = append(sets, "SSE3")
	}
	if processorFeaturePresent(pfSSSE3) {
		sets = append(sets, "SSSE3")
	}
	if processorFeaturePresent(pfSSE41) {
		sets = append(sets, "SSE4.1")
	}
	if processorFeaturePresent(pfSSE42) {
		sets = append(sets, "SSE4.2")
	}
	avx2 := processorFeaturePresent(pfAVX2)
	if processorFeaturePresent(pfAVX) {
		sets = append(sets, "AVX")
	}
	if avx2 {
		sets = append(sets, "AVX2")
		// Every AVX2 capable x86 CPU also implements the FMA3 extension.
		sets = append(sets, "FMA3")
	}
	if processorFeaturePresent(pfAVX512F) {
		sets = append(sets, "AVX-512")
	}
	return sets
}

func processorFeaturePresent(feature uint32) bool {
	proc, err := winapi.Proc(kernel32, "IsProcessorFeaturePresent")
	if err != nil {
		return false
	}
	r, _, _ := proc.Call(uintptr(feature))
	return r != 0
}

func architectureName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

// boostClockTable maps a CPU model fragment to its maximum turbo clock.
// The values are the vendor published all-core boost frequencies.
var boostClockTable = []struct {
	model string
	ghz   float64
}{
	{"i9-13900k", 5.8}, {"i9-13900kf", 5.8}, {"i9-13900ks", 5.8}, {"i9-13900", 5.8},
	{"i9-12900k", 5.2}, {"i9-12900ks", 5.2}, {"i9-12900", 5.2},
	{"i7-13700k", 5.4}, {"i7-13700kf", 5.4}, {"i7-13700", 5.4},
	{"i7-12700k", 5.2}, {"i7-12700kf", 5.2}, {"i7-12700", 5.2},
	{"i5-13600k", 5.1}, {"i5-13600kf", 5.1}, {"i5-13600", 5.1},
	{"i5-12600k", 4.9}, {"i5-12600kf", 4.9}, {"i5-12600", 4.9},
	{"i5-12400f", 4.4}, {"i5-12400", 4.4}, {"i5-12300", 4.4},
	{"i9-11900k", 5.3}, {"i9-11900kf", 5.3}, {"i9-11900", 5.3},
	{"i7-11700k", 5.3}, {"i7-11700kf", 5.3}, {"i7-11700", 5.3},
	{"i5-11600k", 5.0}, {"i5-11600kf", 5.0}, {"i5-11600", 5.0},
	{"i5-11400f", 4.4}, {"i5-11400", 4.4}, {"i5-11500", 4.9},
	{"i9-10900k", 5.3}, {"i9-10900kf", 5.3}, {"i9-10900", 5.3},
	{"i7-10700k", 5.0}, {"i7-10700kf", 5.0}, {"i7-10700", 5.0},
	{"i5-10600k", 4.8}, {"i5-10600kf", 4.8}, {"i5-10600", 4.8},
	{"i5-10400", 4.3}, {"i5-10500", 4.5},
	{"i9-9900k", 5.0}, {"i9-9900kf", 5.0}, {"i9-9900", 5.0},
	{"i7-9700k", 4.9}, {"i7-9700", 4.9},
	{"i5-9600k", 4.3}, {"i5-9600", 4.3}, {"i5-9500", 3.9},
	{"i7-8700k", 4.7}, {"i7-8700", 4.7}, {"i5-8400", 4.0},
	{"ryzen 9 7950x3d", 5.7}, {"ryzen 9 7950x", 5.7}, {"ryzen 9 7950", 5.7},
	{"ryzen 9 7900x3d", 5.6}, {"ryzen 9 7900x", 5.6}, {"ryzen 9 7900", 5.6},
	{"ryzen 9 9950x", 5.7}, {"ryzen 9 9900x", 5.6},
	{"ryzen 7 9800x3d", 5.2}, {"ryzen 7 9800x", 5.2}, {"ryzen 7 7800x3d", 5.0},
	{"ryzen 7 7700x", 5.4}, {"ryzen 7 7600x", 5.3}, {"ryzen 7 7600", 5.1},
	{"ryzen 9 5950x", 4.9}, {"ryzen 9 5900x", 4.9}, {"ryzen 9 5950", 4.9},
	{"ryzen 7 5800x", 4.9}, {"ryzen 7 5700x3d", 4.1}, {"ryzen 7 5700x", 4.6},
	{"ryzen 5 5600", 4.4}, {"ryzen 5 5600x", 4.6}, {"ryzen 5 4500", 4.0},
	{"ryzen 9 3900x", 4.6}, {"ryzen 9 3950x", 4.7}, {"ryzen 9 5900", 4.9},
	{"ryzen 7 3800x", 4.2}, {"ryzen 7 3700x", 4.1}, {"ryzen 5 3600", 4.1},
	{"ryzen 7 5800x3d", 4.5}, {"ryzen 9 5950x3d", 4.9},
}

// boostClockGhz looks the maximum turbo clock of a CPU model up.
func boostClockGhz(name string) (float64, bool) {
	key := strings.ToLower(name)
	key = strings.Split(key, "@")[0]
	key = strings.TrimSpace(key)
	best := ""
	var value float64
	for _, entry := range boostClockTable {
		if strings.Contains(key, entry.model) && len(entry.model) > len(best) {
			best = entry.model
			value = entry.ghz
		}
	}
	if best == "" {
		return 0, false
	}
	return value, true
}
