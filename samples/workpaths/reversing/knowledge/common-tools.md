# Common reversing tools

One-line descriptions of the tools you'll reach for most often in a
binary-analysis chat. Open the relevant subsection when you're
about to use one of them.

## Static disassembly / decompilation

- **Ghidra** — NSA-released, free, full decompiler. Strong for
  unknown architectures; the headless analyser (`analyzeHeadless`)
  is the right entry point when scripting from this chat.
- **IDA Free / Pro** — gold standard for x86/x64 / ARM. Hex-Rays
  decompiler in the Pro version is unmatched. Headless via `idat
  -c -A -S<script>`.
- **radare2 / Cutter** — open-source, scriptable, command-driven.
  Cutter is a Qt GUI on top of radare2's core.

## Dynamic analysis

- **gdb** with the `gef` or `pwndbg` plugins for ergonomic debugging.
- **frida** for runtime hooking — `frida-trace -i <fn> -U <pid>`.
- **strace / ltrace** for syscall / library-call tracing on Linux.
- **Process Monitor** (Procmon) for filesystem + registry tracing on
  Windows.

## File format triage

- **binwalk** — recursive carving + entropy analysis for firmware
  blobs. `binwalk -Me image.bin` extracts everything it recognises.
- **file** + **trid** — fast magic-byte based identification.
- **xxd / hexdump -C** — manual binary inspection when nothing
  fancier is needed.
- **bsdiff** / **vbindiff** — compare two binaries side-by-side.

## Cryptography & encoding

- **CyberChef** — recipe-driven encoding / decoding (works offline).
- **openssl** — when you need to verify a signature or inspect a
  cert in-line.

## Symbol / type recovery

- **dwarfdump** / **readelf -wa** — read DWARF debug info when it's
  present.
- **rabin2** — wrapper over libradare's parsers; faster than firing
  up the full TUI for "what sections does this ELF have?"

When in doubt about which tool fits, default to **file** → **binwalk
-Me** → **rabin2 -I** for the first 30 seconds of triage.
