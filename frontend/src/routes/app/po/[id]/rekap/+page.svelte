<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api, ApiError } from '$lib/api';
	import PingImage from '$lib/components/PingImage.svelte';

	let c = $derived(copy($lang));
	let rec = $state<any>(null);
	let include = $state(false);
	let err = $state('');
	let locked = $state(false);

	async function load() {
		err = '';
		try {
			rec = await api('/api/v1/batches/' + $page.params.id + '/recap?includeUnpaid=' + (include ? '1' : '0'));
			locked = false;
		} catch (e) {
			if (e instanceof ApiError && e.code === 'soft_lock') locked = true;
			err = e instanceof Error ? e.message : '';
		}
	}
	onMount(load);

	async function fulfil(id: string, status: string) {
		await api('/api/v1/orders/' + id + '/fulfilment', { method: 'POST', body: JSON.stringify({ status }) });
		await load();
	}
	async function wa(id: string) {
		const link = await api<{ url: string }>('/api/v1/orders/' + id + '/wa-link?template=ready');
		window.open(link.url, '_blank');
	}
</script>

<div class="space-y-3 px-4 pt-4">
	<a href="/app/po/{$page.params.id}" class="text-sm font-extrabold text-od">← PO</a>
	<h1 class="text-2xl">{c.recap}</h1>
	{#if locked}
		<div class="card p-4 text-center">
			<img src="/icon-512.png" alt="" class="mx-auto h-16 w-16" />
			<p class="mt-2 font-extrabold">{c.softLock}</p>
			<a class="btn mt-3" href="/app/koin">{c.fillKoin}</a>
		</div>
	{:else if rec}
		<div class="rounded-2xl bg-ink p-3 text-white">
			<b class="fd">Total {rec.products.reduce((s: number, p: any) => s + p.Qty, 0)} pcs</b>
			<div class="text-xs font-bold opacity-80">{rec.paidOrders} pesanan lunas</div>
		</div>
		<div class="grid grid-cols-2 gap-2">
			{#each rec.products as p}
				<div class="card p-2.5">
					<span class="relative h-8 w-8 shrink-0 overflow-hidden rounded-lg"><PingImage src={p.Photo} alt="" sizes="32px" /></span>
					<b class="fd text-xl">{p.Qty}</b>
					<div class="text-[11px] font-bold text-ink2">{p.Name} {p.Variant}</div>
				</div>
			{/each}
		</div>
		{#if rec.charges?.length}
			<div class="card p-3 text-sm font-bold">
				{#each rec.charges as ch}<div class="flex"><span class="flex-1">{ch.Name} · {ch.Count}</span><span>Rp {ch.Total.toLocaleString('id-ID')}</span></div>{/each}
			</div>
		{/if}
		<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={include} onchange={load} /> {c.includeUnpaid}</label>
		<div class="card p-3">
			<div class="fd mb-1">{c.checklist}</div>
			{#each rec.checklist as row}
				<div class="flex items-center gap-2 border-b border-dashed border-line py-2">
					<button class="grid h-7 w-7 place-items-center rounded-lg {row.Status === 'completed' ? 'bg-mint text-white' : 'border-2 border-[#D8C9BB]'}" onclick={() => fulfil(row.OrderID, row.Status === 'not_ready' ? 'ready' : 'completed')} aria-label="centang">{row.Status === 'completed' ? '✓' : ''}</button>
					<div class="flex-1">
						<b class="text-sm {row.Status === 'completed' ? 'line-through' : ''}">{row.Name}</b>
						<div class="text-[11px] font-bold text-ink2">{row.Summary}</div>
					</div>
					<button class="text-[11px] font-extrabold text-mintd" onclick={() => wa(row.OrderID)}>WA</button>
					<span class="rounded-lg px-1.5 text-[10px] font-extrabold {row.Method === 'delivery' ? 'bg-[#E8F2FD] text-[#2F8BE6]' : 'bg-mint2 text-mintd'}">{row.Method === 'delivery' ? c.delivery : c.pickup}</span>
				</div>
			{/each}
		</div>
		<a class="btn ghost" href="/api/v1/batches/{$page.params.id}/recap.csv?includeUnpaid={include ? '1' : '0'}">{c.export}</a>
	{/if}
	{#if err && !locked}<p class="text-sm font-bold text-danger">{err}</p>{/if}
</div>
