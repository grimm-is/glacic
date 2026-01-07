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
  import { t } from "svelte-i18n";

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
    { key: "protocol", label: $t("common.protocol") },
    { key: "destination", label: $t("common.destination") },
    { key: "to_address", label: "Forward To" },
    { key: "snat_ip", label: "SNAT IP" },
    { key: "description", label: $t("common.description") },
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

  // Validation error state
  let validationError = $state("");

  function validateIPv4(ip: string): boolean {
    if (!ip) return false;
    const parts = ip.split(".");
    if (parts.length !== 4) return false;
    return parts.every((p) => {
      const n = parseInt(p, 10);
      return !isNaN(n) && n >= 0 && n <= 255;
    });
  }

  function validatePort(port: string): boolean {
    if (!port) return false;
    const n = parseInt(port, 10);
    return !isNaN(n) && n >= 1 && n <= 65535;
  }

  async function saveRule() {
    validationError = "";

    // Validation
    if (ruleType === "dnat") {
      if (!validatePort(ruleDestPort)) {
        validationError = "Invalid port number (1-65535 required)";
        return;
      }
      if (!validateIPv4(ruleToAddress)) {
        validationError = "Invalid IP address format (e.g., 192.168.1.10)";
        return;
      }
      if (ruleToPort && !validatePort(ruleToPort)) {
        validationError = "Invalid forward port number";
        return;
      }
    } else if (ruleType === "snat") {
      if (!validateIPv4(ruleSNATIP)) {
        validationError = "Invalid SNAT IP address";
        return;
      }
    }

    loading = true;
    try {
      let newRule: any;

      if (ruleType === "masquerade") {
        newRule = {
          type: "masquerade",
          interface: ruleInterface,
        };
      } else if (ruleType === "snat") {
        newRule = {
          type: "snat",
          out_interface: ruleInterface,
          src_ip: ruleSrcIP,
          mark: parseInt(ruleMark) || 0,
          snat_ip: ruleSNATIP,
          description: ruleDescription,
        };
      } else {
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
    } catch (e: any) {
      validationError = `Failed to add rule: ${e.message || e}`;
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
    <Button onclick={openAddRule}
      >+ {$t("common.add_item", { values: { item: $t("item.rule") } })}</Button
    >
  </div>

  {#if natRules.length === 0}
    <Card>
      <div class="empty-state">
        <Icon name="arrow-left-right" size="lg" />
        <h3>{$t("common.no_items", { values: { items: $t("item.rule") } })}</h3>
        <p>
          {$t("nat.port_forwarding_desc")}
        </p>
        <Button onclick={openAddRule}>
          <Icon name="plus" size="sm" />
          {$t("common.create_item", { values: { item: $t("item.rule") } })}
        </Button>
      </div>
    </Card>
  {:else}
    <Card>
      <div class="rules-list">
        {#each natRules as rule, index}
          <div class="rule-row">
            <Badge
              variant={rule.type === "masquerade" ? "secondary" : "default"}
            >
              {$t(`nat.type_${rule.type}`)}
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
<Modal
  bind:open={showAddRuleModal}
  title={$t("common.add_item", { values: { item: $t("item.rule") } })}
>
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
        label={$t("common.protocol")}
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

    {#if validationError}
      <div class="validation-error" role="alert" aria-live="polite">
        {validationError}
      </div>
    {/if}

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showAddRuleModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveRule} disabled={loading}>
        {#if loading}<Spinner size="sm" />{/if}
        {$t("nat.add_rule")}
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

  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-8);
    text-align: center;
    color: var(--color-muted);
  }

  .empty-state h3 {
    margin: 0;
    font-size: var(--text-lg);
    font-weight: 600;
    color: var(--color-foreground);
  }

  .empty-state p {
    margin: 0;
    max-width: 400px;
    font-size: var(--text-sm);
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

  .validation-error {
    padding: var(--space-3);
    background-color: rgba(220, 38, 38, 0.1);
    border: 1px solid var(--color-destructive);
    border-radius: var(--radius-md);
    color: var(--color-destructive);
    font-size: var(--text-sm);
  }
</style>
