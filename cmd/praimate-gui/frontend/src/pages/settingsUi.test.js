import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const settingsSource = await readFile(new URL('./Settings.svelte', import.meta.url), 'utf8')
const mcpSource = await readFile(new URL('./MCP.svelte', import.meta.url), 'utf8')

test('destructive data dialog uses an opaque surface and supported-CLI wording', () => {
  assert.match(settingsSource, /\.danger-panel\s*\{[^}]*background:\s*var\(--bg-panel\)/s)
  assert.match(settingsSource, /class="modal-backdrop destructive-backdrop"/)
  assert.doesNotMatch(settingsSource, /from Codex, OpenCode, and DeepSeek config/)
})

test('MCP state and toggle action are presented separately', () => {
  assert.match(mcpSource, /s\.enabled \? 'Enabled' : 'Disabled'/)
  assert.match(mcpSource, /s\.enabled \? 'Disable' : 'Enable'/)
  assert.match(mcpSource, /class:primary=\{!s\.enabled\}/)
})
