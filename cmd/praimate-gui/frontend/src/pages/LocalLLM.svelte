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

  // --- route the config-file CLIs (opencode / codex) to the endpoint ---
  // claude/openclaude route by env (the per-chat toggle); opencode/codex
  // read a provider block from their own config files, which this writes.
  let cliStatus = { codex: false, opencode: false, deepseek: false }
  let applyModel = ''
  let applyBusy = ''

  async function refreshStatus() {
    try { cliStatus = (await api.localCLIStatus()) || cliStatus } catch {}
  }

  async function applyCLI(cli) {
    applyBusy = cli
    error = ''
    try {
      notice = await api.applyLocalToCLI(cli, applyModel.trim())
      await refreshStatus()
    } catch (e) {
      error = String(e)
    } finally {
      applyBusy = ''
    }
  }

  async function disableCLI(cli) {
    applyBusy = cli
    error = ''
    try {
      notice = await api.disableLocalForCLI(cli)
      await refreshStatus()
    } catch (e) {
      error = String(e)
    } finally {
      applyBusy = ''
    }
  }

  onMount(async () => {
    await load()
    await refreshStatus()
  })
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

<h1 style="font-size:16px; margin-top:24px">Route CLIs to the local model</h1>
<p class="subtitle" style="margin-top:-6px">
  <strong>claude</strong> and <strong>openclaude</strong> route by environment — just tick “Use the local LLM” when you start a chat/code/studio session, no setup here.
  <strong>opencode / praimate-code</strong>, <strong>codex</strong> and <strong>deepseek</strong> read their endpoint from their own config files, so apply it once here and every session (chat, code, studio) uses the local model.
  <strong>gemini</strong> isn't supported (its auth schema changes per release).
</p>
<div class="card">
  <label class="lbl">Model to route to</label>
  <input class="field mono" style="max-width:420px" list="apply-models" bind:value={applyModel} placeholder="e.g. qwen2.5-coder:14b" />
  <datalist id="apply-models">{#each models || [] as m}<option value={m}></option>{/each}</datalist>
  <div class="card-sub" style="margin-top:4px">Tip: press “Test connection” above to populate the model list.</div>

  <div class="row" style="margin-top:14px; flex-wrap:wrap; gap:8px">
    <div class="grow">
      <strong>opencode / praimate-code</strong>
      {#if cliStatus.opencode}<span class="pill ok">routed to local</span>{:else}<span class="pill">cloud default</span>{/if}
      <span class="card-sub">(shared config — one apply routes both)</span>
    </div>
    <button class="btn primary" on:click={() => applyCLI('opencode')} disabled={!!applyBusy || !d.endpoint}>{applyBusy === 'opencode' ? 'Applying…' : 'Apply'}</button>
    <button class="btn" on:click={() => disableCLI('opencode')} disabled={!!applyBusy || !cliStatus.opencode}>Disable</button>
  </div>

  <div class="row" style="margin-top:10px; flex-wrap:wrap; gap:8px">
    <div class="grow">
      <strong>codex</strong>
      {#if cliStatus.codex}<span class="pill ok">routed to local</span>{:else}<span class="pill">cloud default</span>{/if}
      <span class="card-sub">(needs an OpenAI /v1/responses-compatible endpoint)</span>
    </div>
    <button class="btn primary" on:click={() => applyCLI('codex')} disabled={!!applyBusy || !d.endpoint}>{applyBusy === 'codex' ? 'Applying…' : 'Apply'}</button>
    <button class="btn" on:click={() => disableCLI('codex')} disabled={!!applyBusy || !cliStatus.codex}>Disable</button>
  </div>

  <div class="row" style="margin-top:10px; flex-wrap:wrap; gap:8px">
    <div class="grow">
      <strong>deepseek</strong>
      {#if cliStatus.deepseek}<span class="pill ok">routed to local</span>{:else}<span class="pill">cloud default</span>{/if}
    </div>
    <button class="btn primary" on:click={() => applyCLI('deepseek')} disabled={!!applyBusy || !d.endpoint}>{applyBusy === 'deepseek' ? 'Applying…' : 'Apply'}</button>
    <button class="btn" on:click={() => disableCLI('deepseek')} disabled={!!applyBusy || !cliStatus.deepseek}>Disable</button>
  </div>
</div>
