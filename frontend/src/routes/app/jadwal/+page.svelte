<script lang="ts">
	import { onMount } from 'svelte';
	import { me, loadMe, brandOf } from '$lib/session';
	import { api, ApiError } from '$lib/api';
	import { wallToISO } from '$lib/format';
	import { liveHours } from '$lib/hours';

	type Slot = { start: string; end: string };
	type Day = { weekday: number; closed: boolean; slots: Slot[] };

	const labels = ['Minggu', 'Senin', 'Selasa', 'Rabu', 'Kamis', 'Jumat', 'Sabtu'];
	const order = [1, 2, 3, 4, 5, 6, 0];
	const zones = [
		{ id: 'Asia/Jakarta', label: 'WIB' },
		{ id: 'Asia/Makassar', label: 'WITA' },
		{ id: 'Asia/Jayapura', label: 'WIT' }
	];
	const reasons = ['Habis', 'Hujan', 'Libur'];

	let brand = $derived(brandOf($me));
	let storeId = $state('');
	let view = $state<any>(null);
	let err = $state('');
	let note = $state('');
	let busy = $state(false);
	let inherit = $state(true);
	let tz = $state('Asia/Jakarta');
	let accept = $state(true);
	let storeDays = $state<Day[]>([]);
	let brandDays = $state<Day[]>([]);
	let copyFrom = $state<number | null>(null);
	let copyWhich = $state<'store' | 'brand'>('store');
	let copyTargets = $state<number[]>([]);
	let copyStores = $state<string[]>([]);
	let spFrom = $state('');
	let spTo = $state('');
	let spNote = $state('');
	let spClosed = $state(true);
	let spScope = $state<'store' | 'brand'>('store');
	let spSlots = $state<Slot[]>([{ start: '10:00', end: '14:00' }]);
	let pauseAt = $state('');
	let reason = $state('Habis');
	let tick = $state(0);

	let hv = $derived.by(() => {
		tick;
		return liveHours(view?.status, 'id', Date.now(), tz);
	});

	function blankWeek(): Day[] {
		return order.map((weekday) => ({ weekday, closed: true, slots: [] }));
	}

	function asDays(rows: any[] | null | undefined): Day[] {
		const by = new Map((rows || []).map((d) => [d.weekday, d]));
		return order.map((weekday) => {
			const d = by.get(weekday);
			if (!d) return { weekday, closed: true, slots: [] };
			return {
				weekday,
				closed: !!d.closed,
				slots: (d.slots || []).map((s: Slot) => ({ start: String(s.start).slice(0, 5), end: String(s.end).slice(0, 5) }))
			};
		});
	}

	function example(days: Day[]) {
		for (const d of days) {
			if (d.weekday === 0) {
				d.closed = true;
				d.slots = [];
			} else {
				d.closed = false;
				d.slots = [
					{ start: '07:00', end: '11:00' },
					{ start: '16:00', end: '21:00' }
				];
			}
		}
		storeDays = [...storeDays];
		brandDays = [...brandDays];
	}

	function applyView(v: any) {
		view = v;
		inherit = !!v.inherit;
		tz = v.timezone || 'Asia/Jakarta';
		accept = !!v.acceptPreordersWhenClosed;
		storeDays = asDays(v.storeDays?.length ? v.storeDays : v.days);
		brandDays = asDays(v.brandDays);
		if (!v.storeDays?.length && !v.days?.length) storeDays = blankWeek();
		if (!v.brandDays?.length) brandDays = blankWeek();
	}

	async function load(id = storeId) {
		if (!id) return;
		view = await api('/api/v1/stores/' + id + '/hours');
		applyView(view);
	}

	$effect(() => {
		const clock = setInterval(() => (tick += 1), 30000);
		return () => clearInterval(clock);
	});

	onMount(async () => {
		const data = await loadMe();
		const b = brandOf(data);
		storeId = b?.stores?.[0]?.id || '';
		if (storeId) await load();
	});

	function addSlot(days: Day[], weekday: number) {
		const d = days.find((x) => x.weekday === weekday);
		if (!d) return;
		d.closed = false;
		d.slots = [...d.slots, { start: '08:00', end: '17:00' }];
		storeDays = [...storeDays];
		brandDays = [...brandDays];
	}

	function removeSlot(days: Day[], weekday: number, index: number) {
		const d = days.find((x) => x.weekday === weekday);
		if (!d) return;
		d.slots = d.slots.filter((_, i) => i !== index);
		if (d.slots.length === 0) d.closed = true;
		storeDays = [...storeDays];
		brandDays = [...brandDays];
	}

	function payload(days: Day[]) {
		return days.map((d) => ({
			weekday: d.weekday,
			closed: d.closed || d.slots.length === 0,
			slots: d.closed ? [] : d.slots.map((s) => ({ start: s.start.slice(0, 5), end: s.end.slice(0, 5) }))
		}));
	}

	async function saveStore() {
		err = '';
		note = '';
		busy = true;
		try {
			const next = await api<any>('/api/v1/stores/' + storeId + '/hours', {
				method: 'PUT',
				body: JSON.stringify({
					inherit,
					timezone: tz,
					acceptPreordersWhenClosed: accept,
					days: inherit ? [] : payload(storeDays)
				})
			});
			applyView(next);
			note = 'Jadwal toko tersimpan.';
			await loadMe();
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal simpan.';
		} finally {
			busy = false;
		}
	}

	async function saveBrand() {
		if (!brand) return;
		err = '';
		note = '';
		busy = true;
		try {
			await api('/api/v1/brands/' + brand.id + '/hours', {
				method: 'PUT',
				body: JSON.stringify({ days: payload(brandDays) })
			});
			await load();
			note = 'Jadwal bawaan brand tersimpan.';
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal simpan.';
		} finally {
			busy = false;
		}
	}

	function applyCopy() {
		const srcList = copyWhich === 'brand' ? brandDays : storeDays;
		const src = srcList.find((d) => d.weekday === copyFrom);
		if (!src) return;
		for (const wd of copyTargets) {
			const d = srcList.find((x) => x.weekday === wd);
			if (!d) continue;
			d.closed = src.closed;
			d.slots = src.slots.map((s) => ({ ...s }));
		}
		storeDays = [...storeDays];
		brandDays = [...brandDays];
		copyFrom = null;
		copyTargets = [];
	}

	async function copyToStores() {
		err = '';
		busy = true;
		try {
			await api('/api/v1/stores/' + storeId + '/hours/copy', {
				method: 'POST',
				body: JSON.stringify({ storeIds: copyStores })
			});
			note = 'Jadwal tersalin ke toko lain.';
			copyStores = [];
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal salin.';
		} finally {
			busy = false;
		}
	}

	async function addSpecial() {
		err = '';
		busy = true;
		try {
			const body = {
				dateFrom: spFrom,
				dateTo: spTo || spFrom,
				closed: spClosed,
				note: spNote,
				slots: spClosed ? [] : spSlots.map((s) => ({ start: s.start.slice(0, 5), end: s.end.slice(0, 5) }))
			};
			if (!brand) return;
			const path = spScope === 'brand' ? '/api/v1/brands/' + brand.id + '/specials' : '/api/v1/stores/' + storeId + '/specials';
			const next = await api<any>(path, { method: 'POST', body: JSON.stringify(body) });
			if (spScope === 'store') applyView(next);
			else await load();
			spNote = '';
			note = 'Hari khusus ditambahkan.';
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal tambah hari khusus.';
		} finally {
			busy = false;
		}
	}

	async function removeSpecial(id: string) {
		await api('/api/v1/specials/' + id, { method: 'DELETE' });
		await load();
	}

	async function override(kind: 'open' | 'closed' | 'pause') {
		err = '';
		busy = true;
		try {
			let until = '';
			if (kind === 'pause') {
				if (!pauseAt) {
					err = 'Isi waktu jeda selesai.';
					busy = false;
					return;
				}
				until = wallToISO(pauseAt.length === 16 ? pauseAt : pauseAt.slice(0, 16), tz);
			}
			const next = await api<any>('/api/v1/stores/' + storeId + '/override', {
				method: 'POST',
				body: JSON.stringify({ kind, reason: kind === 'open' ? '' : reason, until })
			});
			applyView(next);
			await loadMe();
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal ubah status.';
		} finally {
			busy = false;
		}
	}

	async function follow() {
		const next = await api<any>('/api/v1/stores/' + storeId + '/override', { method: 'DELETE' });
		applyView(next);
		await loadMe();
	}
</script>

<div class="space-y-3 px-4 pb-8 pt-4">
	<h1 class="text-2xl">Jam buka</h1>
	{#if brand && brand.stores?.length > 1}
		<select class="h-11 w-full rounded-xl border border-line bg-white px-3" bind:value={storeId} onchange={() => load(storeId)}>
			{#each brand.stores as st}
				<option value={st.id}>{st.name}</option>
			{/each}
		</select>
	{/if}

	{#if view}
		<div class="card p-3.5">
			<div class="flex items-center gap-2">
				<span class="rounded-full px-2.5 py-1 text-xs font-extrabold text-white {hv?.state === 'closing_soon' ? 'bg-o' : hv?.open ? 'bg-[#2EAD5B]' : 'bg-[#9A9A9A]'}">{hv?.badge || view.status?.badge}</span>
				{#if hv?.hint}<span class="text-[11px] font-extrabold text-od">{hv.hint}</span>{/if}
			</div>
			<p class="mt-1 text-[11px] font-bold text-ink2">{view.zone} · {view.status?.source === 'override' ? 'Lewat tombol manual' : view.status?.source === 'special' ? 'Hari khusus' : view.status?.configured ? 'Dari jadwal' : 'Dari tombol buka/tutup'}</p>
			<div class="mt-3 grid grid-cols-2 gap-2">
				<button class="btn sm" disabled={busy} onclick={() => override('open')}>Buka sekarang</button>
				<button class="btn sm ghost" disabled={busy} onclick={() => override('closed')}>Tutup sekarang</button>
			</div>
			<div class="mt-2 flex flex-wrap gap-1">
				{#each reasons as r}
					<button type="button" class="rounded-full px-2.5 py-1 text-[11px] font-extrabold {reason === r ? 'bg-o text-white' : 'bg-o3 text-od'}" onclick={() => (reason = r)}>{r}</button>
				{/each}
			</div>
			<label class="mt-2 block text-xs font-extrabold text-ink2">Jeda sampai
				<input class="mt-1 h-11 w-full rounded-xl border border-line px-3" type="datetime-local" bind:value={pauseAt} />
			</label>
			<button class="btn sm mt-2 w-full" disabled={busy} onclick={() => override('pause')}>Jeda sampai waktu itu</button>
			{#if view.override}
				<button class="btn sm ghost mt-2 w-full" disabled={busy} onclick={follow}>Ikuti jadwal</button>
			{/if}
		</div>

		<div class="card p-3.5">
			<div class="fd">Zona waktu</div>
			<div class="mt-2 grid grid-cols-3 gap-2">
				{#each zones as z}
					<button type="button" class="rounded-xl border-2 py-2 text-sm font-extrabold {tz === z.id ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => (tz = z.id)}>{z.label}</button>
				{/each}
			</div>
		</div>

		<label class="card flex items-center gap-2 p-3.5 text-sm font-extrabold">
			<input type="checkbox" bind:checked={inherit} />
			Toko ini ikut jadwal bawaan brand
		</label>

		{#if !inherit}
			<div class="card p-3.5">
				<div class="flex items-center gap-2">
					<div class="fd flex-1">Jadwal toko ini</div>
					<button class="text-xs font-extrabold text-od" type="button" onclick={() => example(storeDays)}>Contoh 07–11 & 16–21</button>
				</div>
				{#each storeDays as day}
					<div class="mt-3 border-t border-dashed border-line pt-3">
						<div class="flex items-center gap-2">
							<b class="flex-1">{labels[day.weekday]}</b>
							<label class="text-xs font-extrabold text-ink2"><input type="checkbox" bind:checked={day.closed} /> Tutup</label>
						</div>
						{#if !day.closed}
							{#each day.slots as slot, i}
								<div class="mt-2 flex items-center gap-2">
									<input class="h-10 flex-1 rounded-xl border border-line px-2" type="time" bind:value={slot.start} />
									<span class="text-ink3">–</span>
									<input class="h-10 flex-1 rounded-xl border border-line px-2" type="time" bind:value={slot.end} />
									<button class="grid h-10 w-10 place-items-center rounded-xl bg-[#F8E8E4] text-danger" type="button" aria-label="hapus jam" onclick={() => removeSlot(storeDays, day.weekday, i)}>×</button>
								</div>
							{/each}
							<div class="mt-2 flex gap-3 text-xs font-extrabold text-od">
								<button type="button" onclick={() => addSlot(storeDays, day.weekday)}>+ Jam</button>
								<button type="button" onclick={() => { copyWhich = 'store'; copyFrom = day.weekday; copyTargets = []; }}>Salin ke hari lain</button>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}

		<div class="card p-3.5">
			<div class="flex items-center gap-2">
				<div class="fd flex-1">Jadwal bawaan brand</div>
				<button class="text-xs font-extrabold text-od" type="button" onclick={() => example(brandDays)}>Contoh</button>
			</div>
			<p class="mt-1 text-[11px] font-bold text-ink2">Toko yang ikut brand memakai jam ini, di zona waktu masing-masing.</p>
			{#each brandDays as day}
				<div class="mt-3 border-t border-dashed border-line pt-3">
					<div class="flex items-center gap-2">
						<b class="flex-1">{labels[day.weekday]}</b>
						<label class="text-xs font-extrabold text-ink2"><input type="checkbox" bind:checked={day.closed} /> Tutup</label>
					</div>
					{#if !day.closed}
						{#each day.slots as slot, i}
							<div class="mt-2 flex items-center gap-2">
								<input class="h-10 flex-1 rounded-xl border border-line px-2" type="time" bind:value={slot.start} />
								<span class="text-ink3">–</span>
								<input class="h-10 flex-1 rounded-xl border border-line px-2" type="time" bind:value={slot.end} />
								<button class="grid h-10 w-10 place-items-center rounded-xl bg-[#F8E8E4] text-danger" type="button" aria-label="hapus jam" onclick={() => removeSlot(brandDays, day.weekday, i)}>×</button>
							</div>
						{/each}
						<div class="mt-2 flex gap-3 text-xs font-extrabold text-od">
							<button type="button" onclick={() => addSlot(brandDays, day.weekday)}>+ Jam</button>
							<button type="button" onclick={() => { copyWhich = 'brand'; copyFrom = day.weekday; copyTargets = []; }}>Salin ke hari lain</button>
						</div>
					{/if}
				</div>
			{/each}
			<button class="btn sm mt-3 w-full" disabled={busy} onclick={saveBrand}>Simpan jadwal brand</button>
		</div>

		{#if copyFrom !== null}
			<div class="card p-3.5">
				<div class="fd">Salin {labels[copyFrom]} ke</div>
				<div class="mt-2 flex flex-wrap gap-2">
					{#each order as wd}
						{#if wd !== copyFrom}
							<label class="rounded-full bg-o3 px-2.5 py-1 text-xs font-extrabold text-od">
								<input type="checkbox" checked={copyTargets.includes(wd)} onchange={(e) => {
									const on = (e.target as HTMLInputElement).checked;
									copyTargets = on ? [...copyTargets, wd] : copyTargets.filter((x) => x !== wd);
								}} />
								{labels[wd]}
							</label>
						{/if}
					{/each}
				</div>
				<button class="btn sm mt-3" type="button" onclick={applyCopy}>Terapkan</button>
			</div>
		{/if}

		{#if view.stores?.length}
			<div class="card p-3.5">
				<div class="fd">Salin jadwal ini ke toko lain</div>
				{#each view.stores as st}
					<label class="mt-2 flex items-center gap-2 text-sm font-extrabold">
						<input type="checkbox" checked={copyStores.includes(st.id)} onchange={(e) => {
							const on = (e.target as HTMLInputElement).checked;
							copyStores = on ? [...copyStores, st.id] : copyStores.filter((x) => x !== st.id);
						}} />
						{st.name}
					</label>
				{/each}
				<button class="btn sm mt-3" disabled={busy || copyStores.length === 0} onclick={copyToStores}>Salin</button>
			</div>
		{/if}

		<div class="card p-3.5">
			<div class="fd">Hari khusus</div>
			<p class="mt-1 text-[11px] font-bold text-ink2">Lebaran, libur, atau jam berbeda. Menang atas jadwal mingguan.</p>
			{#each view.specials || [] as sp}
				<div class="mt-2 flex items-start gap-2 rounded-xl bg-cream px-3 py-2 text-sm">
					<div class="flex-1">
						<b>{sp.dateFrom}{sp.dateTo !== sp.dateFrom ? ' – ' + sp.dateTo : ''}</b>
						<div class="text-[11px] font-bold text-ink2">{sp.scope === 'brand' ? 'Semua toko' : 'Toko ini'} · {sp.closed ? 'Tutup' : (sp.slots || []).map((s: Slot) => s.start + '–' + s.end).join(', ')}{sp.note ? ' · ' + sp.note : ''}</div>
					</div>
					<button class="text-xs font-extrabold text-danger" type="button" onclick={() => removeSpecial(sp.id)}>Hapus</button>
				</div>
			{/each}
			<div class="mt-3 grid grid-cols-2 gap-2">
				<label class="text-xs font-extrabold text-ink2">Dari<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="date" bind:value={spFrom} /></label>
				<label class="text-xs font-extrabold text-ink2">Sampai<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="date" bind:value={spTo} /></label>
			</div>
			<input class="mt-2 h-10 w-full rounded-xl border border-line px-3 text-sm" placeholder="Catatan, mis. Lebaran" bind:value={spNote} />
			<label class="mt-2 flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={spClosed} /> Tutup seharian</label>
			{#if !spClosed}
				{#each spSlots as slot, i}
					<div class="mt-2 flex items-center gap-2">
						<input class="h-10 flex-1 rounded-xl border border-line px-2" type="time" bind:value={slot.start} />
						<input class="h-10 flex-1 rounded-xl border border-line px-2" type="time" bind:value={slot.end} />
						<button class="text-danger" type="button" aria-label="hapus" onclick={() => (spSlots = spSlots.filter((_, n) => n !== i))}>×</button>
					</div>
				{/each}
				<button class="mt-2 text-xs font-extrabold text-od" type="button" onclick={() => (spSlots = [...spSlots, { start: '10:00', end: '14:00' }])}>+ Jam</button>
			{/if}
			<div class="mt-2 grid grid-cols-2 gap-2 text-xs font-extrabold">
				<button type="button" class="rounded-xl border-2 py-2 {spScope === 'store' ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => (spScope = 'store')}>Toko ini</button>
				<button type="button" class="rounded-xl border-2 py-2 {spScope === 'brand' ? 'border-o bg-o3 text-od' : 'border-line'}" onclick={() => (spScope = 'brand')}>Semua toko</button>
			</div>
			<button class="btn sm mt-3 w-full" disabled={busy || !spFrom} onclick={addSpecial}>Tambah hari khusus</button>
		</div>

		<label class="flex items-center gap-2 text-sm font-extrabold">
			<input type="checkbox" bind:checked={accept} />
			Tetap terima PO untuk tanggal ambil lain saat tutup
		</label>

		{#if err}<p class="rounded-xl bg-red-50 px-3 py-2 text-sm font-extrabold text-danger">{err}</p>{/if}
		{#if note}<p class="rounded-xl bg-mint2 px-3 py-2 text-sm font-extrabold text-mintd">{note}</p>{/if}
		<button class="btn" disabled={busy} onclick={saveStore}>Simpan jadwal toko</button>
	{:else}
		<p class="font-bold">{err || 'Pingping lagi siap-siap…'}</p>
	{/if}
</div>
