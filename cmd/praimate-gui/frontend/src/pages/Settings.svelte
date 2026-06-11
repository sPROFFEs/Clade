<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import {
    ACCENT_PRESETS,
    themeMode,
    accentColor,
    setThemeMode,
    setAccent,
  } from '../lib/theme.js'

  const themeModes = [
    { id: 'light', label: 'Light' },
    { id: 'dark', label: 'Dark' },
    { id: 'system', label: 'System' },
  ]

  let error = ''
  let agents = []

  // Backup (git sync of the workspaces root — shared with the TUI)
  let bk = null            // BackupState
  let bkBusy = ''          // label of the in-flight op, '' = idle
  let bkMsg = ''           // last op result line
  let bkRemote = ''        // remote URL input
  let bkDiverged = null    // {localCommits, remoteCommits} when resolution needed

  function fmtDate(s) {
    try { return new Date(s).toLocaleString() } catch { return s }
  }

  async function bkLoad() {
    try {
      bk = await api.backupStatus()
      bkRemote = bk?.remoteUrl || ''
    } catch (e) {
      bk = null
      error = String(e)
    }
  }

  async function bkOp(label, fn) {
    if (bkBusy) return
    bkBusy = label
    bkMsg = ''
    error = ''
    try {
      const res = await fn()
      if (res) { bk = res; bkRemote = bk.remoteUrl || '' }
      bkDiverged = null
      bkMsg = label + ' ✓'
    } catch (e) {
      error = String(e)
    } finally {
      bkBusy = ''
    }
  }

  async function bkSync() {
    if (bkBusy) return
    bkBusy = 'Sync'
    bkMsg = ''
    error = ''
    bkDiverged = null
    try {
      const res = await api.backupSyncNow()
      bk = res.state
      bkRemote = bk.remoteUrl || ''
      if (res.action === 'diverged') {
        bkDiverged = { local: res.localCommits || [], remote: res.remoteCommits || [] }
        bkMsg = 'Diverged — pick a resolution below.'
      } else {
        bkMsg = { in_sync: 'In sync ✓', pushed: 'Pushed local changes ✓', pulled: 'Pulled remote changes ✓', no_remote: 'No remote configured.' }[res.action] || res.action
      }
    } catch (e) {
      error = String(e)
    } finally {
      bkBusy = ''
    }
  }

  async function bkTest() {
    if (bkBusy) return
    bkBusy = 'Test'
    bkMsg = ''
    error = ''
    try {
      const branch = await api.testBackupRemote(bkRemote)
      bkMsg = `Connection OK — default branch: ${branch}`
    } catch (e) {
      error = String(e)
    } finally {
      bkBusy = ''
    }
  }

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
    await bkLoad()
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

<h1 style="font-size:16px">Appearance</h1>
<div class="card">
  <div class="row" style="justify-content: space-between">
    <div>
      <div class="card-title">Theme</div>
      <div class="card-sub">Light, dark, or follow the OS preference.</div>
    </div>
    <div class="toggle-group">
      {#each themeModes as m}
        <button class:active={$themeMode === m.id} on:click={() => setThemeMode(m.id)}>
          {m.label}
        </button>
      {/each}
    </div>
  </div>
  <div class="row" style="justify-content: space-between; margin-top: 12px">
    <div>
      <div class="card-title">Accent</div>
      <div class="card-sub">Used for primary buttons and focused fields.</div>
    </div>
    <div class="swatch-row">
      {#each ACCENT_PRESETS as preset}
        <button
          class="swatch"
          class:active={preset.color
            ? $accentColor.toLowerCase() === preset.color.toLowerCase()
            : $accentColor === 'default'}
          style={preset.color ? `background:${preset.color}` : ''}
          title={preset.label}
          on:click={() => setAccent(preset.color ?? 'default')}>
          {#if !preset.color}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9" /><path d="M5.6 5.6l12.8 12.8" /></svg>
          {/if}
        </button>
      {/each}
    </div>
  </div>
</div>

<h1 style="font-size:16px; margin-top:24px">Backup — git sync</h1>
<p class="subtitle">Mirrors your chats + templates (the same workspaces root the TUI uses) to a git remote. Settings are shared with the TUI's Backup tab.</p>

{#if !bk}
  <div class="card card-sub">Loading backup status…</div>
{:else if !bk.supported}
  <div class="card card-sub">No workspaces root configured yet — run the <span class="mono">praimate</span> TUI once to set it up.</div>
{:else}
  <div class="card">
    <div class="row" style="justify-content: space-between">
      <div>
        <div class="card-title">Backup enabled</div>
        <div class="card-sub">
          {#if bk.enabled && bk.initialized}
            {bk.branch || 'main'}
            {#if bk.ahead || bk.behind} · ↑{bk.ahead} ↓{bk.behind}{:else} · in sync{/if}
            {#if bk.lastCommit} · {bk.lastCommit}{/if}
          {:else if bk.enabled}
            enabled, repo not initialised yet
          {:else}
            off — flip to initialise the workspaces root as a git repo
          {/if}
        </div>
        {#if bk.lastSyncAt}<div class="card-sub">last sync: {fmtDate(bk.lastSyncAt)}{#if bk.machineId} · machine: <span class="mono">{bk.machineId}</span>{/if}</div>{/if}
      </div>
      <button class="btn" class:primary={bk.enabled} disabled={!!bkBusy}
        on:click={() => bkOp(bk.enabled ? 'Disable' : 'Enable', () => api.setBackupEnabled(!bk.enabled))}>
        {bk.enabled ? 'On' : 'Off'}
      </button>
    </div>

    {#if bk.enabled}
      <label class="lbl">Remote URL (https or ssh — uses your git client's credentials)</label>
      <div class="row">
        <input class="field grow mono" placeholder="git@github.com:you/praimate-backup.git" bind:value={bkRemote} />
        <button class="btn" disabled={!!bkBusy || !bkRemote.trim()} on:click={bkTest}>Test</button>
        <button class="btn" disabled={!!bkBusy} on:click={() => bkOp('Save remote', () => api.setBackupRemote(bkRemote))}>Save</button>
      </div>

      <div class="row" style="margin-top:12px">
        <button class="btn primary" disabled={!!bkBusy || !bk.remoteUrl} on:click={bkSync}>
          {bkBusy === 'Sync' ? 'Syncing…' : 'Sync now'}
        </button>
        <button class="btn" disabled={!!bkBusy}
          on:click={() => bkOp(bk.autoSync ? 'Auto-sync off' : 'Auto-sync on', () => api.setBackupAutoSync(!bk.autoSync))}>
          Auto-sync: {bk.autoSync ? 'on' : 'off'}
        </button>
        {#if bk.autoSync}
          <button class="btn" disabled={!!bkBusy}
            on:click={() => bkOp(bk.forceLocal ? 'Force-local off' : 'Force-local on', () => api.setBackupForceLocal(!bk.forceLocal))}>
            Force local: {bk.forceLocal ? 'on' : 'off'}
          </button>
        {/if}
        <div class="grow"></div>
        <button class="btn danger" disabled={!!bkBusy || !bk.remoteUrl}
          on:click={() => confirm('Force-push local state over the remote?') && bkOp('Force push', api.backupForcePush)}>Force push</button>
        <button class="btn danger" disabled={!!bkBusy || !bk.remoteUrl}
          on:click={() => confirm('Discard ALL local changes and reset to the remote?') && confirm('Really? Local commits and uncommitted changes will be lost.') && bkOp('Reset from remote', api.backupResetFromRemote)}>Reset from remote</button>
        <button class="btn danger" disabled={!!bkBusy || !bk.remoteUrl}
          on:click={() => bkOp('Disconnect', api.backupDisconnect)}>Disconnect</button>
      </div>

      {#if bkMsg}<div class="card-sub" style="margin-top:8px">{bkMsg}</div>{/if}

      {#if bkDiverged}
        <div class="card" style="margin-top:10px; border-color: var(--warn)">
          <div class="card-title">Diverged — local and remote both have new commits</div>
          <div class="row" style="align-items: flex-start; margin-top:8px">
            <div class="grow">
              <div class="card-sub" style="font-weight:600">Local only</div>
              {#each bkDiverged.local as c}<div class="mono card-sub">{c}</div>{/each}
              {#if bkDiverged.local.length === 0}<div class="card-sub">(none)</div>{/if}
            </div>
            <div class="grow">
              <div class="card-sub" style="font-weight:600">Remote only</div>
              {#each bkDiverged.remote as c}<div class="mono card-sub">{c}</div>{/each}
              {#if bkDiverged.remote.length === 0}<div class="card-sub">(none)</div>{/if}
            </div>
          </div>
          <div class="row" style="margin-top:10px">
            <button class="btn primary" disabled={!!bkBusy} on:click={() => bkOp('Merge', () => api.resolveBackupDivergence('merge'))}>Merge (keep both)</button>
            <button class="btn" disabled={!!bkBusy} on:click={() => bkOp('Rebase', () => api.resolveBackupDivergence('rebase'))}>Rebase local on remote</button>
            <button class="btn danger" disabled={!!bkBusy}
              on:click={() => confirm('Discard the remote-only commits?') && bkOp('Force push', () => api.resolveBackupDivergence('forcepush'))}>Force push (keep local)</button>
            <button class="btn danger" disabled={!!bkBusy}
              on:click={() => confirm('Discard the local-only commits?') && bkOp('Reset', () => api.resolveBackupDivergence('reset'))}>Reset (keep remote)</button>
          </div>
        </div>
      {/if}
    {/if}
  </div>
{/if}

<h1 style="font-size:16px; margin-top:24px">Folder watchers</h1>
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
