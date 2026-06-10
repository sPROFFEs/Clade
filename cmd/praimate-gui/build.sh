#!/usr/bin/env bash
# Build the PrAImate GUI binary. The frontend must build first because
# the Go binary embeds frontend/dist via //go:embed.
#
# Linux needs webkit2gtk-4.1 dev headers:
#   apt install libwebkit2gtk-4.1-dev libgtk-3-dev
set -euo pipefail
cd "$(dirname "$0")"

(cd frontend && npm install && npm run build)

# Wails REQUIRES the `desktop` + `production` build tags — without them
# the binary compiles but panics at startup ("Wails applications will
# not build without the correct build tags"). On Linux we also select
# the modern webkit (webkit2gtk-4.1) via webkit2_41; macOS/Windows
# ignore that tag.
TAGS="desktop,production"
case "$(uname -s)" in
  Linux) TAGS="$TAGS,webkit2_41" ;;
esac

go build -trimpath -tags "$TAGS" -ldflags '-s -w' -o praimate-gui .
echo "built ./praimate-gui"
