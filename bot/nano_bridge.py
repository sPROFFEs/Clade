"""Gemini Nano → OpenAI-compatible HTTP bridge.

Standalone subprocess the telemetry bot starts on demand. Runs a
headless Chrome (via Playwright) pinned to a persistent profile that
already has the Prompt API enabled and Gemini Nano downloaded
(see README "Prep Chrome for Nano"). Exposes one endpoint:

    POST  /v1/chat/completions    OpenAI-compatible (non-streaming)
    GET   /healthz                204 if Chrome window is alive

Each chat request:
  1. Reads `messages` from the request body (system + user history).
  2. Flattens them into a single prompt the way Chrome's
     window.LanguageModel.create({initialPrompts}).prompt() expects.
  3. Awaits the model's full reply, returns it as an OpenAI-shaped
     JSON response so any agent CLI that speaks OpenAI v1 can use it
     unchanged.

Why a subprocess and not a thread inside the bot:
  - Playwright runs its own asyncio loop; running it alongside the
    sync telebot polling is brittle.
  - The bridge can be killed cleanly with one SIGTERM when the user
    presses "stop bridge" in Telegram.

Config (env vars, all optional except where noted):
  NANO_BRIDGE_PORT       default 8765
  NANO_BRIDGE_HOST       default 0.0.0.0
  NANO_CHROME_PROFILE    REQUIRED — path to a Chrome user-data dir
  NANO_CHROME_EXECUTABLE override the chromium binary playwright launches
  NANO_HEADLESS          "1" for headless (default), "0" to show the window
  NANO_LOG_FILE          file path; if set, logs go there instead of stdout
"""

from __future__ import annotations

import asyncio
import json
import logging
import os
import shutil
import signal
import subprocess
import sys
import time
import uuid
from pathlib import Path
from typing import Optional


# chrome://flags entries the Prompt API depends on. Without these in
# the profile's Local State, --enable-features alone leaves
# window.LanguageModel undefined on stable Chrome.
ENABLED_LABS = [
    "prompt-api-for-gemini-nano@1",
    "optimization-guide-on-device-model@2",  # Enabled BypassPerfRequirement
]


def _seed_local_state(profile_dir: str) -> None:
    """Mirror nano_setup._seed_local_state so bridge restarts also
    self-heal a profile that lost its flag state."""
    p = Path(profile_dir) / "Local State"
    data: dict = {}
    if p.exists():
        try:
            data = json.loads(p.read_text())
        except (OSError, ValueError):
            data = {}
    browser = data.setdefault("browser", {})
    cur = browser.get("enabled_labs_experiments") or []
    changed = False
    for flag in ENABLED_LABS:
        if flag not in cur:
            cur.append(flag)
            changed = True
    if changed:
        browser["enabled_labs_experiments"] = cur
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(json.dumps(data, indent=2))

try:
    from aiohttp import web
except ImportError:
    sys.stderr.write("nano_bridge: aiohttp not installed. "
                     "pip install -r bot/requirements-bridge.txt\n")
    sys.exit(2)

try:
    from playwright.async_api import async_playwright, BrowserContext, Page
except ImportError:
    sys.stderr.write("nano_bridge: playwright not installed. "
                     "pip install -r bot/requirements-bridge.txt && playwright install chromium\n")
    sys.exit(2)


PORT = int(os.environ.get("NANO_BRIDGE_PORT", "8765"))
HOST = os.environ.get("NANO_BRIDGE_HOST", "0.0.0.0")
PROFILE_DIR = os.environ.get("NANO_CHROME_PROFILE", "")
CHROME_BIN = os.environ.get("NANO_CHROME_EXECUTABLE") or None
# Default to non-headless: Chrome's on-device-model service needs a
# display + GPU to initialise. On a desktop box this attaches to
# DISPLAY=:0; on a true server set NANO_HEADLESS=1 to force headless
# (Nano will probably not work in that mode — that's a Chrome limit).
HEADLESS = os.environ.get("NANO_HEADLESS", "0") != "0"
LOG_FILE = os.environ.get("NANO_LOG_FILE", "")


_xvfb_proc: Optional[subprocess.Popen] = None


def _display_works(disp: str) -> bool:
    """Return True if `xdpyinfo -display <disp>` exits 0 — i.e. an X
    server is actually listening there. We use this to decide whether
    DISPLAY=:0 is a real display or just a hopeful default."""
    if not shutil.which("xdpyinfo"):
        # No xdpyinfo means we can't probe; assume it works rather
        # than spawn Xvfb pre-emptively on a desktop box.
        return True
    try:
        return subprocess.call(["xdpyinfo", "-display", disp],
                                stdout=subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL,
                                timeout=3) == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def _start_xvfb() -> Optional[str]:
    """Spawn a virtual X server on the first free display number and
    return its DISPLAY string. Returns None if Xvfb isn't installed.
    The subprocess is tracked in _xvfb_proc and torn down on bridge
    stop."""
    global _xvfb_proc
    if not shutil.which("Xvfb"):
        return None
    # Pick a display number that's almost certainly free. :99 is the
    # classical CI choice.
    disp = os.environ.get("NANO_XVFB_DISPLAY", ":99")
    try:
        _xvfb_proc = subprocess.Popen(
            ["Xvfb", disp, "-screen", "0", "1280x720x24",
             "-nolisten", "tcp", "-ac"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except OSError:
        return None
    # Give it a moment to come up. Then probe to confirm.
    for _ in range(20):
        time.sleep(0.1)
        if _display_works(disp):
            return disp
    # Probe never returned True — clean up and give up.
    _xvfb_proc.terminate()
    _xvfb_proc = None
    return None


def _stop_xvfb() -> None:
    global _xvfb_proc
    if _xvfb_proc is not None:
        try:
            _xvfb_proc.terminate()
            _xvfb_proc.wait(timeout=5)
        except (OSError, subprocess.TimeoutExpired):
            try:
                _xvfb_proc.kill()
            except OSError:
                pass
        _xvfb_proc = None


def _auto_display() -> None:
    """Decide what Chrome should attach to:
       1. honour DISPLAY if it already points at a working X server,
       2. else try :0 + ~/.Xauthority (the desktop-session case),
       3. else fall back to spawning Xvfb (headless box, no GUI).
    Sets os.environ so the playwright child inherits it."""
    if HEADLESS:
        return

    cur = os.environ.get("DISPLAY", "")
    if cur and _display_works(cur):
        return

    # Try the desktop-session default before bringing up Xvfb.
    if _display_works(":0"):
        os.environ["DISPLAY"] = ":0"
        cand = os.path.join(os.path.expanduser("~"), ".Xauthority")
        if os.path.exists(cand) and not os.environ.get("XAUTHORITY"):
            os.environ["XAUTHORITY"] = cand
        return

    # No real display reachable — spawn Xvfb (virtual framebuffer).
    # Note: GPU-bound features (like Chrome's on-device-model
    # service) may STILL refuse to initialise without real GPU
    # access. That's a Chrome policy, not ours.
    disp = _start_xvfb()
    if disp:
        os.environ["DISPLAY"] = disp
        # XAUTHORITY isn't required for Xvfb because we passed -ac.


_auto_display()


# In-page driver script. Loaded once into about:blank; subsequent
# requests call window.__nanoPrompt(...) which manages a single
# long-lived LanguageModel session for the lifetime of the bridge.
DRIVER_JS = r"""
window.__nanoDiag = () => {
  // Snapshot every AI entrypoint Chrome might expose so we can tell
  // (a) which Chrome channel + version we're on, (b) whether the
  // feature flag took effect at all, (c) what failure mode we're in.
  const ua = navigator.userAgent;
  const ver = (navigator.userAgentData && navigator.userAgentData.brands || [])
    .map(b => b.brand + " " + b.version).join(", ");
  const has = (k) => typeof self[k] !== "undefined";
  return {
    userAgent: ua,
    brands: ver,
    LanguageModel: has("LanguageModel"),
    ai: has("ai"),
    chromeAi: !!(self.chrome && self.chrome.ai),
    chromeAiOriginTrial: !!(self.chrome && self.chrome.aiOriginTrial),
    Writer: has("Writer"),
    Summarizer: has("Summarizer"),
    Translator: has("Translator"),
  };
};

window.__nanoReady = (async () => {
  if (!('LanguageModel' in self)) {
    const diag = window.__nanoDiag();
    throw new Error(
      "Chrome LanguageModel API not exposed.\n" +
      "  diagnostic: " + JSON.stringify(diag, null, 2) + "\n" +
      "  meaning: Chrome launched fine but refused to register the\n" +
      "  Prompt API in this context. Common cause on VMs without GPU\n" +
      "  passthrough: Chrome's perf-check fails so the on-device-model\n" +
      "  service stays disabled. Try Chrome Dev (apt install\n" +
      "  google-chrome-unstable) or use Ollama from Telegram.");
  }
  const avail = await LanguageModel.availability();
  if (avail === "no") {
    throw new Error("LanguageModel.availability() returned 'no' — model not usable on this device.");
  }
  // "downloadable" / "downloading" / "available"; the model may
  // still be fetching on first run.
  return avail;
})();

window.__nanoPrompt = async function(systemPrompt, history, userText) {
  // Each request gets its own session so system+history live for this
  // turn only. Cheap because Nano is local.
  await window.__nanoReady;
  const opts = {};
  if (systemPrompt) {
    opts.initialPrompts = [{ role: "system", content: systemPrompt }];
  }
  if (Array.isArray(history) && history.length) {
    opts.initialPrompts = (opts.initialPrompts || []).concat(history);
  }
  const session = await LanguageModel.create(opts);
  try {
    const result = await session.prompt(userText);
    return { ok: true, text: result };
  } finally {
    session.destroy && session.destroy();
  }
};
"""


def _setup_logging() -> logging.Logger:
    log = logging.getLogger("nano_bridge")
    log.setLevel(logging.INFO)
    fmt = logging.Formatter("%(asctime)s %(levelname)s %(message)s",
                            datefmt="%H:%M:%S")
    if LOG_FILE:
        h: logging.Handler = logging.FileHandler(LOG_FILE)
    else:
        h = logging.StreamHandler(sys.stdout)
    h.setFormatter(fmt)
    log.addHandler(h)
    return log


log = _setup_logging()


class Bridge:
    """Holds the long-lived browser context + page that wraps Nano."""

    def __init__(self) -> None:
        self.pw = None
        self.ctx: Optional[BrowserContext] = None
        self.page: Optional[Page] = None
        self._lock = asyncio.Lock()  # serialise prompt() calls per page

    async def start(self) -> None:
        if not PROFILE_DIR:
            raise RuntimeError("NANO_CHROME_PROFILE env var is required")
        # Pre-flight the profile dir — if it doesn't exist (or is owned
        # by another user, common when the bot ran as root once) the
        # chromium launch hangs with no useful error.
        prof = os.path.abspath(PROFILE_DIR)
        if not os.path.isdir(prof):
            try:
                os.makedirs(prof, exist_ok=True)
                log.info("created profile dir %s", prof)
            except OSError as e:
                raise RuntimeError(f"profile dir {prof} unusable: {e}") from e
        if not os.access(prof, os.W_OK):
            raise RuntimeError(
                f"profile dir {prof} not writable by uid={os.geteuid()} — "
                f"it may have been created as root. fix with: "
                f"sudo chown -R $USER {prof}")

        self.pw = await async_playwright().start()
        # Resolve the chrome binary. Playwright bundles upstream
        # *Chromium*, which does NOT ship Google's on-device-model
        # service — that's a closed-source component only present in
        # Google Chrome / Chrome Beta / Chrome Dev. If Nano is the
        # goal we need a real Chrome binary; we try to discover one
        # automatically and fall back to Playwright's Chromium only
        # if the operator explicitly forces it.
        resolved = CHROME_BIN
        if not resolved:
            for candidate in (
                "/usr/bin/google-chrome",
                "/usr/bin/google-chrome-stable",
                "/usr/bin/google-chrome-beta",
                "/usr/bin/google-chrome-unstable",
                "/opt/google/chrome/chrome",
                "/snap/bin/google-chrome",
                "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
            ):
                if os.path.exists(candidate):
                    resolved = candidate
                    log.info("auto-detected Google Chrome at %s", resolved)
                    break
        if not resolved:
            resolved = self.pw.chromium.executable_path
            log.warning(
                "no Google Chrome found — falling back to Playwright's "
                "Chromium at %s. Gemini Nano almost certainly will NOT "
                "work because the on-device-model service is Chrome-only. "
                "apt install google-chrome-stable, or set "
                "NANO_CHROME_EXECUTABLE=/path/to/google-chrome.", resolved)
        if not resolved or not os.path.exists(resolved):
            raise RuntimeError(
                f"chrome binary not found ({resolved!r}). install Google "
                f"Chrome (apt install google-chrome-stable) or set "
                f"NANO_CHROME_EXECUTABLE.")
        # Self-heal the profile: re-seed Local State in case Chrome
        # rewrote it without our flags between runs.
        _seed_local_state(prof)
        log.info("launching chrome (profile=%s, headless=%s, exec=%s, "
                 "DISPLAY=%s, XAUTHORITY=%s)",
                 prof, HEADLESS, resolved,
                 os.environ.get("DISPLAY") or "(unset)",
                 os.environ.get("XAUTHORITY") or "(unset)")
        if not HEADLESS and not os.environ.get("DISPLAY"):
            raise RuntimeError(
                "non-headless Chrome but no DISPLAY env var. The systemd "
                "service needs `Environment=DISPLAY=:0` and "
                "`Environment=XAUTHORITY=/home/<user>/.Xauthority`. "
                "Re-run sudo ./install.sh — it will detect a desktop "
                "session and add these to the unit.")

        # Persistent context keeps cookies, flags state, and the
        # downloaded Nano model between bridge restarts.
        launch_args = [
            "--no-sandbox",
            "--disable-dev-shm-usage",
            # The Prompt API is gated; these features + blink features
            # + the on-device-model umbrella unlock it. The perf-check
            # bypass and swiftshader keep it alive on virtualised
            # hosts where Chrome's GPU check would otherwise refuse.
            "--enable-features=OptimizationGuideOnDeviceModel,"
            "PromptAPIForGeminiNano,BuiltInAIAPI",
            "--enable-blink-features=AIPromptAPI",
            "--optimization-guide-on-device-model-execution-validation",
            "--use-gl=swiftshader",
            "--enable-unsafe-swiftshader",
        ]
        try:
            self.ctx = await self.pw.chromium.launch_persistent_context(
                prof,
                headless=HEADLESS,
                executable_path=resolved,
                args=launch_args,
            )
        except Exception as e:
            # Surface the full chain so the user sees WHY launch
            # failed. Common causes: chromium missing system deps
            # (apt install libnss3 libxss1 libasound2t64), profile
            # already open in another Chrome, port-of-display weirdness.
            log.exception("chromium launch failed")
            raise RuntimeError(
                f"chromium launch failed: {e!r}. check the log above; "
                f"often it's missing system libs — try: "
                f"playwright install-deps chromium") from e
        log.info("chromium context up")

        self.page = await self.ctx.new_page()
        await self.page.goto("about:blank")
        # Inject the driver and surface availability so a bad profile
        # blows up here, not on the first user prompt.
        await self.page.add_script_tag(content=DRIVER_JS)
        try:
            avail = await self.page.evaluate("window.__nanoReady")
            log.info("LanguageModel.availability = %s", avail)
        except Exception as e:
            await self.stop()
            raise RuntimeError(
                "Nano not usable in this profile.\n"
                f"  underlying error: {e}\n"
                "  things to try (in order):\n"
                "    1. /nano_setup so Local State is re-seeded with the flags.\n"
                "    2. Run Chrome non-headless once on this profile so it can\n"
                "       finish the on-device-model download:\n"
                "         google-chrome --user-data-dir=" + PROFILE_DIR + " \\\n"
                "           chrome://on-device-internals\n"
                "       Wait for the model to reach 'Available', close Chrome,\n"
                "       then /nano_start.\n"
                "    3. If your box has no GPU (typical VPS) Chrome's on-device\n"
                "       model service may refuse no matter what. In that case\n"
                "       use Ollama models from Telegram instead: /use <model>"
            ) from e

    async def stop(self) -> None:
        try:
            if self.ctx:
                await self.ctx.close()
        finally:
            if self.pw:
                await self.pw.stop()
            self.ctx = None
            self.page = None
            # Tear down our Xvfb if we started one.
            _stop_xvfb()

    async def prompt(self, system: str, history: list[dict], user_text: str) -> str:
        if not self.page:
            raise RuntimeError("bridge not started")
        async with self._lock:
            try:
                result = await self.page.evaluate(
                    "args => window.__nanoPrompt(args.s, args.h, args.u)",
                    {"s": system, "h": history, "u": user_text},
                )
            except Exception as e:
                raise RuntimeError(f"nano prompt failed: {e}") from e
        if not result or not result.get("ok"):
            raise RuntimeError(f"nano prompt error: {result}")
        return result["text"]


bridge = Bridge()


# ---------- HTTP handlers ----------

def _split_messages(messages: list[dict]) -> tuple[str, list[dict], str]:
    """Flatten OpenAI 'messages' into (system, history, user_text)."""
    system_parts: list[str] = []
    history: list[dict] = []
    user_text = ""
    for m in messages:
        role = m.get("role", "user")
        content = m.get("content", "")
        if isinstance(content, list):
            # Chat-completions content can be a list of parts; join the
            # text fragments (drop images — Nano text mode here).
            content = "".join(p.get("text", "") for p in content
                              if isinstance(p, dict) and p.get("type") == "text")
        if role == "system":
            system_parts.append(content)
        else:
            history.append({"role": role, "content": content})
    # OpenAI convention: the LAST user message is the "current turn";
    # the rest is history. The Prompt API treats initialPrompts as
    # context, then prompt() as the current question.
    if history and history[-1]["role"] == "user":
        user_text = history[-1]["content"]
        history = history[:-1]
    return "\n\n".join(system_parts), history, user_text


async def handle_chat(req: web.Request) -> web.Response:
    try:
        body = await req.json()
    except ValueError:
        return web.json_response({"error": "invalid json"}, status=400)
    messages = body.get("messages") or []
    if not messages:
        return web.json_response({"error": "messages[] required"}, status=400)

    system, history, user_text = _split_messages(messages)
    if not user_text:
        return web.json_response({"error": "last message must be from user"}, status=400)

    t0 = time.time()
    try:
        reply = await bridge.prompt(system, history, user_text)
    except Exception as e:
        log.exception("prompt failed")
        return web.json_response({"error": str(e)}, status=502)
    log.info("chat reply: %d chars in %.2fs", len(reply), time.time() - t0)

    return web.json_response({
        "id": "chatcmpl-" + uuid.uuid4().hex[:24],
        "object": "chat.completion",
        "created": int(time.time()),
        "model": body.get("model", "gemini-nano"),
        "choices": [{
            "index": 0,
            "finish_reason": "stop",
            "message": {"role": "assistant", "content": reply},
        }],
        "usage": {
            # Nano doesn't report token counts via the JS API. Provide
            # rough chars-as-tokens estimates so clients don't crash.
            "prompt_tokens": sum(len(m.get("content", "")) for m in messages) // 4,
            "completion_tokens": len(reply) // 4,
            "total_tokens": (sum(len(m.get("content", "")) for m in messages) + len(reply)) // 4,
        },
    })


async def handle_health(_req: web.Request) -> web.Response:
    if bridge.page is None:
        return web.Response(status=503, text="not ready")
    return web.Response(status=204)


async def handle_models(_req: web.Request) -> web.Response:
    # Some clients probe /v1/models before sending /v1/chat/completions.
    # Return the synthetic name they're expecting.
    return web.json_response({
        "object": "list",
        "data": [{"id": "gemini-nano", "object": "model", "owned_by": "chrome"}],
    })


def build_app() -> web.Application:
    app = web.Application()
    app.router.add_post("/v1/chat/completions", handle_chat)
    app.router.add_get("/v1/models", handle_models)
    app.router.add_get("/healthz", handle_health)
    return app


async def main() -> None:
    # Graceful shutdown so the Chrome context is closed cleanly.
    stopping = asyncio.Event()

    def _on_signal(*_: object) -> None:
        log.info("signal received, shutting down")
        stopping.set()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, _on_signal)
        except NotImplementedError:
            # Windows lacks add_signal_handler; rely on KeyboardInterrupt
            pass

    try:
        await bridge.start()
    except Exception as e:
        # Bot polls the log file for the "listening on" line and treats
        # the absence as a failure. Log the underlying error here so it
        # ends up in NANO_LOG_FILE where the user can see it via
        # /nano_status (or the 📜 logs button).
        log.error("bridge.start failed: %s", e)
        raise
    app = build_app()
    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, HOST, PORT)
    await site.start()
    log.info("listening on http://%s:%d/v1", HOST, PORT)

    try:
        await stopping.wait()
    except KeyboardInterrupt:
        pass
    finally:
        await runner.cleanup()
        await bridge.stop()
        log.info("bye")


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
