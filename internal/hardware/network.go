package hardware

import (
	"strconv"
	"strings"

	"github.com/unp4tched/cypri-scanner/internal/model"
)

// msftNetAdapter mirrors the MSFT_NetAdapter WMI class.
type msftNetAdapter struct {
	Name                 string `wmi:"Name"`
	InterfaceDescription string `wmi:"InterfaceDescription"`
	Speed                uint64 `wmi:"Speed"`
	MediaConnectionState uint16 `wmi:"MediaConnectionState"`
	AdapterType          uint32 `wmi:"AdapterType"`
	NdisPhysicalMedium   uint32 `wmi:"NdisPhysicalMedium"`
	InterfaceIndex       uint32 `wmi:"InterfaceIndex"`
	DriverDescription    string `wmi:"DriverDescription"`
	HardwareInterface    bool   `wmi:"HardwareInterface"`
}

// win32NetworkAdapter mirrors the Win32_NetworkAdapter WMI class.
type win32NetworkAdapter struct {
	Name            string `wmi:"Name"`
	NetConnectionID string `wmi:"NetConnectionID"`
	Speed           uint64 `wmi:"Speed"`
	NetEnabled      bool   `wmi:"NetEnabled"`
	AdapterTypeID   uint32 `wmi:"AdapterTypeID"`
	PhysicalAdapter bool   `wmi:"PhysicalAdapter"`
	MACAddress      string `wmi:"MACAddress"`
}

// win32NetworkAdapterConfiguration mirrors the Win32_NetworkAdapterConfiguration
// WMI class.
type win32NetworkAdapterConfiguration struct {
	Description string `wmi:"Description"`
	MACAddress  string `wmi:"MACAddress"`
	IPEnabled   bool   `wmi:"IPEnabled"`
}

// networkCandidate is one adapter that could carry the traffic.
type networkCandidate struct {
	name        string
	description string
	speed       uint64
	connected   bool
	medium      uint32
	hardware    bool
}

// collectNetwork gathers the primary network adapter.
func collectNetwork() *model.NetworkInfo {
	adapters := queryStructsNamespace[msftNetAdapter]("SELECT * FROM MSFT_NetAdapter", `root\StandardCimv2`)
	legacy := queryStructs[win32NetworkAdapter]("SELECT * FROM Win32_NetworkAdapter")

	var candidates []networkCandidate
	for _, adapter := range adapters {
		candidates = append(candidates, networkCandidate{
			name:        adapter.Name,
			description: adapter.InterfaceDescription,
			speed:       adapter.Speed,
			connected:   adapter.MediaConnectionState == 1,
			medium:      adapter.NdisPhysicalMedium,
			hardware:    adapter.HardwareInterface,
		})
	}
	if len(candidates) == 0 {
		for _, adapter := range legacy {
			if !adapter.PhysicalAdapter {
				continue
			}
			candidates = append(candidates, networkCandidate{
				name:        adapter.NetConnectionID,
				description: adapter.Name,
				speed:       adapter.Speed,
				connected:   adapter.NetEnabled,
				hardware:    true,
			})
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	// A virtual tunnel or a hypervisor adapter is never the adapter the
	// games travel over, so the physical adapters win over them.
	best := candidates[0]
	for _, current := range candidates[1:] {
		if betterNetworkCandidate(current, best) {
			best = current
		}
	}

	network := &model.NetworkInfo{
		ConnectionType: connectionType(best.name, best.description, best.medium),
		IsConnected:    best.connected,
	}
	if !network.IsConnected {
		network.IsConnected = adapterHasAddress(best.name, best.description)
	}
	name := firstNonEmpty(best.name, best.description)
	if name != "" {
		network.AdapterName = pString(name)
	}
	if best.speed > 0 {
		mbps := int(best.speed / 1000000)
		if mbps > 0 {
			network.SpeedMbps = pInt(mbps)
		}
	}
	return network
}

// betterNetworkCandidate reports whether current describes a better adapter
// than best.
func betterNetworkCandidate(current, best networkCandidate) bool {
	if current.hardware != best.hardware {
		return current.hardware
	}
	if current.connected != best.connected {
		return current.connected
	}
	return current.speed > best.speed
}

// adapterHasAddress reports whether an adapter carries an address, which is
// the sign of a working connection.
//
// The connection state of the network class is empty on several machines, so
// the configuration class is asked as well.
func adapterHasAddress(name, description string) bool {
	for _, config := range queryStructs[win32NetworkAdapterConfiguration]("SELECT * FROM Win32_NetworkAdapterConfiguration") {
		if !config.IPEnabled {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(config.Description), strings.TrimSpace(description)) ||
			strings.EqualFold(strings.TrimSpace(config.Description), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// connectionType classifies a network adapter.
func connectionType(name, description string, medium uint32) string {
	haystack := name + " " + description
	switch {
	case containsAny(haystack, "wi-fi", "wifi", "wireless", "802.11", "airport"):
		return "wifi"
	case containsAny(haystack, "cellular", "lte", "5g", "wwan", "mobile broadband", "sierra", "air card"):
		return "cellular"
	case containsAny(haystack, "ethernet", "gbe", "network adapter", "lan", "realtek", "broadcom", "marvell", "intel("):
		return "ethernet"
	}
	// The NdisPhysicalMedium values of the network driver interface.
	switch medium {
	case 1, 9:
		return "wifi"
	case 8, 12:
		return "cellular"
	case 14, 17:
		return "ethernet"
	}
	return "unknown"
}

// describeNetworkAdapter is used by the progress output.
func describeNetworkAdapter(network *model.NetworkInfo) string {
	if network == nil {
		return ""
	}
	name := "unknown adapter"
	if network.AdapterName != nil {
		name = strings.TrimSpace(*network.AdapterName)
	}
	if network.SpeedMbps != nil {
		return name + " (" + strconv.Itoa(*network.SpeedMbps) + " Mbps)"
	}
	return name
}
