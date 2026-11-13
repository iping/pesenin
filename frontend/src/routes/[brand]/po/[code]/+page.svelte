<script lang="ts">
	import Shop from '$lib/components/Shop.svelte';
	let { data } = $props();
	const title = data.shop.brand.name + ' · ' + (data.shop.batches[0]?.title || 'PO');
</script>

<svelte:head>
	<title>{title}</title>
	<meta property="og:title" content={title} />
	<meta property="og:description" content={data.shop.brand.description || 'Pesan PO tanpa daftar.'} />
	<meta property="og:image" content={data.origin + '/icon-512.png'} />
</svelte:head>

<Shop shop={data.shop} origin={data.origin} />
