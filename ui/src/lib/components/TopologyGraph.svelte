<script lang="ts">
    import { onMount, createEventDispatcher } from "svelte";
    import * as d3 from "d3";
    import type {
        TopologyGraph,
        TopologyNode,
        TopologyLink,
    } from "../stores/app";

    export let graph: TopologyGraph;

    const dispatch = createEventDispatcher();

    let container: HTMLDivElement;
    let svg: SVGSVGElement;
    let width = 800;
    let height = 600;

    // D3 Selection types
    type SVGSelection = d3.Selection<SVGSVGElement, unknown, null, undefined>;
    type GSelection = d3.Selection<SVGGElement, unknown, null, undefined>;

    let root: d3.HierarchyNode<TopologyNode>;
    let rootPoint: d3.HierarchyPointNode<TopologyNode>; // Typed for layout
    let treeLayout: d3.TreeLayout<TopologyNode>;

    // Restore missing vars
    let linkGroup: GSelection;
    let nodeGroup: GSelection;
    let transform = d3.zoomIdentity;
    let initialFitDone = false;

    $: if (graph && svg && width && height) {
        // Debounce update slightly to prevent churn
        updateGraph();
    }

    onMount(() => {
        if (!container) return;

        const resizeObserver = new ResizeObserver((entries) => {
            for (let entry of entries) {
                width = entry.contentRect.width;
                height = entry.contentRect.height;
                if (graph) updateGraph();
            }
        });
        resizeObserver.observe(container);

        initGraph();

        return () => {
            resizeObserver.disconnect();
        };
    });

    function initGraph() {
        const svgSelect = d3.select(svg) as SVGSelection;

        // Zoom behavior
        const zoom = d3
            .zoom<SVGSVGElement, unknown>()
            .scaleExtent([0.1, 4])
            .on("zoom", (event) => {
                transform = event.transform;
                if (linkGroup)
                    linkGroup.attr("transform", transform.toString());
                if (nodeGroup)
                    nodeGroup.attr("transform", transform.toString());
            });

        svgSelect.call(zoom);

        // Create groups for layers
        // Clear previous if re-init
        svgSelect.selectAll("g").remove();

        const g = svgSelect.append("g");
        linkGroup = g.append("g").attr("class", "links");
        nodeGroup = g.append("g").attr("class", "nodes");
    }

    // --- CSS Helper for Colors ---
    // We need to access CSS vars in JS, or use a helper to get computed style.
    // Simpler: use known hex fallbacks or just set styles directly in JS if D3 requires it.
    // D3 needs string colors for fill/stroke.
    // We can use "var(--color-primary)" strings! SVG supports them.

    function updateGraph() {
        if (!graph || !graph.nodes || !graph.links || graph.nodes.length === 0)
            return;

        const parentMap = new Map<string, string | null>();
        graph.nodes.forEach((n) => parentMap.set(n.id, null));
        graph.links.forEach((l) => {
            const sourceId =
                typeof l.source === "object" ? (l.source as any).id : l.source;
            const targetId =
                typeof l.target === "object" ? (l.target as any).id : l.target;
            parentMap.set(targetId, sourceId);
        });

        // Validating Root
        let roots = 0;
        let rootID = "";
        parentMap.forEach((parent, id) => {
            if (parent === null) {
                roots++;
                rootID = id;
            }
        });

        if (roots === 0) {
            console.warn("Topology cycle detected, no root found.");
            const router = graph.nodes.find((n) => n.type === "router");
            if (router) {
                rootID = router.id;
                parentMap.set(rootID, null);
            }
        }

        const stratify = d3
            .stratify<TopologyNode>()
            .id((d) => d.id)
            .parentId((d) => parentMap.get(d.id));

        try {
            root = stratify(graph.nodes);
        } catch (e) {
            console.error("Failed to stratify topology:", e);
            return;
        }

        rootPoint = root as d3.HierarchyPointNode<TopologyNode>;

        // --- Custom "Grid" Layout Implementation ---
        // Instead of d3.tree, we manually position nodes to ensure compact grid limits.
        // Hierarchy: Router(0) -> Interface(1) -> Device(2)

        const nodeWidth = 220;
        const nodeHeight = 60;
        const paddingX = 40; // Horizontal spacing between levels
        const paddingY = 20; // Vertical spacing between nodes
        const gridGap = 20;

        // Level X Positions
        const level0_X = 50;
        const level1_X = level0_X + nodeWidth + 100;
        const level2_X = level1_X + nodeWidth + 80; // Devices start here

        // 1. Identify Interfaces (Level 1)
        const routerNode = rootPoint;
        const interfaces = routerNode.children || [];

        // 2. Calculate Layout Heights
        let currentY = 0;

        routerNode.x = 0; // Temporary, will center later
        routerNode.y = level0_X;

        // Iterate Interfaces
        interfaces.forEach((iface) => {
            const devices = iface.children || [];

            // Calculate Device Grid dimensions
            let gridRows = 0;
            let gridCols = 0;
            let gridBlockHeight = 0;
            let gridBlockWidth = 0;

            if (devices.length > 0) {
                // Aim for a square-ish grid, maybe slightly wider
                gridCols = Math.ceil(Math.sqrt(devices.length));
                // Cap columns if too wide? say max 4 columns to stay compact
                if (gridCols > 3) gridCols = 3;
                gridRows = Math.ceil(devices.length / gridCols);

                gridBlockWidth =
                    gridCols * nodeWidth + (gridCols - 1) * gridGap;
                gridBlockHeight =
                    gridRows * nodeHeight + (gridRows - 1) * gridGap;
            }

            // Height of this "Interface Branch" is max of InterfaceNode vs DeviceGrid
            const branchHeight = Math.max(nodeHeight, gridBlockHeight);

            // Center Interface vertically in this branch
            iface.y = level1_X;
            iface.x = currentY + branchHeight / 2;

            // Position Devices in Grid
            devices.forEach((dev, idx) => {
                const col = idx % gridCols;
                const row = Math.floor(idx / gridCols);

                dev.y = level2_X + col * (nodeWidth + gridGap);
                dev.x =
                    iface.x -
                    gridBlockHeight / 2 +
                    row * (nodeHeight + gridGap) +
                    nodeHeight / 2;
            });

            currentY += branchHeight + paddingY;
        });

        // 3. Center Router relative to all interfaces
        if (interfaces.length > 0) {
            const first = interfaces[0];
            const last = interfaces[interfaces.length - 1];
            routerNode.x = (first.x + last.x) / 2;
        } else {
            routerNode.x = nodeHeight / 2;
            currentY = nodeHeight + paddingY;
        }

        // --- Fit to Screen ---
        // Calculate Bounds
        let minX = Infinity;
        let maxX = -Infinity;
        let minY = Infinity;
        let maxY = -Infinity;
        rootPoint.each((d) => {
            if (d.x < minX) minX = d.x;
            if (d.x > maxX) maxX = d.x;
            if (d.y < minY) minY = d.y;
            if (d.y > maxY) maxY = d.y;
        });

        // Add node dimensions
        // Note: x is vertical, y is horizontal in our logic above?
        // Logic above: x is vertical position (0=top), y is horizontal (0=left).
        // Yes, used `iface.y = level1_X` (horizontal).

        // Bounds need to account for node size
        // Since nodes are anchored centered-left (rect y = -height/2)
        // actually rect is drawn at (0, -height/2).
        // So x is vertical CENTER of node.
        // y is LEFT edge of node.

        minX -= nodeHeight / 2;
        maxX += nodeHeight / 2;
        // minY is already left edge.
        maxY += nodeWidth; // Right edge

        const treeHeight = maxX - minX + 100;
        const treeWidth = maxY - minY + 100;

        if (svg && !initialFitDone) {
            const svgSelect = d3.select(svg) as SVGSelection;

            const scaleX = width / treeWidth;
            const scaleY = height / treeHeight;
            const scale = Math.min(1, Math.min(scaleX, scaleY) * 0.9);

            const centerY = (minX + maxX) / 2;
            const translateY = height / 2 - centerY * scale;
            const translateX = 50 - minY * scale;

            svgSelect.call(
                d3.zoom<SVGSVGElement, unknown>().transform as any,
                d3.zoomIdentity.translate(translateX, translateY).scale(scale),
            );
            initialFitDone = true;
        }

        // --- Render ---

        // Links
        // Custom link generator for Grid
        // d3.linkHorizontal works if we have proper structure.
        // It uses d.y (horizontal) and d.x (vertical).
        // Our logic above set d.y as Horizontal, d.x as Vertical. Matches.

        const linkGen = d3
            .linkHorizontal<
                d3.HierarchyPointLink<TopologyNode>,
                d3.HierarchyPointNode<TopologyNode>
            >()
            .x((d) => d.y)
            .y((d) => d.x);

        linkGroup.attr("transform", null); // Handled by zoom
        nodeGroup.attr("transform", null);

        linkGroup
            .selectAll("path")
            .data(
                rootPoint.links() as unknown as d3.HierarchyPointLink<TopologyNode>[],
                (d) => `${d.source.data.id}-${d.target.data.id}`,
            )
            .join("path")
            .attr("fill", "none")
            .attr("stroke", "var(--color-border)")
            .attr("stroke-width", 2)
            .attr("stroke-opacity", 0.5)
            .attr("d", linkGen);

        // Nodes
        const nodes = nodeGroup
            .selectAll<
                SVGGElement,
                d3.HierarchyPointNode<TopologyNode>
            >("g.node")
            .data(
                rootPoint.descendants() as d3.HierarchyPointNode<TopologyNode>[],
                (d) => d.data.id,
            );

        const nodeEnter = nodes
            .enter()
            .append("g")
            .attr("class", "node")
            .attr("transform", (d) => `translate(${d.y},${d.x})`)
            .attr("opacity", (d) =>
                d.data.type === "virtual_interface" ? 0 : 1,
            )
            .attr("cursor", "pointer")
            // Hoist to front on hover
            .on("mouseenter", function () {
                d3.select(this).raise();
            });

        // Box
        nodeEnter
            .append("rect")
            .attr("width", nodeWidth)
            .attr("height", nodeHeight)
            .attr("x", 0)
            .attr("y", -nodeHeight / 2)
            .attr("rx", 6)
            .attr("ry", 6)
            .attr("fill", "var(--color-surface)")
            .attr("stroke", (d) => getNodeColor(d.data.type))
            .attr("stroke-width", 1)
            .attr("filter", "drop-shadow(0 1px 2px rgb(0 0 0 / 0.05))")
            .attr("opacity", (d) =>
                d.data.type === "virtual_interface" ? 0 : 1,
            );

        // Decoration Line
        nodeEnter
            .append("rect")
            .attr("width", 4)
            .attr("height", nodeHeight)
            .attr("x", 0)
            .attr("y", -nodeHeight / 2)
            .attr("rx", 1)
            .attr("fill", (d) => getNodeColor(d.data.type));

        // Icon Circle
        nodeEnter
            .append("circle")
            .attr("cx", 24)
            .attr("cy", 0)
            .attr("r", 14)
            .attr("fill", (d) => getNodeColor(d.data.type))
            .attr("fill-opacity", 0.1);

        // Text Group
        const textGroup = nodeEnter
            .append("g")
            .attr("transform", "translate(46, 0)");

        // Label
        textGroup
            .append("text")
            .attr("class", "label")
            .attr("dy", "-0.3em")
            .attr("fill", "var(--color-foreground)")
            .style("font-weight", "500")
            .style("font-size", "13px")
            .text((d) => truncate(d.data.label || d.data.id || "Unknown", 22));

        // Sublabel
        textGroup
            .append("text")
            .attr("class", "sublabel")
            .attr("dy", "1.1em")
            .attr("fill", "var(--color-mutedForeground)")
            .style("font-size", "11px")
            .text((d) => d.data.ip || "No IP");

        // Icon
        nodeEnter
            .append("text")
            .attr("class", "material-symbols-rounded")
            .attr("x", 24)
            .attr("y", 0)
            .attr("dy", "0.35em")
            .attr("text-anchor", "middle")
            .attr("fill", (d) => getNodeColor(d.data.type))
            .style("font-size", "18px")
            .text((d) => getIconChar(d.data.type, d.data.icon));

        // Tooltip
        nodeEnter
            .append("title")
            .text(
                (d) =>
                    `${d.data.label}\nIP: ${d.data.ip || "Unknown"}\n${d.data.description || ""}`,
            );

        // Merge Enter + Update selections
        const allNodes = nodeEnter.merge(nodes as any);

        // Update Transitions
        allNodes
            .transition()
            .attr("transform", (d) => `translate(${d.y},${d.x})`);

        allNodes
            .select("rect")
            .attr("stroke", (d) => getNodeColor(d.data.type));

        // Update Text
        allNodes
            .select(".label")
            .text((d) => truncate(d.data.label || d.data.id || "Unknown", 22))
            .attr("fill", "#ffffff") // Force white for visibility
            .style("fill", "#ffffff");

        allNodes
            .select(".sublabel")
            .text(
                (d) =>
                    d.data.ip ||
                    d.data.description ||
                    truncate(d.data.description || "", 25),
            )
            .attr("fill", "rgba(255, 255, 255, 0.7)") // Muted white
            .style("fill", "rgba(255, 255, 255, 0.7)");

        allNodes
            .select(".material-symbols-rounded")
            .text((d) => getIconChar(d.data.type, d.data.icon))
            .attr("fill", (d) => getNodeColor(d.data.type)); // Icon uses type color

        allNodes
            .select("title")
            .text(
                (d) =>
                    `${d.data.label}\nIP: ${d.data.ip || "Unknown"}\n${d.data.description || ""}`,
            );

        nodes.exit().remove();
    }

    function truncate(str: string, len: number) {
        if (!str) return "";
        return str.length > len ? str.substring(0, len) + "..." : str;
    }

    function getNodeColor(type: string): string {
        if (type === "router") return "var(--color-destructive)";
        if (type === "switch") return "var(--color-primary)";
        if (type === "cloud") return "var(--color-primary)";
        return "var(--color-success)";
    }

    function getIconChar(type: string, iconKey?: string): string {
        const key = iconKey || type;
        switch (key) {
            case "router":
            case "gateway":
                return "router";
            case "switch":
            case "bridge":
                return "switch_video";
            case "cloud":
            case "internet":
                return "cloud";
            case "apple":
            case "phone":
            case "mobile":
            case "iphone":
            case "android":
                return "phone_iphone";
            case "tablet":
            case "ipad":
                return "tablet_mac";
            case "laptop":
            case "macbook":
            case "notebook":
                return "laptop_mac";
            case "desktop":
            case "computer":
            case "pc":
            case "workstation":
                return "desktop_windows";
            case "cast":
            case "chromecast":
                return "cast";
            case "printer":
                return "print";
            case "light":
            case "bulb":
                return "lightbulb";
            case "camera":
            case "webcam":
                return "videocam";
            case "speaker":
            case "audio":
            case "sonos":
                return "speaker";
            case "tv":
            case "television":
                return "tv";
            case "nas":
            case "storage":
                return "hard_drive";
            case "server":
                return "dns";
            case "iot":
            case "smart_home":
                return "smart_toy";
            case "game_console":
            case "console":
            case "xbox":
            case "playstation":
            case "nintendo":
                return "videogame_asset";
            case "watch":
                return "watch";
            case "device":
            default:
                return "devices";
        }
    }
</script>

<div class="topology-container" bind:this={container}>
    <svg bind:this={svg} {width} {height}></svg>
</div>

<style>
    .topology-container {
        width: 100%;
        height: 100%; /* Fill parent */
        min-height: 600px;
        background-color: transparent; /* Transparent as requested */
        /* border-radius: var(--radius-lg); Removed border/bg to blend */
        overflow: hidden;
        position: relative;
    }

    svg text {
        fill: var(--color-foreground);
    }

    svg {
        display: block;
        width: 100%;
        height: 100%;
        cursor: grab;
    }

    /* ... rest of style ... */
    svg:active {
        cursor: grabbing;
    }

    :global(.material-symbols-rounded) {
        font-family: "Material Symbols Rounded";
        font-weight: normal;
        font-style: normal;
        font-size: 24px;
        line-height: 1;
        letter-spacing: normal;
        text-transform: none;
        display: inline-block;
        white-space: nowrap;
        word-wrap: normal;
        direction: ltr;
        -webkit-font-feature-settings: "liga";
        font-feature-settings: "liga";
        -webkit-font-smoothing: antialiased;
    }
</style>
