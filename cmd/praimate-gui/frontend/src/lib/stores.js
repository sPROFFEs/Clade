import { writable } from 'svelte/store'

// Cross-page navigation state. App.svelte renders the page named by
// activePage; openChatId, when set, tells the Chats page to open that
// chat as a live thread (used when Agents starts a new chat).
export const activePage = writable('chats')
export const openChatId = writable(null)
