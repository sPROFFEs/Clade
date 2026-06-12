<script>
  import { onMount, onDestroy, tick } from 'svelte'
  import { api, onChatStream, onApproval } from '../lib/api.js'
  import { activePage, openChatId, pendingTerm } from '../lib/stores.js'

  let chats = []
  let workspaceChats = []
  let error = ''
  let selected = null
  let messages = []
  let draft = ''
  let sending = false
  let threadEl
  let toolsLevel = ''
  let attachments = [] // staged for the next send: {name, path, image}
  // Live turn state (claude/openclaude/codex stream events; other CLIs
  // resolve in one shot and this stays empty until the reply lands).
  let stream = null // { text, tools: [{id, tool, detail, done, ok}] }
  let unsubStream = () => {}
  let unsubApproval = () => {}
  let approvals = [] // pending mid-turn permission requests for this chat
  const TOOL_LEVELS = [
    { id: '', label: 'Safe', hint: 'read & answer only — edits/commands denied' },
    { id: 'ask', label: 'Ask', hint: 'ask you before each edit/command (claude & openclaude; other CLIs run safe)' },
    { id: 'edits', label: 'Edits', hint: 'auto-approve file edits in the chat folder' },
    { id: 'full', label: 'Full', hint: 'skip all approvals — edits AND commands' },
  ]

  // Escalation hint: when the last reply shows denied/failed tool calls
  // and the chat isn't already at Full, offer a one-click level bump —
  // the "approve and retry" affordance for CLIs without mid-turn asks.
  $: lastAssistant = [...messages].reverse().find((m) => m.Role === 'assistant')
  $: deniedTools = (lastAssistant?.Meta?.activity || []).some((t) => !t.ok)

  // New clean chat (CLI + model + tools, no agent)
  let creating = false
  let clis = []
  let newCli = ''
  let newModel = ''
  let newTools = ''
  let modelSuggestions = []
  let starting = false

  // Per-chat settings editor (CLI / model / tools), mirroring the TUI's
  // per-chat settings sheet. Works on the open thread AND from list rows.
  let cfg = null // {chat, cli, model, tools, local*, suggestions}
  let cfgSaving = false

  // Search across titles AND message content (TUI parity).
  let search = ''
  let searchResults = null
  let searchTimer
  function onSearchInput() {
    clearTimeout(searchTimer)
    const q = search.trim()
    if (!q) { searchResults = null; return }
    searchTimer = setTimeout(async () => {
      try {
        searchResults = (await api.searchChats(q)) || []
      } catch (e) {
        error = String(e)
      }
    }, 250)
  }
  $: shownChats = searchResults ?? chats
  // Studio sessions live in their own section — they're folder-scoped
  // co-editing chats, not regular conversations.
  $: regularChats = shownChats.filter((c) => c.Settings?.surface !== 'studio')
  $: studioChats = shownChats.filter((c) => c.Settings?.surface === 'studio')

  async function reopenStudio(chat) {
    error = ''
    try {
      await api.openEditorWindow(chat.WorkspacePath, '', '', '', chat.ID)
    } catch (e) {
      error = String(e)
    }
  }

  async function load() {
    try {
      chats = (await api.listChats()) || []
      error = ''
    } catch (e) {
      error = String(e)
    }
    try {
      workspaceChats = (await api.listWorkspaceChats()) || []
    } catch {
      workspaceChats = []
    }
  }

  function openConfig(chat) {
    error = ''
    // Open INSTANTLY with what we already know; the CLI availability
    // probe and model suggestions fill in asynchronously (probing 7
    // CLIs takes seconds — a dialog that waits for it reads as broken).
    cfg = {
      chat,
      cli: chat.CLIAgent,
      model: chat.Settings?.model || '',
      tools: chat.Settings?.tools || '',
      localEndpoint: chat.Settings?.local?.endpoint || '',
      localApiKey: chat.Settings?.local?.api_key || '',
      localModel: chat.Settings?.local?.model || '',
      suggestions: [],
    }
    if (clis.length === 0) {
      api.listCLIs().then((r) => { clis = r || [] }).catch(() => {})
    }
    api.listCLIModels(chat.CLIAgent)
      .then((r) => { if (cfg && cfg.chat.ID === chat.ID) { cfg.suggestions = r || []; cfg = cfg } })
      .catch(() => {})
  }

  async function cfgCliChanged() {
    if (!cfg) return
    cfg.suggestions = (await api.listCLIModels(cfg.cli).catch(() => [])) || []
    cfg = cfg
  }

  async function saveConfig() {
    if (!cfg) return
    cfgSaving = true
    error = ''
    try {
      await api.updateChatConfig(
        cfg.chat.ID, cfg.cli, cfg.model.trim(), cfg.tools,
        cfg.localEndpoint.trim(), cfg.localApiKey, cfg.localModel.trim())
      const id = cfg.chat.ID
      cfg = null
      await load()
      const fresh = chats.find((x) => x.ID === id)
      if (selected?.ID === id && fresh) {
        selected = fresh
        toolsLevel = fresh.Settings?.tools || ''
      }
    } catch (e) {
      error = String(e)
    } finally {
      cfgSaving = false
    }
  }

  async function openNewChat() {
    creating = true
    newModel = ''
    newTools = ''
    try {
      clis = (await api.listCLIs()) || []
      const firstAvailable = clis.find((c) => c.available)
      newCli = firstAvailable ? firstAvailable.id : (clis[0]?.id ?? '')
      await refreshModels()
    } catch (e) {
      error = String(e)
    }
  }

  async function refreshModels() {
    modelSuggestions = []
    if (!newCli) return
    try {
      modelSuggestions = (await api.listCLIModels(newCli)) || []
    } catch {
      modelSuggestions = []
    }
  }

  $: selectedCliInfo = clis.find((c) => c.id === newCli)
  $: modelSupported = !!selectedCliInfo?.modelHint

  async function startClean() {
    if (!newCli || starting) return
    starting = true
    error = ''
    try {
      const chat = await api.startCleanChat(newCli, modelSupported ? newModel.trim() : '', '')
      if (newTools) await api.setChatTools(chat.ID, newTools)
      creating = false
      await load()
      const c = chats.find((x) => x.ID === chat.ID) || chat
      await open(c)
    } catch (e) {
      error = String(e)
    } finally {
      starting = false
    }
  }

  async function openWorkspace(wc) {
    error = ''
    try {
      const res = await api.openWorkspaceChat(wc.id)
      pendingTerm.set({
        termId: res.termId,
        cli: res.cli,
        cwd: res.cwd,
        label: wc.label,
        note: res.note,
      })
      activePage.set('code')
    } catch (e) {
      error = String(e)
    }
  }

  async function open(chat) {
    selected = chat
    toolsLevel = chat.Settings?.tools || ''
    attachments = []
    approvals = []
    try {
      messages = (await api.chatMessages(chat.ID)) || []
      await scrollToBottom()
    } catch (e) {
      error = String(e)
    }
  }

  function back() {
    // Deny anything still pending — leaving the chat must not strand
    // the CLI waiting on an answer.
    for (const ap of approvals) api.resolveApproval(ap.id, false, false).catch(() => {})
    approvals = []
    selected = null
    messages = []
    draft = ''
    attachments = []
    openChatId.set(null)
  }

  async function setTools(level) {
    if (!selected) return
    try {
      await api.setChatTools(selected.ID, level)
      toolsLevel = level
      if (selected.Settings) selected.Settings.tools = level
    } catch (e) {
      error = String(e)
    }
  }

  async function attach() {
    if (!selected) return
    try {
      const picked = (await api.pickChatAttachments(selected.ID)) || []
      attachments = [...attachments, ...picked]
    } catch (e) {
      error = String(e)
    }
  }

  function unattach(path) {
    attachments = attachments.filter((a) => a.path !== path)
  }

  function handleStreamEvent(ev) {
    if (!sending || !selected || ev.chatId !== selected.ID) return
    if (!stream) stream = { text: '', tools: [] }
    if (ev.type === 'text') {
      stream.text += ev.text
    } else if (ev.type === 'tool_start') {
      stream.tools = [...stream.tools, { id: ev.id || '', tool: ev.tool, detail: ev.detail, done: false, ok: true }]
    } else if (ev.type === 'tool_end') {
      const t = [...stream.tools]
      let idx = ev.id ? t.findIndex((x) => x.id === ev.id && !x.done) : -1
      if (idx < 0) idx = t.findIndex((x) => !x.done)
      if (idx >= 0) t[idx] = { ...t[idx], done: true, ok: ev.ok }
      stream.tools = t
    }
    stream = stream
    scrollToBottom()
  }

  async function stop() {
    if (!selected) return
    try { await api.cancelChatTurn(selected.ID) } catch {}
  }

  function handleApproval(req) {
    if (!selected || req.chatId !== selected.ID) {
      // Approval for a chat that isn't open — fail closed immediately
      // rather than letting it hang until the broker timeout.
      api.resolveApproval(req.id, false, false).catch(() => {})
      return
    }
    approvals = [...approvals, req]
    scrollToBottom()
  }

  async function answerApproval(req, allow, always) {
    approvals = approvals.filter((a) => a.id !== req.id)
    try { await api.resolveApproval(req.id, allow, always) } catch (e) { error = String(e) }
  }

  async function send() {
    const text = draft.trim()
    if ((!text && attachments.length === 0) || sending || !selected) return
    sending = true
    error = ''
    stream = null
    const isCommand = text.startsWith('!')
    const staged = attachments
    // Optimistically show the user's message.
    messages = [...messages, { Role: 'user', Content: text, TS: new Date().toISOString(), _pending: true }]
    draft = ''
    attachments = []
    await scrollToBottom()
    try {
      if (isCommand) {
        // "!cmd" runs locally in the chat folder — never sent to the model.
        await api.runChatCommand(selected.ID, text.slice(1))
      } else {
        // Streams live events for CLIs that support it; others resolve
        // in one shot. Interrupting (Stop) resolves with the partial.
        await api.sendChatStream(selected.ID, text, staged.map((a) => a.path))
      }
      // Replace the optimistic copy with the persisted pair.
      messages = (await api.chatMessages(selected.ID)) || messages
    } catch (e) {
      error = String(e)
      messages = messages.filter((m) => !m._pending)
      attachments = staged
    } finally {
      sending = false
      stream = null
      approvals = [] // turn is over; anything unanswered was denied/cancelled
      await scrollToBottom()
    }
  }

  function onKey(e) {
    // Enter sends; Shift+Enter inserts a newline.
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  async function remove(chat) {
    if (!confirm(`Delete chat "${chat.Title}"? This removes its messages too.`)) return
    try {
      await api.deleteChat(chat.ID)
      if (selected?.ID === chat.ID) back()
      await load()
    } catch (e) {
      error = String(e)
    }
  }

  async function scrollToBottom() {
    await tick()
    if (threadEl) threadEl.scrollTop = threadEl.scrollHeight
  }

  function fmtDate(s) {
    try { return new Date(s).toLocaleString() } catch { return s }
  }

  function baseName(p) {
    return String(p).split(/[\\/]/).pop()
  }

  // Studio sends append a focused-file context block for the model —
  // hide it when showing the transcript to the human.
  function cleanMsg(s) {
    return String(s).replace(/\n*\[The user is looking at:[^\]]*\]\s*$/, '')
  }

  function isImg(p) {
    return /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(String(p))
  }

  onMount(async () => {
    unsubStream = onChatStream(handleStreamEvent)
    unsubApproval = onApproval(handleApproval)
    await load()
    // If Agents started a chat and routed us here, open it.
    const id = $openChatId
    if (id) {
      const c = chats.find((x) => x.ID === id)
      if (c) await open(c)
    }
  })

  onDestroy(() => {
    unsubStream()
    unsubApproval()
  })
</script>

{#if cfg}
  <div class="card" style="border-color: var(--accent, #888)">
    <div class="card-title">Chat settings — {cfg.chat.Title}</div>
    <div class="card-sub">Same controls as the TUI's per-chat settings. Switching the CLI starts a fresh session on the next message; the history stays.</div>
    <label class="lbl">CLI</label>
    <select class="field" style="max-width:320px" bind:value={cfg.cli} on:change={cfgCliChanged}>
      {#if clis.length === 0}
        <option value={cfg.cli}>{cfg.cli} (probing CLIs…)</option>
      {/if}
      {#each clis as c}
        <option value={c.id} disabled={!c.available && c.id !== cfg.chat.CLIAgent}>
          {c.label}{c.available ? '' : ' — not installed'}
        </option>
      {/each}
    </select>
    <label class="lbl">Model (blank = CLI default)</label>
    <input class="field mono" style="max-width:420px" list="cfg-model-suggestions" bind:value={cfg.model} />
    <datalist id="cfg-model-suggestions">
      {#each cfg.suggestions as m}<option value={m}></option>{/each}
    </datalist>
    <label class="lbl">Tools</label>
    <div class="row">
      {#each TOOL_LEVELS as lvl}
        <button class="btn sm" class:primary={cfg.tools === lvl.id} title={lvl.hint} on:click={() => (cfg.tools = lvl.id)}>{lvl.label}</button>
      {/each}
    </div>
    {#if cfg.cli === 'claude' || cfg.cli === 'openclaude'}
      <label class="lbl" style="margin-top:10px">Local endpoint (optional — routes THIS chat through a self-hosted backend)</label>
      <div class="row">
        <input class="field grow mono" placeholder="http://localhost:11434 (blank = cloud)" bind:value={cfg.localEndpoint} />
        <input class="field mono" style="max-width:180px" type="password" placeholder="API key" bind:value={cfg.localApiKey} />
        <input class="field mono" style="max-width:180px" placeholder="backend model" bind:value={cfg.localModel} />
      </div>
    {:else if cfg.localEndpoint}
      <div class="card-sub" style="margin-top:8px">Per-chat local routing applies to claude/openclaude only — {cfg.cli} reads its global config (Local LLM tab).</div>
    {/if}
    <div class="row" style="margin-top:12px">
      <button class="btn primary" on:click={saveConfig} disabled={cfgSaving}>{cfgSaving ? 'Saving…' : 'Save'}</button>
      <button class="btn" on:click={() => (cfg = null)}>Cancel</button>
    </div>
  </div>
{/if}

{#if selected}
  <div class="row" style="margin-bottom: 12px">
    <button class="btn" on:click={back}>← Chats</button>
    <div class="grow">
      <strong>{selected.Title}</strong>
      <span class="pill">{selected.CLIAgent}</span>
      {#if selected.Settings?.model}<span class="pill">{selected.Settings.model}</span>{/if}
      {#if selected.ExitKind}<span class="pill" class:ok={selected.ExitKind === 'completed'}>{selected.ExitKind}</span>{/if}
    </div>
    <div class="toolpick" title="How much the CLI agent may do: edit files, run commands">
      <span class="lbl-inline">Tools</span>
      {#each TOOL_LEVELS as lvl}
        <button
          class="btn sm"
          class:primary={toolsLevel === lvl.id}
          title={lvl.hint}
          on:click={() => setTools(lvl.id)}>{lvl.label}</button>
      {/each}
    </div>
    <button class="btn" on:click={() => openConfig(selected)} title="Change the CLI / model / tools behind this chat">⚙ Edit</button>
    <button class="btn danger" on:click={() => remove(selected)}>Delete</button>
  </div>

  {#if error}<div class="banner">{error}</div>{/if}

  <div class="thread" bind:this={threadEl}>
    {#if messages.length === 0}
      <div class="empty">No messages yet — say something below to begin.</div>
    {/if}
    {#each messages as m}
      {#if m.Role === 'command'}
        <div class="msg command" class:pending={m._pending}>
          <div class="who">shell{m.TS ? ' · ' + fmtDate(m.TS) : ''}</div>
          <pre class="cmd-out">{m.Content}</pre>
        </div>
      {:else}
        <div class="msg {m.Role === 'user' ? 'user' : 'assistant'}" class:pending={m._pending}>
          <div class="who">{m.Role}{m.TS ? ' · ' + fmtDate(m.TS) : ''}{m.Meta?.interrupted ? ' · interrupted' : ''}</div>
          <!-- content rendered below; studio context block stripped -->
          {#if m.Meta?.activity?.length}
            <div class="tool-feed">
              {#each m.Meta.activity as t}
                <div class="tool-row" class:err={!t.ok}>
                  <span class="tool-status">{t.ok ? '✓' : '✗'}</span>
                  <span class="tool-name">{t.tool}</span>
                  {#if t.detail}<span class="tool-detail mono">{t.detail}</span>{/if}
                </div>
              {/each}
            </div>
          {/if}
          {cleanMsg(m.Content)}
          {#if m.Meta?.attachments}
            <div class="att-row">
              {#each m.Meta.attachments as path}
                {#if isImg(path)}
                  {#await api.attachmentDataURL(path) then url}
                    <img class="att-img" src={url} alt={baseName(path)} title={path} />
                  {:catch}
                    <span class="pill" title={path}>🖼 {baseName(path)}</span>
                  {/await}
                {:else}
                  <span class="pill" title={path}>📄 {baseName(path)}</span>
                {/if}
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    {/each}
    {#if sending}
      <div class="msg assistant">
        <div class="who">assistant</div>
        {#if stream?.tools?.length}
          <div class="tool-feed">
            {#each stream.tools as t}
              <div class="tool-row" class:err={t.done && !t.ok}>
                <span class="tool-status">{t.done ? (t.ok ? '✓' : '✗') : '◌'}</span>
                <span class="tool-name">{t.tool}</span>
                {#if t.detail}<span class="tool-detail mono">{t.detail}</span>{/if}
              </div>
            {/each}
          </div>
        {/if}
        {#if stream?.text}
          <span>{stream.text}</span><span class="cursor">▍</span>
        {:else}
          <span class="typing">…thinking</span>
        {/if}
      </div>
    {/if}
    {#each approvals as ap (ap.id)}
      <div class="approval-card">
        <div class="approval-head">⚠ The agent asks permission to use <strong>{ap.tool}</strong></div>
        {#if ap.detail}<div class="approval-detail mono">{ap.detail}</div>{/if}
        <div class="row" style="margin-top:8px">
          <button class="btn primary" on:click={() => answerApproval(ap, true, false)}>Allow once</button>
          <button class="btn" on:click={() => answerApproval(ap, true, true)}>Always allow “{ap.tool}” here</button>
          <button class="btn danger" on:click={() => answerApproval(ap, false, false)}>Deny</button>
        </div>
      </div>
    {/each}
  </div>

  {#if deniedTools && !sending && toolsLevel !== 'full'}
    <div class="escalate">
      Some tool calls were denied or failed on the last reply.
      {#if selected.CLIAgent === 'claude' || selected.CLIAgent === 'openclaude'}
        Switch to <button class="btn sm" on:click={() => setTools('ask')}>Ask</button> to approve them live, or
      {/if}
      raise the level and ask again:
      <button class="btn sm" on:click={() => setTools('edits')}>Allow edits</button>
      <button class="btn sm" on:click={() => setTools('full')}>Full access</button>
    </div>
  {/if}

  {#if attachments.length > 0}
    <div class="att-row" style="margin-bottom: 8px">
      {#each attachments as a}
        <span class="pill" title={a.path}>
          {a.image ? '🖼' : '📄'} {a.name}
          <button class="chip-x" on:click={() => unattach(a.path)} title="Remove">×</button>
        </span>
      {/each}
    </div>
  {/if}
  <div class="composer">
    <button class="btn" on:click={attach} disabled={sending} title="Attach images, PDFs or documents — the agent reads them from disk">📎</button>
    <textarea
      class="field"
      rows="2"
      placeholder="Message the agent…  (Enter sends · Shift+Enter newline · !cmd runs a shell command in the chat folder)"
      bind:value={draft}
      on:keydown={onKey}
      disabled={sending}></textarea>
    {#if sending}
      <button class="btn danger" on:click={stop} title="Interrupt the turn — text streamed so far is kept">■ Stop</button>
    {:else}
      <button class="btn primary" on:click={send} disabled={!draft.trim() && attachments.length === 0}>Send</button>
    {/if}
  </div>
{:else}
  <div class="row" style="margin-bottom: 4px">
    <h1 class="grow" style="margin:0">Chats</h1>
    <button class="btn primary" on:click={openNewChat}>+ New chat</button>
  </div>
  <p class="subtitle">Conversations persist to the shared database. Start a clean chat on any CLI/model, or use an agent persona from the Agents page.</p>

  <input
    class="field"
    style="max-width:420px; margin-bottom:10px"
    placeholder="Search chats (titles and message content)…"
    bind:value={search}
    on:input={onSearchInput} />
  {#if searchResults !== null}
    <div class="card-sub" style="margin-bottom:8px">{searchResults.length} match(es) for “{search.trim()}”</div>
  {/if}

  {#if error}<div class="banner">{error}</div>{/if}

  {#if creating}
    <div class="card">
      <div class="card-title">New clean chat</div>
      <div class="card-sub">A plain conversation with the CLI — no agent persona. Pick the CLI and (optionally) pin the model it should use.</div>
      <label class="lbl">CLI</label>
      <select class="field" style="max-width:320px" bind:value={newCli} on:change={refreshModels}>
        {#if clis.length === 0}
          <option value="">probing installed CLIs…</option>
        {/if}
        {#each clis as c}
          <option value={c.id} disabled={!c.available}>
            {c.label}{c.available ? '' : ' — not installed'}
          </option>
        {/each}
      </select>
      <label class="lbl">Model {modelSupported ? `(${selectedCliInfo.modelHint})` : '(this CLI has no model flag — it uses its own config)'}</label>
      <input
        class="field mono"
        style="max-width:420px"
        list="model-suggestions"
        placeholder={modelSupported ? 'blank = CLI default' : 'not supported'}
        bind:value={newModel}
        disabled={!modelSupported} />
      <datalist id="model-suggestions">
        {#each modelSuggestions as m}<option value={m}></option>{/each}
      </datalist>
      <label class="lbl">Tools</label>
      <div class="row">
        {#each TOOL_LEVELS as lvl}
          <button class="btn sm" class:primary={newTools === lvl.id} title={lvl.hint} on:click={() => (newTools = lvl.id)}>{lvl.label}</button>
        {/each}
      </div>
      <div class="row" style="margin-top:12px">
        <button class="btn primary" on:click={startClean} disabled={starting || !newCli}>
          {starting ? 'Starting…' : 'Start chat'}
        </button>
        <button class="btn" on:click={() => (creating = false)}>Cancel</button>
      </div>
    </div>
  {/if}

  {#if shownChats.length === 0 && !creating}
    <div class="empty">{searchResults !== null ? 'No chats match the search.' : 'No chats yet — press “New chat”, or start one from an agent on the Agents page.'}</div>
  {/if}
  {#each regularChats as chat}
    <div class="card row">
      <div class="grow" style="cursor:pointer" on:click={() => open(chat)} on:keydown={(e) => e.key === 'Enter' && open(chat)} role="button" tabindex="0">
        <div class="card-title">{chat.Title}</div>
        <div class="card-sub">
          {chat.CLIAgent}
          {#if chat.Settings?.model} · model: {chat.Settings.model}{/if}
          · {fmtDate(chat.UpdatedAt)}
          {#if chat.ExitKind} · {chat.ExitKind}{/if}
        </div>
      </div>
      <button class="btn" on:click={() => open(chat)}>Open</button>
      <button class="btn" on:click={() => openConfig(chat)} title="Change CLI / model / tools">Edit</button>
      <button class="btn danger" on:click={() => remove(chat)}>Delete</button>
    </div>
  {/each}

  {#if studioChats.length > 0}
    <h1 style="font-size:16px; margin-top:24px">Studio sessions</h1>
    <p class="subtitle">Document-studio chats, scoped to a folder. Reopen the studio window to continue co-editing, or open the transcript like any chat.</p>
    {#each studioChats as chat}
      <div class="card row">
        <div class="grow">
          <div class="card-title">{chat.Title}</div>
          <div class="card-sub mono">
            {chat.WorkspacePath} · {chat.CLIAgent}
            {#if chat.Settings?.model} · {chat.Settings.model}{/if}
            · {fmtDate(chat.UpdatedAt)}
          </div>
        </div>
        <button class="btn primary" on:click={() => reopenStudio(chat)}>Reopen studio</button>
        <button class="btn" on:click={() => open(chat)}>Transcript</button>
        <button class="btn" on:click={() => openConfig(chat)}>Edit</button>
        <button class="btn danger" on:click={() => remove(chat)}>Delete</button>
      </div>
    {/each}
  {/if}

  {#if workspaceChats.length > 0}
    <h1 style="font-size:16px; margin-top:24px">Workspace chats (TUI)</h1>
    <p class="subtitle">Chats created in the <span class="mono">praimate</span> TUI. Open one to resume its CLI session in the Code terminal — same sandbox, native resume where the CLI supports it.</p>
    {#each workspaceChats as wc}
      <div class="card row">
        <div class="grow">
          <div class="card-title">{wc.label}</div>
          <div class="card-sub">
            {wc.agent}{#if wc.template} · {wc.template}{/if} · {fmtDate(wc.lastUsed)}
          </div>
        </div>
        <button class="btn" on:click={() => openWorkspace(wc)}>Open in Code</button>
      </div>
    {/each}
  {/if}
{/if}

<style>
  .thread {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
    padding: 14px;
    /* header row (~48px) + composer (~60px) + page padding: the thread
       takes everything else so there's no dead gap under the composer. */
    height: calc(100vh - 172px);
    overflow-y: auto;
    margin-bottom: 12px;
  }
  .composer {
    display: flex;
    gap: 8px;
    align-items: flex-end;
  }
  .composer textarea { resize: none; }
  .msg.pending { opacity: 0.6; }
  .typing { color: var(--text-dim); font-style: italic; }
  .toolpick { display: flex; align-items: center; gap: 4px; }
  .lbl-inline { color: var(--text-dim); font-size: 12px; margin-right: 2px; }
  .btn.sm { padding: 3px 10px; font-size: 12px; }
  .msg.command { background: var(--bg); border: 1px dashed var(--border); }
  .cmd-out {
    margin: 0;
    font-family: var(--mono, ui-monospace, monospace);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .att-row { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
  .att-img {
    max-width: 260px;
    max-height: 180px;
    border-radius: var(--radius);
    border: 1px solid var(--border);
    display: block;
  }
  .chip-x {
    background: none;
    border: none;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 13px;
    padding: 0 0 0 4px;
  }
  .chip-x:hover { color: var(--text); }
  .tool-feed {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 4px 0 8px;
    padding: 6px 8px;
    border-left: 2px solid var(--border);
    font-size: 12px;
    color: var(--text-dim);
  }
  .tool-row { display: flex; gap: 6px; align-items: baseline; min-width: 0; }
  .tool-row.err .tool-status { color: var(--danger, #e5484d); }
  .tool-status { width: 1em; flex: none; }
  .tool-name { font-weight: 600; flex: none; }
  .tool-detail {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
  }
  .cursor { animation: blink 1s steps(1) infinite; }
  @keyframes blink { 50% { opacity: 0; } }
  .approval-card {
    border: 1px solid var(--warning, #d4a72c);
    border-radius: var(--radius);
    background: var(--panel);
    padding: 10px 12px;
    margin: 8px 0;
  }
  .approval-head { font-size: 13px; }
  .approval-detail {
    margin-top: 6px;
    font-size: 12px;
    color: var(--text-dim);
    word-break: break-all;
  }
  .escalate {
    border: 1px dashed var(--border);
    border-radius: var(--radius);
    padding: 8px 10px;
    margin-bottom: 8px;
    font-size: 12px;
    color: var(--text-dim);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
</style>
