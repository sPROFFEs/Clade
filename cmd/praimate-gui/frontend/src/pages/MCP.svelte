<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let catalogue = []
  let servers = []
  let error = ''
  let connecting = null // catalogue entry being connected
  let apiKey = ''

  // custom-server form
  let showCustom = false
  let cName = ''
  let cTransport = 'stdio'
  let cCommand = ''
  let cURL = ''
  let cEnv = ''

  async function addCustom() {
    if (!cName.trim()) { error = 'Custom MCP needs a name'; return }
    try {
      await api.addCustomMCP(cName.trim(), cTransport, cCommand.trim(), cURL.trim(), cEnv)
      showCustom = false
      cName = ''; cCommand = ''; cURL = ''; cEnv = ''
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
<p class="subtitle">
  Connect MCP providers once; agents that declare them get per-CLI
  config written at launch. <strong>The recommended path is to add a
  custom server</strong> pointing at a process you control (a local
  binary, a docker container, your own HTTP endpoint) — that's the
  configuration the maintainer dogfoods, and it avoids handing API keys
  to third-party gateways. The catalogue further down is convenience
  for hosted services; pick from it only when you actually want to
  share credentials with that vendor.
</p>

{#if error}<div class="banner">{error}</div>{/if}

<div class="row" style="margin-bottom:12px">
  <button class="btn primary" on:click={() => (showCustom = !showCustom)}>
    {showCustom ? 'Cancel' : '+ Add custom MCP server (recommended)'}
  </button>
</div>

{#if showCustom}
  <div class="card">
    <div class="card-title">Add a custom MCP server</div>
    <div class="card-sub">For locally-run or self-hosted servers not in the catalogue (e.g. hexstrike-ai).</div>
    <label class="lbl">Name</label>
    <input class="field" bind:value={cName} placeholder="HexStrike AI" />
    <label class="lbl">Transport</label>
    <select class="field" style="max-width:200px" bind:value={cTransport}>
      <option value="stdio">stdio (local command)</option>
      <option value="http">http</option>
      <option value="sse">sse</option>
    </select>
    {#if cTransport === 'stdio'}
      <label class="lbl">Command (may include args)</label>
      <input class="field mono" bind:value={cCommand} placeholder="hexstrike-mcp --port 9000" />
    {:else}
      <label class="lbl">URL</label>
      <input class="field mono" bind:value={cURL} placeholder="http://127.0.0.1:9000/mcp" />
    {/if}
    <label class="lbl">Environment (KEY=VALUE per line — tokens, etc.)</label>
    <textarea class="field mono" rows="3" bind:value={cEnv} placeholder="HEXSTRIKE_TOKEN=..."></textarea>
    <div class="row" style="margin-top:10px">
      <button class="btn primary" on:click={addCustom}>Add server</button>
    </div>
  </div>
{/if}

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

<h1 style="font-size:16px; margin-top:20px">Catalogue (third-party hosted)</h1>
<p class="subtitle">
  Hosted MCP servers PrAImate ships entries for. Each one means handing
  your prompts (and any API key you paste) to the listed vendor.
  Prefer the custom-server flow above when a self-hosted alternative
  exists.
</p>
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
