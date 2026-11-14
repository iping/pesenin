<script lang="ts">
	import { imageSet } from '$lib/images';
	import { lang, copy } from '$lib/i18n';

	let { url, compact = false }: { url: string; compact?: boolean } = $props();
	let c = $derived(copy($lang));
	let open = $state(false);
	let loaded = $state(false);
	let failed = $state(false);
	let zoomPx = $state(0);
	const set = $derived(imageSet(url));
	const full = $derived(set.full || url);

	function openZoom() {
		const dpr = Math.min(window.devicePixelRatio || 2, 3);
		const px = set.fullWidth || 1600;
		zoomPx = Math.max(320, Math.round(px / dpr));
		open = true;
	}
</script>

<button type="button" class="block w-full text-left" onclick={openZoom}>
	<span class="relative block overflow-hidden rounded-2xl bg-[#E6E6E6]">
		{#if !loaded || failed}
			<img src="/brand/placeholder-1x1.png" alt="" class="{compact ? 'h-40' : 'aspect-[3/4] max-h-80'} w-full object-cover" />
		{/if}
		{#if !failed}
			<img
				src={full}
				alt="bukti transfer"
				loading="lazy"
				decoding="async"
				class="w-full bg-white object-contain {compact ? 'max-h-48' : 'max-h-80'} {loaded ? 'relative' : 'absolute inset-0 opacity-0'}"
				onload={() => (loaded = true)}
				onerror={() => (failed = true)}
			/>
		{/if}
	</span>
	<span class="mt-1 block text-center text-[11px] font-extrabold text-od">{c.zoomProof}</span>
</button>

{#if open}
	<div class="fixed inset-0 z-50 overflow-auto bg-[#1b1b1b]" role="dialog" aria-modal="true">
		<button type="button" class="sticky top-0 z-10 w-full bg-black/80 py-3 text-sm font-extrabold text-white" onclick={() => (open = false)}>{c.close}</button>
		<img src={full} alt="bukti transfer" class="mx-auto block max-w-none" style="width: {zoomPx}px" />
	</div>
{/if}
