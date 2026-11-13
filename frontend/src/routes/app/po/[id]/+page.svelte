<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { lang, copy } from '$lib/i18n';
	import { api } from '$lib/api';
	import QRCode from 'qrcode';

	let c = $derived(copy($lang));
	let b = $state<any>(null);
	let link = $state('');
	let qr = $state('');
	let copied = $state(false);

	onMount(async () => {
		b = await api('/api/v1/batches/' + $page.params.id);
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
		<div class="card p-3 text-sm font-bold">
			{#each b.items as it}
				<div class="flex items-center gap-2 border-b border-dashed border-line py-2">
					<img src="/brand/placeholder-1x1.png" alt="" class="h-10 w-10 rounded-lg object-cover" />
					<div class="flex-1">{it.name}<div class="text-[11px] text-ink2">Rp {it.price.toLocaleString('id-ID')} · {it.sold}/{it.quota ?? '∞'}</div></div>
				</div>
			{/each}
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
