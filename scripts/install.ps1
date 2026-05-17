# install.ps1 — copy clade.exe + wpc.exe to a directory on $PATH.
#
# Run this either inside an extracted release archive (where
# clade.exe and wpc.exe sit next to this script) or from a repo
# checkout after scripts\build.ps1. The script auto-detects which.
#
# Usage:
#   .\scripts\install.ps1                 # %LOCALAPPDATA%\Programs\Clade
#   .\scripts\install.ps1 -Prefix C:\Tools\Clade
#   .\scripts\install.ps1 -AllUsers       # %ProgramFiles%\Clade (needs admin)
#
# After install, if the target dir isn't on PATH the script appends
# it to the User PATH (or Machine PATH with -AllUsers) so new shells
# pick it up immediately. Existing shells need to be reopened.

[CmdletBinding()]
param(
    [string]$Prefix,
    [switch]$AllUsers
)

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Definition

function Get-ArchTriplet {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "windows-amd64" }
        "ARM64" { return "windows-arm64" }
        default { throw "unsupported PROCESSOR_ARCHITECTURE: $arch" }
    }
}

# Try locations in preference order:
#   1. caller's cwd
#   2. script's own dir (release-archive layout)
#   3. dist\<triplet>\ (repo layout after build.ps1)
function Find-BinariesRoot {
    $candidates = @(
        $PWD.Path,
        $here,
        (Join-Path $here ".."),
        (Join-Path (Join-Path $here "..") (Join-Path "dist" (Get-ArchTriplet)))
    )
    foreach ($c in $candidates) {
        if ([string]::IsNullOrWhiteSpace($c)) { continue }
        $clade = Join-Path $c "clade.exe"
        $wpc   = Join-Path $c "wpc.exe"
        if ((Test-Path $clade) -and (Test-Path $wpc)) {
            return (Resolve-Path $c).Path
        }
    }
    return $null
}

$src = Find-BinariesRoot
if (-not $src) {
    Write-Host "x couldn't find clade.exe + wpc.exe." -ForegroundColor Red
    Write-Host ""
    Write-Host "Either:"
    Write-Host "  - cd into the extracted release archive (where clade.exe and"
    Write-Host "    wpc.exe are), then re-run this; or"
    Write-Host "  - from the repo root, run scripts\build.ps1 first."
    exit 1
}
Write-Host "v found binaries in $src" -ForegroundColor Green

# ---------- pick destination ----------
if (-not $Prefix) {
    if ($AllUsers) {
        $Prefix = Join-Path $env:ProgramFiles "Clade"
    } else {
        $Prefix = Join-Path $env:LOCALAPPDATA "Programs\Clade"
    }
}

if (-not (Test-Path $Prefix)) {
    New-Item -ItemType Directory -Path $Prefix -Force | Out-Null
}

# ---------- install ----------
function Install-One($name) {
    $srcPath = Join-Path $src $name
    $dstPath = Join-Path $Prefix $name
    Copy-Item -Path $srcPath -Destination $dstPath -Force
    Write-Host "v $name installed at $dstPath" -ForegroundColor Green
}
Install-One "clade.exe"
Install-One "wpc.exe"

# ---------- PATH ----------
# Add $Prefix to the User (or Machine with -AllUsers) PATH if it
# isn't already there. We touch the persistent env vars via
# [Environment]::SetEnvironmentVariable so new shells pick it up;
# editing $env:PATH only affects the current shell.
$scope = if ($AllUsers) { "Machine" } else { "User" }
$existing = [Environment]::GetEnvironmentVariable("PATH", $scope)
if (-not $existing) { $existing = "" }

$parts = $existing -split ";" | Where-Object { $_ -ne "" }
if ($parts -notcontains $Prefix) {
    $newPath = if ($existing.Trim() -eq "") { $Prefix } else { "$existing;$Prefix" }
    try {
        [Environment]::SetEnvironmentVariable("PATH", $newPath, $scope)
        Write-Host "v $scope PATH now includes $Prefix" -ForegroundColor Green
    } catch {
        Write-Host ""
        Write-Host "! couldn't update $scope PATH automatically: $($_.Exception.Message)" -ForegroundColor Yellow
        Write-Host "  Add this dir to your PATH manually:" -ForegroundColor Yellow
        Write-Host "      $Prefix"
    }
    # Also update the current shell so the user can test right away.
    $env:PATH = "$env:PATH;$Prefix"
} else {
    Write-Host "(already on $scope PATH)" -ForegroundColor DarkGray
}

Write-Host ""
Write-Host "Open a new terminal, then try:    clade -version"
