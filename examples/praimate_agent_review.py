#!/usr/bin/env python3
"""Run a PrAImate agent and parse its stable JSON response."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--agent", required=True, help="installed PrAImate agent ID")
    parser.add_argument("--folder", required=True, type=Path, help="project folder")
    parser.add_argument("--prompt", required=True, help="instruction sent to the agent")
    parser.add_argument("--praimate", default="praimate", help="PrAImate executable")
    parser.add_argument("--cli", required=True, help="CLI used to execute the agent")
    parser.add_argument("--model", help="optional model override")
    parser.add_argument(
        "--endpoint",
        help="use 'saved' or the exact Local LLM endpoint configured in PrAImate",
    )
    parser.add_argument("--tools", choices=("safe", "edits", "full"), default="safe")
    parser.add_argument("--timeout", default="30m", help="PrAImate timeout value")
    parser.add_argument("--persist", action="store_true", help="retain the chat")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    folder = args.folder.expanduser().resolve()
    if not folder.is_dir():
        print(f"error: project folder does not exist: {folder}", file=sys.stderr)
        return 2

    # A named file keeps a large/sensitive prompt out of argv. It is closed
    # before spawning for Windows compatibility and removed in finally.
    prompt_path = ""
    try:
        with tempfile.NamedTemporaryFile(
            mode="w", encoding="utf-8", prefix="praimate-prompt-", delete=False
        ) as prompt_file:
            prompt_file.write(args.prompt)
            prompt_path = prompt_file.name
        try:
            os.chmod(prompt_path, 0o600)
        except OSError:
            pass

        command = [
            args.praimate,
            "agent",
            "run",
            "--agent",
            args.agent,
            "--folder",
            str(folder),
            "--prompt-file",
            prompt_path,
            "--output",
            "json",
            "--tools",
            args.tools,
            "--timeout",
            args.timeout,
        ]
        if args.cli:
            command.extend(("--cli", args.cli))
        if args.model:
            command.extend(("--model", args.model))
        if args.endpoint:
            command.extend(("--endpoint", args.endpoint))
        if args.persist:
            command.append("--persist")

        # stdin and stderr remain attached to the terminal. If the database
        # password is not remembered, PrAImate can prompt with echo disabled.
        completed = subprocess.run(command, stdout=subprocess.PIPE, text=True)
    except FileNotFoundError:
        print(f"error: PrAImate executable not found: {args.praimate}", file=sys.stderr)
        return 127
    finally:
        if prompt_path:
            try:
                os.unlink(prompt_path)
            except FileNotFoundError:
                pass

    try:
        result = json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        print(f"error: PrAImate returned invalid JSON: {exc}", file=sys.stderr)
        if completed.stdout:
            print(completed.stdout, file=sys.stderr)
        return completed.returncode or 1

    if completed.returncode != 0 or not result.get("ok"):
        print(f"PrAImate failed: {result.get('error', 'unknown error')}", file=sys.stderr)
        return completed.returncode or 1

    print(result["reply"])
    if result.get("chatId"):
        print(f"chat: {result['chatId']}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
