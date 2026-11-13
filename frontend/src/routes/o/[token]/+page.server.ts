import { error } from '@sveltejs/kit';
import { publicGet } from '$lib/server/public';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params }) => {
	const order = await publicGet('/api/v1/public/orders/' + encodeURIComponent(params.token));
	if (!order) error(404, 'Pesanan tidak ketemu');
	return { order };
};
