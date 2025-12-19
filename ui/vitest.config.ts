import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
    plugins: [svelte({ hot: !process.env.VITEST })],
    test: {
        include: ['src/**/*.{test,spec}.{js,ts}'],
        // Exclude component tests that require browser environment
        exclude: ['src/**/*.svelte.test.ts', 'src/lib/components/*.test.ts'],
        environment: 'jsdom',
        globals: true,
        setupFiles: ['src/lib/test-setup.ts'],
    },
    resolve: {
        alias: {
            $lib: '/src/lib',
        },
    },
});
