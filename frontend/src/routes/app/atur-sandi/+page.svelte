<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { homeFor } from '$lib/session';

	let c = $derived(copy($lang));
	let token = $derived($page.url.searchParams.get('token') || '');
	let password = $state('');
	let again = $state('');
	let err = $state('');
	let busy = $state(false);

	async function submit() {
		err = '';
		if (password !== again) {
			err = c.passwordMismatch;
			return;
		}
		busy = true;
		try {
			const me = await api<any>('/api/v1/auth/password/reset', {
				method: 'POST',
				body: JSON.stringify({ token, password })
			});
			goto(homeFor(me));
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}
</script>

<div class="px-5 pt-10">
	<img src="/icon-512.png" alt="" class="h-20 w-20" />
	<h1 class="mt-3 text-3xl">{c.resetTitle}</h1>
	<p class="mt-2 font-bold text-ink2">{c.resetLead}</p>
	{#if !token}
		<p class="mt-4 text-sm font-extrabold text-danger">Tautan ini tidak berlaku. Minta yang baru ya.</p>
	{:else}
		<form class="mt-6 space-y-2" onsubmit={(e) => { e.preventDefault(); submit(); }}>
			<label class="text-xs font-extrabold text-ink2">{c.loginPassword}
				<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="password" autocomplete="new-password" minlength="8" maxlength="128" bind:value={password} required />
			</label>
			<label class="text-xs font-extrabold text-ink2">{c.passwordAgain}
				<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="password" autocomplete="new-password" bind:value={again} required />
			</label>
			<p class="text-xs font-bold text-ink2">{c.passwordHint}</p>
			<button class="btn" type="submit" disabled={busy}>{c.resetSubmit}</button>
		</form>
	{/if}
	{#if err}<p class="mt-3 text-sm font-extrabold text-danger">{err}</p>{/if}
	<a class="mt-4 block text-sm font-extrabold text-od" href="/app/lupa-sandi">{c.forgot}</a>
</div>
