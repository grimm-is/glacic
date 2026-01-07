<script lang="ts">
  /**
   * Button Component
   * Variants: default, destructive, outline, ghost, link
   * Sizes: sm, md, lg
   */

  type ButtonVariant = "default" | "destructive" | "outline" | "ghost" | "link";
  type ButtonSize = "sm" | "md" | "lg";

  interface Props {
    variant?: ButtonVariant;
    size?: ButtonSize;
    disabled?: boolean;
    type?: "button" | "submit" | "reset";
    class?: string;
    onclick?: (e: MouseEvent) => void;
  }

  let {
    variant = "default",
    size = "md",
    disabled = false,
    type = "button",
    class: className = "",
    onclick,
    children,
  }: Props & { children?: any } = $props();
</script>

<button
  {type}
  {disabled}
  class="btn btn-{variant} btn-{size} {className}"
  {onclick}
>
  {@render children?.()}
</button>

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    border-radius: var(--radius-md);
    font-weight: 500;
    cursor: pointer;
    transition: all var(--transition-fast);
    border: none;
    font-family: inherit;
  }

  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  /* Sizes */
  .btn-sm {
    font-size: var(--text-sm);
    padding: var(--space-1) var(--space-3);
    height: 2rem;
  }

  .btn-md {
    font-size: var(--text-sm);
    padding: var(--space-2) var(--space-4);
    height: 2.5rem;
  }

  .btn-lg {
    font-size: var(--text-base);
    padding: var(--space-3) var(--space-6);
    height: 3rem;
  }

  /* Variants */
  .btn-default {
    background-color: var(--color-primary);
    color: var(--color-primaryForeground);
  }

  .btn-default:hover:not(:disabled) {
    opacity: 0.9;
  }

  .btn-destructive {
    background-color: var(--color-destructive);
    color: var(--color-destructiveForeground);
  }

  .btn-destructive:hover:not(:disabled) {
    opacity: 0.9;
  }

  .btn-outline {
    background-color: transparent;
    border: 1px solid var(--color-border);
    color: var(--color-foreground);
  }

  .btn-outline:hover:not(:disabled) {
    background-color: var(--color-surfaceHover);
  }

  .btn-ghost {
    background-color: transparent;
    color: var(--color-foreground);
  }

  .btn-ghost:hover:not(:disabled) {
    background-color: var(--color-surfaceHover);
  }

  .btn-link {
    background-color: transparent;
    color: var(--color-primary);
    text-decoration: underline;
    text-underline-offset: 4px;
  }

  .btn-link:hover:not(:disabled) {
    opacity: 0.8;
  }
</style>
