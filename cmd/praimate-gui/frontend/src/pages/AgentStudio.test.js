import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const source = await readFile(new URL('./AgentStudio.svelte', import.meta.url), 'utf8')

test('Agent Studio error banners can be dismissed in both creation and editor views', () => {
  assert.equal((source.match(/aria-label="Cerrar error"/g) || []).length, 2)
  assert.match(source, /function dismissError\(\)\s*\{\s*error = ''/)
})

test('a successful requirements action clears a previous Agent Studio error', () => {
  assert.match(
    source,
    /notice = `Requirements script \$\{requirementsScript\} attached\.`\s*\n\s*dismissError\(\)/,
  )
})
