<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { loadMe, brandOf } from '$lib/session';
	import { api } from '$lib/api';
	import { rp, splitAmount } from '$lib/format';

	let c = $derived(copy($lang));
	let tab = $state('proof_submitted');
	let rows = $state<any[]>([]);
	let brandId = $state('');
	let q = $state('');

	const tabs = [
		['proof_submitted', 'needCheck'],
		['pending', 'proofWait'],
		['paid', 'paid'],
		['', 'done']
	] as const;

	onMount(async () => {
		const me = await loadMe();
		brandId = brandOf(me)?.id || '';
		await reload();
	});

	async function reload() {
		if (!brandId) return;
		const status = tab === 'done' ? '' : tab;
		const amount = q.trim() ? '&amount=' + q.trim() : '';
		rows = await api<any[]>('/api/v1/orders?brandId=' + brandId + (status ? '&status=' + status : '') + amount);
		if (tab === 'done') rows = rows.filter((r) => r.fulfilmentStatus === 'completed' || r.paymentStatus === 'cancelled');
		if (tab === 'paid') rows = rows.filter((r) => r.fulfilmentStatus !== 'completed');
	}

	async function paid(id: string) {
		await api('/api/v1/orders/' + id + '/mark-paid', { method: 'POST' });
		new Audio('/sounds/koin-success.mp3').play().catch(() => {});
		await reload();
	}
	async function reject(id: string) {
		const reason = prompt(c.reason) || '';
		if (!reason) return;
		await api('/api/v1/orders/' + id + '/reject-proof', { method: 'POST', body: JSON.stringify({ reason }) });
		await reload();
	}
</script>

<div class="px-4 pt-4">
	<div class="flex items-center">
		<h1 class="flex-1 text-2xl">{c.orders}</h1>
		<input class="h-10 w-28 rounded-xl border border-line px-2 text-sm" placeholder="75137" bind:value={q} onchange={reload} />
	</div>
	<div class="mt-3 flex rounded-2xl bg-[#F6EBDD] p-1 text-xs font-extrabold">
		{#each tabs as [key, label]}
			<button class="flex-1 rounded-xl py-2 {tab === key ? 'bg-white text-od' : 'text-ink2'}" onclick={() => { tab = key; reload(); }}>{(c as any)[label] || label}</button>
		{/each}
	</div>
	{#if rows.length === 0}
		<div class="card mt-4 p-6 text-center">
			<img src="/brand/placeholder-avatar.png" alt="" class="mx-auto h-16 w-16 rounded-full" />
			<p class="mt-2 text-sm font-extrabold">{c.emptyOrders}</p>
		</div>
	{/if}
	<div class="mt-3 space-y-2.5">
		{#each rows as o}
			{@const parts = splitAmount(o.totalDue)}
			<a class="card block p-3.5" href="/app/pesanan/{o.id}">
				<div class="flex items-center gap-2">
					<img src="/brand/placeholder-avatar.png" alt="" class="h-8 w-8 rounded-full" />
					<div class="flex-1">
						<b class="fd">{o.customer.name}</b>
						{#if o.customer.ordersCount <= 1}<span class="ml-1 rounded bg-[#EEE] px-1.5 text-[10px] font-extrabold text-[#6F6F6F]">{c.newBuyer}</span>{/if}
						<div class="text-[11px] font-bold text-ink2">{o.code}</div>
					</div>
				</div>
				<div class="mt-2 text-[11px] font-bold text-ink2">{o.items.map((i: any) => i.qty + ' ' + i.name).join(' · ')} · {o.fulfilmentMethod === 'pickup' ? c.pickup : c.delivery}</div>
				<div class="fd text-xl">Rp {parts.head}.<span class="rounded bg-o2 px-0.5 text-od">{parts.tail}</span></div>
				{#if o.paymentStatus === 'proof_submitted'}
					<div class="mt-2 flex gap-2">
						<button class="btn sm ghost w-20" onclick={(e) => { e.preventDefault(); reject(o.id); }}>{c.reject}</button>
						<button class="btn sm mint flex-1" onclick={(e) => { e.preventDefault(); paid(o.id); }}>{c.markPaid}</button>
					</div>
				{/if}
			</a>
		{/each}
	</div>
</div>
