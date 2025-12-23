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
  } from "$lib/components";
  import { PolicyEditor } from "$lib/components/policy";

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
  let ruleProtocol = $state("any");
  let ruleDestPort = $state("");
  let ruleSrcIPSet = $state("");
  let ruleDestIPSet = $state("");

  const zones = $derived($config?.zones || []);
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
    ruleProtocol = "any";
    ruleDestPort = "";
    ruleSrcIPSet = "";
    ruleDestIPSet = "";
    showRuleModal = true;
  }

  function openEditRule(policy: any, ruleIndex: number) {
    selectedPolicy = policy;
    editingRuleIndex = ruleIndex;
    const rule = policy.rules[ruleIndex];
    ruleAction = rule.action || "accept";
    ruleName = rule.name || "";
    ruleProtocol = rule.protocol || "any";
    ruleDestPort = rule.dest_port?.toString() || "";
    ruleSrcIPSet = rule.src_ipset || "";
    ruleDestIPSet = rule.dest_ipset || "";
    showRuleModal = true;
  }

  async function saveRule() {
    if (!selectedPolicy || !ruleName) return;

    loading = true;
    try {
      const newRule: any = {
        action: ruleAction,
        name: ruleName,
        protocol: ruleProtocol !== "any" ? ruleProtocol : undefined,
        dest_port: ruleDestPort ? parseInt(ruleDestPort) : undefined,
      };

      if (ruleSrcIPSet) newRule.src_ipset = ruleSrcIPSet;
      if (ruleDestIPSet) newRule.dest_ipset = ruleDestIPSet;

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
        `Delete policy ${policy.from} → ${policy.to}? This will remove all rules.`,
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
    if (!confirm(`Delete rule "${rule.name}"?`)) {
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
</script>

<div class="firewall-page">
  <div class="page-header">
    <h2>Firewall Policies</h2>

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
  </div>

  <!-- ClearPath View: Unified Rule Table -->
  {#if viewMode === "clearpath"}
    <div class="clearpath-container">
      <PolicyEditor title="" showGroupFilter={true} />
    </div>

    <!-- Classic View: Card-based policies -->
  {:else if policies.length === 0}
    <Card>
      <p class="empty-message">No firewall policies configured.</p>
    </Card>
  {:else}
    <div class="policies-grid">
      {#each policies as policy}
        <Card>
          <div class="policy-header">
            <div class="policy-zones">
              <Badge variant="outline">{policy.from}</Badge>
              <span class="policy-arrow">→</span>
              <Badge variant="outline">{policy.to}</Badge>
            </div>
            <div class="policy-actions">
              <Button
                variant="ghost"
                size="sm"
                onclick={() => openAddRule(policy)}>+ Add Rule</Button
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
              {#each policy.rules as rule, ruleIndex}
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
              <p class="no-rules">No rules defined</p>
            {/if}
          </div>
        </Card>
      {/each}
    </div>
  {/if}
</div>

<!-- Add/Edit Rule Modal -->
<Modal
  bind:open={showRuleModal}
  title={isEditMode ? "Edit Firewall Rule" : "Add Firewall Rule"}
>
  <div class="form-stack">
    <Input
      id="rule-name"
      label="Rule Name"
      bind:value={ruleName}
      placeholder="e.g., Allow SSH"
      required
    />

    <Select
      id="rule-action"
      label="Action"
      bind:value={ruleAction}
      options={[
        { value: "accept", label: "Accept" },
        { value: "drop", label: "Drop" },
        { value: "reject", label: "Reject" },
      ]}
    />

    <Select
      id="rule-protocol"
      label="Protocol"
      bind:value={ruleProtocol}
      options={[
        { value: "any", label: "Any" },
        { value: "tcp", label: "TCP" },
        { value: "udp", label: "UDP" },
        { value: "icmp", label: "ICMP" },
      ]}
    />

    <div class="form-row">
      <Input
        id="rule-port"
        label="Destination Port (optional)"
        bind:value={ruleDestPort}
        placeholder="e.g., 22"
        type="number"
      />
    </div>

    <div class="form-row">
      <Select
        id="rule-src-ipset"
        label="Source IPSet (optional)"
        bind:value={ruleSrcIPSet}
        options={[
          { value: "", label: "None" },
          ...availableIPSets.map((n) => ({
            value: n,
            label: n.startsWith("tag_") ? `Tag: ${n.replace("tag_", "")}` : n,
          })),
        ]}
      />
      <Select
        id="rule-dest-ipset"
        label="Dest IPSet (optional)"
        bind:value={ruleDestIPSet}
        options={[
          { value: "", label: "None" },
          ...availableIPSets.map((n) => ({
            value: n,
            label: n.startsWith("tag_") ? `Tag: ${n.replace("tag_", "")}` : n,
          })),
        ]}
      />
    </div>

    <div class="modal-actions">
      <Button variant="ghost" onclick={() => (showRuleModal = false)}
        >Cancel</Button
      >
      <Button onclick={saveRule} disabled={loading || !ruleName}>
        {#if loading}<Spinner size="sm" />{/if}
        Add Rule
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

  .page-header h2 {
    font-size: var(--text-2xl);
    font-weight: 600;
    margin: 0;
    color: var(--color-foreground);
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
</style>
