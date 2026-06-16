<script>
  // Lightweight right-click menu. Caller passes { x, y, items } where
  // each item is { label, danger?, disabled?, action }. The menu
  // positions itself near the cursor, clamped inside the viewport, and
  // closes on outside click / Esc / any action click.
  //
  // No portal: the menu mounts wherever it's used, so any host with
  // position:relative / overflow:hidden could clip it. The Studio file
  // trees mount it as a sibling of the body via the parent component
  // (no clipping ancestor in those layouts).
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'

  /** @type {{x:number, y:number, items: Array<{label:string, danger?:boolean, disabled?:boolean, action: () => void}>}} */
  export let menu = null

  const dispatch = createEventDispatcher()
  let el

  function close() {
    dispatch('close')
  }

  function pick(it) {
    if (it.disabled) return
    try { it.action() } finally { close() }
  }

  function onWindowClick(ev) {
    if (!menu) return
    if (el && el.contains(ev.target)) return
    close()
  }
  function onKey(ev) {
    if (ev.key === 'Escape') { ev.preventDefault(); close() }
  }

  onMount(() => {
    window.addEventListener('mousedown', onWindowClick, true)
    window.addEventListener('keydown', onKey, true)
  })
  onDestroy(() => {
    window.removeEventListener('mousedown', onWindowClick, true)
    window.removeEventListener('keydown', onKey, true)
  })

  // Clamp inside the viewport so right-clicking near the bottom-right
  // doesn't push the menu off-screen.
  $: style = menu ? clamp(menu.x, menu.y) : ''
  function clamp(x, y) {
    const w = 200, h = 36 * (menu?.items?.length || 1) + 8
    const maxX = (typeof window !== 'undefined' ? window.innerWidth : 1000) - w - 4
    const maxY = (typeof window !== 'undefined' ? window.innerHeight : 700) - h - 4
    return `left:${Math.max(4, Math.min(x, maxX))}px;top:${Math.max(4, Math.min(y, maxY))}px;`
  }
</script>

{#if menu}
  <div class="ctxmenu" bind:this={el} style={style} role="menu">
    {#each menu.items as it}
      <button
        class="ctx-item"
        class:danger={it.danger}
        disabled={it.disabled}
        on:click={() => pick(it)}>
        {it.label}
      </button>
    {/each}
  </div>
{/if}

<style>
  .ctxmenu {
    position: fixed;
    z-index: 99999;
    min-width: 180px;
    background: var(--bg-raised, var(--bg-panel));
    color: var(--text);
    border: 1px solid var(--border-bright, var(--border));
    border-radius: 8px;
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
    padding: 4px;
    font-size: 12px;
    user-select: none;
  }
  .ctx-item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: inherit;
    padding: 7px 10px;
    border-radius: 6px;
    cursor: pointer;
    font: inherit;
  }
  .ctx-item:hover:not([disabled]) { background: var(--bg-panel); }
  .ctx-item[disabled] { color: var(--text-dim); cursor: not-allowed; }
  .ctx-item.danger { color: var(--err, #e85c5c); }
  .ctx-item.danger:hover:not([disabled]) {
    background: color-mix(in oklch, var(--err, #e85c5c) 18%, transparent);
  }
</style>
