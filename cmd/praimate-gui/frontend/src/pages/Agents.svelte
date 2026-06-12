<script>
  // Agents — see / edit / add / delete agents, and launch them on any
  // allowed surface: Chat (interpreter), Terminal (live CLI), or
  // Editor (document studio). Editing happens on the YAML wire format
  // in the embedded CodeMirror editor; saving re-validates through the
  // same parser `praimate agent import` uses.
  import { onMount, onDestroy } from 'svelte'
  import { api, onTurn } from '../lib/api.js'
  import { activePage, openChatId } from '../lib/stores.js'
  import CodeEditor from '../lib/CodeEditor.svelte'

  let agents = []
  let error = ''
  let notice = ''

  // view: 'list' | 'edit' | 'run'
  let view = 'list'

  function allows(a, surface) {
    return !a.surfaces?.length || a.surfaces.includes(surface)
  }

  async function load() {
    try {
      agents = (await api.listAgents()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
  }

  // --- launch surfaces -------------------------------------------------------

  let launchCli = {}

  async function launchChat(a) {
    try {
      const cli = launchCli[a.id] || a.supports?.[0] || 'claude'
      const c = await api.startChat(a.id, cli, '')
      openChatId.set(c.ID)
      activePage.set('chats')
    } catch (e) {
      error = String(e)
    }
  }

  async function launchTerminal(a) {
    try {
      const folder = await api.pickFolder()
      if (!folder) return
      const cli = launchCli[a.id] || a.supports?.[0] || 'claude'
      await api.startTerminal(a.id, cli, folder)
      activePage.set('code')
    } catch (e) {
      error = String(e)
    }
  }

  async function launchEditor(a) {
    try {
      const folder = await api.pickFolder()
      if (!folder) return
      const cli = a ? (launchCli[a.id] || a.supports?.[0] || 'claude') : 'claude'
      await api.openEditorWindow(folder, a ? a.id : '', cli, '')
      notice = 'Studio window opened.'
    } catch (e) {
      error = String(e)
    }
  }

  // --- YAML editor -----------------------------------------------------------

  let yamlText = ''
  let editing = null // agent being edited, or {id:''} for new
  let editorRef
  let saving = false

  async function edit(a) {
    error = ''
    try {
      yamlText = await api.agentYAML(a.id)
      editing = a
      view = 'edit'
    } catch (e) {
      error = String(e)
    }
  }

  async function createNew() {
    error = ''
    try {
      yamlText = await api.newAgentTemplateYAML()
      editing = { id: '', name: 'new agent' }
      view = 'edit'
    } catch (e) {
      error = String(e)
    }
  }

  async function save() {
    saving = true
    error = ''
    try {
      const body = editorRef?.getValue() ?? yamlText
      const saved = await api.saveAgentYAML(body)
      notice = `Saved ${saved.name}`
      view = 'list'
      await load()
    } catch (e) {
      error = String(e)
    } finally {
      saving = false
    }
  }

  async function importYAML() {
    try {
      const a = await api.importAgentDialog()
      if (a) { notice = `Imported ${a.name}`; await load() }
    } catch (e) { error = String(e) }
  }

  async function exportYAML(a) {
    try {
      const path = await api.exportAgentDialog(a.id)
      if (path) notice = `Exported to ${path}`
    } catch (e) { error = String(e) }
  }

  async function remove(a) {
    if (!confirm(`Delete agent "${a.name}"?`)) return
    try {
      await api.deleteAgent(a.id)
      await load()
    } catch (e) { error = String(e) }
  }

  // --- workflow run (ported from the old Run page) ---------------------------

  let runAgent = null
  let workflow = null
  let cli = ''
  let cwd = ''
  let inputs = {}
  let privacyCounts = null
  let running = false
  let turns = []
  let result = null
  let unsubscribe = () => {}

  function openRun(a) {
    runAgent = a
    cli = a.supports?.[0] || 'claude'
    const def = a.workflows?.find((w) => w.name === a.default_workflow)
    workflow = def || a.workflows?.[0] || null
    inputs = {}
    privacyCounts = null
    result = null
    turns = []
    if (workflow) for (const inp of workflow.inputs || []) inputs[inp.name] = inp.default || ''
    view = 'run'
  }

  function pickWorkflow(w) {
    workflow = w
    inputs = {}
    privacyCounts = null
    for (const inp of w.inputs || []) inputs[inp.name] = inp.default || ''
  }

  async function chooseFolder() {
    try { const p = await api.pickFolder(); if (p) cwd = p } catch (e) { error = String(e) }
  }

  async function review() {
    try {
      privacyCounts = (await api.privacyPreview(Object.values(inputs).join(' '))) || {}
    } catch (e) { error = String(e) }
  }

  async function startRun() {
    running = true
    turns = []
    result = null
    error = ''
    unsubscribe = onTurn((t) => { turns = [...turns, t] })
    try {
      result = await api.runWorkflow(runAgent.id, workflow.name, cli, cwd, inputs)
    } catch (e) {
      error = String(e)
    } finally {
      unsubscribe()
      running = false
      privacyCounts = null
    }
  }

  function backToList() {
    view = 'list'
    runAgent = null; workflow = null; inputs = {}; turns = []; result = null; privacyCounts = null
    editing = null
  }

  onMount(load)
  onDestroy(() => unsubscribe())

  $: matchTotal = privacyCounts ? Object.values(privacyCounts).reduce((a, b) => a + b, 0) : 0
</script>

{#if view === 'edit'}
  <div class="row" style="margin-bottom: 12px">
    <button class="btn" on:click={backToList}>← Agents</button>
    <strong class="grow">{editing?.id ? `Edit ${editing.name}` : 'New agent'}</strong>
    <button class="btn primary" on:click={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
  </div>
  {#if error}<div class="banner">{error}</div>{/if}
  <div class="agent-editor">
    <CodeEditor bind:this={editorRef} value={yamlText} lang="yaml" />
  </div>
{:else if view === 'run' && runAgent}
  {#if running}
    <div class="card">
      <div class="card-title">Running {runAgent.name} · {workflow.name} on {cli}…</div>
      <div class="card-sub">Streaming turns as they complete.</div>
    </div>
    {#each turns as t}
      <div class="msg user"><div class="who">you (turn {t.index + 1})</div>{t.user_msg}</div>
      <div class="msg assistant"><div class="who">assistant · {t.duration_ms}ms</div>{t.reply}</div>
    {/each}
  {:else if result}
    <div class="row" style="margin-bottom:14px">
      <button class="btn" on:click={backToList}>← Agents</button>
      <span class="pill" class:ok={result.outcome === 'completed'} class:err={result.outcome !== 'completed'}>{result.outcome}</span>
      {#if result.chat_id}<span class="pill">saved: {result.chat_id}</span>{/if}
    </div>
    {#if result.error}<div class="banner">{result.error}</div>{/if}
    {#each result.turns || [] as t}
      <div class="msg user"><div class="who">you (turn {t.index + 1})</div>{t.user_msg}</div>
      <div class="msg assistant"><div class="who">assistant · {t.duration_ms}ms</div>{t.reply}</div>
    {/each}
  {:else}
    <div class="row" style="margin-bottom:14px">
      <button class="btn" on:click={backToList}>← Agents</button>
      <strong>{runAgent.name} — run workflow</strong>
    </div>
    {#if error}<div class="banner">{error}</div>{/if}
    {#if (runAgent.workflows || []).length > 1}
      <label class="lbl">Workflow</label>
      <div class="row" style="flex-wrap:wrap">
        {#each runAgent.workflows as w}
          <button class="btn" class:primary={workflow?.name === w.name} on:click={() => pickWorkflow(w)}>{w.name}</button>
        {/each}
      </div>
    {/if}
    {#if workflow}
      <label class="lbl">CLI</label>
      <select class="field" bind:value={cli} style="max-width:240px">
        {#each runAgent.supports || [] as s}<option value={s}>{s}</option>{/each}
      </select>
      <label class="lbl">Working folder</label>
      <div class="row">
        <input class="field grow" bind:value={cwd} placeholder="(defaults to app cwd)" />
        <button class="btn" on:click={chooseFolder}>Browse…</button>
      </div>
      {#each workflow.inputs || [] as inp}
        <label class="lbl">{inp.prompt || inp.name}{inp.required ? ' *' : ''}</label>
        <input class="field" bind:value={inputs[inp.name]} placeholder={inp.placeholder || ''} />
      {/each}
      {#if privacyCounts === null}
        <div style="margin-top:18px"><button class="btn primary" on:click={review}>Continue</button></div>
      {:else}
        <div class="card" style="margin-top:18px">
          {#if matchTotal === 0}
            <div class="card-title">Privacy scan: clean</div>
          {:else}
            <div class="card-title" style="color:var(--warn)">Privacy scan: {matchTotal} match(es)</div>
            <div class="card-sub">
              Sent REDACTED to the CLI:
              {#each Object.entries(privacyCounts) as [cat, n]}<span class="pill warn">{cat} ×{n}</span>{/each}
            </div>
          {/if}
          <div class="row" style="margin-top:10px">
            <button class="btn primary" on:click={startRun}>Run workflow</button>
            <button class="btn" on:click={() => (privacyCounts = null)}>Back</button>
          </div>
        </div>
      {/if}
    {/if}
  {/if}
{:else}
  <div class="row" style="margin-bottom: 4px">
    <h1 class="grow" style="margin:0">Agents</h1>
    <button class="btn" on:click={() => launchEditor(null)} title="Open the document studio without an agent persona">Open studio…</button>
    <button class="btn" on:click={importYAML}>Import YAML…</button>
    <button class="btn primary" on:click={createNew}>+ New agent</button>
  </div>
  <p class="subtitle">Portable YAML agents, shared with the TUI. Launch them in a Chat, a live Terminal, or the document Studio — each agent declares which surfaces it allows.</p>

  {#if error}<div class="banner">{error}</div>{/if}
  {#if notice}<div class="card card-sub">{notice}</div>{/if}

  {#each agents as a}
    <div class="card">
      <div class="row">
        <div class="grow">
          <div class="card-title">{a.name} <span class="card-sub mono">({a.id})</span></div>
          <div class="card-sub">{a.description?.split('\n')[0]}</div>
        </div>
        {#if (a.supports || []).length > 1}
          <select class="field" style="max-width:130px" bind:value={launchCli[a.id]}>
            {#each a.supports as s}<option value={s}>{s}</option>{/each}
          </select>
        {/if}
        {#if allows(a, 'chat')}<button class="btn primary" on:click={() => launchChat(a)}>Chat</button>{/if}
        {#if allows(a, 'terminal')}<button class="btn" on:click={() => launchTerminal(a)}>Terminal</button>{/if}
        {#if allows(a, 'editor')}<button class="btn" on:click={() => launchEditor(a)}>Studio</button>{/if}
      </div>
      <div class="row" style="margin-top: 8px">
        <div class="grow">
          {#each a.supports || [] as s}<span class="pill">{s}</span>{/each}
          {#each a.surfaces?.length ? a.surfaces : ['chat', 'terminal', 'editor'] as s}<span class="pill ok">{s}</span>{/each}
          {#each a.workflows || [] as w}<span class="pill warn">{w.name}</span>{/each}
        </div>
        {#if (a.workflows || []).length > 0}
          <button class="btn" on:click={() => openRun(a)}>Run workflow</button>
        {/if}
        <button class="btn" on:click={() => edit(a)}>Edit</button>
        <button class="btn" on:click={() => exportYAML(a)}>Export</button>
        <button class="btn danger" on:click={() => remove(a)}>Delete</button>
      </div>
    </div>
  {/each}
{/if}

<style>
  .agent-editor { height: calc(100vh - 140px); }
</style>
