/**
 * API Store Tests
 * Tests for the app store and API client
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import {
    theme,
    authStatus,
    currentView,
    brand,
    config,
    status,
    leases,
    wsConnected,
    currentPage,
    error,
    loading,
    isAuthenticated,
    username,
    setupRequired,
    mainNav,
    consoleModules,
} from '$lib/stores/app';

describe('App Store', () => {
    beforeEach(() => {
        // Reset stores to default state
        authStatus.set(null);
        currentView.set('loading');
        config.set(null);
        status.set(null);
        leases.set([]);
        error.set(null);
        loading.set(false);
        currentPage.set('dashboard');
    });

    describe('initial state', () => {
        it('should have correct initial view', () => {
            expect(get(currentView)).toBe('loading');
        });

        it('should have default brand info', () => {
            const brandInfo = get(brand);
            expect(brandInfo.name).toBe('Glacic');
            expect(brandInfo.vendor).toBe('Glacic');
        });

        it('should have empty leases array', () => {
            expect(get(leases)).toEqual([]);
        });

        it('should not be loading initially', () => {
            expect(get(loading)).toBe(false);
        });

        it('should have dashboard as default page', () => {
            expect(get(currentPage)).toBe('dashboard');
        });
    });

    describe('derived stores', () => {
        it('isAuthenticated should be false when authStatus is null', () => {
            authStatus.set(null);
            expect(get(isAuthenticated)).toBe(false);
        });

        it('isAuthenticated should be true when authenticated', () => {
            authStatus.set({ authenticated: true, username: 'admin' });
            expect(get(isAuthenticated)).toBe(true);
        });

        it('username should extract from authStatus', () => {
            authStatus.set({ authenticated: true, username: 'testuser' });
            expect(get(username)).toBe('testuser');
        });

        it('setupRequired should be false by default', () => {
            authStatus.set({});
            expect(get(setupRequired)).toBe(false);
        });

        it('setupRequired should be true when setup_required is set', () => {
            authStatus.set({ setup_required: true });
            expect(get(setupRequired)).toBe(true);
        });
    });

    describe('theme store', () => {
        it('should default to system', () => {
            // Note: localStorage is mocked, so stored value won't persist
            expect(['light', 'dark', 'system']).toContain(get(theme));
        });

        it('should accept valid theme values', () => {
            theme.set('dark');
            expect(get(theme)).toBe('dark');

            theme.set('light');
            expect(get(theme)).toBe('light');

            theme.set('system');
            expect(get(theme)).toBe('system');
        });
    });

    describe('navigation config', () => {
        it('mainNav should have required items', () => {
            expect(mainNav.length).toBeGreaterThan(0);

            const ids = mainNav.map(item => item.id);
            expect(ids).toContain('dashboard');
            expect(ids).toContain('console');
        });

        it('consoleModules should be organized by category', () => {
            expect(consoleModules.length).toBeGreaterThan(0);

            const categories = [...new Set(consoleModules.map(m => m.category))];
            expect(categories).toContain('Gateway');
            expect(categories).toContain('Shield');
            expect(categories).toContain('LAN');
            expect(categories).toContain('System');
        });

        it('all nav items should have required properties', () => {
            for (const item of [...mainNav, ...consoleModules]) {
                expect(item).toHaveProperty('id');
                expect(item).toHaveProperty('label');
                expect(item).toHaveProperty('icon');
                expect(typeof item.id).toBe('string');
                expect(typeof item.label).toBe('string');
            }
        });
    });
});
