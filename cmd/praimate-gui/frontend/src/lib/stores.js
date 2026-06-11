import { writable } from 'svelte/store'

// Cross-page navigation state. App.svelte renders the page named by
// activePage; openChatId, when set, tells the Chats page to open that
// chat as a live thread (used when Agents starts a new chat).
export const activePage = writable('code')
export const openChatId = writable(null)

// pendingTerm, when set, tells the Code page to attach to an
// already-started PTY instead of launching one — used when the Chats
// page reopens a TUI workspace chat in the terminal.
// Shape: { termId, cli, cwd, label, note }
export const pendingTerm = writable(null)
