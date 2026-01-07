<script lang="ts">
  import Modal from "./Modal.svelte";
  import Button from "./Button.svelte";
  import Icon from "./Icon.svelte";
  import { alertStore } from "$lib/stores/app";
  import { t } from "svelte-i18n";

  const alert = $derived($alertStore);
</script>

<Modal
  open={!!alert}
  title={alert?.title || $t("alert.default_title")}
  size="sm"
  onclose={() => alertStore.dismiss()}
>
  <div class="alert-content">
    {#if alert?.type}
      <div class="alert-icon alert-icon--{alert.type}">
        <Icon name={alert.type} size="lg" filled={true} />
      </div>
    {/if}

    <p class="alert-message">{alert?.message || ""}</p>
  </div>

  <div class="alert-actions">
    <Button
      variant={alert?.type === "error" ? "destructive" : "default"}
      onclick={() => alertStore.dismiss()}
      class="w-full"
    >
      {alert?.confirmText || $t("alert.ok")}
    </Button>
  </div>
</Modal>

<style>
  .alert-content {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: var(--space-4);
    margin-bottom: var(--space-6);
  }

  .alert-icon {
    display: inline-flex;
    padding: var(--space-3);
    border-radius: 50%;
    background-color: var(--color-surface);
  }

  .alert-icon--error {
    color: var(--color-destructive);
    background-color: var(--color-destructive-bg, rgba(239, 68, 68, 0.1));
  }

  .alert-icon--success {
    color: var(--color-success);
    background-color: var(--color-success-bg, rgba(34, 197, 94, 0.1));
  }

  .alert-icon--warning {
    color: var(--color-warning);
    background-color: var(--color-warning-bg, rgba(245, 158, 11, 0.1));
  }

  .alert-message {
    font-size: var(--text-base);
    color: var(--color-foreground);
    margin: 0;
  }

  .alert-actions {
    display: flex;
    justify-content: center;
  }
</style>
