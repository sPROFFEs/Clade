<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import { api } from '../lib/api.js'
  import { term, onTermData, onTermExit } from '../lib/terminal.js'
  import { pendingTerm } from '../lib/stores.js'
  import { get } from 'svelte/store'

  let agents = []
  let error = ''

  // PrAImate Code bundled CLI install state
  let codeInstalled = true
  let installing = false
  let installLog = ''

  // setup state
  let agent = null
  let cli = ''
  let model = ''
  let cwd = ''
  let sessionLabel = '' // header label for the running session

  // CLI + model pickers (shared by the clean-session card and the
  // agent-config view). clis: [{id, available, modelHint}].
  let clis = []
  let modelSuggestions = []
  $: selectedCliInfo = clis.find((c) => c.id === cli)
  $: modelSupported = !!selectedCliInfo?.modelHint

  // Local LLM (Settings → Local LLM). useLocal routes the session at the
  // configured endpoint; localModel picks from its live model list.
  let localOpt = null // { configured, endpoint, apiKey, models[], error }
  let useLocal = false
  let localModel = ''
  // CLIs a terminal can route to a local endpoint by env (matches the
  // Go terminalLocalRoutable). codex/gemini need a Chat instead.
  const LOCAL_ROUTABLE = ['claude', 'openclaude', 'opencode', 'praimate-code']
  $: localRoutable = LOCAL_ROUTABLE.includes(cli)

  async function loadModels() {
    try { modelSuggestions = (await api.listCLIModels(cli)) || [] } catch { modelSuggestions = [] }
  }
  // Refresh model suggestions whenever the chosen CLI changes.
  $: if (cli) { loadModels() }

  async function checkCode() {
    try { codeInstalled = await api.praimateCodeInstalled() } catch { codeInstalled = false }
  }

  async function installCode() {
    installing = true
    installLog = ''
    error = ''
    try {
      installLog = await api.installPraimateCode()
      await checkCode()
    } catch (e) {
      error = 'Install failed: ' + String(e)
    } finally {
      installing = false
    }
  }

  // running terminal
  let started = false
  let exited = false
  let termId = null
  let el            // xterm host div
  let xterm = null
  let fit = null
  let unsubData = () => {}
  let unsubExit = () => {}
  let ro = null

  async function load() {
    try {
      agents = (await api.listAgents()) || []
    } catch (e) {
      error = String(e)
    }
    try {
      clis = (await api.listCLIs()) || []
      // Default the clean-session CLI to the first installed one.
      if (!cli) {
        const firstAvail = clis.find((c) => c.available)
        cli = firstAvail ? firstAvail.id : (clis[0]?.id ?? 'claude')
      }
    } catch { /* clis stay empty; the select still works with defaults */ }
    try { localOpt = await api.localLLMModels() } catch { localOpt = null }
  }

  function pick(a) {
    agent = a
    cli = (a.supports && a.supports[0]) || 'claude'
    model = ''
    error = ''
  }

  // Start a clean session — no agent persona, just CLI + model + folder.
  function newClean() {
    agent = null
    model = ''
    error = ''
    if (!cli) {
      const firstAvail = clis.find((c) => c.available)
      cli = firstAvail ? firstAvail.id : (clis[0]?.id ?? 'claude')
    }
    cleanSetup = true
  }
  let cleanSetup = false

  async function chooseFolder() {
    try {
      const p = await term.pickFolder()
      if (p) cwd = p
    } catch (e) {
      error = String(e)
    }
  }

  async function launch() {
    if (!cwd) { error = 'Pick a project folder first.'; return }
    if (!cli) { error = 'Pick a CLI first.'; return }
    const local = useLocal && localOpt?.configured
    if (local && !localRoutable) {
      error = `${cli} can't route to a local endpoint from a terminal — start a Chat with this CLI instead.`
      return
    }
    error = ''
    try {
      // agent.id when launched from an agent; '' for a clean session
      // (StartTerminal skips the persona/context-file write).
      termId = await term.start(
        agent ? agent.id : '',
        cli,
        local ? '' : (modelSupported ? model.trim() : ''),
        cwd,
        local ? localOpt.endpoint : '',
        local ? localOpt.apiKey : '',
        local ? localModel.trim() : '',
      )
    } catch (e) {
      error = String(e)
      return
    }
    sessionLabel = (agent ? agent.name : cli) + (local ? ' · local' : '')
    started = true
    await tick()
    mountXterm()
  }

  function mountXterm() {
    xterm = new Terminal({
      fontFamily: 'JetBrains Mono, ui-monospace, monospace',
      fontSize: 13,
      cursorBlink: true,
      theme: { background: '#101218', foreground: '#e6e9f0' },
    })
    fit = new FitAddon()
    xterm.loadAddon(fit)
    xterm.open(el)
    fit.fit()
    xterm.focus()

    // keystrokes → PTY
    xterm.onData((d) => term.write(termId, d))
    // PTY → screen
    unsubData = onTermData(termId, (data) => xterm.write(data))
    unsubExit = onTermExit(termId, () => {
      exited = true
      xterm.write('\r\n\x1b[2m[process exited — press “New session” to start again]\x1b[0m\r\n')
    })

    // keep PTY size in sync with the pane
    const sync = () => {
      try {
        fit.fit()
        term.resize(termId, xterm.cols, xterm.rows)
      } catch { /* pane not visible yet */ }
    }
    sync()
    ro = new ResizeObserver(sync)
    ro.observe(el)
  }

  function teardown() {
    unsubData(); unsubExit()
    if (ro) { ro.disconnect(); ro = null }
    if (termId) term.close(termId)
    if (xterm) { xterm.dispose(); xterm = null }
    termId = null
  }

  function reset() {
    teardown()
    started = false
    exited = false
  }

  // Attach to a PTY the Chats page already started (a reopened TUI
  // workspace chat) instead of launching a fresh one.
  async function attachPending(p) {
    termId = p.termId
    sessionLabel = p.label
    cli = p.cli
    cwd = p.cwd
    started = true
    await tick()
    mountXterm()
    if (p.note) {
      xterm.write(`\x1b[2m[${p.note}]\x1b[0m\r\n`)
    }
  }

  onMount(() => {
    load()
    checkCode()
    const p = get(pendingTerm)
    if (p) {
      pendingTerm.set(null)
      attachPending(p)
    }
  })
  onDestroy(teardown)
</script>

<div class="row" style="margin-bottom:4px">
  <h1 class="grow" style="margin:0">Code</h1>
  {#if !started && !cleanSetup}
    <button class="btn primary" on:click={newClean}>+ New session</button>
  {/if}
</div>
<p class="subtitle">Run a CLI live in a project folder — the real tool, with streaming, tool calls, and file edits. Start a clean session on any CLI/model, or launch an agent persona.</p>

{#if error}<div class="banner">{error}</div>{/if}

{#if !started && !codeInstalled}
  <div class="card">
    <div class="card-title">PrAImate Code (bundled coding CLI) not installed</div>
    <div class="card-sub">
      PrAImate Code is our version-pinned build of OpenCode. Install it once
      (~150MB download) to use it as a CLI here and via <span class="mono">praimate code</span>.
    </div>
    <div class="row" style="margin-top:10px">
      <button class="btn primary" on:click={installCode} disabled={installing}>
        {installing ? 'Installing…' : 'Install PrAImate Code'}
      </button>
    </div>
    {#if installLog}<pre class="mono" style="margin-top:10px; max-height:140px; overflow:auto">{installLog}</pre>{/if}
  </div>
{/if}

{#if !started}
  {#if cleanSetup}
    <!-- New clean session: pick CLI + model + folder, no agent persona. -->
    <div class="card">
      <div class="card-title">New code session</div>
      <div class="card-sub">Run a CLI live in your project folder — no agent persona or context file is written. Pick the CLI and (optionally) the model.</div>
      <label class="lbl">CLI</label>
      <select class="field" style="max-width:320px" bind:value={cli}>
        {#if clis.length === 0}<option value="">probing installed CLIs…</option>{/if}
        {#each clis as c}
          <option value={c.id} disabled={!c.available}>{c.label || c.id}{c.available ? '' : ' — not installed'}</option>
        {/each}
      </select>
      {#if localOpt?.configured}
        <label class="row" style="margin-top:12px; gap:8px; cursor:pointer">
          <input type="checkbox" bind:checked={useLocal} disabled={!localRoutable} />
          <span>Use the local LLM from Settings <span class="card-sub mono">{localOpt.endpoint}</span></span>
        </label>
        {#if useLocal && !localRoutable}
          <div class="card-sub" style="color: var(--warn)">{cli} can't be routed to a local endpoint from a terminal — start a Chat with this CLI instead (Chats support local LLMs for every CLI).</div>
        {/if}
      {/if}

      {#if useLocal && localOpt?.configured && localRoutable}
        <label class="lbl">Local model</label>
        <input class="field mono" style="max-width:420px" list="code-local-models" placeholder="model on your endpoint" bind:value={localModel} />
        <datalist id="code-local-models">{#each localOpt.models || [] as m}<option value={m}></option>{/each}</datalist>
        {#if localOpt.error}<div class="card-sub" style="color: var(--warn)">Couldn't list models from the endpoint: {localOpt.error}. You can still type a model name.</div>{/if}
      {:else}
        <label class="lbl">Model {modelSupported ? `(${selectedCliInfo.modelHint})` : '(this CLI has no model flag — it uses its own config)'}</label>
        <input
          class="field mono"
          style="max-width:420px"
          list="code-models"
          placeholder={modelSupported ? 'blank = CLI default' : 'not supported'}
          bind:value={model}
          disabled={!modelSupported} />
        <datalist id="code-models">{#each modelSuggestions as m}<option value={m}></option>{/each}</datalist>
      {/if}
      <label class="lbl">Project folder</label>
      <div class="row">
        <input class="field grow" bind:value={cwd} placeholder="/path/to/your/project" />
        <button class="btn" on:click={chooseFolder}>Browse…</button>
      </div>
      <div class="row" style="margin-top:14px">
        <button class="btn primary" on:click={launch} disabled={!cwd || !cli}>Launch {cli || 'CLI'} here</button>
        <button class="btn" on:click={() => (cleanSetup = false)}>Cancel</button>
      </div>
    </div>
  {:else if !agent}
    {#if agents.length === 0}
      <div class="empty">No agents yet — press “New session” above, or create one on the Agents page.</div>
    {:else}
      <div class="section-label">Or launch from an agent</div>
    {/if}
    {#each agents as a}
      <div class="card row">
        <div class="grow">
          <div class="card-title">{a.name}</div>
          <div class="card-sub">{a.description?.split('\n')[0]}</div>
          <div style="margin-top:6px">{#each a.supports || [] as s}<span class="pill">{s}</span>{/each}</div>
        </div>
        <button class="btn primary" on:click={() => pick(a)}>Use</button>
      </div>
    {/each}
  {:else}
    <div class="row" style="margin-bottom:14px">
      <button class="btn" on:click={() => (agent = null)}>← Agents</button>
      <strong>{agent.name}</strong>
    </div>
    <label class="lbl">CLI</label>
    <select class="field" bind:value={cli} style="max-width:240px">
      {#each agent.supports || [] as s}<option value={s}>{s}</option>{/each}
    </select>
    {#if modelSupported}
      <label class="lbl">Model <span class="card-sub">(optional — blank uses the CLI default)</span></label>
      <input class="field" list="code-models" bind:value={model} placeholder="provider/model or model name" style="max-width:420px" />
      <datalist id="code-models">{#each modelSuggestions as m}<option value={m}></option>{/each}</datalist>
    {/if}
    <label class="lbl">Project folder</label>
    <div class="row">
      <input class="field grow" bind:value={cwd} placeholder="/path/to/your/project" />
      <button class="btn" on:click={chooseFolder}>Browse…</button>
    </div>
    <div style="margin-top:16px">
      <button class="btn primary" on:click={launch} disabled={!cwd}>Launch {cli} here</button>
    </div>
    <p class="card-sub" style="margin-top:10px">
      PrAImate writes the agent's instructions into the folder's context
      file ({cli === 'codex' || cli === 'opencode' ? 'AGENTS.md' : 'CLAUDE.md'})
      so {cli} adopts the persona. The CLI must be installed and on PATH.
    </p>
  {/if}
{:else}
  <div class="row" style="margin-bottom:10px">
    <div class="grow"><strong>{sessionLabel}</strong> <span class="pill">{cli}</span>{#if model}<span class="pill">{model}</span>{/if} <span class="card-sub mono">{cwd}</span></div>
    {#if exited}<button class="btn primary" on:click={reset}>New session</button>{/if}
    <button class="btn danger" on:click={reset}>Stop</button>
  </div>
  <div class="termhost" bind:this={el}></div>
{/if}

<style>
  .termhost {
    height: calc(100vh - 150px);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: #101218;
    padding: 8px;
    overflow: hidden;
  }
</style>
