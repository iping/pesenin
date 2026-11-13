<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { loadMe, brandOf } from '$lib/session';
	import { api } from '$lib/api';

	let c = $derived(copy($lang));
	let storeId = $state('');
	let date = $state($page.url.searchParams.get('date') || '');
	let include = $state(false);
	let prep = $state<any>(null);
	let runs = $state<any>(null);

	onMount(async () => {
		const me = await loadMe();
		const brand = brandOf(me);
		if (!brand) return;
		storeId = brand.stores[0].id;
		if (!date) {
			const today = new Date();
			date = today.toISOString().slice(0, 10);
		}
		await load();
	});

	async function load() {
		if (!storeId || !date) return;
		const q = '?date=' + date + '&includeUnpaid=' + (include ? '1' : '0');
		prep = await api('/api/v1/stores/' + storeId + '/menu/prep' + q);
		runs = await api('/api/v1/stores/' + storeId + '/menu/runs' + q);
	}
</script>

<svelte:head>
	<title>{c.prepList}</title>
	<style>
		@media print {
			nav, .no-print { display: none !important; }
			body, .print-root { background: white !important; }
		}
	</style>
</svelte:head>

<div class="print-root space-y-3 px-4 pb-8 pt-4">
	<div class="no-print flex items-center gap-2">
		<a class="text-sm font-extrabold text-od" href="/app/menu">← {c.menu}</a>
	</div>
	<h1 class="text-2xl">{c.prepList}</h1>
	<div class="no-print flex flex-wrap items-center gap-2">
		<input class="h-10 rounded-xl border border-line px-2" type="date" bind:value={date} onchange={load} />
		<label class="flex items-center gap-1 text-sm font-extrabold"><input type="checkbox" bind:checked={include} onchange={load} /> {c.includeUnpaid}</label>
		<button class="btn sm" onclick={() => window.print()}>{c.print}</button>
	</div>

	{#if prep}
		<p class="text-sm font-extrabold text-ink2">{prep.label} · {prep.total} porsi</p>
		{#each prep.groups as g}
			<div class="card p-3">
				<div class="fd text-od">{g.label}</div>
				{#each g.items as it}
					<div class="flex items-baseline gap-2 border-b border-dashed border-line py-2">
						<b class="fd text-2xl">{it.qty}</b>
						<span class="font-extrabold">{it.name}{it.variant ? ` · ${it.variant}` : ''}</span>
					</div>
				{/each}
			</div>
		{:else}
			<p class="font-bold text-ink2">Belum ada pesanan untuk dimasak.</p>
		{/each}
		{#if storeId}
			<a class="btn ghost no-print" href="/api/v1/stores/{storeId}/menu/prep.csv?date={date}&includeUnpaid={include ? '1' : '0'}">{c.export} {c.prepList}</a>
		{/if}
	{/if}

	<h2 class="pt-2 text-xl">{c.runList}</h2>
	{#if runs}
		{#each runs.rows as row}
			<div class="card p-3 text-sm">
				<div class="flex items-center gap-2">
					<b class="fd flex-1">{row.name}</b>
					<span class="rounded-lg px-1.5 text-[10px] font-extrabold {row.method === 'delivery' ? 'bg-[#E8F2FD] text-[#2F8BE6]' : 'bg-mint2 text-mintd'}">{row.method === 'delivery' ? c.delivery : c.pickup}</span>
				</div>
				<div class="text-[11px] font-bold text-ink2">{row.slotLabel} · {row.code} · {row.wa}</div>
				<div class="mt-1 font-extrabold">{row.summary}</div>
				{#if row.address}<div class="text-[11px] font-bold text-ink2">{row.address}</div>{/if}
				{#if row.note}<div class="text-[11px] font-bold text-ink2">{row.note}</div>{/if}
			</div>
		{:else}
			<p class="font-bold text-ink2">Belum ada antar atau ambil.</p>
		{/each}
		{#if storeId}
			<a class="btn ghost no-print" href="/api/v1/stores/{storeId}/menu/runs.csv?date={date}&includeUnpaid={include ? '1' : '0'}">{c.export} {c.runList}</a>
		{/if}
	{/if}
</div>
