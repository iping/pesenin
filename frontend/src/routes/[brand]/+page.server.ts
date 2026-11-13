import { error } from '@sveltejs/kit';
import { publicGet } from '$lib/server/public';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params, url }) => {
	const shop = await publicGet('/api/v1/public/brands/' + encodeURIComponent(params.brand));
	if (!shop) error(404, 'Toko tidak ketemu');
	return { shop, origin: url.origin };
};
