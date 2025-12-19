<script lang="ts">
  /**
   * Interfaces Page
   * Network interface configuration with full CRUD
   */

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

  let loading = $state(false);
  let showVlanModal = $state(false);
  let showBondModal = $state(false);
  let showEditModal = $state(false);
  let editingInterface = $state<any>(null);

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
  let editZone = $state("");
  let editIpv4 = $state("");
  let editDhcp = $state(false);
  let editMtu = $state("");
  let editGateway = $state("");
  let editDisabled = $state(false);

  const zones = $derived($config?.zones || []);
  const interfaces = $derived($config?.interfaces || []);

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
        const hasIP = iface.IPv4?.length > 0 || iface.DHCP;
        // Check if assigned to a zone
        const hasZone = !!iface.Zone;

        const isAvailable = !inBond && !isBond;
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
    editZone = iface.Zone || "";
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
        description: editDescription || undefined,
        zone: editZone || undefined,
        ipv4: editIpv4
          ? editIpv4
              .split(",")
              .map((s: string) => s.trim())
              .filter(Boolean)
          : undefined,
        dhcp: editDhcp,
        mtu: editMtu ? parseInt(editMtu) : undefined,
        gateway: editGateway || undefined,
        disabled: editDisabled,
      });
      showEditModal = false;
    } catch (e) {
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
    <h2>Network Interfaces</h2>
    <div class="header-actions">
      <Button variant="outline" onclick={openAddVlan}>Add VLAN</Button>
      {#if canCreateBond}
        <Button variant="outline" onclick={openAddBond}>Add Bond</Button>
      {/if}
    </div>
  </div>

  {#if interfaces.length === 0}
    <Card>
      <p class="empty-message">No interfaces configured.</p>
    </Card>
  {:else}
    <div class="interfaces-grid">
      {#each interfaces as iface}
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
              <span class="detail-label">Zone:</span>
              <span
                class="zone-badge"
                style="--zone-color: var(--zone-{getZoneColor(iface.Zone)})"
                >{iface.Zone || "None"}</span
              >
            </div>

            {#if iface.Vendor}
              <div class="detail-row">
                <span class="detail-label">Vendor:</span>
                <span class="detail-value">{iface.Vendor}</span>
              </div>
            {/if}

            <div class="detail-row">
              <span class="detail-label">IPv4:</span>
              <span class="detail-value mono">
                {#if iface.DHCP}
                  DHCP
                {:else if iface.IPv4?.length > 0}
                  {iface.IPv4.join(", ")}
                {:else}
                  None
                {/if}
              </span>
            </div>

            {#if iface.IPv6?.length > 0}
              <div class="detail-row">
                <span class="detail-label">IPv6:</span>
                <span class="detail-value mono">{iface.IPv6.join(", ")}</span>
              </div>
            {/if}

            {#if iface.Gateway}
              <div class="detail-row">
                <span class="detail-label">Gateway:</span>
                <span class="detail-value mono">{iface.Gateway}</span>
              </div>
            {/if}

            {#if iface.MTU}
              <div class="detail-row">
                <span class="detail-label">MTU:</span>
                <span class="detail-value">{iface.MTU}</span>
              </div>
            {/if}

            {#if iface.Bond?.members?.length > 0 || iface.Members?.length > 0}
              <div class="detail-row">
                <span class="detail-label">Bond Members:</span>
                <span class="detail-value">
                  {(iface.Bond?.members || iface.Members || []).join(", ")}
                </span>
              </div>
            {/if}

            {#if iface.VLANs?.length > 0}
              <div class="detail-row">
                <span class="detail-label">VLANs:</span>
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
  title={`Edit ${editingInterface?.Name || "Interface"}`}
>
  <div class="form-stack">
    <Input
      id="edit-description"
      label="Description"
      bind:value={editDescription}
      placeholder="e.g., Primary WAN"
    />

    <Select
      id="edit-zone"
      label="Zone"
      bind:value={editZone}
      options={[
        { value: "", label: "None" },
        ...zones.map((z: any) => ({ value: z.name, label: z.name })),
      ]}
    />

    <Toggle label="Use DHCP" bind:checked={editDhcp} />

    {#if !editDhcp}
      <Input
        id="edit-ipv4"
        label="IPv4 Addresses (comma-separated)"
        bind:value={editIpv4}
        placeholder="192.168.1.1/24, 192.168.1.2/24"
      />

      <Input
        id="edit-gateway"
        label="Default Gateway"
        bind:value={editGateway}
        placeholder="192.168.1.254"
      />
    {/if}

    <Input
      id="edit-mtu"
      label="MTU (optional)"
      bind:value={editMtu}
      placeholder="Default"
      type="number"
    />

    <Toggle
      label="Interface Enabled"
      checked={!editDisabled}
      onchange={(checked) => (editDisabled = !checked)}
    />

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showEditModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveInterfaceEdit} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        Save Changes
      </Button>
    </div>
  </div>
</Modal>

<!-- VLAN Modal -->
<Modal bind:open={showVlanModal} title="Add VLAN Interface">
  <div class="form-stack">
    <Select
      id="vlan-parent"
      label="Parent Interface"
      bind:value={vlanParent}
      options={interfaces
        .filter((i: any) => !i.Name?.includes("."))
        .map((i: any) => ({ value: i.Name, label: i.Name }))}
      required
    />

    <Input
      id="vlan-id"
      label="VLAN ID"
      bind:value={vlanId}
      placeholder="100"
      type="number"
      required
    />

    <Select
      id="vlan-zone"
      label="Zone"
      bind:value={vlanZone}
      options={zones.map((z: any) => ({ value: z.name, label: z.name }))}
      required
    />

    <Input
      id="vlan-ip"
      label="IPv4 Address (optional)"
      bind:value={vlanIp}
      placeholder="192.168.100.1/24"
    />

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showVlanModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveVlan} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        Create VLAN
      </Button>
    </div>
  </div>
</Modal>

<!-- Bond Modal -->
<Modal bind:open={showBondModal} title="Create Bond Interface">
  <div class="form-stack">
    <Input
      id="bond-name"
      label="Bond Name"
      bind:value={bondName}
      placeholder="bond0"
      required
    />

    <Select
      id="bond-zone"
      label="Zone"
      bind:value={bondZone}
      options={zones.map((z: any) => ({ value: z.name, label: z.name }))}
      required
    />

    <Select
      id="bond-mode"
      label="Bonding Mode"
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
      <span class="member-label">Select Member Interfaces</span>
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
          <p class="member-warning">No hardware interfaces detected.</p>
        {/if}
      </div>

      {#if availableInterfaces.length === 0}
        <p class="member-warning">
          ⚠️ No available interfaces for bonding. All interfaces are in use.
        </p>
      {:else if availableInterfaces.length === 1}
        <p class="member-info">
          ℹ️ Only 1 interface available. Bond will be created in degraded mode.
        </p>
      {/if}

      {#if isDegradedBond}
        <p class="member-warning degraded">
          ⚠️ Degraded Bond: Only 1 member selected. Bond will have no
          redundancy.
        </p>
      {/if}
    </div>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showBondModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveBond} disabled={loading || bondMembers.length < 1}>
        {#if loading}<Spinner size="sm" />{/if}
        {isDegradedBond ? "Create Degraded Bond" : "Create Bond"}
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

  .page-header h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
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
