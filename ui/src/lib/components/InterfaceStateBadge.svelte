<script lang="ts">
  import Icon from "./Icon.svelte";
  import { t } from "svelte-i18n";

  interface Props {
    state: string;
    showLabel?: boolean;
    size?: "sm" | "md" | "lg";
  }

  let { state, showLabel = true, size = "md" }: Props = $props();

  // State configuration with a11y labels and colors
  // Note: iconName is mapped in Icon.svelte, but we can override or pass through here
  const stateConfig: Record<
    string,
    { labelKey: string; color: string; iconName: string; filled: boolean }
  > = {
    up: {
      labelKey: "interface.state.up",
      color: "success",
      iconName: "state-up",
      filled: true,
    },
    down: {
      labelKey: "interface.state.down",
      color: "neutral",
      iconName: "state-down",
      filled: false,
    },
    no_carrier: {
      labelKey: "interface.state.no_carrier",
      color: "warning",
      iconName: "state-no_carrier",
      filled: false,
    },
    missing: {
      labelKey: "interface.state.missing",
      color: "error",
      iconName: "state-missing",
      filled: true,
    },
    disabled: {
      labelKey: "interface.state.disabled",
      color: "neutral",
      iconName: "state-disabled",
      filled: false,
    },
    degraded: {
      labelKey: "interface.state.degraded",
      color: "warning",
      iconName: "state-degraded",
      filled: true,
    },
    error: {
      labelKey: "interface.state.error",
      color: "error",
      iconName: "state-error",
      filled: true,
    },
  };

  const config = $derived(
    stateConfig[state] || {
      labelKey: state,
      color: "neutral",
      iconName: "state-down",
      filled: false,
    },
  );

  const label = $derived($t(config.labelKey));
</script>

<span
  class="state-badge state-badge--{config.color} state-badge--{size}"
  role="status"
  aria-label={$t("interface.state_aria", { values: { state: label } })}
>
  <Icon name={config.iconName} {size} filled={config.filled} />
  {#if showLabel}
    <span class="state-badge__label">{label}</span>
  {/if}
</span>

<style>
  .state-badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
    font-size: var(--text-xs);
    font-weight: 500;
    line-height: 1;
  }

  /* Size variants */
  .state-badge--sm {
    padding: 2px var(--space-1);
    font-size: 0.7rem;
  }

  .state-badge--lg {
    padding: var(--space-2) var(--space-3);
    font-size: var(--text-sm);
  }

  /* Color variants */
  .state-badge--success {
    background-color: var(--color-success-bg, rgba(34, 197, 94, 0.15));
    color: var(--color-success, #16a34a);
    border: 1px solid var(--color-success-border, rgba(34, 197, 94, 0.3));
  }

  .state-badge--neutral {
    background-color: var(--color-muted-bg, rgba(107, 114, 128, 0.15));
    color: var(--color-muted, #6b7280);
    border: 1px solid var(--color-muted-border, rgba(107, 114, 128, 0.3));
  }

  .state-badge--warning {
    background-color: var(--color-warning-bg, rgba(245, 158, 11, 0.15));
    color: var(--color-warning, #d97706);
    border: 1px solid var(--color-warning-border, rgba(245, 158, 11, 0.3));
  }

  .state-badge--error {
    background-color: var(--color-destructive-bg, rgba(239, 68, 68, 0.15));
    color: var(--color-destructive, #dc2626);
    border: 1px solid var(--color-destructive-border, rgba(239, 68, 68, 0.3));
  }

  .state-badge__label {
    white-space: nowrap;
  }
</style>
