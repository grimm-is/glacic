<script lang="ts">
  /**
   * Dashboard Page
   * System overview and quick actions
   */

  import { config, status, leases, brand, api } from "$lib/stores/app";
  import { Card, Button, Badge, Spinner, Modal } from "$lib/components";

  let showRebootModal = $state(false);
  let rebooting = $state(false);

  const zoneCount = $derived($config?.zones?.length || 0);
  const interfaceCount = $derived($config?.interfaces?.length || 0);
  const activeLeases = $derived(
    $leases?.filter((l: any) => l.active).length || 0,
  );
  const totalLeases = $derived($leases?.length || 0);
  const isForwarding = $derived($config?.ip_forwarding ?? false);

  async function handleReboot() {
    rebooting = true;
    try {
      await api.reboot();
      // System will reboot, connection will be lost
    } catch (e) {
      console.error("Reboot failed:", e);
      rebooting = false;
    }
  }

  async function toggleForwarding() {
    try {
      await api.setIPForwarding(!isForwarding);
      await api.reloadConfig();
    } catch (e) {
      console.error("Failed to toggle forwarding:", e);
    }
  }
</script>

<div class="dashboard">
  <h2>Welcome to {$brand?.name}</h2>

  <!-- Status Cards -->
  <div class="stats-grid">
    <Card>
      <h3>System Status</h3>
      <div class="stat-row">
        <Badge variant={isForwarding ? "success" : "destructive"}>
          {isForwarding ? "Online" : "Offline"}
        </Badge>
        <span class="stat-label">{$status?.hostname || "Unknown"}</span>
      </div>
      <p class="stat-meta">{$status?.uptime || "Loading..."}</p>
    </Card>

    <Card>
      <h3>Network</h3>
      <p class="stat-value">{interfaceCount} interfaces</p>
      <p class="stat-meta">{zoneCount} zones configured</p>
    </Card>

    <Card>
      <h3>Clients</h3>
      <p class="stat-value">{activeLeases} active</p>
      <p class="stat-meta">{totalLeases} total DHCP leases</p>
    </Card>

    <Card>
      <h3>Protection</h3>
      <Badge variant={$config?.protection?.enabled ? "success" : "secondary"}>
        {$config?.protection?.enabled ? "Active" : "Disabled"}
      </Badge>
      <p class="stat-meta">
        {#if $config?.protection?.anti_spoofing}Anti-spoofing enabled{/if}
      </p>
    </Card>
  </div>

  <!-- Quick Actions -->
  <div class="quick-actions">
    <h3>Quick Actions</h3>
    <div class="action-buttons">
      <Button variant="outline" onclick={toggleForwarding}>
        {isForwarding ? "Disable" : "Enable"} Forwarding
      </Button>
      <Button variant="destructive" onclick={() => (showRebootModal = true)}>
        Reboot System
      </Button>
    </div>
  </div>
</div>

<!-- Reboot Confirmation -->
<Modal bind:open={showRebootModal} title="Confirm Reboot">
  <p>
    Are you sure you want to reboot the system? All active connections will be
    dropped.
  </p>
  <div class="modal-actions">
    <Button variant="ghost" onclick={() => (showRebootModal = false)}
      >Cancel</Button
    >
    <Button variant="destructive" onclick={handleReboot} disabled={rebooting}>
      {#if rebooting}<Spinner size="sm" />{/if}
      Reboot Now
    </Button>
  </div>
</Modal>

<style>
  .dashboard {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .dashboard h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: var(--space-4);
  }

  h3 {
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--color-muted);
    margin: 0 0 var(--space-2) 0;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .stat-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .stat-label {
    font-size: var(--text-base);
    color: var(--color-foreground);
  }

  .stat-value {
    font-size: var(--text-xl);
    font-weight: 600;
    color: var(--color-foreground);
    margin: 0;
  }

  .stat-meta {
    font-size: var(--text-sm);
    color: var(--color-muted);
    margin: var(--space-1) 0 0 0;
  }

  .quick-actions {
    padding: var(--space-6);
    background-color: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
  }

  .action-buttons {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-4);
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-6);
  }
</style>
