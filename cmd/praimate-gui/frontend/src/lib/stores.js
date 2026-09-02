import { writable } from 'svelte/store'
import { api } from './api.js'

// Cross-page navigation state. App.svelte renders the page named by
// activePage; openChatId, when set, tells the Chats page to open that
// chat as a live thread (used when Agents starts a new chat).
export const activePage = writable('code')
// Increment after setting pendingTerm when Code must consume a fresh attach
// request. This also handles clicking a Code session while Code is already
// the active page (setting the same activePage value alone does not remount).
export const pageRevision = writable(0)
export const openChatId = writable(null)

// App-wide operation feedback. Agent launches navigate away from the Agents
// page immediately, so page-local notices disappear before the user can read
// them. The shell owns this toast and keeps it visible across navigation.
export const toast = writable(null)
let toastTimer = null
export function showToast({ title, message = '', tone = 'ok', duration = 4200, dismissible = true }) {
  if (toastTimer) clearTimeout(toastTimer)
  toast.set({ title, message, tone, dismissible })
  toastTimer = duration > 0 ? setTimeout(() => {
    toast.set(null)
    toastTimer = null
  }, duration) : null
}
export function dismissToast() {
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = null
  toast.set(null)
}

// agentStudio, when set, opens the full-screen agent authoring studio.
// Value: { id } for an existing agent, or { id: '' } / { new: true } for
// a brand-new agent. null = studio closed (main shell shown).
export const agentStudio = writable(null)

// CLI & Tools detection cache. Probing the CLIs + managed tools takes
// several seconds; prefetching at app startup means the "CLI & Tools"
// tab renders instantly from cache when the user opens it, then
// refreshes in the background. Shape: { clis, tools, loaded } —
// loaded=false while the first probe is in flight.
export const cliCache = writable({ clis: [], tools: [], loaded: false })

// prefetchCLIs runs the detection once and fills cliCache. force=true
// re-probes even if already loaded (used by the tab's Re-detect button).
let prefetchPromise = null
export function prefetchCLIs(force = false) {
  if (prefetchPromise) return prefetchPromise
  let already = false
  cliCache.update((c) => { already = c.loaded; return c })
  if (already && !force) return Promise.resolve()
  prefetchPromise = (async () => {
    try {
      const [clis, tools] = await Promise.all([
        api.listCLIBackends().catch(() => []),
        api.listManagedTools().catch(() => []),
      ])
      cliCache.set({ clis: clis || [], tools: tools || [], loaded: true })
    } finally {
      prefetchPromise = null
    }
  })()
  return prefetchPromise
}

// pendingTerm, when set, tells the Code page to attach to an
// already-started PTY instead of launching one — used when the Chats
// page reopens a legacy workspace chat in the terminal.
// Shape: { termId, chatId, cli, cwd, label, model, local*, note }
export const pendingTerm = writable(null)
