<script lang="ts">
  /**
   * NAT Page
   * Port forwarding and NAT rules
   */

  import { config, api } from "$lib/stores/app";
  import {
    Card,
    Button,
    Modal,
    Input,
    Select,
    Badge,
    Table,
    Spinner,
    Icon,
  } from "$lib/components";

  let loading = $state(false);
  let showAddRuleModal = $state(false);

  // Rule form
  let ruleType = $state<"dnat" | "masquerade" | "snat">("dnat");
  let ruleProtocol = $state("tcp");
  let ruleDestPort = $state("");
  let ruleToAddress = $state("");
  let ruleToPort = $state("");

  // Advanced fields
  let ruleSrcIP = $state("");
  let ruleSNATIP = $state("");
  let ruleMark = $state("");

  let ruleDescription = $state("");
  let ruleInterface = $state("");

  const natRules = $derived($config?.nat || []);
  const interfaces = $derived($config?.interfaces || []);

  const natColumns = [
    { key: "type", label: "Type" },
    { key: "protocol", label: "Protocol" },
    { key: "destination", label: "Dest Port" },
    { key: "to_address", label: "Forward To" },
    { key: "snat_ip", label: "SNAT IP" },
    { key: "description", label: "Description" },
  ];

  function openAddRule() {
    ruleType = "dnat";
    ruleProtocol = "tcp";
    ruleDestPort = "";
    ruleToAddress = "";
    ruleToPort = "";
    ruleSrcIP = "";
    ruleSNATIP = "";
    ruleMark = "";
    ruleDescription = "";
    try {
      const wanIface = interfaces.find((i: any) => i.Zone === "WAN");
      ruleInterface = wanIface?.Name || interfaces[0]?.Name || "";
    } catch (e) {
      console.warn("Error selecting default interface", e);
      ruleInterface = "";
    }
    showAddRuleModal = true;
  }

  async function saveRule() {
    loading = true;
    try {
      let newRule: any;

      if (ruleType === "masquerade") {
        newRule = {
          type: "masquerade",
          interface: ruleInterface,
        };
      } else if (ruleType === "snat") {
        if (!ruleSNATIP) return;
        newRule = {
          type: "snat",
          out_interface: ruleInterface,
          src_ip: ruleSrcIP,
          mark: parseInt(ruleMark) || 0,
          snat_ip: ruleSNATIP,
          description: ruleDescription,
        };
      } else {
        if (!ruleDestPort || !ruleToAddress) return;
        newRule = {
          type: "dnat",
          protocol: ruleProtocol,
          destination: ruleDestPort,
          to_address: ruleToAddress,
          to_port: ruleToPort || ruleDestPort,
          description: ruleDescription,
        };
      }

      await api.updateNAT([...natRules, newRule]);
      showAddRuleModal = false;
    } catch (e) {
      console.error("Failed to add NAT rule:", e);
    } finally {
      loading = false;
    }
  }

  async function deleteRule(index: number) {
    loading = true;
    try {
      const updatedRules = natRules.filter((_: any, i: number) => i !== index);
      await api.updateNAT(updatedRules);
    } catch (e) {
      console.error("Failed to delete NAT rule:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="nat-page">
  <div class="page-header">
    <h2>NAT / Port Forwarding</h2>
    <Button onclick={openAddRule}>+ Add Rule</Button>
  </div>

  {#if natRules.length === 0}
    <Card>
      <p class="empty-message">No NAT rules configured.</p>
    </Card>
  {:else}
    <Card>
      <div class="rules-list">
        {#each natRules as rule, index}
          <div class="rule-row">
            <Badge
              variant={rule.type === "masquerade" ? "secondary" : "default"}
            >
              {rule.type}
            </Badge>

            {#if rule.type === "masquerade"}
              <span class="rule-detail">Outbound on {rule.interface}</span>
            {:else if rule.type === "snat"}
              <span class="rule-detail">
                <span class="mono">{rule.src_ip || "Any"}</span>
                {#if rule.mark}
                  <Badge variant="outline">Mk:{rule.mark}</Badge>
                {/if}
                → SNAT: <span class="mono">{rule.snat_ip}</span>
                via {rule.out_interface}
              </span>
            {:else}
              <span class="rule-detail mono">
                {rule.protocol?.toUpperCase() || "TCP"}:{rule.destination} → {rule.to_address}:{rule.to_port ||
                  rule.destination}
              </span>
              {#if rule.description}
                <span class="rule-desc">{rule.description}</span>
              {/if}
            {/if}

            <Button variant="ghost" size="sm" onclick={() => deleteRule(index)}
              ><Icon name="delete" size="sm" /></Button
            >
          </div>
        {/each}
      </div>
    </Card>
  {/if}
</div>

<!-- Add Rule Modal -->
<Modal bind:open={showAddRuleModal} title="Add NAT Rule">
  <div class="form-stack">
    <Select
      id="rule-type"
      label="Rule Type"
      bind:value={ruleType}
      options={[
        { value: "dnat", label: "Port Forward (DNAT)" },
        { value: "masquerade", label: "Masquerade (Auto SNAT)" },
        { value: "snat", label: "Static SNAT" },
      ]}
    />

    {#if ruleType === "masquerade" || ruleType === "snat"}
      <Select
        id="rule-interface"
        label="Outbound Interface"
        bind:value={ruleInterface}
        options={interfaces.map((i: any) => ({
          value: i.Name,
          label: `${i.Name} (${i.Zone})`,
        }))}
      />

      {#if ruleType === "snat"}
        <Input
          id="rule-snat-ip"
          label="SNAT IP Address"
          bind:value={ruleSNATIP}
          placeholder="e.g. 1.2.3.4"
          required
        />

        <Input
          id="rule-src-ip"
          label="Source IP Match (Optional)"
          bind:value={ruleSrcIP}
          placeholder="e.g. 10.0.0.0/24"
        />

        <Input
          id="rule-mark"
          label="Firewall Mark Match (Optional)"
          bind:value={ruleMark}
          type="number"
          placeholder="e.g. 10"
        />
      {/if}
    {:else}
      <Select
        id="rule-protocol"
        label="Protocol"
        bind:value={ruleProtocol}
        options={[
          { value: "tcp", label: "TCP" },
          { value: "udp", label: "UDP" },
        ]}
      />

      <Input
        id="rule-dest"
        label="External Port"
        bind:value={ruleDestPort}
        placeholder="e.g., 443"
        type="number"
        required
      />

      <Input
        id="rule-to-addr"
        label="Forward to Address"
        bind:value={ruleToAddress}
        placeholder="e.g., 192.168.1.10"
        required
      />

      <Input
        id="rule-to-port"
        label="Forward to Port (optional)"
        bind:value={ruleToPort}
        placeholder="Same as external if blank"
        type="number"
      />

      <Input
        id="rule-desc"
        label="Description"
        bind:value={ruleDescription}
        placeholder="e.g., Web Server"
      />
    {/if}

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showAddRuleModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveRule} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        Add Rule
      </Button>
    </div>
  </div>
</Modal>

<style>
  .nat-page {
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

  .rules-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .rule-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3);
    background-color: var(--color-backgroundSecondary);
    border-radius: var(--radius-md);
  }

  .rule-detail {
    flex: 1;
    color: var(--color-foreground);
  }

  .rule-desc {
    color: var(--color-muted);
    font-size: var(--text-sm);
  }

  .mono {
    font-family: var(--font-mono);
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
