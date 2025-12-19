/**
 * Theme Store Tests
 * Tests for the centralized theme system
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { theme } from '$lib/theme';

describe('Theme Configuration', () => {
    describe('colors', () => {
        it('should have light and dark variants for all colors', () => {
            const colors = Object.entries(theme.colors);
            expect(colors.length).toBeGreaterThan(0);

            for (const [name, value] of colors) {
                expect(value).toHaveProperty('light');
                expect(value).toHaveProperty('dark');
                expect(typeof value.light).toBe('string');
                expect(typeof value.dark).toBe('string');
            }
        });

        it('should have required semantic colors', () => {
            expect(theme.colors.background).toBeDefined();
            expect(theme.colors.foreground).toBeDefined();
            expect(theme.colors.primary).toBeDefined();
            expect(theme.colors.destructive).toBeDefined();
            expect(theme.colors.success).toBeDefined();
        });
    });

    describe('typography', () => {
        it('should define font families', () => {
            expect(theme.fonts.sans).toContain('Inter');
            expect(theme.fonts.mono).toContain('JetBrains');
        });

        it('should define font sizes', () => {
            expect(theme.fontSizes.xs).toBeDefined();
            expect(theme.fontSizes.sm).toBeDefined();
            expect(theme.fontSizes.base).toBeDefined();
            expect(theme.fontSizes.lg).toBeDefined();
        });
    });

    describe('spacing', () => {
        it('should define spacing scale', () => {
            expect(theme.spacing[0]).toBe('0');
            expect(theme.spacing[1]).toBeDefined();
            expect(theme.spacing[4]).toBeDefined();
        });
    });

    describe('radii', () => {
        it('should define border radii', () => {
            expect(theme.radii.none).toBe('0');
            expect(theme.radii.sm).toBeDefined();
            expect(theme.radii.md).toBeDefined();
            expect(theme.radii.full).toBe('9999px');
        });
    });
});
