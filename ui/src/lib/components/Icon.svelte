<script lang="ts">
  interface Props {
    name: string;
    size?: "sm" | "md" | "lg";
    filled?: boolean;
    class?: string;
  }

  let {
    name,
    size = "md",
    filled = false,
    class: className = "",
  }: Props = $props();

  const sizeMap = {
    sm: "16px",
    md: "20px",
    lg: "24px",
  };

  const style = $derived(`
    font-size: ${sizeMap[size]};
    font-variation-settings: 'FILL' ${filled ? 1 : 0}, 'wght' 400, 'GRAD' 0, 'opsz' 24;
  `);

  // Map our internal names to Google Material Symbols names
  const iconMap: Record<string, string> = {
    "state-up": "check_circle",
    "state-down": "pause_circle",
    "state-no_carrier": "link_off",
    "state-missing": "cancel",
    "state-disabled": "do_not_disturb_on",
    "state-degraded": "warning",
    "state-error": "error",

    check: "check_circle",
    warning: "warning",
    error: "error",

    // Common actions
    plus: "add",
    minus: "remove",
    delete: "delete",
    edit: "edit",
    "arrow-left-right": "swap_horiz",
    "arrow-right": "arrow_forward",
    "arrow-left": "arrow_back",
  };

  const iconName = $derived(iconMap[name] || name);
</script>

<span class="material-symbols-rounded {className}" {style} aria-hidden="true">
  {iconName}
</span>

<style>
  .material-symbols-rounded {
    display: inline-block;
    vertical-align: middle;
    line-height: 1;
    user-select: none;
    /* Default to non-filled, weight 400 */
    font-variation-settings:
      "FILL" 0,
      "wght" 400,
      "GRAD" 0,
      "opsz" 24;
  }
</style>
