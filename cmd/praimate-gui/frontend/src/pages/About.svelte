<script>
  import { onMount } from 'svelte'
  import { api } from '../lib/api.js'
  import logo from '../assets/monke-icon.png'

  let info = null
  let error = ''

  onMount(async () => {
    try {
      info = await api.about()
    } catch (e) {
      error = String(e)
    }
  })
</script>

<h1>About</h1>
<p class="subtitle">Version, platform, privacy, and local-data boundaries.</p>

{#if error}<div class="banner">{error}</div>{/if}

<div class="card hero">
  <img src={logo} alt="PrAImate" />
  <div>
    <div class="product">{info?.name || 'PrAImate'}</div>
    <div class="card-sub">
      {#if info}Version {info.version} · {info.os}/{info.arch}{:else}Loading runtime information…{/if}
    </div>
    <div class="tagline">One harness, every agent — workflows & MCP.</div>
  </div>
</div>

<h2>Privacy</h2>
<div class="card stack">
  <div>
    <strong>No PrAImate telemetry</strong>
    <p>PrAImate does not send product analytics or generate application, query, or terminal log files. Network traffic comes from update checks, installs, configured backups, MCP servers, and the agent/model providers you choose.</p>
  </div>
  <div>
    <strong>Provider boundary</strong>
    <p>Prompts, selected files, and tool output are handled by the chosen CLI and provider. Privacy redaction is a safety net, not a guarantee.</p>
  </div>
  <div>
    <strong>Agent permissions</strong>
    <p>Tool-enabled agents can read or modify the selected working folder. The selected tool level remains the operative security boundary.</p>
  </div>
  <div>
    <strong>Memory-only terminal scrollback</strong>
    <p>Live Code-terminal output is retained only in memory while the process runs. Closed-terminal output cannot be recovered because no history log is written.</p>
  </div>
</div>

<h2>Database encryption</h2>
<div class="card stack">
  <div class="row">
    <span class="pill {info?.databaseEncrypted ? 'ok' : 'err'}">
      {info?.databaseEncrypted ? 'Encrypted' : 'Unavailable'}
    </span>
    <strong>{info?.databaseCipher || 'AES-256-XTS'}</strong>
  </div>
  <p>
    The local SQLite database is transparently encrypted at rest. Its random
    512-bit XTS key is stored separately with user-only filesystem permissions.
    Encryption protects the database when it is copied without the key; it does
    not protect against an attacker controlling the same OS account.
  </p>
  <p>
    AES-XTS provides confidentiality but not tamper authentication. A modified
    database may fail to open or may contain corrupted data.
  </p>
  {#if info?.dbPath}
    <div class="path"><span>Database</span><code>{info.dbPath}</code></div>
    <div class="path"><span>Key file</span><code>{info.dbKeyPath}</code></div>
  {/if}
</div>

<h2>Backups and workspace files</h2>
<div class="card stack">
  <p>
    Git backup is disabled by default. When enabled, it stores workspace chats,
    templates, per-chat MEMORY.md files, agent exports, and a portable plaintext
    SQLite snapshot. Treat access to the backup repository as access to your
    PrAImate data and use a private remote.
  </p>
  <p>Supported desktop systems: Linux and Windows. macOS is unsupported.</p>
</div>

<style>
  h2 { margin: 24px 0 8px; font-size: 16px; }
  .hero { display: flex; align-items: center; gap: 16px; }
  .hero img { width: 58px; height: 58px; border-radius: 13px; }
  .product { font-size: 22px; font-weight: 750; }
  .tagline { margin-top: 7px; color: var(--text-dim); }
  .stack { display: flex; flex-direction: column; gap: 12px; }
  .stack p { margin: 4px 0 0; color: var(--text-dim); line-height: 1.55; }
  .path { display: grid; grid-template-columns: 90px minmax(0, 1fr); gap: 8px; }
  .path span { color: var(--text-dim); }
  .path code { overflow-wrap: anywhere; }
</style>
