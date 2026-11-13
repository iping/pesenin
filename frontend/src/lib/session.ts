import { writable } from 'svelte/store';
import { api } from './api';

export const me = writable<any>(null);

export async function loadMe() {
	const data = await api<any>('/api/v1/me');
	me.set(data);
	return data;
}

export function brandOf(data: any) {
	return data?.brands?.[0] || null;
}
