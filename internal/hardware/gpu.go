package hardware

import (
	"sort"
	"strings"

	"github.com/unp4tched/cypri-scanner/internal/dxdiag"
	"github.com/unp4tched/cypri-scanner/internal/dxgi"
	"github.com/unp4tched/cypri-scanner/internal/model"
	"github.com/unp4tched/cypri-scanner/internal/nvml"
	"github.com/unp4tched/cypri-scanner/internal/opengl"
	"github.com/unp4tched/cypri-scanner/internal/vulkan"
)

// win32VideoController mirrors the Win32_VideoController WMI class.
type win32VideoController struct {
	Name                        string `wmi:"Name"`
	AdapterCompatibility        string `wmi:"AdapterCompatibility"`
	AdapterRAM                  uint32 `wmi:"AdapterRAM"`
	DriverVersion               string `wmi:"DriverVersion"`
	DriverDate                  string `wmi:"DriverDate"`
	VideoProcessor              string `wmi:"VideoProcessor"`
	VideoModeDescription        string `wmi:"VideoModeDescription"`
	CurrentHorizontalResolution uint32 `wmi:"CurrentHorizontalResolution"`
	CurrentVerticalResolution   uint32 `wmi:"CurrentVerticalResolution"`
	CurrentRefreshRate          uint32 `wmi:"CurrentRefreshRate"`
	Status                      string `wmi:"Status"`
	PNPDeviceID                 string `wmi:"PNPDeviceID"`
}

// collectGPU gathers everything known about the graphics adapters.
func collectGPU(report *dxdiag.Report) model.GpuInfo {
	gpu := model.GpuInfo{
		Name:         "Unknown GPU",
		Manufacturer: "Unknown",
		Adapters:     []model.GpuAdapter{},
		ScoreSource:  "unknown",
	}

	adapters := ProbeValue(dxgi.Enumerate)
	nvmlDevices := ProbeValue(nvml.Query)
	vulkanDevices := ProbeValue(vulkan.Enumerate)
	glInfo := ProbeValue(opengl.Query)
	controllers := queryStructs[win32VideoController]("SELECT * FROM Win32_VideoController")

	if len(adapters) == 0 && len(controllers) == 0 {
		return gpu
	}

	primary := pickPrimaryAdapter(adapters, glInfo.Renderer, controllers, report)

	for i, adapter := range adapters {
		entry := model.GpuAdapter{
			Name:            adapter.Description,
			Manufacturer:    gpuManufacturer(adapter.VendorID, adapter.Description),
			DedicatedVramGb: pFloat(roundTo(gib(adapter.DedicatedVideoMemory), 2)),
			SharedVramGb:    pFloat(roundTo(sharedMemoryGb(adapter), 2)),
			IsIntegrated:    pBool(isIntegratedAdapter(adapter)),
		}
		if controller := matchController(adapter.Description, controllers); controller != nil {
			if v := strings.TrimSpace(controller.DriverVersion); v != "" {
				entry.DriverVersion = pString(v)
			}
		}
		if i == primary {
			entry.IsActive = pBool(true)
		} else if len(adapters) > 1 {
			entry.IsActive = pBool(false)
		}
		gpu.Adapters = append(gpu.Adapters, entry)
	}

	// Fall back to WMI when DXGI is unavailable.
	if len(gpu.Adapters) == 0 {
		for _, controller := range controllers {
			dedicated := float64(controller.AdapterRAM) / bytesPerGiB
			entry := model.GpuAdapter{
				Name:          controller.Name,
				Manufacturer:  gpuManufacturer(0, controller.Name),
				SharedVramGb:  pFloat(0),
				DriverVersion: pString(controller.DriverVersion),
				IsIntegrated:  pBool(containsAny(controller.Name, "uhd graphics", "iris", "vega", "radeon(tm) graphics")),
			}
			if dedicated > 0 {
				entry.DedicatedVramGb = pFloat(roundTo(dedicated, 2))
			}
			gpu.Adapters = append(gpu.Adapters, entry)
		}
	}

	var primaryAdapter *dxgi.Adapter
	if primary >= 0 && primary < len(adapters) {
		primaryAdapter = &adapters[primary]
	}

	name := ""
	if primaryAdapter != nil {
		name = primaryAdapter.Description
	}
	if name == "" && len(controllers) > 0 {
		name = controllers[0].Name
	}
	gpu.Name = firstNonEmpty(name, "Unknown GPU")
	gpu.Manufacturer = gpuManufacturer(vendorIDOf(primaryAdapter), gpu.Name)
	gpu.IsPrimary = true

	applyGPUCapabilities(&gpu, gpu.Manufacturer, gpu.Name)
	applyGPUMemory(&gpu, primaryAdapter, nvmlDevices)
	applyGPUDriver(&gpu, primaryAdapter, controllers, report, nvmlDevices)
	applyGPUAPIs(&gpu, primaryAdapter, glInfo, vulkanDevices, report)
	applyGPUShading(&gpu, gpu.Manufacturer, gpu.Name, report, vulkanDevices)

	return gpu
}

// sharedMemoryGb returns the amount of system memory the adapter can use as
// graphics memory. A discrete card addresses its own memory, so only an
// integrated adapter reports a shared frame buffer.
func sharedMemoryGb(adapter dxgi.Adapter) float64 {
	if adapter.DedicatedVideoMemory > 0 {
		return 0
	}
	return gib(adapter.SharedSystemMemory)
}

func isIntegratedAdapter(adapter dxgi.Adapter) bool {
	if containsAny(adapter.Description, "uhd graphics", "iris", "intel arc graphics", "radeon(tm) graphics", "vega") {
		return true
	}
	return adapter.DedicatedVideoMemory == 0 && adapter.SharedSystemMemory > 0
}

func vendorIDOf(adapter *dxgi.Adapter) uint32 {
	if adapter == nil {
		return 0
	}
	return adapter.VendorID
}

// gpuManufacturer maps a PCI vendor id or a product name to a vendor.
func gpuManufacturer(vendorID uint32, name string) string {
	switch vendorID {
	case dxgi.VendorNVIDIA:
		return "NVIDIA"
	case dxgi.VendorAMD, dxgi.VendorATI:
		return "AMD"
	case dxgi.VendorIntel:
		return "Intel"
	}
	switch {
	case containsAny(name, "nvidia", "geforce", "quadro", "tesla", "rtx ", "gtx "):
		return "NVIDIA"
	case containsAny(name, "radeon", "amd radeon", "rx "):
		return "AMD"
	case containsAny(name, "intel", "arc ", "iris", "uhd graphics"):
		return "Intel"
	default:
		return "Unknown"
	}
}

// pickPrimaryAdapter decides which adapter the user is actually gaming on.
func pickPrimaryAdapter(adapters []dxgi.Adapter, glRenderer string, controllers []win32VideoController, report *dxdiag.Report) int {
	candidates := make([]int, 0, len(adapters))
	for i, adapter := range adapters {
		if adapter.IsSoftware {
			continue
		}
		candidates = append(candidates, i)
	}
	if len(candidates) == 0 {
		for i := range adapters {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return -1
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	activeName := activeAdapterName(glRenderer, controllers, report)
	scores := make([]int, len(candidates))
	for i, index := range candidates {
		scores[i] = adapterScore(adapters[index].Description, activeName)
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		return scores[candidates[a]] > scores[candidates[b]]
	})
	best := candidates[0]
	if scores[best] == 0 {
		// Fall back to the adapter with the most dedicated memory.
		for _, index := range candidates {
			if adapters[index].DedicatedVideoMemory > adapters[best].DedicatedVideoMemory {
				best = index
			}
		}
	}
	return best
}

// activeAdapterName guesses the adapter that currently drives the display.
func activeAdapterName(glRenderer string, controllers []win32VideoController, report *dxdiag.Report) string {
	if glRenderer != "" {
		return glRenderer
	}
	for _, controller := range controllers {
		if controller.CurrentHorizontalResolution > 0 && controller.CurrentVerticalResolution > 0 {
			return controller.Name
		}
	}
	if report != nil {
		for _, card := range report.Cards {
			if mode := card.First("Current Mode"); mode != "" {
				return card.First("Card name")
			}
		}
	}
	return ""
}

// adapterScore rates how well an adapter name matches the active adapter.
func adapterScore(adapterName, activeName string) int {
	if activeName == "" {
		return 0
	}
	adapter := normalizeGPUName(adapterName)
	active := normalizeGPUName(activeName)
	if adapter == "" || active == "" {
		return 0
	}
	if adapter == active {
		return 100
	}
	if strings.Contains(active, adapter) || strings.Contains(adapter, active) {
		return 50
	}
	return 0
}

// normalizeGPUName strips everything but letters and digits.
func normalizeGPUName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// matchController finds the WMI video controller that describes an adapter.
func matchController(name string, controllers []win32VideoController) *win32VideoController {
	target := normalizeGPUName(name)
	for i := range controllers {
		candidate := normalizeGPUName(controllers[i].Name)
		if candidate != "" && candidate == target {
			return &controllers[i]
		}
	}
	for i := range controllers {
		candidate := normalizeGPUName(controllers[i].Name)
		if candidate != "" && (strings.Contains(candidate, target) || strings.Contains(target, candidate)) {
			return &controllers[i]
		}
	}
	return nil
}

// applyGPUCapabilities fills the feature flags from the vendor model table.
func applyGPUCapabilities(gpu *model.GpuInfo, manufacturer, name string) {
	spec, ok := gpuCapabilities(manufacturer, name)
	if !ok {
		return
	}
	gpu.SupportsRayTracing = spec.RayTracing
	gpu.SupportsDlss = spec.DLSS
	gpu.SupportsFsr = spec.FSR
	gpu.SupportsXess = spec.XeSS
	gpu.SupportsFrameGeneration = spec.FrameGeneration
	gpu.SupportsMeshShaders = spec.MeshShaders
	gpu.MemoryType = pString(spec.MemoryType)
	gpu.MemoryBusWidthBits = pInt(spec.BusWidthBits)
	switch manufacturer {
	case "NVIDIA":
		gpu.CudaCores = pInt(spec.Cores)
		gpu.ShaderUnits = pInt(spec.Cores)
	case "AMD":
		gpu.StreamProcessors = pInt(spec.Cores)
		gpu.ShaderUnits = pInt(spec.Cores)
	}
}

// applyGPUMemory fills the memory sizes and the clock values.
func applyGPUMemory(gpu *model.GpuInfo, adapter *dxgi.Adapter, nvmlDevices []nvml.Device) {
	if adapter != nil {
		dedicated := roundTo(gib(adapter.DedicatedVideoMemory), 2)
		shared := roundTo(sharedMemoryGb(*adapter), 2)
		gpu.DedicatedVramGb = pFloat(dedicated)
		gpu.SharedVramGb = pFloat(shared)
		gpu.TotalVramGb = pFloat(roundTo(dedicated+shared, 2))
	}
	if device, ok := matchNVML(nvmlDevices, gpu.Name); ok {
		if device.TotalMemoryBytes > 0 {
			total := roundTo(gib(device.TotalMemoryBytes), 2)
			gpu.DedicatedVramGb = pFloat(total)
			gpu.SharedVramGb = pFloat(0)
			gpu.TotalVramGb = pFloat(total)
		}
		if device.GraphicsClockMhz > 0 {
			gpu.CoreClockMhz = pInt(int(device.GraphicsClockMhz))
		}
		if device.MemoryClockMhz > 0 {
			gpu.MemoryClockMhz = pInt(int(device.MemoryClockMhz))
		}
		if device.HasMemoryBusWidth && device.MemoryBusWidthBits > 0 {
			gpu.MemoryBusWidthBits = pInt(int(device.MemoryBusWidthBits))
		}
	}
}

// applyGPUDriver fills the driver version and date.
func applyGPUDriver(gpu *model.GpuInfo, adapter *dxgi.Adapter, controllers []win32VideoController, report *dxdiag.Report, nvmlDevices []nvml.Device) {
	if device, ok := matchNVML(nvmlDevices, gpu.Name); ok && device.DriverVersion != "" {
		gpu.DriverVersion = pString(device.DriverVersion)
	}
	if gpu.DriverVersion == nil {
		if controller := matchController(gpu.Name, controllers); controller != nil {
			if v := strings.TrimSpace(controller.DriverVersion); v != "" {
				gpu.DriverVersion = pString(v)
			}
		}
	}
	if gpu.DriverVersion == nil && report != nil {
		for _, card := range report.Cards {
			if v := strings.TrimSpace(card.First("Driver Version")); v != "" {
				gpu.DriverVersion = pString(v)
				break
			}
		}
	}
	if report != nil {
		for _, card := range report.Cards {
			for _, key := range []string{"Driver Date (UTC)", "Driver Date/Size"} {
				date := dxdiag.DriverDate(card.First(key))
				if date == "" {
					continue
				}
				gpu.DriverDate = pString(date)
				break
			}
			if gpu.DriverDate != nil {
				break
			}
		}
	}
	if gpu.DriverDate == nil {
		if controller := matchController(gpu.Name, controllers); controller != nil {
			if date := isoDate(controller.DriverDate); date != nil {
				gpu.DriverDate = date
			}
		}
	}
}

// applyGPUAPIs fills the DirectX, Vulkan and OpenGL versions.
func applyGPUAPIs(gpu *model.GpuInfo, adapter *dxgi.Adapter, gl opengl.Info, vulkanDevices []vulkan.Device, report *dxdiag.Report) {
	if version := directXVersion(report); version != "" {
		gpu.DirectXVersion = pString(version)
	}
	if feature := highestFeatureLevel(adapter, report); feature != "" {
		gpu.FeatureLevel = pString(feature)
	}
	if len(vulkanDevices) > 0 {
		if version := vulkanDevices[0].APIVersion; version != "" {
			gpu.VulkanVersion = pString(version)
		}
	} else if version, err := vulkan.InstanceVersion(); err == nil && version != "" {
		gpu.VulkanVersion = pString(version)
	}
	if gl.Version != "" {
		gpu.OpenGlVersion = pString(shortVersion(gl.Version))
	}
}

// applyGPUShading fills the shader model and the shader capabilities.
func applyGPUShading(gpu *model.GpuInfo, manufacturer, name string, report *dxdiag.Report, vulkanDevices []vulkan.Device) {
	shaderModel := ""
	if report != nil {
		shaderModel = reportShaderModel(report)
	}
	if shaderModel == "" {
		if spec, ok := gpuCapabilities(manufacturer, name); ok {
			shaderModel = spec.ShaderModel
		}
	}
	if shaderModel != "" {
		gpu.ShaderModel = pString(shaderModel)
		gpu.PixelShaderVersion = pString(shaderModel)
		gpu.VertexShaderVersion = pString(shaderModel)
	}

	geometry := shaderModelMajor(shaderModel) >= 4
	compute := shaderModelMajor(shaderModel) >= 5
	tessellation := shaderModelMajor(shaderModel) >= 5
	gpu.SupportsGeometryShaders = pBool(geometry)
	gpu.SupportsComputeShaders = pBool(compute)
	gpu.SupportsTessellation = pBool(tessellation)

	if !gpu.SupportsRayTracing && vulkanSupportsRayTracing(vulkanDevices) {
		gpu.SupportsRayTracing = true
	}
}

// reportShaderModel looks for the shader model dxdiag reports.
func reportShaderModel(report *dxdiag.Report) string {
	if report == nil {
		return ""
	}
	for _, section := range []string{"Display Devices", "Direct3D 12", "Direct3D 11", "System Information"} {
		for _, pair := range report.Section(section) {
			if strings.EqualFold(pair.Key, "Shader Model") {
				return strings.TrimSpace(pair.Value)
			}
		}
	}
	return ""
}

// vulkanSupportsRayTracing reports whether a Vulkan device exposes a ray
// tracing pipeline extension.
func vulkanSupportsRayTracing(devices []vulkan.Device) bool {
	for _, device := range devices {
		for _, extension := range device.Extensions {
			if extension == "VK_KHR_ray_tracing_pipeline" || extension == "VK_NV_ray_tracing" {
				return true
			}
		}
	}
	return false
}

// matchNVML finds the NVML device that belongs to a card name.
func matchNVML(devices []nvml.Device, name string) (nvml.Device, bool) {
	target := normalizeGPUName(name)
	for _, device := range devices {
		candidate := normalizeGPUName(device.Name)
		if candidate == "" || target == "" {
			continue
		}
		if candidate == target || strings.Contains(candidate, target) || strings.Contains(target, candidate) {
			return device, true
		}
	}
	return nvml.Device{}, false
}

// highestFeatureLevel returns the best DirectX feature level the adapter
// supports. dxdiag knows the Direct3D 12 levels, the D3D11 device only the
// 11_1 ones.
func highestFeatureLevel(adapter *dxgi.Adapter, report *dxdiag.Report) string {
	if report != nil {
		for _, card := range report.Cards {
			levels := card.FeatureLevels()
			if len(levels) == 0 {
				continue
			}
			best := levels[0]
			for _, level := range levels[1:] {
				if featureLevelRank(level) > featureLevelRank(best) {
					best = level
				}
			}
			return best
		}
	}
	if adapter != nil && adapter.FeatureLevel != "" {
		return adapter.FeatureLevel
	}
	return ""
}

// featureLevelRank orders DirectX feature levels.
func featureLevelRank(level string) int {
	fields := strings.Split(level, "_")
	if len(fields) != 2 {
		return 0
	}
	major := 0
	minor := 0
	for i, c := range fields[0] {
		if c < '0' || c > '9' {
			return 0
		}
		major = major*10 + int(c-'0')
		_ = i
	}
	for _, c := range fields[1] {
		if c < '0' || c > '9' {
			return 0
		}
		minor = minor*10 + int(c-'0')
	}
	return major*100 + minor
}

// shaderModelMajor returns the major version of a shader model string.
func shaderModelMajor(shaderModel string) int {
	fields := strings.Split(shaderModel, ".")
	if len(fields) == 0 {
		return 0
	}
	value := 0
	for _, c := range fields[0] {
		if c < '0' || c > '9' {
			return 0
		}
		value = value*10 + int(c-'0')
	}
	return value
}

// shortVersion trims a version string to its major and minor part.
func shortVersion(version string) string {
	fields := strings.Fields(version)
	if len(fields) == 0 {
		return ""
	}
	parts := strings.Split(fields[0], ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return fields[0]
}
