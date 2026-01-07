<script lang="ts">
  /**
   * RuleRow - Single rule display with inline expansion
   * Core component of ClearPath Policy Editor - renders "natural language" rule sentence
   */
  import { slide } from "svelte/transition";
  import AddressPill from "./AddressPill.svelte";
  import Sparkline from "../Sparkline.svelte";
  import { t } from "svelte-i18n";

  // Types matching backend DTOs
  interface ResolvedAddress {
    display_name: string;
    type: string;
    description?: string;
    count: number;
    is_truncated?: boolean;
    preview?: string[];
  }

  interface RuleStats {
    packets: number;
    bytes: number;
    sparkline_data: number[];
  }

  interface RuleWithStats {
    id?: string;
    name?: string;
    description?: string;
    action: string;
    protocol?: string;
    src_ip?: string;
    dest_ip?: string;
    dest_port?: number;
    services?: string[];
    disabled?: boolean;
    group?: string;
    stats?: RuleStats;
    resolved_src?: ResolvedAddress;
    resolved_dest?: ResolvedAddress;
    nft_syntax?: string;
    policy_from?: string;
    policy_to?: string;
  }

  export let rule: RuleWithStats;
  export let isSelected = false;
  export let onToggle: ((id: string, disabled: boolean) => void) | null = null;
  export let onDelete: ((id: string) => void) | null = null;
  export let onDuplicate: ((rule: RuleWithStats) => void) | null = null;

  let expanded = false;

  function toggleRule() {
    if (onToggle && rule.id) {
      onToggle(rule.id, !rule.disabled);
    }
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
  }

  $: portDisplay = rule.services?.length
    ? rule.services.join(", ")
    : rule.dest_port
      ? String(rule.dest_port)
      : "Any";

  $: actionColors = {
    accept: {
      bg: "bg-green-900/60",
      text: "text-green-400",
      border: "border-green-700",
    },
    drop: {
      bg: "bg-red-900/60",
      text: "text-red-400",
      border: "border-red-700",
    },
    reject: {
      bg: "bg-orange-900/60",
      text: "text-orange-400",
      border: "border-orange-700",
    },
  }[rule.action?.toLowerCase()] || {
    bg: "bg-gray-800",
    text: "text-gray-400",
    border: "border-gray-600",
  };
</script>

<div
  class="group border-b border-gray-800 transition-colors"
  class:bg-gray-900={!isSelected && !expanded}
  class:bg-gray-850={!isSelected && expanded}
  class:selected={isSelected}
  class:opacity-50={rule.disabled}
>
  <!-- Main Row -->
  <div
    class="flex items-center h-12 px-4 gap-3 cursor-pointer hover:bg-white/5"
    on:click={() => (expanded = !expanded)}
    on:keydown={(e) => e.key === "Enter" && (expanded = !expanded)}
    role="button"
    tabindex="0"
  >
    <!-- Drag Handle -->
    <div
      class="text-gray-600 hover:text-gray-400 cursor-grab opacity-0 group-hover:opacity-100 transition-opacity"
    >
      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
        <circle cx="8" cy="6" r="2" /><circle cx="16" cy="6" r="2" />
        <circle cx="8" cy="12" r="2" /><circle cx="16" cy="12" r="2" />
        <circle cx="8" cy="18" r="2" /><circle cx="16" cy="18" r="2" />
      </svg>
    </div>

    <!-- Enable/Disable Toggle -->
    <button
      class="w-8 h-4 rounded-full transition-colors relative flex-shrink-0"
      class:bg-green-600={!rule.disabled}
      class:bg-gray-600={rule.disabled}
      on:click|stopPropagation={toggleRule}
      title={rule.disabled ? "Enable rule" : "Disable rule"}
    >
      <div
        class="absolute w-3 h-3 bg-white rounded-full top-0.5 transition-all"
        class:left-0.5={rule.disabled}
        class:left-4={!rule.disabled}
      ></div>
    </button>

    <!-- Rule Sentence: From [Source] To [Dest] On [Port] -->
    <div class="flex-1 flex items-center gap-2 text-sm min-w-0">
      <span
        class="text-gray-500 text-xs uppercase tracking-wider font-semibold shrink-0"
        >{$t("policy.from")}</span
      >
      <AddressPill resolved={rule.resolved_src} raw={rule.src_ip || ""} />

      <span
        class="text-gray-500 text-xs uppercase tracking-wider font-semibold shrink-0"
        >{$t("policy.to")}</span
      >
      <AddressPill resolved={rule.resolved_dest} raw={rule.dest_ip || ""} />

      <span
        class="text-gray-500 text-xs uppercase tracking-wider font-semibold shrink-0"
        >{$t("policy.on")}</span
      >
      <span
        class="px-2 py-0.5 rounded bg-gray-800 border border-gray-700 text-gray-300 text-xs"
      >
        {portDisplay}
      </span>
    </div>

    <!-- Sparkline -->
    <div
      class="w-24 h-6 opacity-50 group-hover:opacity-100 transition-opacity flex-shrink-0"
    >
      {#if rule.stats?.sparkline_data}
        <Sparkline
          data={rule.stats.sparkline_data}
          color={rule.action === "drop" ? "#EF4444" : "#10B981"}
        />
      {/if}
    </div>

    <!-- Action Badge -->
    <div
      class="px-2.5 py-1 rounded text-xs font-bold uppercase tracking-wide border flex-shrink-0 {actionColors.bg} {actionColors.text} {actionColors.border}"
    >
      {rule.action}
    </div>

    <!-- Expand Indicator -->
    <div
      class="text-gray-500 transition-transform {expanded ? 'rotate-180' : ''}"
    >
      <svg
        class="w-4 h-4"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <path d="M6 9l6 6 6-6" />
      </svg>
    </div>
  </div>

  <!-- Expanded Details -->
  {#if expanded}
    <div transition:slide class="bg-gray-950/50 border-t border-gray-800 p-4">
      <div class="grid grid-cols-3 gap-6">
        <!-- Left: Details -->
        <div class="col-span-2 space-y-4">
          <!-- Description -->
          <div>
            <div class="block text-xs text-gray-500 uppercase mb-1">
              {$t("policy.description")}
            </div>
            <div class="text-sm text-gray-300">
              {rule.description || rule.name || $t("policy.no_description")}
            </div>
          </div>

          <!-- Stats -->
          {#if rule.stats}
            <div class="flex gap-6">
              <div>
                <div class="block text-xs text-gray-500 uppercase mb-1">
                  {$t("policy.packets")}
                </div>
                <div class="text-sm text-gray-300 font-mono">
                  {rule.stats.packets?.toLocaleString() || 0}
                </div>
              </div>
              <div>
                <div class="block text-xs text-gray-500 uppercase mb-1">
                  {$t("policy.bytes")}
                </div>
                <div class="text-sm text-gray-300 font-mono">
                  {formatBytes(rule.stats.bytes || 0)}
                </div>
              </div>
            </div>
          {/if}

          <!-- NFT Syntax (Power User) -->
          {#if rule.nft_syntax}
            <div>
              <div class="block text-xs text-gray-500 uppercase mb-1">
                {$t("policy.generated_rule")}
              </div>
              <code
                class="block text-xs text-green-400 bg-gray-900 p-2 rounded font-mono overflow-x-auto"
              >
                {rule.nft_syntax}
              </code>
            </div>
          {/if}
        </div>

        <!-- Right: Actions -->
        <div class="border-l border-gray-800 pl-6 flex flex-col gap-2">
          <button
            class="flex items-center gap-2 text-sm text-gray-400 hover:text-white transition-colors text-left"
            on:click={() => onDuplicate?.(rule)}
          >
            <svg
              class="w-4 h-4"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <rect x="9" y="9" width="13" height="13" rx="2" />
              <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
            </svg>
            {$t("policy.duplicate_rule")}
          </button>

          <button
            class="flex items-center gap-2 text-sm text-red-400 hover:text-red-300 transition-colors text-left mt-auto"
            on:click={() => rule.id && onDelete?.(rule.id)}
          >
            <svg
              class="w-4 h-4"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2"
              />
            </svg>
            {$t("policy.delete_rule")}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>
