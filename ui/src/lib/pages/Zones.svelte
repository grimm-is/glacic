<script lang="ts">
  /**
   * Zones Page
   * Network zone management
   */

  import { config, api, alertStore } from "$lib/stores/app";
  import {
    Card,
    Button,
    Modal,
    Input,
    Select,
    Badge,
    Icon,
    Spinner,
    Toggle,
  } from "$lib/components";
  import { t } from "svelte-i18n";

  let loading = $state(false);
  let showAddZoneModal = $state(false);
  let editingZone = $state<any>(null);

  // Zone form
  let zoneName = $state("");
  let zoneColor = $state("blue");
  let zoneDescription = $state("");
  let zoneExternal = $state(false);

  // Management
  let mgmtWeb = $state(false);
  let mgmtSsh = $state(false);
  let mgmtApi = $state(false);
  let mgmtIcmp = $state(false);

  // Services
  let svcDhcp = $state(false);
  let svcDns = $state(false);
  let svcNtp = $state(false);

  const zones = $derived($config?.zones || []);
  const interfaces = $derived($config?.interfaces || []);

  const colorOptions = [
    { value: "red", label: "Red (WAN)" },
    { value: "green", label: "Green (LAN)" },
    { value: "blue", label: "Blue (Internal)" },
    { value: "orange", label: "Orange (DMZ)" },
    { value: "purple", label: "Purple (Guest)" },
    { value: "cyan", label: "Cyan (IoT)" },
    { value: "gray", label: "Gray (Unused)" },
  ];

  function getZoneInterfaces(zoneName: string): string[] {
    // Interfaces assigned via iface.Zone
    const fromIface = interfaces
      .filter((i: any) => i.Zone === zoneName)
      .map((i: any) => i.Name);

    // Interfaces assigned via zone.interfaces
    const zone = zones.find((z: any) => z.name === zoneName);
    const fromZone = zone?.interfaces || [];

    // Deduplicate
    return [...new Set([...fromIface, ...fromZone])];
  }

  function openAddZone() {
    editingZone = null;
    zoneName = "";
    zoneColor = "blue";
    zoneDescription = "";
    zoneExternal = false;
    // Reset flags
    mgmtWeb = false;
    mgmtSsh = false;
    mgmtApi = false;
    mgmtIcmp = false;
    svcDhcp = false;
    svcDns = false;
    svcNtp = false;
    showAddZoneModal = true;
  }

  function editZone(zone: any) {
    editingZone = zone;
    zoneName = zone.name;
    zoneColor = zone.color || "blue";
    zoneDescription = zone.description || "";
    zoneExternal = zone.external === true; // Treat nil as false

    // Populate Management
    mgmtWeb = zone.management?.web || false;
    mgmtSsh = zone.management?.ssh || false;
    mgmtApi = zone.management?.api || false;
    mgmtIcmp = zone.management?.icmp || false;

    // Populate Services
    svcDhcp = zone.services?.dhcp || false;
    svcDns = zone.services?.dns || false;
    svcNtp = zone.services?.ntp || false;

    showAddZoneModal = true;
  }

  async function saveZone() {
    if (!zoneName) return;

    loading = true;
    try {
      const zoneData = {
        name: zoneName,
        color: zoneColor,
        description: zoneDescription,
        external: zoneExternal,
        management: {
          web: mgmtWeb,
          ssh: mgmtSsh,
          api: mgmtApi,
          icmp: mgmtIcmp,
        },
        services: {
          dhcp: svcDhcp,
          dns: svcDns,
          ntp: svcNtp,
        },
      };

      let updatedZones: any[];

      if (editingZone) {
        // Update existing (preserve other fields like interfaces/matches if user didn't edit them here)
        updatedZones = zones.map((z: any) =>
          z.name === editingZone.name ? { ...z, ...zoneData } : z,
        );
      } else {
        // Add new
        updatedZones = [...zones, zoneData];
      }

      await api.updateZones(updatedZones);
      showAddZoneModal = false;
    } catch (e) {
      console.error("Failed to save zone:", e);
    } finally {
      loading = false;
    }
  }

  async function deleteZone(zone: any) {
    if (getZoneInterfaces(zone.name).length > 0) {
      alertStore.error("Cannot delete zone with assigned interfaces");
      return;
    }

    loading = true;
    try {
      const updatedZones = zones.filter((z: any) => z.name !== zone.name);
      await api.updateZones(updatedZones);
    } catch (e) {
      console.error("Failed to delete zone:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="zones-page">
  <div class="page-header">
    <Button onclick={openAddZone}
      >+ {$t("common.add_item", { values: { item: $t("item.zone") } })}</Button
    >
  </div>

  <div class="zones-grid">
    {#each zones as zone}
      <Card>
        <div class="zone-header">
          <div class="flex items-center gap-2">
            <div
              class="zone-badge"
              style="--zone-color: var(--zone-{zone.color})"
            >
              {zone.name}
            </div>
            {#if zone.external}
              <Badge variant="secondary">{$t("zones.external")}</Badge>
            {/if}
          </div>
          <div class="zone-actions">
            <Button variant="ghost" size="sm" onclick={() => editZone(zone)}
              ><Icon name="edit" size="sm" /></Button
            >
            <Button variant="ghost" size="sm" onclick={() => deleteZone(zone)}
              ><Icon name="delete" size="sm" /></Button
            >
          </div>
        </div>

        {#if zone.description}
          <p class="zone-description">{zone.description}</p>
        {/if}

        <div class="zone-details">
          <div class="detail-section">
            <span class="detail-label">{$t("zones.interfaces")}</span>
            <div class="detail-tags">
              {#if getZoneInterfaces(zone.name).length > 0}
                {#each getZoneInterfaces(zone.name) as iface}
                  <Badge variant="outline">{iface}</Badge>
                {/each}
              {:else}
                <span class="text-sm text-muted-foreground italic"
                  >{$t("zones.none_assigned")}</span
                >
              {/if}
            </div>
          </div>

          {#if zone.management && Object.values(zone.management).some(Boolean)}
            <div class="detail-section">
              <span class="detail-label">{$t("zones.allow")}</span>
              <div class="detail-tags">
                {#if zone.management.web}<Badge variant="secondary"
                    >{$t("zones.svc.web")}</Badge
                  >{/if}
                {#if zone.management.ssh}<Badge variant="secondary"
                    >{$t("zones.svc.ssh")}</Badge
                  >{/if}
                {#if zone.management.api}<Badge variant="secondary"
                    >{$t("zones.svc.api")}</Badge
                  >{/if}
                {#if zone.management.icmp}<Badge variant="secondary"
                    >{$t("zones.svc.icmp")}</Badge
                  >{/if}
              </div>
            </div>
          {/if}

          {#if zone.services && Object.values(zone.services).some(Boolean)}
            <div class="detail-section">
              <span class="detail-label">{$t("zones.services")}</span>
              <div class="detail-tags">
                {#if zone.services.dhcp}<Badge variant="secondary"
                    >{$t("zones.svc.dhcp")}</Badge
                  >{/if}
                {#if zone.services.dns}<Badge variant="secondary"
                    >{$t("zones.svc.dns")}</Badge
                  >{/if}
                {#if zone.services.ntp}<Badge variant="secondary"
                    >{$t("zones.svc.ntp")}</Badge
                  >{/if}
              </div>
            </div>
          {/if}
        </div>
      </Card>
    {/each}
  </div>
</div>

<!-- Add/Edit Zone Modal -->
<Modal
  bind:open={showAddZoneModal}
  title={editingZone
    ? $t("common.edit_item", { values: { item: $t("item.zone") } })
    : $t("common.add_item", { values: { item: $t("item.zone") } })}
>
  <div class="form-stack">
    <div class="grid grid-cols-2 gap-4">
      <Input
        id="zone-name"
        label={$t("zones.zone_name")}
        bind:value={zoneName}
        placeholder="e.g., Guest"
        required
        disabled={!!editingZone}
      />

      <Select
        id="zone-color"
        label={$t("zones.color")}
        bind:value={zoneColor}
        options={colorOptions.map((o) => ({
          ...o,
          label: $t(`zones.colors.${o.value}`),
        }))}
      />
    </div>

    <Input
      id="zone-desc"
      label={$t("common.description")}
      bind:value={zoneDescription}
      placeholder="e.g., Guest network for visitors"
    />

    <div class="p-4 bg-secondary/10 rounded-lg space-y-4">
      <h3 class="text-sm font-medium text-foreground">
        {$t("zones.zone_type")}
      </h3>
      <Toggle label={$t("zones.external_zone")} bind:checked={zoneExternal} />
      <p class="text-xs text-muted-foreground">
        {$t("zones.external_zone_desc")}
      </p>
    </div>

    <div class="grid grid-cols-2 gap-6">
      <div class="space-y-3">
        <h3 class="text-sm font-medium text-foreground">
          {$t("zones.management_access")}
        </h3>
        <p class="text-xs text-muted-foreground mb-2">
          {$t("zones.management_access_desc")}
        </p>
        <Toggle label={$t("zones.svc.web")} bind:checked={mgmtWeb} />
        <Toggle label={$t("zones.svc.ssh")} bind:checked={mgmtSsh} />
        <Toggle label={$t("zones.svc.api")} bind:checked={mgmtApi} />
        <Toggle label={$t("zones.svc.icmp")} bind:checked={mgmtIcmp} />
      </div>

      <div class="space-y-3">
        <h3 class="text-sm font-medium text-foreground">
          {$t("zones.network_services")}
        </h3>
        <p class="text-xs text-muted-foreground mb-2">
          {$t("zones.network_services_desc")}
        </p>
        <Toggle label={$t("zones.svc.dhcp")} bind:checked={svcDhcp} />
        <Toggle label={$t("zones.svc.dns")} bind:checked={svcDns} />
        <Toggle label={$t("zones.svc.ntp")} bind:checked={svcNtp} />
      </div>
    </div>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showAddZoneModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveZone} disabled={loading || !zoneName}>
        {#if loading}<Spinner size="sm" />{/if}
        {editingZone
          ? $t("common.save")
          : $t("common.add_item", { values: { item: $t("item.zone") } })}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .zones-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .zones-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: var(--space-4);
  }

  .zone-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-3);
  }

  .zone-badge {
    display: inline-flex;
    padding: var(--space-1) var(--space-3);
    background-color: var(--zone-color, var(--color-primary));
    color: white;
    font-weight: 600;
    font-size: var(--text-sm);
    border-radius: var(--radius-md);
  }

  .zone-actions {
    display: flex;
    gap: var(--space-1);
  }

  .zone-description {
    color: var(--color-muted);
    font-size: var(--text-sm);
    margin: 0 0 var(--space-3) 0;
  }

  .zone-details {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin-top: var(--space-3);
    padding-top: var(--space-3);
    border-top: 1px solid var(--color-border);
  }

  .detail-section {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .detail-label {
    font-size: var(--text-xs);
    font-weight: 500;
    color: var(--color-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .detail-tags {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .form-stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-4);
    padding-top: var(--space-4);
    border-top: 1px solid var(--color-border);
  }
</style>
