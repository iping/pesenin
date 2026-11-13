<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { rp, when } from '$lib/format';
	import ProofImage from '$lib/components/ProofImage.svelte';

	let c = $derived(copy($lang));
	let o = $state<any>(null);
	let wa = $state('');

	onMount(async () => {
		o = await api('/api/v1/orders/' + $page.params.id);
		const link = await api<{ url: string }>('/api/v1/orders/' + o.id + '/wa-link?template=confirmation');
		wa = link.url;
	});

	async function act(path: string, body?: any) {
		o = await api('/api/v1/orders/' + o.id + path, { method: 'POST', body: body ? JSON.stringify(body) : undefined });
	}
</script>

{#if o}
	<div class="space-y-3 px-4 pt-4">
		<a href="/app/pesanan" class="text-sm font-extrabold text-od">← {c.orders}</a>
		<h1 class="text-2xl">{o.code}</h1>
		<p class="text-sm font-bold text-ink2">{o.customer.name} · {o.customer.wa}</p>
		<div class="card p-3 text-sm font-bold">
			{#each o.items as it}<div class="flex py-1"><span class="flex-1">{it.qty}× {it.name}</span><span>Rp {rp(it.unitPrice * it.qty)}</span></div>{/each}
			{#each o.charges as ch}<div class="flex py-1"><span class="flex-1">{ch.name}</span><span>Rp {rp(ch.amount)}</span></div>{/each}
			<div class="flex py-1"><span class="flex-1">{c.uniqueCode}</span><span>Rp {rp(o.uniqueSuffix)}</span></div>
			<div class="fd flex pt-1 text-lg"><span class="flex-1">{c.transferTotal}</span><span>Rp {rp(o.totalDue)}</span></div>
			<p class="mt-1 text-[11px] text-ink2">{o.paymentStatus} · {o.fulfilmentStatus} · {when(o.createdAt)}</p>
		</div>
		{#each o.proofs as p}
			<ProofImage url={p.url} />
		{/each}
		<div class="grid grid-cols-2 gap-2">
			{#if o.paymentStatus === 'pending' || o.paymentStatus === 'proof_submitted'}
				<button class="btn mint" onclick={() => act('/mark-paid')}>{c.markPaid}</button>
				<button class="btn ghost" onclick={() => act('/reject-proof', { reason: prompt(c.reason) || 'Bukti tidak terbaca' })}>{c.reject}</button>
				<button class="btn ghost" onclick={() => act('/cancel')}>{c.cancel}</button>
			{/if}
			{#if o.paymentStatus === 'paid' && o.fulfilmentStatus === 'not_ready'}
				<button class="btn" onclick={() => act('/fulfilment', { status: 'ready' })}>{c.ready}</button>
			{/if}
			{#if o.paymentStatus === 'paid' && o.fulfilmentStatus === 'ready'}
				<button class="btn mint" onclick={() => act('/fulfilment', { status: 'completed' })}>{c.done}</button>
				<button class="btn ghost" onclick={() => act('/fulfilment', { status: 'no_show' })}>Tidak diambil</button>
			{/if}
		</div>
		{#if wa}<a class="btn wa" href={wa} target="_blank" rel="noreferrer">{c.shareWa}</a>{/if}
	</div>
{/if}
