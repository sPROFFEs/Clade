<script>
  // Document studio window (plan §14-P1) — rendered INSTEAD of the main
  // app when this process was spawned with `-editor <folder>`. Left:
  // file tree. Center: tabbed CodeMirror editors. Right: the chat pane
  // driving the agent. The agent's file edits stream in live via
  // "praimate:editor-fs" events and merge into open tabs with a
  // cursor-preserving diff; user keystrokes flush to disk debounced so
  // the agent's next turn reads what's on screen.
  import { onMount, onDestroy, tick } from 'svelte'
  import { marked } from 'marked'
  import { api, onChatStream, onApproval } from '../lib/api.js'
  import CodeEditor from '../lib/CodeEditor.svelte'

  export let folder = ''
  export let chatId = ''

  let files = []
  let error = ''
  let tabs = [] // [{path, content, dirty, ref, flushTimer, externalPending}]
  let active = '' // active tab path

  function lang(path) {
    if (/\.(md|markdown)$/i.test(path)) return 'markdown'
    if (/\.(ya?ml)$/i.test(path)) return 'yaml'
    return 'plain'
  }

  async function loadTree() {
    try {
      files = (await api.editorListFiles()) || []
    } catch (e) {
      error = String(e)
    }
  }

  async function open(path) {
    const existing = tabs.find((t) => t.path === path)
    if (existing) { active = path; return }
    try {
      const content = await api.editorReadFile(path)
      tabs = [...tabs, { path, content, dirty: false, ref: null, flushTimer: null, externalPending: false }]
      active = path
    } catch (e) {
      error = String(e)
    }
  }

  function close(path) {
    const t = tabs.find((x) => x.path === path)
    if (t?.dirty) flush(t)
    tabs = tabs.filter((x) => x.path !== path)
    if (active === path) active = tabs[tabs.length - 1]?.path || ''
  }

  function onEdit(t, content) {
    t.content = content
    t.dirty = true
    tabs = tabs
    clearTimeout(t.flushTimer)
    // Debounced flush keeps disk (what the agent reads) close to the
    // screen without a write per keystroke.
    t.flushTimer = setTimeout(() => flush(t), 600)
  }

  async function flush(t) {
    if (!t.dirty) return
    try {
      await api.editorWriteFile(t.path, t.content)
      t.dirty = false
      tabs = tabs
    } catch (e) {
      error = String(e)
    }
  }

  // Inline new-file input (window.prompt is unavailable in WebView2).
  let newFileName = ''
  let showNewFile = false

  async function newFile() {
    const name = newFileName.trim()
    if (!name) return
    try {
      const rel = await api.editorCreateFile(name)
      showNewFile = false
      newFileName = ''
      await loadTree()
      await open(rel)
    } catch (e) {
      error = String(e)
    }
  }

  // External change (the agent wrote a file): merge into the open tab
  // with the minimal-span diff; refresh the tree for creates/renames.
  async function onFsEvent(ev) {
    await loadTree()
    const t = tabs.find((x) => x.path === ev.path)
    if (!t) return
    try {
      const fresh = await api.editorReadFile(ev.path)
      if (t.dirty) {
        // The user has unflushed keystrokes — don't clobber them.
        // Surface a banner on the tab instead.
        t.externalPending = true
        t.freshContent = fresh
        tabs = tabs
        return
      }
      t.content = fresh
      t.ref?.setExternal(fresh)
      tabs = tabs
    } catch {}
  }

  function acceptExternal(t) {
    t.content = t.freshContent
    t.dirty = false
    t.externalPending = false
    t.ref?.setExternal(t.freshContent)
    tabs = tabs
  }

  function keepMine(t) {
    t.externalPending = false
    tabs = tabs
    flush(t) // our version wins on disk
  }

  // --- layout: collapsible side panes ---------------------------------------

  let treeOpen = true
  let chatOpen = true
  $: gridCols = `${treeOpen ? '220px' : '30px'} 1fr ${chatOpen ? '340px' : '30px'}`

  // --- toolbar + preview ---------------------------------------------------

  let preview = false
  const TOOLBAR = [
    { label: 'B', title: 'Bold', act: (r) => r.wrapSelection('**', '**') },
    { label: 'I', title: 'Italic', act: (r) => r.wrapSelection('*', '*') },
    { label: 'S', title: 'Strikethrough', act: (r) => r.wrapSelection('~~', '~~') },
    { label: 'H1', title: 'Heading 1', act: (r) => r.toggleLinePrefix('# ') },
    { label: 'H2', title: 'Heading 2', act: (r) => r.toggleLinePrefix('## ') },
    { label: 'H3', title: 'Heading 3', act: (r) => r.toggleLinePrefix('### ') },
    { label: '•', title: 'Bullet list', act: (r) => r.toggleLinePrefix('- ') },
    { label: '1.', title: 'Numbered list', act: (r) => r.toggleLinePrefix('1. ') },
    { label: '☑', title: 'Task list', act: (r) => r.toggleLinePrefix('- [ ] ') },
    { label: '❝', title: 'Quote', act: (r) => r.toggleLinePrefix('> ') },
    { label: '</>', title: 'Inline code', act: (r) => r.wrapSelection('`', '`') },
    { label: '```', title: 'Code block', act: (r) => r.wrapSelection('\n```\n', '\n```\n', 'code') },
    { label: '🔗', title: 'Link', act: (r) => r.wrapSelection('[', '](url)') },
    { label: '▦', title: 'Table', act: (r) => r.insertSnippet('\n| Column | Column |\n| --- | --- |\n| cell | cell |\n') },
    { label: '—', title: 'Horizontal rule', act: (r) => r.insertSnippet('\n---\n') },
  ]
  function toolbarAct(item) {
    const t = tabs.find((x) => x.path === active)
    if (t?.ref) item.act(t.ref)
  }
  $: previewHTML = preview && activeTab ? marked.parse(activeTab.content) : ''

  // --- right-click → ask the agent about the selection ----------------------

  let ask = null // {text, fromLine, toLine, x, y, custom}
  const ASK_ACTIONS = [
    'Improve the writing',
    'Fix grammar and spelling',
    'Make it more concise',
    'Expand with more detail',
    'Summarize it',
    'Translate to English',
  ]

  function onAskCtx(e) {
    const d = e.detail
    // Clamp the popup inside the window.
    ask = {
      ...d,
      x: Math.min(d.x, window.innerWidth - 320),
      y: Math.min(d.y, window.innerHeight - 280),
      custom: '',
    }
  }

  async function askAgent(instruction) {
    if (!ask || !instruction.trim() || sending) return
    const a = ask
    ask = null
    const snippet = a.text.length > 4000 ? a.text.slice(0, 4000) + '…' : a.text
    const msg =
      `In ${active} (lines ${a.fromLine}–${a.toLine}), apply this instruction to the selected text and edit the file directly, keeping everything else unchanged.\n\n` +
      `Instruction: ${instruction.trim()}\n\nSelected text:\n"""\n${snippet}\n"""`
    await sendText(msg)
  }

  // --- chat pane ---------------------------------------------------------

  let chat = null
  let messages = []
  let draft = ''
  let sending = false
  let stream = null
  let approvals = []
  let threadEl
  let unsubStream = () => {}
  let unsubApproval = () => {}

  async function loadChat() {
    try {
      messages = (await api.chatMessages(chatId)) || []
      await scrollToBottom()
    } catch (e) {
      error = String(e)
    }
  }

  function handleStreamEvent(ev) {
    if (!sending || ev.chatId !== chatId) return
    if (!stream) stream = { text: '', tools: [] }
    if (ev.type === 'text') stream.text += ev.text
    else if (ev.type === 'tool_start') stream.tools = [...stream.tools, { id: ev.id || '', tool: ev.tool, detail: ev.detail, done: false, ok: true }]
    else if (ev.type === 'tool_end') {
      const t = [...stream.tools]
      let idx = ev.id ? t.findIndex((x) => x.id === ev.id && !x.done) : -1
      if (idx < 0) idx = t.findIndex((x) => !x.done)
      if (idx >= 0) t[idx] = { ...t[idx], done: true, ok: ev.ok }
      stream.tools = t
    }
    stream = stream
    scrollToBottom()
  }

  function handleApproval(req) {
    if (req.chatId !== chatId) {
      api.resolveApproval(req.id, false, false).catch(() => {})
      return
    }
    approvals = [...approvals, req]
  }

  async function answerApproval(req, allow, always) {
    approvals = approvals.filter((a) => a.id !== req.id)
    try { await api.resolveApproval(req.id, allow, always) } catch (e) { error = String(e) }
  }

  async function send() {
    const text = draft.trim()
    if (!text) return
    draft = ''
    await sendText(text)
  }

  async function sendText(text) {
    if (!text || sending) return
    // Flush every dirty tab first so the agent reads what's on screen.
    for (const t of tabs) if (t.dirty) await flush(t)
    sending = true
    stream = null
    error = ''
    const focused = active ? `\n\n[The user is looking at: ${active} — open files: ${tabs.map((t) => t.path).join(', ')}]` : ''
    messages = [...messages, { Role: 'user', Content: text, TS: new Date().toISOString(), _pending: true }]
    await scrollToBottom()
    try {
      if (text.startsWith('!')) {
        await api.runChatCommand(chatId, text.slice(1))
      } else {
        await api.sendChatStream(chatId, text + focused, [])
      }
      messages = (await api.chatMessages(chatId)) || messages
    } catch (e) {
      error = String(e)
      messages = messages.filter((m) => !m._pending)
    } finally {
      sending = false
      stream = null
      approvals = []
      await scrollToBottom()
    }
  }

  async function stop() {
    try { await api.cancelChatTurn(chatId) } catch {}
  }

  function onKey(e) {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
  }

  async function scrollToBottom() {
    await tick()
    if (threadEl) threadEl.scrollTop = threadEl.scrollHeight
  }

  function fmtDate(s) {
    try { return new Date(s).toLocaleTimeString() } catch { return s }
  }

  let unsubFs = () => {}
  onMount(async () => {
    unsubStream = onChatStream(handleStreamEvent)
    unsubApproval = onApproval(handleApproval)
    if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
      window.runtime.EventsOn('praimate:editor-fs', onFsEvent)
      unsubFs = () => window.runtime.EventsOff('praimate:editor-fs')
    }
    await loadTree()
    await loadChat()
    try { chat = (await api.listChats())?.find((c) => c.ID === chatId) || null } catch {}
    // Open the first markdown file so the window isn't empty.
    const first = files.find((f) => /\.md$/i.test(f)) || files[0]
    if (first) await open(first)
  })
  onDestroy(() => { unsubStream(); unsubApproval(); unsubFs() })

  $: activeTab = tabs.find((t) => t.path === active)
</script>

<div class="studio" style="grid-template-columns: {gridCols}">
  {#if !treeOpen}
    <button class="rail" title="Show files" on:click={() => (treeOpen = true)}>▸<span class="rail-label">Files</span></button>
  {:else}
  <aside class="tree">
    <div class="tree-head">
      <button class="btn sm" title="Hide files" on:click={() => (treeOpen = false)}>◂</button>
      <span class="grow mono" title={folder}>{folder.split(/[\\/]/).pop()}</span>
      <button class="btn sm" on:click={() => (showNewFile = !showNewFile)} title="New file">＋</button>
    </div>
    {#if showNewFile}
      <div class="row" style="padding: 0 4px 8px; gap: 4px">
        <input
          class="field grow mono"
          style="font-size: 12px; padding: 4px 6px"
          placeholder="notes.md"
          bind:value={newFileName}
          on:keydown={(e) => e.key === 'Enter' && newFile()} />
        <button class="btn sm primary" on:click={newFile}>OK</button>
      </div>
    {/if}
    {#each files as f}
      <button class="tree-item" class:active={f === active} on:click={() => open(f)} title={f}>{f}</button>
    {/each}
    {#if files.length === 0}<div class="card-sub" style="padding:8px">No editable files yet — create one.</div>{/if}
  </aside>
  {/if}

  <section class="editor-col">
    {#if error}<div class="banner">{error}</div>{/if}
    <div class="tabbar">
      {#each tabs as t}
        <div class="tab" class:active={t.path === active}>
          <button class="tab-name" on:click={() => (active = t.path)}>{t.path.split('/').pop()}{t.dirty ? ' •' : ''}</button>
          <button class="tab-x" on:click={() => close(t.path)}>×</button>
        </div>
      {/each}
    </div>
    {#if activeTab}
      <div class="toolbar">
        {#each TOOLBAR as item}
          <button class="tb-btn" title={item.title} on:click={() => toolbarAct(item)}>{item.label}</button>
        {/each}
        <span class="grow"></span>
        <button class="tb-btn" class:tb-active={preview} title="Toggle rendered preview" on:click={() => (preview = !preview)}>👁 Preview</button>
      </div>
    {/if}
    <div class="editor-split">
      <div class="editor-stack" class:half={preview}>
        {#each tabs as t (t.path)}
          <div class="editor-host" style:display={t.path === active ? 'flex' : 'none'}>
            {#if t.externalPending}
              <div class="conflict">
                The agent changed <span class="mono">{t.path}</span> while you had unsaved edits.
                <button class="btn sm" on:click={() => acceptExternal(t)}>Take agent's version</button>
                <button class="btn sm" on:click={() => keepMine(t)}>Keep mine</button>
              </div>
            {/if}
            <CodeEditor
              bind:this={t.ref}
              value={t.content}
              lang={lang(t.path)}
              on:change={(e) => onEdit(t, e.detail)}
              on:askctx={onAskCtx} />
          </div>
        {/each}
      </div>
      {#if preview && activeTab}
        <div class="preview-pane md">{@html previewHTML}</div>
      {/if}
    </div>
    {#if tabs.length === 0}
      <div class="empty" style="margin-top:40px">Open a file from the tree — the agent's edits appear here live.</div>
    {/if}
  </section>

  {#if ask}
    <div class="ask-popup" style="left:{ask.x}px; top:{ask.y}px">
      <div class="ask-head">Ask the agent — lines {ask.fromLine}–{ask.toLine}</div>
      {#each ASK_ACTIONS as act}
        <button class="ask-act" on:click={() => askAgent(act)} disabled={sending}>{act}</button>
      {/each}
      <div class="row" style="gap:4px; margin-top:6px">
        <input
          class="field grow"
          style="font-size:12px; padding:4px 6px"
          placeholder="Or tell it what to do…"
          bind:value={ask.custom}
          on:keydown={(e) => e.key === 'Enter' && askAgent(ask.custom)} />
        <button class="btn sm primary" on:click={() => askAgent(ask.custom)} disabled={!ask.custom.trim()}>Go</button>
      </div>
      <button class="ask-close" on:click={() => (ask = null)}>×</button>
    </div>
  {/if}

  {#if !chatOpen}
    <button class="rail" title="Show agent chat" on:click={() => (chatOpen = true)}>◂<span class="rail-label">Chat</span></button>
  {:else}
  <aside class="chatpane">
    <div class="chat-head">
      <strong class="grow">{chat?.Title || 'Agent chat'}</strong>
      <span class="pill">{chat?.CLIAgent || ''}</span>
      <button class="btn sm" title="Hide chat" on:click={() => (chatOpen = false)}>▸</button>
    </div>
    <div class="thread" bind:this={threadEl}>
      {#each messages as m}
        <div class="msg {m.Role === 'user' ? 'user' : 'assistant'}" class:pending={m._pending}>
          <div class="who">{m.Role}{m.TS ? ' · ' + fmtDate(m.TS) : ''}</div>
          {m.Content}
        </div>
      {/each}
      {#if sending}
        <div class="msg assistant">
          <div class="who">assistant</div>
          {#if stream?.tools?.length}
            <div class="tool-feed">
              {#each stream.tools as t}
                <div class="tool-row">{t.done ? (t.ok ? '✓' : '✗') : '◌'} {t.tool} <span class="mono">{t.detail || ''}</span></div>
              {/each}
            </div>
          {/if}
          {#if stream?.text}{stream.text}<span class="cursor">▍</span>{:else}<span class="typing">…working</span>{/if}
        </div>
      {/if}
      {#each approvals as ap (ap.id)}
        <div class="approval-card">
          <div>⚠ Permission: <strong>{ap.tool}</strong></div>
          {#if ap.detail}<div class="mono card-sub">{ap.detail}</div>{/if}
          <div class="row" style="margin-top:6px">
            <button class="btn sm primary" on:click={() => answerApproval(ap, true, false)}>Allow</button>
            <button class="btn sm" on:click={() => answerApproval(ap, true, true)}>Always</button>
            <button class="btn sm danger" on:click={() => answerApproval(ap, false, false)}>Deny</button>
          </div>
        </div>
      {/each}
    </div>
    <div class="composer">
      <textarea
        class="field"
        rows="2"
        placeholder="Ask the agent to write or edit the docs…"
        bind:value={draft}
        on:keydown={onKey}
        disabled={sending}></textarea>
      {#if sending}
        <button class="btn danger" on:click={stop}>■</button>
      {:else}
        <button class="btn primary" on:click={send} disabled={!draft.trim()}>Send</button>
      {/if}
    </div>
  </aside>
  {/if}
</div>

<style>
  .studio {
    display: grid;
    /* columns set inline (collapsible side panes) */
    gap: 10px;
    height: 100vh;
    padding: 10px;
    box-sizing: border-box;
  }
  .rail {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--panel);
    color: var(--text-dim);
    cursor: pointer;
    font-size: 12px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding-top: 10px;
  }
  .rail:hover { color: var(--text); }
  .rail-label { writing-mode: vertical-rl; letter-spacing: 0.08em; }
  .tree {
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--panel);
    padding: 6px;
  }
  .tree-head { display: flex; align-items: center; gap: 6px; padding: 4px 6px 8px; font-size: 12px; color: var(--text-dim); }
  .tree-item {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: var(--text);
    font-size: 12px;
    padding: 4px 6px;
    border-radius: 6px;
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tree-item:hover { background: var(--raised, rgba(255,255,255,0.06)); }
  .tree-item.active { background: var(--raised, rgba(255,255,255,0.1)); }
  .editor-col { display: flex; flex-direction: column; min-width: 0; }
  .tabbar { display: flex; gap: 4px; flex-wrap: wrap; margin-bottom: 6px; }
  .tab {
    display: flex;
    align-items: center;
    border: 1px solid var(--border);
    border-radius: 8px 8px 0 0;
    background: var(--panel);
    font-size: 12px;
  }
  .tab.active { background: var(--bg); border-bottom-color: var(--bg); }
  .tab-name { background: none; border: none; color: var(--text); padding: 5px 4px 5px 10px; cursor: pointer; font-size: 12px; }
  .tab-x { background: none; border: none; color: var(--text-dim); cursor: pointer; padding: 5px 8px 5px 2px; }
  .editor-host { flex: 1; min-height: 0; display: flex; flex-direction: column; }
  .editor-host :global(.cm-host) { flex: 1; }
  .conflict {
    border: 1px solid var(--warning, #d4a72c);
    border-radius: var(--radius);
    padding: 6px 10px;
    margin-bottom: 6px;
    font-size: 12px;
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }
  .chatpane {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--panel);
    padding: 8px;
    min-height: 0;
  }
  .chat-head { display: flex; gap: 6px; align-items: center; padding-bottom: 8px; font-size: 13px; }
  .thread { flex: 1; overflow-y: auto; min-height: 0; }
  .composer { display: flex; gap: 6px; align-items: flex-end; padding-top: 8px; }
  .composer textarea { resize: none; }
  .tool-feed { font-size: 11px; color: var(--text-dim); border-left: 2px solid var(--border); padding: 4px 8px; margin-bottom: 6px; }
  .typing { color: var(--text-dim); font-style: italic; }
  .cursor { animation: blink 1s steps(1) infinite; }
  @keyframes blink { 50% { opacity: 0; } }
  .approval-card { border: 1px solid var(--warning, #d4a72c); border-radius: var(--radius); padding: 8px; margin: 6px 0; font-size: 12px; }
  .msg.pending { opacity: 0.6; }
  .btn.sm { padding: 3px 10px; font-size: 12px; }
  .toolbar {
    display: flex;
    gap: 2px;
    align-items: center;
    flex-wrap: wrap;
    padding: 4px 0 6px;
  }
  .tb-btn {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text);
    font-size: 12px;
    padding: 3px 8px;
    cursor: pointer;
    min-width: 28px;
  }
  .tb-btn:hover { background: var(--raised, rgba(255,255,255,0.08)); }
  .tb-btn.tb-active { background: var(--raised, rgba(255,255,255,0.12)); }
  .editor-split { flex: 1; min-height: 0; display: flex; gap: 8px; }
  .editor-stack { flex: 1; min-width: 0; display: flex; flex-direction: column; }
  .editor-stack.half { flex: 1; }
  .preview-pane {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--panel);
    padding: 14px 18px;
    font-size: 14px;
    line-height: 1.55;
  }
  .preview-pane :global(h1), .preview-pane :global(h2), .preview-pane :global(h3) { margin: 0.8em 0 0.4em; }
  .preview-pane :global(code) { background: var(--bg); padding: 1px 4px; border-radius: 4px; font-size: 12px; }
  .preview-pane :global(pre) { background: var(--bg); padding: 8px 10px; border-radius: 6px; overflow-x: auto; }
  .preview-pane :global(blockquote) { border-left: 3px solid var(--border); margin: 0.5em 0; padding-left: 10px; color: var(--text-dim); }
  .preview-pane :global(table) { border-collapse: collapse; }
  .preview-pane :global(td), .preview-pane :global(th) { border: 1px solid var(--border); padding: 4px 8px; }
  .ask-popup {
    position: fixed;
    /* Above CodeMirror's internal layers (tooltips/panels go to ~300). */
    z-index: 10000;
    width: 300px;
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 8px 30px rgba(0,0,0,0.45);
    padding: 10px;
  }
  .ask-head { font-size: 12px; color: var(--text-dim); margin-bottom: 6px; }
  .ask-act {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: var(--text);
    font-size: 13px;
    padding: 5px 6px;
    border-radius: 6px;
    cursor: pointer;
  }
  .ask-act:hover { background: var(--raised, rgba(255,255,255,0.08)); }
  .ask-close {
    position: absolute;
    top: 6px;
    right: 8px;
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 14px;
  }
</style>
