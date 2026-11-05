// Package model defines the JSON contract that the scanner executable serves
// to the website. The field names and nullability mirror
// `src/lib/types/hardware.ts` in the website repository one-for-one.
package model

// Contract constants.
const (
	// SchemaVersion is the version of the JSON document shape.
	SchemaVersion = 1
	// ScannerVersion identifies the scanner build.
	ScannerVersion = "1.2.2"
	// ScannerSource marks the document as produced by the executable.
	ScannerSource = "exe"
)

// HardwareProfile is the complete document served by the scanner.
type HardwareProfile struct {
	SchemaVersion  int              `json:"schemaVersion"`
	CapturedAt     int64            `json:"capturedAt"`
	CPU            CpuInfo          `json:"cpu"`
	GPU            GpuInfo          `json:"gpu"`
	RAM            RamInfo          `json:"ram"`
	Storage        []DriveInfo      `json:"storage"`
	OS             OsInfo           `json:"os"`
	Display        DisplayInfo      `json:"display"`
	Motherboard    *MotherboardInfo `json:"motherboard"`
	System         SystemInfo       `json:"system"`
	Network        *NetworkInfo     `json:"network"`
	Audio          *AudioInfo       `json:"audio"`
	ScannerVersion string           `json:"scannerVersion"`
	ScannerSource  string           `json:"scannerSource"`
}

// CpuInfo describes the host processor.
type CpuInfo struct {
	Name            string   `json:"name"`
	Manufacturer    string   `json:"manufacturer"`
	Architecture    string   `json:"architecture"`
	Cores           int      `json:"cores"`
	Threads         int      `json:"threads"`
	BaseClockGhz    *float64 `json:"baseClockGhz"`
	BoostClockGhz   *float64 `json:"boostClockGhz"`
	CacheL1Kb       *int     `json:"cacheL1Kb"`
	CacheL2Kb       *int     `json:"cacheL2Kb"`
	CacheL3Kb       *int     `json:"cacheL3Kb"`
	InstructionSets []string `json:"instructionSets"`
	Socket          *string  `json:"socket"`
	Virtualization  *bool    `json:"virtualization"`
	Score           *float64 `json:"score"`
	ScoreSource     string   `json:"scoreSource"`
}

// GpuInfo describes the primary graphics adapter and its capabilities.
type GpuInfo struct {
	Name                    string       `json:"name"`
	Manufacturer            string       `json:"manufacturer"`
	DedicatedVramGb         *float64     `json:"dedicatedVramGb"`
	SharedVramGb            *float64     `json:"sharedVramGb"`
	TotalVramGb             *float64     `json:"totalVramGb"`
	DriverVersion           *string      `json:"driverVersion"`
	DriverDate              *string      `json:"driverDate"`
	DirectXVersion          *string      `json:"directXVersion"`
	FeatureLevel            *string      `json:"featureLevel"`
	ShaderModel             *string      `json:"shaderModel"`
	VulkanVersion           *string      `json:"vulkanVersion"`
	OpenGlVersion           *string      `json:"openGlVersion"`
	SupportsRayTracing      bool         `json:"supportsRayTracing"`
	SupportsDlss            bool         `json:"supportsDlss"`
	SupportsFsr             bool         `json:"supportsFsr"`
	SupportsXess            bool         `json:"supportsXess"`
	SupportsFrameGeneration bool         `json:"supportsFrameGeneration"`
	SupportsMeshShaders     bool         `json:"supportsMeshShaders"`
	ShaderUnits             *int         `json:"shaderUnits"`
	CudaCores               *int         `json:"cudaCores"`
	StreamProcessors        *int         `json:"streamProcessors"`
	CoreClockMhz            *int         `json:"coreClockMhz"`
	MemoryClockMhz          *int         `json:"memoryClockMhz"`
	MemoryType              *string      `json:"memoryType"`
	MemoryBusWidthBits      *int         `json:"memoryBusWidthBits"`
	PixelShaderVersion      *string      `json:"pixelShaderVersion"`
	VertexShaderVersion     *string      `json:"vertexShaderVersion"`
	SupportsGeometryShaders *bool        `json:"supportsGeometryShaders"`
	SupportsComputeShaders  *bool        `json:"supportsComputeShaders"`
	SupportsTessellation    *bool        `json:"supportsTessellation"`
	Adapters                []GpuAdapter `json:"adapters"`
	IsPrimary               bool         `json:"isPrimary"`
	Score                   *float64     `json:"score"`
	ScoreSource             string       `json:"scoreSource"`
}

// GpuAdapter is one entry of the full adapter enumeration.
type GpuAdapter struct {
	Name            string   `json:"name"`
	Manufacturer    string   `json:"manufacturer"`
	DedicatedVramGb *float64 `json:"dedicatedVramGb"`
	SharedVramGb    *float64 `json:"sharedVramGb"`
	DriverVersion   *string  `json:"driverVersion"`
	IsIntegrated    *bool    `json:"isIntegrated"`
	IsActive        *bool    `json:"isActive"`
}

// RamInfo describes system memory.
type RamInfo struct {
	TotalGb       float64     `json:"totalGb"`
	AvailableGb   *float64    `json:"availableGb"`
	SpeedMhz      *int        `json:"speedMhz"`
	RatedSpeedMhz *int        `json:"ratedSpeedMhz"`
	Type          *string     `json:"type"`
	Channels      *int        `json:"channels"`
	ModuleCount   *int        `json:"moduleCount"`
	Modules       []RamModule `json:"modules"`
}

// RamModule is a single physical memory module.
type RamModule struct {
	Slot         *string  `json:"slot"`
	CapacityGb   *float64 `json:"capacityGb"`
	SpeedMhz     *int     `json:"speedMhz"`
	Type         *string  `json:"type"`
	Manufacturer *string  `json:"manufacturer"`
	PartNumber   *string  `json:"partNumber"`
}

// DriveInfo describes one physical storage device.
type DriveInfo struct {
	Model          *string  `json:"model"`
	BusType        string   `json:"busType"`
	MediaType      string   `json:"mediaType"`
	SizeGb         float64  `json:"sizeGb"`
	FreeSpaceGb    *float64 `json:"freeSpaceGb"`
	Partition      *string  `json:"partition"`
	ReadSpeedMbps  *int     `json:"readSpeedMbps"`
	WriteSpeedMbps *int     `json:"writeSpeedMbps"`
}

// OsInfo describes the operating system.
type OsInfo struct {
	Caption        *string `json:"caption"`
	Version        *string `json:"version"`
	Build          *string `json:"build"`
	DisplayVersion *string `json:"displayVersion"`
	Architecture   *string `json:"architecture"`
	Is64Bit        bool    `json:"is64Bit"`
	InstallDate    *string `json:"installDate"`
	DirectXVersion *string `json:"directXVersion"`
	KernelVersion  *string `json:"kernelVersion"`
}

// DisplayInfo describes the attached display topology.
type DisplayInfo struct {
	ResolutionWidth  *int          `json:"resolutionWidth"`
	ResolutionHeight *int          `json:"resolutionHeight"`
	RefreshRateHz    *int          `json:"refreshRateHz"`
	MonitorCount     int           `json:"monitorCount"`
	Monitors         []MonitorInfo `json:"monitors"`
	Primary          *MonitorInfo  `json:"primary"`
	SupportsHdr      *bool         `json:"supportsHdr"`
	ColorDepthBits   *int          `json:"colorDepthBits"`
}

// MonitorInfo describes one attached monitor.
type MonitorInfo struct {
	Name          *string `json:"name"`
	Width         *int    `json:"width"`
	Height        *int    `json:"height"`
	RefreshRateHz *int    `json:"refreshRateHz"`
	Hdr           *bool   `json:"hdr"`
	Primary       bool    `json:"primary"`
}

// MotherboardInfo describes the mainboard and its firmware.
type MotherboardInfo struct {
	Manufacturer *string `json:"manufacturer"`
	Product      *string `json:"product"`
	Version      *string `json:"version"`
	BiosVendor   *string `json:"biosVendor"`
	BiosVersion  *string `json:"biosVersion"`
	BiosDate     *string `json:"biosDate"`
}

// SystemInfo describes the machine class and power state.
type SystemInfo struct {
	Manufacturer          *string `json:"manufacturer"`
	Model                 *string `json:"model"`
	IsLaptop              bool    `json:"isLaptop"`
	IsDesktop             bool    `json:"isDesktop"`
	ChassisType           *string `json:"chassisType"`
	PowerMode             string  `json:"powerMode"`
	BatteryPresent        bool    `json:"batteryPresent"`
	OnAcPower             *bool   `json:"onAcPower"`
	PowerPlan             *string `json:"powerPlan"`
	ThermalThrottlingRisk string  `json:"thermalThrottlingRisk"`
}

// NetworkInfo describes the primary network adapter.
type NetworkInfo struct {
	AdapterName    *string `json:"adapterName"`
	ConnectionType string  `json:"connectionType"`
	SpeedMbps      *int    `json:"speedMbps"`
	IsConnected    bool    `json:"isConnected"`
}

// AudioInfo describes the default audio endpoint.
type AudioInfo struct {
	DeviceName   *string `json:"deviceName"`
	Channels     *int    `json:"channels"`
	SampleRateHz *int    `json:"sampleRateHz"`
	BitDepth     *int    `json:"bitDepth"`
}
