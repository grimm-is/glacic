<script lang="ts">
    /**
     * AddressPill - Smart badge for displaying resolved addresses with popovers
     * Handles alias resolution display: device names, IPSets, zones, etc.
     */
    export let resolved: {
        display_name: string;
        type: string;
        description?: string;
        count: number;
        is_truncated?: boolean;
        preview?: string[];
    } | null = null;

    export let raw: string = "";
    export let size: "sm" | "md" = "sm";

    let showTooltip = false;
    let tooltipTimeout: ReturnType<typeof setTimeout> | null = null;

    function handleMouseEnter() {
        tooltipTimeout = setTimeout(() => {
            showTooltip = true;
        }, 300); // Delay to avoid flickering
    }

    function handleMouseLeave() {
        if (tooltipTimeout) {
            clearTimeout(tooltipTimeout);
            tooltipTimeout = null;
        }
        showTooltip = false;
    }

    // Color and style based on type
    const typeStyles: Record<string, string> = {
        device_named: "text-blue-400 bg-blue-900/40 border-blue-700",
        device_auto: "text-cyan-400 bg-cyan-900/40 border-cyan-700",
        device_vendor: "text-gray-400 bg-gray-800/60 border-gray-600",
        alias: "text-purple-400 bg-purple-900/40 border-purple-700",
        ipset: "text-purple-400 bg-purple-900/40 border-purple-700",
        zone: "text-amber-400 bg-amber-900/40 border-amber-700",
        cidr: "text-green-400 bg-green-900/40 border-green-700",
        service: "text-pink-400 bg-pink-900/40 border-pink-700",
        port: "text-pink-400 bg-pink-900/40 border-pink-700",
        host: "text-gray-300 bg-gray-800/60 border-gray-600",
        ip: "text-gray-300 bg-gray-800/60 border-gray-600",
        any: "text-gray-500 bg-gray-900/40 border-gray-700 italic",
        raw: "text-gray-300 bg-gray-800/60 border-gray-600",
    };

    $: displayName = resolved?.display_name || raw || "Any";
    $: pillType = resolved?.type || "raw";
    $: pillStyle = typeStyles[pillType] || typeStyles.raw;
    $: sizeClass = size === "sm" ? "text-xs px-2 py-0.5" : "text-sm px-3 py-1";
</script>

<div
    class="relative inline-block"
    on:mouseenter={handleMouseEnter}
    on:mouseleave={handleMouseLeave}
    role="button"
    tabindex="0"
>
    <!-- Pill Badge -->
    <div
        class="rounded border cursor-help transition-all hover:brightness-110 {pillStyle} {sizeClass}"
    >
        <span class="font-medium">{displayName}</span>
        {#if resolved && resolved.count > 1}
            <span class="ml-1 opacity-60">({resolved.count})</span>
        {/if}
    </div>

    <!-- Tooltip Popover -->
    {#if showTooltip && resolved && resolved.type !== "any"}
        <div
            class="absolute z-50 bottom-full left-0 mb-2 w-64 bg-gray-800 border border-gray-700 shadow-xl rounded-lg p-3 text-xs pointer-events-none"
            role="tooltip"
        >
            <div class="font-bold text-white mb-1">{resolved.display_name}</div>
            <div class="text-gray-400 mb-2 flex items-center gap-2">
                <span
                    class="px-1.5 py-0.5 rounded bg-gray-700 text-gray-300 uppercase text-[10px] tracking-wide"
                >
                    {resolved.type.replace("_", " ")}
                </span>
                {#if resolved.description}
                    <span class="truncate">{resolved.description}</span>
                {/if}
            </div>

            {#if resolved.preview && resolved.preview.length > 0}
                <div class="space-y-1 max-h-32 overflow-y-auto">
                    {#each resolved.preview as item}
                        <div
                            class="font-mono text-gray-300 bg-gray-900/60 px-1.5 py-0.5 rounded text-[11px]"
                        >
                            {item}
                        </div>
                    {/each}
                    {#if resolved.is_truncated}
                        <div class="text-gray-500 italic">
                            ...and {resolved.count -
                                (resolved.preview?.length || 0)} more
                        </div>
                    {/if}
                </div>
            {/if}
        </div>
    {/if}
</div>
