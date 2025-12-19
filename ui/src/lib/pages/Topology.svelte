<script lang="ts">
  /**
   * Topology Page
   * Network topology visualization
   */

  import { onMount } from "svelte";
  import { api } from "$lib/stores/app";
  import { Card, Badge, Spinner } from "$lib/components";

  let loading = $state(true);
  let nodes = $state<any[]>([]);
  let error = $state("");

  onMount(async () => {
    try {
      const response = await fetch("/api/topology", {
        credentials: "include",
        headers: { "Content-Type": "application/json" },
      });
      if (response.ok) {
        const data = await response.json();
        nodes = data.neighbors || [];
      } else {
        error = "Failed to load topology";
      }
    } catch (e) {
      error = "Connection error";
    } finally {
      loading = false;
    }
  });

  function getRoleColor(role: string) {
    return "outline"; // Default for now
  }

  function getRoleIcon(role: string) {
    return "📱";
  }
</script>

<div class="topology-page">
  <div class="page-header">
    <h2>Network Topology</h2>
  </div>

  {#if loading}
    <Card>
      <div class="loading-state">
        <Spinner size="lg" />
        <p>Discovering network...</p>
      </div>
    </Card>
  {:else if error}
    <Card>
      <p class="error-message">{error}</p>
    </Card>
  {:else}
    <div class="topology-grid">
      {#each nodes as node}
        <Card>
          <div class="node-card">
            <div class="node-icon">📱</div>
            <div class="node-info">
              <h3>{node.alias || node.system_name || "Unknown Device"}</h3>
              <code class="node-ip">{node.chassis_id}</code>
              {#if node.vendor}
                <div class="node-meta">
                  <Badge variant="outline">{node.vendor}</Badge>
                </div>
              {/if}
              {#if node.interface}
                <span class="client-count">via {node.interface}</span>
              {/if}
            </div>
          </div>
        </Card>
      {/each}
    </div>

    {#if nodes.length === 0}
      <Card>
        <p class="empty-message">No network devices discovered.</p>
      </Card>
    {/if}
  {/if}
</div>

<style>
  .topology-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .page-header h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .topology-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: var(--space-4);
  }

  .node-card {
    display: flex;
    align-items: flex-start;
    gap: var(--space-4);
  }

  .node-icon {
    font-size: 2.5rem;
    line-height: 1;
  }

  .node-info {
    flex: 1;
  }

  .node-info h3 {
    margin: 0 0 var(--space-1) 0;
    font-size: var(--text-lg);
    font-weight: 600;
    color: var(--color-foreground);
  }

  .node-ip {
    display: block;
    font-size: var(--text-sm);
    color: var(--color-muted);
    margin-bottom: var(--space-2);
  }

  .node-meta {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .client-count {
    font-size: var(--text-sm);
    color: var(--color-muted);
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
