/**
 * Rules Store - State management for ClearPath Policy Editor
 * Handles API fetching with smart stats merging to avoid UI flickering
 */

import { writable, derived, get } from 'svelte/store';

// ============================================================================
// Types
// ============================================================================

export interface ResolvedAddress {
    display_name: string;
    type: string;
    description?: string;
    count: number;
    is_truncated?: boolean;
    preview?: string[];
}

export interface RuleStats {
    packets: number;
    bytes: number;
    sparkline_data: number[];
}

export interface RuleWithStats {
    id?: string;
    name?: string;
    description?: string;
    action: string;
    protocol?: string;
    src_ip?: string;
    src_ipset?: string;
    dest_ip?: string;
    dest_ipset?: string;
    dest_port?: number;
    services?: string[];
    disabled?: boolean;
    group?: string;
    tags?: string[];
    stats?: RuleStats;
    resolved_src?: ResolvedAddress;
    resolved_dest?: ResolvedAddress;
    nft_syntax?: string;
    policy_from?: string;
    policy_to?: string;
}

export interface PolicyWithStats {
    from: string;
    to: string;
    default_action?: string;
    description?: string;
    rules: RuleWithStats[];
}

export interface GroupInfo {
    name: string;
    count: number;
}

// ============================================================================
// Stores
// ============================================================================

export const policies = writable<PolicyWithStats[]>([]);
export const flatRules = writable<RuleWithStats[]>([]);
export const groups = writable<GroupInfo[]>([]);
export const selectedGroup = writable<string | null>(null);
export const isLoading = writable(false);
export const lastError = writable<string | null>(null);

// Derived store: filtered rules by group
export const filteredRules = derived(
    [flatRules, selectedGroup],
    ([$flatRules, $selectedGroup]) => {
        if (!$selectedGroup) return $flatRules;
        return $flatRules.filter(r => r.group === $selectedGroup);
    }
);

// ============================================================================
// API Methods
// ============================================================================

const API_BASE = '/api';

async function apiRequest(endpoint: string): Promise<any> {
    const response = await fetch(`${API_BASE}${endpoint}`, {
        credentials: 'include',
    });

    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP ${response.status}`);
    }

    return response.json();
}

/**
 * Smart merge: Only update stats field to avoid re-rendering entire rule rows
 */
function mergeStats(existing: RuleWithStats[], incoming: RuleWithStats[]): RuleWithStats[] {
    if (existing.length !== incoming.length) {
        return incoming; // Structure changed, full replace
    }

    return existing.map((rule, i) => {
        const newRule = incoming[i];

        // If IDs match, just merge stats
        if (rule.id === newRule.id) {
            return {
                ...rule,
                stats: newRule.stats,
                resolved_src: newRule.resolved_src,
                resolved_dest: newRule.resolved_dest,
            };
        }

        // Rule changed position/identity, full replace
        return newRule;
    });
}

export const rulesApi = {
    _pollInterval: null as ReturnType<typeof setInterval> | null,

    /**
     * Load all policies with rules (grouped view)
     */
    async loadPolicies(withStats = true) {
        isLoading.set(true);
        lastError.set(null);

        try {
            const url = withStats ? '/rules?with_stats=true' : '/rules';
            const data = await apiRequest(url);
            policies.set(data);
            return data;
        } catch (e) {
            lastError.set(e instanceof Error ? e.message : 'Failed to load policies');
            throw e;
        } finally {
            isLoading.set(false);
        }
    },

    /**
     * Load flat rules list (ungrouped view)
     */
    async loadFlatRules(withStats = true, group?: string) {
        isLoading.set(true);
        lastError.set(null);

        try {
            let url = '/rules/flat';
            const params = new URLSearchParams();
            if (withStats) params.set('with_stats', 'true');
            if (group) params.set('group', group);
            if (params.toString()) url += '?' + params.toString();

            const data = await apiRequest(url);

            // Smart merge to preserve UI state
            const current = get(flatRules);
            if (current.length > 0 && withStats) {
                flatRules.set(mergeStats(current, data));
            } else {
                flatRules.set(data);
            }

            return data;
        } catch (e) {
            lastError.set(e instanceof Error ? e.message : 'Failed to load rules');
            throw e;
        } finally {
            isLoading.set(false);
        }
    },

    /**
     * Load available group tags
     */
    async loadGroups() {
        try {
            const data = await apiRequest('/rules/groups');
            groups.set(data);
            return data;
        } catch (e) {
            console.error('Failed to load groups', e);
            return [];
        }
    },

    /**
     * Start polling for stats updates (every 2s)
     */
    startStatsPolling() {
        this.stopStatsPolling();

        // Initial load
        this.loadFlatRules(true);

        // Poll every 2 seconds
        this._pollInterval = setInterval(() => {
            this.loadFlatRules(true, get(selectedGroup) || undefined).catch(() => {
                // Ignore polling errors to prevent crash
            });
        }, 2000);
    },

    /**
     * Stop stats polling
     */
    stopStatsPolling() {
        if (this._pollInterval) {
            clearInterval(this._pollInterval);
            this._pollInterval = null;
        }
    },

    /**
     * Select a group filter
     */
    selectGroup(group: string | null) {
        selectedGroup.set(group);
        this.loadFlatRules(true, group || undefined);
    },
};
