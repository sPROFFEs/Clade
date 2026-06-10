<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let catalogue = []
  let servers = []
  let error = ''
  let connecting = null // catalogue entry being connected
  let apiKey = ''

  async function load() {
    try {
      catalogue = (await api.mcpCatalogue()) || []
      servers = (await api.mcpServers()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
  }

  function connectedKey(key) {
    return servers.some((s) => s.catalogue_key === key)
  }

  async function connect() {
    try {
      await api.connectMCP(connecting.key, apiKey)
      connecting = null
      apiKey = ''
      await load()
    } catch (e) {
      error = String(e)
    }
  }

  async function toggle(s) {
    try { await api.setMCPEnabled(s.id, !s.enabled); await load() } catch (e) { error = String(e) }
  }

  async function remove(s) {
    if (!confirm(`Disconnect ${s.name}?`)) return
    try { await api.deleteMCPServer(s.id); await load() } catch (e) { error = String(e) }
  }

  onMount(load)
</script>

<h1>MCP</h1>
<p class="subtitle">Connect MCP providers once; agents that declare them get per-CLI config written at launch.</p>

{#if error}<div class="banner">{error}</div>{/if}

{#if connecting}
  <div class="card">
    <div class="card-title">Connect {connecting.name}</div>
    <div class="card-sub">{connecting.description}</div>
    {#if connecting.auth?.type === 'api_key'}
      <label class="lbl">{connecting.auth.label || 'API key'}</label>
      <input class="field" type="password" bind:value={apiKey} placeholder="paste key" />
    {:else}
      <div class="card-sub" style="margin-top:8px">
        OAuth providers authenticate inside the CLI on first use (e.g. /mcp in Claude Code).
      </div>
    {/if}
    <div class="row" style="margin-top:12px">
      <button class="btn primary" on:click={connect}>Connect</button>
      <button class="btn" on:click={() => { connecting = null; apiKey = '' }}>Cancel</button>
    </div>
  </div>
{/if}

{#if servers.length > 0}
  <h1 style="font-size:16px">Connected</h1>
  {#each servers as s}
    <div class="card row">
      <div class="grow">
        <div class="card-title">{s.name} <span class="pill">{s.transport}</span></div>
        <div class="card-sub mono">{s.url || s.command}</div>
      </div>
      <button class="btn" class:primary={s.enabled} on:click={() => toggle(s)}>{s.enabled ? 'Enabled' : 'Disabled'}</button>
      <button class="btn danger" on:click={() => remove(s)}>Remove</button>
    </div>
  {/each}
{/if}

<h1 style="font-size:16px; margin-top:20px">Catalogue</h1>
{#each catalogue as entry}
  <div class="card row">
    <div class="grow">
      <div class="card-title">{entry.name}</div>
      <div class="card-sub">{entry.description}</div>
    </div>
    {#if connectedKey(entry.key)}
      <span class="pill ok">connected</span>
    {:else}
      <button class="btn" on:click={() => (connecting = entry)}>Connect</button>
    {/if}
  </div>
{/each}
