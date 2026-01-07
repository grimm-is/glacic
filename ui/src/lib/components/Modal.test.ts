/**
 * Modal Component Tests
 * Tests for the custom overlay Modal component
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, cleanup } from '@testing-library/svelte';
import Modal from './Modal.svelte';

describe('Modal Component', () => {
    beforeEach(() => {
        cleanup();
    });

    describe('visibility', () => {
        it('should not render content when open is false', () => {
            render(Modal, { props: { open: false, title: 'Test Modal' } });
            expect(document.querySelector('.modal-backdrop')).toBeFalsy();
            expect(document.querySelector('.modal-content')).toBeFalsy();
        });

        it('should render content when open is true', () => {
            render(Modal, { props: { open: true, title: 'Test Modal' } });
            expect(document.querySelector('.modal-backdrop')).toBeTruthy();
            expect(document.querySelector('.modal-content')).toBeTruthy();
        });

        it('should render title when provided', () => {
            render(Modal, { props: { open: true, title: 'Test Title' } });
            const title = document.querySelector('.modal-title');
            expect(title?.textContent).toBe('Test Title');
        });
    });

    describe('closing behavior', () => {
        it('should call onclose when close button is clicked', async () => {
            const onclose = vi.fn();
            render(Modal, { props: { open: true, title: 'Test', onclose } });

            const closeButton = document.querySelector('.modal-close');
            await fireEvent.click(closeButton!);

            expect(onclose).toHaveBeenCalledTimes(1);
        });

        it('should close when clicking backdrop', async () => {
            const onclose = vi.fn();
            render(Modal, { props: { open: true, title: 'Test', onclose } });

            const backdrop = document.querySelector('.modal-backdrop');
            // Click directly on backdrop (not on content)
            await fireEvent.click(backdrop!);

            expect(onclose).toHaveBeenCalled();
        });

        it('should close when pressing Escape', async () => {
            const onclose = vi.fn();
            render(Modal, { props: { open: true, title: 'Test', onclose } });

            await fireEvent.keyDown(window, { key: 'Escape' });

            expect(onclose).toHaveBeenCalled();
        });
    });

    describe('accessibility', () => {
        it('should have dialog role', () => {
            render(Modal, { props: { open: true, title: 'Test' } });
            const backdrop = document.querySelector('.modal-backdrop');
            expect(backdrop?.getAttribute('role')).toBe('dialog');
        });

        it('should have aria-modal attribute', () => {
            render(Modal, { props: { open: true, title: 'Test' } });
            const backdrop = document.querySelector('.modal-backdrop');
            expect(backdrop?.getAttribute('aria-modal')).toBe('true');
        });

        it('should have accessible close button', () => {
            render(Modal, { props: { open: true, title: 'Test' } });
            const closeButton = document.querySelector('.modal-close');
            expect(closeButton?.getAttribute('aria-label')).toBe('Close');
        });
    });

    describe('structure', () => {
        it('should have header section', () => {
            render(Modal, { props: { open: true, title: 'Test' } });
            expect(document.querySelector('.modal-header')).toBeTruthy();
        });

        it('should have body section', () => {
            render(Modal, { props: { open: true, title: 'Test' } });
            expect(document.querySelector('.modal-body')).toBeTruthy();
        });
    });
});
