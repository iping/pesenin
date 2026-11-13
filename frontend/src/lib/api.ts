export class ApiError extends Error {
	code: string;
	status: number;
	constructor(status: number, code: string, message: string) {
		super(message);
		this.status = status;
		this.code = code;
	}
}

export async function api<T>(path: string, opts: RequestInit = {}): Promise<T> {
	const headers = new Headers(opts.headers);
	if (opts.body && !(opts.body instanceof FormData) && !headers.has('Content-Type')) {
		headers.set('Content-Type', 'application/json');
	}
	const res = await fetch(path, { credentials: 'include', ...opts, headers });
	const text = await res.text();
	let data: any = {};
	if (text) {
		try {
			data = JSON.parse(text);
		} catch {
			data = { message: text };
		}
	}
	if (!res.ok) throw new ApiError(res.status, data.error || 'error', data.message || 'Ada gangguan. Coba lagi ya.');
	return data as T;
}

export function idem() {
	return crypto.randomUUID();
}
