import { render, screen, fireEvent } from '@testing-library/svelte';
import { describe, it, expect, vi, beforeEach, beforeAll, type Mock } from 'vitest';
import Network from './Network.svelte';
import { topology, api, networkDevices } from '$lib/stores/app';

// Mock ResizeObserver
beforeAll(() => {
    (globalThis as any).ResizeObserver = class ResizeObserver {
        observe() { }
        unobserve() { }
        disconnect() { }
    };
});
import { get } from 'svelte/store';

// Mock Stores
vi.mock('$lib/stores/app', async () => {
    const { writable } = await import('svelte/store');
    return {
        topology: writable({ nodes: [], links: [] }),
        leases: writable([]),
        config: writable({}),
        alertStore: { show: vi.fn() },
        api: {
            getTopology: vi.fn(),
            updateDeviceIdentity: vi.fn(),
            linkDevice: vi.fn(),
            unlinkDevice: vi.fn()
        },
        networkDevices: writable([])
    };
});

describe('Network Component', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        // Reset stores
        topology.set({ nodes: [], links: [] });
        // Default API mocks
        (api.getTopology as Mock).mockResolvedValue({ graph: { nodes: [], links: [] } });
    });

    it('renders devices tab by default', () => {
        render(Network);
        expect(screen.getByText(/Devices/, { selector: 'button' })).toBeTruthy();
        expect(screen.getByPlaceholderText('Search devices...')).toBeTruthy();
    });

    it('switches to topology tab and displays graph nodes', async () => {
        const { container } = render(Network);

        // Setup initial store data (simulate WebSocket update)
        topology.set({
            nodes: [
                { id: 'router-0', label: 'Gateway', type: 'router' },
                { id: 'sw-eth0', label: 'eth0', type: 'switch' },
                { id: 'dev-1', label: 'Laptop', type: 'device' }
            ],
            links: []
        });

        // Click Topology Tab
        const topologyTab = screen.getByText(/Topology/, { selector: 'button' });
        await fireEvent.click(topologyTab);

        // Verify Nodes Rendered (Labels)
        expect((await screen.findAllByText('Gateway')).length).toBeGreaterThan(0);
        expect((await screen.findAllByText('eth0')).length).toBeGreaterThan(0);
        expect((await screen.findAllByText('Laptop')).length).toBeGreaterThan(0);

        // Verify SVG presence
        expect(container.querySelector('svg')).toBeTruthy();

        // Verify Empty State is NOT present
        expect(screen.queryByText('No Topology Data')).toBeNull();
    });

    it('shows empty state when topology is empty', async () => {
        topology.set({ nodes: [], links: [] });
        render(Network);

        const topologyTab = screen.getByText(/Topology/, { selector: 'button' });
        await fireEvent.click(topologyTab);

        expect(await screen.findByText('No Topology Data')).toBeTruthy();
        expect(screen.getByText('Enable LLDP or waiting for discovery.')).toBeTruthy();
    });

    it('calls API to fetch topology if store is empty on mount', async () => {
        // Mock API response
        const mockGraph = { nodes: [{ id: 'r1', label: 'R1', type: 'router' }], links: [] };
        (api.getTopology as Mock).mockResolvedValueOnce({ graph: mockGraph });

        render(Network);

        // Wait for onMount
        await new Promise(r => setTimeout(r, 10));

        expect(api.getTopology).toHaveBeenCalled();
    });
});
