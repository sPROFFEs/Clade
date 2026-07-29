<script>
  // CLIs — detection + installation of the wrapped CLI agents,
  // Pick an install method per CLI
  // and watch its output stream live.
  import { onMount, onDestroy } from 'svelte'
  import { get } from 'svelte/store'
  import { api } from '../lib/api.js'
  import { cliCache } from '../lib/stores.js'

  let clis = []
  let error = ''
  let loading = true
  let methods = {} // cli → [methods]
  let chosen = {} // cli → method id
  let installing = '' // cli currently installing
  let log = []
  let unsub = () => {}

  // managed helper tools (graphify, gstack, scrapegraph)
  let tools = []
  let toolMethods = {}
  let toolChosen = {}

  async function load() {
    loading = true
    try {
      clis = (await api.listCLIBackends()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
    try { tools = (await api.listManagedTools()) || [] } catch { tools = [] }
    loading = false
    // Keep the app-wide prefetch cache warm so re-opening the tab is
    // instant after this fresh probe.
    cliCache.set({ clis, tools, loaded: true })
  }

  async function showToolMethods(t) {
    if (toolMethods[t.id]) { toolMethods = { ...toolMethods, [t.id]: null }; return }
    try {
      const ms = (await api.listToolInstallMethods(t.id)) || []
      toolMethods = { ...toolMethods, [t.id]: ms }
      toolChosen[t.id] = ms.find((m) => m.recommended)?.id || ms[0]?.id || ''
    } catch (e) {
      error = String(e)
    }
  }

  async function installTool(t) {
    const methodID = toolChosen[t.id]
    if (!methodID) return
    installing = t.id
    log = []
    error = ''
    try {
      await api.installManagedTool(t.id, methodID)
      log = [...log, '✓ install finished — re-probing…']
      await load()
    } catch (e) {
      error = String(e)
    } finally {
      installing = ''
    }
  }

  async function showMethods(cli) {
    if (methods[cli.id]) { methods = { ...methods, [cli.id]: null }; return }
    try {
      const ms = (await api.listInstallMethods(cli.id)) || []
      methods = { ...methods, [cli.id]: ms }
      chosen[cli.id] = ms.find((m) => m.recommended)?.id || ms[0]?.id || ''
    } catch (e) {
      error = String(e)
    }
  }

  async function install(cli) {
    const methodID = chosen[cli.id]
    if (!methodID) return
    installing = cli.id
    log = []
    error = ''
    try {
      await api.installCLI(cli.id, methodID)
      log = [...log, '✓ install finished — re-probing…']
      await load()
    } catch (e) {
      error = String(e)
    } finally {
      installing = ''
    }
  }

  onMount(async () => {
    if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
      window.runtime.EventsOn('praimate:install', (ev) => {
        log = [...log.slice(-300), ev.line]
      })
      unsub = () => window.runtime.EventsOff('praimate:install')
    }
    // Render instantly from the app-warmed prefetch cache, then refresh
    // in the background so the tab never shows a spinner on first open.
    const cached = get(cliCache)
    if (cached.loaded) {
      clis = cached.clis
      tools = cached.tools
      loading = false
    }
    await load()
  })
  onDestroy(() => unsub())
</script>

<div class="row" style="margin-bottom:4px">
  <h1 class="grow" style="margin:0">CLIs</h1>
  <button class="btn" title="Re-scan PATH for tools installed in another terminal" on:click={async () => { await api.refreshPATH(); await load() }} disabled={loading}>Re-scan PATH</button>
  <button class="btn" on:click={load} disabled={loading}>{loading ? 'Probing…' : 'Re-detect'}</button>
</div>
<p class="subtitle">The third-party CLI agents PrAImate wraps. Install or repair them here.</p>

{#if error}
  {#if error.includes('install pnpm directly')}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div class="modal-backdrop" on:click={() => error = ''}>
      <div class="modal-content" on:click|stopPropagation>
        <h2>pnpm installation failed</h2>
        <p class="subtitle" style="margin-bottom:12px">
          The automated installer couldn't set up pnpm (often due to missing permissions or a corepack bug in Node 20+).
          To work around this, please install it directly:
        </p>
        
        <div class="code-block">
          <strong>Linux:</strong><br/>
          <span class="mono">curl -fsSL https://get.pnpm.io/install.sh | sh -</span><br/>
          <span class="mono">exec $SHELL</span><br/>
          <br/>
          <strong>Windows (PowerShell):</strong><br/>
          <span class="mono">iwr https://get.pnpm.io/install.ps1 -useb | iex</span>
        </div>
        
        <p class="subtitle" style="margin-top:12px; margin-bottom:0">
          Once installed, PrAImate will detect it automatically.
        </p>
        
        <div class="row" style="margin-top:16px; justify-content:flex-end">
          <button class="btn" on:click={() => error = ''}>Close</button>
        </div>
      </div>
    </div>
  {:else}
    <div class="banner">{error}</div>
  {/if}
{/if}

{#if loading && clis.length === 0}<div class="empty">Probing installed CLIs…</div>{/if}

{#each clis as c}
  <div class="card">
    <div class="row">
      <div class="grow">
        <div class="card-title">
          {c.label}
          <span class="pill" class:ok={c.installed} class:err={!c.installed}>{c.installed ? 'installed' : 'not installed'}</span>
        </div>
        <div class="card-sub mono">
          {c.binary}{c.version ? ' · ' + c.version : ''}
          {#if c.probeError} · {c.probeError}{/if}
        </div>
      </div>
      <button class="btn" class:primary={!c.installed} on:click={() => showMethods(c)}>
        {c.installed ? 'Update / reinstall…' : 'Install…'}
      </button>
    </div>
    {#if methods[c.id]}
      <div style="margin-top:10px">
        {#if methods[c.id].length === 0}
          <div class="card-sub">No automated method on this OS. Manual: <span class="mono">{c.installHint}</span></div>
        {:else}
          <div class="row">
            <select class="field grow" bind:value={chosen[c.id]}>
              {#each methods[c.id] as m}
                <option value={m.id} disabled={m.missingPrereqs?.length > 0}>
                  {m.label}{m.recommended ? ' (recommended)' : ''}{m.missingPrereqs?.length ? ` — needs ${m.missingPrereqs.join(', ')}` : ''}
                </option>
              {/each}
            </select>
            <button class="btn primary" on:click={() => install(c)} disabled={installing !== ''}>
              {installing === c.id ? 'Installing…' : 'Run'}
            </button>
          </div>
          {#if chosen[c.id]}
            <div class="card-sub mono" style="margin-top:6px">
              {methods[c.id].find((m) => m.id === chosen[c.id])?.command}
            </div>
          {/if}
        {/if}
      </div>
    {/if}
  </div>
{/each}

{#if tools.length > 0}
  <h1 style="font-size:16px; margin-top:24px">Managed tools</h1>
  <p class="subtitle">Helper tools installed into PrAImate-managed prefixes, available to every agent.</p>
  {#each tools as t}
    <div class="card">
      <div class="row">
        <div class="grow">
          <div class="card-title">
            {t.label}
            <span class="pill" class:ok={t.installed} class:err={!t.installed}>{t.installed ? 'installed' : 'not installed'}</span>
          </div>
          <div class="card-sub mono">{t.binary}{t.version ? ' · ' + t.version : ''}</div>
        </div>
        <button class="btn" class:primary={!t.installed} on:click={() => showToolMethods(t)}>
          {t.installed ? 'Update…' : 'Install…'}
        </button>
      </div>
      {#if toolMethods[t.id]}
        <div style="margin-top:10px">
          {#if toolMethods[t.id].length === 0}
            <div class="card-sub">No automated method on this OS. Manual: <span class="mono">{t.installHint}</span></div>
          {:else}
            <div class="row">
              <select class="field grow" bind:value={toolChosen[t.id]}>
                {#each toolMethods[t.id] as m}
                  <option value={m.id} disabled={m.missingPrereqs?.length > 0}>
                    {m.label}{m.recommended ? ' (recommended)' : ''}{m.missingPrereqs?.length ? ` — needs ${m.missingPrereqs.join(', ')}` : ''}
                  </option>
                {/each}
              </select>
              <button class="btn primary" on:click={() => installTool(t)} disabled={installing !== ''}>
                {installing === t.id ? 'Installing…' : 'Run'}
              </button>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/each}
{/if}

{#if log.length > 0}
  <div class="card">
    <div class="card-title">Install output</div>
    <pre class="install-log">{log.join('\n')}</pre>
  </div>
{/if}

<style>
  .install-log {
    margin: 6px 0 0;
    max-height: 280px;
    overflow-y: auto;
    font-family: var(--mono, ui-monospace, monospace);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text-dim);
  }
</style>
