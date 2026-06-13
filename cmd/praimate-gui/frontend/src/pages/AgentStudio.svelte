<script>
  // Agent authoring studio — a Studio-like full-screen view for creating
  // and editing agents. Three panes:
  //   left   : vertical split — knowledge file explorer (top) +
  //            knowledge-base controls / RAG indexing (bottom)
  //   center : the agent's YAML definition (CodeEditor) + Save
  //   right  : an ephemeral AI assistant chat (CLI/model switchable, not
  //            saved) that helps the user write the agent
  import { onMount, onDestroy, tick } from 'svelte'
  import { get } from 'svelte/store'
  import { api, onChatStream, onApproval } from '../lib/api.js'
  import { agentStudio } from '../lib/stores.js'
  import CodeEditor from '../lib/CodeEditor.svelte'

  let error = ''
  let notice = ''

  // --- agent + center editor ---
  let agentId = '' // '' for a new agent until first save
  let isNew = true
  let agentName = ''
  let yamlText = ''
  let editorRef
  let saving = false

  // --- knowledge (left-bottom) ---
  let know = null // AgentKnowledgeInfo
  let knowBusy = false
  const BACKENDS = [
    { id: 'claude-cli', label: 'Claude CLI (no key)' },
    { id: 'local', label: 'Local LLM (Settings)' },
    { id: 'code', label: 'Code-only (AST, no docs)' },
    { id: 'openai', label: 'OpenAI (key)' },
    { id: 'claude', label: 'Claude API (key)' },
    { id: 'gemini', label: 'Gemini (key)' },
    { id: 'deepseek', label: 'DeepSeek (key)' },
  ]
  let ragBackend = 'claude-cli'
  let ragKey = ''
  let ragModel = ''

  // --- RAG progress ---
  let ragRunning = false
  let ragLog = []
  let ragElapsed = 0
  let ragStart = 0
  let ragTimer = null
  let unsubInstall = () => {}

  // --- helper chat (right) ---
  let helperChatId = ''
  let helperCli = ''
  let helperModel = ''
  let clis = []
  let modelSuggestions = []
  let messages = []
  let draft = ''
  let sending = false
  let stream = null
  let approvals = []
  let threadEl
  let unsubStream = () => {}
  let unsubApproval = () => {}

  $: keyNeeded = !['claude-cli', 'local', 'code', ''].includes(ragBackend)
  $: selectedCliInfo = clis.find((c) => c.id === helperCli)
  $: helperModelSupported = !!selectedCliInfo?.modelHint

  // --- layout (collapsible side panes) ---
  let leftOpen = true
  let chatOpen = true
  $: gridCols = `${leftOpen ? '300px' : '34px'} 1fr ${chatOpen ? '360px' : '34px'}`

  async function loadAgent(id) {
    error = ''
    try {
      if (id) {
        isNew = false
        agentId = id
        yamlText = await api.agentYAML(id)
        const a = (await api.listAgents())?.find((x) => x.id === id)
        agentName = a?.name || id
        await loadKnowledge()
      } else {
        isNew = true
        agentId = ''
        agentName = 'New agent'
        yamlText = await api.newAgentTemplateYAML()
        know = null
      }
      editorRef?.setExternal(yamlText)
    } catch (e) {
      error = String(e)
    }
  }

  async function loadKnowledge() {
    if (!agentId) { know = null; return }
    try { know = await api.getAgentKnowledge(agentId) } catch (e) { error = String(e) }
  }

  async function save() {
    if (saving) return
    saving = true
    error = ''
    try {
      const body = editorRef?.getValue() ?? yamlText
      const saved = await api.saveAgentYAML(body)
      agentId = saved.id
      agentName = saved.name
      isNew = false
      notice = `Saved ${saved.name}`
      await loadKnowledge()
    } catch (e) {
      error = String(e)
    } finally {
      saving = false
    }
  }

  // --- knowledge actions ---
  async function setKnowMode(mode) {
    if (!agentId) return
    try {
      await api.setAgentKnowledgeMode(agentId, mode)
      yamlText = await api.agentYAML(agentId) // the knowledge: field changed
      editorRef?.setExternal(yamlText)
      await loadKnowledge()
    } catch (e) { error = String(e) }
  }

  async function addKnowFiles(folder) {
    if (!agentId) return
    try {
      const files = folder
        ? await api.pickAgentKnowledgeFolder(agentId)
        : await api.pickAgentKnowledgeFiles(agentId)
      if (know) { know.files = files || []; know = know }
    } catch (e) { error = String(e) }
  }

  async function rmKnowFile(rel) {
    try {
      const files = await api.deleteAgentKnowledgeFile(agentId, rel)
      if (know) { know.files = files || []; know = know }
    } catch (e) { error = String(e) }
  }

  async function installGraphify() {
    if (knowBusy) return
    knowBusy = true
    try { await api.installBundledGraphify(); notice = 'Bundled graphify installed.'; await loadKnowledge() }
    catch (e) { error = String(e) }
    finally { knowBusy = false }
  }

  async function buildRAG() {
    if (!agentId || ragRunning) return
    ragRunning = true
    ragLog = []
    ragElapsed = 0
    ragStart = Date.now()
    error = ''
    ragTimer = setInterval(() => { ragElapsed = (Date.now() - ragStart) / 1000 }, 200)
    try {
      await api.buildAgentRAG(agentId, ragBackend, ragKey, ragModel)
      notice = 'RAG index built — the agent now retrieves from it.'
      await loadKnowledge()
    } catch (e) {
      error = 'RAG build failed: ' + String(e)
    } finally {
      ragRunning = false
      if (ragTimer) { clearInterval(ragTimer); ragTimer = null }
    }
  }

  // --- helper chat ---
  function handleStreamEvent(ev) {
    if (!sending || ev.chatId !== helperChatId) return
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
    scrollChat()
  }

  function handleApproval(req) {
    if (req.chatId !== helperChatId) { api.resolveApproval(req.id, false, false).catch(() => {}); return }
    approvals = [...approvals, req]
  }
  async function answerApproval(req, allow, always) {
    approvals = approvals.filter((a) => a.id !== req.id)
    try { await api.resolveApproval(req.id, allow, always) } catch (e) { error = String(e) }
  }

  async function send() {
    const text = draft.trim()
    if (!text || sending || !helperChatId) return
    draft = ''
    sending = true
    stream = null
    messages = [...messages, { Role: 'user', Content: text, TS: new Date().toISOString(), _pending: true }]
    await scrollChat()
    try {
      await api.sendChatStream(helperChatId, text, [])
      messages = (await api.chatMessages(helperChatId)) || messages
    } catch (e) {
      error = String(e)
      messages = messages.filter((m) => !m._pending)
    } finally {
      sending = false
      stream = null
      approvals = []
      await scrollChat()
    }
  }
  function onKey(e) { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() } }
  async function stopChat() { try { await api.cancelChatTurn(helperChatId) } catch {} }

  async function applyHelperConfig() {
    if (!helperChatId) return
    try { await api.updateChatConfig(helperChatId, helperCli, helperModelSupported ? helperModel.trim() : '', '', '', '', '') } catch (e) { error = String(e) }
  }
  async function onHelperCli() {
    modelSuggestions = (await api.listCLIModels(helperCli).catch(() => [])) || []
    helperModel = ''
    await applyHelperConfig()
  }

  async function scrollChat() { await tick(); if (threadEl) threadEl.scrollTop = threadEl.scrollHeight }
  function fmtDate(ts) { try { return new Date(ts).toLocaleTimeString() } catch { return '' } }
  function cleanMsg(s) { return (s || '').replace(/\n{3,}/g, '\n\n').trim() }

  async function close() {
    if (helperChatId) { try { await api.deleteChat(helperChatId) } catch {} }
    agentStudio.set(null)
  }

  onMount(async () => {
    const cfg = get(agentStudio) || {}
    try { clis = (await api.listCLIs()) || [] } catch {}
    const firstAvail = clis.find((c) => c.available)
    helperCli = firstAvail ? firstAvail.id : (clis[0]?.id ?? 'claude')
    modelSuggestions = (await api.listCLIModels(helperCli).catch(() => [])) || []
    await loadAgent(cfg.id || '')
    try { const c = await api.startAgentHelperChat(helperCli, helperModel); helperChatId = c.ID } catch (e) { error = String(e) }
    unsubStream = onChatStream(handleStreamEvent)
    unsubApproval = onApproval(handleApproval)
    if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
      window.runtime.EventsOn('praimate:install', (ev) => {
        if (ev && ev.cli === 'graphify:' + agentId) ragLog = [...ragLog.slice(-500), ev.line]
      })
      unsubInstall = () => window.runtime.EventsOff('praimate:install')
    }
  })
  onDestroy(() => { unsubStream(); unsubApproval(); unsubInstall(); if (ragTimer) clearInterval(ragTimer) })
</script>

<div class="astudio" style="grid-template-columns: {gridCols}">
  <!-- LEFT: knowledge files (top) + knowledge controls (bottom) -->
  {#if !leftOpen}
    <button class="rail" title="Show knowledge" on:click={() => (leftOpen = true)}>▸<span class="rail-label">Knowledge</span></button>
  {:else}
  <aside class="left">
    <div class="left-head">
      <strong class="grow">Knowledge base</strong>
      <button class="xbtn" title="Hide" on:click={() => (leftOpen = false)}>◂</button>
    </div>

    <!-- top: file explorer -->
    <div class="files">
      {#if !agentId}
        <div class="hint">Save the agent first to manage its knowledge files.</div>
      {:else if !know?.files?.length}
        <div class="hint">No knowledge files yet — add documents below.</div>
      {:else}
        {#each know.files as f}
          <div class="file-row" title={f}>
            <span class="grow mono">{f}</span>
            <button class="xbtn danger" title="Remove" on:click={() => rmKnowFile(f)}>×</button>
          </div>
        {/each}
      {/if}
    </div>

    <!-- bottom: knowledge controls / RAG -->
    <div class="kctl">
      <div class="lbl2">Mode</div>
      <div class="seg">
        <button class="seg-btn" class:on={know?.mode === '' || !know} disabled={!agentId} on:click={() => setKnowMode('')}>None</button>
        <button class="seg-btn" class:on={know?.mode === 'raw'} disabled={!agentId} on:click={() => setKnowMode('raw')}>Raw</button>
        <button class="seg-btn" class:on={know?.mode === 'rag'} disabled={!agentId} on:click={() => setKnowMode('rag')}>RAG</button>
      </div>

      <div class="row2">
        <button class="btn sm" disabled={!agentId} on:click={() => addKnowFiles(false)}>+ Files</button>
        <button class="btn sm" disabled={!agentId} on:click={() => addKnowFiles(true)}>+ Folder</button>
      </div>

      {#if know?.mode === 'rag'}
        {#if !know.graphifyInstalled}
          <div class="hint" style="color:var(--warn)">graphify not installed — RAG indexing needs it.</div>
          <button class="btn sm" disabled={knowBusy} on:click={installGraphify}>{knowBusy ? 'Installing…' : 'Install graphify'}</button>
        {:else}
          <div class="lbl2">RAG backend</div>
          <select class="field sm" bind:value={ragBackend}>
            {#each BACKENDS as b}<option value={b.id}>{b.label}</option>{/each}
          </select>
          {#if keyNeeded}
            <input class="field sm mono" type="password" placeholder="API key" bind:value={ragKey} />
            <input class="field sm mono" placeholder="model (optional)" bind:value={ragModel} />
          {/if}
          <button class="btn sm primary" disabled={ragRunning || !agentId} on:click={buildRAG}>
            {ragRunning ? 'Indexing…' : (know.hasIndex ? 'Re-index' : 'Build RAG index')}
          </button>

          {#if ragRunning || ragLog.length}
            <div class="rag-prog">
              <div class="rag-bar" class:run={ragRunning}><div class="rag-fill"></div></div>
              <div class="rag-meta">
                {#if ragRunning}<span class="spin">◌</span> indexing… {ragElapsed.toFixed(1)}s
                {:else if know.hasIndex}✓ index ready ({ragElapsed.toFixed(1)}s)
                {:else}done ({ragElapsed.toFixed(1)}s){/if}
              </div>
              {#if ragLog.length}<pre class="rag-log">{ragLog.join('\n')}</pre>{/if}
            </div>
          {:else if know.hasIndex}
            <div class="hint" style="color:var(--ok)">✓ RAG index present.</div>
          {/if}
        {/if}
      {:else if know?.mode === 'raw'}
        <div class="hint">Raw mode: the CLI reads these files directly with its file tools — no indexing.</div>
      {/if}
    </div>
  </aside>
  {/if}

  <!-- CENTER: agent definition editor -->
  <section class="center">
    <div class="center-head">
      <button class="xbtn" title="Back to Agents" on:click={close}>← Agents</button>
      <strong class="grow">{isNew ? 'New agent' : agentName} <span class="card-sub">· definition (YAML)</span></strong>
      <button class="btn primary" on:click={save} disabled={saving}>{saving ? 'Saving…' : (isNew ? 'Create agent' : 'Save')}</button>
    </div>
    {#if error}<div class="banner">{error}</div>{/if}
    {#if notice}<div class="note">{notice}</div>{/if}
    <div class="editor-host">
      <CodeEditor bind:this={editorRef} value={yamlText} lang="yaml" on:change={(e) => (yamlText = e.detail)} />
    </div>
  </section>

  <!-- RIGHT: AI helper chat -->
  {#if !chatOpen}
    <button class="rail" title="Show assistant" on:click={() => (chatOpen = true)}>◂<span class="rail-label">Assistant</span></button>
  {:else}
  <aside class="chat">
    <div class="chat-head">
      <strong class="grow">Authoring assistant</strong>
      <button class="xbtn" title="Hide" on:click={() => (chatOpen = false)}>▸</button>
    </div>
    <div class="row2" style="padding:0 2px 6px">
      <select class="field sm" style="max-width:130px" bind:value={helperCli} on:change={onHelperCli}>
        {#each clis as c}<option value={c.id} disabled={!c.available}>{c.id}{c.available ? '' : ' (n/a)'}</option>{/each}
      </select>
      <input class="field sm mono grow" list="helper-models" placeholder={helperModelSupported ? 'model (blank = default)' : 'no model flag'} bind:value={helperModel} on:change={applyHelperConfig} disabled={!helperModelSupported} />
      <datalist id="helper-models">{#each modelSuggestions as m}<option value={m}></option>{/each}</datalist>
    </div>
    <div class="thread" bind:this={threadEl}>
      {#if messages.length === 0 && !sending}
        <div class="hint">Ask me to draft instructions, suggest workflows, pick CLIs/tools, or review your YAML. I'm not saved to the agent — just here to help.</div>
      {/if}
      {#each messages as m}
        <div class="msg {m.Role === 'user' ? 'user' : 'assistant'}" class:pending={m._pending}>
          <div class="who">{m.Role}{m.TS ? ' · ' + fmtDate(m.TS) : ''}</div>
          {cleanMsg(m.Content)}
        </div>
      {/each}
      {#if sending}
        <div class="msg assistant">
          <div class="who">assistant</div>
          {#if stream?.tools?.length}
            <div class="tool-feed">{#each stream.tools as t}<div>{t.done ? (t.ok ? '✓' : '✗') : '◌'} {t.tool} <span class="mono">{t.detail || ''}</span></div>{/each}</div>
          {/if}
          {#if stream?.text}{stream.text}<span class="cursor">▍</span>{:else}<span class="typing">…thinking</span>{/if}
        </div>
      {/if}
      {#each approvals as ap (ap.id)}
        <div class="approval">
          <div>⚠ Permission: <strong>{ap.tool}</strong></div>
          {#if ap.detail}<div class="mono card-sub">{ap.detail}</div>{/if}
          <div class="row2" style="margin-top:6px">
            <button class="btn sm primary" on:click={() => answerApproval(ap, true, false)}>Allow</button>
            <button class="btn sm" on:click={() => answerApproval(ap, true, true)}>Always</button>
            <button class="btn sm danger" on:click={() => answerApproval(ap, false, false)}>Deny</button>
          </div>
        </div>
      {/each}
    </div>
    <div class="composer">
      <textarea class="field" rows="2" placeholder="Ask the assistant to help build this agent…" bind:value={draft} on:keydown={onKey} disabled={sending}></textarea>
      {#if sending}<button class="btn danger" on:click={stopChat}>■</button>{:else}<button class="btn primary" on:click={send} disabled={!draft.trim() || !helperChatId}>Send</button>{/if}
    </div>
  </aside>
  {/if}
</div>

<style>
  .astudio {
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    gap: 10px;
    height: 100vh;
    padding: 10px;
    box-sizing: border-box;
    overflow: hidden;
    background: var(--bg);
  }
  .rail {
    border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-panel);
    color: var(--text-dim); cursor: pointer; font-size: 12px; display: flex; flex-direction: column;
    align-items: center; gap: 8px; padding-top: 10px;
  }
  .rail-label { writing-mode: vertical-rl; letter-spacing: 0.08em; }

  /* LEFT */
  .left { display: flex; flex-direction: column; min-height: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-panel); }
  .left-head { display: flex; align-items: center; gap: 6px; padding: 8px 10px; border-bottom: 1px solid var(--border); font-size: 13px; }
  .files { flex: 1 1 45%; min-height: 60px; overflow-y: auto; padding: 6px; border-bottom: 1px solid var(--border); }
  .file-row { display: flex; align-items: center; gap: 4px; padding: 3px 4px; border-radius: 6px; font-size: 12px; }
  .file-row:hover { background: var(--bg-raised); }
  .kctl { flex: 1 1 55%; min-height: 0; overflow-y: auto; padding: 8px 10px; display: flex; flex-direction: column; gap: 8px; }
  .lbl2 { font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-dim); }
  .seg { display: flex; gap: 0; }
  .seg-btn { flex: 1; background: var(--bg-panel); border: 1px solid var(--border); color: var(--text-dim); padding: 5px 0; cursor: pointer; font-size: 12px; }
  .seg-btn + .seg-btn { border-left: none; }
  .seg-btn:first-child { border-radius: 7px 0 0 7px; }
  .seg-btn:last-child { border-radius: 0 7px 7px 0; }
  .seg-btn.on { background: var(--bg-raised); color: var(--text); font-weight: 600; }
  .seg-btn:disabled { opacity: 0.45; cursor: default; }
  .row2 { display: flex; gap: 6px; align-items: center; }
  .hint { font-size: 12px; color: var(--text-dim); padding: 4px 2px; line-height: 1.4; }

  /* RAG progress */
  .rag-prog { margin-top: 2px; }
  .rag-bar { height: 6px; border-radius: 4px; background: var(--bg-raised); overflow: hidden; position: relative; }
  .rag-bar .rag-fill { position: absolute; inset: 0; width: 100%; background: var(--accent, #7c6cf2); opacity: 0.25; }
  .rag-bar.run .rag-fill { width: 35%; animation: slide 1.1s ease-in-out infinite; opacity: 0.85; }
  @keyframes slide { 0% { left: -35%; } 100% { left: 100%; } }
  .rag-meta { font-size: 11px; color: var(--text-dim); margin-top: 4px; }
  .spin { display: inline-block; animation: spin 1s steps(8) infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .rag-log { margin-top: 6px; max-height: 120px; overflow-y: auto; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 6px 8px; font-family: var(--mono); font-size: 10.5px; color: var(--text-dim); white-space: pre-wrap; }

  /* CENTER */
  .center { display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden; }
  .center-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
  .editor-host { flex: 1; min-height: 0; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .editor-host :global(.cm-host) { flex: 1; }
  .note { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 8px; padding: 6px 10px; font-size: 12px; color: var(--text-dim); margin-bottom: 6px; }

  /* RIGHT chat */
  .chat { display: flex; flex-direction: column; min-height: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-panel); padding: 8px; }
  .chat-head { display: flex; align-items: center; gap: 6px; padding-bottom: 8px; font-size: 13px; }
  .thread { flex: 1; overflow-y: auto; min-height: 0; }
  .msg { padding: 7px 9px; border-radius: 8px; margin-bottom: 6px; font-size: 13px; white-space: pre-wrap; line-height: 1.45; }
  .msg.user { background: var(--bg-raised); }
  .msg.assistant { background: var(--bg); border: 1px solid var(--border); }
  .msg.pending { opacity: 0.6; }
  .who { font-size: 10.5px; color: var(--text-dim); margin-bottom: 3px; }
  .tool-feed { font-size: 11px; color: var(--text-dim); border-left: 2px solid var(--border); padding: 3px 8px; margin-bottom: 6px; }
  .typing { color: var(--text-dim); font-style: italic; }
  .cursor { animation: blink 1s steps(1) infinite; }
  @keyframes blink { 50% { opacity: 0; } }
  .approval { border: 1px solid var(--warn, #d4a72c); border-radius: 8px; padding: 8px; margin: 6px 0; font-size: 12px; }
  .composer { display: flex; gap: 6px; align-items: flex-end; padding-top: 8px; }
  .composer textarea { resize: none; }

  .field.sm { font-size: 12px; padding: 4px 6px; }
  .btn.sm { padding: 4px 10px; font-size: 12px; }
  .xbtn { background: none; border: none; color: var(--text-dim); cursor: pointer; font-size: 13px; padding: 2px 6px; border-radius: 6px; }
  .xbtn:hover { background: var(--bg-raised); color: var(--text); }
  .xbtn.danger:hover { color: var(--err, #e5484d); }
  .grow { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mono { font-family: var(--mono); }
  .card-sub { color: var(--text-dim); font-size: 11px; font-weight: 400; }
</style>
