// Client-side upload prep. The server re-encodes every file; this only makes
// the upload smaller on mobile data and strips EXIF/GPS before it leaves the phone.
// Variant URL names (f2400-d1440-t480.webp) match backend/internal/images.

export const uploadAccept = 'image/jpeg,image/png,image/webp,image/gif,image/heic,image/heif,.heic,.heif,.jpg,.jpeg,.png,.webp,.gif';

export type UploadProfile = 'proof' | 'product';

type Profile = {
	maxEdge: number;
	quality: number;
	floor: number;
	budget: number;
	name: string;
};

const profiles: Record<UploadProfile, Profile> = {
	// Proofs stay sharp enough to read an account number after the server
	// re-encodes at WebP q86 / 2400px. The floor stops the budget loop
	// from smearing small text.
	proof: { maxEdge: 2400, quality: 0.9, floor: 0.82, budget: 850_000, name: 'bukti' },
	product: { maxEdge: 1600, quality: 0.82, floor: 0.74, budget: 380_000, name: 'foto' }
};

export type ImageSet = {
	src: string;
	full: string;
	display: string;
	thumb: string;
	srcset: string;
	fullWidth: number;
};

const emptySet: ImageSet = { src: '', full: '', display: '', thumb: '', srcset: '', fullWidth: 0 };

export function imageSet(input: string | null | undefined): ImageSet {
	if (!input) return emptySet;
	const url = input.trim();
	const m = url.match(/^(.*\/)([fdt]\d+(?:-[fdt]\d+)*)\.(webp|jpe?g)$/i);
	if (!m) return { src: url, full: url, display: url, thumb: url, srcset: '', fullWidth: 0 };
	const dir = m[1];
	const ext = m[3].toLowerCase();
	const parts = m[2].split('-').map(parsePart);
	if (parts.some((p) => !p)) return { src: url, full: url, display: url, thumb: url, srcset: '', fullWidth: 0 };
	const parsed = parts as { role: 'f' | 'd' | 't'; w: number }[];
	const href = (i: number) => (i === 0 ? url : `${dir}${parsed[i].role}${parsed[i].w}.${ext}`);
	const set: ImageSet = { src: url, full: '', display: '', thumb: '', srcset: '', fullWidth: 0 };
	const cands: { url: string; w: number }[] = [];
	parsed.forEach((p, i) => {
		const u = href(i);
		if (p.role === 'f') {
			set.full = u;
			set.fullWidth = p.w;
		} else if (p.role === 'd') set.display = u;
		else set.thumb = u;
		cands.push({ url: u, w: p.w });
	});
	if (!set.full) set.full = set.src;
	if (!set.display) set.display = set.src;
	if (!set.thumb) set.thumb = set.display;
	if (!set.fullWidth) set.fullWidth = parsed[0].w;
	if (cands.length > 1) {
		cands.sort((a, b) => a.w - b.w);
		set.srcset = cands.map((c) => `${c.url} ${c.w}w`).join(', ');
	}
	return set;
}

function parsePart(s: string): { role: 'f' | 'd' | 't'; w: number } | null {
	const m = s.match(/^([fdt])(\d+)$/);
	if (!m) return null;
	const w = Number(m[2]);
	if (!w || w > 10000) return null;
	return { role: m[1] as 'f' | 'd' | 't', w };
}

export async function prepareUpload(file: File, kind: UploadProfile): Promise<File> {
	if (typeof document === 'undefined') return file;
	const profile = profiles[kind];
	const raster = await rasterBlob(file);
	const oriented = await loadOriented(raster);
	const fitted = fit(oriented.sw, oriented.sh, oriented.orientation, profile.maxEdge);
	const canvas = document.createElement('canvas');
	canvas.width = fitted.cw;
	canvas.height = fitted.ch;
	const ctx = canvas.getContext('2d', { alpha: false });
	if (!ctx) throw new Error('Fotonya belum kebaca. Coba JPG ya.');
	ctx.fillStyle = '#ffffff';
	ctx.fillRect(0, 0, fitted.cw, fitted.ch);
	ctx.imageSmoothingEnabled = true;
	ctx.imageSmoothingQuality = 'high';
	ctx.save();
	applyOrientation(ctx, oriented.orientation, fitted.dw, fitted.dh);
	ctx.drawImage(oriented.source, 0, 0, fitted.dw, fitted.dh);
	ctx.restore();
	oriented.close();

	let quality = profile.quality;
	let blob = await encodeCanvas(canvas, quality);
	while (blob.size > profile.budget && quality - 0.04 >= profile.floor - 0.001) {
		quality = Math.round((quality - 0.04) * 100) / 100;
		blob = await encodeCanvas(canvas, quality);
	}
	if (blob.size > 8 * 1024 * 1024) throw new Error('Fotonya terlalu besar. Coba foto lain ya.');
	const ext = blob.type === 'image/webp' ? 'webp' : 'jpg';
	return new File([blob], `${profile.name}.${ext}`, { type: blob.type || 'image/jpeg', lastModified: Date.now() });
}

type Oriented = {
	source: CanvasImageSource;
	sw: number;
	sh: number;
	orientation: number;
	close: () => void;
};

async function loadOriented(blob: Blob): Promise<Oriented> {
	const meta = await jpegMeta(blob);
	if (typeof createImageBitmap === 'function') {
		try {
			const bmp = await createImageBitmap(blob, { imageOrientation: 'from-image' });
			const swaps = meta.orientation >= 5 && meta.orientation <= 8;
			const ignored = swaps && meta.width > 0 && meta.width !== meta.height && bmp.width === meta.width && bmp.height === meta.height;
			if (!ignored) {
				return { source: bmp, sw: bmp.width, sh: bmp.height, orientation: 1, close: () => bmp.close() };
			}
			return { source: bmp, sw: meta.width, sh: meta.height, orientation: meta.orientation, close: () => bmp.close() };
		} catch {
			/* HTMLImageElement fallback, including browsers without the option */
		}
	}
	const img = await loadImage(blob);
	return { source: img, sw: img.naturalWidth, sh: img.naturalHeight, orientation: meta.orientation || 1, close: () => {} };
}

function fit(sw: number, sh: number, orientation: number, maxEdge: number) {
	const swaps = orientation >= 5 && orientation <= 8;
	const shownW = swaps ? sh : sw;
	const shownH = swaps ? sw : sh;
	const scale = Math.min(1, maxEdge / Math.max(shownW, shownH, 1));
	const dw = Math.max(1, Math.round(sw * scale));
	const dh = Math.max(1, Math.round(sh * scale));
	return {
		dw,
		dh,
		cw: swaps ? dh : dw,
		ch: swaps ? dw : dh
	};
}

function applyOrientation(ctx: CanvasRenderingContext2D, orientation: number, dw: number, dh: number) {
	switch (orientation) {
		case 2:
			ctx.transform(-1, 0, 0, 1, dw, 0);
			break;
		case 3:
			ctx.transform(-1, 0, 0, -1, dw, dh);
			break;
		case 4:
			ctx.transform(1, 0, 0, -1, 0, dh);
			break;
		case 5:
			ctx.transform(0, 1, 1, 0, 0, 0);
			break;
		case 6:
			ctx.transform(0, 1, -1, 0, dh, 0);
			break;
		case 7:
			ctx.transform(0, -1, -1, 0, dh, dw);
			break;
		case 8:
			ctx.transform(0, -1, 1, 0, 0, dw);
			break;
		default:
			break;
	}
}

async function encodeCanvas(canvas: HTMLCanvasElement, quality: number): Promise<Blob> {
	const webp = await canvasBlob(canvas, 'image/webp', quality);
	if (webp && webp.size > 0 && webp.type !== 'image/png') {
		if (!webp.type) return new Blob([webp], { type: 'image/webp' });
		return webp;
	}
	const jpeg = await canvasBlob(canvas, 'image/jpeg', Math.min(0.92, quality + 0.04));
	if (!jpeg || jpeg.size === 0) throw new Error('Fotonya belum kebaca. Coba JPG ya.');
	return jpeg;
}

function canvasBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob | null> {
	return new Promise((resolve) => canvas.toBlob((b) => resolve(b), type, quality));
}

async function rasterBlob(file: File): Promise<Blob> {
	const name = file.name.toLowerCase();
	const type = (file.type || '').toLowerCase();
	const heic = type.includes('heic') || type.includes('heif') || name.endsWith('.heic') || name.endsWith('.heif');
	if (!heic) return file;
	if (typeof createImageBitmap === 'function') {
		try {
			const probe = await createImageBitmap(file);
			probe.close();
			return file;
		} catch {
			/* convert below */
		}
	}
	try {
		const mod = await import('heic2any');
		const out = await mod.default({ blob: file, toType: 'image/jpeg', quality: 0.92 });
		return Array.isArray(out) ? out[0] : out;
	} catch {
		throw new Error('Foto HEIC belum bisa diubah di HP ini. Ubah ke JPG dulu ya.');
	}
}

function loadImage(blob: Blob): Promise<HTMLImageElement> {
	return new Promise((resolve, reject) => {
		const url = URL.createObjectURL(blob);
		const img = new Image();
		img.onload = () => {
			URL.revokeObjectURL(url);
			resolve(img);
		};
		img.onerror = () => {
			URL.revokeObjectURL(url);
			reject(new Error('Fotonya belum kebaca. Coba JPG ya.'));
		};
		img.src = url;
	});
}

async function jpegMeta(blob: Blob): Promise<{ orientation: number; width: number; height: number }> {
	const none = { orientation: 1, width: 0, height: 0 };
	const buf = await blob.slice(0, 256 * 1024).arrayBuffer();
	const v = new DataView(buf);
	if (v.byteLength < 4 || v.getUint16(0) !== 0xffd8) return none;
	let o = 2;
	let orientation = 1;
	let width = 0;
	let height = 0;
	while (o + 4 < v.byteLength) {
		const marker = v.getUint16(o);
		if (marker === 0xffda || marker === 0xffd9) break;
		if ((marker & 0xff00) !== 0xff00) break;
		const len = v.getUint16(o + 2);
		if (len < 2 || o + 2 + len > v.byteLength) break;
		if (marker === 0xffe1 && len > 8 && v.getUint32(o + 4) === 0x45786966) {
			orientation = readOrientation(v, o + 10) || orientation;
		}
		if ((marker >= 0xffc0 && marker <= 0xffc3) || (marker >= 0xffc5 && marker <= 0xffc7) || (marker >= 0xffc9 && marker <= 0xffcb)) {
			height = v.getUint16(o + 5);
			width = v.getUint16(o + 7);
		}
		o += 2 + len;
	}
	return { orientation, width, height };
}

function readOrientation(v: DataView, tiff: number): number {
	if (tiff + 8 > v.byteLength) return 1;
	const be = v.getUint16(tiff) === 0x4d4d;
	const get16 = (p: number) => (be ? v.getUint16(p) : v.getUint16(p, true));
	const get32 = (p: number) => (be ? v.getUint32(p) : v.getUint32(p, true));
	if (get16(tiff + 2) !== 0x002a) return 1;
	const offset = get32(tiff + 4);
	let p = tiff + offset;
	if (p + 2 > v.byteLength) return 1;
	const count = get16(p);
	p += 2;
	for (let i = 0; i < count; i++) {
		const e = p + i * 12;
		if (e + 12 > v.byteLength) return 1;
		if (get16(e) === 0x0112) {
			const n = get16(e + 8);
			return n >= 1 && n <= 8 ? n : 1;
		}
	}
	return 1;
}
