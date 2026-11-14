<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { browser } from '$app/environment';
	import { lang, copy } from '$lib/i18n';

	let splash = $state(true);
	let c = $derived(copy($lang));

	onMount(() => {
		if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});
		const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
		const seen = sessionStorage.getItem('pn_splash');
		if (reduce || seen) splash = false;
		else {
			sessionStorage.setItem('pn_splash', '1');
			setTimeout(() => (splash = false), 1200);
		}
	});
</script>

<svelte:head>
	<title>Pesenin · Hungry? Ping me</title>
</svelte:head>

{#if splash && browser}
	<div class="fixed inset-0 z-50 flex flex-col items-center justify-center bg-cream">
		<picture class="motion">
			<source srcset="/brand/loading.webp" type="image/webp" />
			<img src="/brand/loading.gif" alt="" class="h-28 w-28 object-contain" />
		</picture>
		<img src="/icon-512.png" alt="" class="hidden h-24 w-24 motion-off" style="display:none" />
		<p class="fd mt-3 text-lg">{c.loading}</p>
	</div>
{/if}

<slot />

<style>
	@media (prefers-reduced-motion: reduce) {
		.motion { display: none; }
	}
</style>
