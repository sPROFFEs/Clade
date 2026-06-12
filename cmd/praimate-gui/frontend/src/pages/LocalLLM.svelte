<script>
  // Local LLM — the global default self-hosted endpoint (Ollama,
  // GPUStack, vLLM, LiteLLM…), mirroring the TUI's local-LLM screen.
  // Saved into the shared launcher config, so the TUI's new-chat
  // wizard offers it as "use the saved endpoint", and the backup
  // syncs it across machines.
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let d = { endpoint: '', apiKey: '', wireApi: '', contextTokens: 0, outputTokens: 0 }
  let error = ''
  let notice = ''
  let testing = false
  let saving = false
  let models = null

  async function load() {
    try {
      d = (await api.getLocalLLM()) || d
    } catch (e) {
      error = String(e)
    }
  }

  async function test() {
    testing = true
    error = ''
    models = null
    try {
      models = (await api.testLocalLLM(d.endpoint, d.apiKey)) || []
    } catch (e) {
      error = String(e)
    } finally {
      testing = false
    }
  }

  async function save() {
    saving = true
    error = ''
    try {
      d.contextTokens = Number(d.contextTokens) || 0
      d.outputTokens = Number(d.outputTokens) || 0
      await api.setLocalLLM(d)
      notice = 'Saved — the TUI wizard and other machines (via backup) will see it.'
    } catch (e) {
      error = String(e)
    } finally {
      saving = false
    }
  }

  async function clearAll() {
    d = { endpoint: '', apiKey: '', wireApi: '', contextTokens: 0, outputTokens: 0 }
    await save()
    notice = 'Cleared the saved endpoint.'
  }

  onMount(load)
</script>

<h1>Local LLM</h1>
<p class="subtitle">Default self-hosted OpenAI-compatible endpoint (Ollama, GPUStack, vLLM, LiteLLM…). Chats and the TUI's per-chat wizard reuse it so you never retype the URL.</p>

{#if error}<div class="banner">{error}</div>{/if}
{#if notice}<div class="card card-sub">{notice}</div>{/if}

<div class="card">
  <label class="lbl">Endpoint URL</label>
  <input class="field mono" bind:value={d.endpoint} placeholder="http://localhost:11434" />

  <label class="lbl">API key (optional)</label>
  <input class="field mono" type="password" bind:value={d.apiKey} placeholder="empty for plain Ollama" />

  <label class="lbl">Codex wire API</label>
  <select class="field" style="max-width:260px" bind:value={d.wireApi}>
    <option value="">auto</option>
    <option value="responses">responses (codex ≥0.130)</option>
    <option value="chat">chat completions</option>
  </select>

  <div class="row">
    <div class="grow">
      <label class="lbl">Context tokens hint</label>
      <input class="field" type="number" bind:value={d.contextTokens} placeholder="e.g. 32768" />
    </div>
    <div class="grow">
      <label class="lbl">Output tokens hint</label>
      <input class="field" type="number" bind:value={d.outputTokens} placeholder="e.g. 8192" />
    </div>
  </div>

  <div class="row" style="margin-top:14px">
    <button class="btn" on:click={test} disabled={testing || !d.endpoint}>{testing ? 'Probing…' : 'Test connection'}</button>
    <button class="btn primary" on:click={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
    <button class="btn danger" on:click={clearAll}>Clear</button>
  </div>
</div>

{#if models !== null}
  <div class="card">
    <div class="card-title">{models.length} model(s) at {d.endpoint}</div>
    <div style="margin-top:6px">
      {#each models as m}<span class="pill mono">{m}</span>{/each}
      {#if models.length === 0}<span class="card-sub">endpoint reachable, but the model list is empty</span>{/if}
    </div>
  </div>
{/if}
