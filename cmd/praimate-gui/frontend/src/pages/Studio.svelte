<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import { activePage, openChatId, pageRevision, showToast } from '../lib/stores.js'
  import { localRoutingUnavailableMessage, supportsLocalRouting } from '../lib/localRouting.js'

  let chats = []
  let agents = []
  let clis = []
  let loading = true
  let error = ''
  let form = null
  let localOpt = null

  $: sessions = chats.filter((chat) => chat.Settings?.surface === 'studio')
  $: editorAgents = agents.filter((agent) => !agent.surfaces?.length || agent.surfaces.includes('editor'))
  $: selectedAgent = form?.agentID ? agents.find((agent) => agent.id === form.agentID) : null
  $: compatibleCLIs = !form
    ? []
    : selectedAgent?.supports?.length
      ? clis.filter((cli) => selectedAgent.supports.includes(cli.id))
      : clis
  $: localRoutable = !!form && supportsLocalRouting(form.cli)

  function agentName(chat) {
    if (!chat.AgentID) return 'No agent persona'
    return agents.find((agent) => agent.id === chat.AgentID)?.name || chat.AgentID
  }

  function fmtDate(value) {
    try { return new Date(value).toLocaleString() } catch { return value }
  }

  async function load() {
    loading = true
    try {
      const [loadedChats, loadedAgents] = await Promise.all([
        api.listChats(),
        api.listAgents().catch(() => []),
      ])
      chats = loadedChats || []
      agents = loadedAgents || []
      error = ''
    } catch (e) {
      error = String(e)
    } finally {
      loading = false
    }
  }

  async function openNew() {
    error = ''
    try {
      if (!clis.length) clis = (await api.listCLIs()) || []
      if (localOpt === null) localOpt = await api.localLLMModels().catch(() => ({ configured: false }))
      const first = clis.find((cli) => cli.available) || clis[0]
      form = {
        agentID: '', cli: first?.id || '', model: '', folder: '', useLocal: false,
        localModel: '', suggestions: [], busy: false, preflight: null, preflightChecked: false,
      }
      await cliChanged()
    } catch (e) {
      error = String(e)
    }
  }

  function invalidatePreflight() {
    if (!form) return
    form = { ...form, preflight: null, preflightChecked: false }
  }

  async function agentChanged() {
    if (!form) return
    const nextAgent = agents.find((agent) => agent.id === form.agentID)
    const allowed = nextAgent?.supports?.length
      ? clis.filter((cli) => nextAgent.supports.includes(cli.id))
      : clis
    if (!allowed.some((cli) => cli.id === form.cli && cli.available)) {
      form.cli = allowed.find((cli) => cli.available)?.id || allowed[0]?.id || ''
    }
    invalidatePreflight()
    await cliChanged()
  }

  async function cliChanged() {
    if (!form) return
    if (form.useLocal && !supportsLocalRouting(form.cli)) form.useLocal = false
    form = { ...form, suggestions: [], modelLoading: true, preflight: null, preflightChecked: false }
    const cli = form.cli
    const suggestions = cli ? await api.listCLIModels(cli).catch(() => []) : []
    if (form?.cli === cli) form = { ...form, suggestions: suggestions || [], modelLoading: false }
  }

  async function pickFolder() {
    try {
      const folder = await api.pickFolder()
      if (folder && form) {
        form.folder = folder
        invalidatePreflight()
      }
    } catch (e) {
      error = String(e)
    }
  }

  async function launch() {
    if (!form || form.busy) return
    if (!form.folder) {
      error = 'Pick a project folder first.'
      return
    }
    if (!form.cli) {
      error = 'Install and select a CLI first.'
      return
    }
    const local = form.useLocal && localOpt?.configured
    const endpoint = local ? localOpt.endpoint : ''
    const localModel = local ? form.localModel.trim() : ''
    const model = local ? '' : form.model.trim()
    form.busy = true
    error = ''
    try {
      if (!form.preflightChecked) {
        const check = await api.preflightExecution(form.agentID, 'studio', form.cli, model, 'edits', form.folder, endpoint, localModel)
        form = { ...form, busy: false, preflight: check, preflightChecked: true }
        if (!check?.ok) {
          error = (check?.issues || []).filter((issue) => issue.severity === 'error').map((issue) => issue.message).join('\n') || 'Studio preflight failed.'
          return
        }
        if ((check?.issues || []).length) return
        form.busy = true
      }
      const cli = form.cli
      const folder = form.folder
      showToast({ title: 'Opening Studio', message: `Starting ${agentName({ AgentID: form.agentID })} with ${cli} in ${folder}`, tone: 'busy', duration: 0, dismissible: false })
      await api.openEditorWindow(folder, form.agentID, cli, model, '', endpoint, '', localModel)
      form = null
      await load()
      showToast({ title: 'Studio opened', message: 'The session is ready in a separate Studio window.', tone: 'ok' })
    } catch (e) {
      error = String(e)
      showToast({ title: 'Studio failed to open', message: String(e), tone: 'err', duration: 0 })
      if (form) form.busy = false
    }
  }

  async function reopen(chat) {
    error = ''
    try {
      showToast({ title: 'Opening Studio', message: chat.Title, tone: 'busy', duration: 0, dismissible: false })
      await api.openEditorWindow(chat.WorkspacePath, chat.AgentID || '', chat.CLIAgent || '', chat.Settings?.model || '', chat.ID, '', '', '')
      showToast({ title: 'Studio reopened', message: chat.Title, tone: 'ok' })
    } catch (e) {
      error = String(e)
      showToast({ title: 'Studio failed to open', message: String(e), tone: 'err', duration: 0 })
    }
  }

  function transcript(chat) {
    openChatId.set(chat.ID)
    activePage.set('chats')
    pageRevision.update((value) => value + 1)
  }

  async function remove(chat) {
    if (!confirm(`Delete Studio session "${chat.Title}"? This removes its transcript too.`)) return
    try {
      await api.deleteChat(chat.ID)
      await load()
    } catch (e) {
      error = String(e)
    }
  }

  onMount(load)
</script>

<div class="row" style="margin-bottom:4px">
  <h1 class="grow" style="margin:0">Studio</h1>
  <button class="btn" on:click={load} disabled={loading}>{loading ? 'Refreshing…' : 'Refresh'}</button>
  <button class="btn primary" on:click={openNew}>+ New Studio</button>
</div>
<p class="subtitle">Document-focused agent sessions live here. Reopen an existing workspace or start a new Studio with an optional agent persona.</p>

{#if error}<div class="banner">{error}</div>{/if}
{#if loading && !chats.length}<div class="empty">Loading Studio sessions…</div>{/if}
{#if !loading && sessions.length === 0}<div class="empty">No Studio sessions yet. Press “New Studio” to open one.</div>{/if}

{#each sessions as chat}
  <div class="card row">
    <div class="grow">
      <div class="card-title">{chat.Title}</div>
      <div class="card-sub">{agentName(chat)} · {chat.CLIAgent}{chat.Settings?.model ? ` · ${chat.Settings.model}` : ''} · {fmtDate(chat.UpdatedAt)}</div>
      <div class="card-sub mono studio-path">{chat.WorkspacePath}</div>
    </div>
    <button class="btn primary" on:click={() => reopen(chat)}>Open Studio</button>
    <button class="btn" on:click={() => transcript(chat)}>Transcript</button>
    <button class="btn danger" on:click={() => remove(chat)}>Delete</button>
  </div>
{/each}

{#if form}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="modal-backdrop" on:click|self={() => !form.busy && (form = null)}>
    <div class="modal-content studio-modal" role="dialog" aria-modal="true" aria-labelledby="new-studio-title">
      <h2 id="new-studio-title">New Studio</h2>
      <p class="subtitle">Choose the workspace, optional persona, and CLI used by the Studio assistant.</p>

      <label class="lbl" for="studio-agent">Agent persona</label>
      <select id="studio-agent" class="field" bind:value={form.agentID} on:change={agentChanged}>
        <option value="">No agent persona</option>
        {#each editorAgents as agent}<option value={agent.id}>{agent.name}</option>{/each}
      </select>

      <label class="lbl" for="studio-cli">CLI</label>
      <select id="studio-cli" class="field" bind:value={form.cli} on:change={cliChanged}>
        {#each compatibleCLIs as cli}
          <option value={cli.id} disabled={!cli.available}>{cli.label}{cli.available ? '' : ' — not installed'}</option>
        {/each}
      </select>

      {#if localOpt?.configured && localRoutable}
        <label class="row local-toggle">
          <input type="checkbox" bind:checked={form.useLocal} on:change={invalidatePreflight} />
          <span>Use the local LLM <span class="card-sub mono">{localOpt.endpoint}</span></span>
        </label>
      {:else if localOpt?.configured}
        <div class="card-sub" style="margin-top:10px">{localRoutingUnavailableMessage(form.cli)}</div>
      {/if}

      {#if form.useLocal && localOpt?.configured && localRoutable}
        <label class="lbl" for="studio-local-model">Local model</label>
        <input id="studio-local-model" class="field mono" list="studio-local-models" bind:value={form.localModel} on:input={invalidatePreflight} placeholder="model on your endpoint" />
        <datalist id="studio-local-models">{#each localOpt.models || [] as model}<option value={model}></option>{/each}</datalist>
      {:else}
        <label class="lbl" for="studio-model">Model <span class="card-sub">(blank = CLI default)</span></label>
        <input id="studio-model" class="field mono" list="studio-models" bind:value={form.model} on:input={invalidatePreflight} />
        <datalist id="studio-models">{#each form.suggestions || [] as model}<option value={model}></option>{/each}</datalist>
        {#if form.modelLoading}<div class="card-sub">Loading models…</div>{/if}
      {/if}

      <label class="lbl" for="studio-folder">Project folder *</label>
      <div class="row">
        <input id="studio-folder" class="field grow mono" bind:value={form.folder} on:input={invalidatePreflight} placeholder="folder the Studio may read and edit" />
        <button class="btn" on:click={pickFolder}>Browse…</button>
      </div>

      {#if form.preflight?.issues?.length}
        <div class="preflight">
          {#each form.preflight.issues as issue}<div class="banner">{issue.severity === 'error' ? 'Blocked' : 'Warning'}: {issue.message}</div>{/each}
          {#if form.preflight.ok}<div class="card-sub">Review the warnings, then press Open Studio again.</div>{/if}
        </div>
      {/if}

      <div class="row actions">
        <button class="btn" on:click={() => (form = null)} disabled={form.busy}>Cancel</button>
        <button class="btn primary" on:click={launch} disabled={form.busy}>{form.busy ? 'Opening…' : 'Open Studio'}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .studio-path { margin-top: 3px; overflow-wrap: anywhere; }
  .studio-modal { max-width: 640px; max-height: 90vh; overflow-y: auto; }
  .local-toggle { margin-top: 12px; cursor: pointer; }
  .preflight { margin-top: 12px; }
  .actions { justify-content: flex-end; margin-top: 16px; }
</style>
