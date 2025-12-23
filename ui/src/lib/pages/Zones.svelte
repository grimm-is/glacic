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
    <h2>Network Zones</h2>
    <Button onclick={openAddZone}>+ Add Zone</Button>
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
              <Badge variant="secondary">External</Badge>
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
            <span class="detail-label">Interfaces:</span>
            <div class="detail-tags">
              {#if getZoneInterfaces(zone.name).length > 0}
                {#each getZoneInterfaces(zone.name) as iface}
                  <Badge variant="outline">{iface}</Badge>
                {/each}
              {:else}
                <span class="text-sm text-muted-foreground italic"
                  >None assigned</span
                >
              {/if}
            </div>
          </div>

          {#if zone.management && Object.values(zone.management).some(Boolean)}
            <div class="detail-section">
              <span class="detail-label">Allow:</span>
              <div class="detail-tags">
                {#if zone.management.web}<Badge variant="secondary">Web</Badge
                  >{/if}
                {#if zone.management.ssh}<Badge variant="secondary">SSH</Badge
                  >{/if}
                {#if zone.management.api}<Badge variant="secondary">API</Badge
                  >{/if}
                {#if zone.management.icmp}<Badge variant="secondary">Ping</Badge
                  >{/if}
              </div>
            </div>
          {/if}

          {#if zone.services && Object.values(zone.services).some(Boolean)}
            <div class="detail-section">
              <span class="detail-label">Services:</span>
              <div class="detail-tags">
                {#if zone.services.dhcp}<Badge variant="secondary">DHCP</Badge
                  >{/if}
                {#if zone.services.dns}<Badge variant="secondary">DNS</Badge
                  >{/if}
                {#if zone.services.ntp}<Badge variant="secondary">NTP</Badge
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
  title={editingZone ? "Edit Zone" : "Add Zone"}
>
  <div class="form-stack">
    <div class="grid grid-cols-2 gap-4">
      <Input
        id="zone-name"
        label="Zone Name"
        bind:value={zoneName}
        placeholder="e.g., Guest"
        required
        disabled={!!editingZone}
      />

      <Select
        id="zone-color"
        label="Color"
        bind:value={zoneColor}
        options={colorOptions}
      />
    </div>

    <Input
      id="zone-desc"
      label="Description"
      bind:value={zoneDescription}
      placeholder="e.g., Guest network for visitors"
    />

    <div class="p-4 bg-secondary/10 rounded-lg space-y-4">
      <h3 class="text-sm font-medium text-foreground">Zone Type</h3>
      <Toggle label="External Zone (WAN)" bind:checked={zoneExternal} />
      <p class="text-xs text-muted-foreground">
        Enable for zones connected to the internet (enables Masquerade/NAT)
      </p>
    </div>

    <div class="grid grid-cols-2 gap-6">
      <div class="space-y-3">
        <h3 class="text-sm font-medium text-foreground">Management Access</h3>
        <p class="text-xs text-muted-foreground mb-2">
          Allow access FROM this zone TO the firewall
        </p>
        <Toggle label="Web UI" bind:checked={mgmtWeb} />
        <Toggle label="SSH" bind:checked={mgmtSsh} />
        <Toggle label="API" bind:checked={mgmtApi} />
        <Toggle label="ICMP (Ping)" bind:checked={mgmtIcmp} />
      </div>

      <div class="space-y-3">
        <h3 class="text-sm font-medium text-foreground">Network Services</h3>
        <p class="text-xs text-muted-foreground mb-2">
          Services provided BY the firewall TO this zone
        </p>
        <Toggle label="DHCP Server" bind:checked={svcDhcp} />
        <Toggle label="DNS Resolver" bind:checked={svcDns} />
        <Toggle label="NTP Server" bind:checked={svcNtp} />
      </div>
    </div>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showAddZoneModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveZone} disabled={loading || !zoneName}>
        {#if loading}<Spinner size="sm" />{/if}
        {editingZone ? "Save Changes" : "Add Zone"}
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

  .page-header h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
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
