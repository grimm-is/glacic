/**
 * System API Tests
 * Tests for system-level API methods
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock fetch
const mockFetch = vi.fn();
(global as any).fetch = mockFetch;

import { api } from '$lib/stores/app';

describe('System API Methods', () => {
    beforeEach(() => {
        mockFetch.mockReset();
    });

    describe('reboot', () => {
        it('should POST to /api/system/reboot', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ success: true }),
            });

            await api.reboot();

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/system/reboot',
                expect.objectContaining({
                    method: 'POST',
                })
            );
        });
    });

    describe('setIPForwarding', () => {
        it('should POST enabled state to /api/config/ip-forwarding', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ success: true }),
            });

            await api.setIPForwarding(true);

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/config/ip-forwarding',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify({ enabled: true }),
                })
            );
        });

        it('should disable forwarding correctly', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ success: true }),
            });

            await api.setIPForwarding(false);

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/config/ip-forwarding',
                expect.objectContaining({
                    method: 'POST',
                    body: JSON.stringify({ enabled: false }),
                })
            );
        });
    });

    describe('refreshIPSets', () => {
        it('should POST to refresh specific IPSet', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ success: true }),
            });

            await api.refreshIPSets('firehol_level1');

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/ipsets/firehol_level1?action=refresh',
                expect.objectContaining({
                    method: 'POST',
                })
            );
        });

        it('should refresh all IPSets when no name provided', async () => {
            mockFetch.mockResolvedValueOnce({
                ok: true,
                status: 200,
                json: async () => ({ success: true }),
            });

            await api.refreshIPSets();

            expect(mockFetch).toHaveBeenCalledWith(
                '/api/ipsets?action=refresh',
                expect.objectContaining({
                    method: 'POST',
                })
            );
        });
    });
});
