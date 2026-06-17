<script>
  import { onMount, onDestroy } from 'svelte'
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

  // Update check (Settings parity with `praimate -update`'s probe).
  let updateInfo = null
  let checkingUpdate = false
  async function checkUpdate() {
    checkingUpdate = true
    try {
      updateInfo = await api.checkUpdate()
    } catch (e) {
      error = String(e)
    } finally {
      checkingUpdate = false
    }
  }

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

  // Privacy patterns
  let patterns = []
  let newPattern = ''

  async function load() {
    try {
      agents = (await api.listAgents()) || []
      patterns = (await api.listPrivacyPatterns()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
    await bkLoad()
  }

  async function addPattern() {
    if (!newPattern.trim()) return
    try {
      await api.addPrivacyPattern(newPattern.trim())
      newPattern = ''
      await load()
    } catch (e) { error = String(e) }
  }

  // Build tools from source — for platforms where we don't ship a
  // prebuilt bundle (praimate-code off Linux, graphify off linux/amd64).
  let buildInfo = {} // tool -> BuildToolInfo
  let building = '' // tool currently compiling, '' = idle
  let buildLog = []
  let buildUnsub = () => {}

  let prereqModal = null // { name, detail }

  function showPrereqModal(r) {
    prereqModal = r
  }

  function closePrereqModal() {
    prereqModal = null
  }

  async function loadBuildInfo() {
    const next = {}
    for (const t of ['praimate-code', 'graphify']) {
      try { next[t] = await api.buildRequirements(t) } catch (e) { /* ignore */ }
    }
    buildInfo = next
  }

  async function buildTool(t) {
    if (building) return
    building = t
    buildLog = []
    try {
      await api.buildToolFromSource(t)
      buildLog = [...buildLog, '✓ finished']
      await loadBuildInfo()
    } catch (e) {
      buildLog = [...buildLog, '✗ ' + String(e)]
    } finally {
      building = ''
    }
  }

  onMount(async () => {
    await load()
    await loadBuildInfo()
    if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
      window.runtime.EventsOn('praimate:install', (ev) => {
        if (ev && typeof ev.cli === 'string' && ev.cli.startsWith('build:')) {
          buildLog = [...buildLog.slice(-400), ev.line]
        }
      })
      buildUnsub = () => window.runtime.EventsOff('praimate:install')
    }
  })
  onDestroy(() => buildUnsub())
</script>

<h1>Settings</h1>
<p class="subtitle">Automation and privacy. These rows are shared with the TUI; GUI-only preferences stay separate.</p>

{#if error}<div class="banner">{error}</div>{/if}

<h1 style="font-size:16px">Updates</h1>
<div class="card">
  <div class="row">
    <div class="grow">
      <div class="card-title">PrAImate version</div>
      <div class="card-sub">
        {#if updateInfo}
          {#if updateInfo.hasUpdate}
            v{updateInfo.current} → <strong>v{updateInfo.latest} available</strong> — run <span class="mono">praimate -update</span> (refreshes the GUI binary too), or download: <span class="mono">{updateInfo.url}</span>
          {:else}
            v{updateInfo.current} — up to date
          {/if}
        {:else}
          Check GitHub for a newer release.
        {/if}
      </div>
    </div>
    <button class="btn" on:click={checkUpdate} disabled={checkingUpdate}>{checkingUpdate ? 'Checking…' : 'Check for updates'}</button>
  </div>
</div>

<h1 style="font-size:16px; margin-top:24px">Build bundled tools from source</h1>
<p class="subtitle" style="margin-top:-6px">
  On platforms where we don't ship a prebuilt binary, build it locally from our repo.
  We clone the source, compile, install it into <span class="mono">~/.config/praimate/bin</span>, and delete the temporary checkout.
</p>
{#each [{ id: 'praimate-code', name: 'PrAImate Code' }, { id: 'graphify', name: 'Graphify (RAG)' }] as t}
  {@const info = buildInfo[t.id]}
  <div class="card">
    <div class="row" style="align-items:flex-start">
      <div class="grow">
        <div class="card-title">{t.name}</div>
        <div class="card-sub">{info?.note || 'Compile this tool locally from source.'}</div>
        {#if info}
          <div class="row" style="gap:6px; flex-wrap:wrap; margin-top:8px">
            {#each info.requirements as r}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <span
                class="pill {r.found ? 'ok' : 'err'}"
                title={r.detail}
                style="cursor: pointer"
                on:click={() => showPrereqModal(r)}
              >
                {r.found ? '✓' : '✗'} {r.name}
              </span>
            {/each}
          </div>
          {#if !info.ready}
            <div class="card-sub" style="margin-top:6px; color: var(--warn)">
              Install the missing tool(s) above, then re-open Settings.
            </div>
          {/if}
        {/if}
      </div>
      <button
        class="btn primary"
        on:click={() => buildTool(t.id)}
        disabled={!info || !info.ready || !!building}>
        {building === t.id ? 'Building…' : 'Build from source'}
      </button>
    </div>
  </div>
{/each}
{#if buildLog.length}
  <pre class="build-log">{buildLog.join('\n')}</pre>
{/if}

<h1 style="font-size:16px; margin-top:24px">Appearance</h1>
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

<h1 style="font-size:16px; margin-top:24px">Custom privacy patterns</h1>
<p class="subtitle">
  Extra regexes scanned alongside the built-in patterns (API keys, OAuth
  tokens, SSNs, credit-card numbers, AWS access IDs). Every outgoing
  prompt is redacted: matched substrings are replaced with a placeholder
  before being handed to the wrapped CLI; the reply is un-redacted on
  the way back in. Add patterns to protect strings PrAImate can't guess
  — internal ticket IDs, customer codes, project codenames, etc. Use
  Go's <span class="mono">regexp/syntax</span> (RE2) — no look-arounds.
</p>
<p class="subtitle" style="margin-top:6px">
  Examples: <span class="mono">internal-\d{6}</span> (ticket IDs),
  <span class="mono">[A-Z]{3}-PROJ-\d+</span> (project codes),
  <span class="mono">CUST_[a-z0-9]{12}</span> (customer tokens).
  Patterns are case-sensitive unless you prefix with <span class="mono">(?i)</span>.
</p>
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

{#if prereqModal}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="modal-backdrop" on:click={closePrereqModal}>
    <div class="modal-content" on:click|stopPropagation>
      <h2>How to install {prereqModal.name}</h2>
      <p class="subtitle" style="margin-bottom:12px">{prereqModal.detail}</p>
      
      {#if prereqModal.name === 'git'}
        <div class="code-block">
          <strong>macOS:</strong> <span class="mono">brew install git</span><br/>
          <strong>Linux (Debian/Ubuntu):</strong> <span class="mono">sudo apt install git</span><br/>
          <strong>Windows:</strong> <span class="mono">winget install --id Git.Git -e --source winget</span>
        </div>
      {:else if prereqModal.name === 'bun'}
        <div class="code-block">
          <strong>macOS / Linux:</strong> <span class="mono">curl -fsSL https://bun.sh/install | bash</span><br/>
          <strong>Windows:</strong> <span class="mono">powershell -c "irm bun.sh/install.ps1 | iex"</span>
        </div>
      {:else if prereqModal.name === 'uv'}
        <div class="code-block">
          <strong>macOS / Linux:</strong> <span class="mono">curl -LsSf https://astral.sh/uv/install.sh | sh</span><br/>
          <strong>Windows:</strong> <span class="mono">powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"</span>
        </div>
      {:else if prereqModal.name === 'bash'}
        <div class="code-block">
          <strong>Windows:</strong> Download Git for Windows and ensure "Git Bash" is in your PATH.
        </div>
      {/if}
      
      <div class="row" style="margin-top:16px; justify-content:flex-end">
        <button class="btn" on:click={closePrereqModal}>Close</button>
      </div>
    </div>
  </div>
{/if}
