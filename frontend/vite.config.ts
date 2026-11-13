import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		host: '0.0.0.0',
		port: 43123,
		proxy: {
			'/api': { target: 'http://127.0.0.1:18080', changeOrigin: true }
		}
	},
	preview: { host: '0.0.0.0', port: 43123 }
});
