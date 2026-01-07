import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	// @ts-expect-error process is defined in Node environment
	const env = loadEnv(mode, process.cwd(), '');
	return {
		plugins: [sveltekit()],
		server: {
			proxy: {
				'/api': {
					target: env.API_URL || 'http://localhost:8080',
					changeOrigin: true,
					secure: false,
					ws: true,
					configure: (proxy, _options) => {
						proxy.on('error', (err, _req, _res) => {
							console.log('proxy error', err);
						});
						proxy.on('proxyReq', (proxyReq, req, _res) => {
							// Rewrite Origin header to match target for backend checks
							const target = env.API_URL || 'http://localhost:8080';
							const origin = new URL(target).origin;
							proxyReq.setHeader('Origin', origin);
						});
						proxy.on('proxyReqWs', (proxyReq, req, socket, options, head) => {
							// Rewrite Origin header for WebSockets too
							const target = env.API_URL || 'http://localhost:8080';
							const origin = new URL(target).origin;
							proxyReq.setHeader('Origin', origin);
						});
					}
				}
			}
		}
	};
});

