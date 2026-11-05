package hardware

import (
	"regexp"
	"strings"
)

// gpuSpec holds the vendor specific constants of a graphics card model.
//
// The values come from the published specification sheets of the vendors.
// A model that is not listed stays unknown - nothing is guessed.
type gpuSpec struct {
	Cores           int
	MemoryType      string
	BusWidthBits    int
	ShaderModel     string
	RayTracing      bool
	DLSS            bool
	FSR             bool
	XeSS            bool
	FrameGeneration bool
	MeshShaders     bool
}

// nvidiaModels maps a normalised NVIDIA model key to its specification.
var nvidiaModels = map[string]gpuSpec{
	"rtx5090":        {21760, "GDDR7", 512, "6.9", true, true, true, false, true, true},
	"rtx5080":        {10752, "GDDR7", 256, "6.9", true, true, true, false, true, true},
	"rtx5070ti":      {8960, "GDDR7", 256, "6.9", true, true, true, false, true, true},
	"rtx5070":        {6144, "GDDR7", 192, "6.9", true, true, true, false, true, true},
	"rtx5060ti":      {4608, "GDDR7", 128, "6.9", true, true, true, false, true, true},
	"rtx5060":        {3584, "GDDR7", 128, "6.9", true, true, true, false, true, true},
	"rtx5050":        {2560, "GDDR7", 128, "6.9", true, true, true, false, true, true},
	"rtx4090":        {16384, "GDDR6X", 384, "6.6", true, true, true, false, true, true},
	"rtx4080super":   {10240, "GDDR6X", 256, "6.6", true, true, true, false, true, true},
	"rtx4080":        {9728, "GDDR6X", 256, "6.6", true, true, true, false, true, true},
	"rtx4070tisuper": {16384, "GDDR6X", 192, "6.6", true, true, true, false, true, true},
	"rtx4070ti":      {12288, "GDDR6X", 192, "6.6", true, true, true, false, true, true},
	"rtx4070super":   {7168, "GDDR6X", 192, "6.6", true, true, true, false, true, true},
	"rtx4070":        {5888, "GDDR6X", 192, "6.6", true, true, true, false, true, true},
	"rtx4060ti":      {3072, "GDDR6", 128, "6.6", true, true, true, false, true, true},
	"rtx4060":        {3072, "GDDR6", 128, "6.6", true, true, true, false, true, true},
	"rtx3090ti":      {10240, "GDDR6X", 384, "6.6", true, true, true, false, true, false},
	"rtx3090":        {10496, "GDDR6X", 384, "6.6", true, true, true, false, true, false},
	"rtx3080ti":      {10240, "GDDR6X", 384, "6.6", true, true, true, false, true, false},
	"rtx3080":        {8704, "GDDR6X", 320, "6.6", true, true, true, false, true, false},
	"rtx3070ti":      {6144, "GDDR6", 256, "6.6", true, true, true, false, true, false},
	"rtx3070":        {5888, "GDDR6", 256, "6.6", true, true, true, false, true, false},
	"rtx3060ti":      {4864, "GDDR6", 256, "6.6", true, true, true, false, true, false},
	"rtx3060":        {3584, "GDDR6", 192, "6.6", true, true, true, false, true, false},
	"rtx2080ti":      {4352, "GDDR6", 352, "6.5", true, true, true, false, false, false},
	"rtx2080super":   {3072, "GDDR6", 256, "6.5", true, true, true, false, false, false},
	"rtx2080":        {2944, "GDDR6", 256, "6.5", true, true, true, false, false, false},
	"rtx2070super":   {2560, "GDDR6", 256, "6.5", true, true, true, false, false, false},
	"rtx2070":        {2304, "GDDR6", 256, "6.5", true, true, true, false, false, false},
	"rtx2060super":   {2176, "GDDR6", 192, "6.5", true, true, true, false, false, false},
	"rtx2060":        {1920, "GDDR6", 192, "6.5", true, true, true, false, false, false},
	"gtx1660ti":      {1536, "GDDR6", 192, "5.0", false, false, false, false, false, false},
	"gtx1660super":   {1408, "GDDR6", 192, "5.0", false, false, false, false, false, false},
	"gtx1660":        {1408, "GDDR6", 192, "5.0", false, false, false, false, false, false},
	"gtx1650":        {896, "GDDR6", 128, "5.0", false, false, false, false, false, false},
	"gtx1080ti":      {7168, "GDDR5X", 256, "5.1", false, false, false, false, false, false},
	"gtx1080":        {2560, "GDDR5X", 256, "5.1", false, false, false, false, false, false},
	"gtx1070":        {1920, "GDDR5", 256, "5.1", false, false, false, false, false, false},
	"gtx1060ti":      {3584, "GDDR5", 256, "5.1", false, false, false, false, false, false},
	"gtx1060":        {1920, "GDDR5", 192, "5.1", false, false, false, false, false, false},

	"rtx4090-laptop":   {9728, "GDDR6X", 256, "6.6", true, true, true, false, true, true},
	"rtx4080-laptop":   {7168, "GDDR6X", 192, "6.6", true, true, true, false, true, true},
	"rtx4070ti-laptop": {6144, "GDDR6", 128, "6.6", true, true, true, false, true, true},
	"rtx4070-laptop":   {6144, "GDDR6", 128, "6.6", true, true, true, false, true, true},
	"rtx4060ti-laptop": {3072, "GDDR6", 128, "6.6", true, true, true, false, true, true},
	"rtx4060-laptop":   {3072, "GDDR6", 128, "6.6", true, true, true, false, true, true},
	"rtx4050-laptop":   {5120, "GDDR6", 128, "6.6", true, true, true, false, true, true},
	"rtx3080ti-laptop": {8192, "GDDR6X", 256, "6.6", true, true, true, false, true, false},
	"rtx3080-laptop":   {6144, "GDDR6X", 256, "6.6", true, true, true, false, true, false},
	"rtx3070ti-laptop": {6144, "GDDR6", 256, "6.6", true, true, true, false, true, false},
	"rtx3070-laptop":   {5120, "GDDR6", 256, "6.6", true, true, true, false, true, false},
	"rtx3060ti-laptop": {3840, "GDDR6", 128, "6.6", true, true, true, false, true, false},
	"rtx3060-laptop":   {3840, "GDDR6", 128, "6.6", true, true, true, false, true, false},
	"rtx3050ti-laptop": {2560, "GDDR6", 128, "6.6", true, true, true, false, true, false},
	"rtx3050-laptop":   {2048, "GDDR6", 128, "6.6", true, true, true, false, true, false},
	"gtx1660ti-laptop": {1536, "GDDR6", 128, "5.0", false, false, false, false, false, false},
	"gtx1650-laptop":   {896, "GDDR6", 128, "5.0", false, false, false, false, false, false},
}

// amdModels maps a normalised AMD Radeon model key to its specification.
var amdModels = map[string]gpuSpec{
	"rx7900xtx": {6144, "GDDR6", 320, "6.0", true, false, true, false, true, false},
	"rx7900xt":  {5376, "GDDR6", 320, "6.0", true, false, true, false, true, false},
	"rx7900gre": {3072, "GDDR6", 256, "6.0", true, false, true, false, true, false},
	"rx7800xt":  {3840, "GDDR6", 256, "6.0", true, false, true, false, true, false},
	"rx7700xt":  {3840, "GDDR6", 256, "6.0", true, false, true, false, true, false},
	"rx7600xt":  {2048, "GDDR6", 128, "6.0", true, false, true, false, false, false},
	"rx7600":    {2048, "GDDR6", 128, "6.0", true, false, true, false, false, false},
	"rx6950xt":  {5120, "GDDR6", 320, "6.0", true, false, true, false, false, false},
	"rx6900xt":  {5120, "GDDR6", 320, "6.0", true, false, true, false, false, false},
	"rx6800xt":  {3840, "GDDR6", 256, "6.0", true, false, true, false, false, false},
	"rx6800":    {3840, "GDDR6", 256, "6.0", true, false, true, false, false, false},
	"rx6700xt":  {3840, "GDDR6", 256, "6.0", true, false, true, false, false, false},
	"rx6650xt":  {2048, "GDDR6", 128, "6.0", true, false, true, false, false, false},
	"rx6600xt":  {2048, "GDDR6", 128, "6.0", true, false, true, false, false, false},
	"rx6600":    {1792, "GDDR6", 128, "6.0", true, false, true, false, false, false},
	"rx6500xt":  {2048, "GDDR6", 64, "6.0", true, false, true, false, false, false},
	"rx5900":    {5248, "GDDR6", 256, "5.2", false, false, false, false, false, false},
	"rx5800":    {4096, "GDDR6", 256, "5.2", false, false, false, false, false, false},
	"rx5700xt":  {2560, "GDDR6", 256, "5.2", false, false, false, false, false, false},
	"rx5700":    {2048, "GDDR6", 256, "5.2", false, false, false, false, false, false},
	"rx5600xt":  {2304, "GDDR6", 192, "5.2", false, false, false, false, false, false},
	"rx5500xt":  {2048, "GDDR6", 128, "5.2", false, false, false, false, false, false},
}

// intelModels maps a normalised Intel Arc model key to its specification.
var intelModels = map[string]gpuSpec{
	"arca770": {4096, "GDDR6", 256, "6.0", true, false, true, true, false, false},
	"arca750": {5120, "GDDR6", 224, "6.0", true, false, true, true, false, false},
	"arca580": {2048, "GDDR6", 128, "6.0", true, false, true, true, false, false},
	"arca380": {1024, "GDDR6", 128, "6.0", true, false, true, true, false, false},
}

var (
	nvidiaModelRe = regexp.MustCompile(`(?i)\b(rtx|gtx|quadro|titan)\s?(\d{3,4})`)
	amdModelRe    = regexp.MustCompile(`(?i)\b(rx)\s?(\d{3,4})\s?(xtx|xt|gre|xtx)?`)
	intelModelRe  = regexp.MustCompile(`(?i)\barc\s?([a-b])(\d{3})\b`)
)

// gpuCapabilities returns the vendor specific constants of a card model.
func gpuCapabilities(manufacturer, name string) (gpuSpec, bool) {
	switch manufacturer {
	case "NVIDIA":
		key, ok := nvidiaModelKey(name)
		if !ok {
			return gpuSpec{}, false
		}
		spec, ok := nvidiaModels[key]
		return spec, ok
	case "AMD":
		key, ok := amdModelKey(name)
		if !ok {
			return gpuSpec{}, false
		}
		spec, ok := amdModels[key]
		return spec, ok
	case "Intel":
		key, ok := intelModelKey(name)
		if !ok {
			return gpuSpec{}, false
		}
		spec, ok := intelModels[key]
		return spec, ok
	default:
		return gpuSpec{}, false
	}
}

// nvidiaModelKey normalises an NVIDIA model name, for example
// "NVIDIA GeForce RTX 4070 Ti SUPER" into "rtx4070tisuper".
func nvidiaModelKey(name string) (string, bool) {
	match := nvidiaModelRe.FindStringSubmatch(name)
	if match == nil {
		return "", false
	}
	lower := strings.ToLower(name)
	key := strings.ToLower(match[1]) + match[2]

	rest := lower[strings.Index(lower, strings.ToLower(match[2]))+len(match[2]):]
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "ti") {
		key += "ti"
	}
	if strings.Contains(rest, "super") {
		key += "super"
	}
	if isMobileName(name) {
		key += "-laptop"
	}
	return key, true
}

func amdModelKey(name string) (string, bool) {
	match := amdModelRe.FindStringSubmatch(name)
	if match == nil {
		return "", false
	}
	key := "rx" + match[2]
	if suffix := strings.ToLower(strings.TrimSpace(match[3])); suffix != "" {
		key += suffix
	}
	return key, true
}

func intelModelKey(name string) (string, bool) {
	match := intelModelRe.FindStringSubmatch(name)
	if match == nil {
		return "", false
	}
	return "arc" + strings.ToLower(match[1]) + match[2], true
}

// isMobileName reports whether a card name refers to a laptop chip.
func isMobileName(name string) bool {
	return containsAny(name, "laptop", "mobile")
}
