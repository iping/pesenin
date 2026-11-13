const idr = new Intl.NumberFormat('id-ID');

export function rp(n: number | null | undefined) {
	return idr.format(Number(n || 0));
}

const months = ['Jan', 'Feb', 'Mar', 'Apr', 'Mei', 'Jun', 'Jul', 'Agu', 'Sep', 'Okt', 'Nov', 'Des'];
const weekdays: Record<string, string> = {
	Mon: 'Sen',
	Tue: 'Sel',
	Wed: 'Rab',
	Thu: 'Kam',
	Fri: 'Jum',
	Sat: 'Sab',
	Sun: 'Min'
};

export function tzLabel(tz = 'Asia/Jakarta') {
	if (tz.includes('Makassar')) return 'WITA';
	if (tz.includes('Jayapura')) return 'WIT';
	return 'WIB';
}

function wallParts(iso: string, tz: string) {
	const dtf = new Intl.DateTimeFormat('en-GB', {
		timeZone: tz,
		hourCycle: 'h23',
		weekday: 'short',
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit'
	});
	const p = Object.fromEntries(dtf.formatToParts(new Date(iso)).map((x) => [x.type, x.value]));
	return {
		wd: weekdays[p.weekday] || p.weekday,
		day: String(Number(p.day)),
		month: months[Number(p.month) - 1],
		monthNum: Number(p.month),
		year: p.year,
		hour: p.hour,
		minute: p.minute
	};
}

/** "Sab, 14 Nov 2026 20:00 WIB" */
export function when(iso?: string | null, tz = 'Asia/Jakarta') {
	if (!iso) return '';
	const p = wallParts(iso, tz);
	return `${p.wd}, ${p.day} ${p.month} ${p.year} ${p.hour}:${p.minute} ${tzLabel(tz)}`;
}

/** "Min, 15 Nov 2026 10:00–12:00 WIB" when both ends share a local day. */
export function slot(start?: string | null, end?: string | null, tz = 'Asia/Jakarta') {
	if (!start) return '';
	const a = wallParts(start, tz);
	const label = tzLabel(tz);
	if (!end) return `${a.wd}, ${a.day} ${a.month} ${a.year} ${a.hour}:${a.minute} ${label}`;
	const b = wallParts(end, tz);
	if (a.day === b.day && a.month === b.month && a.year === b.year) {
		return `${a.wd}, ${a.day} ${a.month} ${a.year} ${a.hour}:${a.minute}–${b.hour}:${b.minute} ${label}`;
	}
	return `${when(start, tz)} – ${when(end, tz)}`;
}

let clockSkew = 0;

/** Countdowns use the server clock, not the device clock. */
export function syncClock(serverNow?: string | null) {
	if (!serverNow) return;
	clockSkew = new Date(serverNow).getTime() - Date.now();
}

export function serverNowMs() {
	return Date.now() + clockSkew;
}

function tzOffsetMs(utcMs: number, tz: string) {
	const dtf = new Intl.DateTimeFormat('en-US', {
		timeZone: tz,
		hourCycle: 'h23',
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit',
		second: '2-digit'
	});
	const p = Object.fromEntries(dtf.formatToParts(new Date(utcMs)).map((x) => [x.type, x.value]));
	const asUTC = Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour % 24, +p.minute, +p.second);
	return asUTC - utcMs;
}

/** Interpret a datetime-local value as wall time in the store timezone. */
export function wallToISO(local: string, tz = 'Asia/Jakarta') {
	if (!local) return '';
	const [date, time] = local.split('T');
	const [y, m, d] = date.split('-').map(Number);
	const [hh, mm] = (time || '00:00').split(':').map(Number);
	const utcGuess = Date.UTC(y, m - 1, d, hh, mm);
	return new Date(utcGuess - tzOffsetMs(utcGuess, tz)).toISOString();
}

/** Wall-clock value for a datetime-local input: today + dayOffset at hour:minute in tz. */
export function presetWall(dayOffset: number, hour: number, minute: number, tz = 'Asia/Jakarta') {
	const p = wallParts(new Date().toISOString(), tz);
	const dt = new Date(Date.UTC(Number(p.year), p.monthNum - 1, Number(p.day) + dayOffset));
	const y = dt.getUTCFullYear();
	const m = String(dt.getUTCMonth() + 1).padStart(2, '0');
	const d = String(dt.getUTCDate()).padStart(2, '0');
	return `${y}-${m}-${d}T${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
}

export function snapWall(local: string) {
	if (!local || !local.includes('T')) return local;
	const [date, time] = local.split('T');
	let [hh, mm] = time.split(':').map(Number);
	const rem = mm % 15;
	if (rem !== 0) mm += rem < 8 ? -rem : 15 - rem;
	if (mm >= 60) {
		hh = (hh + 1) % 24;
		mm = 0;
	}
	const p = (n: number) => String(n).padStart(2, '0');
	return `${date}T${p(hh)}:${p(mm)}`;
}

/** Fill a datetime-local input from an absolute timestamp. */
export function isoToWall(iso: string, tz = 'Asia/Jakarta') {
	const dtf = new Intl.DateTimeFormat('en-GB', {
		timeZone: tz,
		hourCycle: 'h23',
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit'
	});
	const p = Object.fromEntries(dtf.formatToParts(new Date(iso)).map((x) => [x.type, x.value]));
	return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
}

export function splitAmount(total: number) {
	const s = String(Math.abs(total)).padStart(4, '0');
	return { head: s.slice(0, -3), tail: s.slice(-3) };
}

export function waPretty(e164: string) {
	const d = e164.replace(/\D/g, '');
	if (d.startsWith('62')) return '0' + d.slice(2);
	return e164;
}

export function countdown(iso: string) {
	const ms = new Date(iso).getTime() - serverNowMs();
	if (ms <= 0) return '00:00:00';
	const h = Math.floor(ms / 3600000);
	const m = Math.floor((ms % 3600000) / 60000);
	const s = Math.floor((ms % 60000) / 1000);
	const p = (n: number) => String(n).padStart(2, '0');
	return `${p(h)}:${p(m)}:${p(s)}`;
}

export function greet(name: string, lang: 'id' | 'en') {
	const h = Number(
		new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Jakarta', hour: '2-digit', hourCycle: 'h23' }).format(new Date())
	);
	const first = name.split(' ')[0] || name;
	if (lang === 'en') {
		const g = h < 11 ? 'Morning' : h < 15 ? 'Afternoon' : h < 18 ? 'Evening' : 'Night';
		return `${g}, ${first}`;
	}
	const g = h < 11 ? 'Pagi' : h < 15 ? 'Siang' : h < 18 ? 'Sore' : 'Malam';
	return `${g}, ${first}`;
}
