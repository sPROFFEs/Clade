"""Thin Ollama HTTP client for the telemetry bot.

Wraps the subset of the Ollama API the bot exposes through Telegram:
  list (tags), running (ps), pull, delete, load, unload, set keep_alive.
Everything is synchronous and returns plain dicts/strings so the bot
can format and send straight to the user.
"""

from __future__ import annotations

import json
import os
import time
from dataclasses import dataclass
from typing import Generator, Optional

import requests

OLLAMA_URL = os.environ.get("OLLAMA_URL", "http://127.0.0.1:11434").rstrip("/")
# Default keep_alive applied by /load and /keepalive commands. Ollama
# accepts a duration string ("5m", "1h", "24h") or "-1" to pin until
# Ollama restarts, "0" to unload right away.
DEFAULT_KEEP_ALIVE = os.environ.get("OLLAMA_KEEP_ALIVE", "1h")


@dataclass
class Model:
    name: str
    size_mb: int          # disk size
    parameters: str       # "8B" / "70B" / ""
    modified_at: str      # ISO timestamp from /api/tags


@dataclass
class RunningModel:
    name: str
    size_mb: int          # VRAM footprint
    expires_at: str       # when it will be auto-unloaded


def _humanize_mb(n: int) -> str:
    if n >= 1024:
        return f"{n/1024:.1f} GB"
    return f"{n} MB"


class OllamaError(Exception):
    pass


def list_models() -> list[Model]:
    """GET /api/tags — installed models, sorted by name."""
    try:
        r = requests.get(f"{OLLAMA_URL}/api/tags", timeout=8)
        r.raise_for_status()
        data = r.json().get("models", [])
    except requests.RequestException as e:
        raise OllamaError(f"list: {e}") from e
    out: list[Model] = []
    for m in data:
        size_mb = int(m.get("size", 0) / (1024 * 1024))
        params = m.get("details", {}).get("parameter_size", "") or ""
        out.append(Model(
            name=m["name"],
            size_mb=size_mb,
            parameters=params,
            modified_at=m.get("modified_at", "")[:19].replace("T", " "),
        ))
    out.sort(key=lambda x: x.name)
    return out


def list_running() -> list[RunningModel]:
    """GET /api/ps — models currently held in VRAM."""
    try:
        r = requests.get(f"{OLLAMA_URL}/api/ps", timeout=5)
        r.raise_for_status()
    except requests.RequestException as e:
        raise OllamaError(f"ps: {e}") from e
    out: list[RunningModel] = []
    for m in r.json().get("models", []):
        out.append(RunningModel(
            name=m["name"],
            size_mb=int(m.get("size_vram", 0) / (1024 * 1024)),
            expires_at=m.get("expires_at", "")[:19].replace("T", " "),
        ))
    return out


def pull(name: str) -> Generator[str, None, None]:
    """POST /api/pull — yields human-readable progress lines."""
    try:
        r = requests.post(f"{OLLAMA_URL}/api/pull",
                          json={"name": name, "stream": True}, stream=True, timeout=None)
    except requests.RequestException as e:
        raise OllamaError(f"pull start: {e}") from e
    if r.status_code != 200:
        raise OllamaError(f"pull http {r.status_code}: {r.text[:200]}")
    last_status = ""
    last_pct = -1
    for raw in r.iter_lines():
        if not raw:
            continue
        try:
            payload = json.loads(raw)
        except ValueError:
            continue
        status = payload.get("status", "")
        # Progress fragments include total/completed bytes; emit a
        # percentage line at most every 10% so we don't spam Telegram.
        total = payload.get("total")
        done = payload.get("completed")
        if total and done:
            pct = int(done * 100 / total)
            if pct // 10 != last_pct // 10:
                last_pct = pct
                yield f"  ↳ {status}: {pct}%"
        elif status and status != last_status:
            last_status = status
            yield f"  ↳ {status}"
        if payload.get("error"):
            raise OllamaError(payload["error"])
    yield "✓ pull complete"


def delete(name: str) -> None:
    """DELETE /api/delete — remove a model and free its disk space."""
    try:
        r = requests.delete(f"{OLLAMA_URL}/api/delete",
                            json={"name": name}, timeout=15)
    except requests.RequestException as e:
        raise OllamaError(f"delete: {e}") from e
    if r.status_code not in (200, 204):
        raise OllamaError(f"delete http {r.status_code}: {r.text[:200]}")


def load(name: str, keep_alive: str = DEFAULT_KEEP_ALIVE) -> None:
    """Force a model into VRAM by sending an empty generate request
    with keep_alive set. "-1" pins until ollama restarts; "1h" / "24h"
    set a TTL; "0" immediately unloads (use unload() for that)."""
    try:
        r = requests.post(f"{OLLAMA_URL}/api/generate",
                          json={"model": name, "prompt": "", "keep_alive": keep_alive},
                          timeout=120)
    except requests.RequestException as e:
        raise OllamaError(f"load: {e}") from e
    if r.status_code != 200:
        raise OllamaError(f"load http {r.status_code}: {r.text[:200]}")


def unload(name: str) -> None:
    """Send keep_alive=0 to flush the model out of VRAM immediately."""
    load(name, keep_alive="0")


def humanize(m: Model) -> str:
    bits = [m.name, _humanize_mb(m.size_mb)]
    if m.parameters:
        bits.append(m.parameters)
    if m.modified_at:
        bits.append(m.modified_at)
    return " · ".join(bits)


def humanize_running(m: RunningModel) -> str:
    when = m.expires_at or "?"
    return f"{m.name} · {_humanize_mb(m.size_mb)} VRAM · expires {when}"


def reachable() -> Optional[str]:
    """Returns server version on success, None if Ollama isn't running."""
    try:
        r = requests.get(f"{OLLAMA_URL}/api/version", timeout=2)
        if r.status_code == 200:
            return r.json().get("version", "?")
    except requests.RequestException:
        pass
    return None


# Tiny self-check (only when run directly).
if __name__ == "__main__":
    v = reachable()
    print(f"ollama: {v or 'unreachable'} at {OLLAMA_URL}")
    if v:
        for m in list_models():
            print(" ", humanize(m))
        running = list_running()
        if running:
            print("running:")
            for m in running:
                print(" ", humanize_running(m))
    # Avoid "imported but unused" lint complaint if you trim the file.
    _ = time
