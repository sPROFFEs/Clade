#!/usr/bin/env python3
"""Capture and validate a PrAImate agent-run JSON response.

The script keeps stdin and stderr attached to the terminal, so an encrypted
PrAImate database can still ask for its password without corrupting stdout.
Only stdout is captured because `praimate agent run --output json` reserves it
for one machine-readable response object.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


SCHEMA = "praimate.agent-run/v1"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--agent", required=True, help="installed PrAImate agent ID")
    parser.add_argument("--cli", required=True, help="CLI adapter to execute")
    parser.add_argument("--folder", required=True, type=Path, help="project directory")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--prompt-file", type=Path, help="prompt text file")
    mode.add_argument("--workflow", help="named workflow configured on the agent")
    parser.add_argument(
        "--input",
        action="append",
        default=[],
        metavar="KEY=VALUE",
        help="workflow input; repeat for multiple values",
    )
    parser.add_argument("--praimate", default="praimate", help="PrAImate executable")
    parser.add_argument("--model", help="model name; required with --endpoint")
    parser.add_argument("--endpoint", help="saved endpoint, or the exact configured URL")
    parser.add_argument("--timeout", default="30m", help="PrAImate timeout")
    parser.add_argument("--tools", choices=("safe", "edits", "full"), default="safe")
    parser.add_argument("--run-id", help="durable idempotency key and status ID")
    parser.add_argument("--retry", action="store_true", help="retry the same durable run ID")
    parser.add_argument(
        "--skip-model-preflight",
        action="store_true",
        help="skip authenticated model-list validation",
    )
    parser.add_argument("--save-response", type=Path, help="write the captured JSON here")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    folder = args.folder.expanduser().resolve()
    if not folder.is_dir():
        print(f"error: project folder does not exist: {folder}", file=sys.stderr)
        return 2
    prompt_file = None
    if args.prompt_file:
        prompt_file = args.prompt_file.expanduser().resolve()
        if not prompt_file.is_file():
            print(f"error: prompt file does not exist: {prompt_file}", file=sys.stderr)
            return 2
    if args.input and not args.workflow:
        print("error: --input requires --workflow", file=sys.stderr)
        return 2
    seen_inputs: set[str] = set()
    for item in args.input:
        key, separator, _ = item.partition("=")
        key = key.strip()
        if not separator or not key:
            print(f"error: invalid --input {item!r}; expected key=value", file=sys.stderr)
            return 2
        if key in seen_inputs:
            print(f"error: duplicate --input key {key!r}", file=sys.stderr)
            return 2
        seen_inputs.add(key)
    if args.endpoint and not args.model:
        print("error: --endpoint requires --model", file=sys.stderr)
        return 2

    command = [
        args.praimate,
        "agent",
        "run",
        "--agent",
        args.agent,
        "--cli",
        args.cli,
        "--folder",
        str(folder),
        "--tools",
        args.tools,
        "--timeout",
        args.timeout,
        "--output",
        "json",
    ]
    if prompt_file:
        command.extend(("--prompt-file", str(prompt_file)))
    else:
        command.extend(("--workflow", args.workflow))
        for item in args.input:
            command.extend(("--input", item))
    if args.model:
        command.extend(("--model", args.model))
    if args.endpoint:
        command.extend(("--endpoint", args.endpoint))
    if args.run_id:
        command.extend(("--run-id", args.run_id))
    if args.retry:
        if not args.run_id:
            print("error: --retry requires --run-id", file=sys.stderr)
            return 2
        command.append("--retry")
    if args.skip_model_preflight:
        command.append("--skip-model-preflight")

    try:
        # stderr and stdin are deliberately inherited. This leaves the hidden
        # database-password prompt usable while stdout stays protocol-only.
        completed = subprocess.run(command, stdout=subprocess.PIPE, text=True)
    except FileNotFoundError:
        print(f"error: PrAImate executable not found: {args.praimate}", file=sys.stderr)
        return 127

    raw_stdout = completed.stdout.strip()
    try:
        result = json.loads(raw_stdout)
    except json.JSONDecodeError as exc:
        print(f"error: invalid JSON on PrAImate stdout: {exc}", file=sys.stderr)
        if raw_stdout:
            print(raw_stdout, file=sys.stderr)
        return completed.returncode or 1
    if not isinstance(result, dict) or result.get("schema") != SCHEMA:
        print(f"error: unexpected PrAImate response: {result!r}", file=sys.stderr)
        return completed.returncode or 1

    if args.save_response:
        destination = args.save_response.expanduser().resolve()
        destination.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(
            mode="w", encoding="utf-8", dir=destination.parent,
            prefix=f".{destination.name}.", delete=False,
        ) as response_file:
            response_file.write(json.dumps(result, indent=2) + "\n")
            temporary_response = response_file.name
        try:
            os.chmod(temporary_response, 0o600)
            os.replace(temporary_response, destination)
        except OSError as exc:
            try:
                os.unlink(temporary_response)
            except FileNotFoundError:
                pass
            print(f"error: save response: {exc}", file=sys.stderr)
            return 1

    if completed.returncode != 0 or not result.get("ok"):
        print(f"PrAImate failed: {result.get('error', 'unknown error')}", file=sys.stderr)
        return completed.returncode or 1

    print(result.get("reply", ""))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
