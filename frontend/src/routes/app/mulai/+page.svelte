<script lang="ts">
	import { goto } from '$app/navigation';
	import { lang, copy } from '$lib/i18n';
	import { api, ApiError } from '$lib/api';
	import { loadMe } from '$lib/session';

	let c = $derived(copy($lang));
	let step = $state(1);
	let name = $state('');
	let slug = $state('');
	let slugInfo = $state<any>(null);
	let bankCode = $state('BCA');
	let accountNumber = $state('');
	let holderName = $state('');
	let productName = $state('');
	let price = $state(15000);
	let brandId = $state('');
	let err = $state('');
	let banks = $state<any[]>([]);

	$effect(() => {
		api('/api/v1/bank-codes').then((rows) => (banks = rows as any[]));
	});

	async function check() {
		if (slug.trim().length < 2 && name) slug = name;
		slugInfo = await api('/api/v1/slugs/check?slug=' + encodeURIComponent(slug || name));
		slug = slugInfo.slug;
	}

	async function step1() {
		err = '';
		try {
			const res = await api<any>('/api/v1/brands', {
				method: 'POST',
				body: JSON.stringify({ name, slug: slugInfo?.slug || slug, description: '', wa: '' })
			});
			brandId = res.brand.id;
			step = 2;
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal';
		}
	}
	async function step2() {
		err = '';
		try {
			await api('/api/v1/brands/' + brandId + '/bank-accounts', {
				method: 'POST',
				body: JSON.stringify({ bankCode, accountNumber, holderName, isDefault: true })
			});
			step = 3;
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal';
		}
	}
	async function step3() {
		err = '';
		try {
			await api('/api/v1/brands/' + brandId + '/products', {
				method: 'POST',
				body: JSON.stringify({ name: productName, price, description: '' })
			});
			await loadMe();
			goto('/app/po/baru');
		} catch (e) {
			err = e instanceof ApiError ? e.message : 'Gagal';
		}
	}
</script>

<div class="px-5 pt-8">
	<img src="/icon-512.png" alt="" class="h-16 w-16" />
	<p class="mt-3 font-extrabold text-ink2">{c.onboard}</p>
	<p class="fd text-sm text-od">{step} / 3</p>
	{#if step === 1}
		<h1 class="text-2xl">{c.stepBrand}</h1>
		<input class="mt-3 h-12 w-full rounded-xl border border-line px-3" placeholder="Dapur Senja" bind:value={name} oninput={check} />
		<input class="mt-2 h-12 w-full rounded-xl border border-line px-3" placeholder="dapur-senja" bind:value={slug} oninput={check} />
		{#if slugInfo}
			<p class="mt-1 text-xs font-extrabold {slugInfo.available ? 'text-mintd' : 'text-danger'}">{slugInfo.available ? 'Tersedia' : slugInfo.reason} {slugInfo.suggestions?.join(', ')}</p>
		{/if}
		<button class="btn mt-4" onclick={step1} disabled={!name}>{c.next}</button>
	{:else if step === 2}
		<h1 class="text-2xl">{c.stepBank}</h1>
		<select class="mt-3 h-12 w-full rounded-xl border border-line px-3" bind:value={bankCode}>
			{#each banks as b}<option value={b.code}>{b.name}</option>{/each}
		</select>
		<input class="mt-2 h-12 w-full rounded-xl border border-line px-3" placeholder="Nomor rekening" bind:value={accountNumber} />
		<input class="mt-2 h-12 w-full rounded-xl border border-line px-3" placeholder="Nama pemilik" bind:value={holderName} />
		<button class="btn mt-4" onclick={step2}>{c.next}</button>
	{:else}
		<h1 class="text-2xl">{c.stepProduct}</h1>
		<input class="mt-3 h-12 w-full rounded-xl border border-line px-3" placeholder="Salt Bread Original" bind:value={productName} />
		<input class="mt-2 h-12 w-full rounded-xl border border-line px-3" type="number" bind:value={price} />
		<button class="btn mt-4" onclick={step3}>{c.publish}</button>
	{/if}
	{#if err}<p class="mt-3 text-sm font-extrabold text-danger">{err}</p>{/if}
</div>
