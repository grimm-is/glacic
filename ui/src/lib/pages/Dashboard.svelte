<script lang="ts">
  /**
   * Dashboard Page
   * System overview and quick actions
   */

  import { config, status, leases, brand, api } from "$lib/stores/app";
  import { t } from "svelte-i18n";
  import { Card, Button, Badge, Spinner, Modal, Icon } from "$lib/components";
  import Sparkline from "$lib/components/Sparkline.svelte";

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
  <h2>{$t("dashboard.welcome", { values: { name: $brand?.name } })}</h2>

  <!-- Status Cards -->
  <div class="stats-grid">
    <Card>
      <h3>{$t("dashboard.system_status")}</h3>
      <div class="stat-row">
        <Badge variant={isForwarding ? "success" : "destructive"}>
          {isForwarding ? $t("dashboard.online") : $t("dashboard.offline")}
        </Badge>
        <span class="stat-label"
          >{$status?.hostname || $t("dashboard.unknown")}</span
        >
      </div>
      <p class="stat-meta">{$status?.uptime || $t("common.loading")}</p>
    </Card>

    <Card>
      <h3>{$t("dashboard.network")}</h3>
      <p class="stat-value">
        {$t("dashboard.interfaces", { values: { n: interfaceCount } })}
      </p>
      <p class="stat-meta">
        {$t("dashboard.zones_configured", { values: { n: zoneCount } })}
      </p>
    </Card>

    <Card>
      <h3>{$t("dashboard.clients")}</h3>
      <p class="stat-value">
        {$t("dashboard.active", { values: { n: activeLeases } })}
      </p>
      <p class="stat-meta">
        {$t("dashboard.total_leases", { values: { n: totalLeases } })}
      </p>
    </Card>

    <Card>
      <h3>{$t("dashboard.protection")}</h3>
      <Badge variant={$config?.protection?.enabled ? "success" : "secondary"}>
        {$config?.protection?.enabled
          ? $t("common.enabled")
          : $t("common.disabled")}
      </Badge>
      <p class="stat-meta">
        {#if $config?.protection?.anti_spoofing}{$t(
            "dashboard.anti_spoofing",
          )}{/if}
      </p>
    </Card>
  </div>

  <!-- Quick Actions -->
  <div class="quick-actions">
    <h3>{$t("dashboard.quick_actions")}</h3>
    <div class="action-buttons">
      <Button variant="outline" onclick={toggleForwarding}>
        {isForwarding
          ? $t("common.disable_item", {
              values: { item: $t("item.forwarding") },
            })
          : $t("common.enable_item", {
              values: { item: $t("item.forwarding") },
            })}
      </Button>
      <Button variant="destructive" onclick={() => (showRebootModal = true)}>
        {$t("dashboard.reboot_system")}
      </Button>
    </div>
  </div>
</div>

<!-- Reboot Confirmation -->
<Modal bind:open={showRebootModal} title={$t("dashboard.confirm_reboot")}>
  <p>
    {$t("dashboard.reboot_warning")}
  </p>
  <div class="modal-actions">
    <Button variant="ghost" onclick={() => (showRebootModal = false)}
      >{$t("common.cancel")}</Button
    >
    <Button variant="destructive" onclick={handleReboot} disabled={rebooting}>
      {#if rebooting}<Spinner size="sm" />{/if}
      {$t("dashboard.reboot_now")}
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
