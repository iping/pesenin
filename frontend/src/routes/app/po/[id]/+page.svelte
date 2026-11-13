<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api, ApiError } from '$lib/api';
	import QRCode from 'qrcode';

	let c = $derived(copy($lang));
	let b = $state<any>(null);
	let link = $state('');
	let qr = $state('');
	let copied = $state(false);
	let err = $state('');
	let quota = $state('');
	let minQty = $state('');
	let minRp = $state('');
	let maxCust = $state('');
	let capOrders = $state('');
	let minProceed = $state('');
	let lead = $state('');
	let waitlist = $state(false);
	let itemCap = $state<Record<string, { quota: string; min: string; max: string }>>({});
	let slotCap = $state<Record<number, { qty: string; orders: string }>>({});

	function blank(v: unknown) {
		return v == null || v === '' ? '' : String(v);
	}
	function num(v: string) {
		return v === '' ? null : Number(v);
	}
	function fill(row: any) {
		const cap = row.capacity || {};
		quota = blank(row.quotaTotal);
		minQty = blank(cap.minQty);
		minRp = blank(cap.minAmountRp);
		maxCust = blank(cap.maxQtyPerCustomer);
		capOrders = blank(cap.capacityOrders);
		minProceed = blank(cap.minProceedQty);
		lead = blank(cap.cutoffLeadMinutes);
		waitlist = !!cap.waitlistEnabled;
		const next: Record<string, { quota: string; min: string; max: string }> = {};
		for (const it of row.items || []) next[it.id] = { quota: blank(it.quota), min: blank(it.minQty), max: blank(it.maxPerCustomer) };
		itemCap = next;
		const slots: Record<number, { qty: string; orders: string }> = {};
		(row.pickupSlots || []).forEach((s: any, i: number) => {
			slots[i] = { qty: blank(s.capacityQty), orders: blank(s.capacityOrders) };
		});
		slotCap = slots;
	}

	onMount(async () => {
		b = await api('/api/v1/batches/' + $page.params.id);
		fill(b);
		const me = await api<any>('/api/v1/me');
		const brand = me.brands.find((x: any) => x.id === b.brandId);
		link = location.origin + '/' + brand.slug + '/po/' + b.code;
		qr = await QRCode.toDataURL(link, { width: 1024, margin: 2, errorCorrectionLevel: 'M' });
	});

	async function flip(open: boolean) {
		b = await api('/api/v1/batches/' + b.id + (open ? '/open' : '/close'), { method: 'POST' });
	}
	async function dup() {
		const n = await api<any>('/api/v1/batches/' + b.id + '/duplicate', { method: 'POST' });
		location.href = '/app/po/' + n.id;
	}
	async function saveCap() {
		err = '';
		try {
			b = await api('/api/v1/batches/' + b.id + '/capacity', {
				method: 'PUT',
				body: JSON.stringify({
					capacityQty: num(quota),
					minQty: num(minQty),
					minAmountRp: num(minRp),
					maxQtyPerCustomer: num(maxCust),
					capacityOrders: num(capOrders),
					minProceedQty: num(minProceed),
					cutoffLeadMinutes: num(lead),
					waitlistEnabled: waitlist,
					items: (b.items || []).map((it: any) => ({
						batchItemId: it.id,
						quota: num(itemCap[it.id]?.quota ?? ''),
						minQty: num(itemCap[it.id]?.min ?? ''),
						maxPerCustomer: num(itemCap[it.id]?.max ?? '')
					})),
					slots: (b.pickupSlots || []).map((s: any, i: number) => ({
						start: s.start,
						capacityQty: num(slotCap[i]?.qty ?? ''),
						capacityOrders: num(slotCap[i]?.orders ?? '')
					}))
				})
			});
			fill(b);
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal simpan.';
		}
	}
	async function decide(decision: 'proceed' | 'cancel') {
		err = '';
		try {
			b = await api('/api/v1/batches/' + b.id + '/quota-decision', { method: 'POST', body: JSON.stringify({ decision }) });
			fill(b);
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal.';
		}
	}
	function download(size: number) {
		const a = document.createElement('a');
		a.href = '/api/v1/public/qr.png?size=' + size + '&url=' + encodeURIComponent(link);
		a.download = b.code + '-' + size + '.png';
		a.click();
	}
</script>

{#if b}
	<div class="space-y-3 px-4 pt-4">
		<a href="/app/po" class="text-sm font-extrabold text-od">← PO</a>
		<h1 class="text-2xl">{b.title}</h1>
		<p class="text-xs font-extrabold text-mintd">{b.status} · {b.code}</p>
		{#if b.capacity?.quotaState === 'below_min'}
			<div class="card border-2 border-o2 p-3">
				<p class="text-sm font-extrabold">Kuota minimum belum tercapai ({b.capacity.heldQty}/{b.capacity.minProceedQty}). Lanjutkan atau batalkan? Batalkan menandai pesanan yang sudah transfer untuk refund.</p>
				<div class="mt-2 flex gap-2">
					<button class="btn sm flex-1" onclick={() => decide('proceed')}>Lanjutkan</button>
					<button class="btn sm ghost flex-1" onclick={() => decide('cancel')}>Batalkan & refund</button>
				</div>
			</div>
		{/if}
		{#if err}<p class="rounded-xl bg-red-50 px-3 py-2 text-sm font-extrabold text-danger">{err}</p>{/if}
		<div class="card space-y-2 p-3 text-sm font-bold">
			<div class="fd">Kapasitas</div>
			{#if b.capacity?.remainingQty != null}<p class="text-xs text-od">Sisa {b.capacity.remainingQty} pcs · masuk {b.capacity.heldQty}</p>{/if}
			{#if b.capacity?.remainingOrders != null}<p class="text-xs text-od">Sisa {b.capacity.remainingOrders} pesanan</p>{/if}
			<label class="text-xs font-extrabold text-ink2">Kuota pcs<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={quota} /></label>
			<div class="grid grid-cols-2 gap-2">
				<label class="text-xs font-extrabold text-ink2">Min pcs<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={minQty} /></label>
				<label class="text-xs font-extrabold text-ink2">Min Rp<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={minRp} /></label>
				<label class="text-xs font-extrabold text-ink2">Maks / orang<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={maxCust} /></label>
				<label class="text-xs font-extrabold text-ink2">Kuota pesanan<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={capOrders} /></label>
				<label class="text-xs font-extrabold text-ink2">Min lanjut<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={minProceed} /></label>
				<label class="text-xs font-extrabold text-ink2">Lead menit<input class="mt-1 h-10 w-full rounded-xl border border-line px-2" type="number" bind:value={lead} /></label>
			</div>
			<label class="flex items-center gap-2 text-sm font-extrabold"><input type="checkbox" bind:checked={waitlist} /> Daftar tunggu</label>
			{#each b.items as it}
				<div class="border-t border-dashed border-line pt-2">
					<div>{it.name}</div>
					<div class="text-[11px] text-ink2">Rp {it.price.toLocaleString('id-ID')} · masuk {it.sold}{#if it.remaining != null} · Sisa {it.remaining}{/if}</div>
					{#if itemCap[it.id]}
						<div class="mt-1 grid grid-cols-3 gap-1">
							<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="kuota" bind:value={itemCap[it.id].quota} />
							<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="min" bind:value={itemCap[it.id].min} />
							<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="maks" bind:value={itemCap[it.id].max} />
						</div>
					{/if}
				</div>
			{/each}
			{#each b.pickupSlots || [] as s, i}
				<div class="text-[11px] text-ink2">{s.label || s.start}{#if s.remainingQty != null} · Sisa {s.remainingQty}{/if}</div>
				{#if slotCap[i]}
					<div class="grid grid-cols-2 gap-1">
						<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="kuota pcs slot" bind:value={slotCap[i].qty} />
						<input class="h-9 rounded-lg border border-line px-1 text-xs" placeholder="kuota pesanan" bind:value={slotCap[i].orders} />
					</div>
				{/if}
			{/each}
			<button class="btn sm w-full" onclick={saveCap}>Simpan kapasitas</button>
		</div>
		<div class="flex gap-2">
			{#if b.status !== 'open'}<button class="btn sm" onclick={() => flip(true)}>{c.storeOpen}</button>{/if}
			{#if b.status === 'open'}<button class="btn sm ghost" onclick={() => flip(false)}>{c.storeClosed}</button>{/if}
			<button class="btn sm ghost" onclick={dup}>Duplikat</button>
			<a class="btn sm mint" href="/app/po/{b.id}/rekap">{c.recap}</a>
		</div>
		<div class="card p-3">
			<div class="break-all text-xs font-extrabold">{link}</div>
			<button class="btn sm mt-2" onclick={() => { navigator.clipboard.writeText(link); copied = true; }}>{copied ? c.copied : c.copy}</button>
			<a class="btn sm wa mt-2" href={'https://wa.me/?text=' + encodeURIComponent(b.title + ' ' + link)}>WhatsApp</a>
		</div>
		{#if qr}
			<img src={qr} alt="QR" class="mx-auto h-40 w-40 rounded-2xl bg-white p-2" />
			<div class="flex gap-2">
				<button class="btn sm ghost flex-1" onclick={() => download(1024)}>PNG 1024</button>
				<button class="btn sm ghost flex-1" onclick={() => download(2048)}>PNG 2048</button>
			</div>
		{/if}
	</div>
{/if}
