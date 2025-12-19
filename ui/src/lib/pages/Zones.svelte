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
  } from "$lib/components";

  let loading = $state(false);
  let showAddZoneModal = $state(false);
  let editingZone = $state<any>(null);

  // Zone form
  let zoneName = $state("");
  let zoneColor = $state("blue");
  let zoneDescription = $state("");

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
    return interfaces
      .filter((i: any) => i.Zone === zoneName)
      .map((i: any) => i.Name);
  }

  function openAddZone() {
    editingZone = null;
    zoneName = "";
    zoneColor = "blue";
    zoneDescription = "";
    showAddZoneModal = true;
  }

  function editZone(zone: any) {
    editingZone = zone;
    zoneName = zone.name;
    zoneColor = zone.color || "blue";
    zoneDescription = zone.description || "";
    showAddZoneModal = true;
  }

  async function saveZone() {
    if (!zoneName) return;

    loading = true;
    try {
      let updatedZones: any[];

      if (editingZone) {
        // Update existing
        updatedZones = zones.map((z: any) =>
          z.name === editingZone.name
            ? { name: zoneName, color: zoneColor, description: zoneDescription }
            : z,
        );
      } else {
        // Add new
        updatedZones = [
          ...zones,
          { name: zoneName, color: zoneColor, description: zoneDescription },
        ];
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
          <div
            class="zone-badge"
            style="--zone-color: var(--zone-{zone.color})"
          >
            {zone.name}
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

        <div class="zone-interfaces">
          <span class="interfaces-label">Interfaces:</span>
          {#if getZoneInterfaces(zone.name).length > 0}
            {#each getZoneInterfaces(zone.name) as iface}
              <Badge variant="outline">{iface}</Badge>
            {/each}
          {:else}
            <span class="no-interfaces">None assigned</span>
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

    <Input
      id="zone-desc"
      label="Description"
      bind:value={zoneDescription}
      placeholder="e.g., Guest network for visitors"
    />

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

  .zone-interfaces {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--text-sm);
  }

  .interfaces-label {
    color: var(--color-muted);
  }

  .no-interfaces {
    color: var(--color-muted);
    font-style: italic;
  }

  /* Zone color variables */
  :global(:root) {
    --zone-red: #dc2626;
    --zone-green: #16a34a;
    --zone-blue: #2563eb;
    --zone-orange: #ea580c;
    --zone-purple: #9333ea;
    --zone-cyan: #0891b2;
    --zone-gray: #6b7280;
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
