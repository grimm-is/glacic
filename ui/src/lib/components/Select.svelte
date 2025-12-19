<script lang="ts">
  /**
   * Select Component
   * Native select wrapper with label and styling
   */

  interface Option {
    value: string;
    label: string;
    disabled?: boolean;
  }

  interface Props {
    id?: string;
    value?: string;
    options?: Option[];
    label?: string;
    placeholder?: string;
    disabled?: boolean;
    required?: boolean;
    class?: string;
  }

  let {
    id = "",
    value = $bindable(""),
    options = [],
    label = "",
    placeholder = "Select an option...",
    disabled = false,
    required = false,
    class: className = "",
  }: Props = $props();
</script>

<div class="select-wrapper {className}">
  {#if label}
    <label for={id} class="select-label">
      {label}
      {#if required}<span class="required">*</span>{/if}
    </label>
  {/if}

  <select {id} bind:value {disabled} {required} class="select">
    {#if placeholder}
      <option value="" disabled selected={!value}>{placeholder}</option>
    {/if}

    {#each options as option}
      <option value={option.value} disabled={option.disabled}>
        {option.label}
      </option>
    {/each}
  </select>
</div>

<style>
  .select-wrapper {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .select-label {
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--color-foreground);
  }

  .required {
    color: var(--color-destructive);
    margin-left: var(--space-1);
  }

  .select {
    width: 100%;
    padding: var(--space-2) var(--space-3);
    font-size: var(--text-sm);
    font-family: inherit;
    color: var(--color-foreground);
    background-color: var(--color-background);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: border-color var(--transition-fast);
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='16' height='16' viewBox='0 0 24 24' fill='none' stroke='currentColor' stroke-width='2'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right var(--space-2) center;
    padding-right: var(--space-8);
  }

  .select:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15);
  }

  .select:disabled {
    opacity: 0.5;
    cursor: not-allowed;
    background-color: var(--color-backgroundSecondary);
  }
</style>
