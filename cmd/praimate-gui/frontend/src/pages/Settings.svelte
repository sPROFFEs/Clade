<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let error = ''
  let agents = []

  // Watchers
  let watchers = []
  let wAgent = ''
  let wPath = ''
  let wPatterns = ''
  let wWorkflow = ''

  // Schedules
  let schedules = []
  let sAgent = ''
  let sCron = ''
  let sWorkflow = ''

  // Privacy patterns
  let patterns = []
  let newPattern = ''

  async function load() {
    try {
      agents = (await api.listAgents()) || []
      watchers = (await api.listWatchers()) || []
      schedules = (await api.listSchedules()) || []
      patterns = (await api.listPrivacyPatterns()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
  }

  async function addWatcher() {
    if (!wAgent || !wPath) { error = 'Watcher needs an agent and a path'; return }
    try {
      const pats = wPatterns.split(',').map((p) => p.trim()).filter(Boolean)
      await api.addWatcher(wAgent, wPath, wWorkflow, pats)
      wPath = ''; wPatterns = ''; wWorkflow = ''
      await load()
    } catch (e) { error = String(e) }
  }

  async function addSchedule() {
    if (!sAgent || !sCron) { error = 'Schedule needs an agent and a cron expression'; return }
    try {
      await api.addCronSchedule(sAgent, sCron, sWorkflow)
      sCron = ''; sWorkflow = ''
      await load()
    } catch (e) { error = String(e) }
  }

  async function addPattern() {
    if (!newPattern.trim()) return
    try {
      await api.addPrivacyPattern(newPattern.trim())
      newPattern = ''
      await load()
    } catch (e) { error = String(e) }
  }

  onMount(load)
</script>

<h1>Settings</h1>
<p class="subtitle">Automation and privacy. These rows are shared with the TUI; GUI-only preferences stay separate.</p>

{#if error}<div class="banner">{error}</div>{/if}

<h1 style="font-size:16px">Folder watchers</h1>
{#each watchers as w}
  <div class="card row">
    <div class="grow">
      <div class="card-title mono">{w.Path}</div>
      <div class="card-sub">
        agent: {w.AgentID || w.ChatID} · patterns: {(w.Patterns || []).join(', ') || '(any)'} · workflow: {w.Workflow || '(default)'}
      </div>
    </div>
    <button class="btn" class:primary={w.Enabled} on:click={async () => { await api.setWatcherEnabled(w.ID, !w.Enabled); await load() }}>
      {w.Enabled ? 'On' : 'Off'}
    </button>
    <button class="btn danger" on:click={async () => { await api.deleteWatcher(w.ID); await load() }}>Delete</button>
  </div>
{/each}
<div class="card">
  <div class="row">
    <select class="field" style="max-width:200px" bind:value={wAgent}>
      <option value="">agent…</option>
      {#each agents as a}<option value={a.id}>{a.name}</option>{/each}
    </select>
    <input class="field grow" placeholder="/path/to/watch" bind:value={wPath} />
  </div>
  <div class="row" style="margin-top:8px">
    <input class="field grow" placeholder="patterns (comma-sep, e.g. *.go)" bind:value={wPatterns} />
    <input class="field grow" placeholder="workflow (blank = default)" bind:value={wWorkflow} />
    <button class="btn primary" on:click={addWatcher}>Add</button>
  </div>
</div>

<h1 style="font-size:16px; margin-top:24px">Schedules</h1>
{#each schedules as s}
  <div class="card row">
    <div class="grow">
      <div class="card-title mono">{s.Cron || s.At}</div>
      <div class="card-sub">
        agent: {s.AgentID || s.ChatID} · workflow: {s.Workflow || '(default)'}
        {#if s.NextRunAt} · next: {new Date(s.NextRunAt).toLocaleString()}{/if}
      </div>
    </div>
    <button class="btn" class:primary={s.Enabled} on:click={async () => { await api.setScheduleEnabled(s.ID, !s.Enabled); await load() }}>
      {s.Enabled ? 'On' : 'Off'}
    </button>
    <button class="btn danger" on:click={async () => { await api.deleteSchedule(s.ID); await load() }}>Delete</button>
  </div>
{/each}
<div class="card row">
  <select class="field" style="max-width:200px" bind:value={sAgent}>
    <option value="">agent…</option>
    {#each agents as a}<option value={a.id}>{a.name}</option>{/each}
  </select>
  <input class="field" style="max-width:180px" placeholder="cron (*/30 * * * *)" bind:value={sCron} />
  <input class="field grow" placeholder="workflow (blank = default)" bind:value={sWorkflow} />
  <button class="btn primary" on:click={addSchedule}>Add</button>
</div>

<h1 style="font-size:16px; margin-top:24px">Custom privacy patterns</h1>
<p class="subtitle">Regexes scanned alongside the built-ins (keys, tokens, SSN, cards) before any prompt leaves PrAImate.</p>
{#each patterns as p, i}
  <div class="card row">
    <div class="grow mono">{p}</div>
    <button class="btn danger" on:click={async () => { await api.deletePrivacyPattern(i); await load() }}>Delete</button>
  </div>
{/each}
<div class="row">
  <input class="field grow mono" placeholder="e.g. internal-\d{6}" bind:value={newPattern} />
  <button class="btn primary" on:click={addPattern}>Add</button>
</div>
