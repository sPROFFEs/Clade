<script>
  import { dismissToast, toast } from './stores.js'
</script>

{#if $toast}
  <div class="toast" class:ok={$toast.tone === 'ok'} class:err={$toast.tone === 'err'} class:busy={$toast.tone === 'busy'} role="status" aria-live="polite">
    <div class="toast-mark" aria-hidden="true">
      {$toast.tone === 'ok' ? '✓' : $toast.tone === 'err' ? '!' : '◌'}
    </div>
    <div class="grow">
      <div class="toast-title">{$toast.title}</div>
      {#if $toast.message}<div class="toast-message">{$toast.message}</div>{/if}
    </div>
    {#if $toast.dismissible !== false}
      <button class="toast-close" on:click={dismissToast} aria-label="Dismiss notification">×</button>
    {/if}
  </div>
{/if}
