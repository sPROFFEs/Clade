# Cross-compile wpc + clade for every supported OS/arch and stage them
# under dist\<os>-<arch>\ ready for distribution.
#
# Usage:
#   .\scripts\build.ps1                            # all targets, default version
#   .\scripts\build.ps1 -Targets linux-amd64       # one target
#   .\scripts\build.ps1 -NoArchive                 # skip zip step
#   .\scripts\build.ps1 -Version 0.2.0             # inject a specific version
#
# The version is stamped into the binary at link time via
# `-X .../internal/version.Current=$Version`, so `clade -version` and
# the self-updater both report it. Default lives in
# internal\version\version.go.
#
# Requires: Go 1.21+. Uses Compress-Archive (built into PowerShell 5+) for
# zips; tar.gz on non-Windows targets needs `tar` (built into Windows 10+).

[CmdletBinding()]
param(
    [string[]] $Targets = @(
        "windows-amd64",
        "linux-amd64",
        "linux-arm64",
        "darwin-amd64",
        "darwin-arm64"
    ),
    [string] $Version = "0.1.13",
    [string] $LdFlags = "-s -w",
    [switch] $NoArchive
)

$ErrorActionPreference = "Stop"

# Combine strip flags + version injection into one -ldflags string. The
# Go linker accepts multiple -X entries inside it.
$FullLdFlags = "$LdFlags -X github.com/sPROFFEs/Clade/internal/version.Current=$Version"
Write-Host "Building version $Version"

# Repo root is the parent of the scripts dir.
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $repoRoot

if (-not (Test-Path "dist")) { New-Item -ItemType Directory -Path "dist" | Out-Null }

function Build-One($triplet) {
    $goos, $goarch = $triplet -split "-", 2
    $ext = ""
    if ($goos -eq "windows") { $ext = ".exe" }

    $out = Join-Path "dist" $triplet
    if (-not (Test-Path $out)) { New-Item -ItemType Directory -Path $out | Out-Null }

    Write-Host "→ $triplet"

    $env:GOOS = $goos
    $env:GOARCH = $goarch
    $env:CGO_ENABLED = "0"

    & go build -trimpath -ldflags $FullLdFlags -o (Join-Path $out "wpc$ext") "./cmd/wpc"
    if ($LASTEXITCODE -ne 0) { throw "wpc build failed for $triplet" }
    & go build -trimpath -ldflags $FullLdFlags -o (Join-Path $out "clade$ext") "./cmd/clade"
    if ($LASTEXITCODE -ne 0) { throw "clade build failed for $triplet" }

    # Bundle samples + docs so the binary is self-sufficient at first run.
    Copy-Item -Recurse -Force "samples" (Join-Path $out "samples")
    $docsOut = Join-Path $out "docs"
    if (-not (Test-Path $docsOut)) { New-Item -ItemType Directory -Path $docsOut | Out-Null }
    Copy-Item "docs/ACTIVATION.md","docs/TARGETS.md","docs/SCHEMA.md","docs/QUICKSTART.md" $docsOut
    Copy-Item "README.md","LICENSE" $out

    # Ship the install scripts inside the archive so the README's
    # `.\scripts\install.ps1` (or `./scripts/install.sh`) line works
    # right after extraction. Both flavors ship in every bundle.
    $scriptsOut = Join-Path $out "scripts"
    if (-not (Test-Path $scriptsOut)) { New-Item -ItemType Directory -Path $scriptsOut | Out-Null }
    Copy-Item "scripts/install.sh","scripts/install.ps1" $scriptsOut

    if (-not $NoArchive) {
        $archiveBase = "clade-$triplet"
        if ($goos -eq "windows") {
            $zip = Join-Path "dist" "$archiveBase.zip"
            if (Test-Path $zip) { Remove-Item $zip }
            # Pass the directory itself (not "$out\*") so the archive
            # contains a top-level <triplet>/ entry matching what
            # install.ps1's Expand-Archive step expects to find.
            Compress-Archive -Path $out -DestinationPath $zip
        } else {
            # tar -czf — works on Windows 10+ (bsdtar bundled in System32).
            $tgz = "$archiveBase.tar.gz"
            Push-Location "dist"
            try {
                & tar -czf $tgz $triplet
                if ($LASTEXITCODE -ne 0) { throw "tar failed for $triplet" }
            } finally {
                Pop-Location
            }
        }
    }
}

foreach ($t in $Targets) {
    Build-One $t
}

# Clean up env vars we set so the user's shell isn't sticky.
Remove-Item Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "Built $($Targets.Count) target(s). Artifacts under dist\:"
Get-ChildItem dist | Format-Table Name, Length -AutoSize
