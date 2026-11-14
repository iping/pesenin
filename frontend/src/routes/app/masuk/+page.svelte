<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { homeFor } from '$lib/session';

	let c = $derived(copy($lang));
	let cfg = $state<any>(null);
	let email = $state('rini@dapur.test');
	let password = $state('');
	let err = $state('');
	let note = $state('');
	let busy = $state(false);
	let inApp = $state(false);

	onMount(async () => {
		inApp = /Instagram|FBAN|FBAV|WhatsApp|TikTok|Line\//i.test(navigator.userAgent);
		cfg = await api('/api/v1/auth/config');
	});

	function go(me: any) {
		goto(homeFor(me));
	}

	async function demo(which: string) {
		err = '';
		try {
			go(await api<any>('/api/v1/auth/dev', { method: 'POST', body: JSON.stringify({ email: which }) }));
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal masuk';
		}
	}

	async function login() {
		err = '';
		busy = true;
		try {
			go(await api<any>('/api/v1/auth/password/login', { method: 'POST', body: JSON.stringify({ email, password }) }));
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal masuk';
		} finally {
			busy = false;
		}
	}
	async function resend() {
		err = '';
		note = '';
		try {
			const res = await api<{ message: string }>('/api/v1/auth/email/resend', {
				method: 'POST',
				body: JSON.stringify({ email })
			});
			note = res.message;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
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
	{/if}

	<form class="mt-6 space-y-2" onsubmit={(e) => { e.preventDefault(); login(); }}>
		<label class="text-xs font-extrabold text-ink2">{c.email}
			<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="email" autocomplete="username" bind:value={email} required />
		</label>
		<label class="text-xs font-extrabold text-ink2">{c.loginPassword}
			<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="password" autocomplete="current-password" bind:value={password} required />
		</label>
		<button class="btn" type="submit" disabled={busy}>{c.loginSubmit}</button>
	</form>
	<div class="mt-3 flex justify-between text-sm font-extrabold">
		<a class="text-od" href="/app/lupa-sandi">{c.forgot}</a>
		<a class="text-od" href="/app/daftar">{c.signUpLink}</a>
	</div>
	<button class="mt-2 text-sm font-extrabold text-od" type="button" onclick={resend}>{c.resendEmail}</button>

	{#if !inApp && cfg?.google}
		<div class="my-4 flex items-center gap-3 text-xs font-extrabold text-ink3">
			<span class="h-px flex-1 bg-line"></span>{c.orDivider}<span class="h-px flex-1 bg-line"></span>
		</div>
		<a class="btn ghost" href="/api/v1/auth/google/start">{c.loginGoogle}</a>
	{/if}

	{#if cfg?.devLogin}
		<div class="card mt-4 space-y-2 p-4">
			<button class="btn" onclick={() => demo(email)}>{c.loginDev}</button>
			<button class="btn ghost" onclick={() => demo('admin@pesenin.test')}>{c.admin}</button>
		</div>
	{/if}

	{#if note}<p class="mt-3 text-sm font-extrabold text-mintd">{note}</p>{/if}
	{#if err}<p class="mt-3 text-sm font-extrabold text-danger">{err}</p>{/if}
	<button class="mt-4 text-sm font-extrabold text-od" onclick={() => lang.update((v) => (v === 'id' ? 'en' : 'id'))}>{c.lang}</button>
</div>
