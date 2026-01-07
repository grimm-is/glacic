<script lang="ts">
    /**
     * Network Page
     * Unified view of network devices, clients, and topology
     */

    import { onMount, onDestroy } from "svelte";
    import { get } from "svelte/store";
    import { leases, config, api, alertStore, topology } from "$lib/stores/app";
    import {
        Card,
        Badge,
        Input,
        Modal,
        Button,
        Icon,
        Spinner,
        TopologyGraph,
    } from "$lib/components";
    import { t } from "svelte-i18n";

    // ... (state)

    // Subscribe to topology store & enrich with device info
    let topologyGraph = $derived.by(() => {
        const raw = $topology || { nodes: [], links: [] };
        if (!raw.nodes) return { nodes: [], links: [] };

        // Create a map of allDevices for fast lookup
        const deviceMap = new Map<string, any>();
        for (const dev of allDevices) {
            if (dev.mac) deviceMap.set(dev.mac.toLowerCase(), dev);
            if (dev.ip_address) deviceMap.set(dev.ip_address, dev);
        }

        const nodes = raw.nodes.map((node) => {
            const rawId = node.id.toLowerCase();

            // 1. Hide Interface Switch Nodes (sw-eth0, etc)
            if (rawId.startsWith("sw-")) {
                return {
                    ...node,
                    type: "virtual_interface",
                    label: node.id,
                    icon: "hub",
                };
            }

            // 2. Lookup Devices (dev-MAC)
            let dev = null;
            if (rawId.startsWith("dev-")) {
                const mac = rawId.replace("dev-", "");
                dev = deviceMap.get(mac);
            } else {
                // Fallback direct lookup just in case
                dev = deviceMap.get(rawId);
            }

            if (dev) {
                return {
                    ...node,
                    label: dev.displayName || node.label,
                    ip: dev.ip_address || node.ip,
                    icon: dev.device_type || node.icon,
                    description: dev.vendor || node.description,
                };
            }

            // 3. Return others (router, unlinked devices) as-is
            return node;
        });

        // Stabilize order to prevent jumping
        nodes.sort((a, b) => a.id.localeCompare(b.id));

        return {
            nodes,
            links: raw.links,
        };
    });

    onMount(async () => {
        await loadDiscoveredDevices();
        // Initial fetch if empty
        if (!get(topology).nodes?.length) {
            loadTopology();
        }
    });

    // ... (loadTopology function updated to just fetch and update store/local state if needed, but store is better)
    async function loadTopology() {
        loadingTopology = true;
        try {
            const data = await api.getTopology(); // Uses api wrapper
            // api.getTopology returns the JSON data.
            // data is now { neighbors: [], graph: { nodes: [], links: [] } }
            // update store manually if not using WS yet or for initial load
            topology.set(data.graph || { nodes: [], links: [] });
        } catch (e) {
            console.error("Failed to load topology", e);
        } finally {
            loadingTopology = false;
        }
    }

    // Tabs
    type Tab = "devices" | "topology";
    let activeTab = $state<Tab>("devices");

    // DEVICES TAB STATE
    let searchQuery = $state("");
    let editingClient = $state<any>(null);
    let editModalOpen = $state(false);
    let discoveredDevices = $state<any[]>([]);
    let loadingDevices = $state(true);
    let loadingTopology = $state(false); // Used for initial fetch spinner if store empty

    // Edit State
    let editAlias = $state("");
    let editOwner = $state("");
    let editType = $state("");
    let editTags = $state("");
    let isSaving = $state(false);

    async function loadDiscoveredDevices() {
        loadingDevices = true;
        try {
            const response = await fetch("/api/network?details=full", {
                credentials: "include",
                headers: { "Content-Type": "application/json" },
            });
            if (response.ok) {
                const data = await response.json();
                discoveredDevices = data.devices || [];
            }
        } catch (e) {
            console.error("Failed to load discovered devices:", e);
        } finally {
            loadingDevices = false;
        }
    }

    // Merge DHCP leases with discovered devices
    const allDevices = $derived.by(() => {
        const clients: any[] = [];
        const seenMACs = new Set<string>();

        // Create map of discovered devices for easy lookup
        const devMap = new Map<string, any>();
        for (const dev of discoveredDevices) {
            if (dev.mac) devMap.set(dev.mac.toLowerCase(), dev);
        }

        // Add DHCP leases first (they have more data)
        for (const lease of $leases || []) {
            const mac = lease.mac?.toLowerCase();
            if (mac) seenMACs.add(mac);

            const dev = devMap.get(mac);

            // Use the best available hostname: user alias -> lease hostname -> discovery hostname
            const displayName =
                lease.alias ||
                dev?.mdns_hostname ||
                dev?.dhcp_hostname ||
                dev?.hostname ||
                lease.hostname ||
                lease.client_id ||
                "Unknown";

            clients.push({
                ...lease,
                // Merge discovery data
                mdns_services: dev?.mdns_services || [],
                mdns_hostname: dev?.mdns_hostname,
                mdns_txt: dev?.mdns_txt || {},
                dhcp_fingerprint: dev?.dhcp_fingerprint,
                dhcp_vendor_class: dev?.dhcp_vendor_class,
                dhcp_client_id: dev?.dhcp_client_id,
                dhcp_options: dev?.dhcp_options || {},
                device_type: dev?.device_type,
                device_model: dev?.device_model,
                // End merge
                source: "dhcp",
                displayName,
                status: lease.active ? "active" : "expired",
                vendor: dev?.vendor || lease.vendor, // Prefer discovery vendor
            });
        }

        // Add discovered devices that don't have DHCP leases
        for (const dev of discoveredDevices) {
            const mac = dev.mac?.toLowerCase();
            if (mac && !seenMACs.has(mac)) {
                const displayName =
                    dev.alias ||
                    dev.mdns_hostname ||
                    dev.dhcp_hostname ||
                    dev.hostname ||
                    dev.vendor ||
                    "Unknown Device";
                clients.push({
                    mac: dev.mac,
                    ip_address: dev.ips?.[0] || "",
                    hostname: dev.hostname,
                    vendor: dev.vendor,
                    alias: dev.alias,
                    interface: dev.interface,
                    source: "discovery",
                    displayName,
                    status: "discovered",
                    last_seen: dev.last_seen,
                    packet_count: dev.packet_count,
                    is_gateway: dev.is_gateway,
                    // Profiling fields
                    mdns_services: dev.mdns_services || [],
                    mdns_hostname: dev.mdns_hostname,
                    mdns_txt: dev.mdns_txt || {},
                    dhcp_fingerprint: dev.dhcp_fingerprint,
                    dhcp_vendor_class: dev.dhcp_vendor_class,
                    dhcp_client_id: dev.dhcp_client_id,
                    dhcp_options: dev.dhcp_options || {},
                    device_type: dev.device_type,
                    device_model: dev.device_model,
                });
            }
        }

        return clients;
    });

    const filteredDevices = $derived(
        searchQuery.trim() === ""
            ? allDevices
            : allDevices.filter((c: any) => {
                  const q = searchQuery.toLowerCase();
                  return (
                      c.displayName?.toLowerCase().includes(q) ||
                      c.ip_address?.includes(q) ||
                      c.mac?.toLowerCase().includes(q) ||
                      c.vendor?.toLowerCase().includes(q) ||
                      (c.tags || []).some((t: string) =>
                          t.toLowerCase().includes(q),
                      )
                  );
              }),
    );

    // Determine if interface is WAN or LAN
    function getInterfaceType(iface: string): {
        label: string;
        variant: "default" | "secondary" | "destructive";
    } {
        if (!iface) return { label: "unknown", variant: "secondary" };
        const lower = iface.toLowerCase();
        if (lower.includes("wan") || lower === "eth0") {
            return { label: "WAN", variant: "destructive" };
        }
        return { label: "LAN", variant: "default" };
    }

    function openEdit(client: any) {
        editingClient = client;
        editAlias = client.alias || "";
        editOwner = client.owner || "";
        editType = client.type || "";
        editTags = (client.tags || []).join(", ");
        editModalOpen = true;
    }

    function closeEdit() {
        editingClient = null;
        editModalOpen = false;
    }

    async function saveIdentity() {
        if (!editingClient) return;
        isSaving = true;

        try {
            const tags = editTags
                .split(",")
                .map((t) => t.trim())
                .filter((t) => t);
            const alias =
                editAlias ||
                editingClient.hostname ||
                `Device-${editingClient.mac}`;

            // Update Identity (Creates if ID is empty)
            const identity = await api.updateDeviceIdentity(
                editingClient.device_id || "",
                alias,
                editOwner,
                editType,
                tags,
            );

            // If we didn't have an ID before, we must Link the MAC to the new Identity
            if (!editingClient.device_id && identity && identity.id) {
                await api.linkDevice(editingClient.mac, identity.id);
            }

            alertStore.show($t("network.update_success"), "success");
            // Refresh
            loadDiscoveredDevices();
            closeEdit();
        } catch (err: any) {
            alertStore.show(
                err.message || $t("network.update_failed"),
                "error",
            );
        } finally {
            isSaving = false;
        }
    }

    async function unlinkIdentity() {
        if (!editingClient || !editingClient.device_id) return;
        if (!confirm($t("network.unlink_confirm"))) return;

        isSaving = true;
        try {
            await api.unlinkDevice(editingClient.mac);
            alertStore.show($t("network.device_unlinked"), "success");
            loadDiscoveredDevices();
            closeEdit();
        } catch (err: any) {
            alertStore.show(
                err.message || $t("network.unlink_failed"),
                "error",
            );
        } finally {
            isSaving = false;
        }
    }

    function formatTimeAgo(timestamp: number): string {
        if (!timestamp) return $t("time_ago.never");
        const seconds = Math.floor(Date.now() / 1000 - timestamp);
        if (seconds < 60)
            return $t("time_ago.seconds", { values: { n: seconds } });
        if (seconds < 3600)
            return $t("time_ago.minutes", {
                values: { n: Math.floor(seconds / 60) },
            });
        if (seconds < 86400)
            return $t("time_ago.hours", {
                values: { n: Math.floor(seconds / 3600) },
            });
        return $t("time_ago.days", {
            values: { n: Math.floor(seconds / 86400) },
        });
    }
</script>

<div class="network-page">
    <div class="page-header">
        <div class="tabs">
            <button
                class="tab-btn"
                class:active={activeTab === "devices"}
                onclick={() => (activeTab = "devices")}
            >
                {$t("network.devices", {
                    values: {
                        n: allDevices.filter(
                            (d) =>
                                d.status === "active" ||
                                d.status === "discovered",
                        ).length,
                    },
                })}
            </button>
            <button
                class="tab-btn"
                class:active={activeTab === "topology"}
                onclick={() => (activeTab = "topology")}
            >
                {$t("network.topology")}
            </button>
        </div>
        <div class="header-actions">
            <button
                class="refresh-btn"
                onclick={() => {
                    loadDiscoveredDevices();
                    loadTopology();
                }}
                disabled={loadingDevices || loadingTopology}
            >
                <Icon name="refresh" size="sm" />
            </button>
        </div>
    </div>

    {#if activeTab === "devices"}
        <Card>
            <div class="search-row">
                <Input
                    id="client-search"
                    placeholder={$t("network.search_placeholder")}
                    bind:value={searchQuery}
                />
            </div>

            {#if loadingDevices && filteredDevices.length === 0}
                <div class="loading-state">
                    <Spinner size="lg" />
                    <p>{$t("network.scanning")}</p>
                </div>
            {:else if filteredDevices.length > 0}
                <div class="clients-list">
                    {#each filteredDevices as client}
                        <div class="client-row">
                            <div class="client-main">
                                <span class="client-name">
                                    {client.displayName}
                                    {#if client.alias}
                                        <span class="client-real-hostname"
                                            >({client.hostname ||
                                                client.mac})</span
                                        >
                                    {/if}
                                </span>
                                <code class="client-ip"
                                    >{client.ip_address}</code
                                >

                                {#if client.tags && client.tags.length > 0}
                                    <div class="client-tags">
                                        {#each client.tags as tag}
                                            <Badge variant="secondary"
                                                >{tag}</Badge
                                            >
                                        {/each}
                                    </div>
                                {/if}
                            </div>

                            <div class="client-meta">
                                {#if client.device_type}
                                    <Badge variant="outline" class="capitalize"
                                        >{client.device_type.replace(
                                            "_",
                                            " ",
                                        )}</Badge
                                    >
                                {/if}

                                {#if client.mdns_services && client.mdns_services.length > 0}
                                    <span
                                        title={client.mdns_services.join(", ")}
                                    >
                                        <Badge variant="secondary"
                                            >{$t("network.mdns")}</Badge
                                        >
                                    </span>
                                {/if}

                                <code class="client-mac">{client.mac}</code>

                                {#if client.interface}
                                    {@const ifaceType = getInterfaceType(
                                        client.interface,
                                    )}
                                    <Badge variant={ifaceType.variant}
                                        >{ifaceType.label}</Badge
                                    >
                                {/if}

                                <Button
                                    size="sm"
                                    variant="ghost"
                                    onclick={() => openEdit(client)}
                                >
                                    {$t("network.manage")}
                                </Button>
                            </div>
                        </div>
                    {/each}
                </div>
            {:else}
                <div class="empty-state">
                    <span class="empty-icon">📱</span>
                    <h3>
                        {$t("common.no_items", {
                            values: { items: $t("item.device") },
                        })}
                    </h3>
                    <p>{$t("network.no_devices_desc")}</p>
                </div>
            {/if}
        </Card>
    {:else if activeTab === "topology"}
        {#if loadingTopology && topologyGraph.nodes.length === 0}
            <Card><div class="loading-state"><Spinner /></div></Card>
        {:else if topologyGraph.nodes.length === 0}
            <Card>
                <div class="empty-state">
                    <h3>{$t("network.no_topology")}</h3>
                    <p>{$t("network.no_topology_desc")}</p>
                </div>
            </Card>
        {:else}
            <!-- Force graph takes full width/height defined in component -->
            <TopologyGraph graph={topologyGraph} />
        {/if}
    {/if}
</div>

<Modal
    bind:open={editModalOpen}
    title={$t("common.edit_item", { values: { item: $t("item.device") } })}
>
    <div class="edit-form">
        {#if editingClient}
            <div class="form-group">
                <label for="mac">{$t("network.mac_address")}</label>
                <code id="mac">{editingClient.mac}</code>
            </div>

            {#if editingClient.vendor}
                <div class="form-group">
                    <label for="vendor">{$t("network.vendor")}</label>
                    <span id="vendor">{editingClient.vendor}</span>
                </div>
            {/if}

            {#if editingClient.dhcp_fingerprint}
                <div class="form-group">
                    <label for="fp">{$t("network.dhcp_fingerprint")}</label>
                    <code id="fp" style="font-size:0.8em"
                        >{editingClient.dhcp_fingerprint}</code
                    >
                </div>
            {/if}

            <div class="form-group">
                <label for="alias">{$t("network.alias")}</label>
                <Input
                    id="alias"
                    bind:value={editAlias}
                    placeholder={$t("network.friendly_name")}
                />
            </div>

            <div class="form-group">
                <label for="owner">{$t("network.owner")}</label>
                <Input
                    id="owner"
                    bind:value={editOwner}
                    placeholder={$t("network.owner_placeholder")}
                />
            </div>

            <div class="form-group">
                <label for="type">{$t("network.type")}</label>
                <Input
                    id="type"
                    bind:value={editType}
                    placeholder={$t("network.type_placeholder")}
                />
            </div>

            <div class="form-group">
                <label for="tags">{$t("network.tags")}</label>
                <Input
                    id="tags"
                    bind:value={editTags}
                    placeholder={$t("network.tags_placeholder")}
                />
            </div>

            <div class="form-group">
                <label for="raw">{$t("network.raw_attributes")}</label>
                <div
                    class="raw-data"
                    style="font-size: 0.8em; color: var(--color-muted);"
                >
                    {#if editingClient.device_model}
                        <div>
                            <strong>{$t("network.model")}</strong>
                            {editingClient.device_model}
                        </div>
                    {/if}
                    {#if editingClient.mdns_hostname}
                        <div>
                            <strong>{$t("network.mdns_host")}</strong>
                            {editingClient.mdns_hostname}
                        </div>
                    {/if}
                    {#if editingClient.dhcp_vendor_class}
                        <div>
                            <strong>{$t("network.dhcp_vendor_class")}</strong>
                            {editingClient.dhcp_vendor_class}
                        </div>
                    {/if}
                    {#if editingClient.dhcp_client_id}
                        <div>
                            <strong>{$t("network.dhcp_client_id")}</strong>
                            {editingClient.dhcp_client_id}
                        </div>
                    {/if}
                    <details>
                        <summary style="cursor: pointer; margin-top: 0.5rem;"
                            >{$t("network.full_raw_data")}</summary
                        >
                        <pre
                            style="background: var(--color-backgroundSecondary); padding: 0.5rem; border-radius: 4px; overflow: auto; margin-top: 0.5rem;">{JSON.stringify(
                                editingClient,
                                null,
                                2,
                            )}</pre>
                    </details>
                </div>
            </div>

            <div class="modal-actions">
                {#if editingClient.device_id}
                    <Button
                        variant="destructive"
                        onclick={unlinkIdentity}
                        disabled={isSaving}
                        >{$t("network.unlink_identity")}</Button
                    >
                {/if}
                <div class="spacer"></div>
                <Button variant="ghost" onclick={closeEdit} disabled={isSaving}
                    >{$t("common.cancel")}</Button
                >
                <Button onclick={saveIdentity} disabled={isSaving}>
                    {isSaving
                        ? $t("network.saving")
                        : $t("network.save_changes")}
                </Button>
            </div>
        {/if}
    </div>
</Modal>

<style>
    .network-page {
        display: flex;
        flex-direction: column;
        gap: var(--space-6);
    }

    .page-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
    }

    .tabs {
        display: flex;
        gap: var(--space-2);
        background: var(--color-surface);
        padding: var(--space-1);
        border-radius: var(--radius-md);
        border: 1px solid var(--color-border);
    }

    .tab-btn {
        padding: var(--space-2) var(--space-4);
        background: none;
        border: none;
        border-radius: var(--radius-sm);
        color: var(--color-muted);
        cursor: pointer;
        font-weight: 500;
    }

    .tab-btn.active {
        background: var(--color-background);
        color: var(--color-foreground);
        box-shadow: var(--shadow-sm);
    }

    .refresh-btn {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-2);
        background: var(--color-surface);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        color: var(--color-foreground);
        cursor: pointer;
    }

    .clients-list {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
    }

    .client-row {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: var(--space-3);
        background-color: var(--color-backgroundSecondary);
        border-radius: var(--radius-md);
    }

    .client-main {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        flex-wrap: wrap;
        flex: 1;
    }

    .client-name {
        font-weight: 500;
        color: var(--color-foreground);
    }

    .client-real-hostname {
        font-size: var(--text-xs);
        color: var(--color-muted);
    }

    .client-ip {
        font-size: var(--text-sm);
        color: var(--color-muted);
        min-width: 120px;
    }

    .client-mac {
        font-family: var(--font-mono);
        font-size: var(--text-xs);
        color: var(--color-muted);
    }

    .client-tags {
        display: flex;
        gap: var(--space-1);
    }

    .client-meta {
        display: flex;
        align-items: center;
        gap: var(--space-3);
    }

    .loading-state,
    .empty-state {
        padding: var(--space-8);
        text-align: center;
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: var(--space-2);
        color: var(--color-muted);
    }
    .empty-icon {
        font-size: 2rem;
    }

    /* Modal Styles */
    .edit-form {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
    }
    .form-group {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
    }
    .form-group label {
        font-size: var(--text-sm);
        font-weight: 500;
    }
    .modal-actions {
        display: flex;
        gap: var(--space-2);
        margin-top: var(--space-4);
    }
    .spacer {
        flex: 1;
    }
</style>
