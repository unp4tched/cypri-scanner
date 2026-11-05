# Cypri Scanner

The desktop hardware scanner for [CanYourPCRunIt](https://canyourpcrunit.com) - published as `CanYourPCRunIt-Scanner.exe`.

It reads your PC's hardware inventory locally and delivers it to the browser tab that generated a one-time pairing code. That's all it does.

**100% safe and open source.** Every line that runs on your machine is in this repository, MIT licensed. No installer, no admin rights, no background service, no telemetry, no account.

- Website: <https://canyourpcrunit.com/scanner>
- Source: <https://github.com/unp4tched/cypri-scanner>
- Current version: **1.2.2** (Windows 10 / 11, 64-bit)

---

## Why it is safe

This is not a promise to take on faith - each claim maps to code you can read:

| Claim | Where to check |
| --- | --- |
| **No admin rights.** The manifest requests `asInvoker`; Windows never shows a UAC prompt. | [`app.manifest`](app.manifest) |
| **No installer, service, driver, or startup entry.** It is a single `.exe` that runs, scans and exits. | [`main.go`](main.go) |
| **Nothing is read before you enter a valid code.** The first network call happens only after the site accepts the code. | [`main.go`](main.go) `runWindow`, [`internal/pair/pair.go`](internal/pair/pair.go) `Claim` |
| **Hardware inventory only.** No files, filenames, documents, pictures, passwords, browser history, clipboard, keystrokes or screen contents. No installed-software list either. | All WMI classes are listed in [`internal/hardware`](internal/hardware) - 19 classes, all hardware or OS metadata |
| **No personal identifiers in the payload.** Drive, RAM, motherboard and network serial numbers / MAC addresses are never copied into the JSON document; neither are hostnames, usernames, IP addresses or Windows product keys. | [`internal/model/types.go`](internal/model/types.go) |
| **No hidden network traffic.** One handshake (version only) and one profile POST to the site you pair with. No analytics, no update checker, no third-party hosts. | [`internal/pair/pair.go`](internal/pair/pair.go) |
| **No listening port, no browser launch.** Pairing is outbound HTTPS only. | [`main.go`](main.go) |
| **Temporary data is cleaned up.** The dxdiag report is written to a temp folder and deleted when the scan ends. | [`internal/dxdiag/dxdiag.go`](internal/dxdiag/dxdiag.go) `Run` |
| **One-time delivery.** The code expires after 10 minutes, can be used once, and the server drops the profile as soon as the browser tab collects it. | [Pairing flow](#how-pairing-works) |
| **You can rebuild it.** The build is plain `go build` with no code generation or vendored blobs; compile it yourself with the commands below. | [Build from source](#build-from-source) |

The scanner runs two Windows built-ins as child processes: `dxdiag.exe` (DirectX capability report, hidden window, output to a temp file) and `powercfg.exe` (reads the active power plan). Both are read-only and are launched with a hidden console. It also reads a handful of standard read-only `HKLM` registry values (Windows version, DirectX version, active power scheme).

### Antivirus false positives

The published binary is an **unsigned** Go executable that enumerates hardware and makes one HTTPS request - a shape Windows Defender and other engines sometimes flag heuristically (`Trojan:Win32/Bearfoos.A!ml` and similar). These are false positives. You can avoid trusting the binary entirely by building it from this source or by verifying the SHA-256 of a download:

```powershell
certutil -hashfile CanYourPCRunIt-Scanner.exe SHA256
```

If you distribute builds yourself, sign the executable with an Authenticode certificate to remove the warning.

---

## How pairing works

The design goal is that no hardware is read until the scanner knows where - and whether - the data will be delivered.

1. **Generate a code.** The browser opens `/scanner` and asks the site for a six-character code (no `I`, `O`, `0` or `1`). It is valid for 10 minutes and one read.
2. **Run the scanner.** Type the code and press Send.
3. **Claim.** The scanner sends `POST /api/scanner/pair/<code>/connect` with only its version. The browser tab flips to "connected". A dead, expired or already-used code fails here, in under a second, before any hardware is touched.
4. **Scan.** The scanner reads the local hardware inventory; the window shows each subsystem as it finishes.
5. **Submit.** One `POST /api/scanner/pair/<code>` carries the profile.
6. **Confirm.** The scanner polls the non-consuming `?peek=1` status endpoint until the tab has actually collected the profile, then shows success. No tab collecting it within 18 seconds produces a warning instead of a false success.

No browser is opened, no loopback port is served, and the code is burned on first read. Everything after the scan happens in the browser: the profile is stored in `localStorage` under `myPC` and all analysis runs client-side.

---

## What it collects

| Subsystem | Fields |
| --- | --- |
| **CPU** | Name, manufacturer, architecture, cores, threads, base/boost clock, L1/L2/L3 cache, instruction sets, socket, virtualization, estimated score |
| **GPU** | Name, manufacturer, dedicated/shared/total VRAM, driver version and date, DirectX version, feature level, shader model, Vulkan and OpenGL versions, ray tracing / DLSS / FSR / XeSS / frame generation / mesh shader support, shader units, clocks, memory type and bus width, full adapter list |
| **RAM** | Total and available GB, speed, rated speed, type, channel count, module count, per-module slot/capacity/speed/type/manufacturer/part number |
| **Storage** | Model, bus type, media type, size, free space, drive letter, read/write speed estimates |
| **OS** | Caption, version, build, display version, architecture, 64-bit, install date, DirectX version, kernel version |
| **Display** | Resolution, refresh rate, monitor count, per-monitor name/resolution/refresh/HDR, primary flag, HDR support, colour depth |
| **Motherboard** | Manufacturer, product, version, BIOS vendor/version/date |
| **System** | Manufacturer, model, laptop/desktop, chassis type, power mode, battery present, AC state, active power plan, thermal-throttling risk |
| **Network** | Primary adapter name, connection type, link speed, connected state |
| **Audio** | Default endpoint name, channel count, sample rate, bit depth |

### What it never collects

Files and filenames, documents, photos, passwords and credentials, browser history or cookies, clipboard contents, keystrokes, screen contents, installed programs, hardware serial numbers, MAC addresses, IP addresses, computer or user names, and Windows product keys.

Only the fields in the table above are serialised into the profile and sent. See [`internal/model/types.go`](internal/model/types.go) for the complete document contract.

---

## Requirements

- Windows 10 or 11, 64-bit
- No administrator rights
- No installer, no runtime, no account
- Outbound internet access to the website you pair with

---

## Build from source

Install [Go 1.23+](https://go.dev/dl/), then:

```powershell
git clone https://github.com/unp4tched/cypri-scanner.git
cd cypri-scanner

# Production build - pairs with https://canyourpcrunit.com by default
go build -trimpath -ldflags "-s -w -X main.defaultSiteURL=https://canyourpcrunit.com" `
  -o dist/CanYourPCRunIt-Scanner.exe .

# Checksum next to the binary
(Get-FileHash dist/CanYourPCRunIt-Scanner.exe -Algorithm SHA256).Hash.ToLower() `
  + "  CanYourPCRunIt-Scanner.exe" | Set-Content dist/CanYourPCRunIt-Scanner.exe.sha256
```

Or run the included script:

```powershell
.\build.ps1                                   # production build
.\build.ps1 -Site http://localhost:3000       # build against a local site
```

The executable is named `CanYourPCRunIt-Scanner.exe` so it matches the download on the website. `app.manifest` is provided for optional embedding (for example with `go-winres` or `mt.exe`); it marks the build `asInvoker` and declares Windows 10/11 compatibility.

---

## Command-line flags

| Flag | Meaning |
| --- | --- |
| `-site <url>` | Website to pair with. Defaults to the `main.defaultSiteURL` build value (`http://localhost:3000` unless overridden) |
| `-code <CODE>` | Skip the code screen and pair immediately |
| `-out <file>` | Write the scan to a JSON file and exit - **without sending anything** |
| `-headless` | Run without a window, printing progress to the console |
| `-version` | Print the scanner version and exit |
| `-ui-preview <dir>` | Render PNG previews of every window state (development only) |

The offline audit path is handy for checking exactly what the scanner sees without any network access:

```powershell
.\CanYourPCRunIt-Scanner.exe -out scan.json
```

---

## Project layout

```
main.go                     entry point, pairing window flow, flags
app.manifest                asInvoker + Windows compatibility manifest
internal/
  pair/                     claim, submit, status polling
  hardware/                 WMI / registry / API probes, one file per subsystem
  dxdiag/                   runs dxdiag and parses its report
  dxgi/ d3d11.go            adapter memory and feature data
  nvml/                     NVIDIA VRAM and driver data
  vulkan/ opengl/           graphics API version probes
  gui/                      native window, canvas and input (no external UI toolkit)
  model/                    JSON contract shared with the website
  winapi/                   small Win32 helpers
```

No third-party UI framework and no CGO: the window, canvas and PNG encoder are implemented against the Win32 API directly. The only dependencies are `github.com/StackExchange/wmi`, `github.com/go-ole/go-ole` and `golang.org/x/sys`.

---

## Verifying the published build

The website publishes the binary at `/downloads/CanYourPCRunIt-Scanner.exe` and the matching hash in `CanYourPCRunIt-Scanner.exe.sha256`. The current release hash is:

```
99d36c1d18cb8ff501777db81aaca9210bfa89393898633614634f289fd00887
```

Compare it with `certutil -hashfile` output to confirm the download wasn't altered in transit. Building this repository is the stronger check: it shows exactly what the program does. The published release was compiled from the same source as this repository; a local build may not be byte-identical because the executable embeds its module path and build metadata.

---

## License

[MIT](LICENSE). Use it, read it, fork it, audit it.
