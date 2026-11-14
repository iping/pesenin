<script lang="ts">
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';

	let c = $derived(copy($lang));
	let email = $state('');
	let err = $state('');
	let note = $state('');
	let busy = $state(false);

	async function submit() {
		err = '';
		note = '';
		busy = true;
		try {
			const res = await api<{ message: string }>('/api/v1/auth/password/forgot', {
				method: 'POST',
				body: JSON.stringify({ email })
			});
			note = res.message;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}
</script>

<div class="px-5 pt-10">
	<img src="/icon-512.png" alt="" class="h-20 w-20" />
	<h1 class="mt-3 text-3xl">{c.forgotTitle}</h1>
	<p class="mt-2 font-bold text-ink2">{c.forgotLead}</p>
	<form class="mt-6 space-y-2" onsubmit={(e) => { e.preventDefault(); submit(); }}>
		<label class="text-xs font-extrabold text-ink2">{c.email}
			<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="email" autocomplete="username" bind:value={email} required />
		</label>
		<button class="btn" type="submit" disabled={busy}>{c.forgotSubmit}</button>
	</form>
	{#if note}<p class="mt-3 text-sm font-extrabold text-mintd">{note}</p>{/if}
	{#if err}<p class="mt-3 text-sm font-extrabold text-danger">{err}</p>{/if}
	<a class="mt-4 block text-sm font-extrabold text-od" href="/app/masuk">{c.loginTitle}</a>
</div>
