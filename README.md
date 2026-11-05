# Cypri Scanner

The desktop hardware scanner for [CanYourPCRunIt](https://canyourpcrunit.com) - published as `CanYourPCRunIt-Scanner.exe`.

It reads your PC's hardware inventory locally and delivers it to the browser tab that generated a one-time pairing code. That's all it does.

**100% safe and open source.** Every line that runs on your machine is in this repository, MIT licensed. No installer, no admin rights, no background service, no telemetry, no account.

- Website: <https://canyourpcrunit.com/scanner>
- Source: <https://github.com/unp4tched/cypri-scanner>
- Current version: **1.2.2** (Windows 10 / 11, 64-bit)

---


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

## Requirements

- Windows 10 or 11, 64-bit
- No administrator rights
- No installer, no runtime, no account
- Outbound internet access to the website you pair with

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
