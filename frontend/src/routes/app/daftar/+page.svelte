<script lang="ts">
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';

	let c = $derived(copy($lang));
	let name = $state('');
	let email = $state('');
	let password = $state('');
	let again = $state('');
	let err = $state('');
	let note = $state('');
	let busy = $state(false);

	async function submit() {
		err = '';
		note = '';
		if (password !== again) {
			err = c.passwordMismatch;
			return;
		}
		busy = true;
		try {
			const res = await api<{ message: string }>('/api/v1/auth/password/signup', {
				method: 'POST',
				body: JSON.stringify({ email, password, name })
			});
			note = res.message;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal daftar';
		} finally {
			busy = false;
		}
	}
</script>

<div class="px-5 pt-10">
	<img src="/icon-512.png" alt="" class="h-20 w-20" />
	<p class="fd mt-3 text-sm text-od">{c.tagline}</p>
	<h1 class="mt-1 text-3xl">{c.signUpTitle}</h1>
	<p class="mt-2 font-bold text-ink2">{c.signUpLead}</p>

	<form class="mt-6 space-y-2" onsubmit={(e) => { e.preventDefault(); submit(); }}>
		<label class="text-xs font-extrabold text-ink2">{c.name}
			<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" autocomplete="name" bind:value={name} />
		</label>
		<label class="text-xs font-extrabold text-ink2">{c.email}
			<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="email" autocomplete="username" bind:value={email} required />
		</label>
		<label class="text-xs font-extrabold text-ink2">{c.loginPassword}
			<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="password" autocomplete="new-password" minlength="8" maxlength="128" bind:value={password} required />
		</label>
		<label class="text-xs font-extrabold text-ink2">{c.passwordAgain}
			<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="password" autocomplete="new-password" bind:value={again} required />
		</label>
		<p class="text-xs font-bold text-ink2">{c.passwordHint}</p>
		<button class="btn" type="submit" disabled={busy}>{c.signUpSubmit}</button>
	</form>
	{#if note}<p class="mt-3 text-sm font-extrabold text-mintd">{note}</p>{/if}
	{#if err}<p class="mt-3 text-sm font-extrabold text-danger">{err}</p>{/if}
	<a class="mt-4 block text-sm font-extrabold text-od" href="/app/masuk">{c.hasAccount}</a>
</div>
