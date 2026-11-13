<script lang="ts">
	import Shop from '$lib/components/Shop.svelte';
	let { data } = $props();
	const title = data.shop.brand.name + ' · Pesenin';
	const desc = data.shop.batches?.[0]?.title || data.shop.brand.description || 'Pesan tanpa daftar.';
</script>

<svelte:head>
	<title>{title}</title>
	<meta property="og:title" content={title} />
	<meta property="og:description" content={desc} />
	<meta property="og:image" content={data.origin + '/icon-512.png'} />
	<meta property="og:url" content={data.origin + '/' + data.shop.brand.slug} />
</svelte:head>

<Shop shop={data.shop} origin={data.origin} />
