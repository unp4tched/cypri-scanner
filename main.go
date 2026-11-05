// Command CanYourPCRunIt-Scanner reads this machine's hardware inventory and
// sends it to the website with a short pairing code.
//
// The flow is deliberately small, and the code comes FIRST:
//
//  1. The user types the six-character code their browser is showing.
//  2. The scanner confirms that code with the website. Nothing has been read
//     yet; the browser tab flips to "connected" as soon as this succeeds.
//  3. Only then does the scan run, with the window showing its progress.
//  4. One HTTPS POST carries the profile to /api/scanner/pair/<code>.
//
// Nothing is opened in a browser, no port is served, and no data is sent
// before the code is confirmed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/unp4tched/cypri-scanner/internal/gui"
	"github.com/unp4tched/cypri-scanner/internal/hardware"
	"github.com/unp4tched/cypri-scanner/internal/model"
	"github.com/unp4tched/cypri-scanner/internal/pair"
	"github.com/unp4tched/cypri-scanner/internal/winapi"
)

// defaultSiteURL is the website the scanner pairs with when no -site flag is
// given. The public domain does not exist yet, so the published build pairs
// with the local dev server. A production build overrides it:
//
//	go build -ldflags "-X main.defaultSiteURL=https://canyourpcrunit.com" ...
var defaultSiteURL = "http://localhost:3000"

const (
	// uploadTimeout caps the single outbound request.
	uploadTimeout = 40 * time.Second
	// successLinger is how long the success screen stays up.
	successLinger = 9 * time.Second
	// deliveryWait is how long we wait for the browser tab to collect the
	// profile before telling the user the upload went nowhere useful.
	deliveryWait = 18 * time.Second
)

func main() {
	// A double-click gets a console window of its own; hide it so only the
	// scanner window shows. A console inherited from a terminal is left alone.
	winapi.HideStartupConsole()

	var (
		site        = flag.String("site", defaultSiteURL, "base URL of the website that shows the pairing code")
		code        = flag.String("code", "", "pairing code; skips the code screen and sends automatically")
		outFile     = flag.String("out", "", "write the scan to this JSON file and exit without sending")
		headless    = flag.Bool("headless", false, "run without the window (used for testing)")
		previewDir  = flag.String("ui-preview", "", "write PNG previews of each window state to this directory and exit")
		printVersion = flag.Bool("version", false, "print the scanner version and exit")
	)
	flag.Parse()

	if *printVersion {
		fmt.Println(model.ScannerVersion)
		return
	}

	if *previewDir != "" {
		if err := writePreviews(*previewDir, *site); err != nil {
			fmt.Fprintf(os.Stderr, "could not write previews: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *outFile != "" {
		profile := hardware.Scan(consoleLogger)
		if err := writeProfile(*outFile, profile); err != nil {
			fmt.Fprintf(os.Stderr, "could not write %s: %v\n", *outFile, err)
			os.Exit(1)
		}
		fmt.Printf("Scan written to %s\n", *outFile)
		return
	}

	if *headless {
		os.Exit(runHeadless(*site, *code))
	}

	runWindow(*site, *code)
}

// runWindow wires the pairing window: code entry, handshake, scan, upload.
func runWindow(site, code string) {
	var (
		profileMu sync.Mutex
		profile   model.HardwareProfile
		scanned   bool
	)

	var app *gui.App
	app = gui.New(gui.Options{
		Site:        site,
		Version:     model.ScannerVersion,
		PrefillCode: pair.NormaliseCode(code),
		AutoClose:   successLinger,
		OnSubmit: func(submitted string) {
			go func() {
				// 1. Confirm the code with the website before reading anything.
				ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
				err := pair.Claim(ctx, site, submitted)
				cancel()
				if err != nil {
					app.UploadFailed(pair.Message(err, site))
					return
				}

				// 2. Scan - unless an earlier attempt already produced one, in
				//    which case a retry only re-sends it.
				profileMu.Lock()
				have := scanned
				profileMu.Unlock()
				if !have {
					app.StartScan()
					scannedProfile := hardware.Scan(func(format string, args ...any) {
						progressLogger(app, format, args...)
					})
					profileMu.Lock()
					profile = scannedProfile
					scanned = true
					profileMu.Unlock()
				}

				// 3. Hand the profile over.
				app.Sending()
				profileMu.Lock()
				current := profile
				profileMu.Unlock()

				ctx, cancel = context.WithTimeout(context.Background(), uploadTimeout)
				defer cancel()

				if err := pair.Submit(ctx, site, submitted, current); err != nil {
					app.UploadFailed(pair.Message(err, site))
					return
				}

				// The upload only proves the site stored it. Wait until a tab
				// actually collects it, so the window never claims success while
				// the browser is still stuck on "reading your hardware".
				if waitForCollection(site, submitted, deliveryWait) {
					app.Uploaded()
					return
				}
				app.UploadedUnread(fmt.Sprintf(
					"Saved on %s, but no open tab collected it. Open %s/scanner, generate a code and run the scan again.",
					pair.SiteLabel(site), pair.SiteLabel(site),
				))
			}()
		},
	})

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "scanner window failed: %v\n", err)
		os.Exit(1)
	}
}

// runHeadless claims, scans, uploads and reports without a window.
func runHeadless(site, code string) int {
	normalised := pair.NormaliseCode(code)
	if normalised == "" {
		profile := hardware.Scan(consoleLogger)
		fmt.Println("No pairing code given; writing the scan to stdout instead.")
		encoded, err := json.MarshalIndent(profile, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not encode the scan: %v\n", err)
			return 1
		}
		fmt.Println(string(encoded))
		return 0
	}

	// Claim first: a dead or used code fails in well under a second, without
	// spending half a minute scanning hardware that cannot be delivered.
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	err := pair.Claim(ctx, site, normalised)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", pair.Message(err, site))
		return 1
	}

	profile := hardware.Scan(consoleLogger)

	ctx, cancel = context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()

	if err := pair.Submit(ctx, site, normalised, profile); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", pair.Message(err, site))
		return 1
	}
	fmt.Printf("Sent %s to %s\n", normalised, site)
	return 0
}

// waitForCollection polls the site until the tab has taken the profile.
//
// It uses the non-consuming status check, so this can never steal the profile
// from the browser tab that is waiting for it.
func waitForCollection(site, code string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		status, err := pair.Status(ctx, site, code)
		cancel()
		if err == nil && status == "delivered" {
			return true
		}
		time.Sleep(700 * time.Millisecond)
	}
	return false
}

// progressLogger turns the scan log into window updates.
//
// The scan reports each subsystem as it finishes, so a line starting with
// "CPU:" means the processor row is done and the next row becomes active.
func progressLogger(app *gui.App, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	trimmed := strings.TrimSpace(line)

	switch {
	case strings.HasPrefix(trimmed, "Collecting hardware inventory"):
		app.SetDetail("Collecting hardware inventory...")
	case strings.HasPrefix(trimmed, "CPU:"):
		app.Step(0, gui.StepDone, strings.TrimSpace(strings.TrimPrefix(trimmed, "CPU:")))
		app.Step(1, gui.StepActive, "")
		app.SetDetail("Reading the display adapter...")
	case strings.HasPrefix(trimmed, "GPU:"):
		app.Step(1, gui.StepDone, strings.TrimSpace(strings.TrimPrefix(trimmed, "GPU:")))
		app.Step(2, gui.StepActive, "")
		app.SetDetail("Reading installed memory...")
	case strings.HasPrefix(trimmed, "RAM:"):
		app.Step(2, gui.StepDone, strings.TrimSpace(strings.TrimPrefix(trimmed, "RAM:")))
		app.Step(3, gui.StepActive, "")
		app.SetDetail("Reading storage devices...")
	case strings.HasPrefix(trimmed, "Storage:"):
		app.Step(3, gui.StepDone, strings.TrimSpace(strings.TrimPrefix(trimmed, "Storage:")))
		app.Step(4, gui.StepActive, "")
		app.SetDetail("Reading system and display details...")
	case strings.HasPrefix(trimmed, "OS:"):
		app.Step(4, gui.StepDone, strings.TrimSpace(strings.TrimPrefix(trimmed, "OS:")))
		app.SetDetail("Finishing up...")
	case strings.HasPrefix(trimmed, "dxdiag"):
		app.SetDetail("Waiting for the DirectX report...")
	}
}

// consoleLogger prints the scan progress for headless runs.
func consoleLogger(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

// writeProfile writes the profile as pretty-printed JSON.
func writeProfile(path string, profile model.HardwareProfile) error {
	encoded, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "" {
		_ = os.MkdirAll(directory, 0o755)
	}
	return os.WriteFile(path, encoded, 0o644)
}

// writePreviews renders each window state to PNG so the layout can be checked
// without sitting in front of the machine.
func writePreviews(directory, site string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	states := []struct {
		name    string
		options gui.PreviewOptions
	}{
		{"01-code.png", gui.PreviewOptions{Phase: gui.PhaseCode}},
		{"02-code-typed.png", gui.PreviewOptions{Phase: gui.PhaseCode, Code: "K7QM42"}},
		{"03-connecting.png", gui.PreviewOptions{Phase: gui.PhaseConnecting}},
		{"04-scanning.png", gui.PreviewOptions{Phase: gui.PhaseScanning, Progress: 0.6, Steps: 3}},
		{"05-sending.png", gui.PreviewOptions{Phase: gui.PhaseSending}},
		{"06-done.png", gui.PreviewOptions{Phase: gui.PhaseDone}},
		{"07-error.png", gui.PreviewOptions{Phase: gui.PhaseError, Code: "K7QM42", HasScan: true,
			Error: "That code is not active. Generate a new one on the website and try again."}},
		{"08-error-connect.png", gui.PreviewOptions{Phase: gui.PhaseError, Code: "K7QM42",
			Error: "That code is not active. Generate a new one on the website and try again."}},
	}

	for _, state := range states {
		options := state.options
		options.Site = site
		options.Version = model.ScannerVersion
		if err := gui.WritePreview(filepath.Join(directory, state.name), options); err != nil {
			return err
		}
	}
	return nil
}
