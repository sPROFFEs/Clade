<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  let snap = null
  let error = ''
  let newKey = ''
  let newValue = ''
  let newFact = ''

  async function load() {
    try {
      snap = await api.getMemory()
      error = ''
    } catch (e) {
      error = String(e)
    }
  }

  async function toggle() {
    try {
      await api.setMemoryEnabled(!snap.enabled)
      await load()
    } catch (e) { error = String(e) }
  }

  async function addIdentity() {
    if (!newKey.trim()) return
    try {
      await api.setIdentity(newKey.trim(), newValue.trim())
      newKey = ''; newValue = ''
      await load()
    } catch (e) { error = String(e) }
  }

  async function removeIdentity(k) {
    try { await api.deleteIdentity(k); await load() } catch (e) { error = String(e) }
  }

  async function addFact() {
    if (!newFact.trim()) return
    try {
      await api.pinFact(newFact.trim())
      newFact = ''
      await load()
    } catch (e) { error = String(e) }
  }

  async function removeFact(id) {
    try { await api.deletePinned(id); await load() } catch (e) { error = String(e) }
  }

  async function removeEpisode(id) {
    try { await api.deleteEpisode(id); await load() } catch (e) { error = String(e) }
  }

  function fmtDate(s) {
    try { return new Date(s).toLocaleString() } catch { return s }
  }

  onMount(load)
</script>

<h1>Memory</h1>
<p class="subtitle">Cross-chat memory: who you are, durable facts, and per-session episodes. Shared with the TUI.</p>

{#if error}<div class="banner">{error}</div>{/if}

{#if snap}
  <div class="card row">
    <div class="grow">
      <div class="card-title">Memory injection</div>
      <div class="card-sub">When on, new runs get identity + relevant episode + top facts prepended (≤800 tokens).</div>
    </div>
    <button class="btn" class:primary={snap.enabled} on:click={toggle}>
      {snap.enabled ? 'Enabled' : 'Disabled'}
    </button>
  </div>

  <h1 style="font-size:16px; margin-top:24px">Identity</h1>
  {#each snap.identity || [] as row}
    <div class="card row">
      <div class="grow"><strong>{row.Key}</strong>: {row.Value} <span class="pill">{row.Source}</span></div>
      <button class="btn danger" on:click={() => removeIdentity(row.Key)}>Delete</button>
    </div>
  {/each}
  <div class="row" style="margin-top:8px">
    <input class="field" style="max-width:180px" placeholder="key (e.g. name)" bind:value={newKey} />
    <input class="field grow" placeholder="value" bind:value={newValue} />
    <button class="btn primary" on:click={addIdentity}>Add</button>
  </div>

  <h1 style="font-size:16px; margin-top:24px">Pinned facts</h1>
  {#each snap.pinned || [] as f}
    <div class="card row">
      <div class="grow">
        {f.Text}
        <span class="pill">salience {f.Salience.toFixed(2)}</span>
        <span class="pill">used {f.UseCount}×</span>
      </div>
      <button class="btn danger" on:click={() => removeFact(f.ID)}>Delete</button>
    </div>
  {/each}
  <div class="row" style="margin-top:8px">
    <input class="field grow" placeholder="new fact (e.g. prefers spaces over tabs)" bind:value={newFact} />
    <button class="btn primary" on:click={addFact}>Pin</button>
  </div>

  <h1 style="font-size:16px; margin-top:24px">Episodes</h1>
  {#if (snap.episodes || []).length === 0}
    <div class="empty">No episodes yet — completed runs distill here when memory is on.</div>
  {/if}
  {#each snap.episodes || [] as ep}
    <div class="card">
      <div class="row">
        <div class="grow card-sub">{fmtDate(ep.CreatedAt)}{ep.ChatID ? ` · ${ep.ChatID}` : ''}</div>
        <button class="btn danger" on:click={() => removeEpisode(ep.ID)}>Delete</button>
      </div>
      <div style="margin-top:6px">{ep.Summary}</div>
      <div style="margin-top:6px">
        {#each ep.Topics || [] as t}<span class="pill">{t}</span>{/each}
      </div>
    </div>
  {/each}
{/if}
