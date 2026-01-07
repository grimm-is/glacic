<script lang="ts">
  /**
   * Logs Page
   * Live streaming logs viewer
   */

  import { onMount, tick } from "svelte";
  import { logs } from "$lib/stores/app";
  import { Card, Button, Select, Badge, Toggle } from "$lib/components";
  import { t } from "svelte-i18n";

  let autoScroll = $state(true);
  let levelFilter = $state("all");
  let sourceFilter = $state("all");
  let logContainer = $state<HTMLDivElement | null>(null);

  const LOG_SOURCES = [
    { value: "all", label: $t("logs.all_sources") },
    { value: "syslog", label: $t("logs.sources.syslog") },
    { value: "nftables", label: $t("logs.sources.nftables") },
    { value: "dhcp", label: $t("logs.sources.dhcp") },
    { value: "dns", label: $t("logs.sources.dns") },
    { value: "dmesg", label: $t("logs.sources.kernel") },
    { value: "api", label: $t("logs.sources.api") },
    { value: "firewall", label: $t("logs.sources.firewall") },
  ];

  const filteredLogs = $derived(
    $logs.filter((l) => {
      if (levelFilter !== "all" && l.level !== levelFilter) return false;
      if (sourceFilter !== "all" && l.source !== sourceFilter) return false;
      return true;
    }),
  );

  function scrollToBottom() {
    if (autoScroll && logContainer) {
      logContainer.scrollTop = logContainer.scrollHeight;
    }
  }

  function clearLogs() {
    logs.set([]);
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

  // Auto-scroll when logs change
  $effect(() => {
    if (filteredLogs.length > 0 && autoScroll) {
      tick().then(scrollToBottom);
    }
  });
</script>

<div class="logs-page">
  <div class="page-header">
    <div class="header-controls">
      <Select
        id="source-filter"
        bind:value={sourceFilter}
        options={LOG_SOURCES}
      />
      <Select
        id="level-filter"
        bind:value={levelFilter}
        options={[
          { value: "all", label: $t("logs.all_levels") },
          { value: "error", label: $t("logs.errors") },
          { value: "warn", label: $t("logs.warnings") },
          { value: "info", label: $t("logs.info") },
          { value: "debug", label: $t("logs.debug") },
        ]}
      />
      <Toggle label={$t("logs.auto_scroll")} bind:checked={autoScroll} />
      <Button variant="ghost" size="sm" onclick={clearLogs}
        >{$t("logs.clear")}</Button
      >
    </div>
  </div>

  <Card>
    <div class="log-viewer" bind:this={logContainer}>
      {#if filteredLogs.length === 0}
        <p class="empty-message">
          {$t("common.no_items", { values: { items: $t("item.log") } })}
        </p>
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
