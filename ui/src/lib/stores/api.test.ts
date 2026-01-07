/**
 * API Store Tests - Extended
 * Additional tests for API methods and edge cases
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// Mock fetch before importing the store
const mockFetch = vi.fn();
(global as any).fetch = mockFetch;

// Now import the stores
import {
    authStatus,
    currentView,
    config,
    status,
    leases,
    api,
} from '$lib/stores/app';

describe('API Client', () => {
    beforeEach(() => {
        mockFetch.mockReset();
        // Reset stores
        authStatus.set(null);
        currentView.set('loading');
        config.set(null);
        status.set(null);
        leases.set([]);
    });

    describe('apiRequest wrapper', () => {
        it('should include credentials in requests', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ name: 'Glacic' }),
            });

            await api.getBrand();

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/brand',
                expect.objectContaining({
                    credentials: 'include',
                })
            );
        });

        it('should set Content-Type to application/json', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({}),
            });

            await api.getBrand();

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/brand',
                expect.objectContaining({
                    headers: expect.objectContaining({
                        'Content-Type': 'application/json',
                    }),
                })
            );
        });

        it('should redirect to login on 401', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: false,
                status: 401,
                text: async () => 'Unauthorized',
            });

            try {
                await api.checkAuth();
            } catch (e) {
                // Expected to throw
            }

            expect(get(currentView)).toBe('login');
        });
    });

    describe('login', () => {
        it('should send credentials as JSON body', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ authenticated: true, username: 'admin' }),
            });

            await api.login('admin', 'password123');

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/auth/login',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify({ username: 'admin', password: 'password123' }),
                })
            );
        });

        it('should update authStatus on successful login', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ authenticated: true, username: 'testuser' }),
            });

            await api.login('testuser', 'pass');

            expect(get(authStatus)).toEqual({ authenticated: true, username: 'testuser' });
        });
    });

    describe('loadDashboard', () => {
        it('should fetch status, config, and leases in parallel', async () => {
            mockFetch
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ hostname: 'glacic', uptime: '1d' }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => ({ ip_forwarding: true, zones: [] }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    status: 200,
                    json: async () => [{ ip: '192.168.1.100', mac: 'aa:bb:cc:dd:ee:ff' }],
                });

            await api.loadDashboard();

            expect(mockFetch).toHaveBeenCalledTimes(3);
            expect(get(status)).toEqual({ hostname: 'glacic', uptime: '1d' });
            expect(get(config)).toEqual({ ip_forwarding: true, zones: [] });
            expect(get(leases)).toEqual([{ ip: '192.168.1.100', mac: 'aa:bb:cc:dd:ee:ff' }]);
        });
    });

    describe('updatePolicies', () => {
        it('should POST policies and reload config', async () => {
            const policies = [{ from: 'LAN', to: 'WAN', rules: [] }];

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
            // Second call should be reloadConfig
            expect(mockFetch).toHaveBeenNthCalledWith(2, '/api/config', expect.anything());
        });
    });

    describe('createVlan', () => {
        it('should POST VLAN data correctly', async () => {
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

            const vlanData = { parent: 'eth0', vlan_id: 100, zone: 'LAN' };
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
    });
});
