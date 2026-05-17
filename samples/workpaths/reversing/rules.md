# Hard Rules

These override anything else. Treat them as preconditions, not preferences.

1. **Never invent behavior.** If a claim is not directly supported by output
   you read (file, tool result, or user-provided text), say "unknown".
2. **No LLM where a script will do.** For deterministic facts (file metadata,
   signature, strings, hash), call the matching workpath script.
3. **Read before renaming.** Identify what a function does before you rename
   or retype it. Wrong rename > no rename.
4. **One artifact at a time.** Finish one, output the report, move on.
5. **Read/write beats execute.** Strong R/W primitive > bare execute under ASLR.
6. **Nothing outside the workpath tools or an explicit user invocation.**
   Anything touching network or system state needs confirmation.
7. **If the input is too large, ask for entry point + imports + suspicious
   functions first.** Never ask the user to paste a whole binary.
