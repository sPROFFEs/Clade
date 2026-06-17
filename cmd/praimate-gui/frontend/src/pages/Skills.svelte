<script>
  // Skills page — full catalogue browser plus user-installed skills
  // CRUD. Three sub-views:
  //   - Catalogue: built-ins + user-added, grouped by CLI
  //   - Your skills: user-added only, with edit + delete
  //   - Add: paste markdown, import from URL, or pick a local ZIP
  // Each catalogue entry has a Default-on-new-chats star.
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'

  const SUPPORTED_CLIS = [
    { id: 'claude',         label: 'Claude' },
    { id: 'openclaude',     label: 'OpenClaude' },
    { id: 'codex',          label: 'Codex' },
    { id: 'opencode',       label: 'OpenCode' },
    { id: 'praimate-code',  label: 'PrAImate Code' },
  ]

  let view = 'catalogue' // 'catalogue' | 'yours' | 'add'
  let catalogue = []
  let userSkills = []
  let defaults = new Set()
  let activeCLI = 'claude'
  let search = ''
  let loading = true
  let saving = false
  let error = ''
  let notice = ''
  let preview = null

  // Add-skill form
  let addSource = 'paste' // 'paste' | 'url' | 'zip'
  let addName = ''
  let addDesc = ''
  let addCLIs = new Set(['claude'])
  let addBody = ''
  let addURL = ''
  let addZipPath = ''
  let importing = false

  async function load() {
    loading = true
    try {
      const [cat, user, def] = await Promise.all([
        api.skillsList().catch(() => []),
        api.skillsUserList().catch(() => []),
        api.skillsDefaults().catch(() => []),
      ])
      catalogue = cat || []
      userSkills = user || []
      defaults = new Set(def || [])
    } catch (e) {
      error = String(e)
    } finally {
      loading = false
    }
  }
  onMount(load)

  $: filtered = catalogue
    .filter((s) => {
      if (activeCLI === 'all') return true
      if (activeCLI === 'universal') return !s.clis || s.clis.length === 0
      return (s.clis || []).includes(activeCLI) || (!s.clis || s.clis.length === 0)
    })
    .filter((s) => {
      if (!search.trim()) return true
      const q = search.toLowerCase()
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
    } catch (e) { error = String(e) } finally { saving = false }
  }

  async function removeUserSkill(s) {
    if (!confirm(`Delete "${s.name}"? Removes it from the catalogue. Chats that had it enabled keep working but the skill body stops getting injected.`)) return
    try {
      await api.deleteUserSkill(s.id)
      notice = `Deleted ${s.name}`
      await load()
    } catch (e) { error = String(e) }
  }

  async function importFromURL() {
    importing = true
    error = ''
    try {
      const seed = await api.importSkillFromURL(addURL.trim())
      if (!addName.trim()) addName = seed.name || ''
      if (!addDesc.trim()) addDesc = seed.description || ''
      addBody = seed.body || ''
      notice = 'Fetched. Fill in name + CLIs and click Save.'
    } catch (e) { error = String(e) }
    finally { importing = false }
  }

  async function pickZip() {
    try { addZipPath = (await api.pickSkillZipFile()) || addZipPath } catch (e) { error = String(e) }
  }
  async function importFromZip() {
    if (!addZipPath) return
    importing = true
    error = ''
    try {
      const seed = await api.importSkillFromZipFile(addZipPath)
      if (!addName.trim()) addName = seed.name || ''
      if (!addDesc.trim()) addDesc = seed.description || ''
      addBody = seed.body || ''
      notice = 'Extracted. Fill in name + CLIs and click Save.'
    } catch (e) { error = String(e) }
    finally { importing = false }
  }

  async function saveSkill() {
    if (saving) return
    saving = true
    error = ''
    try {
      await api.addUserSkill({
        id: '', // backend derives from name
        name: addName.trim(),
        description: addDesc.trim() || `User-installed skill: ${addName.trim()}`,
        clis: Array.from(addCLIs),
        body: addBody,
        source: addURL.trim() || (addZipPath ? `file://${addZipPath}` : 'user'),
      })
      notice = `Saved "${addName.trim()}"`
      addName = ''; addDesc = ''; addBody = ''; addURL = ''; addZipPath = ''
      addCLIs = new Set(['claude'])
      view = 'catalogue'
      await load()
    } catch (e) { error = String(e) }
    finally { saving = false }
  }
</script>

<h1>Skills</h1>
<p class="subtitle">
  Plug-in system-prompt fragments PrAImate prepends to a chat's
  conversation. Mark a skill as <strong>★ Default</strong> to
  auto-enable it on every new chat; per-chat toggles live in the
  chat-settings panel on the Chats page.
</p>

<div class="card warn-card">
  <div class="card-title">Skills are CLI-specific</div>
  <div class="card-sub">
    Each skill is written for a particular CLI's tool model. Enabling
    a Claude skill on a Codex chat won't make Codex behave like Claude;
    PrAImate just prepends the markdown to the system prompt. We
    deliberately <strong>don't attach skills to agents</strong> so the
    same agent stays portable across every wrapped CLI. The chat
    settings panel flags ⚠ on skills enabled for the wrong CLI.
  </div>
</div>

<div class="row" style="margin-top:14px; gap:6px">
  <button class="btn" class:primary={view === 'catalogue'} on:click={() => (view = 'catalogue')}>Catalogue</button>
  <button class="btn" class:primary={view === 'yours'} on:click={() => (view = 'yours')}>Your skills · {userSkills.length}</button>
  <button class="btn" class:primary={view === 'add'} on:click={() => (view = 'add')}>+ Add</button>
</div>

{#if error}<div class="banner">{error}</div>{/if}
{#if notice}<div class="note">{notice}</div>{/if}

{#if view === 'catalogue'}
  <div class="row" style="margin-top:14px; gap:6px; flex-wrap:wrap">
    <button class="btn sm" class:primary={activeCLI === 'all'} on:click={() => (activeCLI = 'all')}>All</button>
    {#each SUPPORTED_CLIS as cli}
      <button class="btn sm" class:primary={activeCLI === cli.id} on:click={() => (activeCLI = cli.id)}>{cli.label}</button>
    {/each}
    <button class="btn sm" class:primary={activeCLI === 'universal'} on:click={() => (activeCLI = 'universal')}>Universal</button>
    <span class="grow"></span>
    <input class="field" style="max-width:260px" placeholder="search…" bind:value={search} />
  </div>

  {#if loading}
    <div class="empty">Loading catalogue…</div>
  {:else if filtered.length === 0}
    <div class="empty">Nothing matches. Try another tab or add your own.</div>
  {/if}

  {#each filtered as s (s.id)}
    <div class="card row">
      <div class="grow">
        <div class="card-title">
          {s.name}
          {#if s.source && s.source !== 'builtin'}<span class="pill user-tag">{s.source.startsWith('http') ? 'imported' : 'user'}</span>{/if}
        </div>
        <div class="card-sub">{s.description}</div>
        <div class="card-sub" style="margin-top:4px">
          {#each (s.clis || []) as c}<span class="pill">{c}</span>{/each}
          {#if !s.clis || s.clis.length === 0}<span class="pill">universal</span>{/if}
        </div>
      </div>
      <button class="btn sm" on:click={() => (preview = preview === s ? null : s)}>
        {preview === s ? 'Hide' : 'Preview'}
      </button>
      <button class="btn" class:primary={defaults.has(s.id)} on:click={() => toggleDefault(s)} title="Auto-enable on every new chat">
        {defaults.has(s.id) ? '★ Default' : '☆ Default'}
      </button>
    </div>
    {#if preview === s}
      <pre class="skill-body mono">{s.body}</pre>
    {/if}
  {/each}

{:else if view === 'yours'}
  {#if userSkills.length === 0}
    <div class="empty" style="margin-top:14px">
      You haven't added any skills yet. Use <strong>+ Add</strong> to paste markdown, import from a URL, or pick a local ZIP.
    </div>
  {/if}
  {#each userSkills as s (s.id)}
    <div class="card row">
      <div class="grow">
        <div class="card-title">{s.name}</div>
        <div class="card-sub">{s.description}</div>
        <div class="card-sub" style="margin-top:4px">
          {#each (s.clis || []) as c}<span class="pill">{c}</span>{/each}
          {#if !s.clis || s.clis.length === 0}<span class="pill">universal</span>{/if}
          {#if s.source}<span class="pill mono">{s.source}</span>{/if}
        </div>
      </div>
      <button class="btn sm" on:click={() => (preview = preview === s ? null : s)}>
        {preview === s ? 'Hide' : 'Preview'}
      </button>
      <button class="btn danger" on:click={() => removeUserSkill(s)}>Delete</button>
    </div>
    {#if preview === s}
      <pre class="skill-body mono">{s.body}</pre>
    {/if}
  {/each}

{:else if view === 'add'}
  <div class="card" style="margin-top:14px">
    <div class="card-title">Add a skill</div>
    <div class="card-sub">
      Paste markdown directly, or import from a URL / local ZIP. After
      importing, fill in the name + supported CLIs and click Save.
    </div>

    <div class="row" style="margin-top:10px">
      <button class="btn sm" class:primary={addSource === 'paste'} on:click={() => (addSource = 'paste')}>Paste</button>
      <button class="btn sm" class:primary={addSource === 'url'}   on:click={() => (addSource = 'url')}>URL</button>
      <button class="btn sm" class:primary={addSource === 'zip'}   on:click={() => (addSource = 'zip')}>Local ZIP</button>
    </div>

    {#if addSource === 'url'}
      <label class="lbl">URL — paste a GitHub repo, a file URL, or a .zip</label>
      <div class="row">
        <input class="field grow mono" placeholder="https://github.com/user/repo/tree/main/skills/debugger" bind:value={addURL} />
        <button class="btn" on:click={importFromURL} disabled={importing || !addURL.trim()}>{importing ? 'Fetching…' : 'Fetch'}</button>
      </div>
      <div class="card-sub" style="margin-top:6px">
        <strong>GitHub URLs are first-class</strong> — no `git` binary needed, we hit the archive endpoint:
        <ul style="margin:4px 0 0 18px; padding:0; font-size:11px">
          <li><span class="mono">github.com/user/repo</span> — whole repo (tries main, falls back to master)</li>
          <li><span class="mono">github.com/user/repo/tree/dev</span> — a specific branch</li>
          <li><span class="mono">github.com/user/repo/tree/main/skills/foo</span> — a subfolder only</li>
          <li><span class="mono">github.com/user/repo/blob/main/skill.md</span> — a single file (rewritten to raw)</li>
          <li><span class="mono">gist.github.com/user/id</span> — gist's first file</li>
        </ul>
        Also accepts a direct <span class="mono">.md</span> / <span class="mono">.markdown</span> / <span class="mono">.txt</span> URL, or a <span class="mono">.zip</span> of markdown files (concatenated, sorted by path).
      </div>
    {:else if addSource === 'zip'}
      <label class="lbl">Local ZIP path</label>
      <div class="row">
        <input class="field grow mono" placeholder="/path/to/skill.zip" bind:value={addZipPath} />
        <button class="btn" on:click={pickZip}>Browse…</button>
        <button class="btn" on:click={importFromZip} disabled={importing || !addZipPath}>{importing ? 'Reading…' : 'Read'}</button>
      </div>
    {/if}

    <label class="lbl">Name</label>
    <input class="field" placeholder="e.g. Threat-modelling helper" bind:value={addName} />

    <label class="lbl">Description (one line)</label>
    <input class="field" placeholder="Walks STRIDE through a given component." bind:value={addDesc} />

    <label class="lbl">Supported CLIs</label>
    <div class="row" style="gap:6px; flex-wrap:wrap">
      {#each SUPPORTED_CLIS as cli}
        <label class="cli-tag">
          <input
            type="checkbox"
            checked={addCLIs.has(cli.id)}
            on:change={(e) => {
              const next = new Set(addCLIs)
              if (e.currentTarget.checked) next.add(cli.id)
              else next.delete(cli.id)
              addCLIs = next
            }} />
          {cli.label}
        </label>
      {/each}
    </div>
    <div class="card-sub" style="margin-top:4px">
      Leave all unchecked to mark the skill <strong>universal</strong> —
      it appears on every CLI tab.
    </div>

    <label class="lbl">Skill body (markdown — prepended to the system prompt)</label>
    <textarea class="field mono" rows="14" placeholder="You are in DEBUGGER mode. Reproduce, isolate, hypothesize, verify…" bind:value={addBody}></textarea>

    <div class="row" style="margin-top:10px">
      <button class="btn primary" on:click={saveSkill} disabled={saving || !addName.trim() || !addBody.trim()}>
        {saving ? 'Saving…' : 'Save'}
      </button>
      <button class="btn" on:click={() => (view = 'catalogue')}>Cancel</button>
    </div>
  </div>
{/if}

<style>
  .warn-card { background: color-mix(in oklch, var(--warn,#d4a72c) 12%, transparent); border-color: var(--warn,#d4a72c); }
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
    max-height: 360px; overflow: auto;
  }
  .pill { font-size: 10px; padding: 1px 6px; margin-right: 4px; background: var(--bg-panel); color: var(--text-dim); border-radius: 5px; }
  .pill.user-tag { background: color-mix(in oklch, var(--accent, #5482ff) 22%, transparent); color: var(--accent, #5482ff); }
  .row { gap: 8px; align-items: center; }
  .grow { flex: 1; min-width: 0; }
  .empty { color: var(--text-dim); padding: 14px 0; font-size: 13px; }
  .note { background: color-mix(in oklch, var(--ok, #4ec9b0) 16%, transparent); border-radius: var(--radius); padding: 6px 10px; margin: 8px 0; font-size: 12px; }
  .cli-tag { display: inline-flex; gap: 4px; align-items: center; padding: 4px 8px; border: 1px solid var(--border); border-radius: 20px; font-size: 12px; cursor: pointer; }
</style>
