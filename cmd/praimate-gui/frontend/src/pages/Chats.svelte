<script>
  import { onMount, tick } from 'svelte'
  import { api } from '../lib/api.js'
  import { openChatId } from '../lib/stores.js'

  let chats = []
  let error = ''
  let selected = null
  let messages = []
  let draft = ''
  let sending = false
  let threadEl

  async function load() {
    try {
      chats = (await api.listChats()) || []
      error = ''
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
  <h1>Chats</h1>
  <p class="subtitle">Conversations persist to the shared database — the TUI sees the same list. Start a new one from the Agents page.</p>

  {#if error}<div class="banner">{error}</div>{/if}

  {#if chats.length === 0}
    <div class="empty">No chats yet — open the Agents page and start one.</div>
  {/if}
  {#each chats as chat}
    <div class="card row">
      <div class="grow" style="cursor:pointer" on:click={() => open(chat)} on:keydown={(e) => e.key === 'Enter' && open(chat)} role="button" tabindex="0">
        <div class="card-title">{chat.Title}</div>
        <div class="card-sub">
          {chat.CLIAgent} · {fmtDate(chat.UpdatedAt)}
          {#if chat.ExitKind} · {chat.ExitKind}{/if}
        </div>
      </div>
      <button class="btn" on:click={() => open(chat)}>Open</button>
      <button class="btn danger" on:click={() => remove(chat)}>Delete</button>
    </div>
  {/each}
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
