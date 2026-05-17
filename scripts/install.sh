#!/usr/bin/env bash
# install.sh — copy clade + wpc to a directory on $PATH.
#
# Run this either inside an extracted release archive (where ./clade
# and ./wpc sit next to this script) or from a repo checkout after
# `scripts/build.sh`. The script auto-detects which it is.
#
# Usage:
#   ./install.sh                  # /usr/local/bin if writable or sudo-able,
#                                 # otherwise ~/.local/bin
#   ./install.sh --user           # force ~/.local/bin (no sudo)
#   ./install.sh --system         # force /usr/local/bin (use sudo if needed)
#   PREFIX=/opt/clade/bin ./install.sh   # custom dir
#
# After install, if the target dir isn't on $PATH the script prints
# the one line you need to add to your shell rc — it does NOT edit
# rc files behind your back.

set -euo pipefail

# ---------- argument parsing ----------
MODE="auto"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --user)   MODE="user"   ; shift ;;
    --system) MODE="system" ; shift ;;
    --help|-h)
      sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) printf 'unknown arg: %s (try --help)\n' "$1" >&2; exit 2 ;;
  esac
done

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------- locate binaries ----------
# Candidate roots, in order of preference:
#   1. caller's cwd (just in case someone did `./scripts/install.sh` from .)
#   2. the script's own dir (release archive layout: binaries next to install.sh)
#   3. dist/<os>-<arch>/ (repo layout after scripts/build.sh)
detect_triplet() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)  os=linux ;;
    darwin) os=darwin ;;
    *)      printf 'unsupported OS: %s\n' "$os" >&2; return 1 ;;
  esac
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) printf 'unsupported arch: %s\n' "$arch" >&2; return 1 ;;
  esac
  printf '%s-%s' "$os" "$arch"
}

find_bins() {
  local cand
  for cand in "$PWD" "$HERE" "$HERE/.." "$HERE/../dist/$(detect_triplet 2>/dev/null)"; do
    [[ -n "$cand" ]] || continue
    if [[ -x "$cand/clade" && -x "$cand/wpc" ]]; then
      printf '%s' "$cand"
      return 0
    fi
  done
  return 1
}

SRC=""
SRC="$(find_bins || true)"
if [[ -z "$SRC" ]]; then
  cat >&2 <<EOF
✗ couldn't find clade + wpc binaries.

Either:
  - cd into the extracted release archive (where ./clade and ./wpc are), then re-run this; or
  - from the repo root, run scripts/build.sh first to produce dist/<os>-<arch>/.
EOF
  exit 1
fi
printf '✓ found binaries in %s\n' "$SRC"

# ---------- pick destination ----------
choose_dest() {
  case "$MODE" in
    user)   printf '%s' "${PREFIX:-$HOME/.local/bin}" ;;
    system) printf '%s' "${PREFIX:-/usr/local/bin}"   ;;
    auto)
      if [[ -n "${PREFIX:-}" ]]; then
        printf '%s' "$PREFIX"
      elif [[ -w /usr/local/bin ]] || command -v sudo >/dev/null 2>&1; then
        printf '%s' "/usr/local/bin"
      else
        printf '%s' "$HOME/.local/bin"
      fi
      ;;
  esac
}

DEST="$(choose_dest)"
mkdir -p "$DEST" 2>/dev/null || true

# Decide whether to wrap copy/chmod in sudo. We only do so if the
# directory exists and isn't writable as us — never when installing
# under $HOME, even if the caller passed --system but redirected
# PREFIX into their own dir.
SUDO=""
if [[ ! -w "$DEST" ]]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
    printf '→ %s not writable; will use sudo\n' "$DEST"
  else
    printf '✗ %s not writable and no sudo available. Re-run with --user.\n' "$DEST" >&2
    exit 1
  fi
fi

# ---------- install ----------
install_one() {
  local bin="$1"
  $SUDO install -m 0755 "$SRC/$bin" "$DEST/$bin"
  printf '✓ %s installed at %s/%s\n' "$bin" "$DEST" "$bin"
}

install_one clade
install_one wpc

# ---------- PATH sanity check ----------
case ":$PATH:" in
  *":$DEST:"*) ;;
  *)
    rc="$HOME/.bashrc"
    [[ -n "${ZSH_VERSION:-}" ]] && rc="$HOME/.zshrc"
    cat <<EOF

⚠  $DEST is NOT on your PATH.

   Add this line to your $rc (or the rc of whichever shell you use):

       export PATH="\$PATH:$DEST"

   …then open a new terminal, or source the file:
       source $rc
EOF
    ;;
esac

printf '\nTry it:    clade -version\n'
