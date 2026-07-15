#!/usr/bin/env bash
# Update PrAImate Code to a newer upstream OpenCode release while keeping
# the PrAImate rebrand (banner + disabled update advisory).
#
# What it does, in order:
#   1. Resolve the target OpenCode tag (latest release, or the tag you pass).
#   2. Re-vendor third_party/opencode at that tag (pristine mirror:
#      .git and node_modules stripped — the rebrand stays a build-time
#      patch, never baked into the vendored tree).
#   3. Bump the pinned-ref notes in third_party/README.md and
#      scripts/build-praimate-code.sh.
#   4. VERIFY the rebrand anchors still exist in the new source
#      (praimate-code-rebrand.sh patches by anchor; if upstream moved
#      the <Logo /> or the update-advisory hook, this fails BEFORE the
#      slow build so you can fix the rebrand script first).
#   5. Build via scripts/build-praimate-code.sh (applies the rebrand,
#      bun install + compile) and verify the produced binary really
#      contains the personalized banner.
#   6. Print — or with --push / --release, perform — the repo update:
#      commit the re-vendor, push, and upload the fresh binary to the
#      current GitHub release.
#
# Usage:
#   scripts/update-praimate-code.sh                    # latest upstream tag
#   scripts/update-praimate-code.sh v1.17.20           # explicit tag
#   scripts/update-praimate-code.sh --push             # + git commit & push
#   scripts/update-praimate-code.sh --push --release   # + upload release asset
#   scripts/update-praimate-code.sh --skip-build       # vendor bump only
#
# Requires: git, curl, bun (for the build), gh (only for --release).
# On Windows run under Git Bash — same as the other scripts here.
set -euo pipefail
cd "$(dirname "$0")/.."
REPO_ROOT="$(pwd)"

UPSTREAM_REPO="sst/opencode"
UPSTREAM_URL="https://github.com/$UPSTREAM_REPO"
VENDOR_DIR="third_party/opencode"
VENDOR_PKG="$VENDOR_DIR/packages/opencode/package.json"
README="third_party/README.md"
BUILD_SCRIPT="scripts/build-praimate-code.sh"
REBRAND_SCRIPT="scripts/praimate-code-rebrand.sh"

TAG=""
DO_PUSH=0
DO_RELEASE=0
SKIP_BUILD=0
for arg in "$@"; do
  case "$arg" in
    --push)       DO_PUSH=1 ;;
    --release)    DO_RELEASE=1 ;;
    --skip-build) SKIP_BUILD=1 ;;
    -h|--help)    sed -n '2,33p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    v*)           TAG="$arg" ;;
    *) echo "error: unknown argument '$arg' (tags look like v1.17.20)" >&2; exit 2 ;;
  esac
done

say()  { printf '\033[32m==> %s\033[0m\n' "$*"; }
note() { printf '    %s\n' "$*"; }
die()  { printf '\033[31merror: %s\033[0m\n' "$*" >&2; exit 1; }

command -v git  >/dev/null || die "git is required"
command -v curl >/dev/null || die "curl is required"
[ "$SKIP_BUILD" = 1 ] || command -v bun >/dev/null || die "bun is required for the build (https://bun.sh); use --skip-build to only re-vendor"

# ---- 1. resolve tags -------------------------------------------------------
CURRENT="v$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$VENDOR_PKG" | head -1)"
[ "$CURRENT" != "v" ] || die "couldn't read the vendored version from $VENDOR_PKG"

if [ -z "$TAG" ]; then
  say "resolving latest $UPSTREAM_REPO release"
  TAG="$(curl -fsSL -H 'User-Agent: praimate-updater' \
    "https://api.github.com/repos/$UPSTREAM_REPO/releases/latest" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$TAG" ] || die "couldn't resolve the latest release tag from the GitHub API"
fi
note "vendored: $CURRENT   target: $TAG"
if [ "$CURRENT" = "$TAG" ]; then
  echo "Already on $TAG — nothing to do."
  exit 0
fi

# Refuse to clobber uncommitted vendor changes.
if ! git diff --quiet "$VENDOR_DIR" 2>/dev/null || \
   [ -n "$(git status --porcelain "$VENDOR_DIR")" ]; then
  die "$VENDOR_DIR has uncommitted changes — commit or discard them first"
fi

# ---- 2. re-vendor ----------------------------------------------------------
say "re-vendoring OpenCode $TAG"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
git clone --quiet --depth 1 --branch "$TAG" "$UPSTREAM_URL" "$WORK/opencode"
rm -rf "$WORK/opencode/.git"
find "$WORK/opencode" -name node_modules -type d -prune -exec rm -rf {} + 2>/dev/null || true
rm -rf "$VENDOR_DIR"
mv "$WORK/opencode" "$VENDOR_DIR"
note "replaced $VENDOR_DIR (pristine mirror, .git + node_modules stripped)"

# ---- 3. bump the pinned-ref notes ------------------------------------------
say "updating pinned-ref notes ($CURRENT -> $TAG)"
sed -i "s|\`$CURRENT\`|\`$TAG\`|" "$README" || true
sed -i "s|OPENCODE_REF:-$CURRENT (vendored|OPENCODE_REF:-$TAG (vendored|" "$BUILD_SCRIPT" || true
grep -q "\`$TAG\`" "$README" || note "WARN: couldn't update the pin in $README — fix it by hand"
grep -q "OPENCODE_REF:-$TAG" "$BUILD_SCRIPT" || note "WARN: couldn't update the pin note in $BUILD_SCRIPT — fix it by hand"

# ---- 4. verify the rebrand anchors survive the bump ------------------------
# praimate-code-rebrand.sh patches by anchor; a moved anchor means the
# personalized banner (or the disabled update nag) would silently vanish.
say "verifying rebrand anchors in $TAG"
APP_TSX="$VENDOR_DIR/packages/tui/src/app.tsx"
HOME_TSX="$VENDOR_DIR/packages/tui/src/routes/home.tsx"
ANCHORS_OK=1
if ! grep -q '<Logo />' "$HOME_TSX" 2>/dev/null; then
  ANCHORS_OK=0
  echo "    MISSING: '<Logo />' in packages/tui/src/routes/home.tsx" >&2
fi
if ! grep -q 'installation.update-available' "$APP_TSX" 2>/dev/null; then
  ANCHORS_OK=0
  echo "    MISSING: update-advisory hook in packages/tui/src/app.tsx" >&2
fi
if [ "$ANCHORS_OK" != 1 ]; then
  die "upstream $TAG moved a rebrand anchor. Update $REBRAND_SCRIPT to match
       the new source, then re-run this script. The vendored tree is already
       at $TAG so you can inspect it in place."
fi
note "both anchors present — the banner patch will apply"

# ---- 5. build + banner check -----------------------------------------------
GOOS="$(go env GOOS 2>/dev/null || uname -s | tr '[:upper:]' '[:lower:]')"
GOARCH="$(go env GOARCH 2>/dev/null || echo amd64)"
EXT=""; [ "$GOOS" = "windows" ] && EXT=".exe"
BIN="dist/$GOOS-$GOARCH/praimate-code$EXT"

if [ "$SKIP_BUILD" = 1 ]; then
  note "(--skip-build: not compiling; run $BUILD_SCRIPT when ready)"
else
  say "building praimate-code $TAG (bun install + compile — several minutes)"
  bash "$BUILD_SCRIPT"
  [ -f "$BIN" ] || die "build finished but $BIN was not produced"

  # The figlet banner is injected as a plain JS string; this fragment of
  # the "praimate-code" lettering survives compilation verbatim.
  say "verifying the personalized banner is inside the binary"
  if grep -aq "_ __ _ _ __ _(_)_ __" "$BIN"; then
    note "banner found in $BIN"
  else
    die "the praimate-code banner is NOT in the built binary — the rebrand
       did not apply. Check the WARN lines from $REBRAND_SCRIPT above."
  fi
  if grep -aq "praimate:managed-build" "$BIN"; then
    note "update-advisory patch found in $BIN"
  else
    note "WARN: update-advisory patch not detected in the binary"
  fi

  # Baseline (no-AVX2) variant — x64 only. Bun's default x64 build
  # crashes with an illegal instruction (0xc000001d on Windows) on CPUs
  # and VMs without AVX2; the installer downloads the -baseline asset
  # on such hosts, so the release must ship it.
  if [ "$GOARCH" = amd64 ]; then
    say "building the baseline (no-AVX2) variant"
    BASELINE=1 bash "$BUILD_SCRIPT"
    BASELINE_BIN="dist/$GOOS-$GOARCH/praimate-code-baseline$EXT"
    [ -f "$BASELINE_BIN" ] || die "baseline build finished but $BASELINE_BIN was not produced"
  fi
fi

# ---- 6. repo update: perform or explain -------------------------------------
PRAIMATE_VERSION="$(sed -n 's/^var Current = "\(.*\)"$/\1/p' internal/version/version.go)"
ASSET="praimate-code-$GOOS-$GOARCH$EXT"

if [ "$DO_PUSH" = 1 ]; then
  say "committing the re-vendor"
  git add "$VENDOR_DIR" "$README" "$BUILD_SCRIPT"
  git commit -m "praimate-code: bump vendored OpenCode $CURRENT -> $TAG

Pristine re-vendor of $UPSTREAM_URL at $TAG; the PrAImate rebrand
(banner + disabled update advisory) stays a build-time patch and its
anchors were verified against the new source."
  git push origin HEAD
else
  say "next step — commit the re-vendor (or re-run with --push):"
  note "git add $VENDOR_DIR $README $BUILD_SCRIPT"
  note "git commit -m 'praimate-code: bump vendored OpenCode $CURRENT -> $TAG'"
  note "git push origin main"
fi

if [ "$SKIP_BUILD" != 1 ]; then
  if [ "$DO_RELEASE" = 1 ]; then
    command -v gh >/dev/null || die "gh CLI is required for --release"
    say "uploading $ASSET to release $PRAIMATE_VERSION"
    cp "$BIN" "dist/$ASSET"
    ( cd dist && sha256sum "$ASSET" > "$ASSET.sha256" )
    gh release upload "$PRAIMATE_VERSION" "dist/$ASSET" "dist/$ASSET.sha256" --clobber
    if [ "$GOARCH" = amd64 ] && [ -f "${BASELINE_BIN:-}" ]; then
      BASELINE_ASSET="praimate-code-$GOOS-$GOARCH-baseline$EXT"
      say "uploading $BASELINE_ASSET (no-AVX2 variant) to release $PRAIMATE_VERSION"
      cp "$BASELINE_BIN" "dist/$BASELINE_ASSET"
      ( cd dist && sha256sum "$BASELINE_ASSET" > "$BASELINE_ASSET.sha256" )
      gh release upload "$PRAIMATE_VERSION" "dist/$BASELINE_ASSET" "dist/$BASELINE_ASSET.sha256" --clobber
    fi
    # Keep the aggregate checksum file honest if the sibling .sha256
    # files are still lying around from the release build.
    if ls dist/praimate-*.sha256 >/dev/null 2>&1; then
      ( cd dist && cat ./*.sha256 > SHA256SUMS )
      gh release upload "$PRAIMATE_VERSION" dist/SHA256SUMS --clobber
      note "SHA256SUMS regenerated + re-uploaded"
    fi
    note "done — the GUI/TUI 'Install PrAImate Code' download now serves $TAG"
  else
    say "next step — publish the new binary (or re-run with --release):"
    note "(remember the baseline variant too: praimate-code-$GOOS-$GOARCH-baseline$EXT)"
    note "cp $BIN dist/$ASSET"
    note "cd dist && sha256sum $ASSET > $ASSET.sha256"
    note "gh release upload $PRAIMATE_VERSION dist/$ASSET dist/$ASSET.sha256 --clobber"
    note "cat dist/*.sha256 > dist/SHA256SUMS && gh release upload $PRAIMATE_VERSION dist/SHA256SUMS --clobber"
  fi
  echo
  note "OPTIONAL: the Windows release zip also bundles praimate-code.exe."
  note "To refresh it, re-run scripts/build.ps1 (the fresh binary is already"
  note "in dist/windows-amd64/) and re-upload praimate-windows-amd64.zip"
  note "with its .sha256 the same way."
fi

say "praimate-code is now tracking OpenCode $TAG"
