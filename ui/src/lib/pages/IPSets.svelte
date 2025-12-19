<script lang="ts">
  /**
   * IPSets Page
   * Blocklist management
   */

  import { config, api } from "$lib/stores/app";
  import {
    Card,
    Button,
    Modal,
    Input,
    Select,
    Badge,
    Spinner,
  } from "$lib/components";

  let loading = $state(false);
  let refreshingSet = $state<string | null>(null);

  const ipsets = $derived($config?.ipsets || []);

  async function refreshIPSet(name: string) {
    refreshingSet = name;
    try {
      await api.refreshIPSets(name);
    } catch (e) {
      console.error("Failed to refresh IPSet:", e);
    } finally {
      refreshingSet = null;
    }
  }

  async function refreshAllIPSets() {
    loading = true;
    try {
      await api.refreshIPSets();
    } catch (e) {
      console.error("Failed to refresh all IPSets:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="ipsets-page">
  <div class="page-header">
    <h2>Blocklists (IPSets)</h2>
    <Button onclick={refreshAllIPSets} disabled={loading}>
      {#if loading}<Spinner size="sm" />{/if}
      Refresh All
    </Button>
  </div>

  <div class="ipsets-section">
    <h3>Device Groups</h3>
    {#if ipsets.filter((s) => s.name?.startsWith("tag_")).length === 0}
      <Card>
        <p class="empty-message">
          No device tag groups found. Add tags to devices in the Clients page.
        </p>
      </Card>
    {:else}
      <div class="ipsets-grid">
        {#each ipsets.filter((s) => s.name?.startsWith("tag_")) as ipset}
          <Card>
            <div class="ipset-header">
              <h3>{ipset.name.replace("tag_", "")}</h3>
              <Badge variant="success">Tag Group</Badge>
            </div>
            <div class="ipset-details">
              <div class="detail-row">
                <span class="detail-label">Devices:</span>
                <span class="detail-value"
                  >{ipset.entries ? ipset.entries.length : 0}</span
                >
              </div>
              <div class="detail-row">
                <span class="detail-label">IPSet Name:</span>
                <span class="detail-value font-mono">{ipset.name}</span>
              </div>
            </div>
          </Card>
        {/each}
      </div>
    {/if}
  </div>

  <div class="ipsets-section">
    <h3>Blocklists & Custom Sets</h3>
    {#if ipsets.filter((s) => !s.name?.startsWith("tag_")).length === 0}
      <Card>
        <p class="empty-message">No other IPSets configured.</p>
      </Card>
    {:else}
      <div class="ipsets-grid">
        {#each ipsets.filter((s) => !s.name?.startsWith("tag_")) as ipset}
          <Card>
            <div class="ipset-header">
              <h3>{ipset.name}</h3>
              <Badge variant={ipset.auto_update ? "success" : "secondary"}>
                {ipset.auto_update ? "Auto" : "Manual"}
              </Badge>
            </div>

            <div class="ipset-details">
              {#if ipset.firehol_list}
                <div class="detail-row">
                  <span class="detail-label">Source:</span>
                  <span class="detail-value">{ipset.firehol_list}</span>
                </div>
              {/if}

              {#if ipset.entries}
                <div class="detail-row">
                  <span class="detail-label">Entries:</span>
                  <span class="detail-value">{ipset.entries.length} IPs</span>
                </div>
              {/if}

              {#if ipset.refresh_hours}
                <div class="detail-row">
                  <span class="detail-label">Refresh:</span>
                  <span class="detail-value">Every {ipset.refresh_hours}h</span>
                </div>
              {/if}

              <div class="detail-row">
                <span class="detail-label">Action:</span>
                <Badge
                  variant={ipset.action === "drop" ? "destructive" : "warning"}
                >
                  {ipset.action || "drop"}
                </Badge>
              </div>

              <div class="detail-row">
                <span class="detail-label">Apply to:</span>
                <span class="detail-value">{ipset.apply_to || "input"}</span>
              </div>
            </div>

            <div class="ipset-actions">
              <Button
                variant="outline"
                size="sm"
                onclick={() => refreshIPSet(ipset.name)}
                disabled={refreshingSet === ipset.name}
              >
                {#if refreshingSet === ipset.name}
                  <Spinner size="sm" />
                {:else}
                  Refresh Now
                {/if}
              </Button>
            </div>
          </Card>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .ipsets-page {
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

  .ipsets-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: var(--space-4);
  }

  .ipset-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-3);
    padding-bottom: var(--space-3);
    border-bottom: 1px solid var(--color-border);
  }

  .ipset-header h3 {
    font-size: var(--text-base);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .ipset-details {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
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

  .ipset-actions {
    display: flex;
    justify-content: flex-end;
  }

  .empty-message {
    color: var(--color-muted);
    text-align: center;
    margin: 0;
  }
</style>
