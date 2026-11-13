<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api, ApiError, idem } from '$lib/api';
	import { rp, syncClock, when } from '$lib/format';

	let c = $derived(copy($lang));
	let code = $derived($page.url.searchParams.get('po') || '');
	let batch = $state<any>(null);
	let charges = $state<any[]>([]);
	let brand = $state<any>(null);
	let store = $state<any>(null);
	let qty = $state<Record<string, number>>({});
	let method = $state<'pickup' | 'delivery'>('pickup');
	let name = $state('');
	let wa = $state('');
	let address = $state('');
	let note = $state('');
	let err = $state('');
	let busy = $state(false);
	let quote = $state<any>(null);
	let slotIndex = $state(0);
	let tz = $state('Asia/Jakarta');

	onMount(async () => {
		const saved = localStorage.getItem('pn_guest');
		if (saved) {
			const g = JSON.parse(saved);
			name = g.name || '';
			wa = g.wa || '';
		}
		const cart = sessionStorage.getItem('cart:' + code);
		if (cart) qty = JSON.parse(cart);
		const pageData = await api<any>('/api/v1/public/batches/' + code);
		batch = pageData.batch;
		charges = pageData.charges || [];
		brand = pageData.brand;
		store = pageData.store;
		tz = pageData.store?.timezone || 'Asia/Jakarta';
		syncClock(pageData.serverNow || pageData.batch?.serverNow);
		if (!pageData.batch.allowPickup) method = 'delivery';
		await refresh();
	});

	let lines = $derived((batch?.items || []).filter((it: any) => qty[it.id] > 0).map((it: any) => ({ ...it, qty: qty[it.id] })));

	async function refresh() {
		if (!code || lines.length === 0) return;
		quote = await api('/api/v1/public/batches/' + code + '/quote', {
			method: 'POST',
			body: JSON.stringify({
				method,
				whatsapp: wa,
				slotIndex,
				items: lines.map((it: any) => ({ batchItemId: it.id, qty: it.qty }))
			})
		});
	}

	$effect(() => {
		method;
		if (batch) refresh().catch(() => {});
	});

	function shownCharges() {
		return (charges || []).filter((ch) => (ch.methods || []).includes(method));
	}

	async function submit() {
		err = '';
		busy = true;
		try {
			localStorage.setItem('pn_guest', JSON.stringify({ name, wa }));
			const order = await api<any>('/api/v1/public/batches/' + code + '/orders', {
				method: 'POST',
				headers: { 'Idempotency-Key': idem() },
				body: JSON.stringify({
					name,
					whatsapp: wa,
					method,
					address,
					note,
					slotIndex,
					items: lines.map((it: any) => ({ batchItemId: it.id, qty: it.qty }))
				})
			});
			sessionStorage.removeItem('cart:' + code);
			goto('/o/' + order.publicToken);
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal pesan.';
		} finally {
			busy = false;
		}
	}
</script>

<div class="mx-auto min-h-screen max-w-[430px] bg-cream pb-36">
	<div class="flex items-center gap-2 px-4 pt-4">
		<a class="grid h-9 w-9 place-items-center rounded-xl bg-white shadow-[0_2px_0_#F1E4D6]" href="/{brand?.slug || ''}" aria-label="kembali">←</a>
		<div>
			<h1 class="text-xl">{c.checkout}</h1>
			<p class="text-[11px] font-extrabold text-ink2">{brand?.name} · {batch?.title}</p>
		</div>
	</div>

	{#if !batch}
		<p class="p-6 font-bold">{c.loading}</p>
	{:else}
		<div class="space-y-2.5 px-4 pt-3">
			<div class="card px-3.5 py-1">
				{#each lines as it}
					<div class="flex items-center gap-2 py-2">
						<div class="min-w-0 flex-1">
							<b>{it.name}</b>
							<div class="text-[11px] font-bold text-ink2">{it.qty} × Rp {rp(it.price)}</div>
						</div>
						<b class="fd">Rp {rp(it.qty * it.price)}</b>
					</div>
				{/each}
			</div>

			<div class="card p-3.5">
				<div class="fd mb-2">{c.receive}</div>
				<div class="grid grid-cols-2 gap-2">
					{#if batch.allowPickup}
						<button class="rounded-2xl border-2 p-2.5 text-left {method === 'pickup' ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => (method = 'pickup')}>
							<b class="fd">{c.pickup}</b>
							<div class="text-[11px] font-bold text-ink2">{c.freeShip}</div>
						</button>
					{/if}
					{#if batch.allowDelivery}
						<button class="rounded-2xl border-2 p-2.5 text-left {method === 'delivery' ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => (method = 'delivery')}>
							<b class="fd">{c.delivery}</b>
							<div class="text-[11px] font-bold text-ink2">+ ongkir</div>
						</button>
					{/if}
				</div>
				{#if method === 'pickup' && store?.address}
					<p class="mt-2 rounded-xl bg-mint2 px-3 py-2 text-xs font-extrabold text-mintd">{store.address}</p>
				{/if}
				{#if (batch.pickupSlots || []).length}
					<div class="mt-2 space-y-1">
						{#each batch.pickupSlots as s, i}
							{@const full = s.remainingQty === 0 || s.remainingOrders === 0}
							<button type="button" disabled={full} class="block w-full rounded-xl border px-3 py-2 text-left text-xs font-extrabold {slotIndex === i ? 'border-o bg-o3 text-od' : 'border-line'} {full ? 'opacity-50' : ''}" onclick={() => (slotIndex = i)}>
								{s.label || when(s.start, tz)}
								{#if s.remainingQty != null} · Sisa {s.remainingQty}{/if}
								{#if full} · Penuh{/if}
							</button>
						{/each}
					</div>
				{/if}
				{#if method === 'delivery'}
					<label class="mt-2 block text-xs font-extrabold text-ink2">{c.address}
						<input class="mt-1 h-11 w-full rounded-xl border border-line bg-[#FFFCF8] px-3" bind:value={address} />
					</label>
				{/if}
			</div>

			<div class="card p-3.5">
				<div class="flex items-center"><div class="fd flex-1">{c.buyer}</div><span class="text-[11px] font-bold text-ink2">{c.noAccount}</span></div>
				<div class="mt-2 grid gap-2">
					<input class="h-11 rounded-xl border border-line bg-[#FFFCF8] px-3" placeholder={c.name} bind:value={name} />
					<input class="h-11 rounded-xl border border-line bg-[#FFFCF8] px-3" placeholder="08…" bind:value={wa} inputmode="tel" />
					<input class="h-11 rounded-xl border border-line bg-[#FFFCF8] px-3" placeholder={c.note} bind:value={note} />
				</div>
			</div>

			<div class="card px-3.5 py-2 text-sm font-extrabold">
				<div class="flex py-1 text-ink2"><span class="flex-1">{c.subtotal}</span><span class="text-ink">Rp {rp(quote?.subtotal || lines.reduce((s: number, it: any) => s + it.qty * it.price, 0))}</span></div>
				{#each quote?.charges || shownCharges() as ch}
					<div class="flex py-1"><span class="flex-1 text-ink2">{ch.name}</span><span>Rp {rp(ch.amount)}</span></div>
				{/each}
			</div>
			{#if batch.capacity?.minQty || batch.capacity?.minAmountRp || batch.capacity?.maxQtyPerCustomer || batch.capacity?.remainingQty != null}
				<p class="rounded-xl bg-o3 px-3 py-2 text-xs font-extrabold text-od">
					{#if batch.capacity.remainingQty != null}Sisa {batch.capacity.remainingQty} pcs. {/if}
					{#if batch.capacity.remainingOrders != null}Sisa {batch.capacity.remainingOrders} pesanan. {/if}
					{#if batch.capacity.minQty}Min {batch.capacity.minQty} pcs. {/if}
					{#if batch.capacity.minAmountRp}Min Rp {rp(batch.capacity.minAmountRp)}. {/if}
					{#if batch.capacity.maxQtyPerCustomer}Maks {batch.capacity.maxQtyPerCustomer} pcs per orang.{/if}
				</p>
			{/if}
			{#each quote?.problems || [] as problem}
				<p class="rounded-xl bg-red-50 px-3 py-2 text-sm font-extrabold text-danger">{problem}</p>
			{/each}
			{#if err}<p class="rounded-xl bg-red-50 px-3 py-2 text-sm font-extrabold text-danger">{err}</p>{/if}
		</div>
	{/if}
</div>

<div class="fixed inset-x-0 bottom-0 z-20 mx-auto flex max-w-[430px] items-center gap-3 rounded-t-[22px] bg-white px-4 py-3 shadow-[0_-6px_20px_rgba(74,42,26,.08)]">
	<div class="flex-1">
		<div class="text-[11px] font-extrabold text-ink2">{c.totalPay}</div>
		<b class="fd text-[22px] leading-none">Rp {rp(quote?.baseTotal || 0)}</b>
		<div class="text-[10px] font-extrabold text-ink3">{c.uniqueLater}</div>
	</div>
	<button class="btn w-[180px]" disabled={busy || !name || !wa || (quote?.problems || []).length > 0} onclick={submit}>{c.orderNow}</button>
</div>
