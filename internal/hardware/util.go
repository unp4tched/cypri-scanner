// Package hardware collects the hardware inventory of the machine.
//
// Every probe is isolated: a missing driver, an absent battery or a WMI class
// that does not exist on this SKU only blanks out the fields it owns, the
// scan always continues.
package hardware

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/StackExchange/wmi"
)

func init() {
	// A SKU that does not expose one of the properties we ask for must not
	// invalidate the whole query.
	wmi.DefaultClient.AllowMissingFields = true
}

// queryStructs runs a WMI query and returns the rows.
//
// A field mismatch error still leaves the rows populated, so it is not fatal;
// a query that fails outright simply yields no rows.
func queryStructs[T any](query string) []T {
	return queryStructsNamespace[T](query, "")
}

// queryStructsNamespace runs a WMI query inside an explicit namespace.
//
// The WMI library panics on some property shapes, for example on an unsigned
// integer array. The panic is caught here so that a single odd class cannot
// abort the whole scan.
func queryStructsNamespace[T any](query, namespace string) []T {
	var out []T
	err := runWMI(query, namespace, &out)
	if err != nil {
		var mismatch *wmi.ErrFieldMismatch
		if !errorsAs(err, &mismatch) {
			// Keep whatever was decoded; the caller treats an empty
			// result as "this subsystem is unavailable".
			return out
		}
	}
	return out
}

// runWMI performs a WMI query and turns a panic inside the library into an
// ordinary error.
func runWMI(query, namespace string, dest any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("wmi query %q panicked: %v", query, r)
		}
	}()

	if namespace == "" {
		return wmi.Query(query, dest)
	}
	return wmi.QueryNamespace(query, dest, namespace)
}

// errorsAs is a tiny wrapper so the helper above stays readable.
func errorsAs(err error, target **wmi.ErrFieldMismatch) bool {
	for err != nil {
		if mismatch, ok := err.(*wmi.ErrFieldMismatch); ok {
			*target = mismatch
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// Probe runs fn and converts a panic into an error so that a single broken
// subsystem can never abort the scan.
func Probe[T any](fn func() (T, error)) (result T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("probe panicked: %v", r)
		}
	}()
	return fn()
}

// ProbeValue runs fn and returns its value, or the zero value on failure.
func ProbeValue[T any](fn func() (T, error)) (result T) {
	v, _ := Probe(fn)
	return v
}

func pFloat(v float64) *float64 { return &v }
func pInt(v int) *int           { return &v }
func pBool(v bool) *bool        { return &v }
func pString(v string) *string  { return &v }

const (
	bytesPerGiB = 1024 * 1024 * 1024
	bytesPerMiB = 1024 * 1024
)

// gib converts a byte count into gibibytes.
func gib(bytes uint64) float64 { return float64(bytes) / bytesPerGiB }

// roundTo rounds a float to the given number of decimals.
func roundTo(v float64, decimals int) float64 {
	factor := 1.0
	for i := 0; i < decimals; i++ {
		factor *= 10
	}
	return float64(int64(v*factor+0.5*sign(v))) / factor
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// parseWMIDateTime parses the CIM datetime format used by WMI, for example
// "20241221090002.000000+060".
func parseWMIDateTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 14 {
		return time.Time{}, false
	}
	parts := []string{value[0:4], value[4:6], value[6:8], value[8:10], value[10:12], value[12:14]}
	numbers := make([]int, 6)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return time.Time{}, false
		}
		numbers[i] = n
	}
	offsetMinutes := 0
	if len(value) >= 24 {
		if v, err := strconv.Atoi(value[21:24]); err == nil {
			if value[20] == '-' {
				v = -v
			}
			offsetMinutes = v
		}
	}
	loc := time.FixedZone("", offsetMinutes*60)
	return time.Date(numbers[0], time.Month(numbers[1]), numbers[2],
		numbers[3], numbers[4], numbers[5], 0, loc), true
}

// wmiDateString converts a WMI datetime into an RFC 3339 timestamp.
func wmiDateString(value string) *string {
	t, ok := parseWMIDateTime(value)
	if !ok {
		return nil
	}
	return pString(t.UTC().Format(time.RFC3339))
}

// isoDate converts a "MM/DD/YYYY" date into an ISO date.
func isoDate(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil
	}
	for _, layout := range []string{"01/02/2006", "2006-01-02", "1/2/2006"} {
		if t, err := time.Parse(layout, fields[0]); err == nil {
			return pString(t.Format("2006-01-02"))
		}
	}
	return nil
}

// firstNonEmpty returns the first non-empty trimmed string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, needles ...string) bool {
	lower := strings.ToLower(s)
	for _, n := range needles {
		if strings.Contains(lower, strings.ToLower(n)) {
			return true
		}
	}
	return false
}
