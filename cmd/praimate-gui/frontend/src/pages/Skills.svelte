<script>
  // Skills page — browse the built-in catalogue, mark skills as
  // default-on-for-new-chats, and (when launched on a specific chat)
  // toggle them per-chat. Skills are CLI-specific by design; the page
  // groups them by CLI and warns when the user activates one designed
  // for a different CLI.
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  const SUPPORTED_CLIS = [
    { id: 'claude',         label: 'Claude' },
    { id: 'openclaude',     label: 'OpenClaude' },
    { id: 'codex',          label: 'Codex' },
    { id: 'opencode',       label: 'OpenCode' },
    { id: 'praimate-code',  label: 'PrAImate Code' },
  ]

  let catalogue = []
  let defaults = new Set()
  let query = ''
  let activeCLI = 'claude'
  let loading = true
  let saving = false
  let error = ''

  async function load() {
    loading = true
    try {
      const [cat, def] = await Promise.all([
        api.skillsList().catch(() => []),
        api.skillsDefaults().catch(() => []),
      ])
      catalogue = cat || []
      defaults = new Set(def || [])
    } catch (e) {
      error = String(e)
    } finally {
      loading = false
    }
  }
  onMount(load)

  $: filtered = catalogue
    .filter((s) => s.clis?.includes(activeCLI))
    .filter((s) => {
      if (!query.trim()) return true
      const q = query.toLowerCase()
      return s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q)
    })

  async function toggleDefault(s) {
    if (saving) return
    saving = true
    try {
      const next = new Set(defaults)
      if (next.has(s.id)) next.delete(s.id)
      else next.add(s.id)
      await api.setSkillsDefaults(Array.from(next))
      defaults = next
    } catch (e) {
      error = String(e)
    } finally {
      saving = false
    }
  }

  function copyBody(s) {
    try { navigator.clipboard.writeText(s.body); notice = `Copied "${s.name}" body to clipboard` }
    catch (e) { error = String(e) }
  }

  let notice = ''
  let preview = null // skill being shown in the preview drawer
</script>

<h1>Skills</h1>
<p class="subtitle">
  Plug-in system-prompt fragments that PrAImate prepends to a chat's
  conversation context. New chats inherit the skills you mark
  <strong>default</strong> here; existing chats get a per-chat toggle
  inside the Chats page settings panel.
</p>

<div class="card" style="background:color-mix(in oklch, var(--warn,#d4a72c) 12%, transparent); border-color:var(--warn,#d4a72c)">
  <div class="card-title">Skills are CLI-specific</div>
  <div class="card-sub">
    Each skill was written for a particular CLI's tool model. A skill
    that tells Claude how to use its file-edit tools won't make Codex
    edit files; one that drives OpenCode's plan/apply loop has no
    equivalent in Claude. PrAImate doesn't enforce compatibility — you
    can enable any skill on any chat — but expect uneven results when
    you cross the line. We <strong>don't</strong> attach skills to
    agents on purpose, so the same agent stays usable across every
    wrapped CLI.
  </div>
</div>

{#if error}<div class="banner">{error}</div>{/if}
{#if notice}<div class="note">{notice}</div>{/if}

<div class="row" style="margin-top:14px; gap:6px; flex-wrap:wrap">
  {#each SUPPORTED_CLIS as cli}
    <button class="btn sm" class:primary={activeCLI === cli.id} on:click={() => (activeCLI = cli.id)}>
      {cli.label}
    </button>
  {/each}
  <span class="grow"></span>
  <input class="field" style="max-width:240px" placeholder="search…" bind:value={query} />
</div>

{#if loading}
  <div class="empty">Loading catalogue…</div>
{:else if filtered.length === 0}
  <div class="empty">No skills target this CLI yet. Add one to <span class="mono">internal/core/skills_catalogue.go</span> and rebuild.</div>
{/if}

{#each filtered as s (s.id)}
  <div class="card row">
    <div class="grow">
      <div class="card-title">{s.name}</div>
      <div class="card-sub">{s.description}</div>
      <div class="card-sub" style="margin-top:4px">
        {#each (s.clis || []) as c}<span class="pill">{c}</span>{/each}
      </div>
    </div>
    <button class="btn sm" on:click={() => (preview = preview === s ? null : s)}>
      {preview === s ? 'Hide' : 'Preview'}
    </button>
    <button class="btn" class:primary={defaults.has(s.id)} on:click={() => toggleDefault(s)}>
      {defaults.has(s.id) ? '★ Default' : '☆ Default'}
    </button>
  </div>
  {#if preview === s}
    <pre class="skill-body mono">{s.body}</pre>
  {/if}
{/each}

<style>
  .skill-body {
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 12px;
    margin: -8px 0 12px 0;
    font-size: 11.5px;
    line-height: 1.5;
    white-space: pre-wrap;
    color: var(--text);
  }
  .pill { font-size: 10px; padding: 1px 6px; margin-right: 4px; background: var(--bg-panel); color: var(--text-dim); border-radius: 5px; }
  .row { gap: 8px; align-items: center; }
  .grow { flex: 1; min-width: 0; }
  .empty { color: var(--text-dim); padding: 14px 0; font-size: 13px; }
  .note { background: color-mix(in oklch, var(--ok, #4ec9b0) 16%, transparent); border-radius: var(--radius); padding: 6px 10px; margin: 8px 0; font-size: 12px; }
</style>
