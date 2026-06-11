<script>
  import { onMount, tick } from 'svelte'
  import { api } from '../lib/api.js'
  import { activePage, openChatId, pendingTerm } from '../lib/stores.js'

  let chats = []
  let workspaceChats = []
  let error = ''
  let selected = null
  let messages = []
  let draft = ''
  let sending = false
  let threadEl

  // New clean chat (CLI + model, no agent)
  let creating = false
  let clis = []
  let newCli = ''
  let newModel = ''
  let modelSuggestions = []
  let starting = false

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

  async function openNewChat() {
    creating = true
    newModel = ''
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
    try {
      messages = (await api.chatMessages(chat.ID)) || []
      await scrollToBottom()
    } catch (e) {
      error = String(e)
    }
  }

  function back() {
    selected = null
    messages = []
    draft = ''
    openChatId.set(null)
  }

  async function send() {
    const text = draft.trim()
    if (!text || sending || !selected) return
    sending = true
    error = ''
    // Optimistically show the user's message.
    messages = [...messages, { Role: 'user', Content: text, TS: new Date().toISOString(), _pending: true }]
    draft = ''
    await scrollToBottom()
    try {
      const turn = await api.sendChat(selected.ID, text)
      // Replace the optimistic copy with the persisted pair.
      messages = (await api.chatMessages(selected.ID)) || messages
      void turn
    } catch (e) {
      error = String(e)
      messages = messages.filter((m) => !m._pending)
    } finally {
      sending = false
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

  onMount(async () => {
    await load()
    // If Agents started a chat and routed us here, open it.
    const id = $openChatId
    if (id) {
      const c = chats.find((x) => x.ID === id)
      if (c) await open(c)
    }
  })
</script>

{#if selected}
  <div class="row" style="margin-bottom: 12px">
    <button class="btn" on:click={back}>← Chats</button>
    <div class="grow">
      <strong>{selected.Title}</strong>
      <span class="pill">{selected.CLIAgent}</span>
      {#if selected.ExitKind}<span class="pill" class:ok={selected.ExitKind === 'completed'}>{selected.ExitKind}</span>{/if}
    </div>
    <button class="btn danger" on:click={() => remove(selected)}>Delete</button>
  </div>

  {#if error}<div class="banner">{error}</div>{/if}

  <div class="thread" bind:this={threadEl}>
    {#if messages.length === 0}
      <div class="empty">No messages yet — say something below to begin.</div>
    {/if}
    {#each messages as m}
      <div class="msg {m.Role === 'user' ? 'user' : 'assistant'}" class:pending={m._pending}>
        <div class="who">{m.Role}{m.TS ? ' · ' + fmtDate(m.TS) : ''}</div>
        {m.Content}
      </div>
    {/each}
    {#if sending}
      <div class="msg assistant"><div class="who">assistant</div><span class="typing">…thinking</span></div>
    {/if}
  </div>

  <div class="composer">
    <textarea
      class="field"
      rows="2"
      placeholder="Message the agent…  (Enter to send, Shift+Enter for newline)"
      bind:value={draft}
      on:keydown={onKey}
      disabled={sending}></textarea>
    <button class="btn primary" on:click={send} disabled={sending || !draft.trim()}>Send</button>
  </div>
{:else}
  <div class="row" style="margin-bottom: 4px">
    <h1 class="grow" style="margin:0">Chats</h1>
    <button class="btn primary" on:click={openNewChat}>+ New chat</button>
  </div>
  <p class="subtitle">Conversations persist to the shared database. Start a clean chat on any CLI/model, or use an agent persona from the Agents page.</p>

  {#if error}<div class="banner">{error}</div>{/if}

  {#if creating}
    <div class="card">
      <div class="card-title">New clean chat</div>
      <div class="card-sub">A plain conversation with the CLI — no agent persona. Pick the CLI and (optionally) pin the model it should use.</div>
      <label class="lbl">CLI</label>
      <select class="field" style="max-width:320px" bind:value={newCli} on:change={refreshModels}>
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
      <div class="row" style="margin-top:12px">
        <button class="btn primary" on:click={startClean} disabled={starting || !newCli}>
          {starting ? 'Starting…' : 'Start chat'}
        </button>
        <button class="btn" on:click={() => (creating = false)}>Cancel</button>
      </div>
    </div>
  {/if}

  {#if chats.length === 0 && !creating}
    <div class="empty">No chats yet — press “New chat”, or start one from an agent on the Agents page.</div>
  {/if}
  {#each chats as chat}
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
      <button class="btn danger" on:click={() => remove(chat)}>Delete</button>
    </div>
  {/each}

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
    height: calc(100vh - 230px);
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
</style>
