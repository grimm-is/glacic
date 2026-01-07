<script lang="ts">
    import { onMount } from "svelte";
    import { api } from "$lib/stores/app";
    import Card from "$lib/components/Card.svelte";
    import Button from "$lib/components/Button.svelte";
    import Badge from "$lib/components/Badge.svelte";
    import { t } from "svelte-i18n";

    type Rule = {
        id: string;
        src_ip: string;
        src_mac: string;
        vendor?: string;
        dst_ip: string;
        dst_port: number;
        dst_hostname?: string;
        protocol: string;
        policy: string;
    };

    let rules = $state<Rule[]>([]);
    let loading = $state(false);
    let error = $state("");
    let filter = $state("pending"); // pending, approved, denied, ignored

    async function loadRules() {
        loading = true;
        error = "";
        try {
            // API call to get rules by status
            // We might need to implement query params in getPendingRules API or filter client side?
            // Existing API: GET /api/learning/rules returns map "pending": [], "approved": [] etc using GetPendingRules("pending") logic?
            // Let's check api handler later. Assuming generic endpoint or filtered.
            // Based on handler, it takes ?status=xxx
            const res = await apiRequest(`/learning/rules?status=${filter}`);
            rules = res || [];
        } catch (e: any) {
            error = e.message;
        } finally {
            loading = false;
        }
    }

    // Helper for direct API request since it's not in api store yet
    async function apiRequest(endpoint: string, options: RequestInit = {}) {
        const res = await fetch(`/api${endpoint}`, {
            ...options,
            credentials: "include",
            headers: { "Content-Type": "application/json", ...options.headers },
        });

        // Handle non-JSON responses (e.g., HTML error pages)
        const contentType = res.headers.get("content-type");
        if (!contentType?.includes("application/json")) {
            throw new Error("Learning service unavailable");
        }

        if (!res.ok) {
            const data = await res.json();
            throw new Error(data.error || `HTTP ${res.status}`);
        }
        return res.json();
    }

    async function approveRule(id: string) {
        try {
            await apiRequest(`/learning/rules/${id}/approve`, {
                method: "POST",
            });
            loadRules();
        } catch (e: any) {
            alert(
                $t("learning.failed_approve", { values: { err: e.message } }),
            );
        }
    }

    async function denyRule(id: string) {
        try {
            await apiRequest(`/learning/rules/${id}/deny`, { method: "POST" });
            loadRules();
        } catch (e: any) {
            alert($t("learning.failed_deny", { values: { err: e.message } }));
        }
    }

    async function deleteRule(id: string) {
        if (
            !confirm(
                $t("common.delete_confirm_item", {
                    values: { item: $t("item.rule") },
                }),
            )
        )
            return;
        try {
            await apiRequest(`/learning/rules/${id}`, { method: "DELETE" });
            loadRules();
        } catch (e: any) {
            alert($t("learning.failed_delete", { values: { err: e.message } }));
        }
    }

    $effect(() => {
        loadRules();
    });
</script>

<div class="page-header">
    <div class="tabs">
        <button
            class:active={filter === "pending"}
            onclick={() => (filter = "pending")}
            >{$t("learning.pending")}</button
        >
        <button
            class:active={filter === "approved"}
            onclick={() => (filter = "approved")}
            >{$t("learning.approved")}</button
        >
        <button
            class:active={filter === "denied"}
            onclick={() => (filter = "denied")}>{$t("learning.denied")}</button
        >
    </div>
</div>

{#if error}
    <div class="error">{error}</div>
{/if}

<Card>
    <div class="table-container">
        {#if loading}
            <div class="p-4 text-center">{$t("learning.loading")}</div>
        {:else if rules.length === 0}
            <div class="p-4 text-center text-muted">
                {$t("learning.no_rules")}
            </div>
        {:else}
            <table>
                <thead>
                    <tr>
                        <th>{$t("common.source")}</th>
                        <th>{$t("common.destination")}</th>
                        <th>{$t("common.protocol")}</th>
                        <th>{$t("learning.reason")}</th>
                        <th class="text-right">{$t("learning.actions")}</th>
                    </tr>
                </thead>
                <tbody>
                    {#each rules as rule}
                        <tr>
                            <td>
                                <div class="cell-stack">
                                    <span class="font-mono">{rule.src_ip}</span>
                                    <span class="text-xs text-muted">
                                        {rule.src_mac}
                                        {#if rule.vendor}({rule.vendor}){/if}
                                    </span>
                                </div>
                            </td>
                            <td>
                                <div class="cell-stack">
                                    <span class="font-mono"
                                        >{rule.dst_ip}:{rule.dst_port}</span
                                    >
                                    {#if rule.dst_hostname}
                                        <span class="text-xs text-muted"
                                            >{rule.dst_hostname}</span
                                        >
                                    {/if}
                                </div>
                            </td>
                            <td>
                                <Badge variant="outline">{rule.protocol}</Badge>
                            </td>
                            <td class="text-sm">{rule.policy}</td>
                            <td class="text-right">
                                <div class="actions">
                                    {#if filter === "pending"}
                                        <Button
                                            size="sm"
                                            variant="default"
                                            onclick={() => approveRule(rule.id)}
                                            >{$t("learning.allow")}</Button
                                        >
                                        <Button
                                            size="sm"
                                            variant="destructive"
                                            onclick={() => denyRule(rule.id)}
                                            >{$t("learning.block")}</Button
                                        >
                                    {:else}
                                        <Button
                                            size="sm"
                                            variant="ghost"
                                            onclick={() => deleteRule(rule.id)}
                                            >{$t("common.delete")}</Button
                                        >
                                    {/if}
                                </div>
                            </td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        {/if}
    </div>
</Card>

<style>
    .page-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: var(--space-4);
    }

    .tabs {
        display: flex;
        gap: var(--space-2);
        background: var(--color-surface);
        padding: var(--space-1);
        border-radius: var(--radius-md);
        border: 1px solid var(--color-border);
    }

    .tabs button {
        background: none;
        border: none;
        padding: var(--space-2) var(--space-4);
        border-radius: var(--radius-sm);
        color: var(--color-muted);
        cursor: pointer;
        font-size: var(--text-sm);
        font-weight: 500;
    }

    .tabs button.active {
        background: var(--color-background);
        color: var(--color-foreground);
        box-shadow: var(--shadow-sm);
    }

    .table-container {
        overflow-x: auto;
    }

    table {
        width: 100%;
        border-collapse: collapse;
    }

    th,
    td {
        padding: var(--space-3);
        text-align: left;
        border-bottom: 1px solid var(--color-border);
    }

    th {
        font-size: var(--text-xs);
        font-weight: 600;
        color: var(--color-muted);
        text-transform: uppercase;
        letter-spacing: 0.05em;
    }

    .cell-stack {
        display: flex;
        flex-direction: column;
    }

    .text-xs {
        font-size: var(--text-xs);
    }
    .text-sm {
        font-size: var(--text-sm);
    }
    .text-muted {
        color: var(--color-muted);
    }
    .font-mono {
        font-family: var(--font-mono);
    }
    .text-right {
        text-align: right;
    }

    .actions {
        display: flex;
        justify-content: flex-end;
        gap: var(--space-2);
    }

    .error {
        color: var(--color-destructive);
        margin-bottom: var(--space-4);
    }
</style>
