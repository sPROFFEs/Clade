"""First-time / update setup for the Gemini Nano bridge.

Run from the bot via /nano_setup, /nano_update, /nano_status — or by
hand:

    python3 nano_setup.py check      # report state, exit 0 if ready
    python3 nano_setup.py install    # full first-time setup
    python3 nano_setup.py update     # update deps + chromium + retry download

Phases:
  1. Ensure the Python bridge deps are importable (playwright +
     aiohttp). If not, pip-install them from requirements-bridge.txt.
  2. Ensure `playwright install chromium` has happened (downloads ~150MB
     of Chromium the first time).
  3. Ensure NANO_CHROME_PROFILE exists.
  4. Launch Chromium with the on-device-model + prompt-api flags,
     monitor LanguageModel.create()'s downloadprogress events until
     the model is "available". Then exit. Subsequent bridge launches
     reuse the profile and skip the download.

Why this avoids the manual chrome://flags step:
  Chrome accepts the same features as command-line flags
  (--enable-features=…), so we don't actually need the chrome://flags
  UI flip. The first download still has to happen against a profile
  that lives where the bridge later opens it.

Progress is printed line-by-line on stdout in plain ASCII so the bot
can stream it back to Telegram.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Optional


HERE = Path(__file__).resolve().parent
REQS_BRIDGE = HERE / "requirements-bridge.txt"

PROFILE_DIR = os.environ.get("NANO_CHROME_PROFILE", "")
CHROME_BIN = os.environ.get("NANO_CHROME_EXECUTABLE") or None
# Default to non-headless because Chrome's on-device-model service
# refuses to download/initialise in headless mode on most setups.
# On a true server you can force headless via NANO_HEADLESS=1.
HEADLESS = os.environ.get("NANO_HEADLESS", "0") != "0"

# When run as a systemd subprocess we may not inherit DISPLAY; fill
# in best-guess values so a non-headless Chrome can attach to the
# user's desktop session.
if not HEADLESS:
    if not os.environ.get("DISPLAY"):
        os.environ["DISPLAY"] = ":0"
    if not os.environ.get("XAUTHORITY"):
        cand = os.path.join(os.path.expanduser("~"), ".Xauthority")
        if os.path.exists(cand):
            os.environ["XAUTHORITY"] = cand


def log(msg: str) -> None:
    """Tag every line so the bot can prefix nicely."""
    print(msg, flush=True)


# ---------- phase 1: python deps ----------

def deps_ok() -> bool:
    try:
        import aiohttp  # noqa: F401
        import playwright  # noqa: F401
    except ImportError:
        return False
    return True


def install_deps() -> int:
    if not REQS_BRIDGE.exists():
        log(f"ERR: {REQS_BRIDGE} missing — cannot resolve bridge deps")
        return 1
    log(f"→ pip install -r {REQS_BRIDGE.name}")
    cmd = [sys.executable, "-m", "pip", "install", "-r", str(REQS_BRIDGE)]
    rc = subprocess.call(cmd)
    if rc != 0:
        log(f"ERR: pip install exited {rc}")
        return rc
    log("✓ python deps ready")
    return 0


# ---------- phase 2: chromium ----------

def chromium_path() -> Optional[str]:
    """Return the chromium binary playwright would launch, or None."""
    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        return None
    try:
        with sync_playwright() as pw:
            exe = pw.chromium.executable_path
            return exe if exe and Path(exe).exists() else None
    except Exception:
        return None


def install_chromium(update: bool = False) -> int:
    """Run `playwright install chromium`. With update=True also passes
    --force to redownload."""
    args = [sys.executable, "-m", "playwright", "install", "chromium"]
    if update:
        args.append("--force")
    log("→ " + " ".join(args))
    rc = subprocess.call(args)
    if rc != 0:
        log(f"ERR: playwright install exited {rc}")
        return rc
    log("✓ chromium ready")
    return 0


# ---------- phase 3: profile + Nano download ----------

# In-page JS: ensure LanguageModel is exposed, trigger the model
# download with a progress monitor, wait for availability.
NANO_PRIME_JS = r"""
(async () => {
  if (!('LanguageModel' in self)) {
    return { ok: false, error:
      "LanguageModel API not exposed. Chromium may be too old, or the " +
      "--enable-features flags didn't take effect." };
  }

  let avail = await LanguageModel.availability();
  if (avail === "no") {
    return { ok: false, error:
      "availability='no' — this device doesn't meet Chrome's Nano requirements " +
      "(insufficient disk, unsupported GPU, or feature unavailable in region)." };
  }

  // Trigger download with a progress monitor — calling create() with
  // a monitor is the documented way to force the model fetch.
  let last = -1;
  await LanguageModel.create({
    monitor(m) {
      m.addEventListener("downloadprogress", (e) => {
        const pct = Math.floor(e.loaded * 100);
        if (pct !== last && (pct === 100 || pct - last >= 5)) {
          last = pct;
          console.log("NANO_PROGRESS " + pct);
        }
      });
    },
  });

  avail = await LanguageModel.availability();
  return { ok: avail === "available", availability: avail };
})()
"""


# chrome://flags entries that need to be persisted in the profile's
# Local State so the Prompt API is exposed and the on-device-model
# bypasses the perf-requirement gate. Each entry is
# "<flag-name>@<variation-index>" where 1=Enabled, 2=Enabled with the
# first non-default variation (used here for "BypassPerfRequirement").
ENABLED_LABS = [
    "prompt-api-for-gemini-nano@1",
    "optimization-guide-on-device-model@2",  # 2 = Enabled BypassPerfRequirement
]


def _seed_local_state(profile_dir: str) -> None:
    """Pre-populate the profile's Local State with the chrome://flags
    settings Gemini Nano needs. Without this, --enable-features alone
    is not enough on stable Chrome — the API checks per-profile flag
    state and stays hidden if the user never toggled the flag in the
    UI. Idempotent: only adds entries that aren't already there."""
    p = Path(profile_dir) / "Local State"
    data: dict = {}
    if p.exists():
        try:
            data = json.loads(p.read_text())
        except (OSError, ValueError):
            # Corrupt or unreadable file — back it up and start fresh
            # so we don't lose anything we couldn't parse.
            backup = p.with_suffix(".bak")
            try:
                p.rename(backup)
                log(f"  ↳ unreadable Local State backed up to {backup}")
            except OSError:
                pass
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
        log(f"  ↳ seeded {len(ENABLED_LABS)} flags into {p}")
    else:
        log("  ↳ Local State already has the required flags")


def ensure_profile() -> int:
    if not PROFILE_DIR:
        log("ERR: NANO_CHROME_PROFILE env var not set (see config.example.env)")
        return 2
    p = Path(PROFILE_DIR)
    if not p.exists():
        log(f"→ creating profile dir {p}")
        p.mkdir(parents=True, exist_ok=True)
    log(f"✓ profile dir {p}")
    _seed_local_state(PROFILE_DIR)
    return 0


def _detect_chrome() -> Optional[str]:
    """Find a *real* Google Chrome — Playwright's bundled Chromium
    does NOT ship the on-device-model service, so Nano won't ever
    download into it. Operator override wins."""
    if CHROME_BIN:
        return CHROME_BIN
    for candidate in (
        "/usr/bin/google-chrome",
        "/usr/bin/google-chrome-stable",
        "/usr/bin/google-chrome-beta",
        "/usr/bin/google-chrome-unstable",
        "/opt/google/chrome/chrome",
        "/snap/bin/google-chrome",
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    ):
        if Path(candidate).exists():
            return candidate
    return None


def trigger_download() -> int:
    """Launch Chrome, prime Nano (downloads if needed), report
    availability, exit. Idempotent — safe to re-run on an already-set-up
    profile (returns immediately when 'available')."""
    from playwright.sync_api import sync_playwright

    # The umbrella + blink-features lines mirror what Chrome's own
    # built-in-AI docs recommend; the optimization-guide flags push
    # past the perf check (which always fails on virtualised GPUs).
    launch_args = [
        "--no-sandbox",
        "--disable-dev-shm-usage",
        "--enable-features=OptimizationGuideOnDeviceModel,"
        "PromptAPIForGeminiNano,BuiltInAIAPI",
        "--enable-blink-features=AIPromptAPI",
        "--optimization-guide-on-device-model-execution-validation",
        # Software GL keeps the on-device-model service from refusing
        # to start on headless boxes without a real GPU; on a desktop
        # box Chrome auto-uses the real GPU anyway.
        "--use-gl=swiftshader",
        "--enable-unsafe-swiftshader",
    ]
    chrome = _detect_chrome()
    if not chrome:
        log("ERR: no Google Chrome found on this system.")
        log("     Playwright's bundled Chromium does NOT include the")
        log("     on-device-model service — Nano cannot download into it.")
        log("     install with: apt install google-chrome-stable")
        log("     or set NANO_CHROME_EXECUTABLE=/path/to/google-chrome")
        return 4
    log(f"→ launching {chrome} (headless={HEADLESS}, profile={PROFILE_DIR})")
    with sync_playwright() as pw:
        ctx = pw.chromium.launch_persistent_context(
            PROFILE_DIR,
            headless=HEADLESS,
            executable_path=chrome,
            args=launch_args,
        )
        try:
            page = ctx.new_page()
            # Surface console messages — that's how we receive
            # NANO_PROGRESS pings from the in-page script.
            def on_console(msg):
                txt = msg.text or ""
                if txt.startswith("NANO_PROGRESS "):
                    pct = txt.split(" ", 1)[1]
                    log(f"  ↳ downloading Nano: {pct}%")
            page.on("console", on_console)

            page.goto("about:blank")
            log("→ priming LanguageModel (this triggers the model download if needed)...")
            result = page.evaluate(NANO_PRIME_JS)
            if not result.get("ok"):
                log(f"ERR: {result.get('error') or 'unknown'} (availability={result.get('availability')})")
                return 3
            log(f"✓ availability = {result['availability']}")
        finally:
            ctx.close()
    return 0


# ---------- subcommands ----------

def cmd_check() -> int:
    log(f"python:    {sys.version.split()[0]}")
    log(f"pip:       {'OK' if shutil.which('pip') else 'missing'}")
    log(f"deps:      {'OK' if deps_ok() else 'missing — run /nano_setup'}")
    chrome = _detect_chrome()
    if chrome:
        log(f"chrome:    {chrome}  (real Google Chrome — Nano supported)")
    else:
        cm = chromium_path()
        if cm:
            log(f"chrome:    NOT FOUND. (Playwright bundles Chromium at {cm}, "
                "but it lacks the on-device-model service Gemini Nano needs.)")
        else:
            log("chrome:    NOT FOUND.")
        log("           install with: apt install google-chrome-stable")
        log("           or set NANO_CHROME_EXECUTABLE=/path/to/google-chrome")
    log(f"profile:   {PROFILE_DIR or '(NANO_CHROME_PROFILE not set)'}")
    if PROFILE_DIR and Path(PROFILE_DIR).exists():
        log("           dir exists")
    elif PROFILE_DIR:
        log("           dir missing — will be created by /nano_setup")
    if deps_ok() and chrome and PROFILE_DIR:
        log("\nReady to start the bridge once Nano is downloaded.")
        log("Run /nano_setup to download Nano if you haven't already.")
        return 0
    return 1


def cmd_install() -> int:
    if not deps_ok():
        rc = install_deps()
        if rc != 0:
            return rc
    if chromium_path() is None:
        rc = install_chromium()
        if rc != 0:
            return rc
    rc = ensure_profile()
    if rc != 0:
        return rc
    return trigger_download()


def cmd_update() -> int:
    log("→ upgrading bridge python deps...")
    cmd = [sys.executable, "-m", "pip", "install", "-U", "-r", str(REQS_BRIDGE)]
    rc = subprocess.call(cmd)
    if rc != 0:
        return rc
    log("✓ deps upgraded")
    rc = install_chromium(update=True)
    if rc != 0:
        return rc
    rc = ensure_profile()
    if rc != 0:
        return rc
    # Re-prime in case Chrome version change broke the model.
    return trigger_download()


# ---------- main ----------

USAGE = """usage: python nano_setup.py {check|install|update}

  check    — report what's installed and what's missing, exit 0 if ready
  install  — first-time setup (deps + chromium + profile + Nano download)
  update   — upgrade deps + chromium, re-trigger Nano download
"""


def main() -> int:
    if len(sys.argv) != 2 or sys.argv[1] not in {"check", "install", "update"}:
        print(USAGE, file=sys.stderr)
        return 2
    {"check": cmd_check, "install": cmd_install, "update": cmd_update}
    return {"check": cmd_check, "install": cmd_install, "update": cmd_update}[sys.argv[1]]()


if __name__ == "__main__":
    sys.exit(main())
