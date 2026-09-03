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
  <div class="picker-backdrop" style="z-index:10000" on:click={done}>
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
