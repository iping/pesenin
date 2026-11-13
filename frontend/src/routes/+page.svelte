<script lang="ts">
	import { lang, copy } from '$lib/i18n';
	let c = $derived(copy($lang));
	function toggle() {
		lang.update((v) => (v === 'id' ? 'en' : 'id'));
	}
</script>

<div class="mx-auto grid min-h-screen max-w-5xl items-center gap-8 px-5 py-10 lg:grid-cols-2">
	<div>
		<img src="/icon-512.png" alt="Pesenin" class="h-20 w-20" />
		<p class="fd mt-4 text-sm text-od">{c.tagline} · {c.taglineId}</p>
		<h1 class="mt-2 text-4xl leading-tight text-ink">{c.headline}</h1>
		<p class="mt-3 text-lg font-bold text-ink2">{c.sub}</p>
		<ul class="mt-6 space-y-2 text-sm font-extrabold">
			<li class="rounded-2xl bg-white px-4 py-3 shadow-[0_2px_0_#F1E4D6]">{c.price}</li>
			<li class="rounded-2xl bg-white px-4 py-3 shadow-[0_2px_0_#F1E4D6]">{c.trust}</li>
			<li class="rounded-2xl bg-white px-4 py-3 shadow-[0_2px_0_#F1E4D6]">{c.guest}</li>
		</ul>
		<div class="mt-6 flex flex-wrap gap-3">
			<a class="btn px-5" href="/app/masuk">{c.sellerCta}</a>
			<button class="btn ghost px-4" onclick={toggle}>{c.lang}</button>
		</div>
		<p class="mt-4 text-xs font-bold text-ink3">{c.customerHint}</p>
	</div>
	<div class="card overflow-hidden p-4">
		<img src="/brand/placeholder-16x9.png" alt="" class="h-44 w-full rounded-2xl object-cover" />
		<div class="mt-4 flex items-center gap-3">
			<img src="/icon-512.png" alt="" class="h-12 w-12" />
			<div>
				<div class="fd text-lg">Dapur Senja</div>
				<div class="text-xs font-extrabold text-ink2">pesenin.product.biz.id/dapur-senja</div>
			</div>
		</div>
		<p class="mt-3 text-sm font-bold text-ink2">Rp 45.023 langsung ketahuan punya siapa. Pingping yang jaga kodenya.</p>
	</div>
</div>
