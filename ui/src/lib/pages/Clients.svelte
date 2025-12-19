<script lang="ts">
  /**
   * Clients Page
   * Connected clients and DHCP leases
   */

  import { leases, config, api, alertStore } from "$lib/stores/app";
  import { Card, Badge, Table, Input, Modal, Button } from "$lib/components";

  let searchQuery = $state("");
  let editingClient = $state<any>(null);
  let editModalOpen = $state(false);

  // Edit State
  let editAlias = $state("");
  let editOwner = $state("");
  let editType = $state("");
  let editTags = $state("");
  let isSaving = $state(false);

  const allClients = $derived(
    ($leases || []).map((lease: any) => ({
      ...lease,
      // Use Alias if available, else Hostname, else ClientID
      displayName:
        lease.alias || lease.hostname || lease.client_id || "Unknown",
      status: lease.active ? "active" : "expired",
    })),
  );

  const filteredClients = $derived(
    searchQuery.trim() === ""
      ? allClients
      : allClients.filter((c: any) => {
          const q = searchQuery.toLowerCase();
          return (
            c.displayName?.toLowerCase().includes(q) ||
            c.ip_address?.includes(q) ||
            c.mac?.toLowerCase().includes(q) ||
            (c.tags || []).some((t: string) => t.toLowerCase().includes(q))
          );
        }),
  );

  function openEdit(client: any) {
    editingClient = client;
    editAlias = client.alias || "";
    editOwner = client.owner || "";
    editType = client.type || "";
    editTags = (client.tags || []).join(", ");
    editModalOpen = true;
  }

  function closeEdit() {
    editingClient = null;
    editModalOpen = false;
  }

  async function saveIdentity() {
    if (!editingClient) return;
    isSaving = true;

    try {
      const tags = editTags
        .split(",")
        .map((t) => t.trim())
        .filter((t) => t);
      const alias =
        editAlias || editingClient.hostname || `Device-${editingClient.mac}`;

      // Update Identity (Creates if ID is empty)
      const identity = await api.updateDeviceIdentity(
        editingClient.device_id || "",
        alias,
        editOwner,
        editType,
        tags,
      );

      // If we didn't have an ID before, we must Link the MAC to the new Identity
      if (!editingClient.device_id && identity && identity.id) {
        await api.linkDevice(editingClient.mac, identity.id);
      }

      alertStore.show("Device updated successfully", "success");
      closeEdit();
    } catch (err: any) {
      alertStore.show(err.message || "Failed to update device", "error");
    } finally {
      isSaving = false;
    }
  }

  async function unlinkIdentity() {
    if (!editingClient || !editingClient.device_id) return;
    if (!confirm("Are you sure you want to unlink this device identity?"))
      return;

    isSaving = true;
    try {
      await api.unlinkDevice(editingClient.mac);
      alertStore.show("Device unlinked", "success");
      closeEdit();
    } catch (err: any) {
      alertStore.show(err.message || "Failed to unlink device", "error");
    } finally {
      isSaving = false;
    }
  }
</script>

<div class="clients-page">
  <div class="page-header">
    <h2>Connected Clients</h2>
    <div class="header-stats">
      <span class="stat">
        <strong
          >{allClients.filter((c: any) => c.status === "active").length}</strong
        > active
      </span>
      <span class="stat">
        <strong>{allClients.length}</strong> total
      </span>
    </div>
  </div>

  <Card>
    <div class="search-row">
      <Input
        id="client-search"
        placeholder="Search by hostname, IP, MAC, or tags..."
        bind:value={searchQuery}
      />
    </div>

    {#if filteredClients.length > 0}
      <div class="clients-list">
        {#each filteredClients as client}
          <div class="client-row">
            <div class="client-main">
              <span class="client-name">
                {client.displayName}
                {#if client.alias}
                  <span class="client-real-hostname">({client.hostname})</span>
                {/if}
              </span>
              <code class="client-ip">{client.ip_address}</code>

              {#if client.tags && client.tags.length > 0}
                <div class="client-tags">
                  {#each client.tags as tag}
                    <Badge variant="neutral" size="sm">{tag}</Badge>
                  {/each}
                </div>
              {/if}
            </div>

            <div class="client-meta">
              <code class="client-mac">{client.mac}</code>
              {#if client.vendor}
                <span class="client-vendor">{client.vendor}</span>
              {/if}
              {#if client.interface}
                <Badge variant="outline">{client.interface}</Badge>
              {/if}

              <Button
                size="sm"
                variant="ghost"
                onclick={() => openEdit(client)}
              >
                Manage
              </Button>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <p class="empty-message">
        {searchQuery
          ? `No clients matching "${searchQuery}"`
          : "No clients connected"}
      </p>
    {/if}
  </Card>
</div>

<Modal bind:open={editModalOpen} title="Manage Device">
  <div class="edit-form">
    {#if editingClient}
      <div class="form-group">
        <label for="mac">MAC Address</label>
        <code id="mac">{editingClient.mac}</code>
      </div>
      <div class="form-group">
        <label for="hostname">Detected Hostname</label>
        <span id="hostname">{editingClient.hostname || "N/A"}</span>
      </div>

      <div class="form-group">
        <label for="alias">Alias (Name)</label>
        <Input id="alias" bind:value={editAlias} placeholder="Friendly Name" />
      </div>

      <div class="form-group">
        <label for="owner">Owner</label>
        <Input id="owner" bind:value={editOwner} placeholder="e.g. Ben" />
      </div>

      <div class="form-group">
        <label for="type">Type</label>
        <Input
          id="type"
          bind:value={editType}
          placeholder="e.g. phone, laptop, iot"
        />
      </div>

      <div class="form-group">
        <label for="tags">Tags (comma separated)</label>
        <Input
          id="tags"
          bind:value={editTags}
          placeholder="kids, iot, printer"
        />
      </div>

      <div class="modal-actions">
        {#if editingClient.device_id}
          <Button
            variant="danger"
            variantType="outline"
            onclick={unlinkIdentity}
            disabled={isSaving}>Unlink Identity</Button
          >
        {/if}
        <div class="spacer"></div>
        <Button variant="secondary" onclick={closeEdit} disabled={isSaving}
          >Cancel</Button
        >
        <Button variant="primary" onclick={saveIdentity} disabled={isSaving}>
          {isSaving ? "Saving..." : "Save Changes"}
        </Button>
      </div>
    {/if}
  </div>
</Modal>

<style>
  .clients-page {
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

  .header-stats {
    display: flex;
    gap: var(--space-4);
  }

  .stat {
    font-size: var(--text-sm);
    color: var(--color-muted);
  }

  .stat strong {
    color: var(--color-foreground);
  }

  .search-row {
    margin-bottom: var(--space-4);
  }

  .clients-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .client-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-3);
    background-color: var(--color-backgroundSecondary);
    border-radius: var(--radius-md);
  }

  .client-main {
    display: flex;
    align-items: center; /* Changed from baseline to center for tags alignment */
    gap: var(--space-3);
    flex-wrap: wrap; /* Allow wrapping on small screens */
  }

  .client-name {
    font-weight: 500;
    color: var(--color-foreground);
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .client-real-hostname {
    font-size: var(--text-xs);
    color: var(--color-muted);
    font-weight: normal;
  }

  .client-ip {
    font-size: var(--text-sm);
    color: var(--color-muted);
  }

  .client-tags {
    display: flex;
    gap: var(--space-1);
  }

  .client-meta {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .client-mac {
    font-size: var(--text-xs);
    color: var(--color-muted);
    font-family: monospace;
  }

  .client-vendor {
    font-size: var(--text-xs);
    color: var(--color-muted);
    max-width: 150px;
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

  /* Modal Styles */
  .edit-form {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .form-group label {
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--color-foreground);
  }

  .form-group code#mac {
    font-family: monospace;
    color: var(--color-muted);
  }

  .form-group span#hostname {
    color: var(--color-foreground);
  }

  .modal-actions {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-4);
    justify-content: flex-end;
  }

  .spacer {
    flex: 1;
  }
</style>
