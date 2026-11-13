self.addEventListener('install', (event) => {
	event.waitUntil(
		caches.open('pesenin-shell-v3').then((cache) =>
			cache.addAll(['/favicon.ico', '/icon-192.png', '/icon-512.png', '/icon-maskable-192.png', '/icon-maskable-512.png', '/site.webmanifest'])
		)
	);
	self.skipWaiting();
});

self.addEventListener('activate', (event) => {
	event.waitUntil(
		caches
			.keys()
			.then((keys) => Promise.all(keys.filter((k) => k.startsWith('pesenin-shell-') && k !== 'pesenin-shell-v3').map((k) => caches.delete(k))))
			.then(() => self.clients.claim())
	);
});

self.addEventListener('fetch', (event) => {
	const url = new URL(event.request.url);
	if (event.request.method !== 'GET' || url.pathname.startsWith('/api')) return;
	event.respondWith(
		fetch(event.request).catch(() => caches.match(event.request).then((hit) => hit || caches.match('/icon-192.png')))
	);
});

self.addEventListener('push', (event) => {
	let data = { title: 'Pesenin', body: 'Ping!', url: '/app' };
	try {
		data = { ...data, ...event.data.json() };
	} catch {
		/* keep defaults */
	}
	event.waitUntil(self.registration.showNotification(data.title, { body: data.body, icon: '/icon-192.png', badge: '/favicon-32x32.png', data }));
});

self.addEventListener('notificationclick', (event) => {
	event.notification.close();
	const url = event.notification.data?.url || '/app';
	event.waitUntil(
		self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
			for (const client of clients) {
				if ('focus' in client) return client.focus();
			}
			return self.clients.openWindow(url);
		})
	);
});
