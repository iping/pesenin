<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { when } from '$lib/format';
	let c = $derived(copy($lang));
	let rows = $state<any[]>([]);
	onMount(async () => {
		rows = await api('/api/v1/notifications');
		await api('/api/v1/notifications/read', { method: 'POST', body: '{}' });
	});
</script>

<div class="px-4 pt-4">
	<h1 class="text-2xl">{c.notifications}</h1>
	<div class="mt-3 space-y-2">
		{#each rows as n}
			<a class="card block p-3" href={n.link || '/app'}>
				<b class="fd">{n.title}</b>
				<p class="text-sm font-bold">{n.body}</p>
				<div class="text-[11px] font-bold text-ink2">{when(n.createdAt)}</div>
			</a>
		{:else}
			<p class="font-bold text-ink2">Ping! Belum ada kabar.</p>
		{/each}
	</div>
</div>
