#!/usr/bin/env bash
# Summarize a binary — file(1) type, size, sha256, and section count
set -euo pipefail

target="${1:-}"
if [[ -z "$target" ]]; then
  echo "usage: file_summary <path>" >&2
  exit 2
fi
if [[ ! -e "$target" ]]; then
  echo "not found: $target" >&2
  exit 1
fi

echo "== path ==";   echo "$target"
echo; echo "== size ==";   wc -c <"$target"
echo; echo "== sha256 =="; sha256sum "$target" | awk '{print $1}'
echo; echo "== file(1) =="; file -b "$target"
