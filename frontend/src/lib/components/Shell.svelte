<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { me, loadMe, brandOf } from '$lib/session';
	import { ApiError, api } from '$lib/api';
	import { rp } from '$lib/format';

	let { children } = $props();
	let c = $derived(copy($lang));
	let path = $derived($page.url.pathname);
	let unread = $state(0);
	let ios = $state(false);
	let askPush = $state(false);
	let err = $state('');

	const hideNav = $derived(
		path.startsWith('/app/masuk') ||
			path.startsWith('/app/daftar') ||
			path.startsWith('/app/lupa-sandi') ||
			path.startsWith('/app/atur-sandi') ||
			path.startsWith('/app/verifikasi-email') ||
			path.startsWith('/app/mulai')
	);

	onMount(async () => {
		const ua = navigator.userAgent;
		const standalone = window.matchMedia('(display-mode: standalone)').matches || (navigator as any).standalone;
		ios = /iPhone|iPad/.test(ua) && !standalone;
		if (hideNav) return;
		try {
			const data = await loadMe();
			if (!data.user.isPlatformAdmin && (!data.brands || data.brands.length === 0) && !path.startsWith('/app/mulai')) {
				goto('/app/mulai');
				return;
			}
			unread = (await api<{ count: number }>('/api/v1/notifications/unread-count')).count;
			const prev = Number(sessionStorage.getItem('pn_unread') || '0');
			if (unread > prev) new Audio('/sounds/ping-new-order.mp3').play().catch(() => {});
			sessionStorage.setItem('pn_unread', String(unread));
			if (localStorage.getItem('pn_push_asked') !== '1' && data.brands?.length && Notification && Notification.permission === 'default') {
				askPush = true;
			}
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) goto('/app/masuk');
			else err = e instanceof Error ? e.message : '';
		}
	});

	async function enablePush() {
		askPush = false;
		localStorage.setItem('pn_push_asked', '1');
		const perm = await Notification.requestPermission();
		if (perm !== 'granted') return;
		const key = await api<{ publicKey: string }>('/api/v1/push/vapid-public-key');
		const reg = await navigator.serviceWorker.ready;
		const sub = await reg.pushManager.subscribe({
			userVisibleOnly: true,
			applicationServerKey: urlBase64ToUint8Array(key.publicKey)
		});
		const json = sub.toJSON();
		await api('/api/v1/push/subscriptions', {
			method: 'POST',
			body: JSON.stringify({ endpoint: json.endpoint, keys: json.keys })
		});
	}

	function urlBase64ToUint8Array(base64String: string) {
		const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
		const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
		const raw = atob(base64);
		const out = new Uint8Array(raw.length);
		for (let i = 0; i < raw.length; ++i) out[i] = raw.charCodeAt(i);
		return out;
	}

	function on(href: string) {
		return path === href || (href !== '/app' && path.startsWith(href));
	}
</script>

<div class="mx-auto min-h-screen max-w-[430px] bg-cream {hideNav ? '' : 'pb-24'}">
	{#if ios && !hideNav}
		<div class="mx-3 mt-3 rounded-2xl bg-ink px-3 py-2 text-xs font-bold text-white">
			<b class="fd">{c.installTitle}</b>
			<div class="opacity-80">{c.installBody}</div>
		</div>
	{/if}
	{#if $me?.verification && (!$me.verification.emailVerified || !$me.verification.whatsappVerified) && !hideNav}
		<a href="/app/verifikasi" class="mx-3 mt-3 block rounded-2xl bg-o3 px-3 py-2 text-sm font-extrabold text-od">
			{$me.verification.kycGate ? c.verifyBanner : c.verifyBannerSoft}
			<span class="mt-1 block text-ink">{c.verifyNow}</span>
		</a>
	{/if}
	{#if $me?.lowBalance && !$me?.softLocked && !hideNav}
		<a href="/app/koin" class="mx-3 mt-3 block rounded-2xl bg-[#FFF8DB] px-3 py-2 text-sm font-extrabold text-[#8A6A00]">{c.lowKoin} · {c.fillKoin}</a>
	{/if}
	{#if $me?.softLocked && !hideNav}
		<a href="/app/koin" class="mx-3 mt-3 block rounded-2xl bg-o px-3 py-2 text-sm font-extrabold text-white">{c.softLock}</a>
	{/if}
	{#if askPush}
		<div class="mx-3 mt-3 rounded-2xl bg-white p-3 shadow-[0_2px_0_#F1E4D6]">
			<p class="text-sm font-extrabold">{c.pushAsk}</p>
			<div class="mt-2 flex gap-2">
				<button class="btn sm" onclick={enablePush}>{c.pushOn}</button>
				<button class="btn sm ghost" onclick={() => { askPush = false; localStorage.setItem('pn_push_asked', '1'); }}>{c.later}</button>
			</div>
		</div>
	{/if}
	{#if err}<p class="px-4 pt-3 text-sm font-bold text-danger">{err}</p>{/if}
	{@render children()}
</div>

{#if !hideNav}
	<nav class="fixed inset-x-0 bottom-0 z-30 mx-auto flex h-[74px] max-w-[430px] rounded-t-[22px] border-t border-line bg-white px-1 pt-2 shadow-[0_-6px_20px_rgba(74,42,26,.06)]">
		{#each [
			['/app', c.navHome, '⌂'],
			['/app/pesanan', c.navOrders, '☰'],
			['/app/po', c.navPo, '▣'],
			['/app/koin', c.navKoin, '★'],
			['/app/akun', c.navAccount, '☺']
		] as [href, label, icon]}
			<a class="relative flex flex-1 flex-col items-center gap-0.5 text-[11px] font-extrabold {on(href) ? 'text-o' : 'text-ink3'}" {href}>
				<span class="grid h-7 w-11 place-items-center rounded-xl {on(href) ? 'bg-o3' : ''}">{icon}</span>
				{label}
				{#if href === '/app/pesanan' && unread}<span class="absolute right-3 top-0 rounded-full bg-danger px-1 text-[10px] text-white">{unread}</span>{/if}
			</a>
		{/each}
	</nav>
{/if}

{#if $me?.wallet && !hideNav}
	<span class="sr-only">Koin {rp($me.wallet.balanceKoin)}</span>
{/if}
