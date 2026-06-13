#!/usr/bin/env bash
# Cross-compile wpc + praimate (TUI) for every supported OS/arch and
# stage them under dist/<os>-<arch>/ ready for distribution. Run from
# the repo root or from anywhere — we cd to the script's parent
# automatically.
#
# Usage:
#   scripts/build.sh                       # all targets, default version
#   scripts/build.sh linux-amd64           # one target
#   scripts/build.sh --no-archive          # skip the zip/tar.gz step
#   scripts/build.sh --version=1.0.1       # inject a specific version
#   scripts/build.sh --with-gui            # also build praimate-gui (native target only)
#   VERSION=1.0.1 scripts/build.sh         # same, via env var
#
# The version is stamped into the binary at link time via
# `-X .../internal/version.Current=$VERSION`, so `praimate -version` and
# the self-updater both report it. Default lives in
# internal/version/version.go.
#
# GUI coverage with --with-gui:
#   - native os/arch       — full cgo build (Linux needs webkit2gtk-4.1
#                            dev headers; macOS needs Xcode CLT)
#   - windows-amd64        — ALWAYS cross-compilable: Wails v2's Windows
#                            backend is pure Go syscalls (WebView2 loads
#                            at runtime), so CGO_ENABLED=0 works
#   - everything else      — skipped; those platforms build from source
#                            via cmd/praimate-gui/build.sh
#
# `praimate --gui` dispatches to the praimate-gui binary shipped next
# to it, so bundles that include the GUI get AIO behaviour for free.
#
# Requires: Go 1.21+, tar + zip (only when archiving); node+npm and
# webkit2gtk-4.1 dev headers when --with-gui is used on Linux.

set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-1.1.21}"
EXTRA_LDFLAGS="${LDFLAGS:--s -w}"  # strip symbols by default — tiny binaries
ARCHIVE=1
WITH_GUI=0
WITH_CODE=0
WITH_GRAPHIFY=0
TARGETS=()

for arg in "$@"; do
  case "$arg" in
    --no-archive) ARCHIVE=0 ;;
    --with-gui) WITH_GUI=1 ;;
    --with-code) WITH_CODE=1 ;;
    --with-graphify) WITH_GRAPHIFY=1 ;;
    --version=*) VERSION="${arg#--version=}" ;;
    -h|--help)
      sed -n '2,26p' "$0"
      exit 0
      ;;
    *) TARGETS+=("$arg") ;;
  esac
done

# Combined linker flags: strip + version injection. The Go linker accepts
# multiple -X entries inside one -ldflags string.
LDFLAGS="$EXTRA_LDFLAGS -X github.com/sPROFFEs/PrAImate/internal/version.Current=$VERSION"

echo "Building version $VERSION"

if [ ${#TARGETS[@]} -eq 0 ]; then
  TARGETS=(
    windows-amd64
    linux-amd64
    linux-arm64
    darwin-amd64
    darwin-arm64
  )
fi

NATIVE_TRIPLET="$(go env GOOS)-$(go env GOARCH)"

mkdir -p dist

build_one() {
  local triplet="$1"
  local goos="${triplet%-*}"
  local goarch="${triplet#*-}"
  local ext=""
  if [ "$goos" = "windows" ]; then ext=".exe"; fi

  local out="dist/$triplet"
  mkdir -p "$out"

  echo "→ $triplet"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out/wpc$ext" ./cmd/wpc
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$out/praimate$ext" ./cmd/praimate

  # GUI: native target (full cgo) + windows-amd64 (pure-Go cross).
  if [ "$WITH_GUI" = "1" ]; then
    if [ "$triplet" = "$NATIVE_TRIPLET" ]; then
      echo "  + praimate-gui (native)"
      bash cmd/praimate-gui/build.sh
      cp "cmd/praimate-gui/praimate-gui$ext" "$out/praimate-gui$ext"
    elif [ "$triplet" = "windows-amd64" ]; then
      echo "  + praimate-gui (windows cross)"
      # Frontend must already be built (the native branch or a prior
      # run does it); build it here if dist assets are missing.
      if [ ! -f cmd/praimate-gui/frontend/dist/index.html ]; then
        ( cd cmd/praimate-gui/frontend && npm install && npm run build )
      fi
      ( cd cmd/praimate-gui && \
        GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
        go build -trimpath -tags desktop,production \
          -ldflags "-s -w -H windowsgui" -o praimate-gui.exe . )
      cp cmd/praimate-gui/praimate-gui.exe "$out/praimate-gui.exe"
    fi
  fi

  # PrAImate Code (rebranded OpenCode): native target only — it's a
  # Bun-compiled standalone that can't cross-compile from here. Needs
  # bun on PATH; skipped quietly if absent so a plain build still works.
  if [ "$WITH_CODE" = "1" ] && [ "$triplet" = "$NATIVE_TRIPLET" ]; then
    if command -v bun >/dev/null 2>&1; then
      echo "  + praimate-code (native, via build-praimate-code.sh)"
      OUT="$out" bash scripts/build-praimate-code.sh
    else
      echo "  (skipping praimate-code: bun not on PATH)" >&2
    fi
  fi

  # Bundled graphify: PyInstaller-frozen standalone, native target only
  # (can't cross-compile). Needs uv; skipped quietly otherwise.
  if [ "$WITH_GRAPHIFY" = "1" ] && [ "$triplet" = "$NATIVE_TRIPLET" ]; then
    if command -v uv >/dev/null 2>&1; then
      echo "  + praimate-graphify (native, via build-graphify.sh)"
      OUT="$out" bash scripts/build-graphify.sh
    else
      echo "  (skipping praimate-graphify: uv not on PATH)" >&2
    fi
  fi

  # Ship samples + activation docs next to the binaries so the bundle is
  # self-contained — first-run can seed from the same directory.
  cp -R samples "$out/"
  # App icon at the bundle root — install.sh uses it for the Linux
  # .desktop shortcuts (Windows shortcuts get the monke icon from the
  # exes' embedded resources: cmd/praimate-gui + cmd/praimate .syso).
  cp cmd/praimate-gui/frontend/src/assets/monke-icon.png "$out/praimate.png"
  mkdir -p "$out/docs"
  cp docs/ACTIVATION.md docs/TARGETS.md docs/SCHEMA.md docs/QUICKSTART.md "$out/docs/"
  cp README.md LICENSE "$out/"

  # Drop the install scripts into a scripts/ subdir so the README's
  # `./scripts/install.sh` snippet works straight out of the archive.
  # Both .sh and .ps1 ship in every bundle — easier than special-casing
  # by OS, and barely any size.
  mkdir -p "$out/scripts"
  cp scripts/install.sh scripts/install.ps1 "$out/scripts/"
  chmod +x "$out/scripts/install.sh" 2>/dev/null || true

  if [ "$ARCHIVE" = "1" ]; then
    case "$goos" in
      windows)
        # IMPORTANT: the windows asset must be a REAL zip with
        # forward-slash entry names — the shipped updater extracts via
        # archive/zip + path.Base, and PowerShell's Compress-Archive
        # writes backslash entries it can't match ("praimate.exe not
        # found in archive"). Windows' native bsdtar produces correct
        # zips; git-bash's GNU tar does NOT (-a is silently wrong).
        if command -v zip >/dev/null 2>&1; then
          ( cd dist && zip -qr "praimate-$triplet.zip" "$triplet" )
        elif [ -x "/c/Windows/System32/tar.exe" ]; then
          ( cd dist && /c/Windows/System32/tar.exe \
              --options zip:compression=deflate \
              -a -cf "praimate-$triplet.zip" "$triplet" )
        elif command -v tar >/dev/null 2>&1; then
          ( cd dist && tar -czf "praimate-$triplet.tar.gz" "$triplet" )
          echo "  (no zip on PATH; built tar.gz instead)" >&2
        else
          echo "  (no zip or tar on PATH; skipping archive)" >&2
        fi
        ;;
      *)
        if command -v tar >/dev/null 2>&1; then
          ( cd dist && tar -czf "praimate-$triplet.tar.gz" "$triplet" )
        else
          echo "  (no tar on PATH; skipping archive)" >&2
        fi
        ;;
    esac
  fi
}

for t in "${TARGETS[@]}"; do
  build_one "$t"
done

echo
echo "Built ${#TARGETS[@]} target(s). Artifacts under dist/:"
ls -1 dist/
