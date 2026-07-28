<script>
  import { createEventDispatcher } from 'svelte'
  import { api } from './api.js'

  const dispatch = createEventDispatcher()
  let busy = false
  let error = ''

  async function accept() {
    if (busy) return
    busy = true
    error = ''
    try {
      await api.acceptPrivacyNotice()
      dispatch('accepted')
    } catch (e) {
      error = String(e)
    } finally {
      busy = false
    }
  }
</script>

<div class="privacy-backdrop">
  <section class="privacy-modal" role="dialog" aria-modal="true" aria-labelledby="privacy-title">
    <div class="eyebrow">Before you start</div>
    <h1 id="privacy-title">Privacy and local-data notice</h1>
    <p>
      PrAImate has no product telemetry and does not generate application,
      query, or terminal log files. It stores chats, agents, settings, and MCP
      configuration locally.
    </p>

    <div class="notice-grid">
      <div class="notice">
        <strong>Encrypted local database</strong>
        <span>
          The SQLite database is encrypted at rest with AES-256-XTS. Its
          random key is stored separately with user-only permissions. This
          protects a copied database, but not someone controlling your OS
          account, and XTS does not authenticate against tampering. Keep the
          database and key together when backing up: losing the key makes the
          database unreadable.
        </span>
      </div>
      <div class="notice">
        <strong>AI providers receive what you send</strong>
        <span>
          Prompts, selected files, and tool output go to the CLI and model
          provider you choose. Built-in redaction catches common secrets but
          cannot guarantee that every sensitive value is removed.
        </span>
      </div>
      <div class="notice">
        <strong>Agents can change files</strong>
        <span>
          Tool-enabled sessions may read, create, edit, or execute files in
          the working folder according to the permission level you select.
          Review the folder and permissions before starting.
        </span>
      </div>
      <div class="notice">
        <strong>Terminal output is memory-only</strong>
        <span>
          Live Code-terminal scrollback is retained only while its process is
          running. It is not written to a diagnostic or history log and cannot
          be recovered after the terminal or application closes.
        </span>
      </div>
      <div class="notice">
        <strong>Backups are your responsibility</strong>
        <span>
          Git backup is off by default. If enabled, it includes workspace
          files, per-chat MEMORY.md files, and a portable plaintext database
          snapshot. Use a private remote you trust and protect its credentials.
        </span>
      </div>
    </div>

    <p class="fine">
      PrAImate supports Linux and Windows. You can review this information
      later on the About page.
    </p>
    {#if error}<div class="banner">{error}</div>{/if}
    <div class="actions">
      <button class="btn primary" disabled={busy} on:click={accept}>
        {busy ? 'Saving…' : 'I understand — continue'}
      </button>
    </div>
  </section>
</div>

<style>
  .privacy-backdrop {
    position: fixed;
    inset: 0;
    z-index: 10000;
    display: grid;
    place-items: center;
    padding: 24px;
    background: color-mix(in srgb, var(--bg) 82%, transparent);
    backdrop-filter: blur(10px);
  }
  .privacy-modal {
    width: min(760px, 94vw);
    max-height: 90vh;
    overflow: auto;
    padding: 28px;
    border: 1px solid var(--border);
    border-radius: 18px;
    background: var(--panel);
    box-shadow: 0 24px 80px rgba(0, 0, 0, .32);
  }
  .privacy-modal h1 { margin: 4px 0 8px; font-size: 25px; }
  .privacy-modal > p { color: var(--text-dim); line-height: 1.55; }
  .eyebrow {
    color: var(--accent);
    font-size: 12px;
    font-weight: 700;
    letter-spacing: .09em;
    text-transform: uppercase;
  }
  .notice-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
    margin: 20px 0;
  }
  .notice {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 14px;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--bg-raised);
  }
  .notice span { color: var(--text-dim); font-size: 13px; line-height: 1.45; }
  .fine { font-size: 12px; }
  .actions { display: flex; justify-content: flex-end; margin-top: 16px; }
  @media (max-width: 680px) {
    .notice-grid { grid-template-columns: 1fr; }
  }
</style>
