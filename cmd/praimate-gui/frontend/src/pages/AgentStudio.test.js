import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const source = await readFile(new URL('./AgentStudio.svelte', import.meta.url), 'utf8')

test('Agent Studio error banners can be dismissed in both creation and editor views', () => {
  assert.ok((source.match(/aria-label="Cerrar error"/g) || []).length >= 2)
  assert.match(source, /function dismissError\(\)\s*\{\s*error = ''/)
})

test('new agent entry preserves guided, manual, and import creation paths', () => {
  assert.match(source, /Guided creation/)
  assert.match(source, /Manual creation/)
  assert.match(source, /Import agent/)
  assert.match(source, /api\.previewGuidedAgent\(guidedRequest\(\)\)/)
  assert.match(source, /api\.createGuidedAgent\(guidedRequest\(\)\)/)
})

test('runtime manifests are edited separately from agent YAML and knowledge files', () => {
  assert.match(source, /const RUNTIME = '__runtime__'/)
  assert.match(source, /api\.saveAgentRuntimeJSON\(agentId, body\)/)
  assert.match(source, /runtimeConfigured\s*\?\s*await api\.agentRuntimeJSON\(agentId\)\s*:\s*await api\.enableAgentRuntime\(agentId\)/)
  assert.match(source, /Advanced capabilities/)
})

test('managed runs expose lifecycle state, working memory, and artifacts', () => {
  assert.match(source, /api\.listManagedRuns\(agentId\)/)
  assert.match(source, /api\.managedRunDetails\(runId\)/)
  assert.match(source, /api\.managedArtifactText\(selectedRun\.id, name\)/)
  assert.match(source, /Managed runs/)
  assert.match(source, /Working memory/)
  assert.match(source, /Artifacts/)
})

test('a successful requirements action clears a previous Agent Studio error', () => {
  assert.match(
    source,
    /notice = `Requirements script \$\{requirementsScript\} attached\.`\s*\n\s*dismissError\(\)/,
  )
})
