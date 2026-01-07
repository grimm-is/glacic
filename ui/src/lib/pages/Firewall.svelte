<script lang="ts">
  /**
   * Firewall Page
   * Policy and rule management
   *
   * Supports two views:
   * - Classic: Card-based policy groups
   * - ClearPath: Unified rule table with sparklines
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
    PillInput,
    Toggle,
  } from "$lib/components";
  import { PolicyEditor } from "$lib/components/policy";
  import { t } from "svelte-i18n";
  import {
    SERVICE_GROUPS,
    getService,
    type ServiceDefinition,
  } from "$lib/data/common_services";
  import { NetworkInput } from "$lib/components";
  import { getAddressType } from "$lib/utils/validation";

  // View mode: 'classic' or 'clearpath'
  let viewMode = $state<"classic" | "clearpath">("classic");

  let loading = $state(false);
  let showRuleModal = $state(false);
  let selectedPolicy = $state<any>(null);
  let editingRuleIndex = $state<number | null>(null);
  let isEditMode = $derived(editingRuleIndex !== null);

  // Rule form
  let ruleAction = $state("accept");
  let ruleName = $state("");
  // Protocol selection (array for PillInput)
  let protocols = $state<string[]>([]);
  let ruleDestPort = $state("");
  let ruleSrc = $state("");
  let ruleDest = $state("");
  let selectedService = $state("");

  // Advanced options state
  let showAdvanced = $state(false);
  let invertSrc = $state(false);
  let invertDest = $state(false);
  let tcpFlagsArray = $state<string[]>([]);
  let maxConnections = $state("");

  // Protocol options for PillInput
  const PROTOCOL_OPTIONS = [
    { value: "tcp", label: "TCP" },
    { value: "udp", label: "UDP" },
    { value: "icmp", label: "ICMP" },
  ];

  // TCP Flags options for PillInput
  const TCP_FLAG_OPTIONS = [
    { value: "syn", label: "SYN" },
    { value: "ack", label: "ACK" },
    { value: "fin", label: "FIN" },
    { value: "rst", label: "RST" },
    { value: "psh", label: "PSH" },
    { value: "urg", label: "URG" },
  ];

  // Reactive: when service changes, update protocol and port
  $effect(() => {
    if (selectedService) {
      const svc = getService(selectedService);
      if (svc) {
        // Set protocols array
        if (svc.protocol === "both") {
          protocols = ["tcp", "udp"];
        } else if (svc.protocol === "tcp") {
          protocols = ["tcp"];
        } else if (svc.protocol === "udp") {
          protocols = ["udp"];
        }
        ruleDestPort = svc.port?.toString() || "";
        // Auto-generate rule name if empty
        if (!ruleName) {
          ruleName = `Allow ${svc.label}`;
        }
      }
    }
  });

  // Add Policy modal state
  let showPolicyModal = $state(false);
  let policyFrom = $state("");
  let policyTo = $state("");

  const zones = $derived($config?.zones || []);
  const zoneNames = $derived(zones.map((z: any) => z.name));
  const policies = $derived($config?.policies || []);
  const ipsets = $derived($config?.ipsets || []);
  const availableIPSets = $derived(
    (Array.isArray(ipsets) ? ipsets : [])
      .map((s: any) => s?.name)
      .filter((n: any) => n)
      .sort(),
  );

  const policyColumns = [
    { key: "from", label: "From" },
    { key: "to", label: "To" },
    { key: "ruleCount", label: "Rules" },
  ];

  const policyData = $derived(
    policies.map((p: any) => ({
      ...p,
      ruleCount: p.rules?.length || 0,
    })),
  );

  // Status values from backend: "live", "pending_add", "pending_edit", "pending_delete"
  type ItemStatus = "live" | "pending_add" | "pending_edit" | "pending_delete";

  // Get CSS class for item status
  function getStatusClass(status: ItemStatus | undefined): string {
    switch (status) {
      case "pending_add":
        return "pending-add";
      case "pending_edit":
        return "pending-modify";
      case "pending_delete":
        return "pending-delete";
      default:
        return "";
    }
  }

  // Get badge text for item status
  function getStatusBadgeText(status: ItemStatus | undefined): string {
    switch (status) {
      case "pending_add":
        return "NEW";
      case "pending_edit":
        return "CHANGED";
      case "pending_delete":
        return "DELETED";
      default:
        return "";
    }
  }

  // Check if item has pending status
  function isPending(status: ItemStatus | undefined): boolean {
    return status !== undefined && status !== "live";
  }

  function getActionBadge(action: string) {
    switch (action) {
      case "accept":
        return "success";
      case "drop":
        return "destructive";
      case "reject":
        return "warning";
      default:
        return "secondary";
    }
  }

  function openAddRule(policy: any) {
    selectedPolicy = policy;
    editingRuleIndex = null;
    ruleAction = "accept";
    ruleName = "";
    // Reset protocols array (empty = any)
    protocols = [];
    ruleDestPort = "";
    ruleDestPort = "";
    ruleSrc = "";
    ruleDest = "";
    selectedService = "";
    // Reset advanced options
    showAdvanced = false;
    invertSrc = false;
    invertDest = false;
    tcpFlagsArray = [];
    maxConnections = "";
    showRuleModal = true;
  }

  function openEditRule(policy: any, ruleIndex: number) {
    selectedPolicy = policy;
    editingRuleIndex = ruleIndex;
    const rule = policy.rules[ruleIndex];
    ruleAction = rule.action || "accept";
    ruleName = rule.name || "";
    // Parse protocol - could be "tcp", "udp", "tcp,udp", etc. into array
    const proto = rule.proto || "";
    protocols = proto
      ? proto
          .split(",")
          .map((p: string) => p.trim())
          .filter((p: string) => p)
      : [];
    ruleDestPort =
      rule.dest_port?.toString() || rule.dest_ports?.join(",") || "";
    rule.dest_port?.toString() || rule.dest_ports?.join(",") || "";

    // Handle Source (IPSet or IP)
    if (rule.src_ipset) {
      ruleSrc = rule.src_ipset;
    } else if (
      rule.src_ip &&
      Array.isArray(rule.src_ip) &&
      rule.src_ip.length > 0
    ) {
      ruleSrc = rule.src_ip[0]; // TODO: Support multiple
    } else if (rule.src_ip) {
      ruleSrc = rule.src_ip as string;
    } else {
      ruleSrc = "";
    }

    // Handle Dest (IPSet or IP)
    if (rule.dest_ipset) {
      ruleDest = rule.dest_ipset;
    } else if (
      rule.dest_ip &&
      Array.isArray(rule.dest_ip) &&
      rule.dest_ip.length > 0
    ) {
      ruleDest = rule.dest_ip[0];
    } else if (rule.dest_ip) {
      ruleDest = rule.dest_ip as string;
    } else {
      ruleDest = "";
    }
    // Load advanced options
    invertSrc = rule.invert_src || false;
    invertDest = rule.invert_dest || false;
    // Parse tcp_flags into array
    const flags = rule.tcp_flags || "";
    tcpFlagsArray = flags
      ? flags
          .split(",")
          .map((f: string) => f.trim())
          .filter((f: string) => f && !f.startsWith("!"))
      : [];
    maxConnections = rule.max_connections?.toString() || "";
    showAdvanced = !!(
      invertSrc ||
      invertDest ||
      tcpFlagsArray.length > 0 ||
      maxConnections
    );
    showRuleModal = true;
  }

  async function saveRule() {
    if (!selectedPolicy || !ruleName) return;

    loading = true;
    try {
      // Protocol string from array
      const protoString =
        protocols.length > 0 ? protocols.join(",") : undefined;

      // Parse ports - support comma-separated and ranges (e.g., "80,443" or "3000-3010")
      let destPorts: number[] = [];
      if (ruleDestPort && ruleDestPort.trim()) {
        const parts = ruleDestPort.split(",").map((p: string) => p.trim());
        for (const part of parts) {
          if (part.includes("-")) {
            const [start, end] = part
              .split("-")
              .map((n: string) => parseInt(n.trim()));
            if (!isNaN(start) && !isNaN(end)) {
              for (let i = start; i <= end; i++) destPorts.push(i);
            }
          } else {
            const port = parseInt(part);
            if (!isNaN(port)) destPorts.push(port);
          }
        }
      }

      const newRule: any = {
        action: ruleAction,
        name: ruleName,
        proto: protoString,
        // Use dest_ports for multiple, dest_port for single
        dest_port: destPorts.length === 1 ? destPorts[0] : undefined,
        dest_ports: destPorts.length > 1 ? destPorts : undefined,
      };

      // Map Source/Dest based on type
      const srcType = getAddressType(ruleSrc);
      if (srcType === "name") {
        newRule.src_ipset = ruleSrc;
      } else if (ruleSrc) {
        // IP, CIDR, or Hostname
        newRule.src_ip = [ruleSrc]; // Backend expects array
      }

      const destType = getAddressType(ruleDest);
      if (destType === "name") {
        newRule.dest_ipset = ruleDest;
      } else if (ruleDest) {
        newRule.dest_ip = [ruleDest];
      }
      // Advanced options
      if (invertSrc) newRule.invert_src = true;
      if (invertDest) newRule.invert_dest = true;
      if (tcpFlagsArray.length > 0) newRule.tcp_flags = tcpFlagsArray.join(",");
      if (maxConnections) newRule.max_connections = parseInt(maxConnections);

      const updatedPolicies = policies.map((p: any) => {
        if (p.from === selectedPolicy.from && p.to === selectedPolicy.to) {
          if (isEditMode && editingRuleIndex !== null) {
            // Edit existing rule
            const newRules = [...(p.rules || [])];
            newRules[editingRuleIndex] = newRule;
            return { ...p, rules: newRules };
          } else {
            // Add new rule
            return { ...p, rules: [...(p.rules || []), newRule] };
          }
        }
        return p;
      });

      await api.updatePolicies(updatedPolicies);
      showRuleModal = false;
    } catch (e) {
      console.error("Failed to save rule:", e);
    } finally {
      loading = false;
    }
  }

  async function deletePolicy(policy: any) {
    if (
      !confirm(
        $t("common.delete_confirm_item", {
          values: { item: $t("item.policy") },
        }),
      )
    ) {
      return;
    }

    loading = true;
    try {
      const updatedPolicies = policies.filter(
        (p: any) => !(p.from === policy.from && p.to === policy.to),
      );
      await api.updatePolicies(updatedPolicies);
    } catch (e) {
      console.error("Failed to delete policy:", e);
    } finally {
      loading = false;
    }
  }

  async function deleteRule(policy: any, ruleIndex: number) {
    const rule = policy.rules[ruleIndex];
    if (
      !confirm(
        $t("common.delete_confirm_item", {
          values: { item: $t("item.rule") },
        }),
      )
    ) {
      return;
    }

    loading = true;
    try {
      const updatedPolicies = policies.map((p: any) => {
        if (p.from === policy.from && p.to === policy.to) {
          const newRules = [...p.rules];
          newRules.splice(ruleIndex, 1);
          return { ...p, rules: newRules };
        }
        return p;
      });
      await api.updatePolicies(updatedPolicies);
    } catch (e) {
      console.error("Failed to delete rule:", e);
    } finally {
      loading = false;
    }
  }

  function openAddPolicy() {
    policyFrom = zoneNames[0] || "";
    policyTo = zoneNames[1] || zoneNames[0] || "";
    showPolicyModal = true;
  }

  async function savePolicy() {
    if (!policyFrom || !policyTo) return;

    // Check for duplicate
    const exists = policies.some(
      (p: any) => p.from === policyFrom && p.to === policyTo,
    );
    if (exists) {
      alert(`Policy ${policyFrom} → ${policyTo} already exists`);
      return;
    }

    loading = true;
    try {
      const newPolicy = {
        from: policyFrom,
        to: policyTo,
        rules: [],
      };
      await api.updatePolicies([...policies, newPolicy]);
      showPolicyModal = false;
    } catch (e) {
      console.error("Failed to create policy:", e);
    } finally {
      loading = false;
    }
  }
</script>

<div class="firewall-page">
  <div class="page-header">
    <!-- View Mode Toggle -->
    <div class="view-toggle">
      <button
        class="toggle-btn"
        class:active={viewMode === "clearpath"}
        onclick={() => (viewMode = "clearpath")}
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          class="icon"
        >
          <path d="M4 6h16M4 12h16M4 18h16" />
        </svg>
        ClearPath
      </button>
      <button
        class="toggle-btn"
        class:active={viewMode === "classic"}
        onclick={() => (viewMode = "classic")}
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          class="icon"
        >
          <rect x="3" y="3" width="7" height="7" /><rect
            x="14"
            y="3"
            width="7"
            height="7"
          />
          <rect x="3" y="14" width="7" height="7" /><rect
            x="14"
            y="14"
            width="7"
            height="7"
          />
        </svg>
        Classic
      </button>
    </div>

    {#if viewMode === "classic"}
      <Button onclick={openAddPolicy}
        >+ {$t("common.add_item", {
          values: { item: $t("item.policy") },
        })}</Button
      >
    {/if}
  </div>

  <!-- ClearPath View: Unified Rule Table -->
  {#if viewMode === "clearpath"}
    <div class="clearpath-container">
      <PolicyEditor title="" showGroupFilter={true} />
    </div>

    <!-- Classic View: Card-based policies -->
  {:else if policies.length === 0}
    <Card>
      <p class="empty-message">
        {$t("common.no_items", { values: { items: $t("item.policy") } })}
      </p>
    </Card>
  {:else}
    <div class="policies-grid">
      {#each policies as policy (policy.from + "-" + policy.to)}
        <Card>
          <div class="policy-card-inner {getStatusClass(policy._status)}">
            <div class="policy-header">
              <div class="policy-zones">
                <Badge variant="outline">{policy.from}</Badge>
                <span class="policy-arrow">→</span>
                <Badge variant="outline">{policy.to}</Badge>
                {#if isPending(policy._status)}
                  <span class="pending-badge {policy._status}"
                    >{getStatusBadgeText(policy._status)}</span
                  >
                {/if}
              </div>
              <div class="policy-actions">
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => openAddRule(policy)}
                  >+ {$t("common.add_item", {
                    values: { item: $t("item.rule") },
                  })}</Button
                >
                <Button
                  variant="ghost"
                  size="sm"
                  onclick={() => deletePolicy(policy)}>🗑️</Button
                >
              </div>
            </div>

            <div class="rules-list">
              {#if policy.rules?.length > 0}
                {#each policy.rules as rule, ruleIndex (rule.name)}
                  <div class="rule-item">
                    <Badge variant={getActionBadge(rule.action)}
                      >{rule.action}</Badge
                    >
                    <span class="rule-name">{rule.name}</span>
                    {#if rule.protocol}
                      <span class="rule-detail">{rule.protocol}</span>
                    {/if}
                    {#if rule.src_ipset}
                      <Badge variant="outline"
                        >src:{rule.src_ipset.replace("tag_", "@")}</Badge
                      >
                    {/if}
                    {#if rule.dest_ipset}
                      <Badge variant="outline"
                        >dst:{rule.dest_ipset.replace("tag_", "@")}</Badge
                      >
                    {/if}
                    {#if rule.dest_port}
                      <span class="rule-detail">:{rule.dest_port}</span>
                    {/if}
                    <button
                      class="rule-edit"
                      onclick={() => openEditRule(policy, ruleIndex)}
                      title="Edit rule">✎</button
                    >
                    <button
                      class="rule-delete"
                      onclick={() => deleteRule(policy, ruleIndex)}
                      title="Delete rule">×</button
                    >
                  </div>
                {/each}
              {:else}
                <p class="no-rules">
                  {$t("common.no_items", {
                    values: { items: $t("item.rule") },
                  })}
                </p>
              {/if}
            </div>
          </div></Card
        >
      {/each}
    </div>
  {/if}
</div>

<!-- Add/Edit Rule Modal -->
<Modal
  bind:open={showRuleModal}
  title={isEditMode
    ? $t("common.edit_item", { values: { item: $t("item.rule") } })
    : $t("common.add_item", { values: { item: $t("item.rule") } })}
>
  <div class="form-stack">
    <Input
      id="rule-name"
      label={$t("common.name")}
      bind:value={ruleName}
      placeholder={$t("firewall.rule_name_placeholder")}
      required
    />

    <Select
      id="rule-action"
      label={$t("common.action")}
      bind:value={ruleAction}
      options={[
        { value: "accept", label: $t("firewall.accept") },
        { value: "drop", label: $t("firewall.drop") },
        { value: "reject", label: $t("firewall.reject") },
      ]}
    />

    <Select
      id="rule-service"
      label={$t("firewall.quick_service")}
      bind:value={selectedService}
      options={[
        { value: "", label: "-- Custom / Manual --" },
        ...SERVICE_GROUPS.flatMap((group) =>
          group.services.map((svc) => ({
            value: svc.name,
            label: `${group.label}: ${svc.label} (${svc.protocol === "both" ? "tcp+udp" : svc.protocol}/${svc.port})`,
          })),
        ),
      ]}
    />

    <PillInput
      id="rule-protocols"
      label={$t("common.protocol")}
      bind:value={protocols}
      options={PROTOCOL_OPTIONS}
      placeholder={$t("firewall.protocol_placeholder")}
    />

    {#if !protocols.includes("icmp") || protocols.includes("tcp") || protocols.includes("udp")}
      <Input
        id="rule-port"
        label={$t("firewall.dest_port")}
        bind:value={ruleDestPort}
        placeholder={$t("firewall.dest_port_placeholder")}
        type="text"
      />
    {/if}

    <div class="form-row">
      <NetworkInput
        id="rule-src"
        label={$t("firewall.source")}
        bind:value={ruleSrc}
        suggestions={availableIPSets}
        placeholder="IP, CIDR, Host, or IPSet"
      />
      <NetworkInput
        id="rule-dest"
        label={$t("firewall.destination")}
        bind:value={ruleDest}
        suggestions={availableIPSets}
        placeholder="IP, CIDR, Host, or IPSet"
      />
    </div>

    <!-- Advanced Options (collapsible) -->
    <details class="advanced-options" bind:open={showAdvanced}>
      <summary>{$t("firewall.advanced_options")}</summary>
      <div class="advanced-content">
        <Toggle label={$t("firewall.invert_src")} bind:checked={invertSrc} />
        <Toggle label={$t("firewall.invert_dest")} bind:checked={invertDest} />

        <PillInput
          id="rule-tcp-flags"
          label={$t("firewall.tcp_flags")}
          bind:value={tcpFlagsArray}
          options={TCP_FLAG_OPTIONS}
          placeholder={$t("firewall.tcp_flags_placeholder")}
        />

        <Input
          id="rule-max-connections"
          label={$t("firewall.max_connections")}
          bind:value={maxConnections}
          placeholder={$t("firewall.max_connections_placeholder")}
          type="number"
        />
      </div>
    </details>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showRuleModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button onclick={saveRule} disabled={loading || !ruleName}>
        {#if loading}<Spinner size="sm" />{/if}
        {$t("common.save_item", { values: { item: $t("item.rule") } })}
      </Button>
    </div>
  </div>
</Modal>

<!-- Add Policy Modal -->
<Modal bind:open={showPolicyModal} title="Add Policy">
  <div class="form-stack" role="form" aria-label="Add new firewall policy">
    <Select
      id="policy-from"
      label={$t("firewall.from_zone")}
      bind:value={policyFrom}
      options={zoneNames.map((n: string) => ({ value: n, label: n }))}
      required
    />

    <Select
      id="policy-to"
      label={$t("firewall.to_zone")}
      bind:value={policyTo}
      options={zoneNames.map((n: string) => ({ value: n, label: n }))}
      required
    />

    <p class="form-hint">
      Traffic from <strong>{policyFrom || "?"}</strong> →
      <strong>{policyTo || "?"}</strong>
    </p>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showPolicyModal = false)}
        >{$t("common.cancel")}</Button
      >
      <Button
        onclick={savePolicy}
        disabled={loading || !policyFrom || !policyTo}
      >
        {#if loading}<Spinner size="sm" />{/if}
        {$t("common.create_item", { values: { item: $t("item.policy") } })}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .firewall-page {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
    height: 100%;
  }

  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .view-toggle {
    display: flex;
    gap: var(--space-1);
    background: var(--color-backgroundSecondary);
    border-radius: var(--radius-md);
    padding: var(--space-1);
  }

  .toggle-btn {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-2) var(--space-3);
    border: none;
    background: transparent;
    color: var(--color-muted);
    font-size: var(--text-sm);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all var(--transition-fast);
  }

  .toggle-btn:hover {
    color: var(--color-foreground);
  }

  .toggle-btn.active {
    background: var(--color-primary);
    color: white;
  }

  .toggle-btn .icon {
    width: 16px;
    height: 16px;
  }

  .clearpath-container {
    flex: 1;
    min-height: 0;
    background: var(--color-backgroundSecondary);
    border-radius: var(--radius-lg);
    overflow: hidden;
  }

  .policies-grid {
    display: grid;
    gap: var(--space-4);
  }

  .policy-card-inner {
    padding: var(--space-3);
    border-radius: var(--radius-md);
    transition:
      background-color var(--transition-fast),
      border-color var(--transition-fast);
  }

  .policy-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-bottom: var(--space-3);
    border-bottom: 1px solid var(--color-border);
    margin-bottom: var(--space-3);
  }

  .policy-zones {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .policy-arrow {
    color: var(--color-muted);
    font-weight: 600;
  }

  .rules-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .rule-item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
    background-color: var(--color-backgroundSecondary);
    border-radius: var(--radius-md);
  }

  .rule-name {
    font-weight: 500;
    color: var(--color-foreground);
  }

  .rule-detail {
    font-size: var(--text-sm);
    color: var(--color-muted);
    font-family: var(--font-mono);
  }

  .no-rules {
    color: var(--color-muted);
    font-size: var(--text-sm);
    margin: 0;
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

  .policy-actions {
    display: flex;
    gap: var(--space-1);
  }

  .rule-delete {
    margin-left: auto;
    background: none;
    border: none;
    color: var(--color-muted);
    cursor: pointer;
    font-size: 1rem;
    padding: var(--space-1);
    border-radius: var(--radius-sm);
    transition:
      color var(--transition-fast),
      background-color var(--transition-fast);
  }

  .rule-delete:hover {
    color: var(--color-destructive);
    background-color: var(
      --color-destructive-foreground,
      rgba(220, 38, 38, 0.1)
    );
  }

  .rule-edit {
    margin-left: auto;
    background: none;
    border: none;
    color: var(--color-muted);
    cursor: pointer;
    font-size: 1rem;
    padding: var(--space-1);
    border-radius: var(--radius-sm);
    transition:
      color var(--transition-fast),
      background-color var(--transition-fast);
  }

  .rule-edit:hover {
    color: var(--color-primary);
    background-color: rgba(59, 130, 246, 0.1);
  }

  .form-hint {
    font-size: var(--text-sm);
    color: var(--color-muted);
    margin: 0;
    padding: var(--space-2);
    background: var(--color-backgroundSecondary);
    border-radius: var(--radius-sm);
  }

  .form-hint strong {
    color: var(--color-foreground);
  }
</style>
