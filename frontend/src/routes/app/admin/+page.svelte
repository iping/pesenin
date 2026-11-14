<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { rp } from '$lib/format';
	import ProofImage from '$lib/components/ProofImage.svelte';

	let c = $derived(copy($lang));
	let queue = $state<any[]>([]);
	let fees = $state<any>(null);
	let settings = $state<any>(null);
	let report = $state<any>(null);
	let fixed = $state(1000);
	let note = $state('Biaya admin manual transfer');
	let reason = $state('');
	let err = $state('');
	let kycStatus = $state('pending_review');
	let kyc = $state<any[]>([]);
	let kycReason = $state('');

	async function load() {
		queue = await api('/api/v1/admin/topups?status=awaiting_verification');
		fees = await api('/api/v1/admin/fee-configs');
		settings = await api('/api/v1/admin/settings');
		report = await api('/api/v1/admin/reports/koin');
		fixed = fees.active.FixedRp ?? 1000;
		await loadKyc();
	}
	async function loadKyc() {
		kyc = await api('/api/v1/admin/kyc/brands?status=' + encodeURIComponent(kycStatus));
	}
	onMount(load);

	async function approve(id: string) {
		await api('/api/v1/admin/topups/' + id + '/approve', { method: 'POST', body: '{}' });
		await load();
	}
	async function reject(id: string) {
		if (!reason) return;
		await api('/api/v1/admin/topups/' + id + '/reject', { method: 'POST', body: JSON.stringify({ reason }) });
		await load();
	}
	async function publish() {
		err = '';
		try {
			await api('/api/v1/admin/fee-configs', { method: 'POST', body: JSON.stringify({ fixedRp: Number(fixed), percentBps: 0, minFeeRp: 0, note }) });
			await load();
		} catch (e) {
			err = e instanceof Error ? e.message : '';
		}
	}
	async function decide(id: string, decision: string) {
		err = '';
		try {
			await api('/api/v1/admin/kyc/brands/' + id + '/' + decision, {
				method: 'POST',
				body: JSON.stringify({ reason: kycReason })
			});
			await loadKyc();
		} catch (e) {
			err = e instanceof Error ? e.message : '';
		}
	}
</script>

<div class="space-y-3 px-4 pt-4">
	<h1 class="text-2xl">{c.admin}</h1>
	{#if err}<p class="text-sm font-bold text-danger">{err}</p>{/if}
	{#if report}
		<div class="card p-3 text-sm font-bold">
			<div>Top up {report.TopupCount} · Rp {rp(report.TopupRp)}</div>
			<div>{c.adminFee} Rp {rp(report.FeeRp)}</div>
			<div>Koin terpakai {report.KoinUsed} · saldo beredar {report.KoinOutstanding}</div>
		</div>
	{/if}
	<div class="card p-3">
		<div class="fd">{c.adminFee}</div>
		<p class="text-xs font-bold text-ink2">Nilai awal Rp 1.000, masih bisa diubah. Contoh desain sempat memakai Rp 2.500.</p>
		<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={fixed} />
		<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" bind:value={note} />
		<button class="btn sm mt-2" onclick={publish}>{c.save}</button>
		{#if fees}
			<div class="mt-2 text-[11px] font-bold text-ink2">Aktif Rp {rp(fees.active.FixedRp)}</div>
		{/if}
	</div>
	<div class="card p-3">
		<div class="fd">Verifikasi toko</div>
		<select class="mt-2 h-10 w-full rounded-xl border border-line px-2" bind:value={kycStatus} onchange={loadKyc}>
			<option value="pending_review">{c.kycPending}</option>
			<option value="contact_verified">{c.kycContact}</option>
			<option value="verified">{c.kycVerified}</option>
			<option value="rejected">{c.kycRejected}</option>
			<option value="suspended">{c.kycSuspended}</option>
			<option value="unverified">{c.kycUnverified}</option>
			<option value="">Semua</option>
		</select>
		<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" placeholder={c.reason} bind:value={kycReason} />
		{#each kyc as b}
			<div class="mt-3 border-t border-line pt-2 text-sm font-bold">
				<div class="fd">{b.name}</div>
				<div class="text-ink2">{b.slug} · {b.ownerEmail} · {b.ownerWhatsapp || 'WA belum ada'}</div>
				{#if b.reason}<div class="text-xs">{b.reason}</div>{/if}
				<div class="mt-2 flex gap-2">
					<button class="btn sm mint flex-1" onclick={() => decide(b.id, 'approve')}>{c.approve}</button>
					<button class="btn sm ghost" onclick={() => decide(b.id, 'reject')}>{c.reject}</button>
					<button class="btn sm ghost" onclick={() => decide(b.id, 'suspend')}>{c.kycSuspended}</button>
				</div>
			</div>
		{:else}
			<p class="mt-2 text-sm font-bold text-ink2">Tidak ada toko di status ini.</p>
		{/each}
	</div>
	{#each queue as t}
		<div class="card p-3">
			<b class="fd">{t.code}</b>
			<div class="text-sm font-bold">Harus Rp {rp(t.totalDueRp)} · fee Rp {rp(t.adminFeeRp)} · koin {t.koinToCredit}</div>
			{#if t.proofUrl}<div class="mt-2"><ProofImage url={t.proofUrl} compact /></div>{/if}
			<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" placeholder={c.reason} bind:value={reason} />
			<div class="mt-2 flex gap-2">
				<button class="btn sm mint flex-1" onclick={() => approve(t.id)}>{c.approve}</button>
				<button class="btn sm ghost" onclick={() => reject(t.id)}>{c.reject}</button>
			</div>
		</div>
	{:else}
		<p class="text-sm font-bold text-ink2">Antrean top up kosong.</p>
	{/each}
	{#if settings}
		<pre class="overflow-auto rounded-xl bg-white p-2 text-[11px]">{JSON.stringify(settings, null, 2)}</pre>
	{/if}
</div>
