<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import { api } from '../lib/api.js'
  import { term, onTermData, onTermExit } from '../lib/terminal.js'

  let agents = []
  let error = ''

  // setup state
  let agent = null
  let cli = ''
  let cwd = ''

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
  }

  function pick(a) {
    agent = a
    cli = (a.supports && a.supports[0]) || 'claude'
    error = ''
  }

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
    error = ''
    try {
      termId = await term.start(agent.id, cli, cwd)
    } catch (e) {
      error = String(e)
      return
    }
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

  onMount(load)
  onDestroy(teardown)
</script>

<h1>Code</h1>
<p class="subtitle">Run an agent's CLI live in a project folder — the real tool, with streaming, tool calls, and file edits.</p>

{#if error}<div class="banner">{error}</div>{/if}

{#if !started}
  {#if !agent}
    {#if agents.length === 0}
      <div class="empty">No agents found.</div>
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
    <div class="grow"><strong>{agent.name}</strong> <span class="pill">{cli}</span> <span class="card-sub mono">{cwd}</span></div>
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
