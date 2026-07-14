#!/usr/bin/env bash
# Name-level rebrand of a vendored OpenCode checkout to "PrAImate Code".
# Invoked by build-praimate-code.sh with the checkout dir as $1.
#
# SCOPE — user-visible, functionally safe edits only. We do NOT rename
# internal package names, config-dir paths, server API routes, or
# env-var prefixes (a blind global rename would break the build and the
# agent). The MIT copyright notice is always preserved (the build script
# ships OpenCode's LICENSE as PRAIMATE-CODE-LICENSE + a NOTICE).
#
# Applied here:
#   1. Disable the "update available" advisory — our managed build is
#      version-pinned and must not nag users to upgrade to upstream.
#   2. Replace the home-screen "opencode" wordmark with a PrAImate title.
#
# Every patch checks its anchor first and warns (not fails) if upstream
# moved it, so a version bump degrades gracefully rather than breaking
# the build.
set -euo pipefail
SRC="${1:?usage: praimate-code-rebrand.sh <opencode-checkout-dir>}"
cd "$SRC"

echo "  - rebrand: applying PrAImate Code patches"

APP_TSX="packages/tui/src/app.tsx"
HOME_TSX="packages/tui/src/routes/home.tsx"

# 1. Short-circuit the update-available handler. `if (true) return`
#    avoids unreachable-code type errors while disabling the dialog.
#    The returned string literal is deliberate: comments are stripped by
#    bun's bundler, but live code survives — so the built binary carries
#    a greppable "praimate:managed-build" marker that
#    update-praimate-code.sh uses to verify the patch really applied.
if [ -f "$APP_TSX" ] && grep -q 'installation.update-available", async (evt) => {' "$APP_TSX"; then
  perl -0pi -e 's/(event\.on\("installation\.update-available", async \(evt\) => \{)/$1\n    if (true) return "praimate:managed-build"; \/\/ upstream update advisory disabled/' "$APP_TSX"
  echo "    · disabled update advisory"
else
  echo "    · WARN: update-advisory anchor not found (upstream moved?); skipped" >&2
fi

# 2. Replace the home-screen block logo with PrAImate Code ASCII
#    lettering art. A TUI can't render the monke PNG icon, so we keep
#    the OpenCode-style block-art look but spell "praimate-code". The
#    art is a figlet "small" rendering, JSON-encoded so its backslashes
#    and backticks survive injection into the TSX as a plain JS string
#    literal. opentui's <text> renders the embedded \n as line breaks,
#    reproducing the multi-line banner the original <Logo /> drew.
PRAIMATE_CODE_ART_JSON="$(cat <<'PCART'
"               _            _                      _     \n _ __ _ _ __ _(_)_ __  __ _| |_ ___ ___ __ ___  __| |___ \n| '_ \\ '_/ _` | | '  \\/ _` |  _/ -_)___/ _/ _ \\/ _` / -_)\n| .__/_| \\__,_|_|_|_|_\\__,_|\\__\\___|   \\__\\___/\\__,_\\___|\n|_|                                                      "
PCART
)"
if [ -f "$HOME_TSX" ] && grep -q '<Logo />' "$HOME_TSX"; then
  REPLACEMENT="<text selectable={false}>{${PRAIMATE_CODE_ART_JSON}}</text>" \
    perl -0pi -e 's/<Logo \/>/$ENV{REPLACEMENT}/' "$HOME_TSX"
  echo "    · replaced home wordmark with praimate-code ASCII banner"
else
  echo "    · WARN: home <Logo /> anchor not found; skipped" >&2
fi

exit 0
