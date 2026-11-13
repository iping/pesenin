<script lang="ts">
	import { goto } from '$app/navigation';
	import { browser } from '$app/environment';
	import { lang, copy } from '$lib/i18n';
	import { rp, countdown, syncClock, when } from '$lib/format';
	import MenuWeek from '$lib/components/MenuWeek.svelte';

	let { shop, origin }: { shop: any; origin: string } = $props();
	let c = $derived(copy($lang));
	let batch = $derived(shop.batches?.[0]);
	let qty = $state<Record<string, number>>({});
	let tick = $state(0);

	let tz = $derived(shop.store?.timezone || 'Asia/Jakarta');

	$effect(() => {
		syncClock(shop.serverNow || batch?.serverNow);
		if (!browser || !batch) return;
		const saved = sessionStorage.getItem('cart:' + batch.code);
		if (saved) qty = JSON.parse(saved);
		const id = setInterval(() => (tick += 1), 1000);
		return () => clearInterval(id);
	});

	function setQty(id: string, next: number, max: number | null) {
		if (next < 0) next = 0;
		if (max != null && next > max) next = max;
		qty = { ...qty, [id]: next };
		if (browser && batch) sessionStorage.setItem('cart:' + batch.code, JSON.stringify(qty));
	}

	let lines = $derived(
		(batch?.items || [])
			.filter((it: any) => (qty[it.id] || 0) > 0)
			.map((it: any) => ({ ...it, qty: qty[it.id] }))
	);
	let count = $derived(lines.reduce((s: number, it: any) => s + it.qty, 0));
	let sub = $derived(lines.reduce((s: number, it: any) => s + it.qty * it.price, 0));
	let sold = $derived((batch?.items || []).reduce((s: number, it: any) => s + (it.sold || 0), 0));
	let quota = $derived(batch?.quotaTotal || 0);
	let pct = $derived(quota ? Math.min(100, Math.round((sold / quota) * 100)) : 0);
	let clock = $derived(batch ? countdown(batch.closesAt) + (tick ? '' : '') : '');

	function checkout() {
		if (!batch || count === 0) return;
		goto(`/${shop.brand.slug}/keranjang?po=${batch.code}`);
	}

	function share() {
		const url = `${origin}/${shop.brand.slug}`;
		if (navigator.share) navigator.share({ title: shop.brand.name, url }).catch(() => {});
		else navigator.clipboard.writeText(url);
	}
</script>

<div class="mx-auto min-h-screen max-w-[430px] bg-cream pb-28">
	<div class="flex items-center gap-2 px-3 pt-3">
		<div class="flex h-9 flex-1 items-center justify-center gap-1 rounded-xl bg-white text-[13px] text-ink2 shadow-[0_1px_0_#F1E4D6]">
			<span class="font-extrabold text-ink">pesenin.product.biz.id</span>
			<span>/{shop.brand.slug}</span>
		</div>
		<button class="grid h-9 w-9 place-items-center rounded-xl bg-white text-ink2" onclick={share} aria-label={c.share}>↗</button>
	</div>

	<div class="relative mx-3 mt-3 h-[118px] overflow-hidden rounded-[22px] bg-gradient-to-br from-[#FFB35C] via-o to-[#F26D0F] text-white">
		<div class="absolute inset-0 opacity-20" style="background-image:radial-gradient(#fff 2px,transparent 2.5px);background-size:18px 18px"></div>
		<div class="absolute right-4 top-3 text-5xl">🥐</div>
		<div class="absolute right-24 top-10 text-4xl">☕</div>
		<div class="relative p-4">
			<div class="fd text-[13px] opacity-90">PO minggu ini</div>
			<div class="fd text-xl leading-tight">{batch?.title || shop.brand.name}</div>
		</div>
	</div>

	<div class="relative -mt-6 px-4">
		<div class="card flex items-center gap-3 p-3.5">
			<img src="/icon-512.png" alt="" class="h-14 w-14" />
			<div class="min-w-0 flex-1">
				<h1 class="text-[19px] leading-tight">{shop.brand.name}</h1>
				<div class="mt-1 flex items-center gap-2">
					<span class="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-extrabold text-white {shop.store.isOpen ? 'bg-[#2EAD5B]' : 'bg-[#9A9A9A]'}">
						<span class="h-1.5 w-1.5 rounded-full bg-white"></span>
						{shop.store.isOpen ? c.storeOpen : c.storeClosed}
					</span>
					{#if shop.store.address}<span class="truncate text-[11px] font-bold text-ink2">{shop.store.address}</span>{/if}
				</div>
			</div>
		</div>

		{#if !shop.store.isOpen && shop.store.acceptPreordersWhenClosed}
			<p class="mt-2 rounded-xl bg-o3 px-3 py-2 text-xs font-bold text-od">{c.closedNote}</p>
		{/if}

		<MenuWeek {shop} poCount={count} />

		{#if batch}
			<div class="card mt-2.5 border-2 border-o2 p-3.5">
				<div class="flex items-start gap-2">
					<div class="flex-1">
						<div class="fd text-base">{batch.title}</div>
						<div class="text-[11px] font-bold text-ink2">Tutup {when(batch.closesAt, tz)}</div>
						{#each batch.pickupSlots || [] as s}
							<div class="text-[11px] font-bold text-ink2">{s.label}</div>
						{/each}
					</div>
					<div class="fd text-sm font-semibold tracking-wide">{clock}</div>
				</div>
				{#if quota}
					<div class="mt-2 flex items-center gap-2">
						<div class="h-2.5 flex-1 overflow-hidden rounded-full bg-[#F3E6D8]"><div class="h-full rounded-full bg-gradient-to-r from-o to-[#FFB04D]" style="width:{pct}%"></div></div>
						<span class="text-[11px] font-extrabold text-od">{sold}/{quota} pcs</span>
					</div>
				{/if}
				<div class="mt-2 flex flex-wrap gap-1.5">
					{#if batch.allowPickup}<span class="rounded-full border border-line bg-white px-2.5 py-1 text-xs font-bold">{c.pickup}</span>{/if}
					{#if batch.allowDelivery}<span class="rounded-full border border-line bg-white px-2.5 py-1 text-xs font-bold">{c.delivery}</span>{/if}
				</div>
			</div>

			<div class="mt-2">
				{#each batch.items as it}
					{@const left = it.remaining}
					<div class="flex items-center gap-3 border-b border-dashed border-line py-2.5">
						<div class="grid h-16 w-16 shrink-0 place-items-center overflow-hidden rounded-2xl bg-[#FFE7C2]">
							{#if it.photoUrl}
								<img src={it.photoUrl} alt="" class="h-full w-full object-cover" />
							{:else}
								<img src="/brand/placeholder-1x1.png" alt="" class="h-full w-full object-cover" />
							{/if}
						</div>
						<div class="min-w-0 flex-1">
							<div class="fd text-[15px]">{it.name}{it.variantName ? ` · ${it.variantName}` : ''}</div>
							{#if it.description}<div class="line-clamp-2 text-[11px] font-bold text-ink2">{it.description}</div>{/if}
							<div class="mt-1 flex items-center gap-2">
								<b class="fd text-[15px]">Rp {rp(it.price)}</b>
								{#if left != null && left <= 5}<span class="text-[11px] font-extrabold text-danger">Sisa {left}</span>{/if}
							</div>
						</div>
						{#if left === 0}
							<span class="text-xs font-extrabold text-ink3">Habis</span>
						{:else if qty[it.id]}
							<div class="flex h-[34px] items-center rounded-xl bg-o3 text-od">
								<button class="w-8 text-lg font-black" onclick={() => setQty(it.id, (qty[it.id] || 0) - 1, left)} aria-label="kurang">−</button>
								<b class="fd min-w-5 text-center text-ink">{qty[it.id]}</b>
								<button class="w-8 text-lg font-black" onclick={() => setQty(it.id, (qty[it.id] || 0) + 1, left)} aria-label="tambah">+</button>
							</div>
						{:else}
							<button class="grid h-[34px] w-[34px] place-items-center rounded-xl bg-o text-2xl text-white shadow-[0_3px_0_#E06F0A]" onclick={() => setQty(it.id, 1, left)} aria-label="tambah">+</button>
						{/if}
					</div>
				{/each}
			</div>
		{:else if !shop.menu?.enabled}
			<div class="card mt-4 p-6 text-center">
				<img src="/brand/placeholder-1x1.png" alt="" class="mx-auto h-24 w-24 rounded-2xl" />
				<p class="mt-3 font-extrabold">{c.emptyOrders}</p>
			</div>
		{/if}
	</div>
</div>

{#if count > 0}
	<div class="fixed inset-x-0 bottom-3 z-20 mx-auto flex h-[62px] max-w-[406px] items-center gap-2 rounded-[20px] bg-ink px-3.5 text-white shadow-[0_10px_24px_rgba(74,42,26,.35)]">
		<div class="relative">
			<span class="text-2xl">🛍</span>
			<span class="absolute -right-2 -top-1 rounded-full border-2 border-ink bg-o px-1.5 text-[11px] font-black">{count}</span>
		</div>
		<div class="flex-1">
			<b class="fd text-[17px]">Rp {rp(sub)}</b>
		</div>
		<button class="btn sm h-11" onclick={checkout}>{c.cart}</button>
	</div>
{/if}
