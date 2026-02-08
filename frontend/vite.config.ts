import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { resolve } from 'node:path';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	const envDir = resolve(process.cwd(), '..');
	const env = loadEnv(mode, envDir, '');
	const apiUrl = env.API_URL ?? 'http://localhost:4000';

	return {
		envDir,
		plugins: [
			sveltekit(),
			tailwindcss() as any],
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
