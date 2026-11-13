<script lang="ts">
	import { onMount } from 'svelte';
	import { loadMe, brandOf } from '$lib/session';
	import { api, ApiError } from '$lib/api';

	let brandId = $state('');
	let data = $state<any>(null);
	let err = $state('');
	let saved = $state('');
	let brand = $state<any>({});
	let storeOn = $state(false);
	let store = $state<any>({});
	let products = $state<any[]>([]);

	function blank(v: unknown) {
		return v == null ? '' : String(v);
	}
	function formOf(src: any) {
		return {
			minQty: blank(src?.minQty),
			minAmountRp: blank(src?.minAmountRp),
			maxQtyPerCustomer: blank(src?.maxQtyPerCustomer),
			capacityQty: blank(src?.capacityQty),
			capacityOrders: blank(src?.capacityOrders),
			minProceedQty: blank(src?.minProceedQty),
			cutoffLeadMinutes: blank(src?.cutoffLeadMinutes),
			waitlistEnabled: !!src?.waitlistEnabled
		};
	}
	function num(v: string) {
		return v === '' ? null : Number(v);
	}
	function payload(f: any) {
		return {
			minQty: num(f.minQty),
			minAmountRp: num(f.minAmountRp),
			maxQtyPerCustomer: num(f.maxQtyPerCustomer),
			capacityQty: num(f.capacityQty),
			capacityOrders: num(f.capacityOrders),
			minProceedQty: num(f.minProceedQty),
			cutoffLeadMinutes: num(f.cutoffLeadMinutes),
			waitlistEnabled: !!f.waitlistEnabled
		};
	}

	onMount(async () => {
		const me = await loadMe();
		brandId = brandOf(me).id;
		await refresh();
	});

	async function refresh() {
		data = await api('/api/v1/brands/' + brandId + '/capacity');
		brand = formOf(data.brand);
		const st = data.stores?.[0];
		storeOn = st ? !st.inherit : false;
		store = formOf(storeOn ? st.override : st?.effective);
		products = (data.products || []).map((p: any) => ({
			...p,
			minQty: blank(p.minQty),
			maxPerCustomer: blank(p.maxPerCustomer),
			capacityQty: blank(p.capacityQty)
		}));
	}

	async function saveBrand() {
		err = '';
		saved = '';
		try {
			data = await api('/api/v1/brands/' + brandId + '/capacity', { method: 'PUT', body: JSON.stringify(payload(brand)) });
			saved = 'Aturan brand tersimpan.';
			await refresh();
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal.';
		}
	}
	async function saveStore() {
		err = '';
		saved = '';
		const id = data.stores?.[0]?.storeId;
		if (!id) return;
		try {
			await api('/api/v1/stores/' + id + '/capacity', {
				method: 'PUT',
				body: JSON.stringify({ ...payload(store), inherit: !storeOn })
			});
			saved = storeOn ? 'Aturan toko tersimpan.' : 'Toko ikut aturan brand.';
			await refresh();
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal.';
		}
	}
	async function saveProduct(p: any) {
		err = '';
		saved = '';
		try {
			await api('/api/v1/products/' + p.productId + '/capacity', {
				method: 'PUT',
				body: JSON.stringify({
					minQty: num(p.minQty),
					maxPerCustomer: num(p.maxPerCustomer),
					capacityQty: num(p.capacityQty)
				})
			});
			saved = p.name + ' tersimpan.';
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal.';
		}
	}
</script>

<div class="space-y-3 px-4 pt-4 pb-8">
	<a href="/app/akun" class="text-sm font-extrabold text-od">← Akun</a>
	<h1 class="text-2xl">Aturan kapasitas</h1>
	<p class="text-xs font-bold text-ink2">Kosong berarti tidak dibatasi. Aturan brand jadi bawaan PO baru. Toko bisa menimpa. Kapasitas tidak boleh diturunkan di bawah pesanan yang sudah masuk.</p>
	{#if err}<p class="rounded-xl bg-red-50 px-3 py-2 text-sm font-extrabold text-danger">{err}</p>{/if}
	{#if saved}<p class="rounded-xl bg-mint2 px-3 py-2 text-sm font-extrabold text-mintd">{saved}</p>{/if}

	<div class="card space-y-2 p-3.5">
		<div class="fd">Bawaan brand</div>
		<div class="grid grid-cols-2 gap-2">
			<label class="text-xs font-extrabold text-ink2">Min pcs<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={brand.minQty} /></label>
			<label class="text-xs font-extrabold text-ink2">Min Rp<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={brand.minAmountRp} /></label>
			<label class="text-xs font-extrabold text-ink2">Maks / orang<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={brand.maxQtyPerCustomer} /></label>
			<label class="text-xs font-extrabold text-ink2">Kuota pcs<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={brand.capacityQty} /></label>
			<label class="text-xs font-extrabold text-ink2">Kuota pesanan<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={brand.capacityOrders} /></label>
			<label class="text-xs font-extrabold text-ink2">Min lanjut<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={brand.minProceedQty} /></label>
			<label class="text-xs font-extrabold text-ink2">Lead menit<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={brand.cutoffLeadMinutes} /></label>
		</div>
		<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={brand.waitlistEnabled} /> Daftar tunggu</label>
		<button class="btn sm w-full" onclick={saveBrand}>Simpan brand</button>
	</div>

	{#if data?.stores?.[0]}
		<div class="card space-y-2 p-3.5">
			<div class="fd">Toko {data.stores[0].name}</div>
			<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={storeOn} /> Pakai aturan khusus toko ini</label>
			{#if storeOn}
				<div class="grid grid-cols-2 gap-2">
					<label class="text-xs font-extrabold text-ink2">Min pcs<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={store.minQty} /></label>
					<label class="text-xs font-extrabold text-ink2">Min Rp<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={store.minAmountRp} /></label>
					<label class="text-xs font-extrabold text-ink2">Maks / orang<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={store.maxQtyPerCustomer} /></label>
					<label class="text-xs font-extrabold text-ink2">Kuota pcs<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={store.capacityQty} /></label>
					<label class="text-xs font-extrabold text-ink2">Kuota pesanan<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={store.capacityOrders} /></label>
					<label class="text-xs font-extrabold text-ink2">Min lanjut<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={store.minProceedQty} /></label>
				</div>
				<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={store.waitlistEnabled} /> Daftar tunggu</label>
			{:else}
				<p class="text-xs font-bold text-ink2">PO baru di toko ini memakai bawaan brand.</p>
			{/if}
			<button class="btn sm w-full" onclick={saveStore}>Simpan toko</button>
		</div>
	{/if}

	<div class="card space-y-3 p-3.5">
		<div class="fd">Per produk</div>
		<p class="text-xs font-bold text-ink2">Dipakai saat produk masuk PO, kalau kuota di PO dikosongkan.</p>
		{#each products as p}
			<div class="border-t border-dashed border-line pt-2">
				<div class="text-sm font-extrabold">{p.name}</div>
				<div class="mt-1 grid grid-cols-3 gap-1">
					<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="kuota" bind:value={p.capacityQty} />
					<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="min" bind:value={p.minQty} />
					<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="maks" bind:value={p.maxPerCustomer} />
				</div>
				<button class="mt-1 text-xs font-extrabold text-od" onclick={() => saveProduct(p)}>Simpan</button>
			</div>
		{/each}
	</div>
</div>
