<script lang="ts">
  import "../lib/styles/global.css";
  import "$lib/i18n";
  import { isLoading, t } from "svelte-i18n";
  import { onMount, onDestroy } from "svelte";
  import { api, currentView, brand } from "$lib/stores/app";
  import AlertModal from "$lib/components/AlertModal.svelte";
  import StagedChangesBar from "$lib/components/StagedChangesBar.svelte";

  let { children } = $props();
  let pendingCheckInterval: ReturnType<typeof setInterval> | null = null;

  onMount(async () => {
    // Load brand info
    await api.getBrand();

    // Check auth status
    const authData = await api.checkAuth();

    if (authData?.setup_required) {
      currentView.set("setup");
    } else if (!authData?.authenticated) {
      currentView.set("login");
    } else {
      await api.loadDashboard();
      currentView.set("app");
      // Note: pending changes status is provided via WebSocket status topic
    }
  });

  onDestroy(() => {
    if (pendingCheckInterval) {
      clearInterval(pendingCheckInterval);
    }
  });
</script>

<svelte:head>
  <title>{$brand?.name || "Glacic"}</title>
  <meta
    name="description"
    content={$brand?.tagline || "Network learning firewall"}
  />
  <!-- Fonts loaded via /fonts/fonts.css in app.html -->
</svelte:head>

{#if $isLoading}
  <div class="loading-overlay">Loading...</div>
{:else}
  {@render children()}
  <AlertModal />
  <StagedChangesBar />
{/if}
