<script lang="ts">
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import { rp } from '$lib/format';

	let { shop, poCount = 0 }: { shop: any; poCount?: number } = $props();
	let c = $derived(copy($lang));
	let override = $state<any>(null);
	let menu = $derived(override ?? shop.menu);
	let selected = $state('');
	let slot = $state('');
	let qty = $state<Record<string, number>>({});

	let day = $derived.by(() => {
		const days = menu?.days || [];
		return (
			days.find((d: any) => d.date === selected) ||
			days.find((d: any) => d.orderable && d.items?.length) ||
			days.find((d: any) => d.items?.length) ||
			days[0]
		);
	});
	let activeSlot = $derived(day?.slots?.some((s: any) => s.id === slot) ? slot : day?.slots?.[0]?.id || '');

	function pick(next: any) {
		selected = next?.date || '';
		slot = next?.slots?.[0]?.id || '';
		qty = {};
	}

	let visible = $derived(
		(day?.items || []).filter((it: any) => !activeSlot || it.mealSlot === '' || it.mealSlot === activeSlot)
	);
	let lines = $derived(visible.filter((it: any) => (qty[keyOf(it)] || 0) > 0));
	let count = $derived(lines.reduce((s: number, it: any) => s + qty[keyOf(it)], 0));
	let sub = $derived(lines.reduce((s: number, it: any) => s + qty[keyOf(it)] * it.price, 0));

	function keyOf(it: any) {
		return it.productId + ':' + (it.variantId || '') + ':' + (it.mealSlot || '');
	}

	function maxOf(it: any) {
		return typeof it.remaining === 'number' ? it.remaining : 99;
	}

	function setQty(it: any, next: number) {
		const max = maxOf(it);
		if (next < 0) next = 0;
		if (next > max) next = max;
		qty = { ...qty, [keyOf(it)]: next };
	}

	async function shift(delta: number) {
		if (!menu?.start) return;
		const d = new Date(menu.start + 'T00:00:00Z');
		d.setUTCDate(d.getUTCDate() + delta * 7);
		const start = d.toISOString().slice(0, 10);
		override = await api('/api/v1/public/brands/' + shop.brand.slug + '/menu?start=' + start);
		selected = '';
	}

	function checkout() {
		if (!day || count === 0) return;
		const storeId = shop.store.id;
		const payload = {
			storeId,
			date: day.date,
			label: day.label,
			slot: activeSlot,
			slotLabel: day.slots?.find((s: any) => s.id === activeSlot)?.label || c.mealAll,
			allowPickup: menu.allowPickup,
			allowDelivery: menu.allowDelivery,
			items: lines.map((it: any) => ({
				productId: it.productId,
				variantId: it.variantId || null,
				name: it.name,
				variantName: it.variantName,
				price: it.price,
				qty: qty[keyOf(it)]
			}))
		};
		sessionStorage.setItem('menu:' + storeId + ':' + day.date + ':' + activeSlot, JSON.stringify(payload));
		goto(`/${shop.brand.slug}/menu?store=${storeId}&date=${day.date}&slot=${encodeURIComponent(activeSlot)}`);
	}

	function macros(n: any) {
		if (!n) return '';
		const bits = [];
		if (n.kcal != null) bits.push(n.kcal + ' kkal');
		if (n.proteinG != null) bits.push('P ' + n.proteinG + 'g');
		if (n.carbG != null) bits.push('K ' + n.carbG + 'g');
		if (n.fatG != null) bits.push('L ' + n.fatG + 'g');
		return bits.join(' · ');
	}
</script>

{#if menu?.enabled}
	<section class="card mt-2.5 p-3">
		<div class="flex items-center gap-2">
			<button class="grid h-8 w-8 place-items-center rounded-xl bg-o3 font-black text-od" onclick={() => shift(-1)} aria-label="minggu sebelumnya">‹</button>
			<div class="flex-1 text-center">
				<div class="fd text-base">{c.comingMenu}</div>
				<div class="text-[11px] font-bold text-ink2">{c.cutoff} {menu.cutoffLabel}</div>
			</div>
			<button class="grid h-8 w-8 place-items-center rounded-xl bg-o3 font-black text-od" onclick={() => shift(1)} aria-label="minggu berikutnya">›</button>
		</div>
		<div class="mt-2 flex gap-1.5 overflow-x-auto pb-1">
			{#each menu.days || [] as d}
				<button
					class="min-w-[64px] rounded-2xl px-2 py-1.5 text-center {d.date === day?.date ? 'bg-o text-white shadow-[0_3px_0_#E06F0A]' : 'bg-o3 text-ink'}"
					onclick={() => pick(d)}
				>
					<div class="text-[10px] font-extrabold">{d.label?.split(',')[0]}</div>
					<div class="fd text-sm">{d.label?.split(' ')[1]}</div>
				</button>
			{/each}
		</div>

		{#if day}
			{#if day.slots?.length}
				<div class="mt-2 flex gap-1.5">
					{#each day.slots as s}
						<button class="rounded-full px-3 py-1 text-xs font-extrabold {activeSlot === s.id ? 'bg-ink text-white' : 'bg-white text-ink2'}" onclick={() => (slot = s.id)}>{s.label}</button>
					{/each}
				</div>
			{/if}
			{#if !day.orderable}
				<p class="mt-2 rounded-xl bg-o3 px-3 py-2 text-xs font-bold text-od">{day.reason}</p>
			{/if}
			{#each visible as it}
				{@const left = it.remaining}
				<div class="flex items-center gap-3 border-b border-dashed border-line py-2.5">
					<div class="grid h-14 w-14 shrink-0 place-items-center overflow-hidden rounded-2xl bg-[#FFE7C2]">
						<img src={it.photoUrl || '/brand/placeholder-1x1.png'} alt="" class="h-full w-full object-cover" />
					</div>
					<div class="min-w-0 flex-1">
						<div class="fd text-[15px]">{it.name}{it.variantName ? ` · ${it.variantName}` : ''}</div>
						{#if macros(it.nutrition)}<div class="text-[11px] font-bold text-ink2">{macros(it.nutrition)}</div>{/if}
						{#if it.nutrition?.tagLabels?.length}
							<div class="mt-1 flex flex-wrap gap-1">
								{#each it.nutrition.tagLabels as tag}
									<span class="rounded-full bg-mint2 px-2 py-0.5 text-[10px] font-extrabold text-mintd">{tag}</span>
								{/each}
							</div>
						{/if}
						{#if it.nutrition?.allergens?.length}
							<div class="text-[10px] font-bold text-ink2">Alergen: {it.nutrition.allergens.join(', ')}</div>
						{/if}
						<div class="mt-1 flex items-center gap-2">
							<b class="fd text-[15px]">Rp {rp(it.price)}</b>
							{#if left != null && left <= 5}<span class="text-[11px] font-extrabold text-danger">Sisa {left}</span>{/if}
						</div>
					</div>
					{#if !day.orderable || left === 0}
						<span class="text-xs font-extrabold text-ink3">{left === 0 ? 'Habis' : ''}</span>
					{:else if qty[keyOf(it)]}
						<div class="flex h-[34px] items-center rounded-xl bg-o3 text-od">
							<button class="w-8 text-lg font-black" onclick={() => setQty(it, (qty[keyOf(it)] || 0) - 1)} aria-label="kurang">−</button>
							<b class="fd min-w-5 text-center text-ink">{qty[keyOf(it)]}</b>
							<button class="w-8 text-lg font-black" onclick={() => setQty(it, (qty[keyOf(it)] || 0) + 1)} aria-label="tambah">+</button>
						</div>
					{:else}
						<button class="grid h-[34px] w-[34px] place-items-center rounded-xl bg-o text-2xl text-white shadow-[0_3px_0_#E06F0A]" onclick={() => setQty(it, 1)} aria-label="tambah">+</button>
					{/if}
				</div>
			{:else}
				<p class="py-4 text-center text-sm font-bold text-ink2">Belum ada menu di hari ini.</p>
			{/each}
			{#if count > 0 && poCount > 0}
				<button class="btn mt-3" onclick={checkout}>{c.orderFor} {day.label}</button>
			{/if}
		{/if}
	</section>
	{#if count > 0 && poCount === 0}
		<div class="fixed inset-x-0 bottom-3 z-20 mx-auto flex h-[62px] max-w-[406px] items-center gap-2 rounded-[20px] bg-ink px-3.5 text-white shadow-[0_10px_24px_rgba(74,42,26,.35)]">
			<div class="flex-1">
				<div class="text-[11px] font-bold opacity-80">{day?.label}{activeSlot ? ` · ${day?.slots?.find((s: any) => s.id === activeSlot)?.label || ''}` : ''}</div>
				<b class="fd text-[17px]">Rp {rp(sub)}</b>
			</div>
			<button class="btn sm h-11" onclick={checkout}>{c.cart}</button>
		</div>
	{/if}
{/if}
