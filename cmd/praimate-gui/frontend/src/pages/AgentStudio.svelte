<script>
  // Agent authoring studio — an IDE-like view for creating/editing agents.
  //   left   : vertical split — agent file tree (top, agent.yaml +
  //            knowledge folder + RAG index) and knowledge/RAG controls
  //            (bottom)
  //   center : tabbed editor — the agent definition (saved to the DB) plus
  //            any opened knowledge files (saved to disk). Right-click a
  //            selection to ask the authoring assistant about it.
  //   right  : an ephemeral AI assistant (CLI/model switchable, not saved)
  import { onMount, onDestroy, tick } from 'svelte'
  import { get } from 'svelte/store'
  import { api, onChatStream, onApproval } from '../lib/api.js'
  import { agentStudio } from '../lib/stores.js'
  import CodeEditor from '../lib/CodeEditor.svelte'

  const DEF = '__definition__'

  let error = ''
  let notice = ''

  // --- agent identity / new-agent name prompt ---
  let agentId = ''
  let isNew = true
  let agentName = ''
  let needName = false
  let newName = ''
  let creating = false

  // --- tabs (center) ---
  // {key, label, lang, content, dirty, ref, isDef}
  let tabs = []
  let active = ''

  // --- file tree (left-top) ---
  let tree = [] // AgentFileNode[]

  // --- knowledge / RAG (left-bottom) ---
  let know = null
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

  // --- ask menu (right-click) ---
  let askSel = null
  const ASK_ACTIONS = ['Improve the wording', 'Make it more concise', 'Explain this', 'Suggest a workflow for this', 'Find issues']

  $: keyNeeded = !['claude-cli', 'local', 'code', ''].includes(ragBackend)
  $: selectedCliInfo = clis.find((c) => c.id === helperCli)
  $: helperModelSupported = !!selectedCliInfo?.modelHint
  $: activeTab = tabs.find((t) => t.key === active)

  // --- layout ---
  let leftOpen = true
  let chatOpen = true
  $: gridCols = `${leftOpen ? '300px' : '34px'} 1fr ${chatOpen ? '360px' : '34px'}`

  function langOf(rel) {
    if (/\.(ya?ml)$/i.test(rel)) return 'yaml'
    if (/\.(md|markdown)$/i.test(rel)) return 'markdown'
    return 'plain'
  }

  // --- load everything for an agent id ---
  async function loadAll(id) {
    error = ''
    agentId = id
    isNew = false
    needName = false
    try {
      const defYaml = await api.agentYAML(id)
      const a = (await api.listAgents())?.find((x) => x.id === id)
      agentName = a?.name || id
      tabs = [{ key: DEF, label: 'agent.yaml', lang: 'yaml', content: defYaml, dirty: false, ref: null, isDef: true }]
      active = DEF
      await refreshTree()
      await loadKnowledge()
      await tick()
      tabs.find((t) => t.isDef)?.ref?.setExternal(defYaml)
    } catch (e) {
      error = String(e)
    }
  }

  async function refreshTree() {
    if (!agentId) { tree = []; return }
    try { tree = (await api.agentKnowledgeTree(agentId)) || [] } catch { tree = [] }
  }
  async function loadKnowledge() {
    if (!agentId) { know = null; return }
    try { know = await api.getAgentKnowledge(agentId) } catch (e) { error = String(e) }
  }

  async function createNamed() {
    if (creating || !newName.trim()) return
    creating = true
    error = ''
    try {
      const a = await api.createAgentFromName(newName.trim())
      // Load in place — do NOT change the agentStudio store, or the
      // {#key} in App.svelte would remount us and leak a 2nd helper chat.
      await loadAll(a.id)
    } catch (e) {
      error = String(e)
    } finally {
      creating = false
    }
  }

  // --- tabs ---
  async function openFile(rel) {
    const ex = tabs.find((t) => t.key === rel)
    if (ex) { active = rel; return }
    try {
      const content = await api.agentReadKnowledgeFile(agentId, rel)
      tabs = [...tabs, { key: rel, label: rel.split('/').pop(), lang: langOf(rel), content, dirty: false, ref: null, isDef: false }]
      active = rel
      await tick()
      tabs.find((t) => t.key === rel)?.ref?.setExternal(content)
    } catch (e) { error = String(e) }
  }
  function closeTab(key) {
    if (key === DEF) return // definition tab stays
    tabs = tabs.filter((t) => t.key !== key)
    if (active === key) active = tabs[tabs.length - 1]?.key || DEF
  }
  function onEdit(tab, content) {
    tab.content = content
    tab.dirty = true
    tabs = tabs
  }

  async function saveActive() {
    const t = activeTab
    if (!t) return
    const body = t.ref?.getValue() ?? t.content
    try {
      if (t.isDef) {
        const saved = await api.saveAgentYAML(body)
        agentId = saved.id
        agentName = saved.name
        notice = `Saved ${saved.name}`
        await refreshTree()
        await loadKnowledge()
      } else {
        await api.agentWriteKnowledgeFile(agentId, t.key, body)
        notice = `Saved ${t.label}`
      }
      t.dirty = false
      tabs = tabs
    } catch (e) { error = String(e) }
  }

  // --- knowledge actions ---
  async function setKnowMode(mode) {
    if (!agentId) return
    try {
      await api.setAgentKnowledgeMode(agentId, mode)
      const y = await api.agentYAML(agentId)
      const d = tabs.find((t) => t.isDef)
      if (d) { d.content = y; d.dirty = false; d.ref?.setExternal(y); tabs = tabs }
      await loadKnowledge()
    } catch (e) { error = String(e) }
  }
  async function addKnowFiles(folder) {
    if (!agentId) return
    try {
      folder ? await api.pickAgentKnowledgeFolder(agentId) : await api.pickAgentKnowledgeFiles(agentId)
      await refreshTree(); await loadKnowledge()
    } catch (e) { error = String(e) }
  }
  async function newFilePrompt() {
    if (!agentId) return
    const name = window.prompt ? window.prompt('New file (e.g. notes.md or subdir/notes.md):', '') : ''
    if (!name) return
    try {
      const rel = await api.agentCreateKnowledgeFile(agentId, name)
      await refreshTree()
      await openFile(rel)
    } catch (e) { error = String(e) }
  }
  async function rmFile(rel) {
    try {
      await api.deleteAgentKnowledgeFile(agentId, rel)
      closeTab(rel)
      await refreshTree(); await loadKnowledge()
    } catch (e) { error = String(e) }
  }
  async function installGraphify() {
    if (knowBusy) return
    knowBusy = true
    try { await api.installBundledGraphify(); notice = 'graphify installed.'; await loadKnowledge() }
    catch (e) { error = String(e) } finally { knowBusy = false }
  }
  async function buildRAG() {
    if (!agentId || ragRunning) return
    ragRunning = true; ragLog = []; ragElapsed = 0; ragStart = Date.now(); error = ''
    ragTimer = setInterval(() => { ragElapsed = (Date.now() - ragStart) / 1000 }, 200)
    try {
      await api.buildAgentRAG(agentId, ragBackend, ragKey, ragModel)
      notice = 'RAG index built.'
      await refreshTree(); await loadKnowledge()
    } catch (e) { error = 'RAG build failed: ' + String(e) }
    finally { ragRunning = false; if (ragTimer) { clearInterval(ragTimer); ragTimer = null } }
  }

  // --- right-click ask menu → routes to the authoring assistant ---
  function onAskCtx(e) {
    askSel = { ...e.detail }
    activeTab?.ref?.openAskMenu(buildAskDom)
  }
  function closeAsk() { activeTab?.ref?.closeAskMenu(); askSel = null }
  function buildAskDom() {
    const root = document.createElement('div')
    root.className = 'ask-menu'
    const head = document.createElement('div')
    head.className = 'ask-head'
    head.textContent = `Ask the assistant — lines ${askSel.fromLine}–${askSel.toLine}`
    const x = document.createElement('div')
    x.className = 'ask-close'; x.setAttribute('role', 'button'); x.tabIndex = 0; x.textContent = '×'
    x.onclick = closeAsk
    head.appendChild(x); root.appendChild(head)
    for (const act of ASK_ACTIONS) {
      const b = document.createElement('div')
      b.className = 'ask-item'; b.setAttribute('role', 'button'); b.tabIndex = 0; b.textContent = act
      b.onclick = () => askAgent(act)
      b.onkeydown = (ev) => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); askAgent(act) } }
      root.appendChild(b)
    }
    const row = document.createElement('div'); row.className = 'ask-free'
    const input = document.createElement('input'); input.className = 'field'; input.placeholder = 'Or tell it what to do…'
    input.onkeydown = (ev) => { if (ev.key === 'Enter' && input.value.trim()) askAgent(input.value); if (ev.key === 'Escape') closeAsk(); ev.stopPropagation() }
    const go = document.createElement('div'); go.className = 'ask-go'; go.setAttribute('role', 'button'); go.tabIndex = 0; go.textContent = 'Go'
    go.onclick = () => input.value.trim() && askAgent(input.value)
    row.appendChild(input); row.appendChild(go); root.appendChild(row)
    root.onmousedown = (ev) => ev.stopPropagation()
    setTimeout(() => input.focus(), 0)
    return root
  }
  async function askAgent(instruction) {
    if (!askSel || !instruction.trim()) return
    const s = askSel
    closeAsk()
    chatOpen = true
    const snippet = s.text.length > 3000 ? s.text.slice(0, 3000) + '…' : s.text
    await sendMsg(`While authoring agent "${agentName}", help with this (lines ${s.fromLine}–${s.toLine} of ${activeTab?.label}):\n\nInstruction: ${instruction.trim()}\n\nSelected:\n"""\n${snippet}\n"""`)
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
    stream = stream; scrollChat()
  }
  function handleApproval(req) {
    if (req.chatId !== helperChatId) { api.resolveApproval(req.id, false, false).catch(() => {}); return }
    approvals = [...approvals, req]
  }
  async function answerApproval(req, allow, always) {
    approvals = approvals.filter((a) => a.id !== req.id)
    try { await api.resolveApproval(req.id, allow, always) } catch (e) { error = String(e) }
  }
  async function send() { const text = draft.trim(); if (!text) return; draft = ''; await sendMsg(text) }
  async function sendMsg(text) {
    if (!text || sending || !helperChatId) return
    sending = true; stream = null
    messages = [...messages, { Role: 'user', Content: text, TS: new Date().toISOString(), _pending: true }]
    await scrollChat()
    try {
      await api.sendChatStream(helperChatId, text, [])
      messages = (await api.chatMessages(helperChatId)) || messages
    } catch (e) { error = String(e); messages = messages.filter((m) => !m._pending) }
    finally { sending = false; stream = null; approvals = []; await scrollChat() }
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
    if (cfg.id) await loadAll(cfg.id)
    else { needName = true; isNew = true } // new agent → ask the name first
    try { const c = await api.startAgentHelperChat(helperCli, helperModel); helperChatId = c.ID } catch (e) { error = String(e) }
    unsubStream = onChatStream(handleStreamEvent)
    unsubApproval = onApproval(handleApproval)
    if (typeof window !== 'undefined' && window.runtime?.EventsOn) {
      window.runtime.EventsOn('praimate:install', (ev) => {
        if (ev && ev.cli === 'graphify:' + agentId) ragLog = [...ragLog.slice(-800), ev.line]
      })
      unsubInstall = () => window.runtime.EventsOff('praimate:install')
    }
  })
  onDestroy(() => { unsubStream(); unsubApproval(); unsubInstall(); if (ragTimer) clearInterval(ragTimer) })
</script>

{#if needName}
  <div class="name-overlay">
    <div class="name-card">
      <h2>Name your agent</h2>
      <p class="sub">A folder is created under your praimate config and the template is loaded with this name.</p>
      {#if error}<div class="banner">{error}</div>{/if}
      <input class="field" placeholder="e.g. Code Review" bind:value={newName} on:keydown={(e) => e.key === 'Enter' && createNamed()} autofocus />
      <div class="row2" style="margin-top:14px; justify-content:flex-end">
        <button class="btn" on:click={close}>Cancel</button>
        <button class="btn primary" on:click={createNamed} disabled={creating || !newName.trim()}>{creating ? 'Creating…' : 'Create & open'}</button>
      </div>
    </div>
  </div>
{:else}
<div class="astudio" style="grid-template-columns: {gridCols}">
  <!-- LEFT -->
  {#if !leftOpen}
    <button class="rail" title="Show files" on:click={() => (leftOpen = true)}>▸<span class="rail-label">Files</span></button>
  {:else}
  <aside class="left">
    <div class="left-head">
      <strong class="grow">{agentName}</strong>
      <button class="xbtn" title="New file" on:click={newFilePrompt}>＋</button>
      <button class="xbtn" title="Hide" on:click={() => (leftOpen = false)}>◂</button>
    </div>

    <!-- file tree -->
    <div class="files">
      <button class="tree-item" class:on={active === DEF} on:click={() => (active = DEF)}>📄 agent.yaml <span class="tag">definition</span></button>
      {#each tree as n}
        <div class="tree-row" style="padding-left:{8 + n.depth * 12}px" title={n.rel}>
          {#if n.isDir}
            <span class="tree-item dir" class:idx={n.isIndex}>{n.isIndex ? '🗂' : '📁'} {n.name}{#if n.isIndex} <span class="tag idx">RAG index</span>{/if}</span>
          {:else}
            <button class="tree-item file grow" class:on={active === n.rel} on:click={() => openFile(n.rel)}>{n.isIndex ? '◦' : '📄'} {n.name}</button>
            {#if !n.isIndex}<button class="xbtn danger" title="Delete" on:click={() => rmFile(n.rel)}>×</button>{/if}
          {/if}
        </div>
      {/each}
      <div class="row2" style="padding:8px 4px 2px">
        <button class="btn sm" on:click={() => addKnowFiles(false)}>+ Files</button>
        <button class="btn sm" on:click={() => addKnowFiles(true)}>+ Folder</button>
      </div>
    </div>

    <!-- knowledge / RAG controls -->
    <div class="kctl">
      <div class="lbl2">Knowledge mode</div>
      <div class="seg">
        <button class="seg-btn" class:on={know?.mode === '' || !know} on:click={() => setKnowMode('')}>None</button>
        <button class="seg-btn" class:on={know?.mode === 'raw'} on:click={() => setKnowMode('raw')}>Raw</button>
        <button class="seg-btn" class:on={know?.mode === 'rag'} on:click={() => setKnowMode('rag')}>RAG</button>
      </div>

      {#if know?.mode === 'rag'}
        {#if !know.graphifyInstalled}
          <div class="hint" style="color:var(--warn)">graphify not installed.</div>
          <button class="btn sm" disabled={knowBusy} on:click={installGraphify}>{knowBusy ? 'Installing…' : 'Install graphify'}</button>
        {:else}
          <div class="lbl2">RAG backend</div>
          <select class="field sm" bind:value={ragBackend}>{#each BACKENDS as b}<option value={b.id}>{b.label}</option>{/each}</select>
          {#if keyNeeded}
            <input class="field sm mono" type="password" placeholder="API key" bind:value={ragKey} />
            <input class="field sm mono" placeholder="model (optional)" bind:value={ragModel} />
          {/if}
          <button class="btn sm primary" disabled={ragRunning} on:click={buildRAG}>{ragRunning ? 'Indexing…' : (know.hasIndex ? 'Re-index' : 'Build RAG index')}</button>
          {#if ragRunning || ragLog.length}
            <div class="rag-bar" class:run={ragRunning}><div class="rag-fill"></div></div>
            <div class="rag-meta">
              {#if ragRunning}<span class="spin">◌</span> indexing… {ragElapsed.toFixed(1)}s
              {:else if know.hasIndex}✓ index ready ({ragElapsed.toFixed(1)}s)
              {:else}done ({ragElapsed.toFixed(1)}s){/if}
            </div>
            <pre class="rag-log">{ragLog.join('\n') || '(waiting for graphify output…)'}</pre>
          {:else if know.hasIndex}
            <div class="hint" style="color:var(--ok)">✓ RAG index present (see graphify-out above).</div>
          {/if}
        {/if}
      {:else if know?.mode === 'raw'}
        <div class="hint">Raw mode: the CLI reads these files directly — no indexing.</div>
      {:else}
        <div class="hint">Pick Raw (read files directly) or RAG (graphify-indexed retrieval) to give this agent a knowledge base.</div>
      {/if}
    </div>
  </aside>
  {/if}

  <!-- CENTER -->
  <section class="center">
    <div class="center-head">
      <button class="xbtn" title="Back to Agents" on:click={close}>← Agents</button>
      <div class="tabbar grow">
        {#each tabs as t}
          <div class="tab" class:active={t.key === active}>
            <button class="tab-name" on:click={() => (active = t.key)}>{t.label}{t.dirty ? ' •' : ''}</button>
            {#if !t.isDef}<button class="tab-x" on:click={() => closeTab(t.key)}>×</button>{/if}
          </div>
        {/each}
      </div>
      <button class="btn primary" on:click={saveActive}>{activeTab?.isDef ? 'Save agent' : 'Save file'}</button>
    </div>
    {#if error}<div class="banner">{error}</div>{/if}
    {#if notice}<div class="note">{notice}</div>{/if}
    <div class="editor-stack">
      {#each tabs as t (t.key)}
        <div class="editor-host" style:display={t.key === active ? 'flex' : 'none'}>
          <CodeEditor bind:this={t.ref} value={t.content} lang={t.lang} on:change={(e) => onEdit(t, e.detail)} on:askctx={onAskCtx} />
        </div>
      {/each}
    </div>
  </section>

  <!-- RIGHT -->
  {#if !chatOpen}
    <button class="rail" title="Show assistant" on:click={() => (chatOpen = true)}>◂<span class="rail-label">Assistant</span></button>
  {:else}
  <aside class="chat">
    <div class="chat-head">
      <strong class="grow">Authoring assistant</strong>
      <button class="xbtn" title="Hide" on:click={() => (chatOpen = false)}>▸</button>
    </div>
    <div class="row2" style="padding:0 2px 6px">
      <select class="field sm" style="max-width:130px" bind:value={helperCli} on:change={onHelperCli}>{#each clis as c}<option value={c.id} disabled={!c.available}>{c.id}{c.available ? '' : ' (n/a)'}</option>{/each}</select>
      <input class="field sm mono grow" list="helper-models" placeholder={helperModelSupported ? 'model (blank = default)' : 'no model flag'} bind:value={helperModel} on:change={applyHelperConfig} disabled={!helperModelSupported} />
      <datalist id="helper-models">{#each modelSuggestions as m}<option value={m}></option>{/each}</datalist>
    </div>
    <div class="thread" bind:this={threadEl}>
      {#if messages.length === 0 && !sending}<div class="hint">Ask me to draft instructions, suggest workflows, pick CLIs/tools, or review your YAML — or right-click a selection in the editor. I'm not saved to the agent.</div>{/if}
      {#each messages as m}
        <div class="msg {m.Role === 'user' ? 'user' : 'assistant'}" class:pending={m._pending}>
          <div class="who">{m.Role}{m.TS ? ' · ' + fmtDate(m.TS) : ''}</div>{cleanMsg(m.Content)}
        </div>
      {/each}
      {#if sending}
        <div class="msg assistant"><div class="who">assistant</div>
          {#if stream?.tools?.length}<div class="tool-feed">{#each stream.tools as t}<div>{t.done ? (t.ok ? '✓' : '✗') : '◌'} {t.tool}</div>{/each}</div>{/if}
          {#if stream?.text}{stream.text}<span class="cursor">▍</span>{:else}<span class="typing">…thinking</span>{/if}
        </div>
      {/if}
      {#each approvals as ap (ap.id)}
        <div class="approval"><div>⚠ Permission: <strong>{ap.tool}</strong></div>
          <div class="row2" style="margin-top:6px"><button class="btn sm primary" on:click={() => answerApproval(ap, true, false)}>Allow</button><button class="btn sm" on:click={() => answerApproval(ap, true, true)}>Always</button><button class="btn sm danger" on:click={() => answerApproval(ap, false, false)}>Deny</button></div>
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
{/if}

<style>
  .astudio { display: grid; grid-template-rows: minmax(0, 1fr); gap: 10px; height: 100vh; padding: 10px; box-sizing: border-box; overflow: hidden; background: var(--bg); }
  .rail { border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-panel); color: var(--text-dim); cursor: pointer; font-size: 12px; display: flex; flex-direction: column; align-items: center; gap: 8px; padding-top: 10px; }
  .rail-label { writing-mode: vertical-rl; letter-spacing: 0.08em; }

  /* LEFT */
  .left { display: flex; flex-direction: column; min-height: 0; border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-panel); }
  .left-head { display: flex; align-items: center; gap: 4px; padding: 8px 10px; border-bottom: 1px solid var(--border); font-size: 13px; }
  .files { flex: 1 1 50%; min-height: 80px; overflow-y: auto; padding: 6px 4px; border-bottom: 1px solid var(--border); }
  .tree-row { display: flex; align-items: center; gap: 2px; }
  .tree-item { display: block; width: 100%; text-align: left; background: none; border: none; color: var(--text); font-size: 12px; padding: 3px 6px; border-radius: 6px; cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  button.tree-item:hover { background: var(--bg-raised); }
  .tree-item.on { background: var(--bg-raised); font-weight: 600; }
  .tree-item.dir { color: var(--text-dim); cursor: default; }
  .tree-item.idx { color: var(--text-dim); }
  .tag { font-size: 10px; color: var(--text-dim); border: 1px solid var(--border); border-radius: 4px; padding: 0 4px; }
  .tag.idx { color: var(--accent, #7c6cf2); }

  .kctl { flex: 1 1 50%; min-height: 0; overflow-y: auto; padding: 8px 10px; display: flex; flex-direction: column; gap: 8px; }
  .lbl2 { font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-dim); }
  .seg { display: flex; }
  .seg-btn { flex: 1; background: var(--bg-panel); border: 1px solid var(--border); color: var(--text-dim); padding: 5px 0; cursor: pointer; font-size: 12px; }
  .seg-btn + .seg-btn { border-left: none; }
  .seg-btn:first-child { border-radius: 7px 0 0 7px; }
  .seg-btn:last-child { border-radius: 0 7px 7px 0; }
  .seg-btn.on { background: var(--bg-raised); color: var(--text); font-weight: 600; }
  .row2 { display: flex; gap: 6px; align-items: center; }
  .hint { font-size: 12px; color: var(--text-dim); padding: 4px 2px; line-height: 1.4; }

  .rag-bar { height: 6px; border-radius: 4px; background: var(--bg-raised); overflow: hidden; position: relative; }
  .rag-bar .rag-fill { position: absolute; inset: 0; width: 100%; background: var(--accent, #7c6cf2); opacity: 0.25; }
  .rag-bar.run .rag-fill { width: 35%; animation: slide 1.1s ease-in-out infinite; opacity: 0.85; }
  @keyframes slide { 0% { left: -35%; } 100% { left: 100%; } }
  .rag-meta { font-size: 11px; color: var(--text-dim); }
  .spin { display: inline-block; animation: spin 1s steps(8) infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  /* The log fills the remaining vertical space so it's readable. */
  .rag-log { flex: 1; min-height: 90px; overflow-y: auto; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; font-family: var(--mono); font-size: 11px; color: var(--text-dim); white-space: pre-wrap; word-break: break-word; }

  /* CENTER */
  .center { display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden; }
  .center-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
  .tabbar { display: flex; gap: 4px; flex-wrap: wrap; overflow: hidden; }
  .tab { display: flex; align-items: center; border: 1px solid var(--border); border-radius: 8px 8px 0 0; background: var(--bg-panel); font-size: 12px; }
  .tab.active { background: var(--bg); }
  .tab-name { background: none; border: none; color: var(--text); padding: 5px 4px 5px 10px; cursor: pointer; font-size: 12px; }
  .tab-x { background: none; border: none; color: var(--text-dim); cursor: pointer; padding: 5px 8px 5px 2px; }
  .editor-stack { flex: 1; min-height: 0; display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .editor-host { flex: 1; min-height: 0; display: flex; flex-direction: column; }
  .editor-host :global(.cm-host) { flex: 1; }
  .note { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 8px; padding: 6px 10px; font-size: 12px; color: var(--text-dim); margin-bottom: 6px; }

  /* RIGHT */
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

  /* new-agent name overlay */
  .name-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; z-index: 50; }
  .name-card { width: 420px; max-width: 92vw; background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--radius); padding: 22px 24px; }
  .name-card h2 { margin: 0 0 4px; font-size: 16px; }
  .name-card .sub { color: var(--text-dim); font-size: 12.5px; margin: 0 0 14px; }

  /* right-click ask menu — HARDCODED black-on-white (WebKitGTK can't
     reliably render theme vars inside CodeMirror's tooltip). */
  :global(.cm-tooltip.ask-tooltip), :global(.cm-tooltip:has(> .ask-menu)) { background: transparent !important; border: none !important; padding: 0 !important; }
  :global(.ask-menu) { width: 280px; padding: 10px; background: #ffffff; color: #1a1a1a; border: 1px solid #c9c9c9; border-radius: 10px; box-shadow: 0 8px 30px rgba(0,0,0,0.45); font-family: inherit; }
  :global(.ask-menu .ask-head) { font-size: 12px; color: #666; display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px; }
  :global(.ask-menu .ask-item) { display: block; width: 100%; text-align: left; color: #1a1a1a; background: #fff; font-size: 13px; padding: 6px 8px; border-radius: 6px; cursor: pointer; user-select: none; }
  :global(.ask-menu .ask-item:hover) { background: #ececec; color: #000; }
  :global(.ask-menu .ask-free) { display: flex; gap: 4px; margin-top: 8px; }
  :global(.ask-menu .ask-free input) { font-size: 12px; padding: 4px 6px; flex: 1; min-width: 0; background: #fff; color: #1a1a1a; border: 1px solid #c9c9c9; border-radius: 6px; }
  :global(.ask-menu .ask-free input::placeholder) { color: #888; }
  :global(.ask-menu .ask-go) { background: #2563eb; color: #fff; border-radius: 8px; font-size: 12px; padding: 5px 12px; cursor: pointer; user-select: none; }
  :global(.ask-menu .ask-close) { color: #666; cursor: pointer; font-size: 16px; line-height: 1; user-select: none; }
</style>
