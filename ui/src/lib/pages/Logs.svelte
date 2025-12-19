<script lang="ts">
  /**
   * Logs Page
   * Live streaming logs viewer
   */

  import { onMount, onDestroy } from "svelte";
  import { Card, Button, Select, Badge, Toggle } from "$lib/components";

  let logs = $state<
    Array<{
      timestamp: string;
      level: string;
      message: string;
      source?: string;
    }>
  >([]);
  let autoScroll = $state(true);
  let levelFilter = $state("all");
  let logContainer = $state<HTMLDivElement | null>(null);
  let pollInterval: ReturnType<typeof setInterval> | null = null;

  const filteredLogs = $derived(
    levelFilter === "all" ? logs : logs.filter((l) => l.level === levelFilter),
  );

  function addMockLog() {
    const levels = ["info", "warn", "error", "debug"];
    const sources = ["firewall", "dhcp", "dns", "api", "system"];
    const messages = [
      "Rule matched: LAN -> WAN accept",
      "DHCP lease renewed for 192.168.1.100",
      "DNS query resolved: example.com",
      "API request: GET /api/status",
      "Configuration applied successfully",
      "Blocked connection from 10.0.0.5",
      "New client connected: aa:bb:cc:dd:ee:ff",
      "IPSet firehol_level1 refreshed (15234 entries)",
    ];

    logs = [
      ...logs.slice(-500),
      {
        timestamp: new Date().toISOString().split("T")[1].split(".")[0],
        level: levels[Math.floor(Math.random() * levels.length)],
        source: sources[Math.floor(Math.random() * sources.length)],
        message: messages[Math.floor(Math.random() * messages.length)],
      },
    ];
  }

  function scrollToBottom() {
    if (autoScroll && logContainer) {
      logContainer.scrollTop = logContainer.scrollHeight;
    }
  }

  function clearLogs() {
    logs = [];
  }

  function getLevelColor(level: string) {
    switch (level) {
      case "error":
        return "destructive";
      case "warn":
        return "warning";
      case "info":
        return "default";
      case "debug":
        return "secondary";
      default:
        return "outline";
    }
  }

  onMount(() => {
    // Simulate log streaming with mock data
    pollInterval = setInterval(() => {
      addMockLog();
      scrollToBottom();
    }, 2000);

    // Add some initial logs
    for (let i = 0; i < 5; i++) {
      addMockLog();
    }
  });

  onDestroy(() => {
    if (pollInterval) {
      clearInterval(pollInterval);
    }
  });

  $effect(() => {
    filteredLogs;
    scrollToBottom();
  });
</script>

<div class="logs-page">
  <div class="page-header">
    <h2>System Logs</h2>
    <div class="header-controls">
      <Select
        id="level-filter"
        bind:value={levelFilter}
        options={[
          { value: "all", label: "All Levels" },
          { value: "error", label: "Errors" },
          { value: "warn", label: "Warnings" },
          { value: "info", label: "Info" },
          { value: "debug", label: "Debug" },
        ]}
      />
      <Toggle label="Auto-scroll" bind:checked={autoScroll} />
      <Button variant="ghost" size="sm" onclick={clearLogs}>Clear</Button>
    </div>
  </div>

  <Card>
    <div class="log-viewer" bind:this={logContainer}>
      {#if filteredLogs.length === 0}
        <p class="empty-message">No logs to display</p>
      {:else}
        {#each filteredLogs as log}
          <div
            class="log-entry"
            class:error={log.level === "error"}
            class:warn={log.level === "warn"}
          >
            <span class="log-time">{log.timestamp}</span>
            <Badge variant={getLevelColor(log.level)}>{log.level}</Badge>
            {#if log.source}
              <span class="log-source">[{log.source}]</span>
            {/if}
            <span class="log-message">{log.message}</span>
          </div>
        {/each}
      {/if}
    </div>
  </Card>
</div>

<style>
  .logs-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    height: 100%;
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--space-4);
  }

  .page-header h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .header-controls {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .log-viewer {
    font-family: var(--font-mono);
    font-size: var(--text-sm);
    max-height: 500px;
    overflow-y: auto;
    background-color: var(--color-backgroundSecondary);
    border-radius: var(--radius-md);
    padding: var(--space-2);
  }

  .log-entry {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
  }

  .log-entry.error {
    background-color: rgba(220, 38, 38, 0.1);
  }

  .log-entry.warn {
    background-color: rgba(234, 179, 8, 0.1);
  }

  .log-time {
    color: var(--color-muted);
    flex-shrink: 0;
  }

  .log-source {
    color: var(--color-primary);
    flex-shrink: 0;
  }

  .log-message {
    color: var(--color-foreground);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .empty-message {
    color: var(--color-muted);
    text-align: center;
    padding: var(--space-6);
    margin: 0;
  }
</style>
