<script lang="ts">
	import { page } from '$app/stores';
	import { lang, copy } from '$lib/i18n';
	let c = $derived(copy($lang));
</script>

<div class="mx-auto max-w-md px-6 py-16 text-center">
	<img src="/icon-512.png" alt="" class="mx-auto h-20 w-20" />
	<h1 class="mt-3 text-2xl">{c.notFound}</h1>
	<p class="text-sm font-bold text-ink2">{$page.status} {$page.error?.message}</p>
	<a class="btn mt-4" href="/">{c.backHome}</a>
</div>
