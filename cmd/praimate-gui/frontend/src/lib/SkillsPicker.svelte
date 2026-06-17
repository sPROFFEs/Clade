<script>
  // SkillsPicker — modal that lets the user toggle skills for a chat.
  // Used by Chats.svelte (settings panel) and the chat-launching
  // surfaces (Code page, Studio editor) so all three offer the same
  // popup interaction. The modal closes via the Done button or
  // Esc/backdrop click.
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'
  import { api } from './api.js'

  export let open = false
  export let cli = 'claude'           // current chat's CLI (for ⚠ markers)
  export let selected = []            // array of skill IDs currently enabled
  export let title = 'Skills'

  const dispatch = createEventDispatcher()

  let catalogue = []
  let loading = true
  let search = ''
  let activeTab = 'all'  // 'all' | 'enabled' | one of the CLIs
  let error = ''

  async function load() {
    loading = true
    try {
      catalogue = (await api.skillsList()) || []
    } catch (e) {
      error = String(e)
    } finally {
      loading = false
    }
  }

  $: filtered = catalogue
    .filter((s) => {
      if (activeTab === 'enabled') return selected.includes(s.id)
      if (activeTab === 'all') return true
      if (activeTab === 'universal') return !s.clis || s.clis.length === 0
      return (s.clis || []).includes(activeTab)
    })
    .filter((s) => {
      if (!search.trim()) return true
      const q = search.toLowerCase()
      return s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q)
    })

  function toggle(id) {
    if (selected.includes(id)) selected = selected.filter((x) => x !== id)
    else selected = [...selected, id]
    dispatch('change', selected)
  }

  function done() {
    dispatch('close', selected)
    open = false
  }

  function onKey(e) {
    if (e.key === 'Escape') done()
  }

  $: if (open) load()
  onMount(() => window.addEventListener('keydown', onKey))
  onDestroy(() => window.removeEventListener('keydown', onKey))
</script>

{#if open}
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="picker-backdrop" on:click={done}>
    <div class="picker" on:click|stopPropagation role="dialog">
      <div class="picker-head">
        <strong class="grow">{title}</strong>
        <span class="meta">{selected.length} enabled</span>
        <button class="picker-x" on:click={done}>×</button>
      </div>

      <div class="picker-toolbar">
        <input class="field" placeholder="Search skills…" bind:value={search} autofocus />
        <div class="tabrow">
          <button class="tab" class:on={activeTab === 'all'} on:click={() => (activeTab = 'all')}>All</button>
          <button class="tab" class:on={activeTab === 'enabled'} on:click={() => (activeTab = 'enabled')}>Enabled · {selected.length}</button>
          <button class="tab" class:on={activeTab === cli}    on:click={() => (activeTab = cli)} title={`For ${cli}`}>For {cli}</button>
          <button class="tab" class:on={activeTab === 'universal'} on:click={() => (activeTab = 'universal')}>Universal</button>
        </div>
      </div>

      <div class="picker-list">
        {#if loading}
          <div class="picker-empty">Loading…</div>
        {:else if filtered.length === 0}
          <div class="picker-empty">No skills match. {activeTab !== 'all' ? 'Try another tab.' : ''}</div>
        {/if}
        {#each filtered as s (s.id)}
          {@const enabled = selected.includes(s.id)}
          {@const matchesCLI = !s.clis || s.clis.length === 0 || s.clis.includes(cli)}
          <div class="picker-row" class:on={enabled}>
            <label class="picker-check">
              <input type="checkbox" checked={enabled} on:change={() => toggle(s.id)} />
            </label>
            <div class="picker-body grow">
              <div class="picker-title">
                {s.name}
                {#if !matchesCLI}<span class="warn" title={`Designed for ${(s.clis||[]).join(', ')} — not ${cli}`}>⚠</span>{/if}
              </div>
              <div class="picker-desc">{s.description}</div>
              <div class="picker-tags">
                {#each (s.clis || []) as c}<span class="tag">{c}</span>{/each}
                {#if !s.clis || s.clis.length === 0}<span class="tag">universal</span>{/if}
                {#if s.source && s.source !== 'builtin'}<span class="tag tag-user">{s.source.startsWith('http') ? 'imported' : (s.source === 'user' ? 'user' : 'custom')}</span>{/if}
              </div>
            </div>
          </div>
        {/each}
      </div>

      <div class="picker-foot">
        {#if error}<span class="err">{error}</span>{/if}
        <span class="grow"></span>
        <button class="btn primary" on:click={done}>Done</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .picker-backdrop {
    position: fixed; inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex; justify-content: center; align-items: flex-start;
    padding-top: 8vh;
    z-index: 9000;
  }
  .picker {
    width: min(720px, 96vw);
    max-height: 78vh;
    display: flex; flex-direction: column;
    background: var(--bg-raised, var(--bg-panel));
    border: 1px solid var(--border-bright, var(--border));
    border-radius: 12px;
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.55);
    overflow: hidden;
  }
  .picker-head {
    display: flex; gap: 10px; align-items: center;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
  }
  .picker-head .meta { color: var(--text-dim); font-size: 12px; }
  .picker-x {
    background: none; border: none; color: var(--text-dim);
    cursor: pointer; padding: 2px 8px; font-size: 18px;
  }
  .picker-toolbar { padding: 10px 14px; border-bottom: 1px solid var(--border); display: flex; flex-direction: column; gap: 8px; }
  .picker-toolbar .field { width: 100%; }
  .tabrow { display: flex; gap: 4px; flex-wrap: wrap; }
  .tab {
    background: none; border: 1px solid var(--border);
    color: var(--text); padding: 4px 10px; font-size: 11px;
    border-radius: 20px; cursor: pointer;
  }
  .tab.on { background: var(--accent, #5482ff); color: var(--accent-fg, white); border-color: transparent; }

  .picker-list { flex: 1; overflow-y: auto; padding: 4px 0; }
  .picker-empty { padding: 20px; text-align: center; color: var(--text-dim); font-size: 13px; }
  .picker-row {
    display: flex; gap: 10px; align-items: flex-start;
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
  }
  .picker-row:hover { background: var(--bg-panel); }
  .picker-row.on { background: color-mix(in oklch, var(--accent, #5482ff) 8%, transparent); }
  .picker-check { padding-top: 2px; }
  .picker-body { min-width: 0; }
  .picker-title { font-weight: 600; font-size: 13px; }
  .picker-title .warn { color: var(--warn, #d4a72c); margin-left: 4px; }
  .picker-desc { color: var(--text-dim); font-size: 12px; margin-top: 2px; line-height: 1.4; }
  .picker-tags { margin-top: 4px; display: flex; gap: 4px; flex-wrap: wrap; }
  .tag { font-size: 10px; padding: 1px 6px; border-radius: 5px; background: var(--bg-panel); color: var(--text-dim); }
  .tag-user { background: color-mix(in oklch, var(--accent, #5482ff) 22%, transparent); color: var(--accent, #5482ff); }

  .picker-foot {
    display: flex; align-items: center; gap: 10px;
    padding: 12px 16px;
    border-top: 1px solid var(--border);
  }
  .picker-foot .err { color: var(--err, #e85c5c); font-size: 12px; }
  .grow { flex: 1; }
</style>
