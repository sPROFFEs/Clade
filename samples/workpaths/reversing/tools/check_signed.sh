#!/usr/bin/env bash
# Report embedded signature/cert info of a PE driver via osslsigncode if available
set -euo pipefail

target="${1:-}"
if [[ -z "$target" ]]; then
  echo "usage: check_signed <path>" >&2
  exit 2
fi
if [[ ! -e "$target" ]]; then
  echo "not found: $target" >&2
  exit 1
fi

if command -v osslsigncode >/dev/null 2>&1; then
  osslsigncode verify "$target" || true
elif command -v pesign >/dev/null 2>&1; then
  pesign --show-signature --in="$target" || true
else
  echo "no PE signature tool found (install osslsigncode or pesign)" >&2
fi
