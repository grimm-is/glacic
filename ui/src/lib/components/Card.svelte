<script lang="ts">
  /**
   * Card Component
   * Container with border and optional shadow
   */

  interface Props {
    class?: string;
  }

  let { class: className = "", children }: Props & { children?: any } =
    $props();
</script>

<div class="card {className}">
  {@render children?.()}
</div>

<style>
  .card {
    background-color: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
    padding: var(--space-4);
  }
</style>
