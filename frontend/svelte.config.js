import adapterNode from '@sveltejs/adapter-node';
import adapterVercel from '@sveltejs/adapter-vercel';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

const onVercel = process.env.VERCEL === '1';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// Docker Compose runs `node build` from adapter-node.
		// Vercel sets VERCEL=1 during its build and uses adapter-vercel.
		adapter: onVercel ? adapterVercel({ runtime: 'nodejs24.x' }) : adapterNode(),
		alias: { $lib: 'src/lib' }
	}
};

export default config;
