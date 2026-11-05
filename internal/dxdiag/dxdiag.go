// Package dxdiag runs the Windows dxdiag diagnostic tool and parses its
// textual report. dxdiag is the only reliable source for the DirectX feature
// levels and shader model a display driver advertises.
package dxdiag

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

// Report is the parsed dxdiag output.
type Report struct {
	// Sections holds every "Key: Value" pair of a section, in file order.
	Sections map[string][]Pair
	// Cards holds the adapters of the "Display Devices" section.
	Cards []Card
}

// Pair is one key/value line of a section.
type Pair struct {
	Key   string
	Value string
}

// Card describes one display adapter as reported by dxdiag.
type Card struct {
	Fields []Pair
}

// First returns the first value stored under key.
func (c Card) First(key string) string {
	for _, p := range c.Fields {
		if strings.EqualFold(p.Key, key) {
			return p.Value
		}
	}
	return ""
}

// All returns every value stored under key.
func (c Card) All(key string) []string {
	var out []string
	for _, p := range c.Fields {
		if strings.EqualFold(p.Key, key) {
			out = append(out, p.Value)
		}
	}
	return out
}

// Section returns the pairs of a named section.
func (r *Report) Section(name string) []Pair {
	if r == nil {
		return nil
	}
	return r.Sections[name]
}

// First returns the first value of key inside the named section.
func (r *Report) First(section, key string) string {
	for _, p := range r.Section(section) {
		if strings.EqualFold(p.Key, key) {
			return p.Value
		}
	}
	return ""
}

// Cards returns the adapters reported in the "Display Devices" section.
func (r *Report) CardNames() []string {
	var names []string
	for _, c := range r.Cards {
		if n := c.First("Card name"); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// Run executes dxdiag and parses the generated report.
func Run(timeout time.Duration) (*Report, error) {
	dir, err := os.MkdirTemp("", "canyourpcrunit-dxdiag")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "dxdiag.txt")
	if err := run(target, timeout); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	return Parse(decodeText(data)), nil
}

func run(target string, timeout time.Duration) error {
	exe, err := exec.LookPath("dxdiag.exe")
	if err != nil {
		exe = filepath.Join(os.Getenv("SystemRoot"), "System32", "dxdiag.exe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, "/dontskip", "/whql:off", "/t", target)
	// dxdiag is a windowed program: without this it briefly draws its own
	// window on top of the scanner.
	cmd.SysProcAttr = winapi.HiddenProcess()
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil && ctx.Err() == nil {
			return err
		}
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return ctx.Err()
	}
	return waitForStableFile(target, timeout)
}

// waitForStableFile waits until dxdiag stopped writing the report.
func waitForStableFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastSize int64 = -1
	stable := 0
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil {
			if info.Size() == lastSize && info.Size() > 0 {
				stable++
				if stable >= 3 {
					return nil
				}
			} else {
				stable = 0
				lastSize = info.Size()
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// decodeText converts the report into a Go string.
//
// dxdiag writes UTF-16LE with a byte order mark on some Windows versions
// and plain ANSI on others, so the encoding is detected instead of assumed.
func decodeText(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return decodeUTF16(data[2:], false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return decodeUTF16(data[2:], true)
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return string(data[3:])
	}

	// Without a byte order mark a UTF-16LE file still shows a zero byte in
	// every second position.
	if looksUTF16LE(data) {
		return decodeUTF16(data, false)
	}
	return string(data)
}

// looksUTF16LE reports whether the data is UTF-16LE without a byte order
// mark.
func looksUTF16LE(data []byte) bool {
	limit := len(data)
	if limit > 512 {
		limit = 512
	}
	zeros := 0
	for i := 1; i < limit; i += 2 {
		if data[i] == 0 {
			zeros++
		}
	}
	return zeros > limit/4
}

// decodeUTF16 turns UTF-16 data into a Go string.
func decodeUTF16(data []byte, bigEndian bool) string {
	if len(data)%2 == 1 {
		data = data[:len(data)-1]
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
			continue
		}
		units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
	}
	return string(utf16.Decode(units))
}

// Parse turns the raw dxdiag text into a Report.
func Parse(text string) *Report {
	report := &Report{Sections: map[string][]Pair{}}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	section := ""
	var current *Card

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if isRule(trimmed) {
			// A rule is followed by the section title and another rule.
			title := ""
			if i+2 < len(lines) {
				title = strings.TrimSpace(lines[i+1])
				if isRule(strings.TrimSpace(lines[i+2])) {
					i += 2
				} else {
					i++
				}
			}
			section = title
			current = nil
			continue
		}

		if trimmed == "" {
			continue
		}

		key, value, ok := splitPair(trimmed)
		if !ok {
			continue
		}

		if section == "Display Devices" {
			if strings.EqualFold(key, "Card name") {
				report.Cards = append(report.Cards, Card{})
				current = &report.Cards[len(report.Cards)-1]
			}
			if current != nil {
				current.Fields = append(current.Fields, Pair{Key: key, Value: value})
				continue
			}
		}
		report.Sections[section] = append(report.Sections[section], Pair{Key: key, Value: value})
	}
	return report
}

func isRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	for _, r := range line {
		if r != '-' {
			return false
		}
	}
	return true
}

func splitPair(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// FeatureLevels returns the DirectX feature levels a card advertises.
//
// dxdiag prints them as a comma separated list.
func (c Card) FeatureLevels() []string {
	raw := c.First("Feature Levels")
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	}) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// MemoryMB parses a "1234 MB" style value into megabytes.
func MemoryMB(value string) (int64, bool) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0, false
	}
	size, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	if len(fields) > 1 && strings.EqualFold(fields[1], "GB") {
		return size * 1024, true
	}
	return size, true
}

// DriverDate normalises the dxdiag driver date to an ISO date.
func DriverDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	datePart := strings.Fields(value)[0]
	for _, layout := range []string{"01/02/2006", "2006-01-02", "1/2/2006"} {
		if t, err := time.Parse(layout, datePart); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}
