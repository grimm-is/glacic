<script lang="ts">
  /**
   * Spinner Component
   * Loading indicator
   */
  import { t } from "svelte-i18n";

  type SpinnerSize = "sm" | "md" | "lg";

  interface Props {
    size?: SpinnerSize;
    class?: string;
  }

  let { size = "md", class: className = "" }: Props = $props();
</script>

<div
  class="spinner spinner-{size} {className}"
  role="status"
  aria-label={$t("common.loading")}
>
  <span class="sr-only">{$t("common.loading")}</span>
</div>

<style>
  .spinner {
    border-radius: 50%;
    border-style: solid;
    border-color: var(--color-primary);
    border-top-color: transparent;
    animation: spin 0.8s linear infinite;
  }

  .spinner-sm {
    width: 1rem;
    height: 1rem;
    border-width: 2px;
  }

  .spinner-md {
    width: 1.5rem;
    height: 1.5rem;
    border-width: 2px;
  }

  .spinner-lg {
    width: 2rem;
    height: 2rem;
    border-width: 3px;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
