import { writable } from 'svelte/store'
import { api } from './api.js'

// Cross-page navigation state. App.svelte renders the page named by
// activePage; openChatId, when set, tells the Chats page to open that
// chat as a live thread (used when Agents starts a new chat).
export const activePage = writable('code')
export const openChatId = writable(null)

// CLI & Tools detection cache. Probing the CLIs + managed tools takes
// several seconds; prefetching at app startup means the "CLI & Tools"
// tab renders instantly from cache when the user opens it, then
// refreshes in the background. Shape: { clis, tools, codeInstalled,
// loaded } — loaded=false while the first probe is in flight.
export const cliCache = writable({ clis: [], tools: [], codeInstalled: false, loaded: false })

// prefetchCLIs runs the detection once and fills cliCache. force=true
// re-probes even if already loaded (used by the tab's Re-detect button).
let prefetchInFlight = false
export async function prefetchCLIs(force = false) {
  if (prefetchInFlight) return
  let already = false
  cliCache.update((c) => { already = c.loaded; return c })
  if (already && !force) return
  prefetchInFlight = true
  try {
    const [clis, tools, codeInstalled] = await Promise.all([
      api.listCLIBackends().catch(() => []),
      api.listManagedTools().catch(() => []),
      api.praimateCodeInstalled().catch(() => false),
    ])
    cliCache.set({ clis: clis || [], tools: tools || [], codeInstalled: !!codeInstalled, loaded: true })
  } finally {
    prefetchInFlight = false
  }
}

// pendingTerm, when set, tells the Code page to attach to an
// already-started PTY instead of launching one — used when the Chats
// page reopens a TUI workspace chat in the terminal.
// Shape: { termId, cli, cwd, label, note }
export const pendingTerm = writable(null)
