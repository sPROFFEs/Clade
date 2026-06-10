#!/usr/bin/env bash
# Build the PrAImate GUI binary. The frontend must build first because
# the Go binary embeds frontend/dist via //go:embed.
#
# Linux needs webkit2gtk-4.1 dev headers:
#   apt install libwebkit2gtk-4.1-dev libgtk-3-dev
set -euo pipefail
cd "$(dirname "$0")"

(cd frontend && npm install && npm run build)

TAGS="webkit2_41"
case "$(uname -s)" in
  Darwin|MINGW*|MSYS*) TAGS="" ;;
esac

if [ -n "$TAGS" ]; then
  go build -trimpath -tags "$TAGS" -ldflags '-s -w' -o praimate-gui .
else
  go build -trimpath -ldflags '-s -w' -o praimate-gui .
fi
echo "built ./praimate-gui"
