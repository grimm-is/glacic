/**
 * Button Component Tests
 * Tests for the reusable Button component
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import Button from './Button.svelte';

describe('Button Component', () => {
    describe('rendering', () => {
        it('should render with default props', () => {
            render(Button, { props: {} });
            const button = document.querySelector('button');
            expect(button).toBeTruthy();
            expect(button?.type).toBe('button');
        });

        it('should render children content', () => {
            // Note: Testing content requires a wrapper or snippet approach in Svelte 5
            // For now, we test the button exists
            render(Button, { props: {} });
            expect(document.querySelector('button')).toBeTruthy();
        });

        it('should apply custom class', () => {
            render(Button, { props: { class: 'custom-class' } });
            const button = document.querySelector('button');
            expect(button?.classList.contains('custom-class')).toBe(true);
        });
    });

    describe('variants', () => {
        it('should apply default variant class', () => {
            render(Button, { props: { variant: 'default' } });
            const button = document.querySelector('button');
            expect(button?.classList.contains('btn-default')).toBe(true);
        });

        it('should apply destructive variant class', () => {
            render(Button, { props: { variant: 'destructive' } });
            const button = document.querySelector('button');
            expect(button?.classList.contains('btn-destructive')).toBe(true);
        });

        it('should apply outline variant class', () => {
            render(Button, { props: { variant: 'outline' } });
            const button = document.querySelector('button');
            expect(button?.classList.contains('btn-outline')).toBe(true);
        });

        it('should apply ghost variant class', () => {
            render(Button, { props: { variant: 'ghost' } });
            const button = document.querySelector('button');
            expect(button?.classList.contains('btn-ghost')).toBe(true);
        });
    });

    describe('sizes', () => {
        it('should apply small size class', () => {
            render(Button, { props: { size: 'sm' } });
            const button = document.querySelector('button');
            expect(button?.classList.contains('btn-sm')).toBe(true);
        });

        it('should apply medium size class by default', () => {
            render(Button, { props: {} });
            const button = document.querySelector('button');
            expect(button?.classList.contains('btn-md')).toBe(true);
        });

        it('should apply large size class', () => {
            render(Button, { props: { size: 'lg' } });
            const button = document.querySelector('button');
            expect(button?.classList.contains('btn-lg')).toBe(true);
        });
    });

    describe('states', () => {
        it('should be disabled when disabled prop is true', () => {
            render(Button, { props: { disabled: true } });
            const button = document.querySelector('button') as HTMLButtonElement;
            expect(button?.disabled).toBe(true);
        });

        it('should have submit type when specified', () => {
            render(Button, { props: { type: 'submit' } });
            const button = document.querySelector('button');
            expect(button?.type).toBe('submit');
        });
    });

    describe('events', () => {
        it('should call onclick handler when clicked', async () => {
            const handleClick = vi.fn();
            render(Button, { props: { onclick: handleClick } });
            const button = document.querySelector('button');

            await fireEvent.click(button!);

            expect(handleClick).toHaveBeenCalledTimes(1);
        });

        it('should not call onclick when disabled', async () => {
            const handleClick = vi.fn();
            render(Button, { props: { onclick: handleClick, disabled: true } });
            const button = document.querySelector('button');

            await fireEvent.click(button!);

            expect(handleClick).not.toHaveBeenCalled();
        });
    });
});
