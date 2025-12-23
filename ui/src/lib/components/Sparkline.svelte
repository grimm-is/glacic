<script lang="ts">
    /**
     * Sparkline - Lightweight SVG sparkline chart for rule traffic visualization
     * Uses raw SVG for performance with 50+ rows
     */
    export let data: number[] = [];
    export let height = 24;
    export let width = 100;
    export let color = "#10B981"; // Emerald-500
    export let showArea = false;

    // Compute SVG polyline points from data
    $: points = (() => {
        if (!data || data.length < 2) return "";
        const max = Math.max(...data, 1); // Avoid division by zero
        return data
            .map((val, i) => {
                const x = (i / (data.length - 1)) * width;
                const y = height - (val / max) * (height - 2); // Leave 1px margin
                return `${x.toFixed(1)},${y.toFixed(1)}`;
            })
            .join(" ");
    })();

    // Area path for filled sparkline
    $: areaPath = (() => {
        if (!showArea || !data || data.length < 2) return "";
        const max = Math.max(...data, 1);
        const pts = data.map((val, i) => {
            const x = (i / (data.length - 1)) * width;
            const y = height - (val / max) * (height - 2);
            return `${x.toFixed(1)},${y.toFixed(1)}`;
        });
        return `M0,${height} L${pts.join(" L")} L${width},${height} Z`;
    })();
</script>

<svg
    {width}
    {height}
    class="overflow-visible"
    role="img"
    aria-label="Traffic sparkline"
>
    {#if showArea && areaPath}
        <path d={areaPath} fill={color} fill-opacity="0.15" />
    {/if}
    {#if points}
        <polyline
            {points}
            fill="none"
            stroke={color}
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
        />
    {:else}
        <!-- No data indicator -->
        <line
            x1="0"
            y1={height / 2}
            x2={width}
            y2={height / 2}
            stroke="currentColor"
            stroke-width="1"
            stroke-dasharray="2,4"
            class="text-gray-700"
        />
    {/if}
</svg>
