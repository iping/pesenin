<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { me, loadMe } from '$lib/session';
	import { api, ApiError, idem } from '$lib/api';
	import { rp, when, countdown, syncClock } from '$lib/format';
	import { prepareUpload, uploadAccept } from '$lib/images';

	let c = $derived(copy($lang));
	let opts = $state<any>(null);
	let amount = $state(50000);
	let custom = $state(false);
	let customAmt = $state(25000);
	let quote = $state<any>(null);
	let topup = $state<any>(null);
	let rows = $state<any[]>([]);
	let ledger = $state<any[]>([]);
	let err = $state('');
	let file = $state<File | null>(null);

	onMount(async () => {
		await loadMe();
		opts = await api('/api/v1/topups/options');
		rows = await api('/api/v1/topups');
		ledger = await api('/api/v1/wallet/transactions');
		await refresh();
	});

	async function refresh() {
		const n = custom ? customAmt : amount;
		try {
			quote = await api('/api/v1/topups/quote', { method: 'POST', body: JSON.stringify({ amount: n }) });
			err = '';
		} catch (e) {
			quote = null;
			err = e instanceof ApiError ? e.message : '';
		}
	}

	async function create() {
		const n = custom ? Number(customAmt) : amount;
		topup = await api<any>('/api/v1/topups', {
			method: 'POST',
			headers: { 'Idempotency-Key': idem() },
			body: JSON.stringify({ amount: n })
		});
		syncClock(topup.serverNow);
		rows = await api('/api/v1/topups');
	}

	async function proof() {
		if (!file || !topup) return;
		err = '';
		let prepared = file;
		try {
			prepared = await prepareUpload(file, 'proof');
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
			return;
		}
		const body = new FormData();
		body.append('file', prepared, prepared.name);
		const res = await fetch('/api/v1/topups/' + topup.id + '/proof', { method: 'POST', body, credentials: 'include' });
		if (!res.ok) {
			const j = await res.json().catch(() => ({}));
			err = j.message || 'Gagal';
			return;
		}
		topup = await api('/api/v1/topups/' + topup.id);
		rows = await api('/api/v1/topups');
	}

	const labels: Record<string, string> = {
		promo_free_credit: 'Gratis pendaftaran',
		topup_credit: 'Isi koin',
		order_debit: 'Pesanan lunas',
		refund_credit: 'Pengembalian',
		adjustment: 'Penyesuaian admin'
	};
</script>

<div class="space-y-3 px-4 pt-4">
	<h1 class="text-2xl">{c.koin}</h1>
	{#if $me?.wallet}
		<div class="flex items-center gap-3 rounded-[20px] bg-gradient-to-br from-[#FFA445] to-o p-3.5 text-white shadow-[0_4px_0_#E06F0A]">
			<span class="grid h-9 w-9 place-items-center rounded-full bg-[#F5B700] font-black text-white">★</span>
			<div class="flex-1">
				<div class="text-[11px] font-bold opacity-90">{c.balance} · 1 koin = Rp 1</div>
				<b class="fd text-2xl">{$me.wallet.balanceKoin.toLocaleString('id-ID')} koin</b>
			</div>
			<div class="text-right text-[11px] font-bold">≈ {$me.wallet.orderEquivalent}<br />pesanan</div>
		</div>
	{/if}

	<div class="card p-3.5">
		<div class="fd mb-2">{c.topup}</div>
		<div class="grid grid-cols-4 gap-1.5">
			{#each opts?.presets || [25000, 50000, 100000] as p, i}
				<button class="rounded-2xl border-2 py-2 text-center {(!custom && amount === p) ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => { custom = false; amount = p; refresh(); }}>
					{#if i === 1}<span class="mb-0.5 block rounded-full bg-mint text-[9px] font-black text-white">Favorit</span>{/if}
					<b class="fd">{p / 1000}rb</b>
				</button>
			{/each}
			<button class="rounded-2xl border-2 py-2 {custom ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => { custom = true; refresh(); }}>
				<b class="fd">{c.custom}</b>
				<div class="text-[9px] font-extrabold text-ink2">{c.minCustom}</div>
			</button>
		</div>
		{#if custom}
			<input class="mt-2 h-11 w-full rounded-xl border border-line px-3" type="number" step="1000" bind:value={customAmt} onchange={refresh} />
		{/if}
		{#if quote}
			<div class="mt-2 rounded-xl border border-line bg-[#FFFCF8] px-3 py-2 text-sm font-extrabold">
				<div class="flex"><span class="flex-1 text-ink2">{c.topup}</span><span>Rp {rp(quote.amount)}</span></div>
				<div class="flex"><span class="flex-1 text-ink2">{c.adminFee}</span><span>Rp {rp(quote.adminFee)}</span></div>
				<div class="flex"><span class="flex-1 text-ink2">{c.uniqueCode}</span><span>{quote.note}</span></div>
			</div>
		{/if}
		{#if err}<p class="mt-2 text-xs font-extrabold text-danger">{err}</p>{/if}
		<button class="btn sm mt-2" onclick={create} disabled={!quote}>{c.confirm}</button>
	</div>

	{#if topup}
		<div class="card border-2 border-o2 p-4 text-center">
			<div class="text-xs font-bold text-ink2">{c.transferExact}</div>
			<div class="fd text-3xl">Rp {rp(topup.totalDueRp)}</div>
			<p class="text-xs font-bold">{topup.bank.bankCode} {topup.bank.accountNumber}<br />a.n. {topup.bank.holderName}</p>
			<p class="mt-1 text-xs font-extrabold">Berlaku sampai {when(topup.expiresAt)} · {countdown(topup.expiresAt)}</p>
			<p class="mt-1 text-xs font-bold text-ink2">{c.koinGet}: {topup.koinToCredit.toLocaleString('id-ID')} · {topup.status}</p>
			{#if topup.status === 'pending' || topup.status === 'awaiting_verification'}
				<input class="mt-2 text-sm" type="file" accept={uploadAccept} onchange={(e) => (file = (e.target as HTMLInputElement).files?.[0] || null)} />
				<button class="btn mt-2" disabled={!file} onclick={proof}>{c.sendProof}</button>
			{/if}
			{#if topup.rejectReason}<p class="mt-2 text-sm font-extrabold text-danger">{topup.rejectReason}</p>{/if}
		</div>
	{/if}

	<div class="card px-3.5 py-1">
		{#each ledger as t}
			<div class="flex items-center gap-2 border-b border-dashed border-line py-2">
				<div class="flex-1">
					<b class="text-sm">{labels[t.type] || t.type}</b>
					<div class="text-[11px] font-bold text-ink2">{when(t.createdAt)} {t.reason}</div>
				</div>
				<b class="fd {t.amountKoin > 0 ? 'text-mintd' : ''}">{t.amountKoin > 0 ? '+' : ''}{t.amountKoin.toLocaleString('id-ID')}</b>
			</div>
		{/each}
	</div>
	<div class="card px-3.5 py-2 text-sm font-bold">
		{#each rows as t}
			<div class="flex py-1"><span class="flex-1">{t.code} · {t.status}</span><span>Rp {rp(t.totalDueRp)}</span></div>
		{/each}
	</div>
</div>
