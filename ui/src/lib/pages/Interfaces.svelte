<script lang="ts">
  /**
   * Interfaces Page
   * Network interface configuration with full CRUD
   */

  import { onMount } from "svelte";
  import { config, api } from "$lib/stores/app";
  import {
    Card,
    Button,
    Modal,
    Input,
    Select,
    Badge,
    Spinner,
    Icon,
    Toggle,
  } from "$lib/components";
  import InterfaceStateBadge from "$lib/components/InterfaceStateBadge.svelte";
  import { t } from "svelte-i18n";

  let loading = $state(false);
  let showVlanModal = $state(false);
  let showBondModal = $state(false);
  let showEditModal = $state(false);
  let editingInterface = $state<any>(null);
  let interfaceStatus = $state<any[]>([]);

  // VLAN form
  let vlanParent = $state("");
  let vlanId = $state("");
  let vlanZone = $state("");
  let vlanIp = $state("");

  // Bond form
  let bondName = $state("");
  let bondZone = $state("");
  let bondMode = $state("balance-rr");
  let bondMembers = $state<string[]>([]);

  // Edit form
  let editDescription = $state("");
  // Zone editing removed as per user request (deprecated field)
  let editIpv4 = $state("");
  let editDhcp = $state(false);
  let editMtu = $state("");
  let editGateway = $state("");
  let editDisabled = $state(false);

  // Load runtime status
  onMount(async () => {
    try {
      const res = await api.getInterfaces();
      // Normalize API response (snake_case) to Component model (PascalCase)
      interfaceStatus = (res.interfaces || []).map((s: any) => ({
        ...s,
        Name: s.name,
        State: s.state,
        IPv4Addrs: s.ipv4_addrs,
        IPv6Addrs: s.ipv6_addrs,
      }));
    } catch (e) {
      console.error("Failed to load interface status", e);
    }
  });

  const zones = $derived($config?.zones || []);
  const rawInterfaces = $derived($config?.interfaces || []);

  // Merge static config with runtime status
  const interfaces = $derived(
    rawInterfaces.map((iface: any) => {
      const status = interfaceStatus.find((s) => s.Name === iface.Name);
      return {
        ...iface,
        // Prefer runtime IPs if available, otherwise fallback to config
        IPv4: status?.IPv4Addrs?.length ? status.IPv4Addrs : iface.IPv4,
        State: status?.State || iface.State,
      };
    }),
  );

  // All hardware interfaces for bond creation (with availability status)
  const hardwareInterfaces = $derived(
    interfaces
      .filter((iface: any) => {
        // Hardware interfaces: not a VLAN (no '.'), not a bond (no 'bond' prefix)
        return !iface.Name?.includes(".") && !iface.Name?.startsWith("bond");
      })
      .map((iface: any) => {
        // Check if already in a bond
        const inBond = interfaces.some(
          (other: any) =>
            other.Bond?.members?.includes(iface.Name) ||
            other.Members?.includes(iface.Name),
        );
        // Check if it IS a bond (has members)
        const isBond =
          iface.Bond?.members?.length > 0 || iface.Members?.length > 0;
        // Check if has an IP assigned (in use)
        // Note: We check config IPv4/DHCP, as runtime IPs might just be auto-conf
        const hasIP =
          (iface.IPv4?.length > 0 && !iface.IPv4[0].startsWith("169.254")) ||
          iface.DHCP;
        // Check if assigned to a zone
        const hasZone = !!iface.Zone;

        const isAvailable = !inBond && !isBond && !hasIP && !hasZone;
        const usageReason = inBond
          ? "in bond"
          : isBond
            ? "is bond"
            : hasIP
              ? "has IP"
              : hasZone
                ? "in zone"
                : null;

        return {
          ...iface,
          isAvailable,
          usageReason,
        };
      }),
  );

  const availableInterfaces = $derived(
    hardwareInterfaces.filter((i: any) => i.isAvailable),
  );
  const hasAnyHardwareInterfaces = $derived(hardwareInterfaces.length > 0);
  const isDegradedBond = $derived(bondMembers.length === 1);

  function getZoneColor(zoneName: string): string {
    const zone = zones.find((z: any) => z.name === zoneName);
    return zone?.color || "gray";
  }

  function getInterfaceType(iface: any): string {
    if (iface.Name?.startsWith("bond")) return "bond";
    if (iface.Name?.includes(".")) return "vlan";
    if (iface.Name?.startsWith("wg")) return "wireguard";
    if (iface.Name?.startsWith("tun") || iface.Name?.startsWith("tap"))
      return "tunnel";
    return "ethernet";
  }

  const canCreateBond = $derived(
    interfaces.filter((i: any) => getInterfaceType(i) === "ethernet").length >=
      2,
  );

  function openEditInterface(iface: any) {
    editingInterface = iface;
    editDescription = iface.Description || "";
    editIpv4 = (iface.IPv4 || []).join(", ");
    editDhcp = iface.DHCP || false;
    editMtu = iface.MTU?.toString() || "";
    editGateway = iface.Gateway || "";
    editDisabled = iface.Disabled || false;
    showEditModal = true;
  }

  async function saveInterfaceEdit() {
    if (!editingInterface) return;

    loading = true;
    try {
      await api.updateInterface({
        name: editingInterface.Name,
        action: "update", // Required by UpdateInterfaceArgs
        description: editDescription || undefined,
        ipv4: editIpv4
          ? editIpv4
              .split(",")
              .map((s: string) => s.trim())
              .filter(Boolean)
          : undefined,
        dhcp: editDhcp,
        mtu: editMtu ? parseInt(editMtu) : undefined,
        // Note: gateway and disabled are not supported by UpdateInterfaceArgs
        // They require separate API endpoints or config updates
      });
      showEditModal = false;
      // Refresh status
      interfaceStatus = await api.getInterfaces();
    } catch (e: any) {
      alert(`Failed to update interface: ${e.message || e}`);
      console.error("Failed to update interface:", e);
    } finally {
      loading = false;
    }
  }

  function openAddVlan() {
    vlanParent = interfaces[0]?.Name || "";
    vlanId = "";
    vlanZone = zones[0]?.name || "";
    vlanIp = "";
    showVlanModal = true;
  }

  async function saveVlan() {
    if (!vlanParent || !vlanId || !vlanZone) return;

    loading = true;
    try {
      await api.createVlan({
        parent: vlanParent,
        vlan_id: parseInt(vlanId),
        zone: vlanZone,
        ipv4: vlanIp || undefined,
      });
      showVlanModal = false;
    } catch (e) {
      console.error("Failed to create VLAN:", e);
    } finally {
      loading = false;
    }
  }

  function openAddBond() {
    bondName = "bond0";
    bondZone = zones[0]?.name || "";
    bondMode = "balance-rr";
    bondMembers = [];
    showBondModal = true;
  }

  function toggleBondMember(ifaceName: string) {
    if (bondMembers.includes(ifaceName)) {
      bondMembers = bondMembers.filter((m) => m !== ifaceName);
    } else {
      bondMembers = [...bondMembers, ifaceName];
    }
  }

  async function saveBond() {
    if (!bondName || !bondZone || bondMembers.length < 1) return;

    loading = true;
    try {
      await api.createBond({
        name: bondName,
        zone: bondZone,
        mode: bondMode,
        members: bondMembers,
      });
      showBondModal = false;
    } catch (e) {
      console.error("Failed to create Bond:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="interfaces-page">
  <div class="page-header">
    <div class="header-actions">
      <Button variant="outline" onclick={openAddVlan}
        >{$t("common.add_item", {
          values: { item: $t("item.vlan") },
        })}</Button
      >
      {#if canCreateBond}
        <Button variant="outline" onclick={openAddBond}
          >{$t("common.add_item", {
            values: { item: $t("item.bond") },
          })}</Button
        >
      {/if}
    </div>
  </div>

  {#if interfaces.length === 0}
    <Card>
      <p class="empty-message">
        {$t("common.no_items", {
          values: { items: $t("item.interface") },
        })}
      </p>
    </Card>
  {:else}
    <div class="interfaces-grid">
      {#each interfaces as iface (iface.Name)}
        <Card>
          <div class="iface-header">
            <div class="iface-name-row">
              <span class="iface-name">{iface.Name}</span>
              {#if iface.Alias}<span class="iface-alias">({iface.Alias})</span
                >{/if}
              <Badge
                variant={getInterfaceType(iface) === "ethernet"
                  ? "outline"
                  : "secondary"}
              >
                {getInterfaceType(iface)}
              </Badge>
              <InterfaceStateBadge
                state={iface.State || (iface.Disabled ? "disabled" : "up")}
                size="sm"
              />
            </div>
            <Button
              variant="ghost"
              size="sm"
              onclick={() => openEditInterface(iface)}
              ><Icon name="edit" size="sm" /></Button
            >
          </div>

          {#if iface.Description}
            <p class="iface-description">{iface.Description}</p>
          {/if}

          <div class="iface-details">
            <div class="detail-row">
              <span class="detail-label">{$t("item.zone")}:</span>
              <span
                class="zone-badge"
                style="--zone-color: var(--zone-{getZoneColor(iface.Zone)})"
                >{iface.Zone || $t("common.none")}</span
              >
            </div>

            {#if iface.Vendor}
              <div class="detail-row">
                <span class="detail-label">{$t("common.vendor")}:</span>
                <span class="detail-value">{iface.Vendor}</span>
              </div>
            {/if}

            <div class="detail-row">
              <span class="detail-label">{$t("interfaces.ipv4")}:</span>
              <span class="detail-value mono">
                {#if iface.DHCP && (!iface.IPv4 || iface.IPv4.length === 0)}
                  {$t("interfaces.dhcp_acquiring")}
                {:else if iface.IPv4?.length > 0}
                  {iface.IPv4.join(", ")}
                  {#if iface.DHCP}
                    <span class="text-xs text-muted-foreground ml-1"
                      >({$t("interfaces.dhcp")})</span
                    >
                  {/if}
                {:else}
                  {$t("common.none")}
                {/if}
              </span>
            </div>

            {#if iface.IPv6?.length > 0}
              <div class="detail-row">
                <span class="detail-label">{$t("interfaces.ipv6")}:</span>
                <span class="detail-value mono">{iface.IPv6.join(", ")}</span>
              </div>
            {/if}

            {#if iface.Gateway}
              <div class="detail-row">
                <span class="detail-label">{$t("common.gateway")}:</span>
                <span class="detail-value mono">{iface.Gateway}</span>
              </div>
            {/if}

            {#if iface.MTU}
              <div class="detail-row">
                <span class="detail-label">{$t("common.mtu")}:</span>
                <span class="detail-value">{iface.MTU}</span>
              </div>
            {/if}

            {#if iface.Bond?.members?.length > 0 || iface.Members?.length > 0}
              <div class="detail-row">
                <span class="detail-label"
                  >{$t("interfaces.bond_members")}:</span
                >
                <span class="detail-value">
                  {(iface.Bond?.members || iface.Members || []).join(", ")}
                </span>
              </div>
            {/if}

            {#if iface.VLANs?.length > 0}
              <div class="detail-row">
                <span class="detail-label">{$t("interfaces.vlans")}:</span>
                <span class="detail-value">
                  {iface.VLANs.map((v: any) => v.ID || v.id).join(", ")}
                </span>
              </div>
            {/if}
          </div>
        </Card>
      {/each}
    </div>
  {/if}
</div>

<!-- Edit Interface Modal -->
<Modal
  bind:open={showEditModal}
  title={$t("common.edit_item", {
    values: { item: editingInterface?.Name || $t("item.interface") },
  })}
>
  <div class="form-stack">
    <Input
      id="edit-description"
      label={$t("common.description")}
      bind:value={editDescription}
      placeholder="e.g., Primary WAN"
    />

    <!-- Zone editing removed (deprecated) -->

    <Toggle label={$t("interfaces.use_dhcp")} bind:checked={editDhcp} />

    {#if !editDhcp}
      <Input
        id="edit-ipv4"
        label={$t("interfaces.ipv4_list")}
        bind:value={editIpv4}
        placeholder="192.168.1.1/24, 192.168.1.2/24"
      />

      <Input
        id="edit-gateway"
        label={$t("common.gateway")}
        bind:value={editGateway}
        placeholder="192.168.1.254"
      />
    {/if}

    <Input
      id="edit-mtu"
      label={$t("common.mtu")}
      bind:value={editMtu}
      placeholder="1500"
      type="text"
    />

    <Toggle
      label={$t("interfaces.interface_enabled")}
      checked={!editDisabled}
      onchange={(checked) => (editDisabled = !checked)}
    />

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showEditModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveInterfaceEdit} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        {$t("common.save")}
      </Button>
    </div>
  </div>
</Modal>

<!-- VLAN Modal -->
<Modal
  bind:open={showVlanModal}
  title={$t("common.add_item", { values: { item: $t("item.vlan") } })}
>
  <div class="form-stack">
    <Select
      id="vlan-parent"
      label={$t("interfaces.parent_interface")}
      bind:value={vlanParent}
      options={interfaces
        .filter((i: any) => !i.Name?.includes("."))
        .map((i: any) => ({ value: i.Name, label: i.Name }))}
      required
    />

    <Input
      id="vlan-id"
      label={$t("interfaces.vlan_id")}
      bind:value={vlanId}
      placeholder="100"
      type="number"
      required
    />

    <Select
      id="vlan-zone"
      label={$t("item.zone")}
      bind:value={vlanZone}
      options={zones.map((z: any) => ({ value: z.name, label: z.name }))}
      required
    />

    <Input
      id="vlan-ip"
      label={$t("interfaces.ipv4_list")}
      bind:value={vlanIp}
      placeholder="192.168.100.1/24"
    />

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showVlanModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveVlan} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        {$t("common.create_item", { values: { item: $t("item.vlan") } })}
      </Button>
    </div>
  </div>
</Modal>

<!-- Bond Modal -->
<Modal
  bind:open={showBondModal}
  title={$t("common.add_item", { values: { item: $t("item.bond") } })}
>
  <div class="form-stack">
    <Input
      id="bond-name"
      label={$t("common.name")}
      bind:value={bondName}
      placeholder="bond0"
      required
    />

    <Select
      id="bond-zone"
      label={$t("item.zone")}
      bind:value={bondZone}
      options={zones.map((z: any) => ({ value: z.name, label: z.name }))}
      required
    />

    <Select
      id="bond-mode"
      label={$t("interfaces.bond_mode")}
      bind:value={bondMode}
      options={[
        { value: "balance-rr", label: "Round Robin (balance-rr)" },
        { value: "active-backup", label: "Active Backup (active-backup)" },
        { value: "balance-xor", label: "XOR (balance-xor)" },
        { value: "broadcast", label: "Broadcast" },
        { value: "802.3ad", label: "LACP (802.3ad)" },
        { value: "balance-tlb", label: "Adaptive TLB (balance-tlb)" },
        { value: "balance-alb", label: "Adaptive ALB (balance-alb)" },
      ]}
    />

    <div class="member-selection">
      <span class="member-label">{$t("interfaces.select_members")}</span>
      <div class="member-list">
        {#each hardwareInterfaces as iface}
          <label class="member-item" class:disabled={!iface.isAvailable}>
            <input
              type="checkbox"
              checked={bondMembers.includes(iface.Name)}
              disabled={!iface.isAvailable}
              onchange={() => toggleBondMember(iface.Name)}
            />
            <span class="member-name">{iface.Name}</span>
            {#if !iface.isAvailable}
              <span class="member-status">({iface.usageReason})</span>
            {/if}
          </label>
        {/each}
        {#if hardwareInterfaces.length === 0}
          <p class="member-warning">{$t("interfaces.no_hardware")}</p>
        {/if}
      </div>

      {#if availableInterfaces.length === 0}
        <p class="member-warning">
          ⚠️ {$t("interfaces.no_available")}
        </p>
      {:else if availableInterfaces.length === 1}
        <p class="member-info">
          ℹ️ {$t("interfaces.one_available")}
        </p>
      {/if}

      {#if isDegradedBond}
        <p class="member-warning degraded">
          ⚠️ {$t("interfaces.degraded_bond")}
        </p>
      {/if}
    </div>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showBondModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveBond} disabled={loading || bondMembers.length < 1}>
        {#if loading}<Spinner size="sm" />{/if}
        {isDegradedBond
          ? $t("common.create_item", { values: { item: $t("item.bond") } })
          : $t("common.create_item", { values: { item: $t("item.bond") } })}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .interfaces-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .header-actions {
    display: flex;
    gap: var(--space-2);
  }

  .interfaces-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: var(--space-4);
  }

  .iface-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-2);
  }

  .iface-name-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .iface-name {
    font-family: var(--font-mono);
    font-weight: 600;
    font-size: var(--text-lg);
    color: var(--color-foreground);
  }

  .iface-description {
    color: var(--color-muted);
    font-size: var(--text-sm);
    margin: 0 0 var(--space-3) 0;
  }

  .iface-details {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-top: var(--space-3);
    border-top: 1px solid var(--color-border);
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    font-size: var(--text-sm);
  }

  .detail-label {
    color: var(--color-muted);
  }

  .detail-value {
    color: var(--color-foreground);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .zone-badge {
    display: inline-flex;
    padding: var(--space-1) var(--space-2);
    background-color: var(--zone-color, var(--color-muted));
    color: white;
    font-weight: 500;
    font-size: var(--text-xs);
    border-radius: var(--radius-sm);
  }

  .empty-message {
    color: var(--color-muted);
    text-align: center;
    margin: 0;
  }

  .form-stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .member-selection {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .member-label {
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--color-foreground);
  }

  .member-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-3);
    background-color: var(--color-backgroundSecondary);
    border-radius: var(--radius-md);
  }

  .member-item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    cursor: pointer;
    font-size: var(--text-sm);
  }

  .member-item.disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .member-name {
    color: var(--color-foreground);
  }

  .member-status {
    color: var(--color-muted);
    font-size: var(--text-xs);
    font-style: italic;
  }

  .member-warning {
    color: var(--color-warning, #f59e0b);
    font-size: var(--text-sm);
    margin: var(--space-2) 0 0 0;
  }

  .member-warning.degraded {
    color: var(--color-destructive);
  }

  .member-info {
    color: var(--color-muted);
    font-size: var(--text-sm);
    margin: var(--space-2) 0 0 0;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-4);
    padding-top: var(--space-4);
    border-top: 1px solid var(--color-border);
  }

  /* Zone color variables */
  :global(:root) {
    --zone-red: #dc2626;
    --zone-green: #16a34a;
    --zone-blue: #2563eb;
    --zone-orange: #ea580c;
    --zone-purple: #9333ea;
    --zone-cyan: #0891b2;
    --zone-gray: #6b7280;
  }
</style>
