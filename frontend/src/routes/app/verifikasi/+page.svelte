<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { loadMe, me } from '$lib/session';

	let c = $derived(copy($lang));
	let v = $derived($me?.verification);
	let email = $state('');
	let password = $state('');
	let phone = $state('');
	let code = $state('');
	let err = $state('');
	let note = $state('');
	let busy = $state(false);

	function kycLabel(status: string) {
		if (status === 'contact_verified') return c.kycContact;
		if (status === 'pending_review') return c.kycPending;
		if (status === 'verified') return c.kycVerified;
		if (status === 'rejected') return c.kycRejected;
		if (status === 'suspended') return c.kycSuspended;
		return c.kycUnverified;
	}

	onMount(async () => {
		const data = await loadMe();
		email = data?.user?.email || '';
		phone = data?.verification?.whatsapp || '';
	});

	async function refresh() {
		await loadMe();
	}

	async function resend() {
		err = '';
		note = '';
		busy = true;
		try {
			const res = await api<{ message: string }>('/api/v1/auth/email/resend', {
				method: 'POST',
				body: JSON.stringify({ email: $me.user.email })
			});
			note = res.message;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}

	async function changeEmail() {
		err = '';
		note = '';
		busy = true;
		try {
			await api('/api/v1/me/email', { method: 'POST', body: JSON.stringify({ email, password }) });
			await refresh();
			note = 'Email diperbarui. Cek kotak masuk untuk tautan baru.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}

	async function sendCode() {
		err = '';
		note = '';
		busy = true;
		try {
			const res = await api<{ message: string }>('/api/v1/me/whatsapp/otp', {
				method: 'POST',
				body: JSON.stringify({ phone })
			});
			note = res.message;
			await refresh();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}

	async function checkCode() {
		err = '';
		note = '';
		busy = true;
		try {
			await api('/api/v1/me/whatsapp/otp/check', { method: 'POST', body: JSON.stringify({ code }) });
			await refresh();
			note = 'WhatsApp terverifikasi.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}

	async function linkPassword() {
		err = '';
		note = '';
		busy = true;
		try {
			const res = await api<{ message: string }>('/api/v1/auth/password/link', { method: 'POST', body: '{}' });
			note = res.message;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}

	async function submitKyc(id: string) {
		err = '';
		note = '';
		busy = true;
		try {
			await api('/api/v1/brands/' + id + '/kyc/submit', { method: 'POST', body: '{}' });
			await refresh();
			note = 'Pengajuan terkirim. Pingping teruskan ke tim Pesenin.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}
</script>

<div class="space-y-3 px-4 pt-4">
	<h1 class="text-2xl">{c.verifyTitle}</h1>
	<p class="text-sm font-bold text-ink2">{v?.kycGate ? c.verifyLead : c.verifyBannerSoft}</p>
	{#if note}<p class="text-sm font-extrabold text-mintd">{note}</p>{/if}
	{#if err}<p class="text-sm font-extrabold text-danger">{err}</p>{/if}

	<div class="card space-y-2 p-3">
		<div class="fd">Email</div>
		<p class="text-sm font-bold">{v?.emailVerified ? '✓ ' + ($me?.user?.email || '') : ($me?.user?.email || '') + ' · belum dikonfirmasi'}</p>
		{#if !v?.emailVerified}
			<button class="btn sm" disabled={busy} onclick={resend}>{c.resendEmail}</button>
		{/if}
		<label class="text-xs font-extrabold text-ink2">{c.changeEmail}
			<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="email" bind:value={email} />
		</label>
		{#if v?.hasPassword}
			<label class="text-xs font-extrabold text-ink2">{c.currentPassword}
				<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="password" bind:value={password} />
			</label>
		{/if}
		<button class="btn sm ghost" disabled={busy} onclick={changeEmail}>{c.save}</button>
		{#if v && !v.hasPassword}
			<button class="btn sm ghost" disabled={busy} onclick={linkPassword}>{c.setPassword}</button>
		{/if}
	</div>

	<div class="card space-y-2 p-3">
		<div class="fd">WhatsApp</div>
		<p class="text-sm font-bold">{v?.whatsappVerified ? '✓ ' + v.whatsapp : 'Belum terverifikasi'}</p>
		<label class="text-xs font-extrabold text-ink2">{c.waNumber}
			<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" placeholder="08…" bind:value={phone} />
		</label>
		<button class="btn sm" disabled={busy} onclick={sendCode}>{c.sendCode}</button>
		<label class="text-xs font-extrabold text-ink2">{c.waCode}
			<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" inputmode="numeric" maxlength="6" bind:value={code} />
		</label>
		<button class="btn sm mint" disabled={busy} onclick={checkCode}>{c.confirm}</button>
	</div>

	{#each v?.brands || [] as b}
		<div class="card p-3">
			<div class="fd">{b.name}</div>
			<p class="text-sm font-bold">{kycLabel(b.status)}</p>
			{#if b.reason}<p class="text-xs font-bold text-ink2">{b.reason}</p>{/if}
			{#if b.status === 'contact_verified' || b.status === 'rejected'}
				<button class="btn sm mt-2" disabled={busy} onclick={() => submitKyc(b.id)}>{c.kycSubmit}</button>
			{/if}
		</div>
	{/each}
</div>
