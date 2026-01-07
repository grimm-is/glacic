<script lang="ts">
  /**
   * Input Component
   * Text input with label and error support
   */

  interface Props {
    id?: string;
    type?: "text" | "password" | "email" | "number" | "url";
    value?: string | number;
    placeholder?: string;
    label?: string;
    error?: string;
    disabled?: boolean;
    required?: boolean;
    class?: string;
  }

  let {
    id = "",
    type = "text",
    value = $bindable(""),
    placeholder = "",
    label = "",
    error = "",
    disabled = false,
    required = false,
    class: className = "",
  }: Props = $props();
</script>

<div class="input-wrapper {className}">
  {#if label}
    <label for={id} class="input-label">
      {label}
      {#if required}<span class="required">*</span>{/if}
    </label>
  {/if}

  <input
    {id}
    {type}
    bind:value
    {placeholder}
    {disabled}
    {required}
    class="input"
    class:error={!!error}
  />

  {#if error}
    <p class="input-error">{error}</p>
  {/if}
</div>

<style>
  .input-wrapper {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .input-label {
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--color-foreground);
  }

  .required {
    color: var(--color-destructive);
    margin-left: var(--space-1);
  }

  .input {
    width: 100%;
    padding: var(--space-2) var(--space-3);
    font-size: var(--text-sm);
    font-family: inherit;
    color: var(--color-foreground);
    background-color: var(--color-background);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    transition:
      border-color var(--transition-fast),
      box-shadow var(--transition-fast);
  }

  .input::placeholder {
    color: var(--color-muted);
  }

  .input:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15);
  }

  .input:disabled {
    opacity: 0.5;
    cursor: not-allowed;
    background-color: var(--color-backgroundSecondary);
  }

  .input.error {
    border-color: var(--color-destructive);
  }

  .input.error:focus {
    box-shadow: 0 0 0 3px rgba(239, 68, 68, 0.15);
  }

  .input-error {
    font-size: var(--text-xs);
    color: var(--color-destructive);
    margin: 0;
  }
</style>
