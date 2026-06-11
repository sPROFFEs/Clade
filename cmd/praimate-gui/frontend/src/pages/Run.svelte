<script>
  import { onMount, onDestroy } from 'svelte'
  import { api, onTurn } from '../lib/api.js'
  import { activePage, openChatId } from '../lib/stores.js'

  let agents = []
  let error = ''

  // Per-agent chosen CLI for the Chat button (defaults to first support).
  let chatCli = {}

  // Start an interactive chat with this agent and jump to the Chats page.
  async function chat(a) {
    try {
      const cli = chatCli[a.id] || (a.supports && a.supports[0]) || 'claude'
      const c = await api.startChat(a.id, cli, '')
      openChatId.set(c.ID)
      activePage.set('chats')
    } catch (e) {
      error = String(e)
    }
  }

  let agent = null
  let workflow = null
  let cli = ''
  let cwd = ''
  let inputs = {}

  // privacy review state: null = not reviewed, {} = clean, {CAT: n} = matches
  let privacyCounts = null
  let running = false
  let turns = []
  let result = null

  let unsubscribe = () => {}

  async function load() {
    try {
      agents = (await api.listAgents()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
  }

  function pickAgent(a) {
    agent = a
    cli = a.supports?.[0] || 'claude'
    const def = a.workflows?.find((w) => w.name === a.default_workflow)
    workflow = def || a.workflows?.[0] || null
    inputs = {}
    privacyCounts = null
    result = null
    turns = []
    if (workflow) for (const inp of workflow.inputs || []) inputs[inp.name] = inp.default || ''
  }

  function pickWorkflow(w) {
    workflow = w
    inputs = {}
    privacyCounts = null
    for (const inp of w.inputs || []) inputs[inp.name] = inp.default || ''
  }

  async function chooseFolder() {
    try {
      const p = await api.pickFolder()
      if (p) cwd = p
    } catch (e) {
      error = String(e)
    }
  }

  async function review() {
    try {
      const joined = Object.values(inputs).join(' ')
      privacyCounts = (await api.privacyPreview(joined)) || {}
    } catch (e) {
      error = String(e)
    }
  }

  async function start() {
    running = true
    turns = []
    result = null
    error = ''
    unsubscribe = onTurn((t) => { turns = [...turns, t] })
    try {
      result = await api.runWorkflow(agent.id, workflow.name, cli, cwd, inputs)
    } catch (e) {
      error = String(e)
    } finally {
      unsubscribe()
      running = false
      privacyCounts = null
    }
  }

  function reset() {
    agent = null; workflow = null; inputs = {}; turns = []; result = null; privacyCounts = null
  }

  onMount(load)
  onDestroy(() => unsubscribe())

  $: matchTotal = privacyCounts ? Object.values(privacyCounts).reduce((a, b) => a + b, 0) : 0
</script>

<h1>Run</h1>
<p class="subtitle">Launch an agent workflow through a third-party CLI. Memory injection and privacy redaction apply automatically.</p>

{#if error}<div class="banner">{error}</div>{/if}

{#if !agent}
  {#if agents.length === 0}
    <div class="empty">No agents found. Built-ins seed automatically — check the backend banner.</div>
  {/if}
  {#each agents as a}
    <div class="card row">
      <div class="grow">
        <div class="card-title">{a.name}</div>
        <div class="card-sub">{a.description?.split('\n')[0]}</div>
        <div style="margin-top:6px">
          {#each a.supports || [] as s}<span class="pill">{s}</span>{/each}
        </div>
      </div>
      {#if (a.supports || []).length > 1}
        <select class="field" style="max-width:130px" bind:value={chatCli[a.id]}>
          {#each a.supports as s}<option value={s}>{s}</option>{/each}
        </select>
      {/if}
      <button class="btn primary" on:click={() => chat(a)}>Chat</button>
      <button class="btn" on:click={() => pickAgent(a)}>Run workflow</button>
    </div>
  {/each}
{:else if running}
  <div class="card">
    <div class="card-title">Running {agent.name} · {workflow.name} on {cli}…</div>
    <div class="card-sub">Streaming turns as they complete.</div>
  </div>
  {#each turns as t}
    <div class="msg user"><div class="who">you (turn {t.index + 1})</div>{t.user_msg}</div>
    <div class="msg assistant"><div class="who">assistant · {t.duration_ms}ms</div>{t.reply}</div>
  {/each}
{:else if result}
  <div class="row" style="margin-bottom:14px">
    <button class="btn" on:click={reset}>← New run</button>
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
    <button class="btn" on:click={reset}>← Agents</button>
    <strong>{agent.name}</strong>
  </div>

  {#if (agent.workflows || []).length > 1}
    <label class="lbl">Workflow</label>
    <div class="row" style="flex-wrap:wrap">
      {#each agent.workflows as w}
        <button class="btn" class:primary={workflow?.name === w.name} on:click={() => pickWorkflow(w)}>{w.name}</button>
      {/each}
    </div>
  {/if}

  {#if workflow}
    <label class="lbl">CLI</label>
    <select class="field" bind:value={cli} style="max-width:240px">
      {#each agent.supports || [] as s}<option value={s}>{s}</option>{/each}
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
      <div style="margin-top:18px">
        <button class="btn primary" on:click={review}>Continue</button>
      </div>
    {:else}
      <div class="card" style="margin-top:18px">
        {#if matchTotal === 0}
          <div class="card-title">Privacy scan: clean</div>
          <div class="card-sub">No secrets detected in your inputs.</div>
        {:else}
          <div class="card-title" style="color:var(--warn)">Privacy scan: {matchTotal} match(es)</div>
          <div class="card-sub">
            These will be sent REDACTED (placeholders) to the CLI:
            {#each Object.entries(privacyCounts) as [cat, n]}<span class="pill warn">{cat} ×{n}</span>{/each}
          </div>
        {/if}
        <div class="row" style="margin-top:10px">
          <button class="btn primary" on:click={start}>Run workflow</button>
          <button class="btn" on:click={() => (privacyCounts = null)}>Back</button>
        </div>
      </div>
    {/if}
  {/if}
{/if}
