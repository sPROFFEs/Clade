<script>
  import { createEventDispatcher } from 'svelte'
  import { api } from './api.js'
  import {
    privacyCompatibility,
    privacyDisclosures,
    privacyIntroduction,
  } from './privacyContent.js'

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
    <header>
      <div class="eyebrow">Before you start</div>
      <h1 id="privacy-title">Privacy and local-data notice</h1>
      <p class="introduction">{privacyIntroduction}</p>
    </header>

    <div class="notice-list">
      {#each privacyDisclosures as disclosure, index}
        <article class="notice">
          <span class="notice-number" aria-hidden="true">{String(index + 1).padStart(2, '0')}</span>
          <div>
            <h2>{disclosure.title}</h2>
            <p>{disclosure.body}</p>
          </div>
        </article>
      {/each}
    </div>

    <footer>
      <p class="fine">{privacyCompatibility}</p>
      {#if error}<div class="banner">{error}</div>{/if}
      <div class="actions">
        <button class="btn primary" disabled={busy} on:click={accept}>
          {busy ? 'Saving…' : 'I understand — continue'}
        </button>
      </div>
    </footer>
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
    width: min(680px, 94vw);
    max-height: 92vh;
    overflow: auto;
    border: 1px solid var(--border-bright);
    border-radius: 14px;
    background: var(--bg-panel);
    box-shadow: 0 24px 80px rgba(0, 0, 0, .38);
  }
  header { padding: 28px 30px 22px; }
  .privacy-modal h1 {
    margin: 5px 0 10px;
    font-size: 25px;
    letter-spacing: -.02em;
  }
  .introduction {
    max-width: 600px;
    margin: 0;
    color: var(--text-dim);
    font-size: 14px;
    line-height: 1.6;
  }
  .eyebrow {
    color: var(--text-dim);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: .1em;
    text-transform: uppercase;
  }
  .notice-list {
    padding: 0 30px;
    border-top: 1px solid var(--border);
  }
  .notice {
    display: grid;
    grid-template-columns: 34px minmax(0, 1fr);
    gap: 12px;
    padding: 17px 0;
    border-bottom: 1px solid var(--border);
  }
  .notice-number {
    padding-top: 2px;
    color: var(--text-dim);
    font: 11px/1.5 var(--mono);
  }
  .notice h2 {
    margin: 0 0 4px;
    font-size: 14px;
    font-weight: 650;
  }
  .notice p {
    margin: 0;
    color: var(--text-dim);
    font-size: 13px;
    line-height: 1.55;
  }
  footer { padding: 18px 30px 24px; }
  .fine {
    margin: 0;
    color: var(--text-dim);
    font-size: 12px;
    line-height: 1.5;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 16px;
  }
  footer .banner { margin-top: 14px; }
  @media (max-width: 560px) {
    .privacy-backdrop { padding: 12px; }
    header, footer { padding-left: 20px; padding-right: 20px; }
    .notice-list { padding: 0 20px; }
    .notice { grid-template-columns: 28px minmax(0, 1fr); gap: 8px; }
  }
</style>
