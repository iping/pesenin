<script lang="ts">
	import { imageSet } from '$lib/images';

	let {
		src = '',
		alt = '',
		sizes = '',
		sharp = false,
		fit = 'cover' as 'cover' | 'contain',
		fill = true,
		eager = false,
		placeholder = 'square' as 'square' | 'wide' | 'avatar',
		class: klass = ''
	}: {
		src?: string | null;
		alt?: string;
		sizes?: string;
		sharp?: boolean;
		fit?: 'cover' | 'contain';
		fill?: boolean;
		eager?: boolean;
		placeholder?: 'square' | 'wide' | 'avatar';
		class?: string;
	} = $props();

	let loaded = $state(false);
	let failed = $state(false);

	const set = $derived(imageSet(src));
	const shown = $derived(sharp ? set.full || set.src : set.src);
	const srcset = $derived(sharp ? '' : set.srcset);
	const ph = $derived(
		placeholder === 'wide' ? '/brand/placeholder-16x9.png' : placeholder === 'avatar' ? '/brand/placeholder-avatar.png' : '/brand/placeholder-1x1.png'
	);

	$effect(() => {
		src;
		loaded = false;
		failed = false;
	});
</script>

<div class="relative overflow-hidden bg-[#E6E6E6] {fill ? 'h-full w-full' : 'w-full'} {klass}">
	{#if !shown || failed || !loaded}
		<img src={ph} alt="" class="{fill ? 'absolute inset-0 h-full w-full' : 'aspect-square w-full'} object-cover" />
	{/if}
	{#if shown && !failed}
		<img
			src={shown}
			srcset={srcset || undefined}
			sizes={srcset && sizes ? sizes : undefined}
			{alt}
			loading={eager ? 'eager' : 'lazy'}
			decoding="async"
			fetchpriority={eager ? 'auto' : 'low'}
			class="{loaded && !fill ? 'relative w-full' : 'absolute inset-0 h-full w-full'} {fit === 'contain' ? 'object-contain' : 'object-cover'} {loaded ? 'opacity-100' : 'opacity-0'}"
			onload={() => (loaded = true)}
			onerror={() => (failed = true)}
		/>
	{/if}
</div>
