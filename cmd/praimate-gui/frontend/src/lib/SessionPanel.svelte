<script>
  // SessionPanel — a flyout listing every open chat/Studio/Code session
  // the user currently has, with a live indicator for chats that are
  // mid-stream. Click an item to jump back to it without losing the
  // running conversation. Triggered from the sidebar's "⚡ Sessions"
  // button.
  //
  // Cross-process Studio windows aren't directly focusable from this
  // process (Wails v2 = one window per process), so clicking a Studio
  // session re-opens the editor window for its folder + chat — if the
  // existing window is still alive, the OS just gives the user two; if
  // it crashed, this brings it back. Trade-off documented in the
  // tooltip.
  import { onMount, onDestroy } from 'svelte'
  import { api } from './api.js'
  import { term } from './terminal.js'
  import { activePage, openChatId, pendingTerm } from './stores.js'

  let chats = []
  let active = new Set()
  let liveTerms = new Map() // chatID → termID (live PTY we can resume)
  let open = false
  let loading = false
  let timer = null

  async function load() {
    if (loading) return
    loading = true
    try {
      const [cs, ids, terms] = await Promise.all([
        api.listChats().catch(() => []),
        api.activeChatIDs().catch(() => []),
        api.listTerminalSessions().catch(() => []),
      ])
      chats = (cs || []).slice(0, 40)
      active = new Set(ids || [])
      const m = new Map()
      for (const t of (terms || [])) if (t.chatId) m.set(t.chatId, t)
      liveTerms = m
    } finally {
      loading = false
    }
  }

  function surfaceOf(c) {
    const s = c?.Settings?.surface || ''
    if (s === 'studio') return 'studio'
    if (s === 'code') return 'code'
    if (s === 'agent-helper') return 'helper'
    return 'chat'
  }

  function surfaceLabel(s) {
    return s === 'studio' ? 'Studio' : s === 'code' ? 'Code · TUI' : s === 'helper' ? 'Agent helper' : 'Chat'
  }

  function fmtAgo(iso) {
    if (!iso) return ''
    const t = new Date(iso).getTime()
    if (Number.isNaN(t)) return ''
    const ms = Date.now() - t
    const s = Math.round(ms / 1000)
    if (s < 60) return `${s}s ago`
    const m = Math.round(s / 60)
    if (m < 60) return `${m}m ago`
    const h = Math.round(m / 60)
    if (h < 24) return `${h}h ago`
    return `${Math.round(h / 24)}d ago`
  }

  async function closeSession(c) {
    if (!confirm(`Close "${c.Title || c.ID}"? Any in-flight reply is cancelled, the PTY (if any) is killed, and the chat row is deleted.`)) return
    // 1. Cancel any in-flight turn — no-op if nothing's running.
    try { await api.cancelChatTurn(c.ID) } catch {}
    // 2. Kill the bound PTY if it's still up.
    const live = liveTerms.get(c.ID)
    if (live) { try { await term.close(live.id) } catch {} }
    // 3. Drop the chat row so it stops showing up.
    try { await api.deleteChat(c.ID) } catch {}
    await load()
  }

  async function jump(c) {
    open = false
    const s = surfaceOf(c)
    if (s === 'studio') {
      // Re-open the Studio window for this chat's folder. If the
      // original window is still alive the OS will surface a second
      // — Wails v2 has no cross-process focus primitive on Linux.
      try {
        await api.openEditorWindow(c.WorkspacePath || '', c.AgentID || '', c.CLIAgent || '', c.Settings?.model || '', c.ID, c.Settings?.local?.endpoint || '', c.Settings?.local?.api_key || '', c.Settings?.local?.model || '')
      } catch (e) { console.error(e) }
      return
    }
    if (s === 'code') {
      // Code TUI chats live on the Code page. If we have a live PTY
      // for this chat, reattach to its existing stream — no second
      // process. If not (GUI was restarted or the process died),
      // start a fresh one in the same folder. The presence of a
      // live term is also what the green "live" dot in the row
      // signals to the user.
      const live = liveTerms.get(c.ID)
      pendingTerm.set({
        termId: live ? live.id : '',
        cli: c.CLIAgent || '',
        cwd: c.WorkspacePath || '',
        label: c.Title || '',
        note: live ? '' : '(previous PTY is gone — starting a fresh session in the same folder)',
      })
      activePage.set('code')
      return
    }
    openChatId.set(c.ID)
    activePage.set('chats')
  }

  onMount(() => {
    load()
    // Refresh every 3s while the panel is open so the live indicator
    // tracks streams without the user hammering a refresh button.
    timer = setInterval(() => { if (open) load() }, 3000)
  })
  onDestroy(() => clearInterval(timer))

  function toggle() {
    open = !open
    if (open) load()
  }
</script>

<div class="wrap">
  <button class="sess-btn" title="Open sessions — chats, Studio, and Code terminals you have running" on:click={toggle} class:on={open}>
    <span class="dot" class:live={active.size > 0}></span>
    <span class="lbl">Sessions{active.size ? ` · ${active.size} live` : ''}</span>
  </button>

  {#if open}
    <div class="sheet">
      <div class="sheet-head">
        <strong class="grow">Open sessions</strong>
        <button class="x" title="Refresh" on:click={load} disabled={loading}>↻</button>
        <button class="x" title="Close" on:click={() => (open = false)}>×</button>
      </div>
      {#if chats.length === 0}
        <div class="empty">No chats yet — start one from the Code, Chats, or Agents page.</div>
      {/if}
      {#each chats as c}
        {@const s = surfaceOf(c)}
        {@const liveStream = active.has(c.ID)}
        {@const liveTerm = s === 'code' && liveTerms.has(c.ID)}
        <div class="row" role="presentation">
          <button class="row-main grow" on:click={() => jump(c)} title={liveTerm ? `${c.Title} (PTY running — click to reattach)` : `Jump to ${c.Title}`}>
            <span class="pulse" class:live={liveStream || liveTerm}></span>
            <span class="surf surf-{s}">{surfaceLabel(s)}</span>
            <span class="title grow">{c.Title || c.ID}</span>
            <span class="meta">{c.CLIAgent || ''} · {fmtAgo(c.UpdatedAt || c.CreatedAt)}</span>
          </button>
          <button class="row-close" title="Close this session (stops the chat / kills the PTY and deletes the row)" on:click|stopPropagation={() => closeSession(c)}>×</button>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .wrap { position: relative; }
  .sess-btn {
    display: inline-flex; align-items: center; gap: 8px;
    background: none; border: 1px solid transparent;
    color: var(--text); cursor: pointer;
    padding: 5px 10px; font-size: 12px; border-radius: 8px;
    width: 100%; text-align: left;
  }
  .sess-btn:hover, .sess-btn.on {
    background: var(--bg-raised, rgba(255,255,255,0.06));
    border-color: var(--border);
  }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-dim); flex: none; }
  .dot.live { background: #4ec9b0; box-shadow: 0 0 0 0 rgba(78,201,176,0.5); animation: pulse 1.6s infinite; }
  .lbl { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .sheet {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    width: min(420px, 96vw);
    max-height: 60vh;
    overflow-y: auto;
    background: var(--bg-raised, var(--bg-panel));
    border: 1px solid var(--border-bright, var(--border));
    border-radius: 10px;
    box-shadow: 0 12px 36px rgba(0,0,0,0.45);
    z-index: 50;
  }
  .sheet-head { display: flex; gap: 6px; align-items: center; padding: 8px 10px; border-bottom: 1px solid var(--border); }
  .sheet-head .x { background: none; border: none; color: var(--text-dim); cursor: pointer; padding: 2px 8px; font-size: 14px; }
  .empty { padding: 16px; color: var(--text-dim); font-size: 12px; }

  .row {
    display: flex; align-items: stretch;
    border-bottom: 1px solid var(--border);
  }
  .row:last-child { border-bottom: none; }
  .row:hover { background: var(--bg-panel); }
  .row-main {
    display: flex; align-items: center; gap: 8px;
    flex: 1; text-align: left;
    background: none; border: none; color: var(--text);
    padding: 8px 10px; font-size: 12px;
    cursor: pointer; min-width: 0;
  }
  .row-close {
    background: none; border: none; color: var(--text-dim);
    cursor: pointer; padding: 0 12px; font-size: 16px;
    border-left: 1px solid var(--border);
  }
  .row-close:hover { color: var(--err, #e85c5c); background: color-mix(in oklch, var(--err, #e85c5c) 12%, transparent); }
  .pulse { width: 8px; height: 8px; border-radius: 50%; background: var(--border); flex: none; }
  .pulse.live { background: #4ec9b0; animation: pulse 1.6s infinite; }
  .surf {
    font-size: 10px; letter-spacing: 0.04em;
    padding: 2px 6px; border-radius: 6px;
    background: var(--bg-panel); color: var(--text-dim);
    flex: none;
  }
  .surf-studio { color: #b09cff; }
  .surf-code   { color: #ffa657; }
  .surf-helper { color: #79c0ff; }
  .title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .meta { font-size: 10px; color: var(--text-dim); flex: none; }
  .grow { flex: 1; min-width: 0; }

  @keyframes pulse {
    0%   { box-shadow: 0 0 0 0 rgba(78,201,176,0.45); }
    70%  { box-shadow: 0 0 0 6px rgba(78,201,176,0); }
    100% { box-shadow: 0 0 0 0 rgba(78,201,176,0); }
  }
</style>
