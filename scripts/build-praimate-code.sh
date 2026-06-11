#!/usr/bin/env bash
# Build "PrAImate Code" — our version-pinned, rebranded build of
# OpenCode (https://github.com/sst/opencode, MIT). Produces a single
# self-contained executable (Bun runtime + web UI + agent all baked in)
# named `praimate-code`, dropped into the directory given by $OUT
# (default: dist/<goos>-<goarch>/).
#
# LICENSE / ATTRIBUTION (important): OpenCode is MIT licensed, which
# permits forking, modifying and redistributing — INCLUDING under a
# different product name — provided the original copyright notice is
# retained. This script therefore copies OpenCode's LICENSE next to the
# binary and writes a NOTICE recording the upstream source + commit.
# Do NOT remove those. We rebrand the product NAME, not the attribution.
#
# Requires: bun (>=1.3), git, ~2GB disk, network. The build downloads
# OpenCode's full dependency tree and compiles a ~100MB binary.
#
# Usage:
#   scripts/build-praimate-code.sh                 # native target → dist/<triplet>/
#   OUT=/some/dir scripts/build-praimate-code.sh    # custom output dir
#   OPENCODE_REF=v1.17.3 scripts/build-praimate-code.sh  # pin override

set -euo pipefail
cd "$(dirname "$0")/.."
REPO_ROOT="$(pwd)"

# Pinned upstream. Bump deliberately after smoke-testing against the
# PrAImate launcher; the whole point of "managed by us" is that this
# moves on our cadence, not upstream's.
OPENCODE_REF="${OPENCODE_REF:-v1.17.3}"
OPENCODE_URL="https://github.com/sst/opencode"

GOOS="$(go env GOOS 2>/dev/null || uname -s | tr '[:upper:]' '[:lower:]')"
GOARCH="$(go env GOARCH 2>/dev/null || echo amd64)"
OUT="${OUT:-$REPO_ROOT/dist/$GOOS-$GOARCH}"
EXT=""
[ "$GOOS" = "windows" ] && EXT=".exe"

command -v bun >/dev/null 2>&1 || { echo "error: bun not found on PATH (install from https://bun.sh)"; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
echo "→ vendoring OpenCode $OPENCODE_REF into $WORK"
git clone --depth 1 --branch "$OPENCODE_REF" "$OPENCODE_URL" "$WORK/opencode" 2>&1 | tail -2
SRC="$WORK/opencode"

echo "→ bun install (this is the heavy step)"
( cd "$SRC" && bun install )

# --- Rebrand pass (name-level, functionally safe) ----------------------
# We rename the produced binary and replace the most visible product
# name in the CLI package's user-facing strings. We deliberately do NOT
# rewrite internal identifiers, package names, config-dir paths, or the
# server's API contracts — a blind global rename would break the build
# and the agent. Deeper cosmetic rebranding is incremental follow-up.
echo "→ applying name-level rebrand"
bash "$REPO_ROOT/scripts/praimate-code-rebrand.sh" "$SRC" || {
  echo "  (rebrand pass skipped/failed; continuing with upstream branding)" >&2
}

echo "→ building standalone binary (native target only)"
# --single restricts build.ts to the current platform, so it doesn't
# download a Bun runtime for every OS/arch (which is slow and fragile
# over the network). We cross-build other targets in separate runs.
( cd "$SRC/packages/opencode" && bun run build --single )

# build.ts emits dist/<name>/bin/opencode — find it.
BUILT="$(find "$SRC/packages/opencode/dist" -type f -name "opencode$EXT" 2>/dev/null | head -1)"
if [ -z "$BUILT" ]; then
  echo "error: could not find built opencode binary under $SRC/packages/opencode/dist"
  exit 1
fi

mkdir -p "$OUT"
install -m 0755 "$BUILT" "$OUT/praimate-code$EXT"
cp "$SRC/LICENSE" "$OUT/PRAIMATE-CODE-LICENSE"
cat > "$OUT/PRAIMATE-CODE-NOTICE" <<EOF
PrAImate Code is a rebranded build of OpenCode.

Upstream:  $OPENCODE_URL
Pinned ref: $OPENCODE_REF
License:   MIT (see PRAIMATE-CODE-LICENSE)

OpenCode is the work of its authors; PrAImate ships a version-pinned
build under a different product name as permitted by the MIT license.
The original copyright notice is retained in PRAIMATE-CODE-LICENSE.
EOF

echo
echo "✓ built $OUT/praimate-code$EXT"
ls -lh "$OUT/praimate-code$EXT"
