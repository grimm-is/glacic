<script lang="ts">
  /**
   * Scanner Page
   * WiFi and network scanner results
   */

  import { onMount } from "svelte";
  import { Card, Badge, Button, Spinner } from "$lib/components";

  let loading = $state(true);
  let scanning = $state(false);
  let results = $state<any[]>([]);
  let error = $state("");

  async function loadResults() {
    try {
      const response = await fetch("/api/scanner/result", {
        credentials: "include",
        headers: { "Content-Type": "application/json" },
      });
      if (response.ok) {
        results = await response.json();
      } else {
        error = "Failed to load scan results";
      }
    } catch (e) {
      error = "Connection error";
    } finally {
      loading = false;
    }
  }

  async function startScan() {
    scanning = true;
    try {
      await fetch("/api/scanner/network", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
      });
      await loadResults();
    } catch (e) {
      // Scan may not be supported
    } finally {
      scanning = false;
    }
  }

  function getSignalStrength(signal: number): {
    label: string;
    variant: string;
  } {
    if (signal > -50) return { label: "Excellent", variant: "success" };
    if (signal > -60) return { label: "Good", variant: "success" };
    if (signal > -70) return { label: "Fair", variant: "warning" };
    return { label: "Weak", variant: "destructive" };
  }

  function getSecurityVariant(security: string) {
    if (security === "Open") return "destructive";
    if (security.includes("WPA3")) return "success";
    if (security.includes("WPA2")) return "default";
    return "secondary";
  }

  onMount(loadResults);
</script>

<div class="scanner-page">
  <div class="page-header">
    <h2>Network Scanner</h2>
    <Button onclick={startScan} disabled={scanning}>
      {#if scanning}<Spinner size="sm" />{/if}
      {scanning ? "Scanning..." : "Scan Now"}
    </Button>
  </div>

  {#if loading}
    <Card>
      <div class="loading-state">
        <Spinner size="lg" />
        <p>Loading results...</p>
      </div>
    </Card>
  {:else if error}
    <Card>
      <p class="error-message">{error}</p>
    </Card>
  {:else if results.length === 0}
    <Card>
      <p class="empty-message">
        No networks found. Click "Scan Now" to discover nearby networks.
      </p>
    </Card>
  {:else}
    <div class="results-grid">
      {#each results.sort((a: any, b: any) => b.signal - a.signal) as network}
        {@const strength = getSignalStrength(network.signal)}
        <Card>
          <div class="network-card">
            <div class="network-header">
              <h3>{network.ssid || "(Hidden)"}</h3>
              <Badge variant={getSecurityVariant(network.security)}
                >{network.security}</Badge
              >
            </div>

            <div class="network-details">
              <div class="detail-row">
                <span class="detail-label">BSSID</span>
                <code class="detail-value">{network.bssid}</code>
              </div>
              <div class="detail-row">
                <span class="detail-label">Channel</span>
                <span class="detail-value">{network.channel}</span>
              </div>
              <div class="detail-row">
                <span class="detail-label">Signal</span>
                <div class="signal-info">
                  <span class="detail-value">{network.signal} dBm</span>
                  <Badge variant={strength.variant}>{strength.label}</Badge>
                </div>
              </div>
            </div>
          </div>
        </Card>
      {/each}
    </div>
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

  .page-header h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .results-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: var(--space-4);
  }

  .network-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-3);
    padding-bottom: var(--space-3);
    border-bottom: 1px solid var(--color-border);
  }

  .network-header h3 {
    margin: 0;
    font-size: var(--text-base);
    font-weight: 600;
    color: var(--color-foreground);
  }

  .network-details {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: var(--text-sm);
  }

  .detail-label {
    color: var(--color-muted);
  }

  .detail-value {
    color: var(--color-foreground);
  }

  .signal-info {
    display: flex;
    align-items: center;
    gap: var(--space-2);
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

  .error-message,
  .empty-message {
    color: var(--color-muted);
    text-align: center;
    margin: 0;
  }
</style>
