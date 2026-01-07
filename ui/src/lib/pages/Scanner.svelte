<script lang="ts">
  /**
   * Scanner Page
   * Network port scanner - discovers services on local network hosts
   */

  import { onMount } from "svelte";
  import { Card, Badge, Button, Spinner, Input } from "$lib/components";
  import { t } from "svelte-i18n";

  interface PortResult {
    port: number;
    name: string;
    description: string;
    banner?: string;
  }

  interface HostResult {
    ip: string;
    hostname?: string;
    mac?: string;
    open_ports: PortResult[];
    scan_duration_ms: number;
  }

  interface ScanResult {
    network: string;
    hosts: HostResult[];
    total_hosts: number;
    duration_ms: number;
    started_at: string;
    error?: string;
  }

  let loading = $state(true);
  let scanning = $state(false);
  let scanResult = $state<ScanResult | null>(null);
  let error = $state("");
  let targetCidr = $state("");

  async function loadResults() {
    try {
      const response = await fetch("/api/scanner/result", {
        credentials: "include",
        headers: { "Content-Type": "application/json" },
      });
      if (response.ok) {
        const data = await response.json();
        if (data.hosts) {
          scanResult = data;
          error = "";
        } else if (data.message) {
          scanResult = null;
          error = "";
        } else {
          scanResult = null;
          error = "";
        }
      } else if (response.status === 403) {
        error = $t("scanner.insufficient_permissions");
      } else {
        error = $t("scanner.failed_load_results");
      }
    } catch (e) {
      error = $t("scanner.connection_error");
    } finally {
      loading = false;
    }
  }

  async function startScan() {
    if (!targetCidr) {
      error = $t("scanner.enter_cidr");
      return;
    }

    scanning = true;
    error = "";
    try {
      const res = await fetch("/api/scanner/network", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cidr: targetCidr }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        error =
          data.error ||
          $t("scanner.scan_failed", { values: { status: res.statusText } });
        return;
      }
      // Poll for results
      await pollForResults();
    } catch (e: any) {
      error = e.message || $t("scanner.scan_failed_generic");
    } finally {
      scanning = false;
    }
  }

  async function pollForResults() {
    // Give the scan a moment to start
    await new Promise((r) => setTimeout(r, 1000));

    for (let i = 0; i < 60; i++) {
      const statusRes = await fetch("/api/scanner/status", {
        credentials: "include",
      });
      if (statusRes.ok) {
        const status = await statusRes.json();
        if (!status.scanning) {
          await loadResults();
          return;
        }
      }
      await new Promise((r) => setTimeout(r, 2000));
    }
    error = $t("scanner.scan_timed_out");
  }

  function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  onMount(loadResults);
</script>

<div class="scanner-page">
  <div class="page-header">
    <div class="scan-controls">
      <Input
        type="text"
        placeholder={$t("scanner.target_placeholder")}
        bind:value={targetCidr}
        disabled={scanning}
      />
      <Button onclick={startScan} disabled={scanning}>
        {#if scanning}<Spinner size="sm" />{/if}
        {scanning ? $t("scanner.scanning") : $t("scanner.scan_network")}
      </Button>
    </div>
  </div>

  {#if loading}
    <Card>
      <div class="loading-state">
        <Spinner size="lg" />
        <p>{$t("scanner.loading_results")}</p>
      </div>
    </Card>
  {:else if error}
    <Card>
      <p class="error-message">{error}</p>
    </Card>
  {:else if !scanResult}
    <Card>
      <div class="empty-state">
        <span class="empty-icon">🔍</span>
        <h3>
          {$t("common.no_items", {
            values: { items: $t("item.result") },
          })}
        </h3>
        <p>
          {$t("scanner.empty_state_desc")}
        </p>
      </div>
    </Card>
  {:else}
    <div class="scan-summary">
      <Card>
        <div class="summary-content">
          <div class="summary-item">
            <span class="summary-label">{$t("scanner.network")}</span>
            <code class="summary-value">{scanResult.network}</code>
          </div>
          <div class="summary-item">
            <span class="summary-label">{$t("scanner.hosts_scanned")}</span>
            <span class="summary-value">{scanResult.total_hosts}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label"
              >{$t("scanner.hosts_with_services")}</span
            >
            <span class="summary-value">{scanResult.hosts.length}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">{$t("scanner.duration")}</span>
            <span class="summary-value"
              >{formatDuration(scanResult.duration_ms)}</span
            >
          </div>
        </div>
      </Card>
    </div>

    {#if scanResult.hosts.length === 0}
      <Card>
        <div class="empty-state">
          <span class="empty-icon">✅</span>
          <h3>{$t("scanner.no_open_ports")}</h3>
          <p>
            {$t("scanner.no_open_ports_desc", {
              values: { n: scanResult.total_hosts },
            })}
          </p>
        </div>
      </Card>
    {:else}
      <div class="results-grid">
        {#each scanResult.hosts as host}
          <Card>
            <div class="host-card">
              <div class="host-header">
                <h3>{host.hostname || host.ip}</h3>
                {#if host.hostname}
                  <code class="host-ip">{host.ip}</code>
                {/if}
              </div>

              {#if host.mac}
                <div class="host-mac">
                  <span class="detail-label">{$t("scanner.mac_label")}</span>
                  <code>{host.mac}</code>
                </div>
              {/if}

              <div class="ports-list">
                {#each host.open_ports as port}
                  <div class="port-item">
                    <div class="port-info">
                      <Badge variant="default">{port.port}</Badge>
                      <span class="port-name">{port.name}</span>
                    </div>
                    <span class="port-desc">{port.description}</span>
                    {#if port.banner}
                      <code class="port-banner">{port.banner}</code>
                    {/if}
                  </div>
                {/each}
              </div>
            </div>
          </Card>
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  .scanner-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .scan-controls {
    display: flex;
    gap: var(--space-3);
    flex: 1;
    max-width: 500px;
  }

  .scan-summary {
    margin-bottom: var(--space-2);
  }

  .summary-content {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-6);
  }

  .summary-item {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .summary-label {
    font-size: var(--text-xs);
    color: var(--color-muted);
    text-transform: uppercase;
  }

  .summary-value {
    font-size: var(--text-base);
    font-weight: 600;
    color: var(--color-foreground);
  }

  .results-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
    gap: var(--space-4);
  }

  .host-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-3);
    padding-bottom: var(--space-3);
    border-bottom: 1px solid var(--color-border);
  }

  .host-header h3 {
    margin: 0;
    font-size: var(--text-base);
    font-weight: 600;
    color: var(--color-foreground);
  }

  .host-ip {
    font-size: var(--text-xs);
    color: var(--color-muted);
  }

  .host-mac {
    font-size: var(--text-sm);
    color: var(--color-muted);
    margin-bottom: var(--space-3);
  }

  .ports-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .port-item {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2);
    background: var(--color-background);
    border-radius: var(--radius-sm);
  }

  .port-info {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .port-name {
    font-weight: 500;
    color: var(--color-foreground);
  }

  .port-desc {
    font-size: var(--text-xs);
    color: var(--color-muted);
  }

  .port-banner {
    font-size: var(--text-xs);
    color: var(--color-muted);
    background: var(--color-muted-background);
    padding: var(--space-1);
    border-radius: var(--radius-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .loading-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-6);
  }

  .loading-state p {
    color: var(--color-muted);
    margin: 0;
  }

  .error-message {
    color: var(--color-destructive);
    text-align: center;
    margin: 0;
  }

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-8);
    text-align: center;
  }

  .empty-state .empty-icon {
    font-size: 3rem;
  }

  .empty-state h3 {
    margin: 0;
    font-size: var(--text-lg);
    font-weight: 600;
    color: var(--color-foreground);
  }

  .empty-state p {
    margin: 0;
    max-width: 400px;
    font-size: var(--text-sm);
    color: var(--color-muted);
  }
</style>
