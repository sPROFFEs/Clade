#!/usr/bin/env bash
set -euo pipefail

SCRIPT_VERSION="0.1.0-experimental"

declare -A SELECTED_COMMANDS

title() {
  printf '\n== %s ==\n' "$1"
}

warn() {
  printf 'Aviso: %s\n' "$1" >&2
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

is_yes() {
  [[ "$1" =~ ^[sSyY] ]]
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

detect_os() {
  local uname_s
  uname_s="$(uname -s 2>/dev/null || printf unknown)"

  case "$uname_s" in
    Darwin)
      printf 'macos'
      return
      ;;
    Linux)
      if grep -qi microsoft /proc/version 2>/dev/null || grep -qi microsoft /proc/sys/kernel/osrelease 2>/dev/null; then
        printf 'wsl'
      else
        printf 'linux'
      fi
      return
      ;;
    MINGW*|MSYS*|CYGWIN*)
      printf 'windows-bash'
      return
      ;;
  esac

  printf 'unknown'
}

detect_pkg_hint() {
  local os="$1"
  case "$os" in
    macos)
      has_cmd brew && printf 'brew' || printf 'none'
      ;;
    linux|wsl)
      if has_cmd apt; then printf 'apt'
      elif has_cmd dnf; then printf 'dnf'
      elif has_cmd pacman; then printf 'pacman'
      elif has_cmd zypper; then printf 'zypper'
      elif has_cmd brew; then printf 'linuxbrew'
      else printf 'none'
      fi
      ;;
    windows-bash)
      if has_cmd winget; then printf 'winget'
      elif has_cmd powershell.exe; then printf 'powershell'
      elif has_cmd cmd.exe; then printf 'cmd'
      else printf 'none'
      fi
      ;;
    *)
      printf 'none'
      ;;
  esac
}

print_detected() {
  local os="$1"
  title "Entorno detectado"
  printf 'Script: %s\n' "$SCRIPT_VERSION"
  printf 'OS: %s\n' "$os"
  printf 'Gestor base: %s\n' "$(detect_pkg_hint "$os")"
  printf 'node: %s\n' "$(has_cmd node && node --version || printf 'no')"
  printf 'npm: %s\n' "$(has_cmd npm && npm --version || printf 'no')"
  printf 'bun: %s\n' "$(has_cmd bun && bun --version || printf 'no')"
  printf 'brew: %s\n' "$(has_cmd brew && brew --version | head -n1 || printf 'no')"
  printf 'winget: %s\n' "$(has_cmd winget && winget --version || printf 'no')"
  printf 'paru: %s\n' "$(has_cmd paru && paru --version | head -n1 || printf 'no')"
}

command_available_reason() {
  local command_id="$1"
  case "$command_id" in
    npm) has_cmd npm ;;
    bun) has_cmd bun ;;
    brew) has_cmd brew ;;
    paru) has_cmd paru ;;
    winget) has_cmd winget ;;
    powershell) has_cmd powershell.exe || has_cmd pwsh || has_cmd powershell ;;
    cmd) has_cmd cmd.exe || has_cmd cmd ;;
    curl) has_cmd curl ;;
    *) return 0 ;;
  esac
}

add_option() {
  local -n arr_ref="$1"
  local id="$2"
  local label="$3"
  local cmd="$4"
  local available_key="$5"

  if command_available_reason "$available_key"; then
    arr_ref+=("$id|$label|$cmd")
  fi
}

build_options() {
  local agent="$1"
  local action="$2"
  local os="$3"
  local -n out="$4"
  out=()

  case "$agent:$action" in
    opencode:install)
      add_option out curl "OpenCode official install script" "curl -fsSL https://opencode.ai/install | bash" curl
      add_option out npm "npm global package" "npm i -g opencode-ai" npm
      add_option out bun "bun global package" "bun add -g opencode-ai" bun
      add_option out brew "Homebrew tap" "brew install anomalyco/tap/opencode" brew
      add_option out paru "AUR via paru" "paru -S opencode" paru
      ;;
    opencode:update)
      add_option out curl "Re-run official install script" "curl -fsSL https://opencode.ai/install | bash" curl
      add_option out npm "npm latest" "npm i -g opencode-ai@latest" npm
      add_option out bun "bun latest" "bun add -g opencode-ai@latest" bun
      add_option out brew "Homebrew upgrade" "brew upgrade anomalyco/tap/opencode" brew
      add_option out paru "AUR upgrade" "paru -Syu opencode" paru
      ;;
    claude:install)
      case "$os" in
        macos)
          add_option out brew "Homebrew cask" "brew install --cask claude-code" brew
          add_option out curl "Official macOS/Linux install script" "curl -fsSL https://claude.ai/install.sh | bash" curl
          ;;
        linux|wsl)
          add_option out curl "Official macOS/Linux/WSL install script" "curl -fsSL https://claude.ai/install.sh | bash" curl
          ;;
        windows-bash)
          add_option out winget "winget package" "winget install Anthropic.ClaudeCode" winget
          add_option out powershell "PowerShell installer" "powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"irm https://claude.ai/install.ps1 | iex\"" powershell
          add_option out cmd "CMD installer" "cmd.exe /c \"curl -fsSL https://claude.ai/install.cmd -o install.cmd && install.cmd && del install.cmd\"" cmd
          ;;
      esac
      ;;
    claude:update)
      case "$os" in
        macos)
          add_option out brew "Homebrew cask upgrade" "brew upgrade --cask claude-code" brew
          add_option out curl "Re-run official install script" "curl -fsSL https://claude.ai/install.sh | bash" curl
          ;;
        linux|wsl)
          add_option out curl "Re-run official install script" "curl -fsSL https://claude.ai/install.sh | bash" curl
          ;;
        windows-bash)
          add_option out winget "winget upgrade" "winget upgrade Anthropic.ClaudeCode" winget
          add_option out powershell "PowerShell installer" "powershell.exe -NoProfile -ExecutionPolicy Bypass -Command \"irm https://claude.ai/install.ps1 | iex\"" powershell
          add_option out cmd "CMD installer" "cmd.exe /c \"curl -fsSL https://claude.ai/install.cmd -o install.cmd && install.cmd && del install.cmd\"" cmd
          ;;
      esac
      ;;
    codex:install)
      case "$os" in
        macos)
          add_option out brew "Homebrew formula" "brew install codex" brew
          add_option out npm "npm global package" "npm i -g @openai/codex" npm
          ;;
        linux|wsl|windows-bash)
          add_option out npm "npm global package" "npm i -g @openai/codex" npm
          add_option out brew "Homebrew/Linuxbrew formula" "brew install codex" brew
          ;;
      esac
      ;;
    codex:update)
      case "$os" in
        macos)
          add_option out brew "Homebrew upgrade" "brew upgrade codex" brew
          add_option out npm "npm latest" "npm i -g @openai/codex@latest" npm
          ;;
        linux|wsl|windows-bash)
          add_option out npm "npm latest" "npm i -g @openai/codex@latest" npm
          add_option out brew "Homebrew/Linuxbrew upgrade" "brew upgrade codex" brew
          ;;
      esac
      ;;
  esac
}

preferred_option_index() {
  local agent="$1"
  local action="$2"
  local os="$3"
  local -n options_ref="$4"
  local preferred_ids=()

  case "$agent:$os" in
    claude:macos) preferred_ids=(brew curl) ;;
    claude:linux|claude:wsl) preferred_ids=(curl) ;;
    claude:windows-bash) preferred_ids=(winget powershell cmd) ;;
    codex:macos) preferred_ids=(brew npm) ;;
    codex:linux|codex:wsl|codex:windows-bash) preferred_ids=(npm brew) ;;
    opencode:macos) preferred_ids=(curl brew npm bun) ;;
    opencode:linux|opencode:wsl) preferred_ids=(curl npm bun brew paru) ;;
    opencode:windows-bash) preferred_ids=(npm bun curl) ;;
    *) preferred_ids=(curl npm brew bun winget powershell cmd paru) ;;
  esac

  local preferred id i option
  for preferred in "${preferred_ids[@]}"; do
    for i in "${!options_ref[@]}"; do
      option="${options_ref[$i]}"
      id="${option%%|*}"
      if [ "$id" = "$preferred" ]; then
        printf '%s' "$i"
        return
      fi
    done
  done

  printf '0'
}

choose_command() {
  local agent="$1"
  local action="$2"
  local os="$3"
  local options=()
  local default_index choice idx option label cmd

  build_options "$agent" "$action" "$os" options
  if [ "${#options[@]}" -eq 0 ]; then
    warn "No hay metodo disponible para $agent ($action) en este entorno."
    return 1
  fi

  default_index="$(preferred_option_index "$agent" "$action" "$os" options)"

  printf '\n%s %s:\n' "$agent" "$action"
  for i in "${!options[@]}"; do
    IFS='|' read -r _ label cmd <<< "${options[$i]}"
    if [ "$i" = "$default_index" ]; then
      printf '  %s. %s [recomendado]\n     %s\n' "$((i + 1))" "$label" "$cmd"
    else
      printf '  %s. %s\n     %s\n' "$((i + 1))" "$label" "$cmd"
    fi
  done
  printf '  0. Saltar\n'

  choice="$(read_default "Elige metodo" "$((default_index + 1))")"
  if [ "$choice" = "0" ]; then
    return 1
  fi
  if ! [[ "$choice" =~ ^[0-9]+$ ]]; then
    warn "Seleccion no numerica, salto $agent."
    return 1
  fi

  idx=$((choice - 1))
  if [ "$idx" -lt 0 ] || [ "$idx" -ge "${#options[@]}" ]; then
    warn "Seleccion fuera de rango, salto $agent."
    return 1
  fi

  option="${options[$idx]}"
  cmd="${option#*|}"
  cmd="${cmd#*|}"
  SELECTED_COMMANDS["$agent"]="$cmd"
}

execute_command() {
  local agent="$1"
  local cmd="${SELECTED_COMMANDS[$agent]:-}"
  [ -n "$cmd" ] || return 0

  title "Ejecutando $agent"
  printf '%s\n' "$cmd"

  if [[ "$cmd" == curl*'|'*bash* ]]; then
    warn "Este metodo ejecuta un script remoto con curl | bash. Es oficial segun tus comandos, pero sigue siendo una ejecucion remota."
  fi

  local confirm
  confirm="$(read_default "Ejecutar ahora? (s/n)" "s")"
  if is_yes "$confirm"; then
    bash -lc "$cmd"
  else
    printf 'Saltado: %s\n' "$agent"
  fi
}

select_agents() {
  local selected="$1"
  case "$selected" in
    all) printf 'opencode claude codex' ;;
    opencode|claude|codex) printf '%s' "$selected" ;;
    *)
      warn "Agente no valido: $selected. Usando all."
      printf 'opencode claude codex'
      ;;
  esac
}

usage() {
  cat <<'EOF'
Uso:
  ./agent-cli-installer.sh
  ./agent-cli-installer.sh install all
  ./agent-cli-installer.sh update codex
  ./agent-cli-installer.sh install opencode --dry-run

Agentes:
  opencode, claude, codex, all

Acciones:
  install, update

Notas:
  - Experimental: Bash primero; PowerShell/CMD vendran despues.
  - Detecta macOS, Linux, WSL y Bash sobre Windows.
  - No instala Node, Bun, Homebrew, winget ni paru; solo los usa si ya existen.
EOF
}

main() {
  local action="${1:-}"
  local target="${2:-}"
  local dry_run="0"
  local os agents agent

  for arg in "$@"; do
    case "$arg" in
      --dry-run) dry_run="1" ;;
      -h|--help) usage; exit 0 ;;
    esac
  done

  os="$(detect_os)"
  print_detected "$os"

  if [ -z "$action" ]; then
    action="$(read_default "Accion: install o update" "install")"
  fi
  case "$action" in
    install|update) ;;
    *) warn "Accion no valida: $action"; usage; exit 1 ;;
  esac

  if [ -z "$target" ] || [[ "$target" == --* ]]; then
    target="$(read_default "Agente: opencode, claude, codex, all" "all")"
  fi

  agents="$(select_agents "$target")"

  for agent in $agents; do
    choose_command "$agent" "$action" "$os" || true
  done

  title "Resumen"
  for agent in $agents; do
    if [ -n "${SELECTED_COMMANDS[$agent]:-}" ]; then
      printf '%s: %s\n' "$agent" "${SELECTED_COMMANDS[$agent]}"
    else
      printf '%s: saltado\n' "$agent"
    fi
  done

  if [ "$dry_run" = "1" ]; then
    printf '\nDry-run activo. No se ejecuta nada.\n'
    exit 0
  fi

  for agent in $agents; do
    execute_command "$agent"
  done
}

main "$@"
