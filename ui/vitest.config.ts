import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
    plugins: [svelte({ hot: !process.env.VITEST })],
    test: {
        include: ['src/**/*.{test,spec}.{js,ts}'],
        // Exclude component tests that require browser environment
        exclude: ['src/lib/components/*.custom-exclude.ts'], // Only exclude specific files if needed
        environment: 'jsdom',
        globals: true,
        setupFiles: ['src/lib/test-setup.ts'],
    },
    resolve: {
        conditions: ['browser'],
        alias: {
            $lib: '/src/lib',
        },
    },
});
