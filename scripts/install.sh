#!/usr/bin/env bash
# PrAImate installer for Linux + macOS.
#
# One-liner:
#   curl -fsSL https://raw.githubusercontent.com/sPROFFEs/PrAImate/main/scripts/install.sh | bash
#
# Or with options (after `-s --` they go to the script, not bash):
#   curl -fsSL https://… | bash -s -- --source      # build from source
#   curl -fsSL https://… | bash -s -- --user        # ~/.local/bin
#   curl -fsSL https://… | bash -s -- --system      # /usr/local/bin
#   curl -fsSL https://… | bash -s -- --yes         # auto-yes all prompts
#
# Or run locally (from inside an extracted release archive or a repo checkout):
#   ./scripts/install.sh
#
# Flags:
#   --binary           grab the prebuilt release tarball (default in one-liner mode)
#   --source           git clone + go build (installs Go if missing, asking first)
#   --user             install to ~/.local/bin (no sudo)
#   --system           install to /usr/local/bin (sudo when needed)
#   --prefix <dir>     custom install dir
#   --yes              auto-confirm prompts (CI / scripted use)
#   -h, --help         show this
#
# The binary path resolves the latest GitHub release via the API.
# Set RELEASE_TAG=<tag> in the environment to pin a specific release.

set -euo pipefail

REPO="sPROFFEs/PrAImate"
RAW_REPO_URL="https://github.com/sPROFFEs/PrAImate"
SOURCE_BRANCH="main"
# Release tag to pull assets from. When unset we resolve "latest" via
# the GitHub API at download time so the installer keeps working as
# the operator publishes new versioned tags (0.1.7, 0.1.8, ...).
# Override with RELEASE_TAG=<tag> in the environment to pin a release.
RELEASE_TAG="${RELEASE_TAG:-}"

MODE=""          # binary | source (empty = ask, default binary in non-tty)
PREFIX_MODE=""   # user | system (empty = auto)
PREFIX=""        # explicit
YES=0

# ---------- arg parsing ----------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)    MODE="binary"  ; shift ;;
    --source)    MODE="source"  ; shift ;;
    --user)      PREFIX_MODE="user" ; shift ;;
    --system)    PREFIX_MODE="system" ; shift ;;
    --prefix)    PREFIX="$2"   ; shift 2 ;;
    --prefix=*)  PREFIX="${1#--prefix=}" ; shift ;;
    --yes|-y)    YES=1 ; shift ;;
    -h|--help)
      sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) printf 'unknown arg: %s (try --help)\n' "$1" >&2; exit 2 ;;
  esac
done

# ---------- tty helpers ----------
# When piped via `curl | bash`, stdin is the pipe. To prompt the user
# we have to read from /dev/tty. Detect once and route every prompt
# through here so the same code handles both interactive runs and
# one-liners.
HAVE_TTY=0
if [[ -e /dev/tty ]]; then
  HAVE_TTY=1
fi

ask() {
  local prompt="$1" default="$2" reply=""
  if [[ "$YES" == "1" ]]; then
    printf '%s\n' "$default"
    return
  fi
  if [[ "$HAVE_TTY" == "0" ]]; then
    printf '%s\n' "$default"
    return
  fi
  printf '%s [%s]: ' "$prompt" "$default" >/dev/tty
  read -r reply </dev/tty || reply=""
  if [[ -z "${reply// }" ]]; then
    printf '%s' "$default"
  else
    printf '%s' "$reply"
  fi
}

yesno() {
  local prompt="$1" default="${2:-n}" reply
  reply="$(ask "$prompt $( [[ "$default" == y ]] && printf '[Y/n]' || printf '[y/N]' )" "$default")"
  [[ "${reply,,}" =~ ^y(es)?$ ]]
}

# ---------- pretty ----------
c_grn() { printf '\033[0;32m%s\033[0m\n' "$*"; }
c_red() { printf '\033[0;31m%s\033[0m\n' "$*" >&2; }
c_yel() { printf '\033[0;33m%s\033[0m\n' "$*"; }
c_dim() { printf '\033[0;90m%s\033[0m\n' "$*"; }

step() { printf '\n'; c_grn "==> $*"; }

# ---------- platform detect ----------
detect_triplet() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)  os=linux ;;
    darwin) os=darwin ;;
    *) c_red "unsupported OS: $os"; exit 1 ;;
  esac
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) c_red "unsupported arch: $arch"; exit 1 ;;
  esac
  printf '%s-%s' "$os" "$arch"
}

TRIPLET="$(detect_triplet)"

# ---------- locate local binaries (release-archive / repo case) ----------
HERE="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd 2>/dev/null || printf '')"

find_local_bins() {
  local cand
  for cand in "$PWD" "$HERE" "$HERE/.." "$HERE/../dist/$TRIPLET"; do
    [[ -n "$cand" ]] || continue
    if [[ -x "$cand/praimate" && -x "$cand/wpc" ]]; then
      printf '%s' "$cand"
      return 0
    fi
  done
  return 1
}

LOCAL_BINS="$(find_local_bins || true)"

# ---------- mode prompt ----------
if [[ -z "$MODE" ]]; then
  if [[ -n "$LOCAL_BINS" ]]; then
    # Inside an archive / repo — just install what's here.
    MODE="local"
    c_dim "(found local binaries in $LOCAL_BINS — skipping download/build prompt)"
  else
    cat <<EOF
How do you want to install PrAImate?

  1. Download a prebuilt release  ${YES:+(default in --yes mode)}
  2. Build from source (needs Go; will offer to install Go if missing)
  3. Cancel
EOF
    choice="$(ask 'Choose 1, 2, or 3' '1')"
    case "$choice" in
      1) MODE="binary" ;;
      2) MODE="source" ;;
      3) c_yel "cancelled."; exit 0 ;;
      *) c_red "invalid choice: $choice"; exit 1 ;;
    esac
  fi
fi

# ---------- destination ----------
choose_dest() {
  if [[ -n "$PREFIX" ]]; then
    printf '%s' "$PREFIX"
    return
  fi
  case "$PREFIX_MODE" in
    user)   printf '%s' "$HOME/.local/bin" ;;
    system) printf '%s' "/usr/local/bin"   ;;
    "")
      # auto: prefer system if writable or sudo available + user agrees,
      # otherwise drop into ~/.local/bin (no sudo needed).
      if [[ -w /usr/local/bin ]]; then
        printf '%s' "/usr/local/bin"
      elif command -v sudo >/dev/null 2>&1 && [[ "$HAVE_TTY" == "1" ]] && ! [[ "$YES" == "1" ]]; then
        if yesno "Install to /usr/local/bin (uses sudo)? Otherwise will use ~/.local/bin." y; then
          printf '%s' "/usr/local/bin"
        else
          printf '%s' "$HOME/.local/bin"
        fi
      else
        printf '%s' "$HOME/.local/bin"
      fi
      ;;
  esac
}

DEST="$(choose_dest)"
mkdir -p "$DEST" 2>/dev/null || true

SUDO=""
if [[ ! -w "$DEST" ]]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    c_red "$DEST is not writable and sudo isn't available."
    c_red "Re-run with --user or --prefix=<writable dir>."
    exit 1
  fi
fi

# ---------- binary path: GitHub release ----------
detect_downloader() {
  if command -v curl >/dev/null 2>&1; then printf 'curl'
  elif command -v wget >/dev/null 2>&1; then printf 'wget'
  else c_red "need curl or wget"; exit 1; fi
}

fetch() {
  # fetch <url> <out>
  local url="$1" out="$2"
  local dl
  dl="$(detect_downloader)"
  if [[ "$dl" == "curl" ]]; then
    curl -fsSL --retry 3 -o "$out" "$url"
  else
    wget -q -O "$out" "$url"
  fi
}

resolve_latest_tag() {
  local api="https://api.github.com/repos/$REPO/releases/latest"
  local dl tag
  dl="$(detect_downloader)"
  # The pattern is intentionally NOT anchored to line-start: GitHub
  # returns the JSON minified (everything on one line), so an `^…` anchor
  # would only match a pretty-printed response and silently fail in
  # production. `.*` on both sides lets sed find "tag_name" anywhere on
  # the line; the non-greedy [^"]* keeps the capture tight.
  if [[ "$dl" == "curl" ]]; then
    tag="$(curl -fsSL -H 'User-Agent: praimate-installer' "$api" 2>/dev/null \
      | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  else
    tag="$(wget -q -O - --header='User-Agent: praimate-installer' "$api" 2>/dev/null \
      | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  fi
  if [[ -z "$tag" ]]; then
    c_red "couldn't query GitHub for the latest release."
    c_red "Set RELEASE_TAG=<tag> in the environment to pin a version and re-run."
    exit 1
  fi
  printf '%s' "$tag"
}

install_from_release() {
  step "Resolving release"
  if [[ -z "$RELEASE_TAG" ]]; then
    RELEASE_TAG="$(resolve_latest_tag)"
  fi
  local fname="praimate-${TRIPLET}.tar.gz"
  local url="https://github.com/$REPO/releases/download/${RELEASE_TAG}/${fname}"
  c_dim "  tag:     $RELEASE_TAG"
  c_dim "  asset:   $fname"
  c_dim "  url:     $url"

  step "Downloading"
  local tmp
  tmp="$(mktemp -d)"
  # Double-quote so $tmp is expanded NOW, while it's still in scope.
  # Single quotes would defer expansion to EXIT-time, by which point
  # `local tmp` is gone and `set -u` would error with "tmp: unbound".
  trap "rm -rf '$tmp'" EXIT
  fetch "$url" "$tmp/$fname" || { c_red "download failed."; exit 1; }
  c_grn "  downloaded $(du -h "$tmp/$fname" | cut -f1)"

  step "Extracting"
  tar -xzf "$tmp/$fname" -C "$tmp"
  local extracted="$tmp/$TRIPLET"
  [[ -d "$extracted" ]] || { c_red "unexpected archive layout under $tmp"; exit 1; }

  step "Installing to $DEST"
  $SUDO install -m 0755 "$extracted/praimate" "$DEST/praimate"
  $SUDO install -m 0755 "$extracted/wpc"   "$DEST/wpc"
  # Legacy archives (≤1.1.3) shipped a praimate-gui binary inside the
  # bundle. The desktop GUI is now the standalone PrAImate GUI Electron
  # app — install it from the .deb/.AppImage/.dmg/.exe on the GitHub
  # release. Keep the copy for old archives so re-installs still work.
  if [[ -f "$extracted/praimate-gui" ]]; then
    $SUDO install -m 0755 "$extracted/praimate-gui" "$DEST/praimate-gui"
    c_grn "  praimate-gui installed (launch with: praimate --gui)"
  fi
  # PrAImate Code (bundled coding CLI). Large (~150MB); only in archives
  # built with --with-code. Installed next to praimate so `praimate code`
  # finds it.
  if [[ -f "$extracted/praimate-code" ]]; then
    $SUDO install -m 0755 "$extracted/praimate-code" "$DEST/praimate-code"
    [[ -f "$extracted/PRAIMATE-CODE-LICENSE" ]] && $SUDO cp "$extracted/PRAIMATE-CODE-LICENSE" "$DEST/" 2>/dev/null || true
    c_grn "  praimate-code installed (launch with: praimate code)"
  fi
  # Ship the bundled samples next to the binary at the XDG-style path
  # the launcher already probes (see internal/launcher SampleCandidates:
  # "<execDir>/../share/praimate/samples/workpaths"). Without this, the
  # first-run "seed example templates" step finds nothing and the user
  # ends up with no workspaces and no praimate-workspaces dir created.
  if [[ -d "$extracted/samples" ]]; then
    local samples_dest
    samples_dest="$(dirname "$DEST")/share/praimate/samples"
    $SUDO mkdir -p "$samples_dest"
    # cp -R preserves the workpaths/ subdir structure the launcher expects.
    # Use --no-preserve=ownership when running under sudo so the files end
    # up readable by everyone, not just the invoking user.
    $SUDO cp -R "$extracted/samples/." "$samples_dest/"
    c_dim "  samples → $samples_dest"
  fi
  c_grn "  ✓ praimate + wpc installed"
}

# ---------- source path: clone + go build ----------
have_go() { command -v go >/dev/null 2>&1; }

detect_pkg_manager() {
  # Print the apt/dnf/pacman/zypper/apk/brew install command for Go,
  # or empty if we don't know the system.
  if [[ "$(uname -s)" == "Darwin" ]]; then
    if command -v brew >/dev/null 2>&1; then
      printf '%s' "brew install go"
    fi
    return
  fi
  if command -v apt-get >/dev/null 2>&1; then
    printf '%s' "sudo apt-get update && sudo apt-get install -y golang-go"
  elif command -v dnf >/dev/null 2>&1; then
    printf '%s' "sudo dnf install -y golang"
  elif command -v pacman >/dev/null 2>&1; then
    printf '%s' "sudo pacman -S --noconfirm go"
  elif command -v zypper >/dev/null 2>&1; then
    printf '%s' "sudo zypper install -y go"
  elif command -v apk >/dev/null 2>&1; then
    printf '%s' "sudo apk add go"
  fi
}

install_go() {
  local cmd
  cmd="$(detect_pkg_manager)"
  if [[ -z "$cmd" ]]; then
    c_red "Go isn't installed and we can't find a known package manager."
    c_red "Install Go manually from https://go.dev/dl/ and re-run this script."
    return 1
  fi
  c_yel "Go isn't installed. The script can install it with:"
  printf '\n    %s\n\n' "$cmd"
  if ! yesno "Run that command now?" y; then
    c_red "Cancelled. Install Go yourself and re-run."
    return 1
  fi
  bash -c "$cmd"
  have_go || { c_red "Go install reported success but 'go' still isn't on PATH."; return 1; }
}

install_from_source() {
  if ! have_go; then
    step "Go not found"
    install_go || exit 1
  fi
  step "Cloning repo"
  local tmp
  tmp="$(mktemp -d)"
  # See install_from_release for why this uses double quotes.
  trap "rm -rf '$tmp'" EXIT
  git clone --depth 1 --branch "$SOURCE_BRANCH" \
    "${RAW_REPO_URL}.git" "$tmp/PrAImate" \
    || { c_red "git clone failed"; exit 1; }

  step "Building (this can take ~30s on first run while Go fetches deps)"
  (
    cd "$tmp/PrAImate"
    GOOS="$(uname -s | tr '[:upper:]' '[:lower:]')" \
    GOARCH="$(case $(uname -m) in x86_64|amd64) printf amd64;; aarch64|arm64) printf arm64;; esac)" \
    CGO_ENABLED=0 \
    go build -trimpath -ldflags '-s -w' -o ./praimate ./cmd/praimate
    GOOS="$(uname -s | tr '[:upper:]' '[:lower:]')" \
    GOARCH="$(case $(uname -m) in x86_64|amd64) printf amd64;; aarch64|arm64) printf arm64;; esac)" \
    CGO_ENABLED=0 \
    go build -trimpath -ldflags '-s -w' -o ./wpc   ./cmd/wpc
  )

  step "Installing to $DEST"
  $SUDO install -m 0755 "$tmp/PrAImate/praimate" "$DEST/praimate"
  $SUDO install -m 0755 "$tmp/PrAImate/wpc"   "$DEST/wpc"
  c_grn "  ✓ praimate + wpc installed"
}

# ---------- local path: bins already next to us ----------
install_local() {
  step "Installing to $DEST"
  $SUDO install -m 0755 "$LOCAL_BINS/praimate" "$DEST/praimate"
  $SUDO install -m 0755 "$LOCAL_BINS/wpc"   "$DEST/wpc"
  if [[ -f "$LOCAL_BINS/praimate-gui" ]]; then
    $SUDO install -m 0755 "$LOCAL_BINS/praimate-gui" "$DEST/praimate-gui"
    c_grn "  ✓ praimate + wpc + praimate-gui installed"
  else
    c_grn "  ✓ praimate + wpc installed"
  fi
}

# ---------- dispatch ----------
case "$MODE" in
  binary) install_from_release ;;
  source) install_from_source ;;
  local)  install_local ;;
  *) c_red "internal: unknown MODE $MODE"; exit 1 ;;
esac

# ---------- PATH sanity check ----------
case ":$PATH:" in
  *":$DEST:"*) ;;
  *)
    rc="$HOME/.bashrc"
    [[ -n "${ZSH_VERSION:-}" || "${SHELL:-}" == */zsh ]] && rc="$HOME/.zshrc"
    cat <<EOF

⚠  $DEST is NOT on your PATH yet.

   Add this line to your $rc (or the rc of the shell you actually use):

       export PATH="\$PATH:$DEST"

   Then open a new terminal, or run:
       source $rc
EOF
    ;;
esac

step "Done"
printf 'Try it:    %s -version\n' "$DEST/praimate"
printf '(after PATH update, just `praimate -version` from any new shell)\n'
