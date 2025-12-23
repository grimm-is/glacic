<script lang="ts">
  /**
   * Modal Component
   * Simple overlay dialog using {#if} for reliable rendering
   */

  interface Props {
    open?: boolean;
    title?: string;
    size?: "sm" | "md" | "lg" | "xl";
    onclose?: () => void;
  }

  let {
    open = $bindable(false),
    title = "",
    size = "md",
    onclose,
    children,
  }: Props & { children?: any } = $props();

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      open = false;
      onclose?.();
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      open = false;
      onclose?.();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <div
    class="modal-backdrop"
    onclick={handleBackdropClick}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    onkeydown={(e) => {
      if (e.key === "Escape") {
        open = false;
        onclose?.();
      }
    }}
  >
    <div class="modal-content modal-{size}">
      {#if title}
        <div class="modal-header">
          <h2 class="modal-title">{title}</h2>
          <button
            class="modal-close"
            onclick={() => {
              open = false;
              onclose?.();
            }}
            aria-label="Close"
          >
            ✕
          </button>
        </div>
      {/if}

      <div class="modal-body">
        {@render children?.()}
      </div>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: var(--z-overlay);
    background-color: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-4);
  }

  .modal-content {
    background-color: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-xl);
    box-shadow: var(--shadow-lg);
    box-shadow: var(--shadow-lg);
    width: 100%;
    max-height: 90vh;
    overflow: auto;
  }

  .modal-sm {
    max-width: 400px;
  }
  .modal-md {
    max-width: 500px;
  }
  .modal-lg {
    max-width: 800px;
  }
  .modal-xl {
    max-width: 1000px;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-6);
    border-bottom: 1px solid var(--color-border);
  }

  .modal-title {
    font-size: var(--text-lg);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .modal-close {
    background: none;
    border: none;
    font-size: var(--text-lg);
    color: var(--color-muted);
    cursor: pointer;
    padding: var(--space-1);
    border-radius: var(--radius-sm);
    transition:
      color var(--transition-fast),
      background-color var(--transition-fast);
  }

  .modal-close:hover {
    color: var(--color-foreground);
    background-color: var(--color-surfaceHover);
  }

  .modal-body {
    padding: var(--space-6);
  }
</style>
