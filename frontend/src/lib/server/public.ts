import { env } from '$env/dynamic/private';

export function apiBase() {
	const configured = (env.API_INTERNAL_URL || '').replace(/\/$/, '');
	if (configured) return configured;
	// Same-origin fallback when the service binding is not injected.
	// Docker Compose and local `vite dev` keep API_INTERNAL_URL or the default below.
	if (env.VERCEL_URL) return `https://${env.VERCEL_URL}`;
	return 'http://127.0.0.1:18080';
}

export async function publicGet(path: string) {
	const res = await fetch(apiBase() + path);
	if (!res.ok) return null;
	return res.json();
}
