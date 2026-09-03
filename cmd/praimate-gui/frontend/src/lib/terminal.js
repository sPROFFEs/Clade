// Terminal bridge — thin wrappers over the PTY Wails bindings plus the
// base64 framing the Go side expects. Kept separate from api.js so the
// xterm imports only load on the pages that need them.

function app() {
  if (typeof window !== 'undefined' && window.go?.main?.App) return window.go.main.App
  return null
}

export const term = {
  pickFolder: () => app()?.PickProjectFolder() ?? Promise.reject(new Error('no backend')),
  start: (agentID, cli, model, cwd, localEndpoint, localApiKey, localModel, resume = false, skills = []) =>
    app()?.StartTerminal(agentID, cli, model || '', cwd, localEndpoint || '', localApiKey || '', localModel || '', !!resume, skills) ??
    Promise.reject(new Error('no backend')),
  write: (id, text) => app()?.WriteTerminal(id, encodeTerminalInput(text)),
  resize: (id, cols, rows) => app()?.ResizeTerminal(id, cols, rows),
  snapshot: (id) => app()?.GetTerminalSnapshot(id) ?? Promise.reject(new Error('no backend')),
  codeSnapshot: (chatID, termID) => app()?.GetCodeSessionSnapshot(chatID, termID || '') ?? Promise.reject(new Error('no backend')),
  close: (id) => app()?.CloseTerminal(id),
}

// xterm's onData value is a JavaScript Unicode string, while a PTY consumes
// bytes. TextEncoder handles accents, combining characters and astral symbols
// without the deprecated escape/unescape conversion (which can throw on lone
// surrogate input). Base64 keeps both UTF-8 and terminal control bytes intact
// across the Wails JSON bridge.
export function encodeTerminalInput(text) {
  const bytes = new TextEncoder().encode(text || '')
  let raw = ''
  const chunkSize = 0x8000
  for (let i = 0; i < bytes.length; i += chunkSize) {
    raw += String.fromCharCode(...bytes.subarray(i, i + chunkSize))
  }
  return btoa(raw)
}

// onData subscribes to PTY output for one terminal id. Returns an
// unsubscribe fn. The Go side emits base64; we decode to a byte string
// xterm can write directly.
export function onTermData(id, handler) {
  if (!(typeof window !== 'undefined' && window.runtime?.EventsOn)) return () => {}
  const evt = 'term:data:' + id
  window.runtime.EventsOn(evt, (payload) => {
    // Older backends emitted a bare base64 string; accept both shapes so the
    // frontend remains usable during a dev hot-reload.
    const b64 = typeof payload === 'string' ? payload : payload?.data
    if (!b64) return
    handler(decodeBase64Bytes(b64), typeof payload === 'string' ? null : payload)
  })
  return () => window.runtime.EventsOff(evt)
}

export function decodeBase64Bytes(b64) {
  const raw = atob(b64 || '')
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export function onTermExit(id, handler) {
  if (!(typeof window !== 'undefined' && window.runtime?.EventsOn)) return () => {}
  const evt = 'term:exit:' + id
  window.runtime.EventsOn(evt, handler)
  return () => window.runtime.EventsOff(evt)
}

function normalizedPath(path) {
  // Lowercasing breaks case-sensitive file systems (Linux, WSL, macOS).
  // Rely on exact string match (minus trailing slashes) to prevent false mismatches.
  return String(path || '').replace(/[\\/]+$/, '')
}

function terminalNameForCLI(cli) { return String(cli || '') }

// Old GUI builds could create the chat row just after navigation had already
// detached the Code page, leaving a live terminal without chatId. Recover
// only when cwd (+ CLI when available) identifies one unbound PTY uniquely.
export function findTerminalForChat(terms, chat) {
  const all = terms || []
  const direct = all.find((t) => t.chatId === chat?.ID)
  if (direct) return direct

  const cwd = normalizedPath(chat?.WorkspacePath)
  if (!cwd) return null
  let candidates = all.filter((t) => !t.chatId && normalizedPath(t.cwd) === cwd)
  const expectedName = terminalNameForCLI(chat?.CLIAgent)
  if (expectedName) {
    const named = candidates.filter((t) => t.name === expectedName)
    if (named.length === 1) return named[0]
    if (named.length > 1) return null
  }
  return candidates.length === 1 ? candidates[0] : null
}
