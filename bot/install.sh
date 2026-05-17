#!/usr/bin/env bash
# install.sh — first-time setup + in-place updater for the telemetry
# bot as a systemd service. Idempotent: re-run any time to change env
# vars, switch the venv, or pick up a new repo location.
#
# What it does:
#   1. Detects an existing service (default name: vm-telemetry). If
#      found, reads its EnvironmentFile so prompts pre-fill with the
#      current values — you can just press Enter to keep each one.
#   2. Asks for TELEGRAM_TOKEN / TELEGRAM_CHAT_ID and the rest, with
#      sensible defaults.
#   3. Creates / refreshes a venv next to the bot and installs
#      requirements.txt (+ bridge deps if you want Gemini Nano).
#   4. Writes <bot>/.env (chmod 600) and a systemd unit pointing at it.
#   5. daemon-reload + enable --now + status. Tails the last log lines.
#
# Usage:
#   sudo ./install.sh                   # interactive, default name
#   sudo SERVICE=my-bot ./install.sh    # override the unit name
#   sudo BRIDGE=1 ./install.sh          # also install Nano bridge deps
#   sudo NONINTERACTIVE=1 ./install.sh  # use current/default values, no prompts
#
# Re-run after changing this script or .env to push the changes live.

set -euo pipefail

# ---------- config knobs ----------
SERVICE="${SERVICE:-vm-telemetry}"
UNIT_PATH="/etc/systemd/system/${SERVICE}.service"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${HERE}/.env"
VENV_DIR="${HERE}/.venv"
NONINTERACTIVE="${NONINTERACTIVE:-0}"
BRIDGE="${BRIDGE:-auto}"   # auto = ask; 1 = always; 0 = never

# Run-as user defaults to the invoking user (whoever ran sudo).
RUN_USER="${SUDO_USER:-${USER}}"
RUN_GROUP="$(id -gn "${RUN_USER}")"

# ---------- helpers ----------
c_red() { printf '\033[0;31m%s\033[0m\n' "$*"; }
c_grn() { printf '\033[0;32m%s\033[0m\n' "$*"; }
c_yel() { printf '\033[0;33m%s\033[0m\n' "$*"; }
c_dim() { printf '\033[0;90m%s\033[0m\n' "$*"; }

need_root() {
  if [[ $EUID -ne 0 ]]; then
    c_red "this installer writes to ${UNIT_PATH} and needs root."
    c_dim "re-run with:  sudo ./install.sh"
    exit 1
  fi
}

ask() {
  # ask "label" "default" [secret]
  # Echoes the answer on stdout. Honours NONINTERACTIVE (uses default).
  local label="$1" default="${2:-}" secret="${3:-}" reply=""
  if [[ "$NONINTERACTIVE" == "1" ]]; then
    printf '%s\n' "$default"
    return
  fi
  local shown="$default"
  if [[ -n "$secret" && -n "$default" ]]; then
    # Mask all but the last 4 chars of a secret so the user can verify
    # which token is in place without leaking it in shell scrollback.
    local tail="${default: -4}"
    shown="********${tail}"
  fi
  if [[ -n "$default" ]]; then
    read -r -p "  ${label} [${shown}]: " reply </dev/tty || true
  else
    read -r -p "  ${label}: " reply </dev/tty || true
  fi
  printf '%s\n' "${reply:-$default}"
}

yesno() {
  # yesno "prompt" "default y|n"
  local prompt="$1" default="${2:-n}" reply=""
  if [[ "$NONINTERACTIVE" == "1" ]]; then
    [[ "$default" == "y" ]]
    return
  fi
  local hint="[y/N]"; [[ "$default" == "y" ]] && hint="[Y/n]"
  read -r -p "  ${prompt} ${hint} " reply </dev/tty || true
  reply="${reply:-$default}"
  [[ "${reply,,}" =~ ^y(es)?$ ]]
}

# Parse KEY from an env file, stripping quotes. Empty string if missing.
read_env() {
  local key="$1" file="$2"
  [[ -f "$file" ]] || { echo ""; return; }
  # Last assignment wins (matches shell behaviour when sourced).
  local line
  line=$(grep -E "^[[:space:]]*${key}=" "$file" 2>/dev/null | tail -n1 || true)
  [[ -n "$line" ]] || { echo ""; return; }
  local val="${line#*=}"
  # Strip surrounding single or double quotes.
  val="${val%\"}"; val="${val#\"}"
  val="${val%\'}"; val="${val#\'}"
  printf '%s\n' "$val"
}

# Look up the EnvironmentFile= line in an existing unit, if any. We
# prefer this over our default path so we keep using whatever the
# running service already points at.
existing_envfile() {
  local unit="$1"
  if systemctl cat "$unit" >/dev/null 2>&1; then
    systemctl cat "$unit" \
      | awk -F= '/^EnvironmentFile=/{ sub("^-","",$2); print $2; exit }'
  fi
}

# ---------- preflight ----------
need_root

if [[ ! -f "${HERE}/telemetry_bot.py" ]]; then
  c_red "telemetry_bot.py not next to install.sh (looked in ${HERE})."
  exit 1
fi

PYTHON_BIN="${PYTHON_BIN:-$(command -v python3 || true)}"
if [[ -z "$PYTHON_BIN" ]]; then
  c_red "python3 not on PATH. apt install python3 python3-venv python3-pip first."
  exit 1
fi

c_grn "==> telemetry bot installer"
c_dim "    repo dir:   ${HERE}"
c_dim "    service:    ${SERVICE} (unit: ${UNIT_PATH})"
c_dim "    run as:     ${RUN_USER}:${RUN_GROUP}"
c_dim "    python:     ${PYTHON_BIN}"
echo

# ---------- detect existing service ----------
EXISTING_ENVFILE=""
if systemctl list-unit-files --no-legend 2>/dev/null | awk '{print $1}' | grep -qx "${SERVICE}.service"; then
  EXISTING_ENVFILE="$(existing_envfile "${SERVICE}.service" || true)"
  c_yel "==> existing service detected"
  systemctl --no-pager --lines=0 status "${SERVICE}.service" || true
  echo
  if [[ -n "$EXISTING_ENVFILE" ]]; then
    c_dim "    using EnvironmentFile from current unit: ${EXISTING_ENVFILE}"
    ENV_FILE="$EXISTING_ENVFILE"
  fi
else
  c_dim "==> no existing ${SERVICE}.service — will create one"
fi
echo

# ---------- gather env values ----------
c_grn "==> bot configuration"
c_dim "    press Enter to keep the [default] shown for each value."
echo

DEF_TOKEN=$(read_env TELEGRAM_TOKEN "$ENV_FILE")
DEF_CHAT=$(read_env TELEGRAM_CHAT_ID "$ENV_FILE")
DEF_OLL=$(read_env OLLAMA_URL "$ENV_FILE");           DEF_OLL="${DEF_OLL:-http://127.0.0.1:11434}"
DEF_KA=$(read_env OLLAMA_KEEP_ALIVE "$ENV_FILE");     DEF_KA="${DEF_KA:-1h}"
DEF_PORT=$(read_env NANO_BRIDGE_PORT "$ENV_FILE");    DEF_PORT="${DEF_PORT:-8765}"
DEF_PROFILE=$(read_env NANO_CHROME_PROFILE "$ENV_FILE")
DEF_PROFILE="${DEF_PROFILE:-/home/${RUN_USER}/.config/code-launcher-nano}"
DEF_CHROME=$(read_env NANO_CHROME_EXECUTABLE "$ENV_FILE")
DEF_LOG=$(read_env NANO_LOG_FILE "$ENV_FILE");        DEF_LOG="${DEF_LOG:-${HERE}/nano_bridge.log}"
DEF_TEMP=$(read_env TEMP_CRIT "$ENV_FILE");           DEF_TEMP="${DEF_TEMP:-85}"
DEF_VRAM=$(read_env VRAM_CRIT "$ENV_FILE");           DEF_VRAM="${DEF_VRAM:-95}"
DEF_DISK=$(read_env DISK_CRIT "$ENV_FILE");           DEF_DISK="${DEF_DISK:-90}"

TOKEN=$(ask    "TELEGRAM_TOKEN (from @BotFather)"   "$DEF_TOKEN" secret)
CHAT=$(ask     "TELEGRAM_CHAT_ID (numeric)"         "$DEF_CHAT")
OLL=$(ask      "OLLAMA_URL"                         "$DEF_OLL")
KA=$(ask       "OLLAMA_KEEP_ALIVE (default TTL)"    "$DEF_KA")
PORT=$(ask     "NANO_BRIDGE_PORT"                   "$DEF_PORT")
PROFILE=$(ask  "NANO_CHROME_PROFILE"                "$DEF_PROFILE")
CHROME=$(ask   "NANO_CHROME_EXECUTABLE (blank=auto)" "$DEF_CHROME")
LOGF=$(ask     "NANO_LOG_FILE"                      "$DEF_LOG")
TEMP=$(ask     "TEMP_CRIT (°C alert)"               "$DEF_TEMP")
VRAM=$(ask     "VRAM_CRIT (%% alert)"                "$DEF_VRAM")
DISK=$(ask     "DISK_CRIT (%% alert)"                "$DEF_DISK")

if [[ -z "$TOKEN" || -z "$CHAT" ]]; then
  c_red "TELEGRAM_TOKEN and TELEGRAM_CHAT_ID are required. aborting."
  exit 1
fi

# ---------- venv + deps ----------
echo
c_grn "==> python venv + dependencies"
if [[ ! -d "$VENV_DIR" ]]; then
  c_dim "    creating ${VENV_DIR}"
  sudo -u "$RUN_USER" "$PYTHON_BIN" -m venv "$VENV_DIR"
else
  c_dim "    reusing existing venv at ${VENV_DIR}"
fi

PIP="${VENV_DIR}/bin/pip"
sudo -u "$RUN_USER" "$PIP" install --upgrade pip >/dev/null
sudo -u "$RUN_USER" "$PIP" install -r "${HERE}/requirements.txt"

INSTALL_BRIDGE=0
case "$BRIDGE" in
  1) INSTALL_BRIDGE=1 ;;
  0) INSTALL_BRIDGE=0 ;;
  *)
    if yesno "also install Gemini Nano bridge deps (playwright + chromium, ~150MB)?" n; then
      INSTALL_BRIDGE=1
    fi
    ;;
esac

if [[ "$INSTALL_BRIDGE" == "1" ]]; then
  sudo -u "$RUN_USER" "$PIP" install -r "${HERE}/requirements-bridge.txt"
  c_dim "    running 'playwright install chromium' (skip on re-run if already present)..."
  sudo -u "$RUN_USER" "${VENV_DIR}/bin/playwright" install chromium || \
    c_yel "    (playwright install returned non-zero; check output above)"
fi

# ---------- write .env ----------
echo
c_grn "==> writing ${ENV_FILE}"
# Use a tmpfile so a crash mid-write can't leave a partial .env that
# the next service start would read.
TMP_ENV="$(mktemp)"
trap 'rm -f "$TMP_ENV"' EXIT
{
  echo "# Generated by install.sh — re-run the installer to change values."
  echo "TELEGRAM_TOKEN=${TOKEN}"
  echo "TELEGRAM_CHAT_ID=${CHAT}"
  echo "OLLAMA_URL=${OLL}"
  echo "OLLAMA_KEEP_ALIVE=${KA}"
  echo "NANO_BRIDGE_PORT=${PORT}"
  echo "NANO_CHROME_PROFILE=${PROFILE}"
  [[ -n "$CHROME" ]] && echo "NANO_CHROME_EXECUTABLE=${CHROME}"
  echo "NANO_LOG_FILE=${LOGF}"
  echo "TEMP_CRIT=${TEMP}"
  echo "VRAM_CRIT=${VRAM}"
  echo "DISK_CRIT=${DISK}"
} > "$TMP_ENV"
install -o "$RUN_USER" -g "$RUN_GROUP" -m 600 "$TMP_ENV" "$ENV_FILE"
c_dim "    permissions: 0600 ${RUN_USER}:${RUN_GROUP}"

# ---------- write systemd unit ----------
echo
c_grn "==> installing ${UNIT_PATH}"
TMP_UNIT="$(mktemp)"
trap 'rm -f "$TMP_ENV" "$TMP_UNIT"' EXIT
cat > "$TMP_UNIT" <<UNIT
[Unit]
Description=Telegram C2 Telemetry Bot
After=network-online.target ollama.service
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_GROUP}
WorkingDirectory=${HERE}
EnvironmentFile=${ENV_FILE}
ExecStart=${VENV_DIR}/bin/python ${HERE}/telemetry_bot.py
Restart=on-failure
RestartSec=3
# Don't restart-storm on bad config — give the user a chance to /journalctl.
StartLimitIntervalSec=60
StartLimitBurst=5

[Install]
WantedBy=multi-user.target
UNIT
install -m 644 "$TMP_UNIT" "$UNIT_PATH"

# ---------- activate ----------
echo
c_grn "==> daemon-reload + enable + restart"
systemctl daemon-reload
systemctl enable "${SERVICE}.service" >/dev/null
systemctl restart "${SERVICE}.service"

sleep 1
echo
c_grn "==> status"
systemctl --no-pager --lines=10 status "${SERVICE}.service" || true

echo
c_grn "==> done."
c_dim "    logs:        journalctl -u ${SERVICE} -f"
c_dim "    restart:     sudo systemctl restart ${SERVICE}"
c_dim "    stop:        sudo systemctl stop ${SERVICE}"
c_dim "    change env:  re-run sudo ./install.sh"
c_dim "    edit unit:   sudo systemctl edit ${SERVICE}    # drop-in override"
