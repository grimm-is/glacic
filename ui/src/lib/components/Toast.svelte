<script lang="ts">
  /**
   * Toast Component
   * Displays notifications from WebSocket
   */

  import { onMount, onDestroy } from "svelte";
  import { wsNotifications } from "$lib/stores/websocket";

  let toasts = $state<
    Array<{
      id: number;
      type: string;
      title: string;
      message: string;
      visible: boolean;
    }>
  >([]);

  let toastId = 0;

  function showToast(notification: any) {
    const id = ++toastId;
    const toast = {
      id,
      type: notification.type || "info",
      title: notification.title || "Notification",
      message: notification.message || "",
      visible: true,
    };

    toasts = [...toasts, toast];

    // Auto-dismiss after 5 seconds
    setTimeout(() => {
      dismissToast(id);
    }, 5000);
  }

  function dismissToast(id: number) {
    toasts = toasts.map((t) => (t.id === id ? { ...t, visible: false } : t));
    // Remove from DOM after fade
    setTimeout(() => {
      toasts = toasts.filter((t) => t.id !== id);
    }, 300);
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
  });
</script>

<div class="toast-container">
  {#each toasts as toast (toast.id)}
    <div
      class="toast {getTypeClass(toast.type)}"
      class:visible={toast.visible}
      role="alert"
    >
      <span class="toast-icon">{getIcon(toast.type)}</span>
      <div class="toast-content">
        <strong class="toast-title">{toast.title}</strong>
        <p class="toast-message">{toast.message}</p>
      </div>
      <button class="toast-close" onclick={() => dismissToast(toast.id)}
        >×</button
      >
    </div>
  {/each}
</div>

<style>
  .toast-container {
    position: fixed;
    top: var(--space-4);
    right: var(--space-4);
    z-index: 9999;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
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
