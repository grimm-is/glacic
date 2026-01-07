<script lang="ts">
    /**
     * PolicyEditor - Main orchestrator for ClearPath Policy Editor
     * Handles data fetching, group filtering, and rule list rendering
     */
    import { onMount, onDestroy } from "svelte";
    import RuleRow from "./RuleRow.svelte";
    import { t } from "svelte-i18n";
    import {
        rulesApi,
        flatRules,
        filteredRules,
        groups,
        selectedGroup,
        isLoading,
        lastError,
        type RuleWithStats,
    } from "$lib/stores/rules";

    export let title = "Firewall Rules";
    export let showGroupFilter = true;

    // Load data on mount
    onMount(() => {
        rulesApi.loadGroups();
        rulesApi.startStatsPolling();
    });

    // Cleanup on destroy
    onDestroy(() => {
        rulesApi.stopStatsPolling();
    });

    function handleGroupSelect(group: string | null) {
        rulesApi.selectGroup(group);
    }

    function handleToggleRule(id: string, disabled: boolean) {
        // TODO: API call to toggle rule
        console.log("Toggle rule", id, disabled);
    }

    function handleDeleteRule(id: string) {
        // TODO: API call to delete rule
        if (confirm("Delete this rule?")) {
            console.log("Delete rule", id);
        }
    }

    function handleDuplicateRule(rule: RuleWithStats) {
        // TODO: API call to duplicate
        console.log("Duplicate rule", rule);
    }

    function handleCreateRule() {
        alert("Please use the Classic view to manage rules for now.");
    }
</script>

<div class="flex flex-col h-full">
    <!-- Header -->
    <div
        class="flex items-center justify-between px-4 py-3 border-b border-gray-800 bg-gray-900"
    >
        <h2 class="text-lg font-semibold text-white">{title}</h2>

        <div class="flex items-center gap-3">
            <!-- Group Filter -->
            {#if showGroupFilter && $groups.length > 0}
                <div class="flex items-center gap-2">
                    <button
                        class="px-3 py-1.5 text-xs rounded transition-colors"
                        class:bg-blue-600={!$selectedGroup}
                        class:text-white={!$selectedGroup}
                        class:bg-gray-800={$selectedGroup}
                        class:text-gray-400={$selectedGroup}
                        on:click={() => handleGroupSelect(null)}
                    >
                        {$t("policy.all")}
                    </button>
                    {#each $groups as group (group.name)}
                        <button
                            class="px-3 py-1.5 text-xs rounded transition-colors"
                            class:bg-blue-600={$selectedGroup === group.name}
                            class:text-white={$selectedGroup === group.name}
                            class:bg-gray-800={$selectedGroup !== group.name}
                            class:text-gray-400={$selectedGroup !== group.name}
                            on:click={() => handleGroupSelect(group.name)}
                        >
                            {group.name}
                            <span class="ml-1 opacity-60">({group.count})</span>
                        </button>
                    {/each}
                </div>
            {/if}

            <!-- Add Rule Button -->
            <button
                class="flex items-center gap-2 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded transition-colors"
                on:click={handleCreateRule}
            >
                <svg
                    class="w-4 h-4"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                >
                    <path d="M12 5v14M5 12h14" />
                </svg>
                {$t("policy.add_rule")}
            </button>
        </div>
    </div>

    <!-- Loading State -->
    {#if $isLoading && $flatRules.length === 0}
        <div class="flex-1 flex items-center justify-center text-gray-500">
            <div class="animate-pulse">{$t("policy.loading_rules")}</div>
        </div>

        <!-- Error State -->
    {:else if $lastError}
        <div class="flex-1 flex items-center justify-center">
            <div
                class="text-red-400 bg-red-900/20 px-4 py-3 rounded border border-red-800"
            >
                {$lastError}
            </div>
        </div>

        <!-- Empty State -->
    {:else if $filteredRules.length === 0}
        <div
            class="flex-1 flex flex-col items-center justify-center text-gray-500 gap-4"
        >
            <svg
                class="w-16 h-16 opacity-30"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1"
            >
                <path
                    d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"
                />
            </svg>
            <p>{$t("policy.no_rules")}</p>
            <button
                class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-500 transition-colors"
            >
                {$t("policy.create_first_rule")}
            </button>
        </div>

        <!-- Rules List -->
    {:else}
        <div class="flex-1 overflow-y-auto">
            {#each $filteredRules as rule (rule.id || `${rule.policy_from}-${rule.policy_to}-${rule.name}`)}
                <RuleRow
                    {rule}
                    onToggle={handleToggleRule}
                    onDelete={handleDeleteRule}
                    onDuplicate={handleDuplicateRule}
                />
            {/each}
        </div>
    {/if}

    <!-- Footer Stats -->
    <div
        class="flex items-center justify-between px-4 py-2 border-t border-gray-800 bg-gray-900/50 text-xs text-gray-500"
    >
        <div>
            {$filteredRules.length} rule{$filteredRules.length !== 1 ? "s" : ""}
            {#if $selectedGroup}
                in "{$selectedGroup}"
            {/if}
        </div>
        <div class="flex items-center gap-2">
            {#if $isLoading}
                <span class="animate-pulse">{$t("policy.updating")}</span>
            {:else}
                <span>{$t("policy.live_stats")}</span>
            {/if}
            <span class="w-2 h-2 rounded-full bg-green-500 animate-pulse"
            ></span>
        </div>
    </div>
</div>
