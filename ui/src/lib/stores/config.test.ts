/**
 * Config API Tests
 * Tests for configuration update methods used by pages
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { get } from 'svelte/store';

// Mock fetch
const mockFetch = vi.fn();
(global as any).fetch = mockFetch;

import { config, api } from '$lib/stores/app';

describe('Configuration API Methods', () => {
    beforeEach(() => {
        mockFetch.mockReset();
        config.set(null);
    });

    describe('updatePolicies', () => {
        it('should POST policies to /api/config/policies', async () => {
            const policies = [
                { from: 'LAN', to: 'WAN', rules: [{ action: 'accept', name: 'Allow All' }] },
                { from: 'WAN', to: 'LAN', rules: [{ action: 'drop', name: 'Block All' }] },
            ];

            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ policies }),
                });

            await api.updatePolicies(policies);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/config/policies',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(policies),
                })
            );
        });

        it('should reload config after updating policies', async () => {
            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ policies: [], zones: [] }),
                });

            await api.updatePolicies([]);

            // Second call should be reloadConfig
            expect(mockFetch).toHaveBeenCalledTimes(2);
            expect(mockFetch).toHaveBeenNthCalledWith(2, '/api/config', expect.anything());
        });
    });

    describe('updateNAT', () => {
        it('should POST NAT rules correctly', async () => {
            const nat = [
                { type: 'masquerade', interface: 'eth0' },
                { type: 'dnat', protocol: 'tcp', destination: '443', to_address: '10.0.0.1' },
            ];

            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({}),
                });

            await api.updateNAT(nat);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/config/nat',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(nat),
                })
            );
        });
    });

    describe('updateZones', () => {
        it('should POST zones with color and description', async () => {
            const zones = [
                { name: 'WAN', color: 'red', description: 'External' },
                { name: 'LAN', color: 'green', description: 'Internal' },
            ];

            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({}),
                });

            await api.updateZones(zones);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/config/zones',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(zones),
                })
            );
        });
    });

    describe('updateDHCP', () => {
        it('should POST DHCP config with scopes', async () => {
            const dhcp = {
                enabled: true,
                scopes: [
                    { name: 'LAN', interface: 'eth1', range_start: '192.168.1.100', range_end: '192.168.1.200' },
                ],
            };

            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({}),
                });

            await api.updateDHCP(dhcp);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/config/dhcp',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(dhcp),
                })
            );
        });
    });

    describe('updateDNS', () => {
        it('should POST DNS config with forwarders', async () => {
            const dns = {
                enabled: true,
                forwarders: ['1.1.1.1', '8.8.8.8'],
                listen_on: ['192.168.1.1'],
            };

            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({}),
                });

            await api.updateDNS(dns);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/config/dns',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(dns),
                })
            );
        });
    });

    describe('updateRoutes', () => {
        it('should POST static routes array', async () => {
            const routes = [
                { destination: '10.0.0.0/8', gateway: '192.168.1.254', metric: 100 },
                { destination: '172.16.0.0/12', interface: 'eth2', metric: 50 },
            ];

            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({}),
                });

            await api.updateRoutes(routes);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/config/routes',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(routes),
                })
            );
        });
    });

    describe('updateVPN', () => {
        it('should POST VPN config', async () => {
            const vpn = {
                enabled: true,
                interface: 'wg0',
                listen_port: 51820,
                peers: [],
            };

            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ success: true }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({}),
                });

            await api.updateVPN(vpn);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/config/vpn',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(vpn),
                })
            );
        });
    });
});
