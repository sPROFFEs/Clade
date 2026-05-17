#!/usr/bin/env bash
# Extract printable strings from a binary, optionally filtered by a regex
set -euo pipefail

target="${1:-}"
pattern="${2:-}"
min="${3:-6}"

if [[ -z "$target" ]]; then
  echo "usage: extract_strings <path> [pattern] [min-length]" >&2
  exit 2
fi

if [[ -n "$pattern" ]]; then
  strings -n "$min" "$target" | grep -E "$pattern" | head -n 200
else
  strings -n "$min" "$target" | head -n 200
fi
