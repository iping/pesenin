<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { loadMe, brandOf } from '$lib/session';
	import { api, ApiError } from '$lib/api';

	let c = $derived(copy($lang));
	let storeId = $state('');
	let brandId = $state('');
	let week = $state<any>(null);
	let products = $state<any[]>([]);
	let selected = $state('');
	let closed = $state(false);
	let notes = $state('');
	let draft = $state<any[]>([]);
	let enabled = $state(false);
	let cutoffDays = $state(1);
	let cutoffTime = $state('18:00');
	let allowPickup = $state(true);
	let allowDelivery = $state(true);
	let dayCap = $state('');
	let templateId = $state('');
	let tplName = $state('Mingguan');
	let tplWeekday = $state(1);
	let tplItems = $state<any[]>([]);
	let err = $state('');
	let msg = $state('');
	const days = ['Min', 'Sen', 'Sel', 'Rab', 'Kam', 'Jum', 'Sab'];
	const slots = [
		{ id: '', label: 'Sepanjang hari' },
		{ id: 'breakfast', label: 'Sarapan' },
		{ id: 'lunch', label: 'Makan siang' },
		{ id: 'dinner', label: 'Makan malam' }
	];

	onMount(async () => {
		const me = await loadMe();
		const brand = brandOf(me);
		if (!brand) return;
		brandId = brand.id;
		storeId = brand.stores[0].id;
		products = await api('/api/v1/brands/' + brandId + '/products');
		const tpls = await api<any[]>('/api/v1/brands/' + brandId + '/menu/templates');
		if (tpls[0]) {
			templateId = tpls[0].id;
			tplName = tpls[0].name;
			tplItems = tpls[0].items || [];
		}
		await load('');
	});

	function applyWeek(data: any) {
		week = data;
		enabled = !!data.enabled;
		cutoffDays = data.cutoffDaysBefore ?? 1;
		cutoffTime = data.cutoffTime || '18:00';
		allowPickup = data.allowPickup !== false;
		allowDelivery = !!data.allowDelivery;
		dayCap = data.dayCapacity ?? '';
		if (data.templateId) templateId = data.templateId;
		const still = data.days?.find((d: any) => d.date === selected);
		choose(still || data.days?.find((d: any) => d.orderable) || data.days?.[0]);
	}

	async function load(start: string) {
		err = '';
		const data = await api<any>('/api/v1/stores/' + storeId + '/menu' + (start ? '?start=' + start : ''));
		applyWeek(data);
	}

	function choose(day: any) {
		if (!day) return;
		selected = day.date;
		closed = !!day.closed;
		notes = day.notes || '';
		draft = (day.items || []).map((it: any) => ({
			productId: it.productId,
			variantId: it.variantId || '',
			mealSlot: it.mealSlot || '',
			quota: it.quota ?? ''
		}));
	}

	function variantsOf(id: string) {
		const p = products.find((x) => x.id === id);
		return (p?.variants || []).filter((v: any) => v.Active !== false);
	}

	function addDraft() {
		draft = [...draft, { productId: products[0]?.id || '', variantId: '', mealSlot: 'lunch', quota: '' }];
	}

	async function saveSettings() {
		err = '';
		msg = '';
		try {
			await api('/api/v1/stores/' + storeId + '/menu/settings', {
				method: 'PUT',
				body: JSON.stringify({
					enabled,
					templateId: templateId || null,
					cutoffDaysBefore: Number(cutoffDays),
					cutoffTime,
					allowPickup,
					allowDelivery,
					dayCapacity: dayCap === '' ? null : Number(dayCap)
				})
			});
			await load(week?.start || '');
			msg = 'Pengaturan tersimpan.';
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal simpan.';
		}
	}

	function payloadItems() {
		return draft
			.filter((it) => it.productId)
			.map((it, i) => ({
				productId: it.productId,
				variantId: it.variantId || null,
				mealSlot: it.mealSlot || '',
				sortOrder: i,
				quota: it.quota === '' || it.quota == null ? null : Number(it.quota)
			}));
	}

	async function saveDay() {
		err = '';
		msg = '';
		try {
			await api('/api/v1/stores/' + storeId + '/menu/days/' + selected, {
				method: 'PUT',
				body: JSON.stringify({ closed, notes, items: closed ? [] : payloadItems() })
			});
			await load(week?.start || '');
			msg = 'Hari tersimpan.';
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal simpan.';
		}
	}

	async function resetDay() {
		err = '';
		try {
			await api('/api/v1/stores/' + storeId + '/menu/days/' + selected, { method: 'DELETE' });
			await load(week?.start || '');
			msg = 'Hari ini ikut template.';
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Tidak bisa.';
		}
	}

	async function copyWeek() {
		err = '';
		msg = '';
		try {
			const res = await api<any>('/api/v1/stores/' + storeId + '/menu/copy-week', {
				method: 'POST',
				body: JSON.stringify({ from: week.start })
			});
			msg = 'Tersalin ' + res.copied.length + ' hari' + (res.skipped?.length ? ', ' + res.skipped.length + ' dilewati karena sudah ada pesanan.' : '.');
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal salin.';
		}
	}

	function shift(delta: number) {
		if (!week?.start) return;
		const d = new Date(week.start + 'T00:00:00Z');
		d.setUTCDate(d.getUTCDate() + delta * 7);
		load(d.toISOString().slice(0, 10));
	}

	let day = $derived(week?.days?.find((d: any) => d.date === selected));
	let tplVisible = $derived(tplItems.filter((it) => it.weekday === tplWeekday));

	function addTpl() {
		tplItems = [...tplItems, { weekday: tplWeekday, mealSlot: 'lunch', productId: products[0]?.id || '', quota: null, sortOrder: tplItems.length }];
	}

	async function saveTemplate() {
		err = '';
		msg = '';
		try {
			const body = {
				name: tplName || 'Mingguan',
				items: tplItems.map((it, i) => ({
					weekday: Number(it.weekday),
					mealSlot: it.mealSlot || '',
					productId: it.productId,
					variantId: it.variantId || null,
					sortOrder: i,
					quota: it.quota === '' || it.quota == null ? null : Number(it.quota)
				}))
			};
			const saved = templateId
				? await api<any>('/api/v1/brands/' + brandId + '/menu/templates/' + templateId, { method: 'PUT', body: JSON.stringify(body) })
				: await api<any>('/api/v1/brands/' + brandId + '/menu/templates', { method: 'POST', body: JSON.stringify(body) });
			templateId = saved.id;
			tplItems = saved.items || [];
			if (!enabled) {
				msg = 'Template tersimpan. Aktifkan menu supaya pelanggan melihatnya.';
			} else {
				await api('/api/v1/stores/' + storeId + '/menu/settings', {
					method: 'PUT',
					body: JSON.stringify({
						enabled,
						templateId,
						cutoffDaysBefore: Number(cutoffDays),
						cutoffTime,
						allowPickup,
						allowDelivery,
						dayCapacity: dayCap === '' ? null : Number(dayCap)
					})
				});
				await load(week?.start || '');
				msg = 'Template tersimpan.';
			}
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal simpan template.';
		}
	}
</script>

<div class="space-y-3 px-4 pb-8 pt-4">
	<div class="flex items-center">
		<h1 class="flex-1 text-2xl">{c.menu}</h1>
		{#if selected}<a class="text-sm font-extrabold text-od" href="/app/menu/cetak?date={selected}">{c.prepList}</a>{/if}
	</div>
	<p class="text-sm font-bold text-ink2">{c.menuLead}</p>

	<div class="card space-y-2 p-3">
		<label class="flex items-center gap-2 text-sm font-extrabold">
			<input type="checkbox" bind:checked={enabled} /> {c.menuOn}
		</label>
		<div class="grid grid-cols-2 gap-2">
			<label class="text-xs font-extrabold text-ink2">{c.cutoff}
				<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" min="0" max="14" bind:value={cutoffDays} />
			</label>
			<label class="text-xs font-extrabold text-ink2">Jam
				<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="time" bind:value={cutoffTime} />
			</label>
		</div>
		<p class="text-[11px] font-bold text-ink2">{c.cutoffHint}</p>
		<label class="text-xs font-extrabold text-ink2">{c.dayCap}
			<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" min="0" placeholder="tanpa batas" bind:value={dayCap} />
		</label>
		<p class="text-[11px] font-bold text-ink2">{c.dayCapHint}</p>
		<div class="flex gap-3 text-sm font-extrabold">
			<label class="flex items-center gap-1"><input type="checkbox" bind:checked={allowPickup} /> {c.pickup}</label>
			<label class="flex items-center gap-1"><input type="checkbox" bind:checked={allowDelivery} /> {c.delivery}</label>
		</div>
		<button class="btn sm" onclick={saveSettings}>{c.save}</button>
	</div>

	{#if week}
		<div class="flex items-center gap-2">
			<button class="grid h-9 w-9 place-items-center rounded-xl bg-white font-black text-od shadow-[0_2px_0_#F1E4D6]" onclick={() => shift(-1)} aria-label="minggu sebelumnya">‹</button>
			<div class="fd flex-1 text-center text-sm">{week.days?.[0]?.label} – {week.days?.[6]?.label}</div>
			<button class="grid h-9 w-9 place-items-center rounded-xl bg-white font-black text-od shadow-[0_2px_0_#F1E4D6]" onclick={() => shift(1)} aria-label="minggu berikutnya">›</button>
		</div>
		<div class="flex gap-1.5 overflow-x-auto">
			{#each week.days as d}
				<button class="min-w-[68px] rounded-2xl px-2 py-2 text-center {d.date === selected ? 'bg-o text-white shadow-[0_3px_0_#E06F0A]' : 'bg-white text-ink'}" onclick={() => choose(d)}>
					<div class="text-[10px] font-extrabold">{d.label.split(',')[0]}</div>
					<div class="fd">{d.label.split(' ')[1]}</div>
					<div class="text-[10px] font-bold opacity-80">{d.closed ? c.closedDay : d.items.length + ' menu'}</div>
				</button>
			{/each}
		</div>

		{#if day}
			<div class="card space-y-2 p-3">
				<div class="flex items-center">
					<b class="fd flex-1">{day.label}</b>
					<span class="text-[11px] font-extrabold text-ink2">{day.source === 'rotation' ? 'Template' : day.source === 'copy' ? 'Salinan' : 'Tanggal ini'}</span>
				</div>
				{#if day.reason && !day.orderable}<p class="text-[11px] font-bold text-od">{day.reason}</p>{/if}
				<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={closed} /> {c.closedDay}</label>
				{#if !closed}
					{#each draft as it, i}
						<div class="rounded-2xl bg-o3 p-2">
							<select class="h-10 w-full rounded-xl border border-line bg-white px-2" bind:value={it.productId}>
								{#each products as p}<option value={p.id}>{p.name}</option>{/each}
							</select>
							{#if variantsOf(it.productId).length}
								<select class="mt-1 h-10 w-full rounded-xl border border-line bg-white px-2" bind:value={it.variantId}>
									<option value="">Tanpa varian</option>
									{#each variantsOf(it.productId) as v}<option value={v.ID}>{v.Name}</option>{/each}
								</select>
							{/if}
							<div class="mt-1 grid grid-cols-2 gap-1">
								<select class="h-10 rounded-xl border border-line bg-white px-2" bind:value={it.mealSlot}>
									{#each slots as s}<option value={s.id}>{s.label}</option>{/each}
								</select>
								<input class="h-10 rounded-xl border border-line bg-white px-2" type="number" min="0" placeholder={c.quota} bind:value={it.quota} />
							</div>
							<button class="mt-1 text-xs font-extrabold text-danger" onclick={() => (draft = draft.filter((_, j) => j !== i))}>Hapus</button>
						</div>
					{/each}
					<button class="btn sm ghost" onclick={addDraft}>+ {c.addItem}</button>
				{/if}
				<input class="h-10 w-full rounded-xl border border-line px-2" placeholder={c.note} bind:value={notes} />
				<button class="btn" onclick={saveDay}>{c.saveDay}</button>
				<div class="flex gap-2">
					<button class="btn sm ghost flex-1" onclick={resetDay}>{c.useTemplate}</button>
					<button class="btn sm ghost flex-1" onclick={copyWeek}>{c.copyWeek}</button>
				</div>
			</div>
		{/if}
	{/if}

	<div class="card space-y-2 p-3">
		<div class="fd">{c.weekTemplate}</div>
		<p class="text-[11px] font-bold text-ink2">{c.weekTemplateHint}</p>
		<input class="h-10 w-full rounded-xl border border-line px-2" bind:value={tplName} />
		<div class="flex gap-1 overflow-x-auto">
			{#each days as name, i}
				<button class="rounded-full px-2.5 py-1 text-xs font-extrabold {tplWeekday === i ? 'bg-o text-white' : 'bg-o3'}" onclick={() => (tplWeekday = i)}>{name}</button>
			{/each}
		</div>
		{#each tplItems as it, i}
			{#if it.weekday === tplWeekday}
				<div class="grid grid-cols-[1fr_auto] gap-1">
					<select class="h-10 rounded-xl border border-line px-2" bind:value={it.productId}>
						{#each products as p}<option value={p.id}>{p.name}</option>{/each}
					</select>
					<button class="text-xs font-extrabold text-danger" onclick={() => (tplItems = tplItems.filter((_, j) => j !== i))}>×</button>
					<select class="col-span-2 h-10 rounded-xl border border-line px-2" bind:value={it.mealSlot}>
						{#each slots as s}<option value={s.id}>{s.label}</option>{/each}
					</select>
				</div>
			{/if}
		{/each}
		<p class="text-[11px] font-bold text-ink2">{tplVisible.length} item</p>
		<button class="btn sm ghost" onclick={addTpl}>+ {c.addItem}</button>
		<button class="btn sm" onclick={saveTemplate}>{c.save}</button>
	</div>

	{#if msg}<p class="text-sm font-extrabold text-mintd">{msg}</p>{/if}
	{#if err}<p class="text-sm font-extrabold text-danger">{err}</p>{/if}
</div>
