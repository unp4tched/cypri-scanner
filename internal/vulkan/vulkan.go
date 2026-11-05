// Package vulkan queries the Vulkan runtime for the instance version and the
// physical devices the loader reports.
package vulkan

import (
	"fmt"
	"unsafe"

	"github.com/unp4tched/cypri-scanner/internal/winapi"
	"golang.org/x/sys/windows"
)

var vulkanDll = windows.NewLazySystemDLL("vulkan-1.dll")

// Vulkan constants.
const (
	vkSuccess                   = 0
	vkStructureTypeInstanceInfo = 1

	vkPhysicalDeviceTypeOther      = 0
	vkPhysicalDeviceTypeIntegrated = 1
	vkPhysicalDeviceTypeDiscrete   = 2
	vkPhysicalDeviceTypeVirtual    = 3
	vkPhysicalDeviceTypeCPU        = 4

	maxPhysicalDeviceNameSize = 256
	maxExtensionNameSize      = 256
)

// Device is one physical device reported by the Vulkan loader.
type Device struct {
	Name          string
	APIVersion    string
	APIVersionRaw uint32
	DriverVersion uint32
	VendorID      uint32
	DeviceID      uint32
	DeviceType    uint32
	Extensions    []string
}

// PhysicalDeviceType returns a human readable device type.
func (d Device) PhysicalDeviceType() string {
	switch d.DeviceType {
	case vkPhysicalDeviceTypeIntegrated:
		return "integrated"
	case vkPhysicalDeviceTypeDiscrete:
		return "discrete"
	case vkPhysicalDeviceTypeVirtual:
		return "virtual"
	case vkPhysicalDeviceTypeCPU:
		return "cpu"
	default:
		return "other"
	}
}

// InstanceVersion returns the highest Vulkan version the loader supports.
func InstanceVersion() (string, error) {
	proc, err := winapi.Proc(vulkanDll, "vkEnumerateInstanceVersion")
	if err != nil {
		return "", err
	}
	var version uint32
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&version)))
	if int32(r) < 0 {
		return "", fmt.Errorf("vkEnumerateInstanceVersion failed: %d", int32(r))
	}
	return FormatVersion(version), nil
}

// FormatVersion renders a packed Vulkan API version as "major.minor.patch".
func FormatVersion(version uint32) string {
	major := version >> 22
	minor := (version >> 12) & 0x3FF
	patch := version & 0xFFF
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// instanceCreateInfo is the Win32 layout of VkInstanceCreateInfo.
type instanceCreateInfo struct {
	sType                   uint32
	_                       uint32
	pNext                   uintptr
	flags                   uint32
	_                       uint32
	enabledLayerCount       uint32
	ppEnabledLayerNames     uintptr
	enabledExtensionCount   uint32
	_                       uint32
	ppEnabledExtensionNames uintptr
}

// Enumerate creates a minimal Vulkan instance and lists the physical devices.
func Enumerate() ([]Device, error) {
	if err := vulkanDll.Load(); err != nil {
		return nil, err
	}
	createInstance, err := winapi.Proc(vulkanDll, "vkCreateInstance")
	if err != nil {
		return nil, err
	}
	getProcAddr, err := winapi.Proc(vulkanDll, "vkGetInstanceProcAddr")
	if err != nil {
		return nil, err
	}

	info := instanceCreateInfo{sType: vkStructureTypeInstanceInfo}
	var instance uintptr
	r, _, _ := createInstance.Call(
		uintptr(unsafe.Pointer(&info)),
		0,
		uintptr(unsafe.Pointer(&instance)),
	)
	if int32(r) < 0 {
		return nil, fmt.Errorf("vkCreateInstance failed: %d", int32(r))
	}
	defer destroyInstance(instance, getProcAddr.Addr())

	enumerate, err := instanceProc(getProcAddr.Addr(), instance, "vkEnumeratePhysicalDevices")
	if err != nil {
		return nil, err
	}
	properties, err := instanceProc(getProcAddr.Addr(), instance, "vkGetPhysicalDeviceProperties")
	if err != nil {
		return nil, err
	}
	extensions, err := instanceProc(getProcAddr.Addr(), instance, "vkEnumerateDeviceExtensionProperties")
	if err != nil {
		return nil, err
	}

	var count uint32
	r = call3(enumerate, instance, uintptr(unsafe.Pointer(&count)), 0)
	if int32(r) < 0 || count == 0 {
		return nil, fmt.Errorf("vkEnumeratePhysicalDevices failed: %d", int32(r))
	}

	handles := make([]uintptr, count)
	r = call3(enumerate, instance, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&handles[0])))
	if int32(r) < 0 {
		return nil, fmt.Errorf("vkEnumeratePhysicalDevices failed: %d", int32(r))
	}

	devices := make([]Device, 0, count)
	for _, handle := range handles {
		buf := make([]byte, 2048)
		call2(properties, handle, uintptr(unsafe.Pointer(&buf[0])))

		d := Device{
			APIVersionRaw: readUint32(buf, 0),
			DriverVersion: readUint32(buf, 4),
			VendorID:      readUint32(buf, 8),
			DeviceID:      readUint32(buf, 12),
			DeviceType:    readUint32(buf, 16),
		}
		d.APIVersion = FormatVersion(d.APIVersionRaw)
		// The name follows the four version fields and the device type.
		d.Name = readCString(buf[20 : 20+maxPhysicalDeviceNameSize])
		d.Extensions = deviceExtensions(extensions, handle)
		devices = append(devices, d)
	}
	return devices, nil
}

func deviceExtensions(proc uintptr, handle uintptr) []string {
	var count uint32
	r := call4(proc, handle, 0, uintptr(unsafe.Pointer(&count)), 0)
	if int32(r) < 0 || count == 0 {
		return nil
	}
	size := uint64(count) * 260
	buf := make([]byte, size)
	r = call4(proc, handle, 0, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if int32(r) < 0 {
		return nil
	}
	out := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		off := int(i) * 260
		name := readCString(buf[off : off+maxExtensionNameSize])
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func destroyInstance(instance uintptr, getProcAddr uintptr) {
	proc, err := instanceProc(getProcAddr, instance, "vkDestroyInstance")
	if err != nil {
		return
	}
	call2(proc, instance, 0)
}

// instanceProc resolves an instance level entry point.
func instanceProc(getProcAddr uintptr, instance uintptr, name string) (uintptr, error) {
	// Vulkan expects a plain null terminated byte string, not a UTF-16
	// string.
	buffer := make([]byte, len(name)+1)
	copy(buffer, name)

	p := call2(getProcAddr, instance, uintptr(unsafe.Pointer(&buffer[0])))
	if p == 0 {
		return 0, fmt.Errorf("vulkan entry point %q not found", name)
	}
	return p, nil
}

func call2(fn uintptr, a, b uintptr) uintptr {
	return winapi.Call(fn, a, b)
}

func call3(fn uintptr, a, b, c uintptr) uintptr {
	return winapi.Call(fn, a, b, c)
}

func call4(fn uintptr, a, b, c, d uintptr) uintptr {
	return winapi.Call(fn, a, b, c, d)
}

func readUint32(buf []byte, offset int) uint32 {
	if len(buf) < offset+4 {
		return 0
	}
	return uint32(buf[offset]) | uint32(buf[offset+1])<<8 | uint32(buf[offset+2])<<16 | uint32(buf[offset+3])<<24
}

func readCString(buf []byte) string {
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
