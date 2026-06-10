<script>
  import { onMount } from 'svelte'
  import { api } from './lib/api.js'
  import Chats from './pages/Chats.svelte'
  import Run from './pages/Run.svelte'
  import Agents from './pages/Agents.svelte'
  import Memory from './pages/Memory.svelte'
  import MCP from './pages/MCP.svelte'
  import Settings from './pages/Settings.svelte'

  const pages = [
    { id: 'chats', label: 'Chats', component: Chats },
    { id: 'run', label: 'Run', component: Run },
    { id: 'agents', label: 'Agents', component: Agents },
    { id: 'memory', label: 'Memory', component: Memory },
    { id: 'mcp', label: 'MCP', component: MCP },
    { id: 'settings', label: 'Settings', component: Settings },
  ]

  let active = 'chats'
  let health = null

  onMount(async () => {
    try {
      health = await api.health()
    } catch (e) {
      health = { ok: false, error: String(e) }
    }
  })

  $: current = pages.find((p) => p.id === active)
</script>

<div class="shell">
  <nav class="sidebar">
    <div class="brand">PrAImate</div>
    {#each pages as p}
      <button
        class="nav-item"
        class:active={active === p.id}
        on:click={() => (active = p.id)}>{p.label}</button>
    {/each}
    <div style="flex:1"></div>
    {#if health}
      <div class="card-sub" style="padding: 8px 12px">
        {#if health.ok}
          <span class="pill ok">v{health.version}</span>
        {:else}
          <span class="pill err">backend error</span>
        {/if}
      </div>
    {/if}
  </nav>

  <main class="main">
    {#if health && !health.ok}
      <div class="banner">Backend failed to initialise: {health.error}</div>
    {/if}
    <svelte:component this={current.component} />
  </main>
</div>
