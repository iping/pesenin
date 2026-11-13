<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { loadMe, brandOf } from '$lib/session';
	import { api } from '$lib/api';
	import { when } from '$lib/format';

	let c = $derived(copy($lang));
	let rows = $state<any[]>([]);
	let brandId = $state('');
	onMount(async () => {
		const me = await loadMe();
		brandId = brandOf(me).id;
		rows = await api('/api/v1/brands/' + brandId + '/batches');
	});
</script>

<div class="px-4 pt-4">
	<div class="flex items-center">
		<h1 class="flex-1 text-2xl">PO</h1>
		<a class="mr-2 text-sm font-extrabold text-od" href="/app/menu">{c.menu}</a>
		<a class="btn sm" href="/app/po/baru">{c.makePo}</a>
	</div>
	<div class="mt-3 space-y-2">
		{#each rows as b}
			<a class="card block p-3.5" href="/app/po/{b.id}">
				<div class="flex"><b class="fd flex-1">{b.title}</b><span class="text-xs font-extrabold text-od">{b.status}</span></div>
				<div class="text-[11px] font-bold text-ink2">{b.code} · {when(b.closesAt)}</div>
			</a>
		{:else}
			<p class="font-bold text-ink2">{c.emptyOrders}</p>
		{/each}
	</div>
</div>
