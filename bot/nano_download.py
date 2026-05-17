"""Open chrome://on-device-internals in a real-Chrome session and
dump what Chrome reports about the on-device-model state.

We can't use Playwright for this — chrome:// URLs are blocked from
remote control via CDP. So we launch Chrome directly (under Xvfb
when no real display) and read what the page renders by dumping it
via `--dump-dom`. That gives us the same status text the user would
see if they opened the page manually, which tells us:

  - whether the model is "Downloaded" / "Downloading" / "Not Downloaded"
  - the actual error reason if the service refuses (low disk, bad GPU,
    enterprise policy override, etc.)

Side effect: just loading the page is enough on most setups to wake
the on-device-model service and start the download. So this script
doubles as a "kick the model into action" command.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import Optional


PROFILE_DIR = os.environ.get("NANO_CHROME_PROFILE", "")
CHROME_BIN = os.environ.get("NANO_CHROME_EXECUTABLE", "")


def _detect_chrome() -> Optional[str]:
    if CHROME_BIN:
        return CHROME_BIN
    for c in (
        "/usr/bin/google-chrome-unstable",
        "/opt/google/chrome-unstable/chrome",
        "/usr/bin/google-chrome-beta",
        "/opt/google/chrome-beta/chrome",
        "/usr/bin/google-chrome-stable",
        "/usr/bin/google-chrome",
        "/opt/google/chrome/chrome",
    ):
        if Path(c).exists():
            return c
    return None


def _start_xvfb() -> Optional[subprocess.Popen]:
    if not shutil.which("Xvfb"):
        return None
    disp = os.environ.get("NANO_XVFB_DISPLAY", ":99")
    proc = subprocess.Popen(
        ["Xvfb", disp, "-screen", "0", "1280x720x24",
         "-nolisten", "tcp", "-ac"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # Wait briefly for the display to come up.
    if shutil.which("xdpyinfo"):
        for _ in range(20):
            time.sleep(0.1)
            if subprocess.call(["xdpyinfo", "-display", disp],
                               stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL,
                               timeout=3) == 0:
                os.environ["DISPLAY"] = disp
                return proc
    else:
        time.sleep(1)
        os.environ["DISPLAY"] = disp
        return proc
    proc.terminate()
    return None


def main() -> int:
    print(f"profile: {PROFILE_DIR or '(NANO_CHROME_PROFILE not set!)'}", flush=True)
    if not PROFILE_DIR:
        return 2
    chrome = _detect_chrome()
    if not chrome:
        print("ERR: no Google Chrome found.", flush=True)
        return 3
    print(f"chrome:  {chrome}", flush=True)

    # The systemd unit sets DISPLAY=:0 even when no real X server is
    # listening there (typical on a headless Proxmox VM). So we
    # actively probe before trusting the env — and fall back to Xvfb
    # if the existing display doesn't answer.
    xvfb = None
    cur = os.environ.get("DISPLAY", "")

    def _display_works(disp: str) -> bool:
        if not shutil.which("xdpyinfo"):
            return False  # can't verify; spawn Xvfb to be safe
        try:
            return subprocess.call(["xdpyinfo", "-display", disp],
                                   stdout=subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL,
                                   timeout=3) == 0
        except (OSError, subprocess.TimeoutExpired):
            return False

    if cur and _display_works(cur):
        print(f"DISPLAY: {cur} (reachable)", flush=True)
    else:
        if cur:
            print(f"DISPLAY: {cur} (NOT reachable — spawning Xvfb)", flush=True)
        else:
            print("→ no DISPLAY; spawning Xvfb...", flush=True)
        # Clear XAUTHORITY too so Chrome doesn't try to auth against
        # the wrong cookie for our Xvfb display.
        os.environ.pop("XAUTHORITY", None)
        xvfb = _start_xvfb()
        if not xvfb:
            print("WARN: couldn't spawn Xvfb — chrome will probably fail to start.",
                  flush=True)

    # First: tell the user what's actually on disk. The on-device
    # model store lives under the profile; if it's empty Chrome has
    # never started a download, which is itself diagnostic.
    print("\n=== profile contents (looking for on-device-model traces) ===",
          flush=True)
    interesting = []
    for root, dirs, files in os.walk(PROFILE_DIR):
        depth = root[len(PROFILE_DIR):].count(os.sep)
        if depth > 4:
            dirs[:] = []
            continue
        for name in dirs + files:
            low = name.lower()
            if any(k in low for k in ("optimization_guide", "on_device",
                                       "ondevice", "model")):
                full = os.path.join(root, name)
                try:
                    size = os.path.getsize(full) if os.path.isfile(full) else 0
                    kind = "f" if os.path.isfile(full) else "d"
                    interesting.append((full, kind, size))
                except OSError:
                    pass
    if interesting:
        for full, kind, size in interesting[:40]:
            tag = f"{size:,}B" if kind == "f" else "[dir]"
            print(f"  {tag:>14}  {full}", flush=True)
    else:
        print("  (no model-related files/dirs found — Chrome has never "
              "attempted a Nano download on this profile)", flush=True)

    try:
        # --dump-dom returns the rendered DOM and exits, no UI needed.
        # We MUST also pass --headless=new — otherwise Chrome tries to
        # open a real window and hangs on Xvfb without a WM.
        args = [
            chrome,
            f"--user-data-dir={PROFILE_DIR}",
            "--headless=new",
            "--no-sandbox",
            "--disable-dev-shm-usage",
            "--enable-features=OptimizationGuideOnDeviceModel,"
            "PromptAPIForGeminiNano,BuiltInAIAPI",
            "--enable-blink-features=AIPromptAPI",
            "--use-gl=swiftshader",
            "--enable-unsafe-swiftshader",
            "--virtual-time-budget=10000",  # let async data load
            "--dump-dom",
            "chrome://on-device-internals",
        ]
        print(f"→ {' '.join(args[:3])} ... chrome://on-device-internals",
              flush=True)
        try:
            out = subprocess.check_output(args, timeout=60,
                                          stderr=subprocess.STDOUT)
        except subprocess.TimeoutExpired:
            print("ERR: chrome timed out after 60s", flush=True)
            return 4
        except subprocess.CalledProcessError as e:
            print(f"ERR: chrome exited {e.returncode}:", flush=True)
            print(e.output.decode("utf-8", errors="replace")[-2000:], flush=True)
            return 5

        body = out.decode("utf-8", errors="replace")
        # Look for the interesting fragments. The page structure has
        # shifted across Chrome versions, so we grep multiple patterns
        # and print anything that mentions the model.
        print("\n=== model-related lines from chrome://on-device-internals ===",
              flush=True)
        found = 0
        for line in body.splitlines():
            low = line.lower()
            if any(k in low for k in (
                "foundational", "on-device", "model", "available",
                "download", "register", "criteria", "eligibility",
                "performance class")):
                # Strip HTML tags crudely; the page is mostly text.
                import re
                txt = re.sub(r"<[^>]+>", "", line).strip()
                if txt and len(txt) < 400:
                    print("  " + txt, flush=True)
                    found += 1
        if not found:
            print("  (no model lines found — page may have a different layout)",
                  flush=True)
            print("\n=== first 60 lines of stripped body ===", flush=True)
            import re
            for i, line in enumerate(re.sub(r"<[^>]+>", " ", body).splitlines()):
                line = line.strip()
                if line:
                    print("  " + line[:300], flush=True)
                    if i > 60:
                        break

        # After dumping, also try once to see if availability changed.
        # We can't easily run JS here without Playwright; the bridge
        # will retry on its next /nano_start anyway.
        print("\n→ done. Now retry /nano_start (the page visit may have "
              "woken up the on-device-model service).", flush=True)
        return 0
    finally:
        if xvfb:
            try:
                xvfb.terminate()
                xvfb.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                pass


if __name__ == "__main__":
    sys.exit(main())
