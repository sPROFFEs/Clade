#!/usr/bin/env bash
# install_chrome.sh — install Google Chrome stable on Debian/Ubuntu so
# the Nano bridge has a usable browser. Idempotent: safe to re-run.
#
# Why this script exists: Playwright bundles upstream Chromium, which
# does NOT ship Google's on-device-model service (the bit that
# downloads Gemini Nano). Vanilla Chromium will never expose
# window.LanguageModel. The bridge therefore needs real Google
# Chrome.
#
# Usage:
#   sudo ./install_chrome.sh

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "needs root — re-run with:  sudo $0" >&2
  exit 1
fi

# Already installed?
if command -v google-chrome >/dev/null 2>&1; then
  loc="$(command -v google-chrome)"
  ver="$("$loc" --version 2>/dev/null || true)"
  echo "✓ google-chrome already installed: $loc"
  echo "  $ver"
  exit 0
fi

# Sanity-check we're on a Debian-derived system; apt-get is what we
# drive below. RHEL/Fedora paths are very different — give up early
# with a useful pointer instead of failing in dpkg.
if ! command -v apt-get >/dev/null 2>&1; then
  cat >&2 <<EOF
this script only knows Debian/Ubuntu (apt-get).
on Fedora/RHEL, follow: https://www.google.com/intl/en/chrome/?platform=linux
then re-run /nano_setup from Telegram.
EOF
  exit 2
fi

# Ensure we have the bits we need to set up the apt source.
echo "→ apt-get update + prereqs (wget, gnupg)"
apt-get update -qq
apt-get install -y -qq wget gnupg ca-certificates >/dev/null

KEYRING=/usr/share/keyrings/google-chrome.gpg
LIST=/etc/apt/sources.list.d/google-chrome.list

if [[ ! -f "$KEYRING" ]]; then
  echo "→ adding Google signing key to ${KEYRING}"
  # dearmor here so apt-get's signed-by= works without legacy apt-key.
  wget -qO- https://dl.google.com/linux/linux_signing_key.pub \
    | gpg --dearmor -o "$KEYRING"
fi

if [[ ! -f "$LIST" ]]; then
  echo "→ writing apt source to ${LIST}"
  echo "deb [arch=amd64 signed-by=${KEYRING}] http://dl.google.com/linux/chrome/deb/ stable main" \
    > "$LIST"
fi

echo "→ apt-get update (Google source) + install google-chrome-stable"
apt-get update -qq
apt-get install -y google-chrome-stable

# Confirm the binary the bridge will auto-detect.
loc="$(command -v google-chrome || true)"
if [[ -z "$loc" ]]; then
  echo "✗ install reported success but google-chrome is not on PATH." >&2
  exit 3
fi
echo
echo "✓ installed: $loc"
"$loc" --version
echo
echo "next steps (from Telegram):"
echo "  /nano_check       # should now say 'real Google Chrome — Nano supported'"
echo "  /nano_setup       # downloads Gemini Nano (~1.5 GB, first time only)"
echo "  /nano_start       # spawn the bridge"
