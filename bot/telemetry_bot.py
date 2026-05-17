"""Telemetry + Ollama + Gemini-Nano-bridge controller bot.

Replaces the older telemetry_bot.txt. Dropped:
  - ComfyUI launch button (no longer relevant)
  - Spanish-only "kill all" wording (kept the action, English-friendly)

Added:
  - Richer hardware status (CPU/RAM/disk/net + per-GPU temp/util/VRAM
    + top processes + Ollama running models + bridge state).
  - Ollama model management: /models /pull /rm /load /unload /loaded
    /keepalive.
  - Gemini Nano bridge controller: /nano_start  /nano_stop  /nano_url
    /nano_status — starts nano_bridge.py as a subprocess, parses the
    "listening on …" line out of its log, and reports the exact URL
    you paste into code-launcher's Ollama screen.

Config: read from env vars. See config.example.env.
"""

from __future__ import annotations

import logging
import os
import re
import shlex
import socket
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import Optional

import psutil
import telebot
from telebot.types import (
    BotCommand,
    ForceReply,
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    KeyboardButton,
    ReplyKeyboardMarkup,
)

# Local modules
import ollama_client as oc


# ---------- config ----------

def _env(name: str, default: str = "") -> str:
    v = os.environ.get(name, default)
    if not v and default == "":
        sys.stderr.write(f"telemetry_bot: missing env var {name}\n")
        sys.exit(2)
    return v


TOKEN = _env("TELEGRAM_TOKEN")
CHAT_ID = int(_env("TELEGRAM_CHAT_ID"))
TEMP_CRIT = int(_env("TEMP_CRIT", "85"))
VRAM_CRIT = int(_env("VRAM_CRIT", "95"))
DISK_CRIT = int(_env("DISK_CRIT", "90"))
NANO_PORT = int(_env("NANO_BRIDGE_PORT", "8765"))
NANO_LOG = _env("NANO_LOG_FILE", str(Path(__file__).parent / "nano_bridge.log"))

bot = telebot.TeleBot(TOKEN)

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("bot")


# ---------- auth ----------

def auth(func):
    def wrapper(message):
        if message.chat.id != CHAT_ID:
            log.warning("denied chat_id=%s", message.chat.id)
            bot.reply_to(message, "🚫 unauthorized.")
            return
        return func(message)
    return wrapper


# ---------- keyboards ----------

def main_kb() -> ReplyKeyboardMarkup:
    """Persistent reply keyboard. Stays under every message so the
    user can always jump to a top-level area in one tap."""
    kb = ReplyKeyboardMarkup(resize_keyboard=True, row_width=2)
    kb.add(KeyboardButton("📊 status"),
           KeyboardButton("🧠 ollama"))
    kb.add(KeyboardButton("🤖 nano bridge"),
           KeyboardButton("⚡ loaded"))
    kb.add(KeyboardButton("ℹ️ help"),
           KeyboardButton("💀 kill all"))
    return kb


def ollama_kb() -> InlineKeyboardMarkup:
    """Top-level Ollama menu. Each button leads to its own
    interactive flow — no more 'type /pull <name>' hints."""
    kb = InlineKeyboardMarkup(row_width=2)
    kb.add(InlineKeyboardButton("📦 browse models", callback_data="oll:browse"),
           InlineKeyboardButton("⚡ loaded", callback_data="oll:loaded"))
    kb.add(InlineKeyboardButton("⬇️ pull new model", callback_data="oll:pull"),
           InlineKeyboardButton("⏱ keepalive", callback_data="oll:keepmenu"))
    kb.add(InlineKeyboardButton("🔄 refresh", callback_data="oll:refresh"))
    return kb


def model_list_kb(models: list, prefix: str = "m") -> InlineKeyboardMarkup:
    """One button per installed model — clicking opens the action
    sheet for that model. Truncated names so long IDs don't overflow."""
    kb = InlineKeyboardMarkup(row_width=1)
    for m in models:
        label = f"{m.name[:34]:34s} · {oc._humanize_mb(m.size_mb)}"
        kb.add(InlineKeyboardButton(label.strip() + "  ›",
                                    callback_data=f"{prefix}:open:{m.name}"))
    kb.add(InlineKeyboardButton("← back", callback_data="oll:back"))
    return kb


def model_actions_kb(name: str, is_loaded: bool) -> InlineKeyboardMarkup:
    """Per-model action sheet. Hides 'unload' when not in VRAM,
    'load' when already loaded — no dead actions."""
    kb = InlineKeyboardMarkup(row_width=2)
    if is_loaded:
        kb.add(InlineKeyboardButton("❄️ unload", callback_data=f"m:unload:{name}"))
    else:
        kb.add(InlineKeyboardButton("🔥 load", callback_data=f"m:load:{name}"))
    kb.add(InlineKeyboardButton("🗑 delete", callback_data=f"m:delask:{name}"))
    kb.add(InlineKeyboardButton("← back", callback_data="oll:browse"))
    return kb


def keepalive_kb() -> InlineKeyboardMarkup:
    """Quick-pick TTLs. 'pin' = -1 (until ollama restart),
    '❄️ 0' = 0 (flush right now)."""
    kb = InlineKeyboardMarkup(row_width=5)
    kb.row(*[InlineKeyboardButton(lbl, callback_data=f"k:{v}")
             for lbl, v in [("5m", "5m"), ("1h", "1h"), ("24h", "24h"),
                            ("📌 pin", "-1"), ("❄️ 0", "0")]])
    kb.add(InlineKeyboardButton("← back", callback_data="oll:back"))
    return kb


def confirm_kb(yes_data: str, label_yes: str = "✅ yes",
               label_no: str = "❌ no") -> InlineKeyboardMarkup:
    """Two-button confirmation. yes_data is the callback fired on
    'yes'; 'no' always cancels (callback c:cancel)."""
    kb = InlineKeyboardMarkup(row_width=2)
    kb.add(InlineKeyboardButton(label_yes, callback_data=yes_data),
           InlineKeyboardButton(label_no, callback_data="c:cancel"))
    return kb


def status_refresh_kb() -> InlineKeyboardMarkup:
    kb = InlineKeyboardMarkup(row_width=2)
    kb.add(InlineKeyboardButton("🔄 refresh", callback_data="status:refresh"),
           InlineKeyboardButton("⚡ loaded", callback_data="oll:loaded"))
    return kb


def nano_kb() -> InlineKeyboardMarkup:
    kb = InlineKeyboardMarkup(row_width=2)
    if bridge.running():
        kb.add(InlineKeyboardButton("⏹ stop bridge", callback_data="nano:stop"),
               InlineKeyboardButton("🔗 url", callback_data="nano:url"))
        kb.add(InlineKeyboardButton("📋 status", callback_data="nano:status"),
               InlineKeyboardButton("📜 logs", callback_data="nano:logs"))
    elif _bridge_ready():
        kb.add(InlineKeyboardButton("▶️ start bridge", callback_data="nano:start"))
        kb.add(InlineKeyboardButton("📋 status", callback_data="nano:status"),
               InlineKeyboardButton("⬆️ update", callback_data="nano:update"))
    else:
        # Surface setup directly when prereqs missing — saves a hop.
        kb.add(InlineKeyboardButton("⚙️ setup (one-time)", callback_data="nano:setup"))
        kb.add(InlineKeyboardButton("📋 check state", callback_data="nano:check"))
    return kb


# ---------- telemetry ----------

def _gpu_stats() -> Optional[list[dict]]:
    """nvidia-smi parse. Adds GPU utilization %, not just temp/VRAM."""
    try:
        out = subprocess.check_output([
            "nvidia-smi",
            "--query-gpu=index,name,temperature.gpu,utilization.gpu,memory.used,memory.total",
            "--format=csv,noheader,nounits",
        ], encoding="utf-8", timeout=4)
    except (subprocess.CalledProcessError, FileNotFoundError, subprocess.TimeoutExpired):
        return None
    gpus = []
    for line in out.strip().splitlines():
        parts = [p.strip() for p in line.split(",")]
        if len(parts) != 6:
            continue
        idx, name, temp, util, used, total = parts
        used_i, total_i = int(used), int(total)
        gpus.append({
            "id": idx, "name": name, "temp": int(temp), "util": int(util),
            "mem_used": used_i, "mem_total": total_i,
            "mem_pct": round(used_i / total_i * 100, 1) if total_i else 0,
        })
    return gpus


def _top_procs(n: int = 3) -> list[str]:
    """Top N processes by CPU%."""
    procs = []
    for p in psutil.process_iter(["name", "cpu_percent", "memory_percent"]):
        try:
            procs.append(p.info)
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            continue
    procs.sort(key=lambda x: x.get("cpu_percent") or 0, reverse=True)
    out = []
    for p in procs[:n]:
        out.append(f"  {p['name'][:24]:24s} cpu={p.get('cpu_percent', 0):4.1f}% "
                   f"mem={p.get('memory_percent', 0):4.1f}%")
    return out


def _net_io() -> str:
    io = psutil.net_io_counters()
    return f"net  ↑ {io.bytes_sent/1e9:.1f} GB · ↓ {io.bytes_recv/1e9:.1f} GB"


def status_text() -> str:
    cpu = psutil.cpu_percent(interval=0.4)
    ram = psutil.virtual_memory().percent
    disk = psutil.disk_usage("/").percent
    parts = [
        "*📊 system*",
        f"cpu {cpu:4.1f}% · ram {ram:4.1f}% · disk {disk:4.1f}%",
        _net_io(),
    ]
    gpus = _gpu_stats()
    if gpus:
        parts.append("\n*🎮 gpus*")
        for g in gpus:
            parts.append(f"[{g['id']}] {g['name']}: {g['temp']}°C · util {g['util']:3d}% · "
                         f"vram {g['mem_used']}/{g['mem_total']} MB ({g['mem_pct']}%)")
    else:
        parts.append("\n*🎮 gpus*: nvidia-smi unavailable")

    # Ollama line — version + currently-loaded models
    v = oc.reachable()
    if v:
        parts.append(f"\n*🧠 ollama* v{v}")
        try:
            running = oc.list_running()
            if running:
                for m in running:
                    parts.append("  ⚡ " + oc.humanize_running(m))
            else:
                parts.append("  (no models loaded)")
        except oc.OllamaError as e:
            parts.append(f"  (ps error: {e})")
    else:
        parts.append("\n*🧠 ollama*: unreachable")

    # Bridge
    parts.append("\n*🤖 nano bridge*")
    parts.append(f"  {'running on ' + bridge.url() if bridge.running() else 'stopped'}")

    # Top procs
    parts.append("\n*top procs*")
    parts.extend(_top_procs())
    return "\n".join(parts)


# ---------- critical alerts ----------

def alert_loop() -> None:
    """Background daemon: pings the user when temp/VRAM/disk cross
    thresholds. Back-off to 5 min after firing so we don't spam."""
    while True:
        try:
            alerts = []
            disk = psutil.disk_usage("/").percent
            if disk > DISK_CRIT:
                alerts.append(f"⚠️ DISK {disk:.0f}% > {DISK_CRIT}%")
            gpus = _gpu_stats() or []
            for g in gpus:
                if g["temp"] > TEMP_CRIT:
                    alerts.append(f"🔥 GPU{g['id']} TEMP {g['temp']}°C > {TEMP_CRIT}")
                if g["mem_pct"] > VRAM_CRIT:
                    alerts.append(f"💥 GPU{g['id']} VRAM {g['mem_pct']:.0f}% > {VRAM_CRIT}")
            if alerts:
                bot.send_message(CHAT_ID, "🚨 *ALERT*\n" + "\n".join(alerts),
                                 parse_mode="Markdown")
                time.sleep(300)
            else:
                time.sleep(60)
        except Exception:
            log.exception("alert loop crashed")
            time.sleep(60)


# ---------- kill-all ----------

def purge() -> None:
    """Stop ollama + the nano bridge and reclaim VRAM."""
    bridge.stop()
    subprocess.run(["sudo", "systemctl", "stop", "ollama"],
                   stderr=subprocess.DEVNULL)
    subprocess.run(["sudo", "pkill", "-9", "ollama"], stderr=subprocess.DEVNULL)
    # NVIDIA driver takes a second to actually release VRAM
    time.sleep(2)


# ---------- nano bridge controller ----------

def _detect_local_ip() -> str:
    """Best-effort: the IP a remote host on this LAN can reach. Falls
    back to whatever hostname resolves to, then 127.0.0.1."""
    try:
        # No packets are actually sent; the connect() picks the right
        # outgoing interface so getsockname() returns the right IP.
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 53))
        ip = s.getsockname()[0]
        s.close()
        return ip
    except OSError:
        try:
            return socket.gethostbyname(socket.gethostname())
        except OSError:
            return "127.0.0.1"


class NanoBridgeController:
    """Wraps the nano_bridge.py subprocess lifecycle."""

    def __init__(self, port: int, log_file: str) -> None:
        self.port = port
        self.log_file = log_file
        self.proc: Optional[subprocess.Popen] = None
        self._started_at: float = 0.0

    def running(self) -> bool:
        return self.proc is not None and self.proc.poll() is None

    def url(self) -> str:
        return f"http://{_detect_local_ip()}:{self.port}/v1"

    def localhost_url(self) -> str:
        return f"http://127.0.0.1:{self.port}/v1"

    def start(self) -> str:
        if self.running():
            return f"already running on {self.url()}"
        if not os.environ.get("NANO_CHROME_PROFILE"):
            return "❌ NANO_CHROME_PROFILE not set — see README 'Prep Chrome for Nano'"
        if not _bridge_ready():
            return ("❌ bridge prerequisites missing.\n"
                    "Run `/nano_setup` first — it installs Playwright, downloads "
                    "Chromium, and primes Gemini Nano (one-time, may take several "
                    "minutes on first use).")
        script = Path(__file__).parent / "nano_bridge.py"
        if not script.exists():
            return f"❌ {script} missing"
        env = os.environ.copy()
        env["NANO_BRIDGE_PORT"] = str(self.port)
        env["NANO_LOG_FILE"] = self.log_file
        # Truncate stale log so we can poll for the "listening on" line.
        open(self.log_file, "w").close()
        self.proc = subprocess.Popen(
            [sys.executable, str(script)],
            env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            start_new_session=True,
        )
        self._started_at = time.time()
        # Wait up to 20s for the bridge to print "listening on ..."
        # (first launch downloads Nano if not present — can take a while).
        for _ in range(20):
            time.sleep(1)
            if self.proc.poll() is not None:
                tail = _tail(self.log_file, 30)
                self.proc = None
                return f"❌ bridge exited during start:\n```\n{tail}\n```"
            if "listening on" in _tail(self.log_file, 200):
                return (f"✅ bridge up\n"
                        f"\n*paste into code-launcher Ollama screen:*\n"
                        f"  endpoint:  `{self.url()}`\n"
                        f"  model:     `gemini-nano`\n"
                        f"\n_(or localhost-only: `{self.localhost_url()}`)_")
        return ("⚠️ bridge running but didn't announce 'listening on' within 20s. "
                f"Tail:\n```\n{_tail(self.log_file, 30)}\n```")

    def stop(self) -> str:
        if not self.running():
            return "not running"
        assert self.proc
        self.proc.terminate()
        try:
            self.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.proc.kill()
        self.proc = None
        return "🛑 bridge stopped"

    def status(self) -> str:
        if not self.running():
            return "stopped"
        uptime = int(time.time() - self._started_at)
        return (f"running · pid {self.proc.pid} · uptime {uptime}s\n"
                f"url: {self.url()}\n"
                f"localhost: {self.localhost_url()}\n"
                f"log tail:\n```\n{_tail(self.log_file, 8)}\n```")


def _tail(path: str, n: int) -> str:
    try:
        with open(path, "r", errors="replace") as f:
            return "\n".join(f.read().splitlines()[-n:])
    except OSError:
        return ""


bridge = NanoBridgeController(NANO_PORT, NANO_LOG)


# Quick check the bridge's Python deps + Chromium are usable without
# importing playwright at the top of the bot (keeps the bot startable
# on minimal hosts that haven't installed bridge deps yet).
def _bridge_ready() -> bool:
    try:
        import playwright as _pw  # noqa: F401
        import aiohttp as _ah     # noqa: F401
    except ImportError:
        return False
    # Verify chromium artifact is present. We do this cheaply by
    # asking the playwright CLI; importing sync_playwright would spin
    # up a node child unnecessarily on every status check.
    try:
        rc = subprocess.call(
            [sys.executable, "-c",
             "from playwright.sync_api import sync_playwright;"
             "import pathlib,sys;"
             "exe = sync_playwright().__enter__().chromium.executable_path;"
             "sys.exit(0 if exe and pathlib.Path(exe).exists() else 1)"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        return rc == 0
    except (subprocess.TimeoutExpired, OSError):
        return False


def _run_setup_streaming(message, mode: str) -> None:
    """Spawn nano_setup.py with the given subcommand and stream its
    stdout line-by-line into a single Telegram message (edited in
    place so we don't spam)."""
    script = Path(__file__).parent / "nano_setup.py"
    if not script.exists():
        bot.reply_to(message, f"❌ {script} missing")
        return
    placeholder = bot.reply_to(message, f"⚙️ running `nano_setup.py {mode}`...",
                               parse_mode="Markdown")
    buf: list[str] = [f"⚙️ `nano_setup.py {mode}`\n```"]

    def _flush(force: bool = False) -> None:
        # Telegram caps messages at 4096 chars; keep a tail window.
        body = "\n".join(buf[-40:]) + "\n```"
        try:
            bot.edit_message_text(body, CHAT_ID, placeholder.message_id,
                                  parse_mode="Markdown")
        except Exception:
            # Likely "message is not modified" — fine.
            pass

    try:
        proc = subprocess.Popen(
            [sys.executable, str(script), mode],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            bufsize=1)
    except OSError as e:
        bot.edit_message_text(f"❌ launch failed: {e}", CHAT_ID,
                              placeholder.message_id)
        return

    last_edit = 0.0
    assert proc.stdout
    for line in proc.stdout:
        buf.append(line.rstrip())
        now = time.time()
        if now - last_edit > 1.5:
            _flush()
            last_edit = now
    proc.wait()
    buf.append(f"\n_exit code: {proc.returncode}_")
    _flush(force=True)


# ---------- end controller helpers ----------


# ---------- text / slash handlers ----------

@bot.message_handler(commands=["start", "menu"])
@auth
def cmd_start(m):
    bot.send_message(CHAT_ID, "*c2 telemetry online*\n"
                              "use the buttons or `/help` for commands.",
                     reply_markup=main_kb(), parse_mode="Markdown")


@bot.message_handler(commands=["help"])
@auth
def cmd_help(m):
    bot.send_message(CHAT_ID, (
        "_tip: every slash command also appears in Telegram's `/` menu._\n"
        "\n*hardware*\n"
        "  📊 status / /status — full snapshot (with 🔄 refresh)\n"
        "\n*ollama* — tap *🧠 ollama* for a button menu\n"
        "  /models — interactive browser (tap a model for load/unload/delete)\n"
        "  /loaded — currently in VRAM\n"
        "  /pull [name] — no arg → asks interactively\n"
        "  /rm <name>\n"
        "  /load <name>\n"
        "  /unload <name>\n"
        "  /keepalive [duration] — `5m` `1h` `24h` `-1` (pin) `0` (unload),\n"
        "    no arg → shows quick-pick buttons\n"
        "\n*nano bridge* — tap *🤖 nano bridge* for state-aware buttons\n"
        "  /nano_setup — one-time: install Playwright + Chromium + download Nano\n"
        "  /nano_update — upgrade bridge deps + Chromium + re-prime Nano\n"
        "  /nano_check — report install state (no changes)\n"
        "  /nano_start — start headless chrome bridge\n"
        "  /nano_stop\n"
        "  /nano_url — paste-ready URL for code-launcher\n"
        "  /nano_status — running state + prereqs\n"
        "\n*danger*\n"
        "  /kill or 💀 kill all — stop ollama + bridge, free VRAM (asks ✅/❌ first)\n"
    ), parse_mode="Markdown")


@bot.message_handler(commands=["status"])
@auth
def cmd_status(m):
    bot.send_message(CHAT_ID, status_text(), parse_mode="Markdown",
                     reply_markup=status_refresh_kb())


@bot.message_handler(func=lambda m: m.text == "📊 status")
@auth
def btn_status(m):
    cmd_status(m)


@bot.message_handler(func=lambda m: m.text == "🧠 ollama")
@auth
def btn_ollama(m):
    bot.send_message(CHAT_ID, "*ollama menu*", parse_mode="Markdown",
                     reply_markup=ollama_kb())


@bot.message_handler(func=lambda m: m.text == "🤖 nano bridge")
@auth
def btn_nano(m):
    bot.send_message(CHAT_ID, "*gemini nano bridge*", parse_mode="Markdown",
                     reply_markup=nano_kb())


@bot.message_handler(func=lambda m: m.text in ("ℹ️ help", "ℹ help"))
@auth
def btn_help(m):
    # Reply-keyboard help shortcut → reuse the /help handler.
    cmd_help(m)


@bot.message_handler(func=lambda m: m.text == "⚡ loaded")
@auth
def btn_loaded(m):
    cmd_loaded(m)


@bot.message_handler(commands=["kill"])
@auth
def cmd_kill(m):
    # Confirmation prompt before the destructive op.
    bot.send_message(CHAT_ID,
                     "Stop Ollama + nano bridge and clear VRAM?",
                     reply_markup=confirm_kb("c:kill"))


@bot.message_handler(func=lambda m: m.text == "💀 kill all")
@auth
def btn_kill(m):
    cmd_kill(m)


# ---------- ollama slash commands ----------

@bot.message_handler(commands=["models"])
@auth
def cmd_models(m):
    try:
        items = oc.list_models()
    except oc.OllamaError as e:
        bot.reply_to(m, f"❌ {e}")
        return
    if not items:
        bot.reply_to(m, "(no models installed; tap *⬇️ pull new model* or `/pull <name>`)",
                     parse_mode="Markdown", reply_markup=ollama_kb())
        return
    bot.reply_to(m, f"*installed models ({len(items)})* — tap one:",
                 parse_mode="Markdown", reply_markup=model_list_kb(items))


@bot.message_handler(commands=["loaded"])
@auth
def cmd_loaded(m):
    try:
        items = oc.list_running()
    except oc.OllamaError as e:
        bot.reply_to(m, f"❌ {e}")
        return
    if not items:
        bot.reply_to(m, "no models in VRAM right now.")
        return
    body = "\n".join("⚡ " + oc.humanize_running(x) for x in items)
    bot.reply_to(m, f"*loaded*\n{body}", parse_mode="Markdown")


def _arg(message) -> str:
    parts = shlex.split(message.text or "")
    return parts[1] if len(parts) > 1 else ""


def _do_pull(name: str, reply_to_msg=None) -> None:
    """Shared pull logic so /pull, the force-reply flow, and the
    inline 'pull new model' button all share one progress-updating
    implementation."""
    name = name.strip()
    if not name:
        bot.send_message(CHAT_ID, "❌ empty model name; cancelled.")
        return
    msg = bot.send_message(CHAT_ID, f"⬇️ pulling `{name}`...",
                           parse_mode="Markdown",
                           reply_to_message_id=reply_to_msg.message_id if reply_to_msg else None)
    try:
        for line in oc.pull(name):
            try:
                bot.edit_message_text(line, CHAT_ID, msg.message_id)
            except Exception:
                pass
    except oc.OllamaError as e:
        bot.send_message(CHAT_ID, f"❌ pull failed: {e}")
        return
    bot.send_message(CHAT_ID, f"✅ `{name}` ready", parse_mode="Markdown",
                     reply_markup=ollama_kb())


@bot.message_handler(commands=["pull"])
@auth
def cmd_pull(m):
    name = _arg(m)
    if not name:
        # No arg given — ask interactively. Telegram's ForceReply
        # places the cursor in the input box pre-quoted to the bot,
        # so the next message becomes the model name without the user
        # needing to remember "/pull".
        prompt = bot.send_message(
            CHAT_ID,
            "model to pull? (e.g. `gemma3:4b`, `qwen3:7b`, `phi3:mini`)",
            parse_mode="Markdown",
            reply_markup=ForceReply(selective=True))
        bot.register_next_step_handler(prompt, _pull_step)
        return
    _do_pull(name, reply_to_msg=m)


def _pull_step(m) -> None:
    if m.chat.id != CHAT_ID:
        return
    name = (m.text or "").strip()
    if not name or name.startswith("/"):
        bot.reply_to(m, "cancelled.")
        return
    _do_pull(name, reply_to_msg=m)


@bot.message_handler(commands=["rm", "delete"])
@auth
def cmd_rm(m):
    name = _arg(m)
    if not name:
        bot.reply_to(m, "usage: `/rm <model>`", parse_mode="Markdown")
        return
    try:
        oc.delete(name)
    except oc.OllamaError as e:
        bot.reply_to(m, f"❌ {e}")
        return
    bot.reply_to(m, f"🗑 removed `{name}`", parse_mode="Markdown")


@bot.message_handler(commands=["load"])
@auth
def cmd_load(m):
    name = _arg(m)
    if not name:
        bot.reply_to(m, "usage: `/load <model> [keep_alive]` — defaults to env OLLAMA_KEEP_ALIVE",
                     parse_mode="Markdown")
        return
    parts = shlex.split(m.text or "")
    ka = parts[2] if len(parts) > 2 else oc.DEFAULT_KEEP_ALIVE
    bot.reply_to(m, f"🔥 loading `{name}` (keep_alive={ka})...",
                 parse_mode="Markdown")
    try:
        oc.load(name, keep_alive=ka)
    except oc.OllamaError as e:
        bot.send_message(CHAT_ID, f"❌ load failed: {e}")
        return
    bot.send_message(CHAT_ID, f"⚡ loaded `{name}` (TTL `{ka}`)", parse_mode="Markdown")


@bot.message_handler(commands=["unload"])
@auth
def cmd_unload(m):
    name = _arg(m)
    if not name:
        bot.reply_to(m, "usage: `/unload <model>`", parse_mode="Markdown")
        return
    try:
        oc.unload(name)
    except oc.OllamaError as e:
        bot.reply_to(m, f"❌ {e}")
        return
    bot.reply_to(m, f"❄️ unloaded `{name}`", parse_mode="Markdown")


@bot.message_handler(commands=["keepalive"])
@auth
def cmd_keepalive(m):
    dur = _arg(m)
    if not dur:
        bot.reply_to(m,
                     f"*keepalive* — currently `{oc.DEFAULT_KEEP_ALIVE}`\n"
                     "applies to currently-loaded models + future loads.",
                     parse_mode="Markdown", reply_markup=keepalive_kb())
        return
    # Apply to every currently-loaded model.
    try:
        running = oc.list_running()
    except oc.OllamaError as e:
        bot.reply_to(m, f"❌ {e}")
        return
    oc.DEFAULT_KEEP_ALIVE = dur
    if not running:
        bot.reply_to(m, f"⏱  default keep_alive = `{dur}` (no loaded models to re-pin)",
                     parse_mode="Markdown")
        return
    errs = []
    for r in running:
        try:
            oc.load(r.name, keep_alive=dur)
        except oc.OllamaError as e:
            errs.append(f"{r.name}: {e}")
    if errs:
        bot.reply_to(m, "❌ partial:\n" + "\n".join(errs))
    else:
        bot.reply_to(m, f"⏱  keep_alive set to `{dur}` on {len(running)} model(s)",
                     parse_mode="Markdown")


# ---------- nano bridge slash commands ----------

@bot.message_handler(commands=["nano_start"])
@auth
def cmd_nano_start(m):
    bot.reply_to(m, "🤖 starting headless chrome + bridge...")
    bot.send_message(CHAT_ID, bridge.start(), parse_mode="Markdown")


@bot.message_handler(commands=["nano_stop"])
@auth
def cmd_nano_stop(m):
    bot.reply_to(m, bridge.stop())


@bot.message_handler(commands=["nano_url"])
@auth
def cmd_nano_url(m):
    if not bridge.running():
        bot.reply_to(m, "bridge not running. start it with /nano_start first.")
        return
    bot.reply_to(m, (
        "*code-launcher Ollama screen → endpoint:*\n"
        f"  `{bridge.url()}`\n"
        f"\n_(localhost-only: `{bridge.localhost_url()}`)_\n"
        f"\nmodel field: `gemini-nano`"
    ), parse_mode="Markdown")


@bot.message_handler(commands=["nano_status"])
@auth
def cmd_nano_status(m):
    parts = [bridge.status()]
    parts.append(f"\n*prereqs:* {'✓ ready' if _bridge_ready() else '❌ run /nano_setup'}")
    bot.reply_to(m, "\n".join(parts), parse_mode="Markdown")


@bot.message_handler(commands=["nano_setup"])
@auth
def cmd_nano_setup(m):
    _run_setup_streaming(m, "install")


@bot.message_handler(commands=["nano_update"])
@auth
def cmd_nano_update(m):
    _run_setup_streaming(m, "update")


@bot.message_handler(commands=["nano_check"])
@auth
def cmd_nano_check(m):
    _run_setup_streaming(m, "check")


# ---------- inline keyboard callbacks ----------

def _loaded_names() -> set:
    """Names currently in VRAM, used to render the right per-model
    action sheet (load vs unload)."""
    try:
        return {r.name for r in oc.list_running()}
    except oc.OllamaError:
        return set()


def _safe_edit(c, text: str, kb=None) -> None:
    """Edit the message the callback originated from, falling back to
    sending a new one if the edit is rejected (e.g. content identical).
    Keeps the chat tidy — clicking inline buttons mutates one panel
    rather than spawning a new bubble per click."""
    try:
        bot.edit_message_text(text, CHAT_ID, c.message.message_id,
                              parse_mode="Markdown", reply_markup=kb)
    except Exception:
        bot.send_message(CHAT_ID, text, parse_mode="Markdown", reply_markup=kb)


@bot.callback_query_handler(func=lambda c: True)
def on_cb(c):
    if c.message.chat.id != CHAT_ID:
        return
    data = c.data or ""

    # --- ollama top-level ---
    if data == "oll:back" or data == "oll:refresh":
        _safe_edit(c, "*ollama menu*", ollama_kb())

    elif data == "oll:browse":
        try:
            items = oc.list_models()
        except oc.OllamaError as e:
            _safe_edit(c, f"❌ {e}", ollama_kb())
        else:
            if not items:
                _safe_edit(c, "no models installed yet. tap *⬇️ pull new model*.",
                           ollama_kb())
            else:
                _safe_edit(c, f"*installed models ({len(items)})* — tap one:",
                           model_list_kb(items))

    elif data == "oll:loaded":
        try:
            items = oc.list_running()
            body = "\n".join("⚡ " + oc.humanize_running(x) for x in items) or "(none loaded)"
        except oc.OllamaError as e:
            body = f"❌ {e}"
        _safe_edit(c, f"*loaded*\n{body}", ollama_kb())

    elif data == "oll:pull":
        bot.answer_callback_query(c.id)
        prompt = bot.send_message(
            CHAT_ID,
            "model to pull? (e.g. `gemma3:4b`, `qwen3:7b`, `phi3:mini`)",
            parse_mode="Markdown",
            reply_markup=ForceReply(selective=True))
        bot.register_next_step_handler(prompt, _pull_step)
        return

    elif data == "oll:keepmenu":
        _safe_edit(c,
                   f"*keepalive* — currently `{oc.DEFAULT_KEEP_ALIVE}`\n"
                   "applies to currently-loaded models + future loads.",
                   keepalive_kb())

    # --- per-model action sheet ---
    elif data.startswith("m:open:"):
        name = data[len("m:open:"):]
        loaded = name in _loaded_names()
        _safe_edit(c, f"*{name}* {'· ⚡ loaded' if loaded else ''}",
                   model_actions_kb(name, loaded))

    elif data.startswith("m:load:"):
        name = data[len("m:load:"):]
        bot.answer_callback_query(c.id, f"loading {name}...")
        try:
            oc.load(name)
        except oc.OllamaError as e:
            _safe_edit(c, f"❌ load failed: {e}", model_actions_kb(name, False))
            return
        _safe_edit(c, f"⚡ `{name}` loaded (TTL `{oc.DEFAULT_KEEP_ALIVE}`)",
                   model_actions_kb(name, True))
        return

    elif data.startswith("m:unload:"):
        name = data[len("m:unload:"):]
        bot.answer_callback_query(c.id, f"unloading {name}...")
        try:
            oc.unload(name)
        except oc.OllamaError as e:
            _safe_edit(c, f"❌ unload failed: {e}", model_actions_kb(name, True))
            return
        _safe_edit(c, f"❄️ `{name}` unloaded", model_actions_kb(name, False))
        return

    elif data.startswith("m:delask:"):
        name = data[len("m:delask:"):]
        _safe_edit(c, f"delete *{name}*? this frees disk space and cannot be undone.",
                   confirm_kb(f"m:del:{name}"))

    elif data.startswith("m:del:"):
        name = data[len("m:del:"):]
        bot.answer_callback_query(c.id, f"deleting {name}...")
        try:
            oc.delete(name)
        except oc.OllamaError as e:
            _safe_edit(c, f"❌ delete failed: {e}", ollama_kb())
            return
        _safe_edit(c, f"🗑 `{name}` removed.", ollama_kb())
        return

    # --- keepalive quick-pick ---
    elif data.startswith("k:"):
        dur = data[2:]
        oc.DEFAULT_KEEP_ALIVE = dur
        try:
            running = oc.list_running()
        except oc.OllamaError:
            running = []
        errs = []
        for r in running:
            try:
                oc.load(r.name, keep_alive=dur)
            except oc.OllamaError as e:
                errs.append(f"{r.name}: {e}")
        suffix = f" · applied to {len(running)} loaded model(s)" if running else ""
        if errs:
            _safe_edit(c, f"⏱ keep_alive = `{dur}`{suffix}\n❌ partial:\n" + "\n".join(errs),
                       ollama_kb())
        else:
            _safe_edit(c, f"⏱ keep_alive = `{dur}`{suffix}", ollama_kb())
        return

    # --- generic confirmation outcomes ---
    elif data == "c:cancel":
        _safe_edit(c, "cancelled.", ollama_kb())
        return
    elif data == "c:kill":
        bot.answer_callback_query(c.id, "purging...")
        _safe_edit(c, "🧹 purging VRAM, stopping ollama + bridge...")
        purge()
        bot.send_message(CHAT_ID, "✅ purged.", reply_markup=main_kb())
        return

    # --- status refresh ---
    elif data == "status:refresh":
        _safe_edit(c, status_text(), status_refresh_kb())
        return

    # --- nano bridge ---
    elif data == "nano:start":
        bot.answer_callback_query(c.id, "starting bridge...")
        result = bridge.start()
        _safe_edit(c, result, nano_kb())
        return
    elif data == "nano:stop":
        bot.answer_callback_query(c.id, "stopping...")
        _safe_edit(c, bridge.stop(), nano_kb())
        return
    elif data == "nano:url":
        _safe_edit(c,
                   f"*endpoint:* `{bridge.url()}`\n"
                   f"_(localhost: `{bridge.localhost_url()}`)_\n"
                   f"*model:* `gemini-nano`",
                   nano_kb())
        return
    elif data == "nano:status":
        body = bridge.status() + f"\n*prereqs:* {'✓ ready' if _bridge_ready() else '❌ run /nano_setup'}"
        _safe_edit(c, body, nano_kb())
        return
    elif data == "nano:setup":
        bot.answer_callback_query(c.id)
        _run_setup_streaming(c.message, "install")
        return
    elif data == "nano:update":
        bot.answer_callback_query(c.id)
        _run_setup_streaming(c.message, "update")
        return
    elif data == "nano:check":
        bot.answer_callback_query(c.id)
        _run_setup_streaming(c.message, "check")
        return
    elif data == "nano:logs":
        tail = _tail(NANO_LOG, 20) or "(empty)"
        _safe_edit(c, f"*nano_bridge.log (last 20 lines)*\n```\n{tail}\n```",
                   nano_kb())
        return

    bot.answer_callback_query(c.id)


# ---------- main ----------

SLASH_COMMANDS = [
    BotCommand("start",         "show main menu"),
    BotCommand("help",          "all commands"),
    BotCommand("status",        "hardware + ollama + bridge snapshot"),
    BotCommand("models",        "browse / load / delete installed models"),
    BotCommand("loaded",        "models currently in VRAM"),
    BotCommand("pull",          "download a model (or just type the name)"),
    BotCommand("rm",            "delete a model"),
    BotCommand("load",          "pin a model into VRAM"),
    BotCommand("unload",        "flush a model from VRAM"),
    BotCommand("keepalive",     "set TTL for loaded models"),
    BotCommand("nano_setup",    "first-time Nano bridge setup"),
    BotCommand("nano_update",   "update Nano bridge"),
    BotCommand("nano_check",    "report Nano prereq state"),
    BotCommand("nano_start",    "start headless-Chrome bridge"),
    BotCommand("nano_stop",     "stop bridge"),
    BotCommand("nano_url",      "show bridge URL (paste into code-launcher)"),
    BotCommand("nano_status",   "bridge state + prereqs"),
    BotCommand("kill",          "purge all AI processes + bridge (DESTRUCTIVE)"),
]


def main() -> None:
    t = threading.Thread(target=alert_loop, name="alerts", daemon=True)
    t.start()
    # Register slash commands so Telegram's '/' autocomplete shows
    # them with descriptions. Setting "scope=default" applies it to
    # every chat the bot ever talks to — the auth decorator still
    # gates execution, this is just discovery.
    try:
        bot.set_my_commands(SLASH_COMMANDS)
        log.info("registered %d slash commands", len(SLASH_COMMANDS))
    except Exception:
        log.exception("set_my_commands failed (continuing)")
    try:
        bot.send_message(CHAT_ID,
                         "🛡 telemetry bot online · `/start` for menu, `/help` for commands.",
                         parse_mode="Markdown", reply_markup=main_kb())
    except Exception:
        log.exception("startup send failed (chat id wrong? token revoked?)")
    log.info("polling...")
    bot.infinity_polling(timeout=30, long_polling_timeout=20)


if __name__ == "__main__":
    # Silence noisy regex warnings from telebot's internals.
    _ = re
    main()
