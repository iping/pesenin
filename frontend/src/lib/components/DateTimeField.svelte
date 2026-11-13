<script lang="ts">
	import { presetWall, snapWall, tzLabel } from '$lib/format';

	let {
		label,
		value = $bindable(''),
		tz = 'Asia/Jakarta',
		optional = false
	}: { label: string; value: string; tz?: string; optional?: boolean } = $props();

	const presets = [
		{ name: 'Besok 08:00', day: 1, h: 8, m: 0 },
		{ name: 'Besok 20:00', day: 1, h: 20, m: 0 },
		{ name: 'Lusa 09:00', day: 2, h: 9, m: 0 },
		{ name: 'Lusa 20:00', day: 2, h: 20, m: 0 }
	];

	function apply(day: number, h: number, m: number) {
		value = presetWall(day, h, m, tz);
	}
</script>

<label class="block text-xs font-extrabold text-ink2">
	{label} <span class="text-od">{tzLabel(tz)}</span>
	{#if optional}<span class="text-ink3"> · opsional</span>{/if}
	<input
		class="mt-1 h-10 w-full rounded-xl border border-line bg-[#FFFCF8] px-2 text-sm font-bold text-ink"
		type="datetime-local"
		step="900"
		bind:value
		onchange={() => (value = snapWall(value))}
	/>
	<span class="mt-1 flex flex-wrap gap-1">
		{#each presets as p}
			<button type="button" class="rounded-full bg-o3 px-2 py-0.5 text-[10px] font-extrabold text-od" onclick={() => apply(p.day, p.h, p.m)}>{p.name}</button>
		{/each}
		{#if optional && value}
			<button type="button" class="rounded-full bg-[#EEE] px-2 py-0.5 text-[10px] font-extrabold text-ink2" onclick={() => (value = '')}>Hapus</button>
		{/if}
	</span>
</label>
