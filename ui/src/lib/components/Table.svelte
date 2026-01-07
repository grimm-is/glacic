<script lang="ts">
  /**
   * Table Component
   * Data table with header and row rendering
   */

  interface Column {
    key: string;
    label: string;
    width?: string;
    align?: "left" | "center" | "right";
  }

  interface Props {
    columns: Column[];
    data: Record<string, any>[];
    emptyMessage?: string;
    class?: string;
    children?: any;
  }

  let {
    columns = [],
    data = [],
    emptyMessage = "No data available",
    class: className = "",
    children,
  }: Props = $props();
</script>

<div class="table-container {className}">
  <table class="table">
    <thead>
      <tr>
        {#each columns as col}
          <th style:width={col.width} style:text-align={col.align || "left"}>
            {col.label}
          </th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#if data.length === 0}
        <tr>
          <td colspan={columns.length} class="empty-row">
            {emptyMessage}
          </td>
        </tr>
      {:else}
        {#each data as row, i}
          <tr>
            {#if children}
              {@render children(row, i)}
            {:else}
              {#each columns as col}
                <td style:text-align={col.align || "left"}>
                  {row[col.key] ?? ""}
                </td>
              {/each}
            {/if}
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>

<style>
  .table-container {
    overflow-x: auto;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
  }

  .table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--text-sm);
  }

  th {
    padding: var(--space-3) var(--space-4);
    background-color: var(--color-backgroundSecondary);
    font-weight: 500;
    color: var(--color-foreground);
    border-bottom: 1px solid var(--color-border);
    text-align: left;
  }

  td {
    padding: var(--space-3) var(--space-4);
    border-bottom: 1px solid var(--color-border);
    color: var(--color-foreground);
  }

  tr:last-child td {
    border-bottom: none;
  }

  tr:hover td {
    background-color: var(--color-surfaceHover);
  }

  .empty-row {
    text-align: center;
    color: var(--color-muted);
    padding: var(--space-8);
  }
</style>
