$ErrorActionPreference = "Stop"

$Repo = "schtonn/clipall"
$Binary = "clipall.exe"
$InstallDir = "$env:LOCALAPPDATA\clipall"

Write-Host "==> Fetching latest release..." -ForegroundColor Cyan
$Release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
$Tag = $Release.tag_name
Write-Host "==> Latest release: $Tag" -ForegroundColor Cyan

$OutPath = Join-Path $InstallDir $Binary

# Check the exact path this script installs, rather than another binary on PATH.
if (Test-Path $OutPath) {
    $Current = & $OutPath --version 2>$null
    if ($Current -eq "clipall $Tag") {
        Write-Host "==> Already up to date ($Tag)" -ForegroundColor Green
        exit 0
    }
    Write-Host "==> Updating: $Current -> $Tag" -ForegroundColor Cyan
} else {
    Write-Host "==> Installing: $Tag" -ForegroundColor Cyan
}

$Asset = "clipall-windows-amd64.exe"
$Url = "https://github.com/$Repo/releases/download/$Tag/$Asset"
$ChecksumUrl = "https://github.com/$Repo/releases/download/$Tag/checksums.txt"

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

Write-Host "==> Downloading $Url..." -ForegroundColor Cyan
$TempPath = Join-Path ([System.IO.Path]::GetTempPath()) ("clipall-" + [guid]::NewGuid().ToString() + ".exe")
$ChecksumPath = Join-Path ([System.IO.Path]::GetTempPath()) ("clipall-checksums-" + [guid]::NewGuid().ToString() + ".txt")

try {
    # Keep the installed binary intact if the download fails.
    Invoke-WebRequest -Uri $Url -OutFile $TempPath -UseBasicParsing
    Invoke-WebRequest -Uri $ChecksumUrl -OutFile $ChecksumPath -UseBasicParsing

    Write-Host "==> Verifying SHA-256 checksum..." -ForegroundColor Cyan
    $ChecksumLine = Get-Content $ChecksumPath | Where-Object { $_ -match "^([0-9a-fA-F]{64})\s+$([regex]::Escape($Asset))$" } | Select-Object -First 1
    if (-not $ChecksumLine) {
        throw "No checksum found for $Asset"
    }
    $Expected = ($ChecksumLine -split '\s+')[0].ToLowerInvariant()
    $Actual = (Get-FileHash -Path $TempPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected) {
        throw "Checksum mismatch for $Asset"
    }

    # Windows does not allow replacing a running executable. Stop only clipall
    # instances launched from this install directory before the atomic update.
    $Running = Get-Process clipall -ErrorAction SilentlyContinue | Where-Object {
        try { $_.Path -eq $OutPath } catch { $false }
    }
    if ($Running) {
        Write-Host "==> Stopping the installed clipall process..." -ForegroundColor Cyan
        $Running | Stop-Process -Force
        $Running | Wait-Process -ErrorAction SilentlyContinue
    }

    Move-Item -Path $TempPath -Destination $OutPath -Force
} finally {
    if (Test-Path $TempPath) {
        Remove-Item $TempPath -Force
    }
    if (Test-Path $ChecksumPath) {
        Remove-Item $ChecksumPath -Force
    }
}

# Add to PATH if not already present.
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    Write-Host "==> Added $InstallDir to user PATH" -ForegroundColor Cyan
}

Write-Host "==> Installed clipall $Tag to $OutPath" -ForegroundColor Green
Write-Host ""
Write-Host "  Run: clipall"
Write-Host "  First run discovers Tailscale peers and offers to enable autostart."
Write-Host "  Override discovery: clipall --peers <hostname>:9876"
Write-Host ""
Write-Host "  Restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
