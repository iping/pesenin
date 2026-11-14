import { error } from '@sveltejs/kit';
import { publicGet } from '$lib/server/public';
import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params, url }) => {
	const page = await publicGet('/api/v1/public/batches/' + encodeURIComponent(params.code));
	if (!page) error(404, 'PO tidak ketemu');
	const shop = {
		brand: page.brand,
		store: page.store,
		batches: [page.batch],
		charges: page.charges,
		ordering: page.ordering,
		statusPath: page.statusPath,
		serverNow: page.serverNow
	};
	return { shop, origin: url.origin, page };
};
