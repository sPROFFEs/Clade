<script>
  import { onMount } from 'svelte'
  import { api } from './lib/api.js'
  import { activePage, prefetchCLIs } from './lib/stores.js'
  import { initTheme, themeMode, setThemeMode } from './lib/theme.js'
  import logo from './assets/monke-icon.png'
  import mascot from './assets/monke-mascot.png'
  import Chats from './pages/Chats.svelte'
  import Code from './pages/Code.svelte'
  import Agents from './pages/Agents.svelte'
  import CLIs from './pages/CLIs.svelte'
  import LocalLLM from './pages/LocalLLM.svelte'
  import Memory from './pages/Memory.svelte'
  import MCP from './pages/MCP.svelte'
  import Settings from './pages/Settings.svelte'
  import Editor from './pages/Editor.svelte'
  import Setup from './pages/Setup.svelte'

  // Lucide-style outline icon paths (24x24 viewBox, stroke-based).
  const icons = {
    code: 'M8 9l-4 3 4 3M16 9l4 3-4 3M13 5l-2 14',
    chats: 'M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z',
    run: 'M12 8V4m0 0h4m-4 0H8m-4 9a8 8 0 0 1 16 0v4a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM9 14h.01M15 14h.01',
    agents: 'M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7',
    mcp: 'M12 22v-5M9 8V2M15 8V2M6 8h12v5a6 6 0 0 1-12 0z',
    memory: 'M12 5a3 3 0 1 0-5.9.8A3 3 0 0 0 4 9a3 3 0 0 0 1 5.9 3 3 0 0 0 5 2.6V5zM12 5a3 3 0 1 1 5.9.8A3 3 0 0 1 20 9a3 3 0 0 1-1 5.9 3 3 0 0 1-5 2.6V5z',
    settings:
      'M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
    sun: 'M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10zM12 1v2M12 21v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M1 12h2M21 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4',
    moon: 'M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z',
    monitor: 'M2 4a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2zM8 21h8M12 17v4',
    chevronLeft: 'M15 18l-6-6 6-6',
    chevronRight: 'M9 18l6-6-6-6',
  }

  // Sidebar collapse — icons-only rail. Persisted to localStorage so it
  // survives reloads; default expanded on first run.
  let collapsed = false
  try {
    collapsed = localStorage.getItem('praimate:sidebar-collapsed') === '1'
  } catch {}
  function toggleCollapsed() {
    collapsed = !collapsed
    try {
      localStorage.setItem('praimate:sidebar-collapsed', collapsed ? '1' : '0')
    } catch {}
  }

  // Code-oriented order: lead with the live coding terminal, then
  // conversations and agents, with config last.
  const pages = [
    { id: 'code', label: 'Code', icon: icons.code, component: Code },
    { id: 'chats', label: 'Chats', icon: icons.chats, component: Chats },
    { id: 'agents', label: 'Agents', icon: icons.run, component: Agents },
    { id: 'clis', label: 'CLI & Tools', icon: icons.agents, component: CLIs },
    { id: 'localllm', label: 'Local LLM', icon: icons.monitor, component: LocalLLM },
    { id: 'mcp', label: 'MCP', icon: icons.mcp, component: MCP },
    { id: 'memory', label: 'Memory', icon: icons.memory, component: Memory },
    { id: 'settings', label: 'Settings', icon: icons.settings, component: Settings },
  ]

  let health = null
  // Studio mode: this process was spawned as a document-editor window —
  // render the Editor shell instead of the main app.
  let editorMode = null
  // First-run setup (no launcher config yet) — same flow as the TUI.
  let firstRun = null

  onMount(async () => {
    initTheme()
    try {
      editorMode = await api.editorMode()
    } catch {
      editorMode = { active: false }
    }
    try {
      firstRun = await api.firstRun()
    } catch {
      firstRun = { needed: false }
    }
    try {
      health = await api.health()
    } catch (e) {
      health = { ok: false, error: String(e) }
    }
    // Warm the CLI & Tools detection cache in the background so the tab
    // opens instantly instead of probing on first view. Only for the
    // main app window (skip in editor/setup modes).
    if (editorMode && !editorMode.active && !firstRun?.needed) {
      prefetchCLIs()
    }
  })

  function setupDone() {
    firstRun = { ...firstRun, needed: false }
  }

  // Quick theme cycle in the sidebar footer: dark → light → system.
  const modeOrder = ['dark', 'light', 'system']
  function cycleTheme() {
    const next = modeOrder[(modeOrder.indexOf($themeMode) + 1) % modeOrder.length]
    setThemeMode(next)
  }
  $: themeIcon =
    $themeMode === 'dark' ? icons.moon : $themeMode === 'light' ? icons.sun : icons.monitor

  // Re-key the page component on each activePage change so a page like
  // Chats re-runs onMount (and picks up a freshly started chat).
  $: current = pages.find((p) => p.id === $activePage) || pages[0]
</script>

{#if editorMode?.active}
  <Editor folder={editorMode.folder} chatId={editorMode.chatId} />
{:else if editorMode && firstRun?.needed}
  <Setup defaultRoot={firstRun.defaultRoot} on:done={setupDone} />
{:else if editorMode}
<div class="shell">
  <nav class="sidebar" class:collapsed>
    <!-- Collapsed: the logo itself is the expand button (no floating
         chevron overlapping the icon). Expanded: logo + wordmark + a
         collapse chevron pinned to the right edge. -->
    <div class="brand" class:clickable={collapsed} on:click={collapsed ? toggleCollapsed : undefined} title={collapsed ? 'Expand sidebar' : ''}>
      <img src={logo} alt="PrAImate" />
      {#if !collapsed}
        <span>PrAImate</span>
        <button
          class="icon-btn collapse-btn"
          title="Collapse sidebar"
          on:click|stopPropagation={toggleCollapsed}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d={icons.chevronLeft} /></svg>
        </button>
      {/if}
    </div>
    {#each pages as p}
      <button
        class="nav-item"
        class:active={$activePage === p.id}
        title={collapsed ? p.label : ''}
        on:click={() => activePage.set(p.id)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d={p.icon} /></svg>
        {#if !collapsed}<span class="nav-label">{p.label}</span>{/if}
      </button>
    {/each}
    <div style="flex:1"></div>
    <div class="mascot" style="background-image:url({mascot})" aria-hidden="true"></div>
    <div class="sidebar-footer">
      {#if health}
        {#if health.ok}
          <span class="pill ok">v{health.version}</span>
        {:else}
          <span class="pill err">backend error</span>
        {/if}
      {:else}
        <span></span>
      {/if}
      <button class="icon-btn" title="Theme: {$themeMode}" on:click={cycleTheme}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d={themeIcon} /></svg>
      </button>
    </div>
  </nav>

  <main class="main">
    {#if health && !health.ok}
      <div class="banner">Backend failed to initialise: {health.error}</div>
    {/if}
    {#key $activePage}
      <svelte:component this={current.component} />
    {/key}
  </main>
</div>
{/if}
