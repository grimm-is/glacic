import { render } from '@testing-library/svelte';
import { describe, it, expect, beforeAll } from 'vitest';
import TopologyGraph from './TopologyGraph.svelte';

// Mock ResizeObserver
beforeAll(() => {
    (globalThis as any).ResizeObserver = class ResizeObserver {
        observe() { }
        unobserve() { }
        disconnect() { }
    };
});

describe('TopologyGraph', () => {
    it('renders svg container', () => {
        const { container } = render(TopologyGraph, {
            props: {
                graph: { nodes: [], links: [] }
            }
        });
        expect(container.querySelector('svg')).toBeTruthy();
    });

    it('renders nodes and links', () => {
        const graph = {
            nodes: [
                { id: '1', label: 'Node 1', type: 'router' },
                { id: '2', label: 'Node 2', type: 'device' }
            ],
            links: [
                { source: '1', target: '2' }
            ]
        };

        const { container, getAllByText } = render(TopologyGraph, {
            props: { graph }
        } as any); // Cast as any to avoid strict type checks on partial mocks if needed

        // Check for SVG
        const svg = container.querySelector('svg');
        expect(svg).toBeTruthy();

        // Check for labels (d3 appends them to DOM)
        expect(getAllByText('Node 1').length).toBeGreaterThan(0);
        expect(getAllByText('Node 2').length).toBeGreaterThan(0);

        // Check for nodes (circles)
        const circles = container.querySelectorAll('circle');
        expect(circles.length).toBe(2);

        // Check for links (lines)
        const lines = container.querySelectorAll('line');
        expect(lines.length).toBe(1);
    });
});
