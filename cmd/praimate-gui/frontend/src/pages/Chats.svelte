<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let chats = []
  let error = ''
  let selected = null
  let messages = []

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
    } catch (e) {
      error = String(e)
    }
  }

  async function remove(chat) {
    if (!confirm(`Delete chat "${chat.Title}"? This removes its messages too.`)) return
    try {
      await api.deleteChat(chat.ID)
      if (selected?.ID === chat.ID) { selected = null; messages = [] }
      await load()
    } catch (e) {
      error = String(e)
    }
  }

  function fmtDate(s) {
    try { return new Date(s).toLocaleString() } catch { return s }
  }

  onMount(load)
</script>

<h1>Chats</h1>
<p class="subtitle">Workflow runs persisted to the shared database — the TUI sees the same list.</p>

{#if error}<div class="banner">{error}</div>{/if}

{#if selected}
  <div class="row" style="margin-bottom: 14px">
    <button class="btn" on:click={() => { selected = null; messages = [] }}>← Back</button>
    <div class="grow">
      <strong>{selected.Title}</strong>
      <span class="pill">{selected.CLIAgent}</span>
      {#if selected.ExitKind}<span class="pill" class:ok={selected.ExitKind === 'completed'} class:err={selected.ExitKind !== 'completed'}>{selected.ExitKind}</span>{/if}
    </div>
  </div>
  {#if messages.length === 0}
    <div class="empty">No messages stored for this chat.</div>
  {/if}
  {#each messages as m}
    <div class="msg {m.Role === 'user' ? 'user' : 'assistant'}">
      <div class="who">{m.Role} · {fmtDate(m.TS)}</div>
      {m.Content}
    </div>
  {/each}
{:else}
  {#if chats.length === 0}
    <div class="empty">No chats yet — launch one from the Run page.</div>
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
      <button class="btn danger" on:click={() => remove(chat)}>Delete</button>
    </div>
  {/each}
{/if}
