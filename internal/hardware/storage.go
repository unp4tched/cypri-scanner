package hardware

import (
	"strings"

	"github.com/unp4tched/cypri-scanner/internal/model"
)

// win32DiskDrive mirrors the Win32_DiskDrive WMI class.
type win32DiskDrive struct {
	DeviceID      string `wmi:"DeviceID"`
	Index         uint32 `wmi:"Index"`
	Model         string `wmi:"Model"`
	Caption       string `wmi:"Caption"`
	SerialNumber  string `wmi:"SerialNumber"`
	Size          uint64 `wmi:"Size"`
	InterfaceType string `wmi:"InterfaceType"`
	MediaType     string `wmi:"MediaType"`
	Partitions    uint32 `wmi:"Partitions"`
	Status        string `wmi:"Status"`
	PNPDeviceID   string `wmi:"PNPDeviceID"`
}

// win32LogicalDisk mirrors the Win32_LogicalDisk WMI class.
type win32LogicalDisk struct {
	DeviceID    string `wmi:"DeviceID"`
	VolumeName  string `wmi:"VolumeName"`
	FileSystem  string `wmi:"FileSystem"`
	Size        uint64 `wmi:"Size"`
	FreeSpace   uint64 `wmi:"FreeSpace"`
	DriveType   uint32 `wmi:"DriveType"`
	Description string `wmi:"Description"`
}

// msftPhysicalDisk mirrors the MSFT_PhysicalDisk WMI class.
type msftPhysicalDisk struct {
	FriendlyName string `wmi:"FriendlyName"`
	MediaType    uint16 `wmi:"MediaType"`
	BusType      uint16 `wmi:"BusType"`
	Size         uint64 `wmi:"Size"`
	SerialNumber string `wmi:"SerialNumber"`
	UniqueId     string `wmi:"UniqueId"`
	HealthStatus uint16 `wmi:"HealthStatus"`
}

// win32DiskDriveToDiskPartition is the association between a physical disk
// and its partitions.
type win32DiskDriveToDiskPartition struct {
	Antecedent string `wmi:"Antecedent"`
	Dependent  string `wmi:"Dependent"`
}

// win32LogicalDiskToPartition is the association between a volume and the
// partition it is formatted on.
type win32LogicalDiskToPartition struct {
	Antecedent string `wmi:"Antecedent"`
	Dependent  string `wmi:"Dependent"`
}

// collectStorage gathers the physical disks and the volumes on them.
func collectStorage() []model.DriveInfo {
	drives := []model.DriveInfo{}

	physicalDisks := queryStructsNamespace[msftPhysicalDisk]("SELECT * FROM MSFT_PhysicalDisk",
		`root\Microsoft\Windows\Storage`)
	logicalDisks := queryStructs[win32LogicalDisk]("SELECT * FROM Win32_LogicalDisk")
	diskDrives := queryStructs[win32DiskDrive]("SELECT * FROM Win32_DiskDrive")

	volumesByPartition := partitionVolumes(logicalDisks)
	partitionsByDisk := diskPartitions()

	for _, disk := range diskDrives {
		drive := model.DriveInfo{
			BusType:   "Unknown",
			MediaType: "Unknown",
		}

		model_ := firstNonEmpty(disk.Model, disk.Caption)
		if model_ != "" {
			drive.Model = pString(model_)
		}
		if disk.Size > 0 {
			drive.SizeGb = roundTo(gib(disk.Size), 2)
		}

		physical := matchPhysicalDisk(disk, physicalDisks)
		if physical != nil {
			drive.BusType = busTypeName(physical.BusType)
			drive.MediaType = mediaTypeName(physical.MediaType)
			if physical.Size > 0 && drive.SizeGb == 0 {
				drive.SizeGb = roundTo(gib(physical.Size), 2)
			}
			if name := strings.TrimSpace(physical.FriendlyName); name != "" && drive.Model == nil {
				drive.Model = pString(name)
			}
		}
		if drive.BusType == "Unknown" {
			drive.BusType = busTypeFromDrive(disk)
		}
		if drive.MediaType == "Unknown" {
			drive.MediaType = mediaTypeForDrive(model_, disk.MediaType)
		}

		for _, partition := range partitionsByDisk[disk.DeviceID] {
			volume, ok := volumesByPartition[partition]
			if !ok {
				continue
			}
			drive.Partition = pString(volume.DeviceID)
			drive.FreeSpaceGb = pFloat(roundTo(gib(volume.FreeSpace), 2))
			break
		}

		drives = append(drives, drive)
	}

	// Physical disks that WMI did not expose through Win32_DiskDrive.
	for _, physical := range physicalDisks {
		if matchedPhysicalDisk(diskDrives, physical) {
			continue
		}
		drive := model.DriveInfo{
			BusType:   busTypeName(physical.BusType),
			MediaType: mediaTypeName(physical.MediaType),
		}
		if name := strings.TrimSpace(physical.FriendlyName); name != "" {
			drive.Model = pString(name)
		}
		if physical.Size > 0 {
			drive.SizeGb = roundTo(gib(physical.Size), 2)
		}
		drives = append(drives, drive)
	}

	if len(drives) == 0 {
		// No physical disk information: report the volumes themselves.
		for _, volume := range logicalDisks {
			drive := model.DriveInfo{
				BusType:   "Unknown",
				MediaType: "Unknown",
				Partition: pString(volume.DeviceID),
			}
			if volume.Size > 0 {
				drive.SizeGb = roundTo(gib(volume.Size), 2)
			}
			drive.FreeSpaceGb = pFloat(roundTo(gib(volume.FreeSpace), 2))
			drives = append(drives, drive)
		}
	}
	return drives
}

// partitionVolumes maps a partition onto the volume formatted on it.
func partitionVolumes(logicalDisks []win32LogicalDisk) map[string]win32LogicalDisk {
	result := map[string]win32LogicalDisk{}
	links := queryStructs[win32LogicalDiskToPartition]("SELECT * FROM Win32_LogicalDiskToPartition")
	for _, link := range links {
		for _, volume := range logicalDisks {
			if strings.EqualFold(volume.DeviceID, strings.TrimSpace(link.Antecedent)) {
				result[strings.TrimSpace(link.Dependent)] = volume
			}
		}
	}
	return result
}

// diskPartitions maps a physical disk onto the partitions it owns.
func diskPartitions() map[string][]string {
	result := map[string][]string{}
	links := queryStructs[win32DiskDriveToDiskPartition]("SELECT * FROM Win32_DiskDriveToDiskPartition")
	for _, link := range links {
		drive := strings.TrimSpace(link.Antecedent)
		partition := strings.TrimSpace(link.Dependent)
		if drive == "" || partition == "" {
			continue
		}
		result[drive] = append(result[drive], partition)
	}
	return result
}

// matchPhysicalDisk finds the storage physical disk that belongs to a
// Win32_DiskDrive entry.
func matchPhysicalDisk(disk win32DiskDrive, physicalDisks []msftPhysicalDisk) *msftPhysicalDisk {
	serial := normalizeSerial(disk.SerialNumber)
	for i := range physicalDisks {
		if serial != "" && normalizeSerial(physicalDisks[i].SerialNumber) == serial {
			return &physicalDisks[i]
		}
	}
	for i := range physicalDisks {
		if disk.Size > 0 && physicalDisks[i].Size > 0 &&
			absDiff(physicalDisks[i].Size, disk.Size) < 1024*1024 {
			return &physicalDisks[i]
		}
	}
	for i := range physicalDisks {
		if strings.EqualFold(strings.TrimSpace(physicalDisks[i].FriendlyName), strings.TrimSpace(disk.Model)) {
			return &physicalDisks[i]
		}
	}
	return nil
}

func matchedPhysicalDisk(diskDrives []win32DiskDrive, physical msftPhysicalDisk) bool {
	for _, disk := range diskDrives {
		if matchPhysicalDisk(disk, []msftPhysicalDisk{physical}) != nil {
			return true
		}
	}
	return false
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

// normalizeSerial removes the dashes and spaces of a serial number.
func normalizeSerial(serial string) string {
	var b strings.Builder
	for _, r := range serial {
		if r != '-' && r != ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// busTypeName maps an MSFT_PhysicalDisk bus type to the JSON vocabulary.
//
// The values are the ones of the MSFT_Storage BusType enumeration.
func busTypeName(bus uint16) string {
	switch bus {
	case 17:
		return "NVMe"
	case 11, 3:
		return "SATA"
	case 10:
		return "SAS"
	case 5, 9:
		return "USB"
	case 1:
		return "SCSI"
	case 2:
		return "IDE"
	case 8:
		return "Fibre Channel"
	case 16:
		return "Storage Spaces"
	case 14, 15:
		return "Virtual"
	default:
		return "Unknown"
	}
}

// mediaTypeName maps an MSFT_PhysicalDisk media type to the JSON vocabulary.
//
// The values are the ones of the MSFT_PhysicalDisk MediaType enumeration.
func mediaTypeName(media uint16) string {
	switch media {
	case 3:
		return "HDD"
	case 4:
		return "SSD"
	case 5:
		return "SCM"
	default:
		return "Unknown"
	}
}

// busTypeFromDrive guesses the bus of a disk that the storage namespace did
// not describe.
//
// Windows reports an NVMe disk as a SCSI disk, so the plug and play
// identifier is checked first.
func busTypeFromDrive(disk win32DiskDrive) string {
	deviceID := strings.ToUpper(disk.PNPDeviceID)
	switch {
	case strings.Contains(deviceID, "NVME"):
		return "NVMe"
	case strings.Contains(deviceID, "USBSTOR"):
		return "USB"
	}
	return busTypeFromInterface(disk.InterfaceType)
}

// busTypeFromInterface maps the Win32_DiskDrive interface type.
func busTypeFromInterface(value string) string {
	switch {
	case strings.Contains(strings.ToLower(value), "nvme"):
		return "NVMe"
	case strings.Contains(strings.ToLower(value), "usb"):
		return "USB"
	case strings.Contains(strings.ToLower(value), "scsi"):
		return "SCSI"
	case strings.Contains(strings.ToLower(value), "ide"), strings.Contains(strings.ToLower(value), "ata"):
		return "SATA"
	default:
		return "Unknown"
	}
}

// mediaTypeForDrive guesses the media type of a disk that the storage
// namespace did not describe.
//
// Windows calls every fixed disk a "fixed hard disk media", so the model name
// is the only hint that is left.
func mediaTypeForDrive(model, wmiMedia string) string {
	switch strings.ToLower(strings.TrimSpace(wmiMedia)) {
	case "fixed hard disk media":
		if looksLikeSSD(model) {
			return "SSD"
		}
		return "HDD"
	default:
		return "Unknown"
	}
}

// looksLikeSSD reports whether a model name belongs to a solid state disk.
func looksLikeSSD(model string) bool {
	return containsAny(model,
		"ssd",
		"nvme",
		"m.2",
		"snv",
		"samsung mz",
		"solid state",
		"ssd ",
	)
}
