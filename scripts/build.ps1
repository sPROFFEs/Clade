# Cross-compile wpc + praimate for every supported OS/arch and stage them
# under dist\<os>-<arch>\ ready for distribution.
#
# Usage:
#   .\scripts\build.ps1                            # all targets, default version
#   .\scripts\build.ps1 -Targets linux-amd64       # one target
#   .\scripts\build.ps1 -NoArchive                 # skip zip step
#   .\scripts\build.ps1 -Version 0.2.0             # inject a specific version
#
# The version is stamped into the binary at link time via
# `-X .../internal/version.Current=$Version`, so `praimate -version` and
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
    [string] $Version = "1.1.18",
    [string] $LdFlags = "-s -w",
    [switch] $NoArchive
)

$ErrorActionPreference = "Stop"

# Combine strip flags + version injection into one -ldflags string. The
# Go linker accepts multiple -X entries inside it.
$FullLdFlags = "$LdFlags -X github.com/sPROFFEs/PrAImate/internal/version.Current=$Version"
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
    & go build -trimpath -ldflags $FullLdFlags -o (Join-Path $out "praimate$ext") "./cmd/praimate"
    if ($LASTEXITCODE -ne 0) { throw "praimate build failed for $triplet" }
    # Transitional shim through 1.0.x; removed in 1.1.

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
        $archiveBase = "praimate-$triplet"
        if ($goos -eq "windows") {
            $zip = Join-Path "dist" "$archiveBase.zip"
            if (Test-Path $zip) { Remove-Item $zip }
            # IMPORTANT: Compress-Archive AND
            # [IO.Compression.ZipFile]::CreateFromDirectory both write
            # Windows BACKSLASH separators into the ZIP central
            # directory on PowerShell 5.1 / .NET Framework 4.x —
            # because both go through the same buggy underlying
            # API. The ZIP spec (PKWARE APPNOTE §4.4.17.1) requires
            # forward slashes, and Go's archive/zip reads raw, so a
            # backslash-laden zip makes path.Base() return the whole
            # path. The self-updater then fails with
            # "praimate.exe not found in archive" (the 0.1.14 regression).
            #
            # The fix is to write entries one by one with explicitly
            # normalized "/" names. Same .NET API, no auto-conversion.
            #
            # Both assemblies are needed on a clean PS session:
            # System.IO.Compression carries ZipArchiveMode / CompressionLevel;
            # System.IO.Compression.FileSystem carries ZipFile.
            Add-Type -AssemblyName System.IO.Compression
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            $distAbs = (Resolve-Path "dist").Path
            $zipAbs = Join-Path $distAbs "$archiveBase.zip"
            $srcAbs = (Resolve-Path $out).Path
            $srcParent = Split-Path $srcAbs -Parent
            $zw = [System.IO.Compression.ZipFile]::Open(
                $zipAbs,
                [System.IO.Compression.ZipArchiveMode]::Create)
            try {
                Get-ChildItem -Path $srcAbs -Recurse -File | ForEach-Object {
                    $relPath = $_.FullName.Substring($srcParent.Length + 1)
                    $entryName = $relPath.Replace([char]92, [char]47)
                    $entry = $zw.CreateEntry(
                        $entryName,
                        [System.IO.Compression.CompressionLevel]::Optimal)
                    $entryStream = $entry.Open()
                    try {
                        $fileStream = [System.IO.File]::OpenRead($_.FullName)
                        try { $fileStream.CopyTo($entryStream) }
                        finally { $fileStream.Dispose() }
                    } finally { $entryStream.Dispose() }
                }
            } finally { $zw.Dispose() }
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
