<script lang="ts">
  /**
   * DNS Page
   * DNS server settings and upstream configuration
   */

  import { config, api } from "$lib/stores/app";
  import {
    Card,
    Button,
    Modal,
    Input,
    Badge,
    Spinner,
    Icon,
    Toggle,
  } from "$lib/components";

  let loading = $state(false);
  let showAddForwarderModal = $state(false);
  let showServeModal = $state(false);
  let newForwarder = $state("");
  let editingServe = $state<any>(null);

  // Serve Config
  let serveZone = $state("");
  let serveLocalDomain = $state("");
  let serveExpandHosts = $state(false);
  let serveDhcp = $state(false);
  let serveCache = $state(false);
  let serveCacheSize = $state("10000");
  let serveLogging = $state(false);

  const dnsConfig = $derived(
    $config?.dns ||
      $config?.dns_server || { enabled: false, forwarders: [], listen_on: [] },
  );

  const usingNewFormat = $derived(!!$config?.dns);

  async function toggleDNS() {
    loading = true;
    try {
      // Logic depends on legacy vs new.
      // For new format, often presence implies enabled, or we toggle specific services.
      // But preserving existing logic for now if it works.
      const field = usingNewFormat ? "dns" : "dns_server";
      await api.updateDNS({
        [field]: {
          ...dnsConfig,
          enabled: !dnsConfig.enabled,
        },
      });
    } catch (e) {
      console.error("Failed to toggle DNS:", e);
    } finally {
      loading = false;
    }
  }

  function openAddServe() {
    editingServe = null;
    serveZone = "lan";
    serveLocalDomain = "lan";
    serveExpandHosts = true;
    serveDhcp = true;
    serveCache = true;
    serveCacheSize = 10000;
    serveLogging = false;
    showServeModal = true;
  }

  function editServe(serve: any) {
    editingServe = serve;
    serveZone = serve.zone;
    serveLocalDomain = serve.local_domain || "";
    serveExpandHosts = serve.expand_hosts || false;
    serveDhcp = serve.dhcp_integration || false;
    serveCache = serve.cache_enabled || false;
    serveCacheSize = serve.cache_size || 10000;
    serveLogging = serve.query_logging || false;
    showServeModal = true;
  }

  async function saveServe() {
    if (!serveZone) return;

    loading = true;
    try {
      const serveData = {
        zone: serveZone,
        local_domain: serveLocalDomain,
        expand_hosts: serveExpandHosts,
        dhcp_integration: serveDhcp,
        cache_enabled: serveCache,
        cache_size: Number(serveCacheSize),
        query_logging: serveLogging,
      };

      let updatedServe: any[];
      const currentServes = dnsConfig.serve || [];

      if (editingServe) {
        updatedServe = currentServes.map((s: any) =>
          s.zone === editingServe.zone ? { ...s, ...serveData } : s,
        );
      } else {
        updatedServe = [...currentServes, serveData];
      }

      await api.updateDNS({
        dns: {
          ...dnsConfig,
          serve: updatedServe,
        },
      });
      showServeModal = false;
    } catch (e) {
      console.error("Failed to save serve config:", e);
    } finally {
      loading = false;
    }
  }

  async function deleteServe(zoneName: string) {
    if (!confirm(`Delete DNS serving for zone ${zoneName}?`)) return;

    loading = true;
    try {
      const currentServes = dnsConfig.serve || [];
      const updatedServe = currentServes.filter(
        (s: any) => s.zone !== zoneName,
      );

      await api.updateDNS({
        dns: {
          ...dnsConfig,
          serve: updatedServe,
        },
      });
    } catch (e) {
      console.error("Failed to delete serve config:", e);
    } finally {
      loading = false;
    }
  }

  async function addForwarder() {
    if (!newForwarder) return;

    loading = true;
    try {
      const field = usingNewFormat ? "dns" : "dns_server";
      await api.updateDNS({
        [field]: {
          ...dnsConfig,
          forwarders: [...(dnsConfig.forwarders || []), newForwarder],
        },
      });
      showAddForwarderModal = false;
      newForwarder = "";
    } catch (e) {
      console.error("Failed to add forwarder:", e);
    } finally {
      loading = false;
    }
  }

  async function removeForwarder(ip: string) {
    loading = true;
    try {
      const field = usingNewFormat ? "dns" : "dns_server";
      await api.updateDNS({
        [field]: {
          ...dnsConfig,
          forwarders: dnsConfig.forwarders.filter((f: string) => f !== ip),
        },
      });
    } catch (e) {
      console.error("Failed to remove forwarder:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="dns-page">
  <div class="page-header">
    <h2>DNS Server</h2>
    <div class="header-actions">
      <Button
        variant={dnsConfig.enabled ? "destructive" : "default"}
        onclick={toggleDNS}
        disabled={loading}
      >
        {dnsConfig.enabled ? "Disable DNS" : "Enable DNS"}
      </Button>
    </div>
  </div>

  <!-- Status -->
  <Card>
    <div class="status-row">
      <span class="status-label">Status:</span>
      <Badge variant={dnsConfig.enabled ? "success" : "secondary"}>
        {dnsConfig.enabled ? "Running" : "Stopped"}
      </Badge>
    </div>
    {#if usingNewFormat}
      {#each dnsConfig.serve || [] as serve}
        {#if serve.listen_on?.length > 0}
          <div class="status-row" style="margin-top: var(--space-2)">
            <span class="status-label">Listening on ({serve.zone}):</span>
            <span class="mono">{serve.listen_on.join(", ")}</span>
          </div>
        {/if}
      {/each}
    {:else if dnsConfig.listen_on?.length > 0}
      <div class="status-row" style="margin-top: var(--space-2)">
        <span class="status-label">Listening on:</span>
        <span class="mono">{dnsConfig.listen_on.join(", ")}</span>
      </div>
    {/if}
  </Card>

  <!-- Forwarders -->
  <div class="section">
    <div class="section-header">
      <h3>Upstream Forwarders</h3>
      <Button variant="outline" onclick={() => (showAddForwarderModal = true)}>
        + Add Forwarder
      </Button>
    </div>

    {#if dnsConfig.forwarders?.length > 0}
      <div class="forwarders-list">
        {#each dnsConfig.forwarders as forwarder}
          <Card>
            <div class="forwarder-item">
              <span class="forwarder-ip mono">{forwarder}</span>
              <Button
                variant="ghost"
                onclick={() => removeForwarder(forwarder)}
              >
                <Icon name="delete" />
              </Button>
            </div>
          </Card>
        {/each}
      </div>
    {:else}
      <Card>
        <p class="empty-message">No upstream forwarders configured.</p>
      </Card>
    {/if}
  </div>

  <!-- Zone Serving (New Format) -->
  {#if usingNewFormat}
    <div class="section">
      <div class="section-header">
        <h3>Zone Serving</h3>
        <Button variant="outline" onclick={openAddServe}>
          + Add Zone Config
        </Button>
      </div>

      {#if dnsConfig.serve?.length > 0}
        <div class="serve-list">
          {#each dnsConfig.serve as serve}
            <Card>
              <div class="serve-item">
                <div class="serve-info">
                  <span class="zone-badge">{serve.zone}</span>
                  <div class="serve-details">
                    {#if serve.local_domain}
                      <Badge variant="outline"
                        >Domain: {serve.local_domain}</Badge
                      >
                    {/if}
                    {#if serve.cache_enabled}
                      <Badge variant="secondary"
                        >Cache: {serve.cache_size}</Badge
                      >
                    {/if}
                    {#if serve.dhcp_integration}
                      <Badge variant="secondary">DHCP Linked</Badge>
                    {/if}
                  </div>
                </div>
                <div class="serve-actions">
                  <Button variant="ghost" onclick={() => editServe(serve)}>
                    <Icon name="edit" />
                  </Button>
                  <Button
                    variant="ghost"
                    onclick={() => deleteServe(serve.zone)}
                  >
                    <Icon name="delete" />
                  </Button>
                </div>
              </div>
            </Card>
          {/each}
        </div>
      {:else}
        <Card>
          <p class="empty-message">No zone serving configured.</p>
        </Card>
      {/if}
    </div>
  {/if}

  <!-- DNS Inspection (Only shown if using new format) -->
  {#if usingNewFormat && dnsConfig.inspect?.length > 0}
    <div class="section">
      <div class="section-header">
        <h3>DNS Interception / Inspection</h3>
      </div>
      <div class="inspect-list">
        {#each dnsConfig.inspect as inspect}
          <Card>
            <div class="inspect-item">
              <div class="inspect-info">
                <span class="zone-name"
                  >Zone: <strong>{inspect.zone}</strong></span
                >
                <Badge
                  variant={inspect.mode === "redirect"
                    ? "warning"
                    : "secondary"}
                >
                  {inspect.mode === "redirect"
                    ? "Transparent Redirect"
                    : "Passive Inspection"}
                </Badge>
              </div>
              {#if inspect.exclude_router}
                <span class="exclude-router-tag">Excluding Router IP</span>
              {/if}
            </div>
          </Card>
        {/each}
      </div>
    </div>
  {/if}
</div>

<!-- Add Forwarder Modal -->
<Modal bind:open={showAddForwarderModal} title="Add Upstream Forwarder">
  <div class="form-stack">
    <Input
      id="forwarder-ip"
      label="DNS Server IP"
      bind:value={newForwarder}
      placeholder="e.g., 1.1.1.1 or 8.8.8.8"
      required
    />

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showAddForwarderModal = false)}
        >Cancel</Button
      >
      <Button onclick={addForwarder} disabled={loading || !newForwarder}>
        {#if loading}<Spinner size="sm" />{/if}
        Add Forwarder
      </Button>
    </div>
  </div>
</Modal>

<!-- Add/Edit Serve Modal -->
<Modal
  bind:open={showServeModal}
  title={editingServe ? "Edit Zone Config" : "Add Zone Config"}
>
  <div class="form-stack">
    <div class="grid grid-cols-2 gap-4">
      <Input
        id="serve-zone"
        label="Zone Name"
        bind:value={serveZone}
        placeholder="e.g., lan / internal-*"
        required
        disabled={!!editingServe}
      />
      <Input
        id="serve-domain"
        label="Local Domain"
        bind:value={serveLocalDomain}
        placeholder="e.g., lan"
      />
    </div>

    <div class="p-4 bg-secondary/10 rounded-lg space-y-4">
      <h3 class="text-sm font-medium text-foreground">Integration</h3>
      <Toggle label="DHCP Integration" bind:checked={serveDhcp} />
      <p class="text-xs text-muted-foreground pb-2">
        Resolve hostnames from DHCP leases
      </p>

      <Toggle label="Expand Hosts" bind:checked={serveExpandHosts} />
      <p class="text-xs text-muted-foreground">Expand /etc/hosts entries</p>
    </div>

    <div class="p-4 bg-secondary/10 rounded-lg space-y-4">
      <div class="flex items-center justify-between">
        <h3 class="text-sm font-medium text-foreground">Caching</h3>
        <Toggle label="" bind:checked={serveCache} />
      </div>

      {#if serveCache}
        <Input
          id="serve-cache-size"
          label="Cache Size"
          type="number"
          bind:value={serveCacheSize}
        />
      {/if}
    </div>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showServeModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveServe} disabled={loading || !serveZone}>
        {#if loading}<Spinner size="sm" />{/if}
        {editingServe ? "Save Changes" : "Add Config"}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .dns-page {
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

  .forwarders-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .forwarder-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .forwarder-ip {
    color: var(--color-foreground);
  }

  .inspect-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .inspect-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .inspect-info {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }

  .zone-name {
    color: var(--color-foreground);
  }

  .exclude-router-tag {
    font-size: var(--text-xs);
    background: var(--color-surface-hover);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
    color: var(--color-muted);
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
  .serve-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .serve-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .serve-info {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }

  .zone-badge {
    background-color: var(--color-primary);
    color: white;
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-md);
    font-weight: 600;
    font-size: var(--text-sm);
  }

  .serve-details {
    display: flex;
    gap: var(--space-2);
  }

  .serve-actions {
    display: flex;
    gap: var(--space-1);
  }
</style>
