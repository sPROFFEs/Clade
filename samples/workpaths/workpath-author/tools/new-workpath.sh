#!/usr/bin/env bash
# Scaffold a fresh workpath directory with the standard layout.
# Usage:  new-workpath <name>
# Creates ./<name>/ in the current directory with mission.md +
# workpath.json + an HTML-comment-only personality.md. Optional
# tools/, agents/, knowledge/ are left out (create them when you
# have something to put in them).

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: new-workpath.sh <name>" >&2
  exit 2
fi

name="$1"
# Schema requires ^[a-z0-9][a-z0-9_-]*$ for the workpath name.
if [[ ! "$name" =~ ^[a-z0-9][a-z0-9_-]*$ ]]; then
  echo "invalid workpath name: '$name'" >&2
  echo "must match ^[a-z0-9][a-z0-9_-]*$ (lowercase, digits, _, -)" >&2
  exit 1
fi

if [[ -e "$name" ]]; then
  echo "already exists: $name" >&2
  exit 1
fi

mkdir -p "$name"
cat > "$name/workpath.json" <<EOF
{
  "description": "ONE-LINE summary — edit me before first launch.",
  "version": "1"
}
EOF

cat > "$name/mission.md" <<EOF
# $name

> ONE-LINE description — copy this to workpath.json:description too.

What this workpath is for, in 2-4 sentences. Be concrete about
what the agent does and how it behaves at a high level; the
playbook fills in the procedure.
EOF

cat > "$name/personality.md" <<EOF
<!--
Persona file. Anything below this comment becomes the persona
prepended at the top of the compiled instructions. Comments-only
files are treated as "no persona" and produce no output.
-->
EOF

echo "✓ scaffolded $name/"
echo "  next: edit $name/mission.md and $name/workpath.json,"
echo "  then add playbook.md / rules.md / tools/ / agents/ /"
echo "  knowledge/ as needed."
