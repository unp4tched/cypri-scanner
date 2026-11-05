# Builds CanYourPCRunIt-Scanner.exe and writes its SHA-256 next to it.
#
#   .\build.ps1                                # production (canyourpcrunit.com)
#   .\build.ps1 -Site http://localhost:3000    # local development site
param(
    [string]$Site = "https://canyourpcrunit.com"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

New-Item -ItemType Directory -Force dist | Out-Null

go build -trimpath -ldflags "-s -w -X main.defaultSiteURL=$Site" `
    -o dist/CanYourPCRunIt-Scanner.exe .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$hash = (Get-FileHash dist/CanYourPCRunIt-Scanner.exe -Algorithm SHA256).Hash.ToLower()
"$hash  CanYourPCRunIt-Scanner.exe" | Set-Content -Encoding ascii dist/CanYourPCRunIt-Scanner.exe.sha256

Write-Host "Built dist/CanYourPCRunIt-Scanner.exe"
Write-Host "SHA-256 $hash"
