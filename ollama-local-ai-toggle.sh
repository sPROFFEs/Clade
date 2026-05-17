#!/usr/bin/env bash
set -euo pipefail

PROVIDER_NAME="ollama_remote"
PROFILE_NAME="ollama_remote"
OPENCODE_PROVIDER_NAME="ollama_remote"
CODEX_CONFIG_DIR="${CODEX_HOME:-$HOME/.codex}"
CODEX_CONFIG="$CODEX_CONFIG_DIR/config.toml"
OPENCODE_CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/opencode"
OPENCODE_CONFIG="$OPENCODE_CONFIG_DIR/opencode.json"
SESSION_ONLY="${SESSION_ONLY:-0}"

title() {
  printf '\n== %s ==\n' "$1"
}

read_default() {
  local prompt="$1"
  local default="$2"
  local value
  printf '%s [%s]: ' "$prompt" "$default" >&2
  read -r value
  if [ -z "${value// }" ]; then
    printf '%s' "$default"
  else
    printf '%s' "$value"
  fi
}

normalize_endpoint() {
  local endpoint="${1%/}"
  case "$endpoint" in
    http://*|https://*) ;;
    *) endpoint="http://$endpoint" ;;
  esac
  printf '%s' "${endpoint%/}"
}

is_yes() {
  [[ "$1" =~ ^[sSyY] ]]
}

http_get() {
  local url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsS --max-time 8 "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- --timeout=8 "$url"
  else
    return 1
  fi
}

detect_models() {
  local endpoint="$1"
  local payload

  if payload="$(http_get "$endpoint/api/tags" 2>/dev/null)"; then
    printf '%s\n' "$payload" | sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sort -u
    return 0
  fi

  if payload="$(http_get "$endpoint/v1/models" 2>/dev/null)"; then
    printf '%s\n' "$payload" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sort -u
    return 0
  fi

  return 1
}

select_model() {
  local models=("$@")
  local choice idx

  if [ "${#models[@]}" -eq 0 ]; then
    read_default "No he podido detectar modelos. Escribe el nombre exacto" "qwen3-coder"
    return
  fi

  printf '\nModelos detectados:\n' >&2
  for i in "${!models[@]}"; do
    printf '  %s. %s\n' "$((i + 1))" "${models[$i]}" >&2
  done

  while true; do
    printf 'Elige numero o escribe un modelo: ' >&2
    read -r choice
    if [[ "$choice" =~ ^[0-9]+$ ]]; then
      idx=$((choice - 1))
      if [ "$idx" -ge 0 ] && [ "$idx" -lt "${#models[@]}" ]; then
        printf '%s' "${models[$idx]}"
        return
      fi
    fi
    if [ -n "${choice// }" ]; then
      printf '%s' "$choice"
      return
    fi
  done
}

shell_rc_file() {
  if [ -n "${ZSH_VERSION:-}" ]; then
    printf '%s' "$HOME/.zshrc"
  else
    printf '%s' "$HOME/.bashrc"
  fi
}

write_env_block() {
  local endpoint="$1"
  local rc_file
  rc_file="$(shell_rc_file)"
  touch "$rc_file"
  awk '
    $0 == "# >>> ollama-claude-codex" { skip=1; next }
    $0 == "# <<< ollama-claude-codex" { skip=0; next }
    skip != 1 { print }
  ' "$rc_file" > "$rc_file.tmp"
  cat >> "$rc_file.tmp" <<EOF
# >>> ollama-claude-codex
export ANTHROPIC_AUTH_TOKEN="ollama"
export ANTHROPIC_API_KEY=""
export ANTHROPIC_BASE_URL="$endpoint"
export OPENAI_API_KEY="ollama"
# <<< ollama-claude-codex
EOF
  mv "$rc_file.tmp" "$rc_file"
  export ANTHROPIC_AUTH_TOKEN="ollama"
  export ANTHROPIC_API_KEY=""
  export ANTHROPIC_BASE_URL="$endpoint"
  export OPENAI_API_KEY="ollama"
  printf 'Variables escritas en %s\n' "$rc_file"
}

remove_env_block() {
  local rc_file
  rc_file="$(shell_rc_file)"
  [ -f "$rc_file" ] || return 0
  awk '
    $0 == "# >>> ollama-claude-codex" { skip=1; next }
    $0 == "# <<< ollama-claude-codex" { skip=0; next }
    skip != 1 { print }
  ' "$rc_file" > "$rc_file.tmp"
  mv "$rc_file.tmp" "$rc_file"
  unset ANTHROPIC_AUTH_TOKEN ANTHROPIC_API_KEY ANTHROPIC_BASE_URL OPENAI_API_KEY
  printf 'Bloque eliminado de %s\n' "$rc_file"
}

remove_toml_table() {
  local header="$1"
  local file="$2"
  awk -v header="$header" '
    $0 == header { skip=1; next }
    skip == 1 && $0 ~ /^\[/ { skip=0 }
    skip != 1 { print }
  ' "$file"
}

update_codex_config() {
  local endpoint="$1"
  local model="$2"
  local make_default="$3"
  local wire_api="$4"
  local backup tmp base_url

  mkdir -p "$CODEX_CONFIG_DIR"
  touch "$CODEX_CONFIG"
  backup="$CODEX_CONFIG.bak-$(date +%Y%m%d-%H%M%S)"
  cp "$CODEX_CONFIG" "$backup"

  tmp="$(mktemp)"
  remove_toml_table "[model_providers.$PROVIDER_NAME]" "$CODEX_CONFIG" > "$tmp"
  remove_toml_table "[profiles.$PROFILE_NAME]" "$tmp" > "$tmp.2"
  mv "$tmp.2" "$tmp"

  if [ "$make_default" = "1" ]; then
    awk -v provider="$PROVIDER_NAME" '
      $0 ~ /^[[:space:]]*model_provider[[:space:]]*=/ { next }
      $0 ~ /^[[:space:]]*model[[:space:]]*=/ { next }
      { print }
    ' "$tmp" > "$tmp.2"
    {
      printf 'model_provider = "%s"\n' "$PROVIDER_NAME"
      printf 'model = "%s"\n' "$model"
      cat "$tmp.2"
    } > "$tmp"
  fi

  base_url="${endpoint%/}/v1"
  {
    sed '/^[[:space:]]*$/N;/^\n$/D' "$tmp"
    printf '\n[model_providers.%s]\n' "$PROVIDER_NAME"
    printf 'name = "Ollama Remote"\n'
    printf 'base_url = "%s"\n' "$base_url"
    printf 'env_key = "OPENAI_API_KEY"\n'
    printf 'wire_api = "%s"\n' "$wire_api"
    printf '\n[profiles.%s]\n' "$PROFILE_NAME"
    printf 'model_provider = "%s"\n' "$PROVIDER_NAME"
    printf 'model = "%s"\n' "$model"
  } > "$CODEX_CONFIG"

  rm -f "$tmp" "$tmp.2"
  printf 'Codex configurado en %s\nBackup: %s\n' "$CODEX_CONFIG" "$backup"
}

disable_codex_config() {
  [ -f "$CODEX_CONFIG" ] || {
    printf 'No existe config de Codex.\n'
    return 0
  }

  local backup tmp
  backup="$CODEX_CONFIG.bak-$(date +%Y%m%d-%H%M%S)"
  cp "$CODEX_CONFIG" "$backup"
  tmp="$(mktemp)"
  remove_toml_table "[model_providers.$PROVIDER_NAME]" "$CODEX_CONFIG" > "$tmp"
  remove_toml_table "[profiles.$PROFILE_NAME]" "$tmp" > "$tmp.2"
  awk -v provider="$PROVIDER_NAME" '
    $0 ~ "^[[:space:]]*model_provider[[:space:]]*=[[:space:]]*\"" provider "\"[[:space:]]*$" { next }
    { print }
  ' "$tmp.2" > "$CODEX_CONFIG"
  rm -f "$tmp" "$tmp.2"
  printf 'Bloques de Codex eliminados. Backup: %s\n' "$backup"
}

update_opencode_config() {
  local endpoint="$1"
  local model="$2"
  local make_default="$3"
  local backup=""

  command -v python3 >/dev/null 2>&1 || {
    printf 'Python 3 es necesario para editar opencode.json sin romper JSON.\n' >&2
    return 1
  }

  mkdir -p "$OPENCODE_CONFIG_DIR"
  if [ -f "$OPENCODE_CONFIG" ]; then
    backup="$OPENCODE_CONFIG.bak-$(date +%Y%m%d-%H%M%S)"
    cp "$OPENCODE_CONFIG" "$backup"
  fi

  OPENCODE_CONFIG="$OPENCODE_CONFIG" \
  OPENCODE_PROVIDER_NAME="$OPENCODE_PROVIDER_NAME" \
  OPENCODE_ENDPOINT="${endpoint%/}/v1" \
  OPENCODE_MODEL="$model" \
  OPENCODE_MAKE_DEFAULT="$make_default" \
  python3 - <<'PY'
import json
import os
from pathlib import Path

path = Path(os.environ["OPENCODE_CONFIG"])
provider = os.environ["OPENCODE_PROVIDER_NAME"]
endpoint = os.environ["OPENCODE_ENDPOINT"]
model = os.environ["OPENCODE_MODEL"]
make_default = os.environ["OPENCODE_MAKE_DEFAULT"] == "1"

if path.exists() and path.read_text(encoding="utf-8").strip():
    data = json.loads(path.read_text(encoding="utf-8"))
else:
    data = {}

data["$schema"] = "https://opencode.ai/config.json"
data.setdefault("provider", {})
data["provider"][provider] = {
    "npm": "@ai-sdk/openai-compatible",
    "name": "Ollama Remote",
    "options": {
        "baseURL": endpoint
    },
    "models": {
        model: {
            "name": model
        }
    }
}

if make_default:
    data["model"] = f"{provider}/{model}"
    data["small_model"] = f"{provider}/{model}"

path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
PY

  printf 'OpenCode configurado en %s\n' "$OPENCODE_CONFIG"
  if [ -n "$backup" ]; then
    printf 'Backup: %s\n' "$backup"
  fi
}

disable_opencode_config() {
  [ -f "$OPENCODE_CONFIG" ] || {
    printf 'No existe config de OpenCode.\n'
    return 0
  }

  command -v python3 >/dev/null 2>&1 || {
    printf 'Python 3 es necesario para editar opencode.json sin romper JSON.\n' >&2
    return 1
  }

  local backup
  backup="$OPENCODE_CONFIG.bak-$(date +%Y%m%d-%H%M%S)"
  cp "$OPENCODE_CONFIG" "$backup"

  OPENCODE_CONFIG="$OPENCODE_CONFIG" \
  OPENCODE_PROVIDER_NAME="$OPENCODE_PROVIDER_NAME" \
  python3 - <<'PY'
import json
import os
from pathlib import Path

path = Path(os.environ["OPENCODE_CONFIG"])
provider = os.environ["OPENCODE_PROVIDER_NAME"]
data = json.loads(path.read_text(encoding="utf-8"))

if isinstance(data.get("provider"), dict):
    data["provider"].pop(provider, None)

for key in ("model", "small_model"):
    value = data.get(key)
    if isinstance(value, str) and value.startswith(provider + "/"):
        data.pop(key, None)

path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
PY

  printf 'Provider de OpenCode eliminado. Backup: %s\n' "$backup"
}

enable_local_models() {
  title "Endpoint Ollama"
  endpoint="$(normalize_endpoint "$(read_default "Endpoint remoto" "http://192.168.1.50:11434")")"

  printf 'Probando %s ...\n' "$endpoint"
  mapfile -t models < <(detect_models "$endpoint" || true)
  if [ "${#models[@]}" -eq 0 ]; then
    printf 'No se han detectado modelos. Revisa firewall, OLLAMA_HOST y /api/tags.\n'
  fi

  model="$(select_model "${models[@]}")"

  title "Herramientas"
  enable_claude="$(read_default "Configurar Claude Code? (s/n)" "s")"
  enable_codex="$(read_default "Configurar Codex CLI? (s/n)" "s")"
  enable_opencode="$(read_default "Configurar OpenCode? (s/n)" "s")"

  if is_yes "$enable_claude"; then
    if [ "$SESSION_ONLY" = "1" ]; then
      export ANTHROPIC_AUTH_TOKEN="ollama"
      export ANTHROPIC_API_KEY=""
      export ANTHROPIC_BASE_URL="$endpoint"
    else
      write_env_block "$endpoint"
    fi
    printf 'Claude Code configurado. Uso: claude --model %s\n' "$model"
  fi

  if is_yes "$enable_codex"; then
    export OPENAI_API_KEY="ollama"
    make_default="$(read_default "Hacer Ollama el default de Codex? Si dices no, usa codex -p $PROFILE_NAME (s/n)" "n")"
    wire_api="$(read_default "Wire API para Codex: chat o responses" "chat")"
    case "$wire_api" in
      chat|responses) ;;
      *) wire_api="chat" ;;
    esac
    if is_yes "$make_default"; then
      update_codex_config "$endpoint" "$model" "1" "$wire_api"
    else
      update_codex_config "$endpoint" "$model" "0" "$wire_api"
    fi
    printf 'Uso recomendado: codex -p %s\n' "$PROFILE_NAME"
  fi

  if is_yes "$enable_opencode"; then
    make_default="$(read_default "Hacer Ollama el default de OpenCode? (s/n)" "s")"
    if is_yes "$make_default"; then
      update_opencode_config "$endpoint" "$model" "1"
    else
      update_opencode_config "$endpoint" "$model" "0"
    fi
    printf 'Uso: opencode y luego /models si quieres cambiar modelo\n'
  fi
}

disable_local_models() {
  title "Deshabilitar"
  disable_claude="$(read_default "Quitar variables de Claude Code? (s/n)" "s")"
  disable_codex="$(read_default "Quitar provider/profile Ollama de Codex? (s/n)" "s")"
  disable_opencode="$(read_default "Quitar provider Ollama de OpenCode? (s/n)" "s")"

  if is_yes "$disable_claude"; then
    remove_env_block
  fi

  if is_yes "$disable_codex"; then
    disable_codex_config
  fi

  if is_yes "$disable_opencode"; then
    disable_opencode_config
  fi
}

show_status() {
  title "Estado"
  printf 'ANTHROPIC_BASE_URL = %s\n' "${ANTHROPIC_BASE_URL:-}"
  printf 'ANTHROPIC_AUTH_TOKEN = %s\n' "${ANTHROPIC_AUTH_TOKEN:-}"
  if [ -n "${OPENAI_API_KEY:-}" ]; then
    printf 'OPENAI_API_KEY set = true\n'
  else
    printf 'OPENAI_API_KEY set = false\n'
  fi
  printf 'Codex config = %s\n' "$CODEX_CONFIG"
  if [ -f "$CODEX_CONFIG" ]; then
    grep -E 'ollama_remote|model_provider|base_url|wire_api|^[[:space:]]*model[[:space:]]*=' "$CODEX_CONFIG" || true
  fi
  printf 'OpenCode config = %s\n' "$OPENCODE_CONFIG"
  if [ -f "$OPENCODE_CONFIG" ]; then
    grep -E 'ollama_remote|baseURL|model|small_model' "$OPENCODE_CONFIG" || true
  fi
}

while true; do
  title "Ollama remoto para Claude Code / Codex CLI"
  printf '1. Habilitar/configurar\n'
  printf '2. Deshabilitar\n'
  printf '3. Estado\n'
  printf '4. Salir\n'
  read -r -p "Opcion: " choice

  case "$choice" in
    1) enable_local_models ;;
    2) disable_local_models ;;
    3) show_status ;;
    4) exit 0 ;;
    *) printf 'Opcion no valida\n' ;;
  esac
done
