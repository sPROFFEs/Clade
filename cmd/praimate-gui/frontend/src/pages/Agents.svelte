<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let agents = []
  let error = ''
  let notice = ''

  async function load() {
    try {
      agents = (await api.listAgents()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
  }

  async function importYAML() {
    try {
      const a = await api.importAgentDialog()
      if (a) { notice = `Imported ${a.name}`; await load() }
    } catch (e) {
      error = String(e)
    }
  }

  async function exportYAML(a) {
    try {
      const path = await api.exportAgentDialog(a.id)
      if (path) notice = `Exported to ${path}`
    } catch (e) {
      error = String(e)
    }
  }

  async function remove(a) {
    if (!confirm(`Delete agent "${a.name}"?`)) return
    try {
      await api.deleteAgent(a.id)
      await load()
    } catch (e) {
      error = String(e)
    }
  }

  onMount(load)
</script>

<h1>Agents</h1>
<p class="subtitle">Portable YAML agents — shareable between machines and between the TUI and GUI.</p>

{#if error}<div class="banner">{error}</div>{/if}
{#if notice}<div class="card card-sub">{notice}</div>{/if}

<div class="row" style="margin-bottom: 16px">
  <button class="btn primary" on:click={importYAML}>Import YAML…</button>
</div>

{#each agents as a}
  <div class="card">
    <div class="row">
      <div class="grow">
        <div class="card-title">{a.name} <span class="card-sub mono">({a.id})</span></div>
        <div class="card-sub">{a.description?.split('\n')[0]}</div>
      </div>
      <button class="btn" on:click={() => exportYAML(a)}>Export</button>
      <button class="btn danger" on:click={() => remove(a)}>Delete</button>
    </div>
    <div style="margin-top: 8px">
      {#each a.supports || [] as s}<span class="pill">{s}</span>{/each}
      {#each a.workflows || [] as w}<span class="pill ok">{w.name}</span>{/each}
      {#each a.mcp_servers || [] as m}<span class="pill warn">mcp:{m}</span>{/each}
    </div>
  </div>
{/each}
