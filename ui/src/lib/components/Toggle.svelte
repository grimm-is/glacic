<script lang="ts">
  /**
   * Toggle Switch Component
   * A visual toggle switch for boolean on/off settings
   */

  interface Props {
    checked?: boolean;
    label?: string;
    disabled?: boolean;
    onchange?: (checked: boolean) => void;
  }

  let {
    checked = $bindable(false),
    label = "",
    disabled = false,
    onchange,
  }: Props = $props();

  function handleChange(e: Event) {
    const target = e.target as HTMLInputElement;
    checked = target.checked;
    onchange?.(checked);
  }
</script>

<div class="toggle-container" class:disabled>
  {#if label}
    <span class="toggle-label">{label}</span>
  {/if}
  <label class="toggle-switch">
    <input type="checkbox" {checked} {disabled} onchange={handleChange} />
    <span class="toggle-slider"></span>
  </label>
</div>

<style>
  .toggle-container {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-1) 0;
  }

  .toggle-container.disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .toggle-label {
    font-size: var(--text-sm);
    color: var(--color-foreground);
  }

  .toggle-switch {
    position: relative;
    display: inline-block;
    width: 44px;
    height: 24px;
    flex-shrink: 0;
  }

  .toggle-switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }

  .toggle-slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: var(--color-muted);
    transition: 0.2s;
    border-radius: 24px;
  }

  .toggle-slider:before {
    position: absolute;
    content: "";
    height: 18px;
    width: 18px;
    left: 3px;
    bottom: 3px;
    background-color: white;
    transition: 0.2s;
    border-radius: 50%;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
  }

  .toggle-switch input:checked + .toggle-slider {
    background-color: var(--color-success, #16a34a);
  }

  .toggle-switch input:checked + .toggle-slider:before {
    transform: translateX(20px);
  }

  .toggle-container.disabled .toggle-slider {
    cursor: not-allowed;
  }
</style>
