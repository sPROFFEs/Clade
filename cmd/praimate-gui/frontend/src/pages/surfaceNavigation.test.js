import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { api as apiBridge } from '../lib/api.js'

const app = await readFile(new URL('../App.svelte', import.meta.url), 'utf8')
const agents = await readFile(new URL('./Agents.svelte', import.meta.url), 'utf8')
const chats = await readFile(new URL('./Chats.svelte', import.meta.url), 'utf8')
const clis = await readFile(new URL('./CLIs.svelte', import.meta.url), 'utf8')
const studio = await readFile(new URL('./Studio.svelte', import.meta.url), 'utf8')
const code = await readFile(new URL('./Code.svelte', import.meta.url), 'utf8')
const detached = await readFile(new URL('./DetachedSession.svelte', import.meta.url), 'utf8')

test('Studio owns studio-session navigation and Chats excludes its rows', () => {
  assert.match(app, /id: 'studio', label: 'Studio'/)
  assert.match(studio, /Settings\?\.surface === 'studio'/)
  assert.match(studio, /\+ New Studio/)
  assert.match(studio, /api\.openEditorWindow/)
  assert.match(studio, /openChatId\.set\(chat\.ID\)/)
  assert.doesNotMatch(chats, /studioChats/)
  assert.match(chats, /surface !== 'studio'/)
})

test('agent surface launch uses a modal and app-wide completion toast', () => {
  assert.match(agents, /class="modal-backdrop"/)
  assert.match(agents, /aria-labelledby="agent-launch-title"/)
  assert.match(agents, /showToast\(\{ title: 'Chat ready'/)
  assert.match(agents, /showToast\(\{ title: 'Terminal ready'/)
  assert.match(agents, /showToast\(\{ title: 'Studio opened'/)
})

test('agent terminal launch matches backend arguments and keeps session state after closing the modal', async () => {
  globalThis.window = {
    go: { main: { App: { StartTerminal: (...args) => Promise.resolve(args) } } },
  }
  try {
    const args = await apiBridge.startTerminal('agent', 'praimate-code', '', '/tmp/project', '', '', '')
    assert.equal(args.length, 9)
    assert.deepEqual(args[8], [])
  } finally {
    delete globalThis.window
  }
  assert.match(agents, /api\.recordCodeSession\(agent \? agent\.id : '', cli,/)
  assert.match(agents, /const sessionName = dlg\.name\.trim\(\)/)
  assert.doesNotMatch(agents, /dlg = null[\s\S]{0,300}dlg\.name/)
})

test('CLI installation remains modal through PATH refresh and detection', () => {
  assert.match(clis, /class="modal-content install-modal"/)
  assert.match(clis, /await api\.refreshPATH\(\)/)
  assert.match(clis, /await load\(\)/)
  assert.match(clis, /Installation complete\. PATH was refreshed and the executable was detected\./)
  assert.match(clis, /detected\.binary/)
})

test('chat and terminal sessions can detach without stopping their backend work', () => {
  assert.match(chats, /api\.detachSession\('chat', chat\.ID/)
  assert.match(chats, /detachedChats\.has\(req\.chatId\)/)
  assert.match(code, /api\.detachSession\('terminal', id/)
  assert.match(code, /teardown\(false\)/)
  assert.match(detached, /Move this terminal|Connected to PrAImate|praimate:detached-disconnected/)
  assert.match(detached, /if \(!disconnectTimer\) disconnectTimer = setTimeout/)
  assert.match(detached, /async function stopTerminal[\s\S]*window\.runtime\?\.Quit\?\.\(\)/)
})

test('detached mode is resolved before database unlock and heavy pages are lazy-loaded', () => {
  assert.match(app, /detachedMode = await api\.detachedMode\(\)/)
  assert.ok(app.indexOf('detachedMode = await api.detachedMode()') < app.indexOf('databaseLock = await api.databaseLockStatus()'))
  assert.match(app, /load: \(\) => import\('\.\/pages\/Code\.svelte'\)/)
  assert.doesNotMatch(app, /import Code from '\.\/pages\/Code\.svelte'/)
  assert.match(app, /Close secondary windows first/)
})
