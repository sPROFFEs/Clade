# telemetry bot

Telegram-driven C2 for an Ollama / Gemini-Nano box. Replaces the
older `telemetry_bot.txt`. Three responsibilities:

1. **Telemetry + alerts** — hardware snapshot on demand, plus a
   background daemon that pings on temp / VRAM / disk thresholds.
2. **Ollama model lifecycle** — list, pull, delete, load/unload, set
   keep-alive (a.k.a. "lifetime in VRAM"), all from chat.
3. **Gemini Nano → OpenAI-compatible bridge** (optional) — on demand,
   spin up a headless Chrome via Playwright that exposes Chrome's
   built-in Gemini Nano model as a `POST /v1/chat/completions`
   endpoint. The bot reports the exact URL to paste into
   code-launcher's Ollama screen.

```
~/code-launcher/skills-project/bot/
├── telemetry_bot.py         main bot (telebot polling loop)
├── ollama_client.py         thin Ollama HTTP wrapper
├── nano_bridge.py           Chrome + aiohttp subprocess
├── requirements.txt         pyTelegramBotAPI + psutil + requests
├── requirements-bridge.txt  playwright + aiohttp (only for nano)
└── config.example.env       template for env vars
```

## Install

```sh
cd bot
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
# Only if you want the Nano bridge:
pip install -r requirements-bridge.txt
playwright install chromium     # downloads a managed Chromium
```

## Configure

Copy `config.example.env` to `.env` (or just export the vars):

```sh
export TELEGRAM_TOKEN=...
export TELEGRAM_CHAT_ID=...
export OLLAMA_URL=http://127.0.0.1:11434      # default
export NANO_BRIDGE_PORT=8765                  # default
export NANO_CHROME_PROFILE=/home/$USER/.config/code-launcher-nano   # required for bridge
```

## Run

```sh
python telemetry_bot.py
```

It sends `🛡 telemetry bot online` to your chat and starts the alert
daemon. Use `/start` for the keyboard, `/help` for the full command
list.

---

## Telegram commands

| Cmd                  | Effect                                                    |
|----------------------|-----------------------------------------------------------|
| `/start` `/menu`     | Show the reply keyboard                                   |
| `/help`              | Full command list                                         |
| `📊 status`          | CPU / RAM / disk / net / per-GPU / Ollama-loaded / bridge |
| `💀 kill all`         | Stop Ollama + bridge, free VRAM                           |
| **Ollama**           |                                                           |
| `/models`            | List installed models with size + params                  |
| `/loaded`            | Models currently in VRAM (with TTL)                       |
| `/pull <name>`       | Download a model (e.g. `/pull gemma3:4b`); progress in-line |
| `/rm <name>`         | Delete a model from disk                                  |
| `/load <name> [TTL]` | Pin into VRAM, default TTL = `OLLAMA_KEEP_ALIVE` env      |
| `/unload <name>`     | Flush from VRAM right away                                |
| `/keepalive <dur>`   | `5m` `1h` `24h` `-1` (pin) `0` (unload). Applied to every currently-loaded model and used as the new default for `/load` |
| **Nano bridge**      |                                                           |
| `/nano_start`        | Spawn headless Chrome, expose HTTP bridge                 |
| `/nano_stop`         | SIGTERM the bridge                                        |
| `/nano_url`          | The exact endpoint URL to paste into code-launcher        |
| `/nano_status`       | PID, uptime, log tail                                     |

---

## Gemini Nano bridge — first-time setup

The bridge runs headless Chrome and proxies requests through Chrome's
built-in `window.LanguageModel` (Gemini Nano). Chrome **does not
expose** the model as a downloadable file or over HTTP; you can only
reach it via a JavaScript API inside a browser tab. That's why a
headless Chrome stays running for the duration of the bridge.

**Before `/nano_start` will work, the chosen Chrome profile must have
the Prompt API enabled and Gemini Nano downloaded.** Headless mode
will not download the model from scratch. Do this once, manually:

### Prep Chrome for Nano

1. Pick a profile path you can dedicate to the bridge. The
   `config.example.env` suggests `~/.config/code-launcher-nano`.
2. On the box where the bridge will run, open a **non-headless** Chrome
   using that profile dir:

   ```sh
   google-chrome --user-data-dir=/home/$USER/.config/code-launcher-nano
   ```

   (Use `chromium` if that's what you have. On macOS:
   `/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome
   --user-data-dir=...`.)

3. Inside that Chrome:
   - Go to `chrome://flags/#prompt-api-for-gemini-nano` → set to
     **Enabled**.
   - Go to `chrome://flags/#optimization-guide-on-device-model` → set
     to **Enabled BypassPerfRequirement**.
   - Click **Relaunch** when prompted (this keeps the flags inside
     the dedicated profile, not your daily one).
4. Once relaunched, open `chrome://on-device-internals`. Check that
   **Foundational Model Information** shows a model with non-zero
   size. If it says "Downloading", leave Chrome open until it
   finishes (can be 1.5–2 GB; takes minutes on a fast link).
5. Optional sanity check: open DevTools console on any page, run
   `await LanguageModel.availability()`. Should return `"available"`.
6. Close Chrome.

The profile is now ready. `/nano_start` from Telegram will launch
headless Chrome pointed at it.

### What the bridge exposes

```
POST  http://<server-ip>:8765/v1/chat/completions   OpenAI-shaped, non-streaming
GET   http://<server-ip>:8765/v1/models             reports model id "gemini-nano"
GET   http://<server-ip>:8765/healthz               204 when Chrome window is alive
```

After `/nano_start`, the bot replies with the exact URL it discovered
for the host, e.g.:

```
✅ bridge up

paste into code-launcher Ollama screen:
  endpoint:  http://192.168.100.242:8765
  model:     gemini-nano
```

(The endpoint the launcher wants is the host:port — the `/v1`
suffix is appended by the launcher when it talks to it.)

In code-launcher: `o` on a chat → endpoint = the URL above → model =
`gemini-nano` → tick `claude` (and any other agent you want) → apply.
That chat then routes through your headless Chrome → Gemini Nano.

### Caveats

- **Single user, single tab.** Per request the bridge creates a fresh
  `LanguageModel` session, runs `prompt()`, destroys it. Parallel
  requests are serialised through one `asyncio.Lock`. Don't expect
  Ollama-level concurrency.
- **No streaming.** The bridge waits for the full reply, then returns
  it as one `chat.completion` response. Most agents handle this fine.
- **No tool calls.** Gemini Nano via the Prompt API doesn't natively
  do OpenAI-style tool calls. Agents that depend on tool use
  (function calling) will get plain text only.
- **Profile lock.** While `/nano_start` is running, you can't open
  Chrome on the same profile from elsewhere — Chromium serialises
  per-profile.
- **Resource cost.** Headless Chrome + Nano sits in RAM the whole
  time. `/nano_stop` (or `💀 kill all`) gets it back.

---

## Alert thresholds

| Var          | Default | What it pings on            |
|--------------|---------|-----------------------------|
| `TEMP_CRIT`  | `85`    | Any GPU's temperature in °C |
| `VRAM_CRIT`  | `95`    | Any GPU's VRAM percentage   |
| `DISK_CRIT`  | `90`    | `/` partition fill          |

Alert fires, then sleeps 5 min before checking again to avoid spam.

---

## Running as a systemd service

```ini
# /etc/systemd/system/telemetry-bot.service
[Unit]
Description=Telegram telemetry bot for ollama + nano bridge
After=network.target ollama.service

[Service]
Type=simple
User=youruser
WorkingDirectory=/home/youruser/code-launcher/skills-project/bot
EnvironmentFile=/home/youruser/code-launcher/skills-project/bot/.env
ExecStart=/home/youruser/code-launcher/skills-project/bot/.venv/bin/python telemetry_bot.py
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now telemetry-bot
journalctl -u telemetry-bot -f
```

`/nano_start` from Telegram will spawn the bridge as a child of the
bot, so `systemctl stop telemetry-bot` cleans up both.

---

## Migrating from the old `telemetry_bot.txt`

What was removed:
- **ComfyUI launch button** — drop it; if you still need ComfyUI,
  start it from systemd or a separate script.
- **`sudo pkill -9 -f 'main.py'`** — was matching ComfyUI; gone with
  the ComfyUI feature.
- Hardcoded TOKEN / CHAT_ID at the top of the file — replaced by env
  vars so you can ship the code without leaking secrets.

What's preserved:
- Auth pattern (single allowed chat id).
- The `sudo systemctl stop ollama` + `pkill -9 ollama` + sleep
  sequence in `purge()`.
- Background critical-alert thread, same thresholds (now overridable
  via env).
