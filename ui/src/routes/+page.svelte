<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import {
    currentView,
    brand,
    currentPage,
    pages,
    status,
    config,
    api,
    theme,
  } from "$lib/stores/app";
  import {
    connectWebSocket,
    disconnectWebSocket,
    wsConnected,
  } from "$lib/stores/websocket";
  import Button from "$lib/components/Button.svelte";
  import Input from "$lib/components/Input.svelte";
  import Card from "$lib/components/Card.svelte";
  import Modal from "$lib/components/Modal.svelte";
  import SearchBar from "$lib/components/SearchBar.svelte";
  import ThemeToggle from "$lib/components/ThemeToggle.svelte";
  import Toast from "$lib/components/Toast.svelte";
  import NotificationBell from "$lib/components/NotificationBell.svelte";
  import { t } from "svelte-i18n";

  // Page components
  import {
    Dashboard,
    Interfaces,
    Firewall,
    DHCP,
    DNS,
    NAT,
    Zones,
    IPSets,
    Routing,
    VPN,
    Network,
    Logs,
    Scanner,
    NetworkLearning,
    Settings,
    NotFound,
  } from "$lib/pages";

  // Login form state
  let loginUsername = $state("");
  let loginPassword = $state("");
  let loginError = $state("");

  // Setup form state
  let setupUsername = $state("admin");
  let setupPassword = $state("");
  let setupConfirm = $state("");
  let setupError = $state("");
  let sidebarOpen = $state(false);

  function closeSidebar() {
    sidebarOpen = false;
  }

  async function handleLogin() {
    loginError = "";
    try {
      await api.login(loginUsername, loginPassword);
      await api.loadDashboard();
      currentView.set("app");
    } catch (e: any) {
      loginError = e.message || "Login failed";
    }
  }

  async function handleSetup() {
    setupError = "";
    if (setupPassword !== setupConfirm) {
      setupError = "Passwords do not match";
      return;
    }
    if (setupPassword.length < 8) {
      setupError = "Password must be at least 8 characters";
      return;
    }
    try {
      await api.createAdmin(setupUsername, setupPassword);
      await api.loadDashboard();
      currentView.set("app");
    } catch (e: any) {
      setupError = e.message || "Setup failed";
    }
  }

  // Connect WebSocket when authenticated
  onMount(() => {
    // Sync current page from hash
    currentPage.syncWithHash();

    // Connect if already authenticated
    if ($currentView === "app") {
      connectWebSocket(["status", "logs", "stats", "notification"]);
    }
  });

  onDestroy(() => {
    disconnectWebSocket();
  });

  // Also connect when transitioning to app view
  $effect(() => {
    if ($currentView === "app") {
      connectWebSocket(["status", "logs", "stats", "notification"]);
    }
  });

  function toggleTheme() {
    const current = $theme;
    const next =
      current === "light" ? "dark" : current === "dark" ? "system" : "light";
    theme.set(next);
  }

  const pageTitle = $derived(
    $pages.find((p) => p.id === $currentPage)?.label || "Dashboard",
  );
</script>

<!-- Loading View -->
{#if $currentView === "loading"}
  <div class="loading-view">
    <div class="loading-content">
      <div class="loading-icon">🛡️</div>
      <p class="loading-text">{$t("common.loading")}</p>
    </div>
  </div>

  <!-- Setup View -->
{:else if $currentView === "setup"}
  <div class="auth-view">
    <div class="auth-container">
      <div class="auth-header">
        <div class="auth-icon">🛡️</div>
        <h1 class="auth-title">
          {$t("auth.setup_title", { values: { name: $brand?.name } })}
        </h1>
        <p class="auth-subtitle">{$t("auth.setup_subtitle")}</p>
      </div>

      <Card class="auth-card">
        <form
          onsubmit={(e) => {
            e.preventDefault();
            handleSetup();
          }}
        >
          <div class="form-stack">
            <Input
              id="setup-username"
              label={$t("auth.username")}
              bind:value={setupUsername}
              placeholder={$t("auth.username_placeholder")}
              required
            />

            <Input
              id="setup-password"
              type="password"
              label={$t("auth.password")}
              bind:value={setupPassword}
              placeholder={$t("auth.password_min_chars")}
              required
            />

            <Input
              id="setup-confirm"
              type="password"
              label={$t("auth.confirm_password")}
              bind:value={setupConfirm}
              placeholder={$t("auth.confirm_password_placeholder")}
              required
            />

            {#if setupError}
              <div class="error-message">{setupError}</div>
            {/if}

            <Button type="submit">{$t("auth.create_account")}</Button>
          </div>
        </form>
      </Card>
    </div>
  </div>

  <!-- Login View -->
{:else if $currentView === "login"}
  <div class="auth-view">
    <div class="auth-container">
      <div class="auth-header">
        <div class="auth-icon">🛡️</div>
        <h1 class="auth-title">{$brand?.name}</h1>
        <p class="auth-subtitle">{$t("auth.signin_subtitle")}</p>
      </div>

      <Card class="auth-card">
        <form
          onsubmit={(e) => {
            e.preventDefault();
            handleLogin();
          }}
        >
          <div class="form-stack">
            <Input
              id="login-username"
              label={$t("auth.username")}
              bind:value={loginUsername}
              placeholder={$t("auth.username")}
              required
            />

            <Input
              id="login-password"
              type="password"
              label={$t("auth.password")}
              bind:value={loginPassword}
              placeholder={$t("auth.password")}
              required
            />

            {#if loginError}
              <div class="error-message">{loginError}</div>
            {/if}

            <Button type="submit">{$t("auth.login")}</Button>
          </div>
        </form>
      </Card>
    </div>
  </div>

  <!-- Main App View -->
{:else}
  <Toast />
  <div class="app-layout">
    <!-- Mobile Overlay -->
    <div
      class="sidebar-overlay"
      class:visible={sidebarOpen}
      onclick={closeSidebar}
      role="presentation"
    ></div>

    <!-- Sidebar -->
    <aside class="sidebar" class:open={sidebarOpen}>
      <div class="sidebar-header">
        <div class="brand-icon">🛡️</div>
        <span class="brand-name">{$brand?.name}</span>
      </div>

      <nav class="sidebar-nav">
        <a
          href="#dashboard"
          class="nav-item"
          class:active={$currentPage === "dashboard"}
        >
          {$t("nav.dashboard")}
        </a>
        <a
          href="#interfaces"
          class="nav-item"
          class:active={$currentPage === "interfaces"}
        >
          {$t("nav.interfaces")}
        </a>
        <a
          href="#firewall"
          class="nav-item"
          class:active={$currentPage === "firewall"}
        >
          {$t("nav.firewall")}
        </a>
        <a
          href="#learning"
          class="nav-item"
          class:active={$currentPage === "learning"}
        >
          {$t("nav.learning")}
        </a>
        <a href="#nat" class="nav-item" class:active={$currentPage === "nat"}>
          {$t("nav.nat")}
        </a>
        <a
          href="#zones"
          class="nav-item"
          class:active={$currentPage === "zones"}
        >
          {$t("nav.zones")}
        </a>
        <a
          href="#ipsets"
          class="nav-item"
          class:active={$currentPage === "ipsets"}
        >
          {$t("nav.ipsets")}
        </a>
        <a href="#dhcp" class="nav-item" class:active={$currentPage === "dhcp"}>
          {$t("nav.dhcp")}
        </a>
        <a href="#dns" class="nav-item" class:active={$currentPage === "dns"}>
          {$t("nav.dns")}
        </a>
        <a
          href="#routing"
          class="nav-item"
          class:active={$currentPage === "routing"}
        >
          {$t("nav.routing")}
        </a>
        <a href="#vpn" class="nav-item" class:active={$currentPage === "vpn"}>
          {$t("nav.vpn")}
        </a>

        <div class="nav-divider"></div>

        <a
          href="#network"
          class="nav-item"
          class:active={$currentPage === "network"}
        >
          {$t("nav.network")}
        </a>
        <a href="#logs" class="nav-item" class:active={$currentPage === "logs"}>
          {$t("nav.logs")}
        </a>
        <a
          href="#scanner"
          class="nav-item"
          class:active={$currentPage === "scanner"}
        >
          {$t("nav.scanner")}
        </a>
        <a
          href="#settings"
          class="nav-item"
          class:active={$currentPage === "settings"}
        >
          {$t("nav.settings")}
        </a>
      </nav>

      <div class="sidebar-footer">
        <ThemeToggle />
      </div>
    </aside>

    <!-- Main Content -->
    <div class="main-wrapper">
      <header class="header">
        <button
          class="mobile-menu-toggle"
          onclick={() => (sidebarOpen = !sidebarOpen)}
        >
          ☰
        </button>
        <h1 class="page-title">{pageTitle}</h1>

        <SearchBar />

        <div class="header-actions">
          <span
            class="status-indicator"
            class:online={$config?.ip_forwarding}
            title={$config?.ip_forwarding
              ? "IP forwarding is enabled - routing traffic between networks"
              : "IP forwarding is disabled - no traffic routing"}
            aria-label={$config?.ip_forwarding
              ? "Router status: Active"
              : "Router status: Standby"}
          >
            {$config?.ip_forwarding
              ? $t("dashboard.active_state")
              : $t("dashboard.disabled_state")}
          </span>
          <NotificationBell />
          <button class="logout-btn" onclick={() => api.logout()}
            >{$t("auth.logout")}</button
          >
        </div>
      </header>

      <main class="main-content">
        {#if $currentPage === "dashboard"}
          <Dashboard />
        {:else if $currentPage === "interfaces"}
          <Interfaces />
        {:else if $currentPage === "firewall"}
          <Firewall />
        {:else if $currentPage === "learning"}
          <NetworkLearning />
        {:else if $currentPage === "dhcp"}
          <DHCP />
        {:else if $currentPage === "dns"}
          <DNS />
        {:else if $currentPage === "nat"}
          <NAT />
        {:else if $currentPage === "zones"}
          <Zones />
        {:else if $currentPage === "ipsets"}
          <IPSets />
        {:else if $currentPage === "routing"}
          <Routing />
        {:else if $currentPage === "vpn"}
          <VPN />
        {:else if $currentPage === "network"}
          <Network />
        {:else if $currentPage === "logs"}
          <Logs />
        {:else if $currentPage === "scanner"}
          <Scanner />
        {:else if $currentPage === "settings"}
          <Settings />
        {:else}
          <NotFound />
        {/if}
      </main>
    </div>
  </div>
{/if}

<style>
  /* Loading View */
  .loading-view {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: var(--color-background);
  }

  .loading-content {
    text-align: center;
  }

  .loading-icon {
    font-size: 3rem;
    margin-bottom: var(--space-4);
  }

  .loading-text {
    color: var(--color-muted);
  }

  /* Auth Views (Login/Setup) */
  .auth-view {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-4);
    background-color: var(--color-background);
  }

  .auth-container {
    width: 100%;
    max-width: 400px;
  }

  .auth-header {
    text-align: center;
    margin-bottom: var(--space-6);
  }

  .auth-icon {
    font-size: 3rem;
    margin-bottom: var(--space-4);
  }

  .auth-title {
    font-size: var(--text-2xl);
    font-weight: 700;
    color: var(--color-foreground);
    margin: 0 0 var(--space-2) 0;
  }

  .auth-subtitle {
    color: var(--color-muted);
    margin: 0;
  }

  .form-stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .error-message {
    padding: var(--space-3);
    background-color: rgba(239, 68, 68, 0.1);
    border: 1px solid var(--color-destructive);
    border-radius: var(--radius-md);
    color: var(--color-destructive);
    font-size: var(--text-sm);
  }

  /* App Layout */
  .app-layout {
    display: flex;
    min-height: 100vh;
  }

  /* Sidebar */
  .sidebar {
    width: 240px;
    background-color: var(--color-surface);
    border-right: 1px solid var(--color-border);
    display: flex;
    flex-direction: column;
    position: sticky;
    top: 0;
    height: 100vh;
  }

  .sidebar-header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-4);
    border-bottom: 1px solid var(--color-border);
  }

  .brand-icon {
    font-size: 1.5rem;
  }

  .brand-name {
    font-weight: 600;
    color: var(--color-foreground);
  }

  .sidebar-nav {
    flex: 1;
    padding: var(--space-2);
  }

  .nav-item {
    display: block;
    width: 100%;
    padding: var(--space-3) var(--space-4);
    text-align: left;
    background: none;
    border: none;
    border-radius: var(--radius-md);
    color: var(--color-foreground);
    font-size: var(--text-sm);
    font-family: inherit;
    cursor: pointer;
    transition: background-color var(--transition-fast);
    text-decoration: none;
  }

  .nav-item:hover {
    background-color: var(--color-surfaceHover);
  }

  .nav-item.active {
    background-color: var(--color-primary);
    color: var(--color-primaryForeground);
  }

  .sidebar-footer {
    padding: var(--space-4);
    border-top: 1px solid var(--color-border);
  }

  /* Main Content Wrapper */
  .main-wrapper {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  /* Header */
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-6);
    background-color: var(--color-background);
    border-bottom: 1px solid var(--color-border);
  }

  .page-title {
    font-size: var(--text-xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }

  .status-indicator {
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-full);
    font-size: var(--text-sm);
    font-weight: 500;
    background-color: var(--color-destructive);
    color: var(--color-destructiveForeground);
  }

  .status-indicator.online {
    background-color: var(--color-success);
    color: var(--color-successForeground);
  }

  .logout-btn {
    padding: var(--space-2) var(--space-4);
    background: none;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    color: var(--color-foreground);
    font-size: var(--text-sm);
    cursor: pointer;
    transition: background-color var(--transition-fast);
  }

  .logout-btn:hover {
    background-color: var(--color-surfaceHover);
  }

  /* Main Content */
  .main-content {
    flex: 1;
    padding: var(--space-6);
    overflow-y: auto;
    background-color: var(--color-backgroundSecondary);
  }

  /* Navigation Divider */
  .nav-divider {
    height: 1px;
    background-color: var(--color-border);
    margin: var(--space-2) var(--space-3);
  }

  /* Mobile Menu Toggle */
  .mobile-menu-toggle {
    display: none;
    background: none;
    border: none;
    padding: var(--space-2);
    cursor: pointer;
    font-size: 1.5rem;
  }

  /* Mobile Responsive Styles */
  @media (max-width: 768px) {
    .app-layout {
      flex-direction: column;
    }

    .sidebar {
      position: fixed;
      left: -100%;
      top: 0;
      z-index: 1000;
      width: 280px;
      transition: left 0.3s ease;
      box-shadow: var(--shadow-lg);
    }

    .sidebar.open {
      left: 0;
    }

    .sidebar-overlay {
      display: none;
      position: fixed;
      inset: 0;
      background-color: rgba(0, 0, 0, 0.5);
      z-index: 999;
    }

    .sidebar-overlay.visible {
      display: block;
    }

    .mobile-menu-toggle {
      display: block;
    }

    .header {
      gap: var(--space-2);
    }

    .header-actions {
      gap: var(--space-2);
    }

    .main-content {
      padding: var(--space-4);
    }

    /* Login form mobile */
  }

  /* Tablet Responsive */
  @media (max-width: 1024px) and (min-width: 769px) {
    .sidebar {
      width: 200px;
    }

    .main-content {
      padding: var(--space-4);
    }
  }
</style>
