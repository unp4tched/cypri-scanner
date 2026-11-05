package hardware

import (
	"strings"

	"github.com/unp4tched/cypri-scanner/internal/model"
)

// win32BaseBoard mirrors the Win32_BaseBoard WMI class.
type win32BaseBoard struct {
	Manufacturer string `wmi:"Manufacturer"`
	Product      string `wmi:"Product"`
	Version      string `wmi:"Version"`
	SerialNumber string `wmi:"SerialNumber"`
}

// win32BIOS mirrors the Win32_BIOS WMI class.
type win32BIOS struct {
	Manufacturer      string `wmi:"Manufacturer"`
	SMBIOSBIOSVersion string `wmi:"SMBIOSBIOSVersion"`
	Version           string `wmi:"Version"`
	ReleaseDate       string `wmi:"ReleaseDate"`
}

// collectMotherboard gathers the mainboard and its firmware.
func collectMotherboard() *model.MotherboardInfo {
	boards := queryStructs[win32BaseBoard]("SELECT * FROM Win32_BaseBoard")
	bios := queryStructs[win32BIOS]("SELECT * FROM Win32_BIOS")
	if len(boards) == 0 && len(bios) == 0 {
		return nil
	}

	board := &model.MotherboardInfo{}
	if len(boards) > 0 {
		if value := strings.TrimSpace(boards[0].Manufacturer); value != "" {
			board.Manufacturer = pString(value)
		}
		if value := strings.TrimSpace(boards[0].Product); value != "" {
			board.Product = pString(value)
		}
		if value := strings.TrimSpace(boards[0].Version); value != "" {
			board.Version = pString(value)
		}
	}
	if len(bios) > 0 {
		if value := strings.TrimSpace(bios[0].Manufacturer); value != "" {
			board.BiosVendor = pString(value)
		}
		version := strings.TrimSpace(bios[0].SMBIOSBIOSVersion)
		if version == "" {
			version = strings.TrimSpace(bios[0].Version)
		}
		if version != "" {
			board.BiosVersion = pString(version)
		}
		if date := isoDate(bios[0].ReleaseDate); date != nil {
			board.BiosDate = date
		} else if raw := strings.TrimSpace(bios[0].ReleaseDate); raw != "" {
			if t, ok := parseWMIDateTime(raw); ok {
				board.BiosDate = pString(t.UTC().Format("2006-01-02"))
			}
		}
	}
	return board
}
