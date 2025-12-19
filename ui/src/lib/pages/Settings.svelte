<script lang="ts">
    /**
     * Settings Page
     * Global firewall settings
     */

    import { config, api } from "$lib/stores/app";
    import { Card, Button, Toggle, Badge, Spinner } from "$lib/components";

    let loading = $state(false);

    // Computed state from config
    const ipForwarding = $derived($config?.ip_forwarding ?? false);
    const mssClamping = $derived($config?.mss_clamping ?? false);
    const flowOffload = $derived($config?.enable_flow_offload ?? false);

    async function updateSetting(key: string, value: boolean) {
        loading = true;
        try {
            const payload = { [key]: value };
            await api.updateSettings(payload);
        } catch (e) {
            console.error(`Failed to update ${key}:`, e);
        } finally {
            loading = false;
        }
    }
</script>

<div class="settings-page">
    <div class="page-header">
        <h2>Global Settings</h2>
    </div>

    <div class="settings-grid">
        <!-- IP Forwarding -->
        <Card>
            <div class="setting-item">
                <div class="setting-info">
                    <div class="setting-label">IP Forwarding</div>
                    <div class="setting-desc">
                        Enable packet forwarding between interfaces. Required
                        for router functionality.
                    </div>
                </div>
                <div class="setting-control">
                    <Toggle
                        checked={ipForwarding}
                        onchange={(val) => updateSetting("ip_forwarding", val)}
                        disabled={loading}
                    />
                </div>
            </div>
        </Card>

        <!-- MSS Clamping -->
        <Card>
            <div class="setting-item">
                <div class="setting-info">
                    <div class="setting-label">MSS Clamping</div>
                    <div class="setting-desc">
                        Automatically clamp TCP Maximum Segment Size to Path
                        MTU. Fixes connectivity issues (e.g., stalling HTTPS) on
                        PPPoE/VPN links.
                    </div>
                </div>
                <div class="setting-control">
                    <Toggle
                        checked={mssClamping}
                        onchange={(val) => updateSetting("mss_clamping", val)}
                        disabled={loading}
                    />
                </div>
            </div>
        </Card>

        <!-- Flow Offload -->
        <Card>
            <div class="setting-item">
                <div class="setting-info">
                    <div class="setting-label">
                        Flow Offload
                        <Badge variant="warning">Experimental</Badge>
                    </div>
                    <div class="setting-desc">
                        Bypass the standard packet processing path for
                        established connections to improve throughput. May
                        conflict with some advanced features.
                    </div>
                </div>
                <div class="setting-control">
                    <Toggle
                        checked={flowOffload}
                        onchange={(val) =>
                            updateSetting("enable_flow_offload", val)}
                        disabled={loading}
                    />
                </div>
            </div>
        </Card>
    </div>
</div>

<style>
    .settings-page {
        display: flex;
        flex-direction: column;
        gap: var(--space-6);
        max-width: 800px;
    }

    .page-header h2 {
        font-size: var(--text-2xl);
        font-weight: 600;
        margin: 0;
        color: var(--color-foreground);
    }

    .settings-grid {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
    }

    .setting-item {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-6);
    }

    .setting-info {
        flex: 1;
    }

    .setting-label {
        font-weight: 600;
        color: var(--color-foreground);
        margin-bottom: var(--space-1);
        display: flex;
        align-items: center;
        gap: var(--space-2);
    }

    .setting-desc {
        color: var(--color-muted);
        font-size: var(--text-sm);
        line-height: 1.5;
    }

    .setting-control {
        flex-shrink: 0;
    }
</style>
