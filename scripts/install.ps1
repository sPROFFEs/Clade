# Clade installer for Windows.
#
# One-liner:
#   iwr -useb https://raw.githubusercontent.com/sPROFFEs/Clade/main/scripts/install.ps1 | iex
#
# Or with options (you must download then run for arguments to bind):
#   iwr -useb https://… -OutFile install.ps1
#   .\install.ps1 -Mode Source -AllUsers
#
# Or run locally (from inside an extracted release archive or repo checkout):
#   .\scripts\install.ps1
#
# Parameters:
#   -Mode Binary | Source       what to install (prompts if omitted)
#   -Prefix <dir>               install dir (default: %LOCALAPPDATA%\Programs\Clade)
#   -AllUsers                   install to %ProgramFiles%\Clade (needs admin)
#   -Yes                        auto-confirm prompts
#
# The binary path resolves the latest GitHub release via the API.
# Set $env:RELEASE_TAG=<tag> to pin a specific release instead.

[CmdletBinding()]
param(
    [ValidateSet("Binary","Source","")]
    [string]$Mode = "",
    [string]$Prefix = "",
    [switch]$AllUsers,
    [switch]$Yes
)

$ErrorActionPreference = "Stop"

$Repo = "sPROFFEs/Clade"
$SourceBranch = "main"
# Release tag to pull assets from. When unset we resolve "latest" via
# the GitHub API at download time so the installer keeps working as
# the operator publishes new versioned tags (0.1.7, 0.1.8, ...).
# Override with $env:RELEASE_TAG=<tag> to pin a specific release.
$ReleaseTag = $env:RELEASE_TAG

# ---------- pretty ----------
function Step($Text) { Write-Host ""; Write-Host "==> $Text" -ForegroundColor Green }
function Info($Text) { Write-Host "    $Text" -ForegroundColor DarkGray }
function Warn($Text) { Write-Host $Text -ForegroundColor Yellow }
function Fail($Text) { Write-Host $Text -ForegroundColor Red; exit 1 }

# ---------- prompts that work even from iwr | iex ----------
# `iwr | iex` runs the script in the host PowerShell, so Read-Host
# still works. But $Host.UI may be a non-interactive host (PSExec,
# CI). Detect once and route every prompt through this.
$IsInteractive = -not ([Console]::IsInputRedirected) -and ($Host.Name -ne "Default Host")

function Ask($PromptText, $Default) {
    if ($Yes) { return $Default }
    if (-not $IsInteractive) { return $Default }
    $reply = Read-Host "$PromptText [$Default]"
    if ([string]::IsNullOrWhiteSpace($reply)) { return $Default }
    return $reply.Trim()
}

function YesNo($PromptText, $DefaultYes = $true) {
    $hint = if ($DefaultYes) { "[Y/n]" } else { "[y/N]" }
    $default = if ($DefaultYes) { "y" } else { "n" }
    $r = (Ask "$PromptText $hint" $default).ToLower()
    return $r -match "^y(es)?$"
}

# ---------- platform detect ----------
function Get-ArchTriplet {
    $arch = $env:PROCESSOR_ARCHITECTURE
    switch ($arch) {
        "AMD64" { return "windows-amd64" }
        "ARM64" { return "windows-arm64" }
        default { Fail "unsupported PROCESSOR_ARCHITECTURE: $arch" }
    }
}
$Triplet = Get-ArchTriplet

# ---------- locate local binaries (release-archive / repo case) ----------
$here = if ($MyInvocation.MyCommand.Path) {
    Split-Path -Parent $MyInvocation.MyCommand.Path
} else { $null }

function Find-LocalBins {
    $cands = @($PWD.Path)
    if ($here) {
        $cands += $here
        $cands += (Join-Path $here "..")
        $cands += (Join-Path (Join-Path $here "..") (Join-Path "dist" $Triplet))
    }
    foreach ($c in $cands) {
        if ([string]::IsNullOrWhiteSpace($c)) { continue }
        $clade = Join-Path $c "clade.exe"
        $wpc   = Join-Path $c "wpc.exe"
        if ((Test-Path $clade) -and (Test-Path $wpc)) {
            return (Resolve-Path $c).Path
        }
    }
    return $null
}
$LocalBins = Find-LocalBins

# ---------- mode prompt ----------
if (-not $Mode) {
    if ($LocalBins) {
        Info "(found local binaries in $LocalBins - skipping download/build prompt)"
        $Mode = "Local"
    } else {
        Write-Host ""
        Write-Host "How do you want to install Clade?"
        Write-Host "  1. Download a prebuilt release"
        Write-Host "  2. Build from source (needs Go; will offer to install Go if missing)"
        Write-Host "  3. Cancel"
        $choice = Ask "Choose 1, 2, or 3" "1"
        switch ($choice) {
            "1" { $Mode = "Binary" }
            "2" { $Mode = "Source" }
            "3" { Warn "cancelled."; exit 0 }
            default { Fail "invalid choice: $choice" }
        }
    }
}

# ---------- destination ----------
function Choose-Dest {
    if ($Prefix) { return $Prefix }
    if ($AllUsers) {
        return (Join-Path $env:ProgramFiles "Clade")
    }
    return (Join-Path $env:LOCALAPPDATA "Programs\Clade")
}
$Dest = Choose-Dest
if (-not (Test-Path $Dest)) {
    New-Item -ItemType Directory -Path $Dest -Force | Out-Null
}

# Touching %ProgramFiles% needs admin. Detect early.
function Test-IsAdmin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $p  = New-Object Security.Principal.WindowsPrincipal($id)
    return $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}
if ($AllUsers -and -not (Test-IsAdmin)) {
    Fail "-AllUsers needs an elevated PowerShell. Re-open as Administrator and re-run."
}

# ---------- binary path: GitHub release ----------
function Resolve-LatestTag {
    $api = "https://api.github.com/repos/$Repo/releases/latest"
    try {
        $r = Invoke-RestMethod -Uri $api -UseBasicParsing -Headers @{ "User-Agent" = "clade-installer" } -ErrorAction Stop
    } catch {
        Fail "couldn't query GitHub for the latest release ($($_.Exception.Message)). Set `$env:RELEASE_TAG to pin a version and re-run."
    }
    if (-not $r.tag_name) {
        Fail "GitHub API response had no tag_name. Set `$env:RELEASE_TAG to pin a version and re-run."
    }
    return $r.tag_name
}

function Install-Binary {
    Step "Resolving release"
    if (-not $ReleaseTag) {
        $ReleaseTag = Resolve-LatestTag
    }
    $fname = "clade-$Triplet.zip"
    $url   = "https://github.com/$Repo/releases/download/$ReleaseTag/$fname"
    Info "tag:   $ReleaseTag"
    Info "asset: $fname"
    Info "url:   $url"

    Step "Downloading"
    $tmp = New-Item -ItemType Directory -Force -Path (Join-Path $env:TEMP ("clade-install-" + [Guid]::NewGuid().ToString("N")))
    try {
        $zip = Join-Path $tmp.FullName $fname
        Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing -ErrorAction Stop
        Info ("downloaded " + ((Get-Item $zip).Length / 1MB).ToString("0.0") + " MB")

        Step "Extracting"
        Expand-Archive -Path $zip -DestinationPath $tmp.FullName -Force
        $extracted = Join-Path $tmp.FullName $Triplet
        if (-not (Test-Path $extracted)) {
            Fail "unexpected archive layout under $($tmp.FullName)"
        }

        Step "Installing to $Dest"
        Copy-Item -Path (Join-Path $extracted "clade.exe") -Destination $Dest -Force
        Copy-Item -Path (Join-Path $extracted "wpc.exe")   -Destination $Dest -Force
        # Ship the bundled samples next to the binary at the same path
        # the launcher probes in SampleCandidates:
        # "<execDir>\..\share\clade\samples\workpaths". Without this,
        # the first-run "seed example templates" finds nothing.
        $samplesSrc = Join-Path $extracted "samples"
        if (Test-Path $samplesSrc) {
            $samplesDest = Join-Path (Split-Path $Dest -Parent) "share\clade\samples"
            New-Item -ItemType Directory -Force -Path $samplesDest | Out-Null
            Copy-Item -Path (Join-Path $samplesSrc "*") -Destination $samplesDest -Recurse -Force
            Info "samples -> $samplesDest"
        }
        Write-Host "  v clade.exe + wpc.exe installed" -ForegroundColor Green
    } finally {
        if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
    }
}

# ---------- source path: clone + go build ----------
function Have-Go { return [bool](Get-Command go -ErrorAction SilentlyContinue) }

function Install-Go {
    Step "Go isn't installed"
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Host "Can install via winget:"
        Write-Host ""
        Write-Host "    winget install --id GoLang.Go -e --accept-package-agreements --accept-source-agreements"
        Write-Host ""
        if (YesNo "Run this now?") {
            & winget install --id GoLang.Go -e --accept-package-agreements --accept-source-agreements
            # winget puts go on PATH but the current shell won't see it
            # without refreshing. Pull the env var fresh.
            $env:PATH = [Environment]::GetEnvironmentVariable("PATH","Machine") + ";" + [Environment]::GetEnvironmentVariable("PATH","User")
        } else {
            Fail "Cancelled. Install Go from https://go.dev/dl/ and re-run."
        }
    } else {
        Warn "winget isn't available on this system."
        Warn "Install Go manually from https://go.dev/dl/ then re-run this script."
        exit 1
    }
    if (-not (Have-Go)) {
        Fail "Go install reported success but 'go' still isn't on PATH. Open a new PowerShell and re-run."
    }
}

function Install-Source {
    if (-not (Have-Go)) { Install-Go }
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        Fail "git is required for --source builds. Install from https://git-scm.com/ first."
    }

    Step "Cloning repo"
    $tmp = New-Item -ItemType Directory -Force -Path (Join-Path $env:TEMP ("clade-src-" + [Guid]::NewGuid().ToString("N")))
    try {
        & git clone --depth 1 --branch $SourceBranch "https://github.com/$Repo.git" (Join-Path $tmp.FullName "Clade")
        if ($LASTEXITCODE -ne 0) { Fail "git clone failed" }

        Step "Building"
        Push-Location (Join-Path $tmp.FullName "Clade")
        try {
            $env:CGO_ENABLED = "0"
            & go build -trimpath -ldflags '-s -w' -o clade.exe ./cmd/clade
            if ($LASTEXITCODE -ne 0) { Fail "go build (clade) failed" }
            & go build -trimpath -ldflags '-s -w' -o wpc.exe   ./cmd/wpc
            if ($LASTEXITCODE -ne 0) { Fail "go build (wpc) failed" }

            Step "Installing to $Dest"
            Copy-Item -Path ".\clade.exe" -Destination $Dest -Force
            Copy-Item -Path ".\wpc.exe"   -Destination $Dest -Force
            Write-Host "  v clade.exe + wpc.exe installed" -ForegroundColor Green
        } finally {
            Pop-Location
        }
    } finally {
        if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
    }
}

# ---------- local path: bins already next to us ----------
function Install-Local {
    Step "Installing to $Dest"
    Copy-Item -Path (Join-Path $LocalBins "clade.exe") -Destination $Dest -Force
    Copy-Item -Path (Join-Path $LocalBins "wpc.exe")   -Destination $Dest -Force
    Write-Host "  v clade.exe + wpc.exe installed" -ForegroundColor Green
}

# ---------- dispatch ----------
switch ($Mode) {
    "Binary" { Install-Binary }
    "Source" { Install-Source }
    "Local"  { Install-Local }
    default  { Fail "internal: unknown Mode $Mode" }
}

# ---------- PATH ----------
Step "Updating PATH"
$scope = if ($AllUsers) { "Machine" } else { "User" }
$existing = [Environment]::GetEnvironmentVariable("PATH", $scope)
if (-not $existing) { $existing = "" }
$parts = $existing -split ";" | Where-Object { $_ -ne "" }
if ($parts -notcontains $Dest) {
    $newPath = if ($existing.Trim() -eq "") { $Dest } else { "$existing;$Dest" }
    try {
        [Environment]::SetEnvironmentVariable("PATH", $newPath, $scope)
        Write-Host "  v $scope PATH now includes $Dest" -ForegroundColor Green
    } catch {
        Warn "couldn't update $scope PATH automatically: $($_.Exception.Message)"
        Warn "Add this dir to your PATH manually:"
        Write-Host "      $Dest"
    }
    # Also update the current shell so the user can test right away.
    $env:PATH = "$env:PATH;$Dest"
} else {
    Info "(already on $scope PATH)"
}

Step "Done"
Write-Host "Open a new terminal, then try:    clade -version"
