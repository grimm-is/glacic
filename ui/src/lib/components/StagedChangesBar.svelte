<script lang="ts">
  import { hasPendingChanges, api, alertStore } from '$lib/stores/app';

  let applying = $state(false);
  let discarding = $state(false);

  async function handleApply() {
    applying = true;
    try {
      await api.applyConfig();
      alertStore.success('Configuration applied successfully');
    } catch (e: any) {
      alertStore.error(e.message || 'Failed to apply configuration');
    } finally {
      applying = false;
    }
  }

  async function handleDiscard() {
    discarding = true;
    try {
      await api.discardConfig();
      alertStore.success('Changes discarded');
    } catch (e: any) {
      alertStore.error(e.message || 'Failed to discard changes');
    } finally {
      discarding = false;
    }
  }
</script>

{#if $hasPendingChanges}
  <div class="staged-bar">
    <div class="staged-content">
      <span class="staged-icon">⚠️</span>
      <span class="staged-text">You have unsaved configuration changes</span>
      <div class="staged-actions">
        <button 
          class="btn-discard" 
          onclick={handleDiscard} 
          disabled={discarding || applying}
        >
          {discarding ? 'Discarding...' : 'Discard'}
        </button>
        <button 
          class="btn-apply" 
          onclick={handleApply}
          disabled={applying || discarding}
        >
          {applying ? 'Applying...' : 'Apply Changes'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .staged-bar {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: linear-gradient(135deg, #1e293b 0%, #334155 100%);
    border-top: 2px solid #f59e0b;
    padding: 0.75rem 1rem;
    z-index: 1000;
    box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.3);
  }

  .staged-content {
    max-width: 1200px;
    margin: 0 auto;
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-wrap: wrap;
    justify-content: center;
  }

  .staged-icon {
    font-size: 1.25rem;
  }

  .staged-text {
    color: #fbbf24;
    font-weight: 500;
    font-size: 0.95rem;
  }

  .staged-actions {
    display: flex;
    gap: 0.5rem;
  }

  .btn-discard, .btn-apply {
    padding: 0.5rem 1rem;
    border-radius: 0.375rem;
    font-weight: 500;
    font-size: 0.875rem;
    cursor: pointer;
    transition: all 0.2s;
    border: none;
  }

  .btn-discard {
    background: #475569;
    color: #e2e8f0;
  }

  .btn-discard:hover:not(:disabled) {
    background: #64748b;
  }

  .btn-apply {
    background: linear-gradient(135deg, #10b981 0%, #059669 100%);
    color: white;
  }

  .btn-apply:hover:not(:disabled) {
    background: linear-gradient(135deg, #059669 0%, #047857 100%);
  }

  .btn-discard:disabled, .btn-apply:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  :global(.dark) .staged-bar {
    background: linear-gradient(135deg, #0f172a 0%, #1e293b 100%);
  }
</style>
