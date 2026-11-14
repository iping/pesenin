<script lang="ts">
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { me, loadMe, brandOf } from '$lib/session';
	import { api } from '$lib/api';
	import { rp, greet } from '$lib/format';
	import { liveHours } from '$lib/hours';
	import QRCode from 'qrcode';

	let c = $derived(copy($lang));
	let dash = $state<any>(null);
	let brand = $derived(brandOf($me));
	let qr = $state('');
	let tick = $state(0);

	let st = $derived(brand?.stores?.[0]);
	let hv = $derived.by(() => {
		tick;
		return liveHours(st?.hours, $lang, Date.now(), st?.timezone || 'Asia/Jakarta');
	});
	let openNow = $derived(hv ? hv.open : !!st?.isOpen);

	$effect(() => {
		const clock = setInterval(() => (tick += 1), 30000);
		return () => clearInterval(clock);
	});

	onMount(async () => {
		const data = $me || (await loadMe());
		const b = brandOf(data);
		if (!b) return;
		dash = await api('/api/v1/dashboard?brandId=' + b.id);
		qr = await QRCode.toDataURL(location.origin + '/' + b.slug, { width: 280, margin: 1, errorCorrectionLevel: 'M' });
	});

	async function toggle() {
		if (!st) return;
		const open = openNow;
		if (st.hours?.source && st.hours.source !== 'legacy') {
			if (st.hours.source === 'override' && st.hours.scheduleOpen !== open) {
				await api('/api/v1/stores/' + st.id + '/override', { method: 'DELETE' });
			} else {
				await api('/api/v1/stores/' + st.id + '/override', {
					method: 'POST',
					body: JSON.stringify({ kind: open ? 'closed' : 'open' })
				});
			}
		} else {
			await api('/api/v1/stores/' + st.id, {
				method: 'PATCH',
				body: JSON.stringify({
					name: st.name,
					address: st.address,
					mapsUrl: st.mapsUrl,
					isOpen: !open,
					acceptPreordersWhenClosed: st.acceptPreordersWhenClosed
				})
			});
		}
		await loadMe();
	}

	function downloadQR() {
		const a = document.createElement('a');
		a.href = qr;
		a.download = (brand?.slug || 'toko') + '-qr.png';
		a.click();
	}
</script>

{#if $me && brand}
	<div class="space-y-2.5 px-4 pt-4">
		<div class="flex items-center gap-2">
			<img src="/icon-512.png" alt="" class="h-11 w-11" />
			<div class="flex-1">
				<div class="text-sm font-bold text-ink2">{greet($me.user.name, $lang)} 👋</div>
				<h1 class="text-xl">{brand.name}</h1>
			</div>
			<a href="/app/notif" class="relative grid h-10 w-10 place-items-center rounded-2xl bg-white shadow-[0_2px_0_#F1E4D6]" aria-label={c.notifications}>🔔</a>
		</div>

		<div class="card flex items-center gap-3 p-3">
			<img src="/icon-512.png" alt="" class="h-11 w-11" />
			<div class="flex-1">
				<div class="flex flex-wrap items-center gap-2">
					<b class="fd">Toko lagi</b>
					<span class="rounded-full px-2 py-0.5 text-xs font-extrabold text-white {hv?.state === 'closing_soon' ? 'bg-o' : openNow ? 'bg-[#2EAD5B]' : 'bg-[#9A9A9A]'}">{hv?.badge || (openNow ? c.storeOpen : c.storeClosed)}</span>
				</div>
				<div class="text-[11px] font-bold text-ink2">{hv?.hint || st?.address || brand.slug}</div>
				<a class="text-[11px] font-extrabold text-od" href="/app/jadwal">{c.hoursLink}</a>
			</div>
			<button class="h-7 w-12 rounded-full {openNow ? 'bg-mint' : 'bg-[#D9D0C8]'}" onclick={toggle} aria-label="buka tutup"><span class="block h-5 w-5 rounded-full bg-white {openNow ? 'ml-6' : 'ml-1'}"></span></button>
		</div>

		<div class="relative overflow-hidden rounded-[22px] bg-gradient-to-br from-[#FFA445] via-o to-[#EA6A0C] p-4 text-white shadow-[0_5px_0_#E06F0A]">
			<div class="text-sm font-bold opacity-90">{c.balance}</div>
			<div class="fd text-3xl">{$me.wallet.balanceKoin.toLocaleString('id-ID')}</div>
			<div class="text-xs font-bold opacity-90">≈ {$me.wallet.orderEquivalent} pesanan · Rp {rp($me.orderFee)}/pesanan</div>
			<a class="btn sm absolute right-4 top-4 bg-white text-od shadow-[0_3px_0_#F3C79B]" href="/app/koin">+ {c.topup}</a>
		</div>

		<div class="grid grid-cols-3 gap-2">
			<div class="card p-2.5"><div class="text-[11px] font-bold text-ink2">{c.orders}</div><b class="fd text-xl">{dash?.todayOrders ?? '–'}</b></div>
			<div class="card bg-[#FFF8F0] p-2.5"><div class="text-[11px] font-bold text-ink2">{c.needCheck}</div><b class="fd text-xl text-od">{dash?.needCheck ?? '–'}</b></div>
			<div class="card p-2.5"><div class="text-[11px] font-bold text-ink2">Omzet</div><b class="fd text-xl text-mintd">{dash ? rp(dash.omzet) : '–'}</b></div>
		</div>

		{#if dash?.openBatch}
			<a class="card block p-3.5" href="/app/po/{dash.openBatch.id}">
				<b class="fd">{dash.openBatch.title}</b>
				<div class="text-[11px] font-bold text-ink2">{dash.openBatch.status}</div>
			</a>
		{:else}
			<div class="card p-4 text-center">
				<img src="/brand/placeholder-1x1.png" alt="" class="mx-auto h-16 w-16 rounded-2xl" />
				<p class="mt-2 text-sm font-extrabold">{c.emptyOrders}</p>
			</div>
		{/if}

		<div class="grid grid-cols-4 gap-2 text-center text-[11px] font-extrabold">
			<a href="/app/po/baru" class="card grid place-items-center py-3 text-o">{c.makePo}</a>
			<a href="/{brand.slug}" class="card grid place-items-center py-3 text-mintd">{c.share}</a>
			<button class="card py-3" onclick={downloadQR}>{c.qr}</button>
			<a href={dash?.openBatch ? `/app/po/${dash.openBatch.id}/rekap` : '/app/po'} class="card grid place-items-center py-3 text-[#2F8BE6]">{c.recap}</a>
		</div>
		{#if qr}
			<div class="card flex items-center gap-3 bg-ink p-3 text-white">
				<img src={qr} alt="QR" class="h-20 w-20 rounded-xl bg-white p-1" />
				<div>
					<b class="fd text-lg">{c.tagline}</b>
					<div class="text-[11px] font-bold opacity-80">{location.origin}/{brand.slug}</div>
					<button class="btn sm mt-2" onclick={downloadQR}>PNG</button>
				</div>
			</div>
		{/if}
	</div>
{:else}
	<p class="p-6 font-bold">{c.loading}</p>
{/if}
