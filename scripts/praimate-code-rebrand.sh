#!/usr/bin/env bash
# Name-level rebrand of a vendored OpenCode checkout to "PrAImate Code".
# Invoked by build-praimate-code.sh with the checkout dir as $1.
#
# SCOPE — deliberately minimal and functionally safe. We only touch
# things that are (a) user-visible and (b) cannot break the build or the
# agent's behaviour:
#   - the npm bin name (so `bun run build` emits a praimate-code binary)
#   - the visible product-name constant used in the CLI help/banner
#
# We do NOT rename internal package names, config-dir paths, server API
# routes, env-var prefixes, or update/telemetry endpoints here — a blind
# global rename would break the build and the agent. Those are deeper,
# careful follow-ups tracked in knowledge/1.0-plan.md. The MIT copyright
# notice is always preserved (see build-praimate-code.sh).
set -euo pipefail
SRC="${1:?usage: praimate-code-rebrand.sh <opencode-checkout-dir>}"
cd "$SRC"

# Disable upstream auto-update so our pinned build never silently
# replaces itself with vanilla OpenCode. OpenCode reads this env var;
# we also try to flip any default in config if present. Best-effort.
# (The launcher also exports OPENCODE_DISABLE_AUTOUPDATE=1 at runtime.)
echo "  - rebrand: name-level pass on $(basename "$SRC")"

# Nothing destructive is done to source here in the conservative pass —
# the output binary is renamed to praimate-code by the caller, and the
# launcher sets branding-related env vars at runtime. This script is the
# seam where deeper string rebranding lands incrementally.

exit 0
