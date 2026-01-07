<script lang="ts">
  /**
   * Badge Component
   * Status indicator with color variants
   */

  type BadgeVariant =
    | "default"
    | "secondary"
    | "destructive"
    | "success"
    | "warning"
    | "outline";

  interface Props {
    variant?: BadgeVariant;
    class?: string;
  }

  let {
    variant = "default",
    class: className = "",
    children,
  }: Props & { children?: any } = $props();
</script>

<span class="badge badge-{variant} {className}">
  {@render children?.()}
</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    border-radius: var(--radius-full);
    padding: var(--space-1) var(--space-2);
    font-size: var(--text-xs);
    font-weight: 500;
    line-height: 1;
  }

  .badge-default {
    background-color: var(--color-primary);
    color: var(--color-primaryForeground);
  }

  .badge-secondary {
    background-color: var(--color-backgroundSecondary);
    color: var(--color-foreground);
  }

  .badge-destructive {
    background-color: var(--color-destructive);
    color: var(--color-destructiveForeground);
  }

  .badge-success {
    background-color: var(--color-success);
    color: var(--color-successForeground);
  }

  .badge-warning {
    background-color: var(--color-warning);
    color: var(--color-warningForeground);
  }

  .badge-outline {
    background-color: transparent;
    border: 1px solid var(--color-border);
    color: var(--color-foreground);
  }
</style>
