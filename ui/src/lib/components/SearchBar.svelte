<script lang="ts">
  /**
   * SearchBar Component
   * Global search/command palette for quick navigation
   */

  import { currentPage } from "$lib/stores/app";
  import { t } from "svelte-i18n";

  // All searchable pages
  const searchablePages = [
    {
      id: "dashboard",
      name: "Dashboard",
      path: "Dashboard",
      keywords: ["home", "overview", "status"],
    },
    {
      id: "interfaces",
      name: "Interfaces",
      path: "Interfaces",
      keywords: ["network", "eth", "lan", "wan", "vlan", "bond"],
    },
    {
      id: "firewall",
      name: "Firewall",
      path: "Firewall / Policies",
      keywords: ["rules", "policy", "block", "allow", "drop"],
    },
    {
      id: "nat",
      name: "NAT",
      path: "NAT / Port Forwarding",
      keywords: ["dnat", "snat", "masquerade", "port forward", "redirect"],
    },
    {
      id: "zones",
      name: "Zones",
      path: "Zones",
      keywords: ["network zones", "trust", "segments"],
    },
    {
      id: "ipsets",
      name: "IPSets",
      path: "IPSets / Blocklists",
      keywords: ["blocklist", "whitelist", "firehol", "threat intel"],
    },
    {
      id: "dhcp",
      name: "DHCP",
      path: "Services / DHCP Server",
      keywords: ["leases", "scope", "pool", "ip assignment"],
    },
    {
      id: "dns",
      name: "DNS",
      path: "Services / DNS Server",
      keywords: ["resolver", "forwarder", "nameserver"],
    },
    {
      id: "routing",
      name: "Routing",
      path: "Routing / Static Routes",
      keywords: ["routes", "gateway", "metric"],
    },
    {
      id: "vpn",
      name: "VPN",
      path: "VPN / WireGuard",
      keywords: ["wireguard", "peers", "tunnel", "wg0"],
    },
    {
      id: "topology",
      name: "Topology",
      path: "Network / Topology",
      keywords: ["map", "devices", "nodes", "discovery"],
    },
    {
      id: "clients",
      name: "Clients",
      path: "Network / Clients",
      keywords: ["connected", "leases", "devices", "hosts"],
    },
    {
      id: "logs",
      name: "Logs",
      path: "System / Logs",
      keywords: ["syslog", "events", "audit", "history"],
    },
    {
      id: "scanner",
      name: "Scanner",
      path: "Network / Scanner",
      keywords: ["wifi", "ssid", "networks", "scan"],
    },
  ];

  let query = $state("");
  let focused = $state(false);
  let selectedIndex = $state(0);
  let inputRef = $state<HTMLInputElement | null>(null);

  const filtered = $derived(
    query.trim() === ""
      ? []
      : searchablePages.filter((page) => {
          const q = query.toLowerCase();
          return (
            page.name.toLowerCase().includes(q) ||
            page.path.toLowerCase().includes(q) ||
            page.keywords.some((k) => k.includes(q))
          );
        }),
  );

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      query = "";
      inputRef?.blur();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      selectedIndex = Math.min(selectedIndex + 1, filtered.length - 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selectedIndex = Math.max(selectedIndex - 1, 0);
    } else if (e.key === "Enter" && filtered.length > 0) {
      e.preventDefault();
      navigateTo(filtered[selectedIndex]);
    }
  }

  function navigateTo(page: (typeof searchablePages)[0]) {
    currentPage.set(page.id);
    query = "";
    inputRef?.blur();
  }

  function highlightMatch(text: string, q: string): string {
    if (!q) return text;
    const regex = new RegExp(`(${q})`, "gi");
    return text.replace(regex, "<mark>$1</mark>");
  }

  // Reset selection when query changes
  $effect(() => {
    query;
    selectedIndex = 0;
  });

  // Global keyboard shortcut (Cmd/Ctrl + K)
  if (typeof window !== "undefined") {
    window.addEventListener("keydown", (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        inputRef?.focus();
      }
    });
  }
</script>

<div class="search-container" class:focused>
  <div class="search-icon">
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
    >
      <circle cx="11" cy="11" r="8" />
      <path d="m21 21-4.35-4.35" />
    </svg>
  </div>

  <input
    bind:this={inputRef}
    type="text"
    class="search-input"
    placeholder={$t("search.placeholder")}
    bind:value={query}
    onfocus={() => (focused = true)}
    onblur={() => setTimeout(() => (focused = false), 150)}
    onkeydown={handleKeydown}
  />

  {#if query && filtered.length > 0}
    <div class="search-results">
      {#each filtered as result, i}
        <button
          class="search-result"
          class:selected={i === selectedIndex}
          onclick={() => navigateTo(result)}
          onmouseenter={() => (selectedIndex = i)}
        >
          {@html highlightMatch(result.path, query)}
        </button>
      {/each}
    </div>
  {:else if query && filtered.length === 0}
    <div class="search-results">
      <div class="no-results">
        {$t("common.no_results", { values: { query } })}
      </div>
    </div>
  {/if}
</div>

<style>
  .search-container {
    position: relative;
    display: flex;
    align-items: center;
    width: 100%;
    max-width: 300px;
  }

  .search-icon {
    position: absolute;
    left: var(--space-3);
    color: var(--color-muted);
    pointer-events: none;
  }

  .search-input {
    width: 100%;
    padding: var(--space-2) var(--space-3) var(--space-2) var(--space-8);
    font-size: var(--text-sm);
    color: var(--color-foreground);
    background-color: var(--color-backgroundSecondary);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    outline: none;
    transition: all 0.15s ease;
  }

  .search-input:focus {
    border-color: var(--color-primary);
    box-shadow: 0 0 0 2px rgba(var(--color-primary-rgb, 79, 70, 229), 0.2);
  }

  .search-input::placeholder {
    color: var(--color-muted);
  }

  .search-results {
    position: absolute;
    top: calc(100% + var(--space-1));
    left: 0;
    right: 0;
    background-color: var(--color-background);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
    z-index: 100;
  }

  .search-result {
    display: block;
    width: 100%;
    padding: var(--space-2) var(--space-3);
    font-size: var(--text-sm);
    text-align: left;
    color: var(--color-foreground);
    background: none;
    border: none;
    cursor: pointer;
    transition: background-color 0.1s ease;
  }

  .search-result.selected {
    background-color: var(--color-primary);
    color: white;
  }

  .search-result :global(mark) {
    background-color: transparent;
    font-weight: 700;
    color: inherit;
  }

  .no-results {
    padding: var(--space-3);
    font-size: var(--text-sm);
    color: var(--color-muted);
    text-align: center;
  }
</style>
