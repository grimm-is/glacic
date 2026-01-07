<script lang="ts">
  /**
   * Toast Component
   * Shows brief notification popup and stores in history
   */

  import { onMount, onDestroy } from "svelte";
  import { addNotification } from "$lib/stores/notifications";

  let currentToast = $state<{
    id: number;
    type: string;
    title: string;
    message: string;
    visible: boolean;
  } | null>(null);

  let hideTimeout: ReturnType<typeof setTimeout> | null = null;
  let fadeTimeout: ReturnType<typeof setTimeout> | null = null;

  function showToast(notification: any) {
    const type = notification.type || "info";
    const title = notification.title || "Notification";
    const message = notification.message || "";

    // Add to notification history
    const stored = addNotification(type, title, message);

    // Clear any pending timeouts
    if (hideTimeout) clearTimeout(hideTimeout);
    if (fadeTimeout) clearTimeout(fadeTimeout);

    // Show the toast
    currentToast = {
      id: stored.id,
      type,
      title,
      message,
      visible: true,
    };

    // Determine display time based on type
    const displayTime = type === "error" || type === "warning" ? 5000 : 3000;

    // Auto-hide after delay
    hideTimeout = setTimeout(() => {
      if (currentToast) {
        currentToast = { ...currentToast, visible: false };
      }
      fadeTimeout = setTimeout(() => {
        currentToast = null;
      }, 300);
    }, displayTime);
  }

  function dismissToast() {
    if (hideTimeout) clearTimeout(hideTimeout);
    if (fadeTimeout) clearTimeout(fadeTimeout);
    if (currentToast) {
      currentToast = { ...currentToast, visible: false };
      fadeTimeout = setTimeout(() => {
        currentToast = null;
      }, 300);
    }
  }

  function getTypeClass(type: string) {
    switch (type) {
      case "success":
        return "toast-success";
      case "error":
        return "toast-error";
      case "warning":
        return "toast-warning";
      default:
        return "toast-info";
    }
  }

  function getIcon(type: string) {
    switch (type) {
      case "success":
        return "✓";
      case "error":
        return "✕";
      case "warning":
        return "⚠";
      default:
        return "ℹ";
    }
  }

  function handleNotification(event: CustomEvent) {
    showToast(event.detail);
  }

  onMount(() => {
    window.addEventListener(
      "ws-notification",
      handleNotification as EventListener,
    );
  });

  onDestroy(() => {
    if (typeof window !== "undefined") {
      window.removeEventListener(
        "ws-notification",
        handleNotification as EventListener,
      );
    }
    if (hideTimeout) clearTimeout(hideTimeout);
    if (fadeTimeout) clearTimeout(fadeTimeout);
  });
</script>

<div class="toast-container">
  {#if currentToast}
    <div
      class="toast {getTypeClass(currentToast.type)}"
      class:visible={currentToast.visible}
      role="alert"
    >
      <span class="toast-icon">{getIcon(currentToast.type)}</span>
      <div class="toast-content">
        <strong class="toast-title">{currentToast.title}</strong>
        <p class="toast-message">{currentToast.message}</p>
      </div>
      <button class="toast-close" onclick={dismissToast}>×</button>
    </div>
  {/if}
</div>

<style>
  .toast-container {
    position: fixed;
    top: var(--space-4);
    right: var(--space-4);
    z-index: 9999;
    pointer-events: none;
  }

  .toast {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    background-color: var(--color-background);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    min-width: 300px;
    max-width: 400px;
    pointer-events: auto;
    opacity: 0;
    transform: translateX(100%);
    transition:
      opacity 0.3s ease,
      transform 0.3s ease;
  }

  .toast.visible {
    opacity: 1;
    transform: translateX(0);
  }

  .toast-success {
    border-left: 4px solid var(--color-success);
  }
  .toast-error {
    border-left: 4px solid var(--color-destructive);
  }
  .toast-warning {
    border-left: 4px solid #f59e0b;
  }
  .toast-info {
    border-left: 4px solid var(--color-primary);
  }

  .toast-icon {
    font-size: 1.2rem;
    flex-shrink: 0;
  }

  .toast-success .toast-icon {
    color: var(--color-success);
  }
  .toast-error .toast-icon {
    color: var(--color-destructive);
  }
  .toast-warning .toast-icon {
    color: #f59e0b;
  }
  .toast-info .toast-icon {
    color: var(--color-primary);
  }

  .toast-content {
    flex: 1;
  }

  .toast-title {
    display: block;
    font-size: var(--text-sm);
    color: var(--color-foreground);
    margin-bottom: var(--space-1);
  }

  .toast-message {
    font-size: var(--text-sm);
    color: var(--color-muted);
    margin: 0;
  }

  .toast-close {
    background: none;
    border: none;
    font-size: 1.2rem;
    color: var(--color-muted);
    cursor: pointer;
    padding: 0;
    line-height: 1;
  }

  .toast-close:hover {
    color: var(--color-foreground);
  }
</style>
