# CLI agent API

PrAImate can run an installed agent without opening the desktop application.
This makes it an adapter between scripts and the configured CLI/model while
retaining the agent's instructions, knowledge, MCP references, runtime policy,
and encrypted settings.

## Basic call

```bash
praimate agent run \
  --agent DEV-TEAM \
  --cli praimate-code \
  --folder /path/to/project \
  --prompt "Review this code and report confirmed defects"
```

`praimate --agent ...` is an equivalent compact form. The agent must already
exist in PrAImate. `--folder` must name an accessible directory. `--cli`
selects the actual adapter that executes the agent and must be one of the
agent's supported CLIs. If omitted, PrAImate uses the first supported CLI;
automation should normally specify it to avoid depending on agent ordering.

For long or sensitive prompts, prefer a protected file so the prompt does not
appear in the process list:

```bash
praimate agent run \
  --agent DEV-TEAM \
  --cli praimate-code \
  --folder /path/to/project \
  --prompt-file /path/to/protected-prompt.txt
```

To reproduce the GUI's local-endpoint selection, pass the endpoint and the
model it serves:

```bash
praimate agent run \
  --agent DEV-TEAM \
  --cli praimate-code \
  --endpoint saved \
  --model qwen3-coder \
  --folder /path/to/project \
  --prompt-file /path/to/protected-prompt.txt
```

`saved` selects the endpoint configured in the GUI's Local LLM tab. An explicit
URL is accepted only when it matches that saved endpoint. This prevents a
headless caller from redirecting the encrypted API key to another server. The
key is loaded from encrypted settings and is deliberately not accepted in
argv. Local routing is available for Claude, OpenClaude, OpenCode, and PrAImate
Code. Codex local routing remains unsupported.

Piped stdin is also accepted when neither `--prompt` nor `--prompt-file` is
present. Do not use prompt stdin when an interactive database-password prompt
may be required: a pipe is not a terminal.

## Database unlock

PrAImate never accepts the database password as a command-line argument. Such
arguments are commonly visible in process listings and shell history.

Unlock sources are tried in this order:

1. `PRAIMATE_DB_PASSWORD`, intended only for a child process in unattended
   automation. PrAImate removes it from its own environment immediately after
   reading it.
2. The password remembered by Windows Credential Manager or the Linux desktop
   Secret Service.
3. If stdin is an interactive terminal, a `PrAImate database password:` prompt
   on stderr. Input echo is disabled, so the password is not displayed or
   stored in shell history.
4. If no source is available in a non-interactive run, PrAImate returns a
   structured error instead of hanging.

`--db-password-stdin` is available for wrappers. On a terminal it uses the
same hidden prompt. From a pipe it reads one password line, but then the prompt
must come from `--prompt` or a real `--prompt-file`; one stdin stream cannot
carry both values.

For interactive automation, inherit stdin and stderr and capture stdout only.
That preserves the hidden prompt while keeping stdout valid JSON. The supplied
[Python example](../examples/praimate_agent_review.py) does exactly this.

For fully unattended automation, use an OS credential store where possible.
If an environment secret is necessary, inject it into the child process from
the CI secret manager rather than exporting it in an interactive shell.

## Output contract

The default `--output json` writes exactly one
`praimate.agent-run/v1` object to stdout:

```json
{
  "schema": "praimate.agent-run/v1",
  "ok": true,
  "agentId": "DEV-TEAM",
  "agentName": "Development Team",
  "cli": "praimate-code",
  "runtime": "native",
  "reply": "No confirmed defects found.",
  "durationMs": 8421
}
```

Failures use the same schema with `ok: false`, an `error` string, and a nonzero
exit status. Diagnostics and password prompts go to stderr, never stdout.

Other formats:

- `--output jsonl` emits zero or more `type: "event"` objects followed by one
  `type: "result"` object. Use it for live progress.
- `--output text` prints only the final reply and is meant for humans, not
  stable programmatic parsing.

Exit statuses are `0` for success, `1` for an execution/configuration failure,
`2` for invalid arguments or input, and `124` when `--timeout` expires.

## Options and trust policy

| Option | Meaning |
| --- | --- |
| `--cli NAME` | Select the CLI adapter that runs the agent. |
| `--model NAME` | Override the cloud model, or select the model served by `--endpoint`. |
| `--endpoint saved\|URL` | Route through the configured local endpoint; a URL must match the saved value. Requires `--model`. |
| `--prompt TEXT` | Supply a short prompt inline. |
| `--prompt-file PATH` | Read a prompt from a file; `-` means stdin. |
| `--timeout 30m` | Set the execution deadline; `0` disables it. |
| `--tools safe` | Default. Do not approve mutation-capable managed tools. |
| `--tools edits` | Permit declared project writes when enforceable. |
| `--tools full` | Approve every capability declared by the agent. |
| `--persist` | Keep the backing chat and include `chatId` in the result. |
| `--output FORMAT` | Select `json`, `jsonl`, or `text`. |

Use `safe` for analysis. `edits` and especially `full` grant the selected agent
more authority over the project and must be explicit trust decisions. Without
`--persist`, PrAImate deletes the temporary chat after producing the result.

## Python integration

The runnable example captures and validates the JSON response, safely supplies
the prompt through a temporary file, and leaves the terminal attached for a
hidden database-password prompt:

```bash
python3 examples/praimate_agent_review.py \
  --agent DEV-TEAM \
  --cli praimate-code \
  --folder /path/to/project \
  --prompt "Review the project and return only actionable findings"
```

On Windows, run the same script with `py` and pass
`--praimate praimate.exe` if the executable is not resolved automatically.
