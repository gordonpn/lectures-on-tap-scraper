import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, process.cwd(), '');
	const apiUrl = env.API_URL ?? 'http://localhost:4000';

	return {
		plugins: [sveltekit()],
		server: {
			host: true,
			port: 5173,
			strictPort: true,
			proxy: {
				'/api': {
					target: apiUrl,
					changeOrigin: true
				}
			}
		}
	};
});
