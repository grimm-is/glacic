<script lang="ts">
  /**
   * VPN Page
   * WireGuard peer management
   */

  import { config, api } from "$lib/stores/app";
  import { Card, Button, Modal, Input, Badge, Spinner } from "$lib/components";

  let loading = $state(false);

  const vpnConfig = $derived($config?.vpn || { enabled: false, peers: [] });
  const peers = $derived(vpnConfig.peers || []);

  async function toggleVPN() {
    loading = true;
    try {
      await api.updateVPN({
        ...vpnConfig,
        enabled: !vpnConfig.enabled,
      });
    } catch (e) {
      console.error("Failed to toggle VPN:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="vpn-page">
  <div class="page-header">
    <h2>WireGuard VPN</h2>
    <div class="header-actions">
      <Button
        variant={vpnConfig.enabled ? "destructive" : "default"}
        onclick={toggleVPN}
        disabled={loading}
      >
        {vpnConfig.enabled ? "Disable VPN" : "Enable VPN"}
      </Button>
    </div>
  </div>

  <!-- Status -->
  <Card>
    <div class="status-row">
      <span class="status-label">Status:</span>
      <Badge variant={vpnConfig.enabled ? "success" : "secondary"}>
        {vpnConfig.enabled ? "Active" : "Inactive"}
      </Badge>
    </div>
    {#if vpnConfig.interface}
      <div class="status-row" style="margin-top: var(--space-2)">
        <span class="status-label">Interface:</span>
        <span class="mono">{vpnConfig.interface}</span>
      </div>
    {/if}
    {#if vpnConfig.listen_port}
      <div class="status-row" style="margin-top: var(--space-2)">
        <span class="status-label">Listen Port:</span>
        <span class="mono">{vpnConfig.listen_port}</span>
      </div>
    {/if}
  </Card>

  <!-- Peers -->
  <div class="section">
    <div class="section-header">
      <h3>Peers ({peers.length})</h3>
    </div>

    {#if peers.length === 0}
      <Card>
        <p class="empty-message">No WireGuard peers configured.</p>
      </Card>
    {:else}
      <div class="peers-grid">
        {#each peers as peer}
          <Card>
            <div class="peer-header">
              <h4>{peer.name || "Unnamed Peer"}</h4>
              <Badge variant={peer.handshake ? "success" : "secondary"}>
                {peer.handshake ? "Connected" : "Offline"}
              </Badge>
            </div>

            <div class="peer-details">
              <div class="detail-row">
                <span class="detail-label">Public Key:</span>
                <code class="detail-value"
                  >{peer.public_key?.substring(0, 12)}...</code
                >
              </div>

              {#if peer.endpoint}
                <div class="detail-row">
                  <span class="detail-label">Endpoint:</span>
                  <code class="detail-value">{peer.endpoint}</code>
                </div>
              {/if}

              {#if peer.allowed_ips}
                <div class="detail-row">
                  <span class="detail-label">Allowed IPs:</span>
                  <code class="detail-value">{peer.allowed_ips}</code>
                </div>
              {/if}

              {#if peer.handshake}
                <div class="detail-row">
                  <span class="detail-label">Last Handshake:</span>
                  <span class="detail-value">{peer.handshake}</span>
                </div>
              {/if}
            </div>
          </Card>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .vpn-page {
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

  .peers-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: var(--space-4);
  }

  .peer-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-3);
    padding-bottom: var(--space-3);
    border-bottom: 1px solid var(--color-border);
  }

  .peer-header h4 {
    font-size: var(--text-base);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .peer-details {
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
    font-family: var(--font-mono);
    font-size: var(--text-xs);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .empty-message {
    color: var(--color-muted);
    text-align: center;
    margin: 0;
  }
</style>
