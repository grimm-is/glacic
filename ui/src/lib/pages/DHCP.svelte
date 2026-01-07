<script lang="ts">
  /**
   * DHCP Page
   * DHCP server settings and lease management
   */

  import { config, leases, api } from "$lib/stores/app";
  import {
    Card,
    Button,
    Modal,
    Input,
    Select,
    Badge,
    Table,
    Spinner,
    Icon,
    Toggle,
  } from "$lib/components";
  import { t } from "svelte-i18n";

  let loading = $state(false);
  let showScopeModal = $state(false);
  let editingIndex = $state<number | null>(null);
  let isEditMode = $derived(editingIndex !== null);
  let showSettingsModal = $state(false);

  // Global Settings
  let dhcpMode = $state("builtin");
  let dhcpLeaseFile = $state("");

  // Scope form
  let scopeName = $state("");
  let scopeInterface = $state("");
  let scopeRangeStart = $state("");
  let scopeRangeEnd = $state("");
  let scopeRouter = $state("");
  let scopeDns = $state("");
  let scopeLeaseTime = $state("24h");
  let scopeDomain = $state("");
  let scopeReservations = $state<any[]>([]);

  // Reservation form
  let newResMac = $state("");
  let newResIp = $state("");
  let newResHostname = $state("");

  const dhcpConfig = $derived($config?.dhcp || { enabled: false, scopes: [] });
  const interfaces = $derived($config?.interfaces || []);
  const activeLeases = $derived(($leases || []).filter((l: any) => l.active));

  const leaseColumns = [
    { key: "ip", label: "IP Address" },
    { key: "alias", label: "Device" },
    { key: "mac", label: "MAC Address" },
    { key: "vendor", label: "Vendor" },
    { key: "hostname", label: "Hostname" },
    { key: "interface", label: "Interface" },
  ];

  async function toggleDHCP() {
    loading = true;
    try {
      await api.updateDHCP({
        ...dhcpConfig,
        enabled: !dhcpConfig.enabled,
      });
    } catch (e) {
      console.error("Failed to toggle DHCP:", e);
    } finally {
      loading = false;
    }
  }

  function openSettings() {
    dhcpMode = dhcpConfig.mode || "builtin";
    dhcpLeaseFile = dhcpConfig.external_lease_file || "";
    showSettingsModal = true;
  }

  async function saveSettings() {
    loading = true;
    try {
      await api.updateDHCP({
        ...dhcpConfig,
        mode: dhcpMode,
        external_lease_file: dhcpLeaseFile || undefined,
      });
      showSettingsModal = false;
    } catch (e) {
      console.error("Failed to save DHCP settings:", e);
    } finally {
      loading = false;
    }
  }

  function openAddScope() {
    editingIndex = null;
    scopeName = "";
    scopeInterface = interfaces[0]?.Name || "";
    scopeRangeStart = "";
    scopeRangeEnd = "";
    scopeRouter = "";
    scopeDns = "";
    scopeLeaseTime = "24h";
    scopeDomain = "";
    scopeReservations = [];
    showScopeModal = true;
  }

  function openEditScope(index: number) {
    editingIndex = index;
    const scope = dhcpConfig.scopes[index];
    scopeName = scope.name || "";
    scopeInterface = scope.interface || interfaces[0]?.Name || "";
    scopeRangeStart = scope.range_start || "";
    scopeRangeEnd = scope.range_end || "";
    scopeRouter = scope.router || "";
    scopeDns = (scope.dns || []).join(", ");
    scopeLeaseTime = scope.lease_time || "24h";
    scopeDomain = scope.domain || "";
    scopeReservations = [...(scope.reservations || [])];
    showScopeModal = true;
  }

  async function saveScope() {
    if (!scopeName || !scopeInterface || !scopeRangeStart || !scopeRangeEnd)
      return;

    loading = true;
    try {
      const newScope = {
        name: scopeName,
        interface: scopeInterface,
        range_start: scopeRangeStart,
        range_end: scopeRangeEnd,
        router: scopeRouter || undefined,
        dns: scopeDns
          ? scopeDns
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean)
          : undefined,
        lease_time: scopeLeaseTime,
        domain: scopeDomain || undefined,
        reservations: scopeReservations,
      };

      let updatedScopes;
      if (isEditMode && editingIndex !== null) {
        updatedScopes = [...(dhcpConfig.scopes || [])];
        updatedScopes[editingIndex] = newScope;
      } else {
        updatedScopes = [...(dhcpConfig.scopes || []), newScope];
      }

      await api.updateDHCP({
        ...dhcpConfig,
        scopes: updatedScopes,
      });
      showScopeModal = false;
    } catch (e) {
      console.error("Failed to save DHCP scope:", e);
    } finally {
      loading = false;
    }
  }

  function addReservation() {
    if (!newResMac || !newResIp) return;
    scopeReservations = [
      ...scopeReservations,
      { mac: newResMac, ip: newResIp, hostname: newResHostname },
    ];
    newResMac = "";
    newResIp = "";
    newResHostname = "";
  }

  function removeReservation(mac: string) {
    scopeReservations = scopeReservations.filter((r) => r.mac !== mac);
  }

  async function deleteScope(index: number) {
    const scope = dhcpConfig.scopes[index];
    if (
      !confirm(
        $t("common.delete_confirm_item", {
          values: { item: $t("item.scope") },
        }),
      )
    )
      return;

    loading = true;
    try {
      const updatedScopes = dhcpConfig.scopes.filter(
        (_: any, i: number) => i !== index,
      );
      await api.updateDHCP({
        ...dhcpConfig,
        scopes: updatedScopes,
      });
    } catch (e) {
      console.error("Failed to delete scope:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="dhcp-page">
  <div class="page-header">
    <div class="header-actions">
      <Button variant="outline" onclick={openSettings} disabled={loading}>
        <Icon name="settings" />
        {$t("common.settings")}
      </Button>
      <Button
        variant={dhcpConfig.enabled ? "destructive" : "default"}
        onclick={toggleDHCP}
        disabled={loading}
      >
        {dhcpConfig.enabled ? $t("common.disable") : $t("common.enable")}
      </Button>
    </div>
  </div>

  <!-- Status -->
  <!-- Status -->
  <Card>
    <div class="status-row">
      <span class="status-label">{$t("common.status")}:</span>
      <Badge variant={dhcpConfig.enabled ? "success" : "secondary"}>
        {dhcpConfig.enabled ? $t("common.running") : $t("common.stopped")}
      </Badge>
    </div>
    {#if dhcpConfig.mode && dhcpConfig.mode !== "builtin"}
      <div class="status-row mt-2">
        <span class="status-label">{$t("dhcp.mode")}:</span>
        <Badge variant="outline">{dhcpConfig.mode}</Badge>
      </div>
    {/if}
  </Card>

  <!-- Scopes -->
  <div class="section">
    <div class="section-header">
      <h3>{$t("dhcp.scopes")}</h3>
      <Button variant="outline" size="sm" onclick={openAddScope}
        >+ {$t("common.add_item", {
          values: { item: $t("item.scope") },
        })}</Button
      >
    </div>

    {#if dhcpConfig.scopes?.length > 0}
      <div class="scopes-grid">
        {#each dhcpConfig.scopes as scope, scopeIndex}
          <Card>
            <div class="scope-header">
              <h4>{scope.name}</h4>
              <div class="scope-actions">
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => openEditScope(scopeIndex)}
                  ><Icon name="edit" size="sm" /></Button
                >
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => deleteScope(scopeIndex)}
                  ><Icon name="delete" size="sm" /></Button
                >
              </div>
            </div>
            <div class="scope-details">
              <div class="detail-row">
                <span class="detail-label">{$t("dhcp.interface")}:</span>
                <span class="detail-value">{scope.interface}</span>
              </div>
              <div class="detail-row">
                <span class="detail-label">{$t("dhcp.range")}:</span>
                <span class="detail-value mono"
                  >{scope.range_start} - {scope.range_end}</span
                >
              </div>
              {#if scope.router}
                <div class="detail-row">
                  <span class="detail-label">{$t("dhcp.router")}:</span>
                  <span class="detail-value mono">{scope.router}</span>
                </div>
              {/if}
            </div>
          </Card>
        {/each}
      </div>
    {:else}
      <Card>
        <p class="empty-message">
          {$t("common.no_items", { values: { items: $t("item.scope") } })}
        </p>
      </Card>
    {/if}
  </div>

  <!-- Leases -->
  <div class="section">
    <div class="section-header">
      <h3>
        {$t("dhcp.active_leases", { values: { n: activeLeases.length } })}
      </h3>
    </div>

    <Card>
      <Table
        columns={leaseColumns}
        data={activeLeases}
        emptyMessage="No active DHCP leases"
      />
    </Card>
  </div>
</div>

<!-- Add/Edit Scope Modal -->
<Modal
  bind:open={showScopeModal}
  title={isEditMode
    ? $t("common.edit_item", { values: { item: $t("item.scope") } })
    : $t("common.add_item", { values: { item: $t("item.scope") } })}
>
  <div class="form-stack">
    <Input
      id="scope-name"
      label={$t("dhcp.scope_name")}
      bind:value={scopeName}
      placeholder={$t("dhcp.scope_name_placeholder")}
      required
    />

    <Select
      id="scope-interface"
      label={$t("dhcp.interface")}
      bind:value={scopeInterface}
      options={interfaces.map((i: any) => ({ value: i.Name, label: i.Name }))}
      required
    />

    <Input
      id="scope-start"
      label={$t("dhcp.range_start")}
      bind:value={scopeRangeStart}
      placeholder="192.168.1.100"
      required
    />

    <Input
      id="scope-end"
      label={$t("dhcp.range_end")}
      bind:value={scopeRangeEnd}
      placeholder="192.168.1.200"
      required
    />

    <Input
      id="scope-router"
      label={$t("dhcp.router_optional")}
      bind:value={scopeRouter}
      placeholder="192.168.1.1"
    />

    <div class="grid grid-cols-2 gap-4">
      <Input
        id="scope-lease"
        label={$t("dhcp.lease_time")}
        bind:value={scopeLeaseTime}
        placeholder="24h"
      />
      <Input
        id="scope-domain"
        label={$t("dhcp.domain")}
        bind:value={scopeDomain}
        placeholder="lan"
      />
    </div>

    <Input
      id="scope-dns"
      label={$t("dhcp.dns_servers")}
      bind:value={scopeDns}
      placeholder="1.1.1.1, 8.8.8.8"
    />

    <!-- Reservations -->
    <div class="reservations-section bg-secondary/10 p-4 rounded-lg">
      <h3 class="text-sm font-medium mb-3">{$t("dhcp.static_reservations")}</h3>

      {#if scopeReservations.length > 0}
        <div class="space-y-2 mb-4">
          {#each scopeReservations as res}
            <div
              class="flex items-center justify-between bg-background p-2 rounded border border-border"
            >
              <div class="flex flex-col text-xs">
                <span class="font-mono">{res.mac}</span>
                <span class="text-muted-foreground">{res.ip}</span>
              </div>
              <div class="flex items-center gap-2">
                {#if res.hostname}<span class="text-xs text-muted-foreground"
                    >{res.hostname}</span
                  >{/if}
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => removeReservation(res.mac)}
                >
                  <Icon name="delete" />
                </Button>
              </div>
            </div>
          {/each}
        </div>
      {/if}

      <div class="grid grid-cols-3 gap-2">
        <Input
          placeholder="MAC Address"
          bind:value={newResMac}
          class="text-xs"
        />
        <Input placeholder="IP Address" bind:value={newResIp} class="text-xs" />
        <Input
          placeholder="Hostname"
          bind:value={newResHostname}
          class="text-xs"
        />
      </div>
      <div class="mt-2 flex justify-end">
        <Button
          variant="outline"
          size="sm"
          onclick={addReservation}
          disabled={!newResMac || !newResIp}>{$t("common.add")}</Button
        >
      </div>
    </div>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showScopeModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveScope} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        {$t("common.save_item", { values: { item: $t("item.scope") } })}
      </Button>
    </div>
  </div>
</Modal>

<!-- Settings Modal -->
<Modal bind:open={showSettingsModal} title={$t("dhcp.settings_title")}>
  <div class="form-stack">
    <Select
      id="dhcp-mode"
      label={$t("dhcp.server_mode")}
      options={[
        { value: "builtin", label: $t("dhcp.server_modes.builtin") },
        { value: "external", label: $t("dhcp.server_modes.external") },
        { value: "import", label: $t("dhcp.server_modes.import") },
      ]}
      bind:value={dhcpMode}
    />

    {#if dhcpMode === "import"}
      <Input
        id="lease-file"
        label="External Lease File"
        bind:value={dhcpLeaseFile}
        placeholder="/var/lib/misc/dnsmasq.leases"
      />
    {/if}

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showSettingsModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveSettings} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        {$t("common.save")}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .dhcp-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .status-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .status-label {
    font-weight: 500;
    color: var(--color-foreground);
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .section-header h3 {
    font-size: var(--text-lg);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .scopes-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: var(--space-4);
  }

  .scopes-grid h4 {
    font-size: var(--text-base);
    font-weight: 600;
    margin: 0 0 var(--space-3) 0;
    color: var(--color-foreground);
  }

  .scope-details {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
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

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-4);
    padding-top: var(--space-4);
    border-top: 1px solid var(--color-border);
  }

  .header-actions {
    display: flex;
    gap: var(--space-2);
  }
</style>
