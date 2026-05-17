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
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    ReplyKeyboardMarkup,
    KeyboardButton,
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
    kb = ReplyKeyboardMarkup(resize_keyboard=True, row_width=2)
    kb.add(KeyboardButton("📊 status"),
           KeyboardButton("🧠 ollama"),
           KeyboardButton("🤖 nano bridge"),
           KeyboardButton("💀 kill all"))
    return kb


def ollama_kb() -> InlineKeyboardMarkup:
    kb = InlineKeyboardMarkup(row_width=2)
    kb.add(InlineKeyboardButton("📦 list", callback_data="oll:list"),
           InlineKeyboardButton("⚡ loaded", callback_data="oll:loaded"))
    kb.add(InlineKeyboardButton("⬇️  pull (type /pull <name>)", callback_data="oll:hint_pull"),
           InlineKeyboardButton("🗑  rm  (type /rm <name>)", callback_data="oll:hint_rm"))
    kb.add(InlineKeyboardButton("🔥 load (type /load <name>)", callback_data="oll:hint_load"),
           InlineKeyboardButton("❄️  unload (type /unload <name>)", callback_data="oll:hint_unload"))
    kb.add(InlineKeyboardButton("⏱  keepalive (type /keepalive <dur>)", callback_data="oll:hint_keep"))
    return kb


def nano_kb() -> InlineKeyboardMarkup:
    kb = InlineKeyboardMarkup(row_width=2)
    if bridge.running():
        kb.add(InlineKeyboardButton("⏹  stop bridge", callback_data="nano:stop"),
               InlineKeyboardButton("🔗 url", callback_data="nano:url"))
    else:
        kb.add(InlineKeyboardButton("▶️  start bridge", callback_data="nano:start"))
    kb.add(InlineKeyboardButton("📋 status", callback_data="nano:status"))
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
        "*hardware*\n"
        "  📊 status — full snapshot\n"
        "\n*ollama*\n"
        "  /models — list installed\n"
        "  /loaded — currently in VRAM\n"
        "  /pull <name>\n"
        "  /rm <name>\n"
        "  /load <name>\n"
        "  /unload <name>\n"
        "  /keepalive <duration> — `5m` `1h` `24h` `-1` (pin) `0` (unload)\n"
        "\n*nano bridge*\n"
        "  /nano_start — start headless chrome bridge\n"
        "  /nano_stop\n"
        "  /nano_url — paste-ready URL for code-launcher\n"
        "  /nano_status\n"
        "\n*danger*\n"
        "  💀 kill all — stop ollama + bridge, free VRAM\n"
    ), parse_mode="Markdown")


@bot.message_handler(func=lambda m: m.text == "📊 status")
@auth
def btn_status(m):
    bot.send_message(CHAT_ID, status_text(), parse_mode="Markdown")


@bot.message_handler(func=lambda m: m.text == "🧠 ollama")
@auth
def btn_ollama(m):
    bot.send_message(CHAT_ID, "*ollama menu* — pick or use `/help`",
                     parse_mode="Markdown", reply_markup=ollama_kb())


@bot.message_handler(func=lambda m: m.text == "🤖 nano bridge")
@auth
def btn_nano(m):
    bot.send_message(CHAT_ID, "*gemini nano bridge*", parse_mode="Markdown",
                     reply_markup=nano_kb())


@bot.message_handler(func=lambda m: m.text == "💀 kill all")
@auth
def btn_kill(m):
    bot.send_message(CHAT_ID, "🧹 purging VRAM, stopping ollama + bridge...")
    purge()
    bot.send_message(CHAT_ID, "✅ purged.")


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
        bot.reply_to(m, "(no models installed; pull one with `/pull <name>`)",
                     parse_mode="Markdown")
        return
    body = "\n".join("• " + oc.humanize(x) for x in items)
    bot.reply_to(m, f"*models ({len(items)})*\n{body}", parse_mode="Markdown")


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


@bot.message_handler(commands=["pull"])
@auth
def cmd_pull(m):
    name = _arg(m)
    if not name:
        bot.reply_to(m, "usage: `/pull <model>` — e.g. `/pull gemma3:4b`",
                     parse_mode="Markdown")
        return
    msg = bot.reply_to(m, f"⬇️  pulling `{name}`...", parse_mode="Markdown")
    try:
        for line in oc.pull(name):
            # Edit the same message rather than spamming new ones.
            try:
                bot.edit_message_text(line, CHAT_ID, msg.message_id)
            except Exception:
                pass
    except oc.OllamaError as e:
        bot.send_message(CHAT_ID, f"❌ pull failed: {e}")
        return
    bot.send_message(CHAT_ID, f"✅ `{name}` ready", parse_mode="Markdown")


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
        bot.reply_to(m, (
            f"current default keep_alive: `{oc.DEFAULT_KEEP_ALIVE}`\n"
            "usage: `/keepalive <duration>` — `5m`, `1h`, `24h`, `-1` (pin), `0` (immediate unload)\n"
            "applies to subsequent `/load` calls; affects new requests until the next change."
        ), parse_mode="Markdown")
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
    bot.reply_to(m, bridge.status(), parse_mode="Markdown")


# ---------- inline keyboard callbacks ----------

@bot.callback_query_handler(func=lambda c: True)
def on_cb(c):
    if c.message.chat.id != CHAT_ID:
        return
    if c.data == "oll:list":
        try:
            items = oc.list_models()
            body = "\n".join("• " + oc.humanize(x) for x in items) or "(none)"
        except oc.OllamaError as e:
            body = f"❌ {e}"
        bot.send_message(CHAT_ID, f"*models*\n{body}", parse_mode="Markdown")
    elif c.data == "oll:loaded":
        try:
            items = oc.list_running()
            body = "\n".join("⚡ " + oc.humanize_running(x) for x in items) or "(none loaded)"
        except oc.OllamaError as e:
            body = f"❌ {e}"
        bot.send_message(CHAT_ID, f"*loaded*\n{body}", parse_mode="Markdown")
    elif c.data == "oll:hint_pull":
        bot.send_message(CHAT_ID, "type:  `/pull <model-name>`  e.g. `/pull gemma3:4b`",
                         parse_mode="Markdown")
    elif c.data == "oll:hint_rm":
        bot.send_message(CHAT_ID, "type:  `/rm <model-name>`", parse_mode="Markdown")
    elif c.data == "oll:hint_load":
        bot.send_message(CHAT_ID, "type:  `/load <model-name> [keep_alive]`",
                         parse_mode="Markdown")
    elif c.data == "oll:hint_unload":
        bot.send_message(CHAT_ID, "type:  `/unload <model-name>`", parse_mode="Markdown")
    elif c.data == "oll:hint_keep":
        bot.send_message(CHAT_ID, "type:  `/keepalive <duration>` — `5m`, `1h`, `24h`, `-1`, `0`",
                         parse_mode="Markdown")
    elif c.data == "nano:start":
        bot.send_message(CHAT_ID, "starting bridge...")
        bot.send_message(CHAT_ID, bridge.start(), parse_mode="Markdown")
    elif c.data == "nano:stop":
        bot.send_message(CHAT_ID, bridge.stop())
    elif c.data == "nano:url":
        bot.send_message(CHAT_ID, (
            "*endpoint:* " + bridge.url() + "\n"
            "*model:* `gemini-nano`"), parse_mode="Markdown")
    elif c.data == "nano:status":
        bot.send_message(CHAT_ID, bridge.status(), parse_mode="Markdown")
    bot.answer_callback_query(c.id)


# ---------- main ----------

def main() -> None:
    t = threading.Thread(target=alert_loop, name="alerts", daemon=True)
    t.start()
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
