const dayId = ['Minggu', 'Senin', 'Selasa', 'Rabu', 'Kamis', 'Jumat', 'Sabtu'];
const dayEn = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const wdIndex: Record<string, number> = { Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6 };

export type LiveHours = {
	open: boolean;
	state: 'open' | 'closing_soon' | 'closed';
	badge: string;
	hint: string;
};

function parts(ms: number, tz: string) {
	const dtf = new Intl.DateTimeFormat('en-US', {
		timeZone: tz,
		hourCycle: 'h23',
		weekday: 'short',
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit'
	});
	const p = Object.fromEntries(dtf.formatToParts(new Date(ms)).map((x) => [x.type, x.value]));
	let hour = p.hour;
	if (hour === '24') hour = '00';
	return {
		wd: wdIndex[p.weekday] ?? 0,
		date: `${p.year}-${p.month}-${p.day}`,
		hour,
		minute: p.minute
	};
}

/** Recompute the chip from the server payload as the clock crosses the next boundary. */
export function liveHours(h: any, lang: 'id' | 'en', nowMs: number, tz = 'Asia/Jakarta'): LiveHours | null {
	if (!h || typeof h !== 'object') return null;
	const closeAt = h.nextCloseAt ? Date.parse(h.nextCloseAt) : NaN;
	const openAt = h.nextOpenAt ? Date.parse(h.nextOpenAt) : NaN;
	let open = !!h.open;
	if (open && Number.isFinite(closeAt) && nowMs >= closeAt) open = false;
	if (!open && Number.isFinite(openAt) && nowMs >= openAt) open = true;
	const id = lang !== 'en';
	if (open) {
		const soon = Number.isFinite(closeAt) && closeAt > nowMs && closeAt - nowMs <= 30 * 60 * 1000;
		const clock = soon ? `${parts(closeAt, tz).hour}:${parts(closeAt, tz).minute}` : '';
		return {
			open: true,
			state: soon ? 'closing_soon' : 'open',
			badge: id ? 'Buka' : 'Open',
			hint: soon ? (id ? `Segera tutup · ${clock}` : `Closing soon · ${clock}`) : ''
		};
	}
	let badge = id ? 'Tutup' : 'Closed';
	if (Number.isFinite(openAt) && openAt > nowMs) {
		const local = parts(openAt, tz);
		const today = parts(nowMs, tz);
		const clock = `${local.hour}:${local.minute}`;
		if (local.date === today.date) badge = id ? `Buka pukul ${clock}` : `Opens at ${clock}`;
		else badge = id ? `Buka lagi ${dayId[local.wd]} ${clock}` : `Opens again ${dayEn[local.wd]} ${clock}`;
	}
	let hint = '';
	if (h.source === 'override' && h.reason) hint = id ? `Tutup sementara · ${h.reason}` : `Closed for now · ${h.reason}`;
	else if (h.note) hint = String(h.note);
	return { open: false, state: 'closed', badge, hint };
}

/** True when the pickup instant falls on a later civil date in the store zone. */
export function slotIsFuture(iso: string | null | undefined, tz: string, nowMs: number) {
	if (!iso) return false;
	const at = Date.parse(iso);
	if (!Number.isFinite(at)) return false;
	return parts(at, tz).date > parts(nowMs, tz).date;
}
