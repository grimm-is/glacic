/**
 * Interface API Tests
 * Tests for interface-related API methods
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock fetch
const mockFetch = vi.fn();
(global as any).fetch = mockFetch;

import { api, config } from '$lib/stores/app';

describe('Interface API Methods', () => {
    beforeEach(() => {
        mockFetch.mockReset();
        config.set(null);
    });

    describe('createVlan', () => {
        it('should POST VLAN with parent and ID', async () => {
            const vlanData = {
                parent: 'eth0',
                vlan_id: 100,
                zone: 'DMZ',
                ipv4: '10.100.0.1/24',
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

            await api.createVlan(vlanData);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/vlans',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(vlanData),
                })
            );
        });

        it('should reload config after creating VLAN', async () => {
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

            await api.createVlan({ parent: 'eth0', vlan_id: 200 });

            expect(mockFetch).toHaveBeenCalledTimes(2);
            expect(mockFetch).toHaveBeenNthCalledWith(2, '/api/config', expect.anything());
        });
    });

    describe('createBond', () => {
        it('should POST bond with name and members', async () => {
            const bondData = {
                name: 'bond0',
                zone: 'LAN',
                members: ['eth1', 'eth2'],
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

            await api.createBond(bondData);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/bonds',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(bondData),
                })
            );
        });
    });

    describe('updateInterface', () => {
        it('should POST interface update data', async () => {
            const updateData = {
                name: 'eth0',
                zone: 'WAN',
                ipv4: ['192.168.1.1/24'],
                description: 'Updated interface',
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

            await api.updateInterface(updateData);

            expect(mockFetch).toHaveBeenNthCalledWith(
                1,
                '/api/interfaces/update',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify(updateData),
                })
            );
        });
    });
});
