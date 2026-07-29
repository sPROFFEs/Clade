<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { commandForMCPForm, envForMCPForm } from '../lib/mcpForm.js'

  let catalogue = []
  let servers = []
  let error = ''

  // custom-server form
  let showCustom = false
  let cName = ''
  let cTransport = 'stdio'
  let cCommand = ''
  let cURL = ''
  let cEnv = ''
  let editingID = ''

  function resetForm() {
    showCustom = false
    editingID = ''
    cName = ''
    cTransport = 'stdio'
    cCommand = ''
    cURL = ''
    cEnv = ''
  }

  function openAdd() {
    if (showCustom && !editingID) {
      resetForm()
      return
    }
    resetForm()
    showCustom = true
  }

  function edit(s) {
    editingID = s.id
    cName = s.name || ''
    cTransport = s.transport || 'stdio'
    cCommand = commandForMCPForm(s)
    cURL = s.url || ''
    cEnv = envForMCPForm(s)
    showCustom = true
    error = ''
  }

  function configureTemplate(entry) {
    editingID = ''
    cName = entry.name
    cTransport = entry.transport || 'stdio'
    cCommand = commandForMCPForm(entry)
    cURL = entry.url || ''
    cEnv = entry.auth?.env_var ? `${entry.auth.env_var}=` : ''
    showCustom = true
    error = ''
  }

  async function saveCustom() {
    if (!cName.trim()) { error = 'Local MCP needs a name'; return }
    const command = cTransport === 'stdio' ? cCommand.trim() : ''
    const url = cTransport === 'stdio' ? '' : cURL.trim()
    try {
      if (editingID) {
        await api.updateMCPServer(editingID, cName.trim(), cTransport, command, url, cEnv)
      } else {
        await api.addCustomMCP(cName.trim(), cTransport, command, url, cEnv)
      }
      resetForm()
      await load()
    } catch (e) {
      error = String(e)
    }
  }

  async function load() {
    try {
      catalogue = (await api.mcpCatalogue()) || []
      servers = (await api.mcpServers()) || []
      error = ''
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
<p class="subtitle">
  Add MCP servers that run on this computer or on infrastructure you
  control. PrAImate writes their per-CLI configuration when an agent
  starts.
</p>

{#if error}<div class="banner">{error}</div>{/if}

<div class="row" style="margin-bottom:12px">
  <button class="btn primary" on:click={openAdd}>
    {showCustom && !editingID ? 'Cancel' : '+ Add local MCP server'}
  </button>
</div>

{#if showCustom}
  <div class="card">
    <div class="card-title">{editingID ? 'Edit local MCP server' : 'Configure a local MCP server'}</div>
    <div class="card-sub">Configure a local process, container, or endpoint hosted on infrastructure you control.</div>
    <label class="lbl" for="mcp-name">Name</label>
    <input id="mcp-name" class="field" bind:value={cName} placeholder="HexStrike AI" />
    <label class="lbl" for="mcp-transport">Transport</label>
    <select id="mcp-transport" class="field" style="max-width:200px" bind:value={cTransport}>
      <option value="stdio">stdio (local command)</option>
      <option value="http">http</option>
      <option value="sse">sse</option>
    </select>
    {#if cTransport === 'stdio'}
      <label class="lbl" for="mcp-command">Command (may include args)</label>
      <input id="mcp-command" class="field mono" bind:value={cCommand} placeholder="hexstrike-mcp --port 9000" />
    {:else}
      <label class="lbl" for="mcp-url">URL</label>
      <input id="mcp-url" class="field mono" bind:value={cURL} placeholder="http://127.0.0.1:9000/mcp" />
    {/if}
    <label class="lbl" for="mcp-environment">Environment (KEY=VALUE per line — tokens, etc.)</label>
    <textarea id="mcp-environment" class="field mono" rows="3" bind:value={cEnv} placeholder="HEXSTRIKE_TOKEN=..."></textarea>
    <div class="row" style="margin-top:10px">
      <button class="btn primary" on:click={saveCustom}>{editingID ? 'Save changes' : 'Add server'}</button>
      {#if editingID}<button class="btn" on:click={resetForm}>Cancel</button>{/if}
    </div>
  </div>
{/if}

{#if servers.length > 0}
  <h1 style="font-size:16px">Your MCP servers</h1>
  {#each servers as s}
    <div class="card row">
      <div class="grow">
        <div class="card-title">{s.name} <span class="pill">{s.transport}</span></div>
        <div class="card-sub mono">{s.url || s.command}</div>
      </div>
      <button class="btn" class:primary={s.enabled} on:click={() => toggle(s)}>{s.enabled ? 'Enabled' : 'Disabled'}</button>
      <button class="btn" on:click={() => edit(s)}>Edit</button>
      <button class="btn danger" on:click={() => remove(s)}>Remove</button>
    </div>
  {/each}
{/if}

<h1 style="font-size:16px; margin-top:20px">Local catalogue</h1>
<p class="subtitle">
  Optional MCP utilities that PrAImate launches as local processes.
</p>
{#each catalogue as entry}
  <div class="card row">
    <div class="grow">
      <div class="card-title">{entry.name}</div>
      <div class="card-sub">{entry.description}</div>
    </div>
    {#if servers.some((server) => server.id === entry.key)}
      <button class="btn" on:click={() => edit(servers.find((server) => server.id === entry.key))}>Edit</button>
    {:else}
      <button class="btn" on:click={() => configureTemplate(entry)}>Configure</button>
    {/if}
  </div>
{/each}
