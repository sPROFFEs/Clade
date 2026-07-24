#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEST_TMP="$(mktemp -d)"
trap 'rm -rf "$TEST_TMP"' EXIT
mkdir -p "$TEST_TMP/bin"

cat > "$TEST_TMP/bin/bun" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$1" in
  install)
    for asset in apple-touch-icon-v3.png favicon-v3.ico site.webmanifest social-share.png; do
      test -f "packages/app/public/$asset"
      test ! -L "packages/app/public/$asset"
    done
    ;;
  run)
    mkdir -p dist/test/bin
    : > dist/test/bin/opencode
    chmod +x dist/test/bin/opencode
    ;;
esac
EOF
chmod +x "$TEST_TMP/bin/bun"

PATH="$TEST_TMP/bin:$PATH" OUT="$TEST_TMP/out" \
  bash "$REPO_ROOT/scripts/build-praimate-code.sh" >/dev/null

test -x "$TEST_TMP/out/praimate-code"
echo "praimate-code public assets: ok"
