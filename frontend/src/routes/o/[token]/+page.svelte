<script lang="ts">
	import { lang, copy } from '$lib/i18n';
	import { api, ApiError } from '$lib/api';
	import { rp, splitAmount, when, countdown, syncClock } from '$lib/format';
	import { prepareUpload, uploadAccept } from '$lib/images';

	let { data } = $props();
	let order = $state(data.order);
	let c = $derived(copy($lang));
	let err = $state('');
	let busy = $state(false);
	let copied = $state('');
	let tick = $state(0);
	let file = $state<File | null>(null);

	$effect(() => {
		const id = setInterval(() => (tick += 1), 1000);
		return () => clearInterval(id);
	});

	let tz = $derived(order.store?.timezone || 'Asia/Jakarta');
	$effect(() => syncClock(order.serverNow));
	let parts = $derived(splitAmount(order.totalDue));
	let clock = $derived(order.expiresAt ? countdown(order.expiresAt) + (tick ? '' : '') : '');
	let paid = $derived(order.paymentStatus === 'paid' || order.paymentStatus === 'cancelled' || order.paymentStatus === 'expired');

	async function copyText(label: string, value: string) {
		await navigator.clipboard.writeText(value);
		copied = label;
		setTimeout(() => (copied = ''), 1200);
	}

	async function send() {
		if (!file) return;
		busy = true;
		err = '';
		try {
			const prepared = await prepareUpload(file, 'proof');
			const body = new FormData();
			body.append('file', prepared, prepared.name);
			const res = await fetch('/api/v1/public/orders/' + order.publicToken + '/proof', { method: 'POST', body });
			if (!res.ok) {
				const j = await res.json().catch(() => ({}));
				throw new ApiError(res.status, j.error || 'bad_file', j.message || 'Gagal unggah.');
			}
			order = await api('/api/v1/public/orders/' + order.publicToken);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Gagal';
		} finally {
			busy = false;
		}
	}

	function waSeller() {
		const n = (order.brand.waNumber || '').replace('+', '');
		const text = encodeURIComponent(`Halo, saya ${order.customer.name}, pesanan ${order.code}.`);
		return n ? `https://wa.me/${n}?text=${text}` : '';
	}

	const steps = $derived([
		['Dipesan', true],
		['Bukti dikirim', order.paymentStatus !== 'pending' && order.paymentStatus !== 'expired'],
		['Dibayar', order.paymentStatus === 'paid'],
		['Diproses', order.paymentStatus === 'paid' && order.fulfilmentStatus !== 'not_ready' ? false : order.paymentStatus === 'paid'],
		['Siap diambil', order.fulfilmentStatus === 'ready' || order.fulfilmentStatus === 'completed']
	]);
</script>

<svelte:head>
	<title>{order.code} · {order.brand.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="mx-auto min-h-screen max-w-[430px] bg-cream px-4 pb-28 pt-4">
	<h1 class="text-xl">{paid && order.paymentStatus === 'paid' ? c.statusTitle : c.payTitle}</h1>
	<p class="text-[11px] font-extrabold text-ink2">{order.code} · {order.brand.name}</p>

	{#if order.paymentStatus === 'paid'}
		<div class="card relative mt-3 overflow-hidden bg-gradient-to-b from-o3 to-white p-4">
			<div class="flex items-center gap-3">
				<img src="/icon-512.png" alt="" class="h-16 w-16 -rotate-6" />
				<div>
					<h2 class="text-xl leading-tight">{c.paidTitle} 🎉</h2>
					<p class="mt-1 text-sm font-bold text-ink2">Pingping lagi bantu {order.brand.name} siapin pesananmu.</p>
				</div>
			</div>
		</div>
		<div class="card mt-3 p-4">
			{#each steps as [label, on], i}
				<div class="flex gap-3 pb-3">
					<span class="grid h-6 w-6 place-items-center rounded-full text-white {on ? 'bg-mint' : 'border-2 border-[#E6DCD2] bg-white'}"></span>
					<div>
						<b class="fd {on ? '' : 'text-ink3'}">{label}</b>
						{#if i === 4}<div class="text-[11px] font-bold text-ink2">{order.pickupSlotLabel || when(order.batch.readyAt, tz)} · {order.store.address}</div>{/if}
					</div>
				</div>
			{/each}
		</div>
	{:else if order.paymentStatus === 'expired' || order.paymentStatus === 'cancelled'}
		<div class="card mt-3 p-4">
			<h2 class="text-xl">{order.paymentStatus === 'expired' ? 'Waktu bayar habis' : 'Pesanan dibatalkan'}</h2>
			<p class="mt-1 text-sm font-bold text-ink2">Kalau masih mau, pesan ulang lewat link toko ya.</p>
		</div>
	{:else}
		{#if order.expiresAt}
			<div class="mt-3 flex items-center gap-2 rounded-2xl bg-ink px-3.5 py-2.5 text-white">
				<div class="flex-1"><div class="text-[11px] font-bold opacity-80">Batas {when(order.expiresAt, tz)}</div><b class="fd">Sisa waktu</b></div>
				<b class="fd text-lg">{clock}</b>
			</div>
		{/if}
		<div class="card mt-3 border-2 border-o2 p-4 text-center">
			<div class="text-sm font-bold text-ink2">{c.transferExact}</div>
			<div class="fd mt-1 text-4xl">Rp {parts.head.replace(/\B(?=(\d{3})+(?!\d))/g, '.')}.<span class="text-o underline decoration-[3px] underline-offset-4">{parts.tail}</span></div>
			<button class="mx-auto mt-1 text-sm font-extrabold text-od" onclick={() => copyText('amount', String(order.totalDue))}>{copied === 'amount' ? c.copied : c.copy}</button>
			<div class="mt-1 text-xs font-bold text-ink2">Pesanan Rp {rp(order.baseTotal)} + <span class="rounded bg-o2 px-1 text-od">kode unik {order.uniqueSuffix}</span></div>
			<p class="mt-3 rounded-2xl bg-o3 p-3 text-left text-[11px] font-bold">3 angka terakhir (<b class="text-od">{parts.tail}</b>) itu kode unik pesananmu. Transfer pas sampai digit terakhir ya, biar penjual langsung tahu ini kamu!</p>
		</div>
		<div class="card mt-3 flex items-center gap-3 p-3.5">
			<div class="grid h-9 w-14 place-items-center rounded-lg bg-[#0060AF] text-sm font-black italic text-white">{order.bank.bankCode}</div>
			<div class="flex-1">
				<div class="text-[11px] font-bold text-ink2">{order.bank.bankCode} · a.n. {order.bank.holderName}</div>
				<b class="fd text-lg tracking-wide">{order.bank.accountNumber}</b>
			</div>
			<button class="btn sm ghost text-od" onclick={() => copyText('bank', order.bank.accountNumber)}>{c.copy}</button>
		</div>
		<div class="mt-3 space-y-1 text-sm font-bold">
			{#each order.items as it}
				<div class="flex"><span class="flex-1">{it.qty}× {it.name}</span><span>Rp {rp(it.unitPrice * it.qty)}</span></div>
			{/each}
			{#each order.charges as ch}
				<div class="flex"><span class="flex-1">{ch.name}</span><span>Rp {rp(ch.amount)}</span></div>
			{/each}
		</div>
		{#if order.latestRejectReason}
			<p class="mt-3 rounded-xl bg-red-50 px-3 py-2 text-sm font-extrabold text-danger">Bukti ditolak: {order.latestRejectReason}. Upload ulang ya.</p>
		{/if}
		<label class="mt-3 flex cursor-pointer items-center gap-3 rounded-[20px] border-2 border-dashed border-o bg-white p-3.5">
			<div class="grid h-12 w-12 place-items-center rounded-2xl bg-o3 text-2xl">📷</div>
			<div class="flex-1">
				<b class="fd">{c.upload}</b>
				<div class="text-[11px] font-bold text-ink2">{file ? file.name : c.uploadHint}</div>
			</div>
			<input class="hidden" type="file" accept={uploadAccept} onchange={(e) => (file = (e.target as HTMLInputElement).files?.[0] || null)} />
		</label>
		{#if err}<p class="mt-2 text-sm font-extrabold text-danger">{err}</p>{/if}
		<p class="mt-3 text-center text-[11px] font-bold text-ink2">{c.moneyDirect}</p>
	{/if}

	{#if waSeller()}
		<a class="btn wa mt-4" href={waSeller()} target="_blank" rel="noreferrer">{c.chatSeller}</a>
	{/if}
</div>

{#if !paid}
	<div class="fixed inset-x-0 bottom-4 z-20 mx-auto max-w-[400px] px-4">
		<button class="btn w-full" disabled={!file || busy} onclick={send}>{c.sendProof}</button>
	</div>
{/if}
