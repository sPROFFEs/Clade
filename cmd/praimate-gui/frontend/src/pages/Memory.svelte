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
<p class="subtitle">
  Cross-chat memory: things PrAImate remembers across every chat,
  agent, and CLI. Backed by the same SQLite store the TUI uses, so
  what you add here applies to terminal runs too. Three layers — see
  each section below — and the global toggle at the top decides whether
  any of them gets injected into a new run's system prompt.
</p>

{#if error}<div class="banner">{error}</div>{/if}

{#if snap}
  <div class="card row">
    <div class="grow">
      <div class="card-title">Memory injection</div>
      <div class="card-sub">
        Master switch. When ON, every new chat / terminal run begins
        with a memory block prepended to its system prompt: your
        identity rows, the single most-relevant past episode for the
        current task, and the top-N highest-salience pinned facts
        (capped at ~800 tokens so it fits any context window). When
        OFF, nothing is injected — chats run cold. Toggling does not
        delete anything; the data sits dormant.
      </div>
    </div>
    <button class="btn" class:primary={snap.enabled} on:click={toggle}>
      {snap.enabled ? 'Enabled' : 'Disabled'}
    </button>
  </div>

  <h1 style="font-size:16px; margin-top:24px">Identity</h1>
  <p class="subtitle">
    Static key→value pairs about you. Always injected when memory is
    on — every run starts knowing your name, role, time zone, preferred
    editor, etc. Add what would be tedious to repeat in every chat.
    Common keys: <span class="mono">name</span>,
    <span class="mono">role</span>, <span class="mono">tz</span>,
    <span class="mono">editor</span>, <span class="mono">os</span>.
  </p>
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
  <p class="subtitle">
    Standing preferences and constraints that aren't an identity field
    but you want every run to honour. Each fact carries a salience
    (auto-tuned each time it's referenced) and a use count. When memory
    is on, the highest-salience facts get prepended until the token
    budget is hit — pin the ones you'd be annoyed to re-explain.
    Examples: "prefers spaces over tabs", "no force-pushes to main",
    "always write tests before merging".
  </p>
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
  <p class="subtitle">
    Auto-generated summaries of past chats. When a run ends, PrAImate
    asks the distillation endpoint (Settings → Distillation) to turn
    the transcript into a one-paragraph summary + a topic list. On the
    next run, the episode whose topics overlap most with the new
    prompt is prepended so the model has continuity — "you helped me
    set up X yesterday; this is the next step". Delete an episode to
    keep it out of future injections; deleting the underlying chat
    also drops its episode.
  </p>
  {#if (snap.episodes || []).length === 0}
    <div class="empty">No episodes yet — completed runs distill here when memory is on AND a distillation endpoint is configured in Settings.</div>
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
