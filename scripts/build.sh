#!/usr/bin/env bash
# Cross-compile wpc + clade for every supported OS/arch and stage them
# under dist/<os>-<arch>/ ready for distribution. Run from the repo root or
# from anywhere — we cd to the script's parent automatically.
#
# Usage:
#   scripts/build.sh                       # all targets, default version
#   scripts/build.sh linux-amd64           # one target
#   scripts/build.sh --no-archive          # skip the zip/tar.gz step
#   scripts/build.sh --version=0.2.0       # inject a specific version
#   VERSION=0.2.0 scripts/build.sh         # same, via env var
#
# The version is stamped into the binary at link time via
# `-X .../internal/version.Current=$VERSION`, so `clade -version` and
# the self-updater both report it. Default lives in
# internal/version/version.go.
#
# Requires: Go 1.21+, tar + zip (only when archiving).

set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-0.1.12}"
EXTRA_LDFLAGS="${LDFLAGS:--s -w}"  # strip symbols by default — tiny binaries
ARCHIVE=1
TARGETS=()

for arg in "$@"; do
  case "$arg" in
    --no-archive) ARCHIVE=0 ;;
    --version=*) VERSION="${arg#--version=}" ;;
    -h|--help)
      sed -n '2,19p' "$0"
      exit 0
      ;;
    *) TARGETS+=("$arg") ;;
  esac
done

# Combined linker flags: strip + version injection. The Go linker accepts
# multiple -X entries inside one -ldflags string.
LDFLAGS="$EXTRA_LDFLAGS -X github.com/sPROFFEs/Clade/internal/version.Current=$VERSION"

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
    go build -trimpath -ldflags "$LDFLAGS" -o "$out/clade$ext" ./cmd/clade

  # Ship samples + activation docs next to the binaries so the bundle is
  # self-contained — first-run can seed from the same directory.
  cp -R samples "$out/"
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
        if command -v zip >/dev/null 2>&1; then
          ( cd dist && zip -qr "clade-$triplet.zip" "$triplet" )
        elif command -v tar >/dev/null 2>&1; then
          ( cd dist && tar -czf "clade-$triplet.tar.gz" "$triplet" )
          echo "  (no zip on PATH; built tar.gz instead)" >&2
        else
          echo "  (no zip or tar on PATH; skipping archive)" >&2
        fi
        ;;
      *)
        if command -v tar >/dev/null 2>&1; then
          ( cd dist && tar -czf "clade-$triplet.tar.gz" "$triplet" )
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
