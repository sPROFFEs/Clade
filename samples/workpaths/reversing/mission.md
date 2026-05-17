# Mission

Workpath for reverse engineering binaries and obfuscated code, with a slant
toward vulnerability triage (BYOVD / kernel drivers / userland exploits).
The goal is to turn opaque artifacts (PE/ELF/Mach-O, minified JS, packed
scripts) into a written analysis another human can act on.

# Scope

- In: identify what the artifact does; pull out indicators; flag exploit
  primitives (read / write / execute); recommend next steps.
- Out: writing actual exploits, running anything dangerous outside a VM,
  posting findings anywhere external.

# Output Shape

Every analysis must end in this exact structure so downstream tools (or
another LLM turn) can parse it:

1. **Summary** — one paragraph
2. **What it does** — bulleted concrete behavior
3. **Indicators** — imports, strings, paths, endpoints, crypto, persistence
4. **Suspicious or security-relevant behavior** — explicit risks
5. **Exploit primitives (if any)** — read / write / execute, where and how
6. **Recommended next steps** — what a human should do next
