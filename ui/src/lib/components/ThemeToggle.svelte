<script lang="ts">
  /**
   * ThemeToggle Component
   * Toggles between light, dark, and system themes with dropdown
   */

  import { theme } from "$lib/stores/app";

  let open = $state(false);

  const themes = [
    { id: "light", label: "Light", icon: "☀️" },
    { id: "dark", label: "Dark", icon: "🌙" },
    { id: "system", label: "System", icon: "💻" },
  ];

  function selectTheme(newTheme: string) {
    if ($theme !== newTheme) {
      // Set directly via store or use toggle logic
      theme.set(newTheme as "light" | "dark" | "system");
    }
    open = false;
  }

  function getIcon(t: string) {
    return themes.find((th) => th.id === t)?.icon || "💻";
  }
</script>

<div class="theme-toggle-container">
  <button
    class="theme-toggle-btn"
    onclick={() => (open = !open)}
    aria-label="Toggle theme"
  >
    {getIcon($theme)}
  </button>

  {#if open}
    <div class="theme-dropdown">
      {#each themes as t}
        <button
          class="theme-option"
          class:active={$theme === t.id}
          onclick={() => selectTheme(t.id)}
        >
          <span class="theme-icon">{t.icon}</span>
          <span class="theme-label">{t.label}</span>
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .theme-toggle-container {
    position: relative;
  }

  .theme-toggle-btn {
    background: none;
    border: 1px solid var(--color-border);
    padding: var(--space-2);
    border-radius: var(--radius-md);
    cursor: pointer;
    font-size: 1rem;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    transition: background-color var(--transition-fast);
  }

  .theme-toggle-btn:hover {
    background-color: var(--color-surfaceHover);
  }

  .theme-dropdown {
    position: absolute;
    bottom: calc(100% + var(--space-2));
    left: 0;
    background-color: var(--color-background);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
    z-index: 100;
    min-width: 120px;
  }

  .theme-option {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    background: none;
    border: none;
    cursor: pointer;
    font-size: var(--text-sm);
    color: var(--color-foreground);
    transition: background-color var(--transition-fast);
  }

  .theme-option:hover {
    background-color: var(--color-surfaceHover);
  }

  .theme-option.active {
    background-color: var(--color-primary);
    color: white;
  }

  .theme-icon {
    font-size: 1rem;
  }

  .theme-label {
    flex: 1;
    text-align: left;
  }
</style>
