<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { homeFor } from '$lib/session';

	let c = $derived(copy($lang));
	let err = $state('');
	let note = $state('');

	onMount(async () => {
		const token = $page.url.searchParams.get('token') || '';
		if (!token) {
			err = 'Tautan ini tidak berlaku. Minta yang baru ya.';
			return;
		}
		try {
			const me = await api<any>('/api/v1/auth/email/verify', { method: 'POST', body: JSON.stringify({ token }) });
			goto(homeFor(me));
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		}
	});
</script>

<div class="px-5 pt-10">
	<img src="/icon-512.png" alt="" class="h-20 w-20" />
	<h1 class="mt-3 text-3xl">{c.verifyEmailTitle}</h1>
	<p class="mt-2 font-bold text-ink2">{c.verifyEmailLead}</p>
	{#if note}<p class="mt-3 text-sm font-extrabold text-mintd">{note}</p>{/if}
	{#if err}<p class="mt-3 text-sm font-extrabold text-danger">{err}</p>{/if}
	<a class="mt-4 block text-sm font-extrabold text-od" href="/app/masuk">{c.loginTitle}</a>
</div>
