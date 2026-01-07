/**
 * Vitest test setup file
 * Configures test environment before each test run
 */

import { vi } from 'vitest';

// declare global {
//    var fetch: any;
// }
(globalThis as any).fetch = vi.fn();

// Mock localStorage for tests
const localStorageMock = {
    getItem: vi.fn(),
    setItem: vi.fn(),
    removeItem: vi.fn(),
    clear: vi.fn(),
};

Object.defineProperty(global, 'localStorage', { value: localStorageMock });

// Mock matchMedia for theme tests
Object.defineProperty(global, 'matchMedia', {
    value: vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
    })),
});

// Mock fetch for API tests
global.fetch = vi.fn();
