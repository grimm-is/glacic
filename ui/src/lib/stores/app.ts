/**
 * Glacic API Client and State Management
 *
 * Adapted from ui-archive/src/lib/stores/app.js
 * Provides centralized state stores and API methods.
 */

import { writable, derived, get } from 'svelte/store';

// ============================================================================
// Theme State
// ============================================================================

const storedTheme = typeof localStorage !== 'undefined' ? localStorage.getItem('theme') : null;
export const theme = writable<'light' | 'dark' | 'system'>(storedTheme as any || 'system');

// Apply theme to DOM
if (typeof document !== 'undefined') {
    const updateTheme = (value: string) => {
        const isDark = value === 'dark' || (value === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);

        if (isDark) {
            document.documentElement.classList.add('dark');
        } else {
            document.documentElement.classList.remove('dark');
        }
    };

    theme.subscribe(value => {
        localStorage.setItem('theme', value);
        updateTheme(value);
    });

    // Listen for system preference changes
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (get(theme) === 'system') {
            updateTheme('system');
        }
    });
}

// ============================================================================
// Auth State
// ============================================================================

export const authStatus = writable<any>(null);
export const currentView = writable<'loading' | 'setup' | 'login' | 'app'>('loading');

// ============================================================================
// App Data
// ============================================================================

export const brand = writable({
    name: 'Glacic',
    vendor: 'Glacic',
    website: 'https://glacic.com',
    tagline: 'Network learning firewall',
});

export const config = writable<any>(null);
export const status = writable<any>(null);
export const leases = writable<any[]>([]);
export const logs = writable<any[]>([]);
// Topology Types
export interface TopologyNode {
    id: string; // IP or MAC or ID
    label: string;
    type: string;
    x?: number;
    y?: number;
    fx?: number | null;
    fy?: number | null;
    ip?: string;
    icon?: string;
    description?: string;
}

export interface TopologyLink {
    source: string | TopologyNode; // D3 replaces string ID with Node object
    target: string | TopologyNode;
}

export interface TopologyGraph {
    nodes: TopologyNode[];
    links: TopologyLink[];
}

export const topology = writable<TopologyGraph>({ nodes: [], links: [] });
export const networkDevices = writable<any[]>([]);
export const hasPendingChanges = writable<boolean>(false);

// ============================================================================
// Config Item Status (inline _status field from backend)
// ============================================================================

// Status values returned by backend on config items
export type ItemStatus = 'live' | 'pending_add' | 'pending_edit' | 'pending_delete';

// Check if a config has pending changes (from _has_pending_changes field)
export function hasPendingItems(config: any): boolean {
    return config?._has_pending_changes === true;
}

// ============================================================================
// Config Normalization (lowercase -> PascalCase for UI compatibility)
// ============================================================================

/**
 * Normalize interface object field names from lowercase (config JSON)
 * to PascalCase (UI convention).
 */
function normalizeInterface(iface: any): any {
    if (!iface) return iface;
    return {
        Name: iface.name ?? iface.Name ?? '',
        Description: iface.description ?? iface.Description ?? '',
        Zone: iface.zone ?? iface.Zone ?? '',
        IPv4: iface.ipv4 ?? iface.IPv4 ?? [],
        IPv6: iface.ipv6 ?? iface.IPv6 ?? [],
        DHCP: iface.dhcp ?? iface.DHCP ?? false,
        DHCP_V6: iface.dhcp_v6 ?? iface.DHCP_V6 ?? false,
        RA: iface.ra ?? iface.RA ?? false,
        Gateway: iface.gateway ?? iface.Gateway ?? '',
        MTU: iface.mtu ?? iface.MTU ?? 0,
        Disabled: iface.disabled ?? iface.Disabled ?? false,
        State: iface.state ?? iface.State ?? '',
        MAC: iface.mac ?? iface.MAC ?? '',
        Vendor: iface.vendor ?? iface.Vendor ?? '',
        Alias: iface.alias ?? iface.Alias ?? '',
        Bond: iface.bond ?? iface.Bond ?? null,
        Members: iface.members ?? iface.Members ?? [],
        AccessWebUI: iface.access_web_ui ?? iface.AccessWebUI ?? false,
        WebUIPort: iface.web_ui_port ?? iface.WebUIPort ?? 0,
        // Preserve any extra fields
        ...iface,
    };
}

/**
 * Normalize entire config object to ensure consistent field naming.
 */
function normalizeConfig(cfg: any): any {
    if (!cfg) return cfg;
    return {
        ...cfg,
        interfaces: Array.isArray(cfg.interfaces)
            ? cfg.interfaces.map(normalizeInterface)
            : cfg.interfaces,
    };
}

// ============================================================================
// WebSocket State
// ============================================================================

export const wsConnected = writable(false);
export const wsSupported = writable(true);

// ============================================================================
// Navigation (Hash-based for browser back/forward support)
// ============================================================================

// Read initial page from hash or default to dashboard
function getPageFromHash(): string {
    if (typeof window === 'undefined') return 'dashboard';
    const hash = window.location.hash.replace('#', '');
    return hash || 'dashboard';
}

// Create a custom store that syncs with hash
function createHashStore() {
    const { subscribe, set } = writable(getPageFromHash());

    // Listen for hash changes (back/forward button)
    if (typeof window !== 'undefined') {
        window.addEventListener('hashchange', () => {
            const page = getPageFromHash();
            set(page);
        });
    }

    return {
        subscribe,
        set: (page: string) => {
            if (typeof window !== 'undefined') {
                // Update hash without triggering navigation
                window.history.pushState(null, '', `#${page}`);
            }
            set(page);
        },
        replace: (page: string) => {
            if (typeof window !== 'undefined') {
                window.history.replaceState(null, '', `#${page}`);
            }
            set(page);
        },
        syncWithHash: () => {
            if (typeof window !== 'undefined') {
                const page = getPageFromHash();
                set(page);
            }
        }
    };
}

export const currentPage = createHashStore();

// ============================================================================
// UI State
// ============================================================================

export const error = writable<string | null>(null);
export const loading = writable(false);
export const mobileMenuOpen = writable(false);

// Global Alert Store
interface AlertState {
    title?: string;
    message: string;
    type?: 'info' | 'success' | 'warning' | 'error';
    confirmText?: string;
}

function createAlertStore() {
    const { subscribe, set } = writable<AlertState | null>(null);

    return {
        subscribe,
        show: (message: string, type: AlertState['type'] = 'info', title?: string) => {
            set({
                message,
                type,
                title: title || (type === 'error' ? 'Error' : 'Alert')
            });
        },
        error: (message: string, title = 'Error') => {
            set({ message, type: 'error', title });
        },
        success: (message: string, title = 'Success') => {
            set({ message, type: 'success', title });
        },
        dismiss: () => set(null)
    };
}

export const alertStore = createAlertStore();

// ============================================================================
// Derived Stores
// ============================================================================

export const isAuthenticated = derived(authStatus, $auth => $auth?.authenticated ?? false);
export const username = derived(authStatus, $auth => $auth?.username ?? '');
export const setupRequired = derived(authStatus, $auth => $auth?.setup_required ?? false);

// ============================================================================
// API Client
// ============================================================================

const API_BASE = '/api';

async function apiRequest(endpoint: string, options: RequestInit = {}): Promise<any> {
    const url = `${API_BASE}${endpoint}`;
    // Get current auth state for CSRF token
    const auth = get(authStatus);

    const defaultOptions: RequestInit = {
        headers: {
            'Content-Type': 'application/json',
            ...(auth?.csrf_token ? { 'X-CSRF-Token': auth.csrf_token } : {}),
        },
        credentials: 'include',
    };

    const response = await fetch(url, { ...defaultOptions, ...options });

    if (response.status === 401) {
        currentView.set('login');
        throw new Error('Unauthorized');
    }

    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `HTTP ${response.status}`);
    }

    return response.json();
}

export const api = {
    // Internal WebSocket reference
    _ws: null as WebSocket | null,
    _pollInterval: null as ReturnType<typeof setInterval> | null,
    _lastTopologyJson: '' as string,

    // ========================================
    // Brand
    // ========================================

    async getBrand() {
        try {
            const data = await apiRequest('/brand');
            brand.set(data);
            return data;
        } catch (e) {
            console.error('Failed to load brand info', e);
            return null;
        }
    },

    // ========================================
    // Auth
    // ========================================

    async checkAuth() {
        try {
            const data = await apiRequest('/auth/status');
            authStatus.set(data);
            this.connectStatusWS();
            return data;
        } catch (e) {
            return null;
        }
    },

    async login(user: string, pass: string) {
        const data = await apiRequest('/auth/login', {
            method: 'POST',
            body: JSON.stringify({ username: user, password: pass }),
        });
        authStatus.set(data);
        return data;
    },

    async logout() {
        await fetch(`${API_BASE}/auth/logout`, { method: 'POST' });
        authStatus.set(null);
        currentView.set('login');
    },

    async createAdmin(user: string, pass: string) {
        const data = await apiRequest('/setup/create-admin', {
            method: 'POST',
            body: JSON.stringify({ username: user, password: pass }),
        });
        authStatus.set(data);
        return data;
    },

    // ========================================
    // WebSocket
    // ========================================

    connectStatusWS() {
        if (typeof window === 'undefined') return;
        if (!get(wsSupported)) {
            this.startPolling();
            return;
        }

        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(`${proto}//${window.location.host}/api/ws/status`);
        this._ws = ws;

        ws.onopen = () => {
            wsConnected.set(true);
            this.stopPolling();
            ws.send(JSON.stringify({
                action: 'subscribe',
                topics: ['status', 'config', 'leases', 'notification', 'pending_status', 'logs', 'topology', 'network']
            }));
        };

        ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                if (data.topic) {
                    switch (data.topic) {
                        case 'status':
                            status.set(data.data);
                            if (data.data.has_pending_changes !== undefined) {
                                hasPendingChanges.set(data.data.has_pending_changes);
                            }
                            break;
                        case 'config':
                            config.set(normalizeConfig(data.data));
                            break;
                        case 'leases':
                            leases.set(data.data);
                            break;
                        case 'topology':
                            // Data is GetTopologyReply (graph + neighbors)
                            const newTopo = data.data.graph || { nodes: [], links: [] };
                            const newTopoJson = JSON.stringify(newTopo);

                            // Deduplicate updates to prevent expensive re-renders
                            if (newTopoJson === this._lastTopologyJson) {
                                return;
                            }

                            this._lastTopologyJson = newTopoJson;
                            topology.set(newTopo);
                            break;
                        case 'network':
                            // Data is []NetworkDevice
                            networkDevices.set(data.data || []);
                            break;
                        case 'pending_status':
                            hasPendingChanges.set(data.data.has_pending);
                            break;
                        case 'logs':
                            const newLogs = data.data;
                            if (Array.isArray(newLogs)) {
                                logs.update(current => {
                                    const updated = [...current, ...newLogs];
                                    return updated.slice(-1000);
                                });
                            }
                            break;
                        case 'notification':
                            const notif = data.data;
                            alertStore.show(notif.message, notif.type === 'error' ? 'error' : 'info');
                            if (typeof window !== 'undefined') {
                                window.dispatchEvent(new CustomEvent('ws-notification', { detail: notif }));
                            }
                            break;
                    }
                } else {
                    status.set(data);
                }
            } catch (e) {
                console.error('WS Parse error', e);
            }
        };

        ws.onclose = () => {
            wsConnected.set(false);
            this._ws = null;
            this.loadDashboard();
            if (get(wsSupported)) {
                setTimeout(() => this.connectStatusWS(), 5000);
            }
        };

        ws.onerror = () => {
            if (!get(wsConnected)) {
                wsSupported.set(false);
                this.startPolling();
            }
        };
    },

    startPolling() {
        if (this._pollInterval) return;
        this._pollInterval = setInterval(() => {
            this.loadDashboard();
        }, 5000);
    },

    stopPolling() {
        if (this._pollInterval) {
            clearInterval(this._pollInterval);
            this._pollInterval = null;
        }
    },

    // ========================================
    // Dashboard Data
    // ========================================

    async loadDashboard() {
        loading.set(true);
        try {
            const [statusData, configData, leasesData] = await Promise.all([
                apiRequest('/status'),
                apiRequest('/config'),
                apiRequest('/leases'),
            ]);
            status.set(statusData);
            config.set(normalizeConfig(configData));
            leases.set(leasesData || []);
        } catch (e) {
            console.error('Failed to load dashboard', e);
        } finally {
            loading.set(false);
        }
    },

    async reloadConfig() {
        const data = await apiRequest('/config');
        config.set(normalizeConfig(data));
        return data;
    },

    // ========================================
    // Config Staging
    // ========================================

    async checkPendingChanges() {
        try {
            const data = await apiRequest('/config/pending-status');
            hasPendingChanges.set(data.has_changes ?? false);
            return data.has_changes;
        } catch (e) {
            console.error('Failed to check pending changes', e);
            return false;
        }
    },

    async applyConfig() {
        const result = await apiRequest('/config/apply', { method: 'POST' });
        hasPendingChanges.set(false);
        await this.reloadConfig();
        return result;
    },

    async discardConfig() {
        const result = await apiRequest('/config/discard', { method: 'POST' });
        hasPendingChanges.set(false);
        await this.reloadConfig();
        return result;
    },

    // ========================================
    // Interfaces
    // ========================================

    async getInterfaces() {
        return apiRequest('/interfaces');
    },

    async updateInterface(data: any) {
        const result = await apiRequest('/interfaces/update', {
            method: 'POST',
            body: JSON.stringify(data),
        });
        await this.reloadConfig();
        return result;
    },

    async createVlan(data: any) {
        const result = await apiRequest('/vlans', {
            method: 'POST',
            body: JSON.stringify(data),
        });
        await this.reloadConfig();
        return result;
    },

    async createBond(data: any) {
        const result = await apiRequest('/bonds', {
            method: 'POST',
            body: JSON.stringify(data),
        });
        await this.reloadConfig();
        return result;
    },

    // ========================================
    // Config Updates
    // ========================================

    async updatePolicies(policies: any) {
        const result = await apiRequest('/config/policies', {
            method: 'POST',
            body: JSON.stringify(policies),
        });
        await this.reloadConfig();
        return result;
    },

    async updateNAT(nat: any) {
        const result = await apiRequest('/config/nat', {
            method: 'POST',
            body: JSON.stringify(nat),
        });
        await this.reloadConfig();
        return result;
    },

    async updateZones(zones: any) {
        const result = await apiRequest('/config/zones', {
            method: 'POST',
            body: JSON.stringify(zones),
        });
        await this.reloadConfig();
        return result;
    },

    async updateDHCP(dhcp: any) {
        const result = await apiRequest('/config/dhcp', {
            method: 'POST',
            body: JSON.stringify(dhcp),
        });
        await this.reloadConfig();
        return result;
    },

    async updateDNS(dns: any) {
        const result = await apiRequest('/config/dns', {
            method: 'POST',
            body: JSON.stringify(dns),
        });
        await this.reloadConfig();
        return result;
    },

    async updateRoutes(routes: any) {
        const result = await apiRequest('/config/routes', {
            method: 'POST',
            body: JSON.stringify(routes),
        });
        await this.reloadConfig();
        return result;
    },

    async updateVPN(vpnConfig: any) {
        const result = await apiRequest('/config/vpn', {
            method: 'POST',
            body: JSON.stringify(vpnConfig),
        });
        await this.reloadConfig();
        return result;
    },

    async updateMarkRules(rules: any) {
        // We'll use a specific endpoint or generic structure
        // Since I didn't add a specific handler in server.go for this, I should probably add one
        // OR rely on a pattern.
        // Let's add the method and then ensure server handles it.
        // Wait, I didn't add `/api/config/mark_rules` in server.go!
        // I should have.
        // I will add the endpoint in server.go in next step.
        const result = await apiRequest('/config/mark_rules', {
            method: 'POST',
            body: JSON.stringify(rules),
        });
        await this.reloadConfig();
        return result;
    },

    async updateUIDRouting(rules: any) {
        const result = await apiRequest('/config/uid_routing', {
            method: 'POST',
            body: JSON.stringify(rules),
        });
        await this.reloadConfig();
        return result;
    },

    // ========================================
    // System
    // ========================================

    async reboot() {
        return apiRequest('/system/reboot', { method: 'POST' });
    },

    async setIPForwarding(enabled: boolean) {
        return apiRequest('/config/ip-forwarding', {
            method: 'POST',
            body: JSON.stringify({ enabled }),
        });
    },

    async getSafeModeStatus() {
        return apiRequest('/system/safe-mode');
    },

    async enterSafeMode() {
        return apiRequest('/system/safe-mode', { method: 'POST' });
    },

    async exitSafeMode() {
        return apiRequest('/system/safe-mode', { method: 'DELETE' });
    },

    async updateSettings(settings: any) {
        return apiRequest('/config/settings', {
            method: 'POST',
            body: JSON.stringify(settings),
        });
    },

    // ========================================
    // Users
    // ========================================

    async getUsers() {
        return apiRequest('/users');
    },

    async createUser(user: string, pass: string, role: string) {
        return apiRequest('/users', {
            method: 'POST',
            body: JSON.stringify({ username: user, password: pass, role }),
        });
    },

    async deleteUser(user: string) {
        return apiRequest(`/users/${encodeURIComponent(user)}`, { method: 'DELETE' });
    },

    // ========================================
    // Backups
    // ========================================

    async listBackups() {
        return apiRequest('/backups');
    },

    async createBackup(description = '') {
        return apiRequest('/backups/create', {
            method: 'POST',
            body: JSON.stringify({ description }),
        });
    },

    async restoreBackup(version: number) {
        const result = await apiRequest('/backups/restore', {
            method: 'POST',
            body: JSON.stringify({ version }),
        });
        await this.reloadConfig();
        return result;
    },

    // ========================================
    // IPSets
    // ========================================

    async refreshIPSets(name?: string) {
        const url = name ? `/ipsets/${encodeURIComponent(name)}?action=refresh` : '/ipsets?action=refresh';
        return apiRequest(url, { method: 'POST' });
    },

    async getIPSetStatus() {
        return apiRequest('/ipsets/status');
    },

    // ========================================
    // Topology
    // ========================================

    // ========================================
    // Device Management
    // ========================================

    async updateDeviceIdentity(id: string, alias?: string, owner?: string, type?: string, tags?: string[]) {
        const result = await apiRequest('/devices/identity', {
            method: 'POST',
            body: JSON.stringify({ id, alias, owner, type, tags }),
        });
        await this.loadDashboard(); // Reload leases to get updated info
        return result;
    },

    async linkDevice(mac: string, identityID: string) {
        const result = await apiRequest('/devices/link', {
            method: 'POST',
            body: JSON.stringify({ mac, identity_id: identityID }),
        });
        await this.loadDashboard();
        return result;
    },

    async unlinkDevice(mac: string) {
        const result = await apiRequest('/devices/unlink', {
            method: 'POST',
            body: JSON.stringify({ mac }),
        });
        await this.loadDashboard();
        return result;
    },

    async getTopology() {
        return apiRequest('/topology');
    },
};

// ============================================================================
// Navigation Config
// ============================================================================

export const mainNav = [
    { id: 'dashboard', label: 'Dashboard', icon: 'Activity' },
    { id: 'network', label: 'Network', icon: 'Share2' },
    { id: 'console', label: 'Console', icon: 'Settings' },
];

export const consoleModules = [
    // Gateway
    { id: 'interfaces', label: 'Interfaces', category: 'Gateway', icon: 'Network' },
    { id: 'routing', label: 'Static Routes', category: 'Gateway', icon: 'Route' },
    { id: 'nat', label: 'Port Forwarding', category: 'Gateway', icon: 'ArrowLeftRight' },

    // Shield
    { id: 'firewall', label: 'Firewall', category: 'Shield', icon: 'Shield' },
    { id: 'learning', label: 'Learning', category: 'Shield', icon: 'Eye' },
    { id: 'ipsets', label: 'IP Sets', category: 'Shield', icon: 'ListFilter' },
    { id: 'vpn', label: 'VPN', category: 'Shield', icon: 'Lock' },

    // LAN
    { id: 'dhcp', label: 'DHCP', category: 'LAN', icon: 'Server' },
    { id: 'dns', label: 'DNS', category: 'LAN', icon: 'Globe' },
    { id: 'zones', label: 'Zones', category: 'LAN', icon: 'Layers' },

    // System
    { id: 'logs', label: 'Logs', category: 'System', icon: 'ScrollText' },
    { id: 'scanner', label: 'Scanner', category: 'System', icon: 'Search' },
    { id: 'users', label: 'Users', category: 'System', icon: 'Users' },
    { id: 'settings', label: 'Settings', category: 'System', icon: 'Slider' },
    { id: 'backups', label: 'Backups', category: 'System', icon: 'Save' },
];

export const pages = derived(config, () => {
    return [...mainNav, ...consoleModules];
});
