<script lang="ts">
  import "../lib/styles/global.css";
  import { onMount } from "svelte";
  import { api, currentView, brand } from "$lib/stores/app";
  import AlertModal from "$lib/components/AlertModal.svelte";

  let { children } = $props();

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

{@render children()}

<AlertModal />
