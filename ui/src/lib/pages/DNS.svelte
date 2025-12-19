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
  } from "$lib/components";

  let loading = $state(false);
  let showAddForwarderModal = $state(false);
  let newForwarder = $state("");

  const dnsConfig = $derived(
    $config?.dns ||
      $config?.dns_server || { enabled: false, forwarders: [], listen_on: [] },
  );

  const usingNewFormat = $derived(!!$config?.dns);

  async function toggleDNS() {
    loading = true;
    try {
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
      <Button
        variant="outline"
        size="sm"
        onclick={() => (showAddForwarderModal = true)}
      >
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
                size="sm"
                onclick={() => removeForwarder(forwarder)}
              >
                <Icon name="delete" size="sm" />
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
</style>
