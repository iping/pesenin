<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { me, loadMe, brandOf } from '$lib/session';
	import { api } from '$lib/api';

	let c = $derived(copy($lang));
	let products = $state<any[]>([]);
	let charges = $state<any[]>([]);
	let banks = $state<any[]>([]);
	let bankCodes = $state<any[]>([]);
	let templates = $state<any[]>([]);
	let pname = $state('');
	let pprice = $state(10000);
	let pdesc = $state('');
	let cname = $state('Packing');
	let camount = $state(2000);
	let cmethod = $state('both');
	let bcode = $state('BCA');
	let bnum = $state('');
	let bholder = $state('');
	let brand = $derived(brandOf($me));
	let nuts = $state<Record<string, any>>({});

	onMount(async () => {
		await loadMe();
		bankCodes = await api('/api/v1/bank-codes');
		await refresh();
	});

	async function refresh() {
		if (!brand) return;
		products = await api('/api/v1/brands/' + brand.id + '/products');
		const next: Record<string, any> = {};
		for (const p of products) {
			const n = p.nutrition || {};
			next[p.id] = {
				kcal: n.kcal ?? '',
				proteinG: n.proteinG ?? '',
				carbG: n.carbG ?? '',
				fatG: n.fatG ?? '',
				allergens: (n.allergens || []).join(', '),
				tags: (n.tags || []).join(', ')
			};
		}
		nuts = next;
		charges = await api('/api/v1/brands/' + brand.id + '/extra-charges');
		banks = await api('/api/v1/brands/' + brand.id + '/bank-accounts');
		templates = await api('/api/v1/brands/' + brand.id + '/templates');
	}

	function numOrNull(v: any) {
		if (v === '' || v == null) return null;
		const n = Number(v);
		return Number.isFinite(n) ? n : null;
	}
	async function saveNut(id: string) {
		const n = nuts[id];
		await api('/api/v1/products/' + id + '/nutrition', {
			method: 'PUT',
			body: JSON.stringify({
				kcal: numOrNull(n.kcal),
				proteinG: numOrNull(n.proteinG),
				carbG: numOrNull(n.carbG),
				fatG: numOrNull(n.fatG),
				allergens: String(n.allergens || '').split(',').map((s) => s.trim()).filter(Boolean),
				tags: String(n.tags || '').split(',').map((s) => s.trim()).filter(Boolean)
			})
		});
		await refresh();
	}
	async function addProduct() {
		await api('/api/v1/brands/' + brand.id + '/products', { method: 'POST', body: JSON.stringify({ name: pname, price: Number(pprice), description: pdesc }) });
		pname = '';
		await refresh();
	}
	async function photo(id: string, file: File) {
		const body = new FormData();
		body.append('file', file);
		await fetch('/api/v1/products/' + id + '/photo', { method: 'POST', body, credentials: 'include' });
		await refresh();
	}
	async function addCharge() {
		const methods = cmethod === 'both' ? ['pickup', 'delivery'] : [cmethod];
		await api('/api/v1/brands/' + brand.id + '/extra-charges', {
			method: 'POST',
			body: JSON.stringify({ name: cname, amount: Number(camount), methods, active: true })
		});
		await refresh();
	}
	async function toggleCharge(ch: any) {
		await api('/api/v1/extra-charges/' + ch.ID, {
			method: 'PATCH',
			body: JSON.stringify({ name: ch.Name, amount: ch.AmountRp, methods: ch.FulfilmentMethods, active: !ch.Active })
		});
		await refresh();
	}
	async function addBank() {
		await api('/api/v1/brands/' + brand.id + '/bank-accounts', {
			method: 'POST',
			body: JSON.stringify({ bankCode: bcode, accountNumber: bnum, holderName: bholder, isDefault: banks.length === 0 })
		});
		bnum = '';
		await refresh();
	}
	async function saveTpl(t: any) {
		await api('/api/v1/brands/' + brand.id + '/templates', { method: 'PUT', body: JSON.stringify({ kind: t.Kind, body: t.Body }) });
	}
	async function preorder(v: boolean) {
		const st = brand.stores[0];
		await api('/api/v1/stores/' + st.id, {
			method: 'PATCH',
			body: JSON.stringify({ name: st.name, address: st.address, mapsUrl: st.mapsUrl, isOpen: st.isOpen, acceptPreordersWhenClosed: v })
		});
		await loadMe();
	}
	async function logout() {
		await api('/api/v1/auth/logout', { method: 'POST' });
		goto('/app/masuk');
	}
</script>

<div class="space-y-3 px-4 pt-4">
	<div class="flex items-center">
		<h1 class="flex-1 text-2xl">{c.navAccount}</h1>
		<button class="text-sm font-extrabold text-od" onclick={() => lang.update((v) => (v === 'id' ? 'en' : 'id'))}>{c.lang}</button>
	</div>
	{#if $me}
		<p class="text-sm font-bold text-ink2">{$me.user.name} · {$me.user.email}</p>
		{#if $me.user.isPlatformAdmin}<a class="btn sm" href="/app/admin">{c.admin}</a>{/if}
	{/if}
	{#if brand}
		<label class="flex items-center gap-2 text-sm font-extrabold">
			<input type="checkbox" checked={brand.stores[0].acceptPreordersWhenClosed} onchange={(e) => preorder((e.target as HTMLInputElement).checked)} />
			{c.preorderOk}
		</label>
		<a class="text-sm font-extrabold text-od" href="/app/pelanggan">{c.customers}</a>

		<div class="card p-3">
			<div class="fd">{c.products}</div>
			{#each products as p}
				<div class="mt-2 flex items-center gap-2 text-sm font-bold">
					<img src={p.photoUrl || '/brand/placeholder-1x1.png'} alt="" class="h-10 w-10 rounded-lg object-cover" />
					<div class="flex-1">{p.name}<div class="text-[11px] text-ink2">Rp {p.price.toLocaleString('id-ID')}</div></div>
					<label class="text-[11px] text-od">foto<input class="hidden" type="file" accept="image/*" onchange={(e) => { const f = (e.target as HTMLInputElement).files?.[0]; if (f) photo(p.id, f); }} /></label>
				</div>
				{#if nuts[p.id]}
					<details class="mt-1 rounded-xl bg-o3 p-2">
						<summary class="text-[11px] font-extrabold text-od">{c.nutrition}</summary>
						<div class="mt-2 grid grid-cols-2 gap-1">
							<input class="h-9 rounded-lg border border-line px-2 text-xs" placeholder={c.kcal} bind:value={nuts[p.id].kcal} />
							<input class="h-9 rounded-lg border border-line px-2 text-xs" placeholder={c.protein} bind:value={nuts[p.id].proteinG} />
							<input class="h-9 rounded-lg border border-line px-2 text-xs" placeholder={c.carb} bind:value={nuts[p.id].carbG} />
							<input class="h-9 rounded-lg border border-line px-2 text-xs" placeholder={c.fat} bind:value={nuts[p.id].fatG} />
						</div>
						<input class="mt-1 h-9 w-full rounded-lg border border-line px-2 text-xs" placeholder={c.allergens} bind:value={nuts[p.id].allergens} />
						<input class="mt-1 h-9 w-full rounded-lg border border-line px-2 text-xs" placeholder={c.tagsHint} bind:value={nuts[p.id].tags} />
						<button class="btn sm ghost mt-1" onclick={() => saveNut(p.id)}>{c.save}</button>
					</details>
				{/if}
			{/each}
			<div class="mt-2 grid gap-2">
				<input class="h-10 rounded-xl border border-line px-2" placeholder={c.name} bind:value={pname} />
				<input class="h-10 rounded-xl border border-line px-2" type="number" bind:value={pprice} />
				<input class="h-10 rounded-xl border border-line px-2" placeholder={c.note} bind:value={pdesc} />
				<button class="btn sm" onclick={addProduct}>+ {c.products}</button>
			</div>
		</div>

		<div class="card p-3">
			<div class="fd">{c.charges}</div>
			{#each charges as ch}
				<div class="mt-2 flex items-center text-sm font-bold">
					<span class="flex-1">{ch.Name} · Rp {ch.AmountRp.toLocaleString('id-ID')}</span>
					<button class="text-xs text-od" onclick={() => toggleCharge(ch)}>{ch.Active ? c.storeOpen : c.storeClosed}</button>
				</div>
			{/each}
			<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" bind:value={cname} />
			<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={camount} />
			<select class="mt-2 h-10 w-full rounded-xl border border-line px-2" bind:value={cmethod}>
				<option value="both">{c.pickup} + {c.delivery}</option>
				<option value="pickup">{c.pickup}</option>
				<option value="delivery">{c.delivery}</option>
			</select>
			<button class="btn sm mt-2" onclick={addCharge}>+ {c.charges}</button>
		</div>

		<div class="card p-3">
			<div class="fd">{c.bank}</div>
			{#each banks as b}
				<div class="mt-1 text-sm font-bold">{b.BankCode} {b.AccountNumber} · {b.HolderName}</div>
			{/each}
			<select class="mt-2 h-10 w-full rounded-xl border border-line px-2" bind:value={bcode}>
				{#each bankCodes as b}<option value={b.code}>{b.name}</option>{/each}
			</select>
			<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" placeholder="5230…" bind:value={bnum} />
			<input class="mt-2 h-10 w-full rounded-xl border border-line px-2" placeholder="a.n." bind:value={bholder} />
			<button class="btn sm mt-2" onclick={addBank}>+ {c.bank}</button>
		</div>

		<div class="card p-3">
			<div class="fd">{c.templates}</div>
			{#each templates as t}
				<div class="mt-2 text-xs font-extrabold text-ink2">{t.Kind}</div>
				<textarea class="w-full rounded-xl border border-line p-2 text-sm" rows="4" bind:value={t.Body}></textarea>
				<button class="btn sm ghost mt-1" onclick={() => saveTpl(t)}>{c.save}</button>
			{/each}
		</div>
	{/if}
	<button class="btn ghost" onclick={logout}>{c.logout}</button>
</div>
