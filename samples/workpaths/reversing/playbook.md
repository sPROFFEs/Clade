# Stages

Process every target through these stages in order. Do not skip ahead.
If a prerequisite is missing, stop and tell the user what's missing
instead of guessing.

## Stage 1 — Triage

- Run `file_summary` on the path; record format / arch / size.
- For PE drivers, also run `check_signed` and note the signer.
- Reject obvious non-targets early (known-blocked drivers, tiny stubs).

## Stage 2 — Surface scan (cheap)

- Run `extract_strings` and flag suspicious imports (`MmMapIoSpace`,
  `ZwMapViewOfSection`, `ProbeForRead/Write`, `IoCreateDevice`).
- Note any IOCTL codes (`0x22XXXX` patterns).
- Note unusually small driver sizes — often "god mode" IOCTL wrappers.

## Stage 3 — Decompile / read code

- For pasted pseudocode: read top-down, identify IOCTL handlers and
  unchecked user-controlled writes.
- For raw binaries: ask the user to paste decompiled output. Do not
  invent decompilation.

## Stage 4 — Vulnerability scan

Pattern-match for known dangerous shapes:

- Arbitrary physical memory mapping (`MmMapIoSpace[Ex]` with user-supplied addrs/sizes).
- Arbitrary virtual R/W (`MmGetSystemAddressForMdlSafe`, missing `Probe*` checks).
- Unchecked `IOCTL METHOD_NEITHER` handlers.
- Integer overflow into copy size.
- Token / process handle abuse.

For each finding: file:line or function, one-paragraph description,
primitive class (read / write / execute), confidence (low / medium / high).

## Stage 5 — Scoring (multi-target)

Two integers 0-100 per target + a one-line reason:

- `exploitability` — primitive strength, preconditions, mitigations.
- `byovd_usability` — handle-open ACLs, signer reputation, size, WDAC.

Strong read+write beats bare execute.

## Stage 6 — Final report

Emit the structured report from the mission "Output Shape". Nothing else.
