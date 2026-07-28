"""One-time cleanup for log files written by older Graphify releases."""
from pathlib import Path


def remove_legacy_logs() -> None:
    for name in ("graphify-queries.log", "graphify-rebuild.log"):
        try:
            (Path.home() / ".cache" / name).unlink(missing_ok=True)
        except OSError:
            pass
