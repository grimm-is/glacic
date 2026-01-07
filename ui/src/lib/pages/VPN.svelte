<script lang="ts">
  /**
   * VPN Page
   * WireGuard connection and peer management
   */

  import { config, api } from "$lib/stores/app";
  import {
    Card,
    Button,
    Modal,
    Input,
    Badge,
    Spinner,
    Icon,
    Table,
  } from "$lib/components";
  import { t } from "svelte-i18n";

  let loading = $state(false);
  let showConnModal = $state(false);
  let showPeerModal = $state(false);

  // Connection State
  let editingConnIndex = $state<number | null>(null);
  let connName = $state("");
  let connInterface = $state("wg0");
  let connPort = $state("51820");
  let connPrivateKey = $state("");
  let connAddresses = $state("");
  let connDns = $state("");
  let connMtu = $state("1420");
  let connMark = $state("");
  let connPeers = $state<any[]>([]);

  // Peer State (nested)
  let editingPeerIndex = $state<number | null>(null);
  let peerName = $state("");
  let peerPublicKey = $state("");
  let peerPresharedKey = $state("");
  let peerEndpoint = $state("");
  let peerAllowedIps = $state("");
  let peerKeepalive = $state("25");

  const vpnConfig = $derived($config?.vpn || { wireguard: [] });
  const connections = $derived(vpnConfig.wireguard || []);

  const peerColumns = [
    { key: "name", label: $t("vpn.name") },
    { key: "public_key", label: $t("vpn.public_key") },
    { key: "allowed_ips", label: $t("vpn.allowed_ips") },
    { key: "endpoint", label: $t("vpn.endpoint") },
  ];

  /* --- Connection Management --- */

  function openAddConnection() {
    editingConnIndex = null;
    connName = "New Connection";
    connInterface = "wg0";
    connPort = "51820";
    connPrivateKey = "";
    connAddresses = "";
    connDns = "";
    connMtu = "1420";
    connMark = "";
    connPeers = [];
    showConnModal = true;
  }

  function openEditConnection(index: number) {
    editingConnIndex = index;
    const c = connections[index];
    connName = c.name || "";
    connInterface = c.interface || "wg0";
    connPort = String(c.listen_port || "51820");
    connPrivateKey = c.private_key || "";
    connAddresses = (c.address || []).join(", ");
    connDns = (c.dns || []).join(", ");
    connMtu = String(c.mtu || "1420");
    connMark = String(c.fwmark || "");
    connPeers = [...(c.peers || [])];
    showConnModal = true;
  }

  async function saveConnection() {
    loading = true;
    try {
      const newConn = {
        name: connName,
        interface: connInterface,
        listen_port: Number(connPort),
        private_key: connPrivateKey,
        address: connAddresses
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        dns: connDns
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        mtu: Number(connMtu),
        fwmark: connMark ? Number(connMark) : undefined,
        peers: connPeers,
        enabled: true, // Default to enabled on create/save
      };

      let updatedWg = [...connections];
      if (editingConnIndex !== null) {
        updatedWg[editingConnIndex] = {
          ...updatedWg[editingConnIndex],
          ...newConn,
        };
      } else {
        updatedWg.push(newConn);
      }

      await api.updateVPN({
        ...vpnConfig,
        wireguard: updatedWg,
      });
      showConnModal = false;
    } catch (e) {
      console.error("Failed to save connection:", e);
    } finally {
      loading = false;
    }
  }

  async function deleteConnection(index: number) {
    if (
      !confirm(
        $t("common.delete_confirm_item", {
          values: { item: $t("item.interface") },
        }),
      )
    )
      return;
    loading = true;
    try {
      const updatedWg = connections.filter((_: any, i: number) => i !== index);
      await api.updateVPN({
        ...vpnConfig,
        wireguard: updatedWg,
      });
    } catch (e) {
      console.error("Failed to delete connection:", e);
    } finally {
      loading = false;
    }
  }

  /* --- Peer Management --- */

  function openAddPeer() {
    editingPeerIndex = null;
    peerName = "";
    peerPublicKey = "";
    peerPresharedKey = "";
    peerEndpoint = "";
    peerAllowedIps = "";
    peerKeepalive = "25";
    showPeerModal = true;
  }

  function openEditPeer(index: number) {
    editingPeerIndex = index;
    const p = connPeers[index];
    peerName = p.name || "";
    peerPublicKey = p.public_key || "";
    peerPresharedKey = p.preshared_key || "";
    peerEndpoint = p.endpoint || "";
    peerAllowedIps = (p.allowed_ips || []).join(", ");
    peerKeepalive = String(p.persistent_keepalive || "25");
    showPeerModal = true;
  }

  function savePeer() {
    const newPeer = {
      name: peerName,
      public_key: peerPublicKey,
      preshared_key: peerPresharedKey || undefined,
      endpoint: peerEndpoint || undefined,
      allowed_ips: peerAllowedIps
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
      persistent_keepalive: Number(peerKeepalive),
    };

    if (editingPeerIndex !== null) {
      connPeers[editingPeerIndex] = newPeer;
    } else {
      connPeers = [...connPeers, newPeer];
    }
    showPeerModal = false;
  }

  function deletePeer(index: number) {
    if (
      !confirm(
        $t("common.delete_confirm_item", {
          values: { item: $t("item.peer") },
        }),
      )
    )
      return;
    connPeers = connPeers.filter((_, i) => i !== index);
  }

  function generateKey() {
    // TODO: Implement key generation via API or WASM?
    // For now just placeholders or instruct user.
    alert(
      "Key generation not implemented in UI yet. Please generate elsewhere.",
    );
  }
</script>

<div class="vpn-page">
  <div class="page-header">
    <div class="header-actions">
      <Button onclick={openAddConnection}
        >+ {$t("common.add_item", {
          values: { item: $t("item.interface") },
        })}</Button
      >
    </div>
  </div>

  {#if connections.length === 0}
    <Card>
      <p class="empty-message">
        {$t("common.no_items", { values: { items: $t("item.interface") } })}
      </p>
    </Card>
  {:else}
    <div class="connections-list">
      {#each connections as conn, i}
        <Card>
          <div class="conn-header">
            <div class="flex items-center gap-3">
              <h3 class="text-lg font-bold">{conn.name}</h3>
              <Badge variant="outline">{conn.interface}</Badge>
              <Badge variant={conn.enabled ? "success" : "secondary"}>
                {conn.enabled ? $t("common.enabled") : $t("common.disabled")}
              </Badge>
            </div>
            <div class="conn-actions">
              <Button variant="ghost" onclick={() => openEditConnection(i)}>
                <Icon name="edit" />
              </Button>
              <Button variant="ghost" onclick={() => deleteConnection(i)}>
                <Icon name="delete" />
              </Button>
            </div>
          </div>

          <div
            class="conn-details mt-4 grid grid-cols-2 md:grid-cols-4 gap-4 text-sm"
          >
            <div>
              <span class="text-muted-foreground block text-xs"
                >{$t("vpn.port")}</span
              >
              <span class="font-mono">{conn.listen_port}</span>
            </div>
            <div>
              <span class="text-muted-foreground block text-xs"
                >{$t("vpn.address")}</span
              >
              <span class="font-mono">{(conn.address || []).join(", ")}</span>
            </div>
            <div>
              <span class="text-muted-foreground block text-xs"
                >{$t("vpn.dns")}</span
              >
              <span class="font-mono">{(conn.dns || []).join(", ")}</span>
            </div>
            <div>
              <span class="text-muted-foreground block text-xs"
                >{$t("vpn.peers")}</span
              >
              <span>{(conn.peers || []).length}</span>
            </div>
          </div>
        </Card>
      {/each}
    </div>
  {/if}
</div>

<!-- Connection Modal -->
<Modal
  bind:open={showConnModal}
  title={editingConnIndex !== null
    ? $t("common.edit_item", { values: { item: $t("item.interface") } })
    : $t("common.add_item", { values: { item: $t("item.interface") } })}
  size="lg"
>
  <div class="form-stack">
    <div class="grid grid-cols-2 gap-4">
      <Input
        id="conn-name"
        label={$t("common.name")}
        bind:value={connName}
        placeholder={$t("vpn.name")}
      />
      <Input
        id="conn-iface"
        label={$t("common.interface")}
        bind:value={connInterface}
        placeholder="wg0"
      />
    </div>

    <div class="grid grid-cols-3 gap-4">
      <Input
        id="conn-port"
        label={$t("vpn.listen_port")}
        type="number"
        bind:value={connPort}
      />
      <Input
        id="conn-mtu"
        label={$t("common.mtu")}
        type="number"
        bind:value={connMtu}
        placeholder="1420"
      />
      <Input
        id="conn-mark"
        label={$t("vpn.firewall_mark")}
        type="number"
        bind:value={connMark}
        placeholder={$t("common.optional")}
      />
    </div>

    <div class="flex gap-2 items-end">
      <div class="flex-1">
        <Input
          id="conn-privkey"
          label={$t("vpn.private_key")}
          bind:value={connPrivateKey}
          type="password"
          placeholder="base64 key..."
        />
      </div>
      <Button variant="outline" onclick={generateKey}
        >{$t("vpn.generate")}</Button
      >
    </div>

    <Input
      id="conn-addr"
      label={$t("vpn.addresses")}
      bind:value={connAddresses}
      placeholder="10.100.0.1/24"
    />

    <Input
      id="conn-dns"
      label={$t("vpn.dns_servers")}
      bind:value={connDns}
      placeholder="1.1.1.1"
    />

    <div class="peers-section border-t border-border pt-4 mt-2">
      <div class="flex justify-between items-center mb-3">
        <h3 class="text-sm font-medium">{$t("vpn.peers")}</h3>
        <Button size="sm" variant="outline" onclick={openAddPeer}
          >+ {$t("common.add_item", {
            values: { item: $t("item.peer") },
          })}</Button
        >
      </div>

      {#if connPeers.length > 0}
        <div class="peers-list space-y-2">
          {#each connPeers as peer, i}
            <div
              class="flex items-center justify-between p-3 bg-secondary/10 rounded-md border border-border"
            >
              <div class="grid grid-cols-3 gap-4 flex-1 text-sm">
                <div class="font-medium">{peer.name}</div>
                <div class="font-mono text-xs text-muted-foreground truncate">
                  {peer.public_key}
                </div>
                <div class="font-mono text-xs">
                  {(peer.allowed_ips || []).join(", ")}
                </div>
              </div>
              <div class="flex gap-1 ml-2">
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => openEditPeer(i)}
                >
                  <Icon name="edit" />
                </Button>
                <Button variant="ghost" size="sm" onclick={() => deletePeer(i)}>
                  <Icon name="delete" />
                </Button>
              </div>
            </div>
          {/each}
        </div>
      {:else}
        <p class="text-sm text-muted-foreground italic">
          {$t("common.no_items", { values: { items: $t("item.peer") } })}
        </p>
      {/if}
    </div>

    <div class="modal-actions">
      <Button
        variant="ghost"
        onclick={() => {
          showConnModal = false;
        }}>{$t("common.cancel")}</Button
      >
      <Button onclick={saveConnection} disabled={loading}
        >{$t("common.save_item", {
          values: { item: $t("item.interface") },
        })}</Button
      >
    </div>
  </div>
</Modal>

<!-- Peer Modal (Stacked) -->
{#if showPeerModal}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
  >
    <div
      class="bg-background border border-border rounded-lg shadow-xl w-full max-w-md p-6 m-4"
      role="dialog"
    >
      <h3 class="text-lg font-semibold mb-4">
        {editingPeerIndex !== null
          ? $t("common.edit_item", { values: { item: $t("item.peer") } })
          : $t("common.add_item", { values: { item: $t("item.peer") } })}
      </h3>

      <div class="form-stack space-y-4">
        <Input
          id="peer-name"
          label={$t("common.name")}
          bind:value={peerName}
          required
        />
        <Input
          id="peer-pubkey"
          label={$t("vpn.public_key")}
          bind:value={peerPublicKey}
          required
          placeholder="base64..."
        />
        <Input
          id="peer-psk"
          label={$t("vpn.preshared_key")}
          bind:value={peerPresharedKey}
          type="password"
        />
        <Input
          id="peer-endpoint"
          label={$t("vpn.endpoint")}
          bind:value={peerEndpoint}
          placeholder="ip:port"
        />
        <Input
          id="peer-ips"
          label={$t("vpn.allowed_ips")}
          bind:value={peerAllowedIps}
          placeholder="0.0.0.0/0"
        />
        <Input
          id="peer-ka"
          label={$t("vpn.keepalive")}
          type="number"
          bind:value={peerKeepalive}
        />

        <div class="flex justify-end gap-2 mt-6 pt-4 border-t border-border">
          <Button variant="ghost" onclick={() => (showPeerModal = false)}
            >{$t("common.cancel")}</Button
          >
          <Button onclick={savePeer} disabled={!peerName || !peerPublicKey}
            >{$t("common.save_item", {
              values: { item: $t("item.peer") },
            })}</Button
          >
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .vpn-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .connections-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .conn-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding-bottom: var(--space-2);
    border-bottom: 1px solid var(--color-border);
  }

  .empty-message {
    color: var(--color-muted);
    text-align: center;
    margin: 0;
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
