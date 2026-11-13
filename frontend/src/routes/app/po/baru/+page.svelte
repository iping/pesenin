<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { loadMe, brandOf } from '$lib/session';
	import { api, ApiError } from '$lib/api';
	import { presetWall, snapWall, wallToISO } from '$lib/format';
	import DateTimeField from '$lib/components/DateTimeField.svelte';

	let c = $derived(copy($lang));
	let brandId = $state('');
	let products = $state<any[]>([]);
	let title = $state('PO baru');
	let quota = $state(50);
	let pickup = $state(true);
	let delivery = $state(true);
	let notes = $state('');
	let tz = $state('Asia/Jakarta');
	let opens = $state('');
	let closes = $state('');
	let ready = $state('');
	let payBy = $state('');
	let slots = $state([{ start: '', end: '' }]);
	let picked = $state<Record<string, boolean>>({});
	let err = $state('');

	onMount(async () => {
		const me = await loadMe();
		const b = brandOf(me);
		brandId = b.id;
		tz = b.stores?.[0]?.timezone || 'Asia/Jakarta';
		products = await api('/api/v1/brands/' + brandId + '/products');
		for (const p of products) if (p.active) picked[p.id] = true;
		opens = presetWall(0, 8, 0, tz);
		closes = presetWall(1, 20, 0, tz);
		ready = presetWall(2, 9, 0, tz);
		slots = [{ start: presetWall(2, 9, 0, tz), end: presetWall(2, 12, 0, tz) }];
	});

	async function save(publish: boolean) {
		err = '';
		try {
			const pickupSlots = slots.map((s) => ({ start: wallToISO(snapWall(s.start), tz), end: wallToISO(snapWall(s.end), tz) }));
			for (const s of slots) {
				if (!s.start || !s.end || snapWall(s.end) <= snapWall(s.start)) {
					err = 'Jam selesai slot harus setelah jam mulai.';
					return;
				}
			}
			const items = products.filter((p) => picked[p.id]).map((p) => ({ productId: p.id, price: p.price, quota: null }));
			const batch = await api<any>('/api/v1/brands/' + brandId + '/batches', {
				method: 'POST',
				body: JSON.stringify({
					title,
					opensAt: wallToISO(snapWall(opens), tz),
					closesAt: wallToISO(snapWall(closes), tz),
					readyAt: wallToISO(snapWall(ready), tz),
					pickupSlots,
					paymentDeadlineAt: payBy ? wallToISO(snapWall(payBy), tz) : null,
					quotaTotal: quota, allowPickup: pickup, allowDelivery: delivery, notes, publish, items
				})
			});
			localStorage.setItem('pn_push_asked', localStorage.getItem('pn_push_asked') || '');
			goto('/app/po/' + batch.id);
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal';
		}
	}
</script>

<div class="space-y-3 px-4 pt-4">
	<h1 class="text-2xl">{c.makePo}</h1>
	{#if err}<p class="rounded-xl bg-red-50 px-3 py-2 text-sm font-extrabold text-danger">{err}</p>{/if}
	<div class="card space-y-2 p-3.5">
		<div class="fd">{c.period}</div>
		<input class="h-11 w-full rounded-xl border border-line px-3" bind:value={title} />
		<DateTimeField label="Buka" bind:value={opens} {tz} />
		<DateTimeField label="Tutup" bind:value={closes} {tz} />
		<DateTimeField label="Siap" bind:value={ready} {tz} />
		{#each slots as s, i}
			<div class="rounded-xl bg-cream p-2">
				<DateTimeField label="Slot ambil mulai" bind:value={slots[i].start} {tz} />
				<DateTimeField label="Slot ambil selesai" bind:value={slots[i].end} {tz} />
			</div>
		{/each}
		<button type="button" class="text-xs font-extrabold text-od" onclick={() => (slots = [...slots, { start: ready, end: ready }])}>+ Slot</button>
		<DateTimeField label="Batas bayar" bind:value={payBy} {tz} optional />
		<label class="text-xs font-extrabold text-ink2">{c.quota}<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={quota} /></label>
		<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={pickup} /> {c.pickup}</label>
		<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={delivery} /> {c.delivery}</label>
		<textarea class="w-full rounded-xl border border-line p-2" rows="2" bind:value={notes} placeholder={c.note}></textarea>
	</div>
	<div class="card p-3.5">
		<div class="fd">{c.products}</div>
		{#each products as p}
			<label class="mt-2 flex items-center gap-2 text-sm font-extrabold">
				<input type="checkbox" bind:checked={picked[p.id]} />
				{#if p.photoUrl}<img src={p.photoUrl} alt="" class="h-8 w-8 rounded-lg object-cover" />{:else}<img src="/brand/placeholder-1x1.png" alt="" class="h-8 w-8 rounded-lg object-cover" />{/if}
				<span class="flex-1">{p.name}</span>
				<span>Rp {p.price.toLocaleString('id-ID')}</span>
			</label>
		{/each}
	</div>
	<div class="flex gap-2 pb-4">
		<button class="btn ghost flex-1" onclick={() => save(false)}>{c.save}</button>
		<button class="btn flex-1" onclick={() => save(true)}>{c.publish}</button>
	</div>
</div>
