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
list. The bot also calls Telegram's `setMyCommands` at startup, so
every command shows up in the chat's **`/` autocomplete menu** with a
short description — no need to memorise them.

## UX cheat sheet

- **Persistent reply keyboard** under every message: `📊 status`,
  `🧠 ollama`, `🤖 nano bridge`, `⚡ loaded`, `ℹ️ help`, `💀 kill all`.
- **`🧠 ollama`** opens an inline menu: *browse models · loaded · pull
  new model · keepalive · refresh*.
- **Browse models** lists every installed model as a button. Tap one
  → action sheet (`🔥 load` / `❄️ unload` / `🗑 delete`). `load` and
  `unload` are hidden when the action doesn't make sense.
- **`⬇️ pull new model`** opens a `ForceReply` prompt — type just the
  model name, no `/pull` prefix needed.
- **`⏱ keepalive`** shows quick-pick buttons (`5m` `1h` `24h`
  `📌 pin` `❄️ 0`). The pick applies to all currently-loaded models
  and becomes the new default.
- **Destructive actions** (`💀 kill all`, `🗑 delete`) always ask
  `✅ yes / ❌ no` first.
- **`📊 status`** ships with a `🔄 refresh` button — taps re-edit the
  same message instead of spawning new ones.
- **`🤖 nano bridge`** keyboard is state-aware: shows *setup* when
  prereqs are missing, *start* when ready, *stop / url / logs* when
  running.

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
| **Chat with a model** |                                                          |
| `/ask <text>`        | One-shot question to the current target (no memory)       |
| `/chat`              | Toggle "chat mode" — every non-command message goes to the current target with rolling history |
| `/use <model>`       | Switch chat target to an Ollama model (`/use llama3.1:8b`) |
| `/use_nano`          | Switch chat target back to Gemini Nano                    |
| `/reset`             | Clear chat history for the current target                 |
| _(also)_             | Tap **💬 chat** on any model's action sheet — sets target + flips chat mode in one tap |
| **Nano bridge**      |                                                           |
| `/nano_setup`        | One-time install: deps + Chromium + Nano model download   |
| `/nano_update`       | Upgrade bridge deps + Chromium, re-prime Nano             |
| `/nano_check`        | Report install state (no changes)                         |
| `/nano_start`        | Spawn headless Chrome, expose HTTP bridge                 |
| `/nano_stop`         | SIGTERM the bridge                                        |
| `/nano_url`          | The exact endpoint URL to paste into code-launcher        |
| `/nano_status`       | PID, uptime, log tail, prereq state                       |

---

## Gemini Nano bridge — first-time setup

The bridge runs headless Chrome and proxies requests through Chrome's
built-in `window.LanguageModel` (Gemini Nano). Chrome **does not
expose** the model as a downloadable file or over HTTP; you can only
reach it via a JavaScript API inside a browser tab. That's why a
headless Chrome stays running for the duration of the bridge.

> **You must have Google Chrome installed, not just Playwright's
> Chromium.** Gemini Nano is downloaded by Chrome's *Optimization
> Guide on-device model service*, which is a closed-source Google
> component absent from upstream Chromium. The bridge auto-detects
> `/usr/bin/google-chrome` (and a few common aliases); set
> `NANO_CHROME_EXECUTABLE` to override.
>
> ```sh
> # Debian/Ubuntu — add Google's apt repo + install:
> wget -qO- https://dl.google.com/linux/linux_signing_key.pub \
>   | sudo gpg --dearmor -o /usr/share/keyrings/google-chrome.gpg
> echo "deb [arch=amd64 signed-by=/usr/share/keyrings/google-chrome.gpg] \
>   http://dl.google.com/linux/chrome/deb/ stable main" \
>   | sudo tee /etc/apt/sources.list.d/google-chrome.list
> sudo apt update && sudo apt install -y google-chrome-stable
> ```
>
> Symptom when Chromium is used instead of Chrome:
> `Chrome LanguageModel API not available in this profile`.

### Recommended path: run `/nano_setup` from Telegram

The bot ships `nano_setup.py`, which handles the whole prep for you:

1. Installs the bridge's Python deps (`playwright`, `aiohttp`) via
   `pip install -r requirements-bridge.txt`.
2. Runs `playwright install chromium` (downloads ~150 MB of Chromium
   the first time; no-op on subsequent runs).
3. Creates `NANO_CHROME_PROFILE` if it doesn't exist.
4. Launches Chromium against that profile with
   `--enable-features=OptimizationGuideOnDeviceModel,PromptAPIForGeminiNano`
   — Chrome accepts those features via CLI, so **no manual
   `chrome://flags` clicking required**.
5. Calls `LanguageModel.create({monitor:…})` which triggers the Nano
   download and emits `downloadprogress` events; the bot streams them
   back as `↳ downloading Nano: 5% → 10% → … → 100%`.
6. Confirms `LanguageModel.availability() === "available"` and exits.

From Telegram:
```
/nano_setup
```
The bot edits a single message in place with the live output of every
step. First run on a clean box: ~5–10 min (Chromium + Nano downloads).
Subsequent runs: seconds.

- `/nano_check` — same script in **report-only** mode. No changes.
- `/nano_update` — upgrades the deps, re-runs `playwright install
  chromium --force`, re-primes Nano. Use after a Chrome major bump.

### Bridge runs non-headless by default

Chrome's on-device-model service refuses to initialise in pure
headless mode on most setups — even with a GPU available. So the
bridge default is now **`NANO_HEADLESS=0`** (non-headless), and the
installer wires `DISPLAY` + `XAUTHORITY` into the systemd unit so
the service can attach to your desktop session. You'll see a Chrome
window pop open when `/nano_start` runs; close it manually and the
bridge stops cleanly.

If you really want headless (true server, no display), set
`NANO_HEADLESS=1` in `bot/.env`. Expect Nano not to work in that
configuration — that's a Chrome limit, not a bot bug.

### When `/nano_setup` reports availability but `/nano_start` fails

Symptom: `Chrome LanguageModel API not available in this profile`,
even after `apt install google-chrome-stable`.

This happens because Chrome stable checks **per-profile flag state**
(stored in `<profile>/Local State`) *in addition to* the
`--enable-features=…` CLI flags. The installer now seeds that file
automatically with:

```json
{ "browser": { "enabled_labs_experiments": [
    "prompt-api-for-gemini-nano@1",
    "optimization-guide-on-device-model@2"
] } }
```

If you've upgraded from an older bot version, run `/nano_setup` once
more — that re-runs `ensure_profile` and seeds the flags.

If that *still* fails, the most reliable recovery is to open Chrome
**non-headless** against the profile once so the on-device-model
service can complete its first-run handshake:

```sh
google-chrome --user-data-dir=$NANO_CHROME_PROFILE chrome://on-device-internals
```

Wait for the model to reach **Available** in that page, close Chrome,
then `/nano_start` from Telegram. On a headless VPS without a real
GPU, Chrome's model service may refuse outright — see the next
section for the realistic alternative.

### Don't want to fight headless Chrome? Use Ollama from Telegram.

The same `/ask`, `/chat`, and `💬 chat` flow works against any Ollama
model — no Chrome, no Nano, no headless gymnastics:

```
/use llama3.1:8b   # set chat target
/chat              # turn chat mode on
hello              # send any text
```

`/use_nano` switches back when Nano is finally happy.

### Fallback: if `/nano_setup` can't trigger the download

On some boxes Chrome's on-device-model service refuses to fetch
headlessly (rare; reported when Chrome's policy gates trip). Manual
flow:

1. Open a **non-headless** Chrome with the profile dir:
   ```sh
   google-chrome --user-data-dir=/home/$USER/.config/code-launcher-nano
   ```
2. `chrome://flags/#prompt-api-for-gemini-nano` → Enabled.
3. `chrome://flags/#optimization-guide-on-device-model` → Enabled BypassPerfRequirement.
4. Relaunch. `chrome://on-device-internals` → wait for the model to
   finish (1.5–2 GB).
5. Sanity check in DevTools: `await LanguageModel.availability()` →
   `"available"`.
6. Close Chrome. `/nano_start` will use that profile.

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

Use the bundled installer — it's idempotent, so re-run any time to
change env vars, point at a new venv, or pick up the latest unit
template:

```sh
sudo ./install.sh
```

What it does:

1. **Detects an existing `vm-telemetry.service`** (override with
   `sudo SERVICE=my-bot ./install.sh`). If found, it reads the unit's
   current `EnvironmentFile` so every prompt pre-fills with the value
   that's running today — press Enter to keep, type to change.
2. Prompts for `TELEGRAM_TOKEN` (masked), `TELEGRAM_CHAT_ID`,
   `OLLAMA_URL`, keep-alive, the three alert thresholds, and the Nano
   bridge knobs.
3. Creates `.venv/` next to the bot, installs `requirements.txt`, and
   optionally `requirements-bridge.txt` + `playwright install chromium`
   for Gemini Nano (`BRIDGE=1` to force on, `BRIDGE=0` to skip).
4. Writes `bot/.env` with `chmod 600`.
5. Installs `/etc/systemd/system/vm-telemetry.service`,
   `daemon-reload`, `enable --now`, prints `status` + log location.

Useful follow-ups:

```sh
journalctl -u vm-telemetry -f                 # live logs
sudo systemctl restart vm-telemetry           # after manual .env edit
sudo systemctl edit vm-telemetry              # systemd drop-in override
systemctl cat vm-telemetry                    # the full effective unit
sudo NONINTERACTIVE=1 ./install.sh            # re-deploy unattended (keeps current values)
```

`/nano_start` from Telegram spawns the bridge as a child of the bot,
so `systemctl stop vm-telemetry` cleans up both.

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
