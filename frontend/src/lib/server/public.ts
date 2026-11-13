import { env } from '$env/dynamic/private';

export function apiBase() {
	return env.API_INTERNAL_URL || 'http://127.0.0.1:18080';
}

export async function publicGet(path: string) {
	const res = await fetch(apiBase() + path);
	if (!res.ok) return null;
	return res.json();
}
