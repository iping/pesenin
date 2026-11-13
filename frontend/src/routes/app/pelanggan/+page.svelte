<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { loadMe, brandOf } from '$lib/session';
	import { api } from '$lib/api';
	import { rp, waPretty } from '$lib/format';
	let c = $derived(copy($lang));
	let rows = $state<any[]>([]);
	onMount(async () => {
		const me = await loadMe();
		rows = await api('/api/v1/brands/' + brandOf(me).id + '/customers');
	});
</script>

<div class="px-4 pt-4">
	<h1 class="text-2xl">{c.customers}</h1>
	<div class="mt-3 space-y-2">
		{#each rows as r}
			<div class="card p-3">
				<b class="fd">{r.name}</b>
				<div class="text-xs font-bold text-ink2">{waPretty(r.wa)} · {r.ordersCount} pesanan · Rp {rp(r.totalSpent)}</div>
				<a class="text-xs font-extrabold text-mintd" href={'https://wa.me/' + r.wa.replace('+', '')}>WhatsApp</a>
			</div>
		{:else}
			<p class="font-bold text-ink2">{c.emptyOrders}</p>
		{/each}
	</div>
</div>
