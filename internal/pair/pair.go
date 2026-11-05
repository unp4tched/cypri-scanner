// Package pair pairs the scanner with the website using a short code the
// user generated in their browser.
//
// The flow is two requests, in this order:
//
//  1. Claim: POST `/api/scanner/pair/<code>/connect` with the version only.
//     This happens before a single hardware query, so the browser tab can
//     show "connected" while the scan runs.
//  2. Submit: one POST carrying the hardware profile. The code is single use
//     on the site, so a captured request cannot be replayed.
package pair

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/unp4tched/cypri-scanner/internal/model"
)

// CodeLength is the number of characters in a pairing code.
const CodeLength = 6

// Alphabet mirrors the website's unambiguous alphabet (no I, O, 0 or 1).
const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Kind classifies a failed submission so the GUI can explain it.
type Kind int

const (
	// KindNetwork means the site could not be reached at all.
	KindNetwork Kind = iota
	// KindNotFound means the code is not active (typo, or already expired).
	KindNotFound
	// KindExpired means the code exists but ran out of time.
	KindExpired
	// KindUsed means another scan already claimed the code.
	KindUsed
	// KindInvalid means the site rejected the code shape or the payload.
	KindInvalid
	// KindRateLimited means too many attempts came from this machine.
	KindRateLimited
	// KindServer means the site answered with an unexpected status.
	KindServer
)

// Error is a submission failure with a message that is safe to show a user.
type Error struct {
	Kind   Kind
	Status int
}

func (e *Error) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("pairing failed (%d)", e.Status)
	}
	return "pairing failed"
}

// NormaliseCode uppercases a code and drops separators such as the dash people
// like to type. It keeps at most CodeLength characters.
func NormaliseCode(raw string) string {
	var builder strings.Builder
	for _, character := range strings.ToUpper(strings.TrimSpace(raw)) {
		if strings.ContainsRune(alphabet, character) {
			builder.WriteRune(character)
			if builder.Len() == CodeLength {
				break
			}
		}
	}
	return builder.String()
}

// ValidCode reports whether a normalised code has the expected shape.
func ValidCode(code string) bool {
	if len(code) != CodeLength {
		return false
	}
	for _, character := range code {
		if !strings.ContainsRune(alphabet, character) {
			return false
		}
	}
	return true
}

type payload struct {
	ScannerVersion string               `json:"scannerVersion"`
	CapturedAt     int64                `json:"capturedAt"`
	Hardware       model.HardwareProfile `json:"hardware"`
}

// Claim performs the handshake: it tells the website this code is about to be
// used, BEFORE any hardware is read. The site flips its tab to "connected" so
// the user sees the pairing worked, and the scan only starts once the site has
// accepted the code. No hardware data is transmitted here - only the version.
func Claim(ctx context.Context, site string, code string) error {
	endpoint, err := submitURL(site, code)
	if err != nil {
		return &Error{Kind: KindInvalid}
	}
	endpoint += "/connect"

	body, err := json.Marshal(struct {
		ScannerVersion string `json:"scannerVersion"`
	}{ScannerVersion: model.ScannerVersion})
	if err != nil {
		return &Error{Kind: KindInvalid}
	}
	return post(ctx, endpoint, body)
}

// Submit posts the profile to the site and waits for it to be accepted.
func Submit(ctx context.Context, site string, code string, profile model.HardwareProfile) error {
	endpoint, err := submitURL(site, code)
	if err != nil {
		return &Error{Kind: KindInvalid}
	}

	body, err := json.Marshal(payload{
		ScannerVersion: model.ScannerVersion,
		CapturedAt:     profile.CapturedAt,
		Hardware:       profile,
	})
	if err != nil {
		return &Error{Kind: KindInvalid}
	}
	return post(ctx, endpoint, body)
}

// newClient builds the short-lived client every request uses.
func newClient() *http.Client {
	return &http.Client{
		Timeout: 25 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
		},
	}
}

// Status asks whether the tab has collected the profile yet.
//
// It uses the non-consuming peek endpoint: polling the normal status URL would
// burn the profile and the browser tab would never see the hardware it is
// waiting for.
func Status(ctx context.Context, site string, code string) (string, error) {
	endpoint, err := submitURL(site, code)
	if err != nil {
		return "", &Error{Kind: KindInvalid}
	}
	endpoint += "?peek=1"

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", &Error{Kind: KindInvalid}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "CanYourPCRunIt-Scanner/"+model.ScannerVersion)

	response, err := newClient().Do(request)
	if err != nil {
		return "", &Error{Kind: KindNetwork}
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", &Error{Kind: KindServer, Status: response.StatusCode}
	}

	var decoded struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2048)).Decode(&decoded); err != nil {
		return "", &Error{Kind: KindServer, Status: response.StatusCode}
	}
	return decoded.Status, nil
}

// post performs one bounded JSON POST to a pairing endpoint and maps the
// response to a user-facing error, or nil on 2xx.
func post(ctx context.Context, endpoint string, body []byte) error {
	client := newClient()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return &Error{Kind: KindInvalid}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "CanYourPCRunIt-Scanner/"+model.ScannerVersion)

	response, err := client.Do(request)
	if err != nil {
		return &Error{Kind: KindNetwork}
	}
	defer response.Body.Close()

	// Drain a bounded amount so the connection can be reused, and so we can
	// surface the server's error code when it sends one.
	reply, _ := io.ReadAll(io.LimitReader(response.Body, 4096))

	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated {
		return nil
	}

	var decoded struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(reply, &decoded)

	switch response.StatusCode {
	case http.StatusNotFound:
		return &Error{Kind: KindNotFound, Status: response.StatusCode}
	case http.StatusGone:
		return &Error{Kind: KindExpired, Status: response.StatusCode}
	case http.StatusConflict:
		return &Error{Kind: KindUsed, Status: response.StatusCode}
	case http.StatusTooManyRequests:
		return &Error{Kind: KindRateLimited, Status: response.StatusCode}
	case http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusRequestEntityTooLarge:
		if decoded.Error == "invalid_profile" || decoded.Error == "profile_too_large" {
			return &Error{Kind: KindInvalid, Status: response.StatusCode}
		}
		return &Error{Kind: KindNotFound, Status: response.StatusCode}
	default:
		return &Error{Kind: KindServer, Status: response.StatusCode}
	}
}

// Message turns a submission or claim failure into a sentence for the GUI.
func Message(err error, site string) string {
	host := hostOf(site)
	var failure *Error
	if !errors.As(err, &failure) {
		return "Something went wrong while sending your specs. Try again."
	}
	switch failure.Kind {
	case KindNetwork:
		if loopbackSite(site) {
			return fmt.Sprintf("Could not reach %s. The local site is not running - start it with `npm run dev`, then try again.", host)
		}
		return fmt.Sprintf("Could not reach %s. Check your connection and that the site is online.", host)
	case KindNotFound:
		return "That code is not active. Generate a new one on the website and try again."
	case KindExpired:
		return "That code expired. Generate a fresh one on the website."
	case KindUsed:
		return "That code was already used. Generate a new one on the website."
	case KindRateLimited:
		return "Too many attempts. Wait a minute, then try again."
	case KindInvalid:
		return "The website could not read the scan. Try again, or import the JSON file it saved."
	default:
		return fmt.Sprintf("%s answered with an unexpected error (%d). Try again in a moment.", host, failure.Status)
	}
}

// submitURL builds the endpoint for a code, rejecting anything malformed.
func submitURL(site, code string) (string, error) {
	if !ValidCode(code) {
		return "", fmt.Errorf("invalid code")
	}
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(site), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid site url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme")
	}
	return parsed.String() + "/api/scanner/pair/" + code, nil
}

// loopbackSite reports whether the site points at this machine, which is how
// the scanner runs while the public domain does not exist yet.
func loopbackSite(site string) bool {
	parsed, err := url.Parse(strings.TrimSpace(site))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	return false
}

// hostOf returns the host of a site URL for display.
func hostOf(site string) string {
	parsed, err := url.Parse(strings.TrimSpace(site))
	if err != nil || parsed.Host == "" {
		return "the website"
	}
	return parsed.Host
}

// SiteLabel formats a site URL for the GUI ("canyourpcrunit.com").
func SiteLabel(site string) string {
	return hostOf(site)
}
