<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';

	let c = $derived(copy($lang));
	let cfg = $state<any>(null);
	let email = $state('rini@dapur.test');
	let err = $state('');
	let inApp = $state(false);

	onMount(async () => {
		inApp = /Instagram|FBAN|FBAV|WhatsApp|TikTok|Line\//i.test(navigator.userAgent);
		cfg = await api('/api/v1/auth/config');
	});

	async function demo(which: string) {
		err = '';
		try {
			const me = await api<any>('/api/v1/auth/dev', { method: 'POST', body: JSON.stringify({ email: which }) });
			if (me.user.isPlatformAdmin && (!me.brands || me.brands.length === 0)) goto('/app/admin');
			else if (!me.brands || me.brands.length === 0) goto('/app/mulai');
			else goto('/app');
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal masuk';
		}
	}
</script>

<div class="px-5 pt-10">
	<img src="/icon-512.png" alt="" class="h-20 w-20" />
	<p class="fd mt-3 text-sm text-od">{c.tagline}</p>
	<h1 class="mt-1 text-3xl">{c.loginTitle}</h1>
	<p class="mt-2 font-bold text-ink2">{c.loginLead}</p>

	{#if inApp}
		<div class="card mt-6 p-4">
			<h2 class="text-lg">{c.openChrome}</h2>
			<p class="mt-1 text-sm font-bold text-ink2">{c.openChromeBody}</p>
		</div>
	{:else}
		{#if cfg?.google}
			<a class="btn mt-6" href="/api/v1/auth/google/start">{c.loginGoogle}</a>
		{/if}
		{#if cfg?.devLogin}
			<div class="card mt-4 space-y-2 p-4">
				<label class="text-xs font-extrabold text-ink2">{c.loginEmail}
					<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" bind:value={email} />
				</label>
				<button class="btn" onclick={() => demo(email)}>{c.loginDev}</button>
				<button class="btn ghost" onclick={() => demo('admin@pesenin.test')}>{c.admin}</button>
			</div>
		{/if}
		{#if cfg && !cfg.google && !cfg.devLogin}
			<p class="mt-4 text-sm font-bold">Google belum diset di server ini.</p>
		{/if}
	{/if}
	{#if err}<p class="mt-3 text-sm font-extrabold text-danger">{err}</p>{/if}
	<button class="mt-4 text-sm font-extrabold text-od" onclick={() => lang.update((v) => (v === 'id' ? 'en' : 'id'))}>{c.lang}</button>
</div>
