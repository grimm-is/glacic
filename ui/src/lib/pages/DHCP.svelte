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
  } from "$lib/components";

  let loading = $state(false);
  let showScopeModal = $state(false);
  let editingIndex = $state<number | null>(null);
  let isEditMode = $derived(editingIndex !== null);

  // Scope form
  let scopeName = $state("");
  let scopeInterface = $state("");
  let scopeRangeStart = $state("");
  let scopeRangeEnd = $state("");
  let scopeRouter = $state("");

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

  function openAddScope() {
    editingIndex = null;
    scopeName = "";
    scopeInterface = interfaces[0]?.Name || "";
    scopeRangeStart = "";
    scopeRangeEnd = "";
    scopeRouter = "";
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

  async function deleteScope(index: number) {
    const scope = dhcpConfig.scopes[index];
    if (!confirm(`Delete scope "${scope.name}"?`)) return;

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
    <h2>DHCP Server</h2>
    <div class="header-actions">
      <Button
        variant={dhcpConfig.enabled ? "destructive" : "default"}
        onclick={toggleDHCP}
        disabled={loading}
      >
        {dhcpConfig.enabled ? "Disable DHCP" : "Enable DHCP"}
      </Button>
    </div>
  </div>

  <!-- Status -->
  <Card>
    <div class="status-row">
      <span class="status-label">Status:</span>
      <Badge variant={dhcpConfig.enabled ? "success" : "secondary"}>
        {dhcpConfig.enabled ? "Running" : "Stopped"}
      </Badge>
    </div>
  </Card>

  <!-- Scopes -->
  <div class="section">
    <div class="section-header">
      <h3>Scopes</h3>
      <Button variant="outline" size="sm" onclick={openAddScope}
        >+ Add Scope</Button
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
                <span class="detail-label">Interface:</span>
                <span class="detail-value">{scope.interface}</span>
              </div>
              <div class="detail-row">
                <span class="detail-label">Range:</span>
                <span class="detail-value mono"
                  >{scope.range_start} - {scope.range_end}</span
                >
              </div>
              {#if scope.router}
                <div class="detail-row">
                  <span class="detail-label">Router:</span>
                  <span class="detail-value mono">{scope.router}</span>
                </div>
              {/if}
            </div>
          </Card>
        {/each}
      </div>
    {:else}
      <Card>
        <p class="empty-message">No DHCP scopes configured.</p>
      </Card>
    {/if}
  </div>

  <!-- Leases -->
  <div class="section">
    <div class="section-header">
      <h3>Active Leases ({activeLeases.length})</h3>
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
  title={isEditMode ? "Edit DHCP Scope" : "Add DHCP Scope"}
>
  <div class="form-stack">
    <Input
      id="scope-name"
      label="Scope Name"
      bind:value={scopeName}
      placeholder="e.g., LAN Pool"
      required
    />

    <Select
      id="scope-interface"
      label="Interface"
      bind:value={scopeInterface}
      options={interfaces.map((i: any) => ({ value: i.Name, label: i.Name }))}
      required
    />

    <Input
      id="scope-start"
      label="Range Start"
      bind:value={scopeRangeStart}
      placeholder="192.168.1.100"
      required
    />

    <Input
      id="scope-end"
      label="Range End"
      bind:value={scopeRangeEnd}
      placeholder="192.168.1.200"
      required
    />

    <Input
      id="scope-router"
      label="Router (optional)"
      bind:value={scopeRouter}
      placeholder="192.168.1.1"
    />

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showScopeModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveScope} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        Add Scope
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

  .page-header h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
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
</style>
