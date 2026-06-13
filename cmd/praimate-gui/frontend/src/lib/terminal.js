// Terminal bridge — thin wrappers over the PTY Wails bindings plus the
// base64 framing the Go side expects. Kept separate from api.js so the
// xterm imports only load on the pages that need them.

function app() {
  if (typeof window !== 'undefined' && window.go?.main?.App) return window.go.main.App
  return null
}

export const term = {
  pickFolder: () => app()?.PickProjectFolder() ?? Promise.reject(new Error('no backend')),
  start: (agentID, cli, model, cwd, localEndpoint, localApiKey, localModel) =>
    app()?.StartTerminal(agentID, cli, model || '', cwd, localEndpoint || '', localApiKey || '', localModel || '') ??
    Promise.reject(new Error('no backend')),
  write: (id, bytes) => {
    // bytes is a string of raw chars from xterm onData; encode to base64.
    const b64 = btoa(unescape(encodeURIComponent(bytes)))
    return app()?.WriteTerminal(id, b64)
  },
  resize: (id, cols, rows) => app()?.ResizeTerminal(id, cols, rows),
  close: (id) => app()?.CloseTerminal(id),
}

// onData subscribes to PTY output for one terminal id. Returns an
// unsubscribe fn. The Go side emits base64; we decode to a byte string
// xterm can write directly.
export function onTermData(id, handler) {
  if (!(typeof window !== 'undefined' && window.runtime?.EventsOn)) return () => {}
  const evt = 'term:data:' + id
  window.runtime.EventsOn(evt, (b64) => {
    const raw = decodeURIComponent(escape(atob(b64)))
    handler(raw)
  })
  return () => window.runtime.EventsOff(evt)
}

export function onTermExit(id, handler) {
  if (!(typeof window !== 'undefined' && window.runtime?.EventsOn)) return () => {}
  const evt = 'term:exit:' + id
  window.runtime.EventsOn(evt, handler)
  return () => window.runtime.EventsOff(evt)
}
