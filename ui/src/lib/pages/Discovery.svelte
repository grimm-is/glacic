<script lang="ts">
  /**
   * Discovery Page
   * Shows all discovered network devices
   */

  import { onMount } from "svelte";
  import { Card, Badge, Spinner, Icon } from "$lib/components";

  interface NetworkDevice {
    mac: string;
    ips: string[];
    interface: string;
    first_seen: number;
    last_seen: number;
    hostname?: string;
    vendor?: string;
    alias?: string;
    hop_count: number;
    flags?: string[];
    packet_count: number;
  }

  let loading = $state(true);
  let devices = $state<NetworkDevice[]>([]);
  let error = $state("");

  onMount(async () => {
    await loadDevices();
  });

  async function loadDevices() {
    loading = true;
    try {
      const response = await fetch("/api/network-devices", {
        credentials: "include",
        headers: { "Content-Type": "application/json" },
      });
      if (response.ok) {
        const data = await response.json();
        devices = data.devices || [];
      } else {
        error = "Failed to load devices";
      }
    } catch (e) {
      error = "Connection error";
    } finally {
      loading = false;
    }
  }

  function formatTime(timestamp: number): string {
    if (!timestamp) return "Never";
    const date = new Date(timestamp * 1000);
    return date.toLocaleTimeString();
  }

  function formatTimeAgo(timestamp: number): string {
    if (!timestamp) return "Never";
    const seconds = Math.floor(Date.now() / 1000 - timestamp);
    if (seconds < 60) return `${seconds}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
  }

  // Group devices by interface
  let devicesByInterface = $derived(() => {
    const groups: Record<string, NetworkDevice[]> = {};
    for (const dev of devices) {
      const iface = dev.interface || "unknown";
      if (!groups[iface]) groups[iface] = [];
      groups[iface].push(dev);
    }
    return groups;
  });
</script>

<div class="discovery-page">
  <div class="page-header">
    <h2>Network Discovery</h2>
    <button class="refresh-btn" onclick={loadDevices} disabled={loading}>
      <Icon name="refresh" size="sm" />
      Refresh
    </button>
  </div>

  {#if loading}
    <Card>
      <div class="loading-state">
        <Spinner size="lg" />
        <p>Loading devices...</p>
      </div>
    </Card>
  {:else if error}
    <Card>
      <p class="error-message">{error}</p>
    </Card>
  {:else if devices.length === 0}
    <Card>
      <div class="empty-state">
        <Icon name="devices" size="lg" />
        <p>No devices discovered yet.</p>
        <p class="hint">
          Devices will appear as they send traffic through the firewall.
        </p>
      </div>
    </Card>
  {:else}
    <div class="stats-row">
      <Card>
        <div class="stat">
          <span class="stat-value">{devices.length}</span>
          <span class="stat-label">Devices</span>
        </div>
      </Card>
      <Card>
        <div class="stat">
          <span class="stat-value"
            >{Object.keys(devicesByInterface()).length}</span
          >
          <span class="stat-label">Interfaces</span>
        </div>
      </Card>
      <Card>
        <div class="stat">
          <span class="stat-value"
            >{devices.filter((d) => d.flags?.includes("new")).length}</span
          >
          <span class="stat-label">New</span>
        </div>
      </Card>
    </div>

    <div class="devices-grid">
      {#each devices as device}
        <Card>
          <div class="device-card">
            <div class="device-header">
              <div class="device-icon">
                <Icon name="device" size="md" />
              </div>
              <div class="device-title">
                <h4>{device.alias || device.hostname || device.mac}</h4>
                {#if device.alias || device.hostname}
                  <code class="mac">{device.mac}</code>
                {/if}
              </div>
              {#if device.flags?.includes("new")}
                <Badge variant="success">New</Badge>
              {/if}
            </div>

            <div class="device-details">
              {#if device.ips?.length > 0}
                <div class="detail-row">
                  <span class="label">IP</span>
                  <span class="value mono">{device.ips.join(", ")}</span>
                </div>
              {/if}
              {#if device.vendor}
                <div class="detail-row">
                  <span class="label">Vendor</span>
                  <span class="value">{device.vendor}</span>
                </div>
              {/if}
              <div class="detail-row">
                <span class="label">Interface</span>
                <span class="value">{device.interface}</span>
              </div>
              <div class="detail-row">
                <span class="label">Last seen</span>
                <span class="value">{formatTimeAgo(device.last_seen)}</span>
              </div>
              <div class="detail-row">
                <span class="label">Packets</span>
                <span class="value">{device.packet_count.toLocaleString()}</span
                >
              </div>
              {#if device.hop_count > 0}
                <div class="detail-row">
                  <span class="label">Hops</span>
                  <span class="value">{device.hop_count}</span>
                </div>
              {/if}
            </div>
          </div>
        </Card>
      {/each}
    </div>
  {/if}
</div>

<style>
  .discovery-page {
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

  .refresh-btn {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    color: var(--color-foreground);
    font-size: var(--text-sm);
    cursor: pointer;
    transition: background-color var(--transition-fast);
  }

  .refresh-btn:hover:not(:disabled) {
    background: var(--color-surfaceHover);
  }

  .refresh-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .stats-row {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
    gap: var(--space-4);
  }

  .stat {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: var(--space-2);
  }

  .stat-value {
    font-size: var(--text-2xl);
    font-weight: 700;
    color: var(--color-foreground);
  }

  .stat-label {
    font-size: var(--text-sm);
    color: var(--color-muted);
  }

  .devices-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: var(--space-4);
  }

  .device-card {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .device-header {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
  }

  .device-icon {
    color: var(--color-muted);
  }

  .device-title {
    flex: 1;
    min-width: 0;
  }

  .device-title h4 {
    margin: 0;
    font-size: var(--text-base);
    font-weight: 600;
    color: var(--color-foreground);
    word-break: break-all;
  }

  .mac {
    font-size: var(--text-xs);
    color: var(--color-muted);
  }

  .device-details {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    font-size: var(--text-sm);
  }

  .label {
    color: var(--color-muted);
  }

  .value {
    color: var(--color-foreground);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .loading-state,
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-8);
    text-align: center;
  }

  .loading-state p,
  .empty-state p {
    color: var(--color-muted);
    margin: 0;
  }

  .hint {
    font-size: var(--text-sm);
  }

  .error-message {
    color: var(--color-error);
    text-align: center;
    margin: 0;
  }
</style>
