<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api, ApiError, idem } from '$lib/api';
	import { rp } from '$lib/format';

	let c = $derived(copy($lang));
	let cart = $state<any>(null);
	let method = $state<'pickup' | 'delivery'>('pickup');
	let name = $state('');
	let wa = $state('');
	let address = $state('');
	let note = $state('');
	let err = $state('');
	let busy = $state(false);
	let quote = $state<any>(null);
	let slug = $derived($page.params.brand);

	let storeId = $derived($page.url.searchParams.get('store') || '');
	let date = $derived($page.url.searchParams.get('date') || '');
	let slot = $derived($page.url.searchParams.get('slot') || '');

	onMount(() => {
		const saved = localStorage.getItem('pn_guest');
		if (saved) {
			const g = JSON.parse(saved);
			name = g.name || '';
			wa = g.wa || '';
		}
		const raw = sessionStorage.getItem('menu:' + storeId + ':' + date + ':' + slot);
		cart = raw ? JSON.parse(raw) : null;
		if (cart && !cart.allowPickup) method = 'delivery';
		refresh().catch(() => {});
	});

	async function refresh() {
		if (!cart || !storeId) return;
		quote = await api('/api/v1/public/stores/' + storeId + '/menu/quote', {
			method: 'POST',
			body: JSON.stringify({
				date,
				mealSlot: slot,
				method,
				items: cart.items.map((it: any) => ({ productId: it.productId, variantId: it.variantId, qty: it.qty }))
			})
		});
	}

	$effect(() => {
		method;
		if (cart) refresh().catch(() => {});
	});

	async function submit() {
		err = '';
		busy = true;
		try {
			localStorage.setItem('pn_guest', JSON.stringify({ name, wa }));
			const order = await api<any>('/api/v1/public/stores/' + storeId + '/menu/orders', {
				method: 'POST',
				headers: { 'Idempotency-Key': idem() },
				body: JSON.stringify({
					date,
					mealSlot: slot,
					name,
					whatsapp: wa,
					method,
					address,
					note,
					items: cart.items.map((it: any) => ({ productId: it.productId, variantId: it.variantId, qty: it.qty }))
				})
			});
			sessionStorage.removeItem('menu:' + storeId + ':' + date + ':' + slot);
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
		<a class="grid h-9 w-9 place-items-center rounded-xl bg-white shadow-[0_2px_0_#F1E4D6]" href="/{slug}" aria-label="kembali">←</a>
		<div>
			<h1 class="text-xl">{c.checkout}</h1>
			<p class="text-[11px] font-extrabold text-ink2">{cart?.label || date}{cart?.slotLabel ? ` · ${cart.slotLabel}` : ''}</p>
		</div>
	</div>

	{#if !cart}
		<p class="p-6 font-bold">Keranjang menu kosong.</p>
	{:else}
		<div class="space-y-2.5 px-4 pt-3">
			<div class="card px-3.5 py-1">
				{#each cart.items as it}
					<div class="flex items-center gap-2 py-2">
						<div class="min-w-0 flex-1">
							<b>{it.name}{it.variantName ? ` · ${it.variantName}` : ''}</b>
							<div class="text-[11px] font-bold text-ink2">{it.qty} × Rp {rp(it.price)}</div>
						</div>
						<b class="fd">Rp {rp(it.qty * it.price)}</b>
					</div>
				{/each}
			</div>

			<div class="card p-3.5">
				<div class="fd mb-2">{c.receive}</div>
				<div class="grid grid-cols-2 gap-2">
					{#if cart.allowPickup}
						<button class="rounded-2xl border-2 p-2.5 text-left {method === 'pickup' ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => (method = 'pickup')}>
							<b class="fd">{c.pickup}</b>
						</button>
					{/if}
					{#if cart.allowDelivery}
						<button class="rounded-2xl border-2 p-2.5 text-left {method === 'delivery' ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => (method = 'delivery')}>
							<b class="fd">{c.delivery}</b>
						</button>
					{/if}
				</div>
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
				<div class="flex py-1 text-ink2"><span class="flex-1">{c.subtotal}</span><span class="text-ink">Rp {rp(quote?.subtotal || cart.items.reduce((s: number, it: any) => s + it.qty * it.price, 0))}</span></div>
				{#each quote?.charges || [] as ch}
					<div class="flex py-1"><span class="flex-1 text-ink2">{ch.name}</span><span>Rp {rp(ch.amount)}</span></div>
				{/each}
			</div>
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
	<button class="btn w-[180px]" disabled={busy || !name || !wa || !cart} onclick={submit}>{c.orderNow}</button>
</div>
