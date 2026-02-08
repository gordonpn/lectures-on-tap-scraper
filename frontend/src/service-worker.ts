/// <reference lib="webworker" />

const sw = self as unknown as ServiceWorkerGlobalScope;

sw.addEventListener('install', (event) => {
	sw.skipWaiting();
	event.waitUntil(Promise.resolve());
});

sw.addEventListener('activate', (event) => {
	event.waitUntil(sw.clients.claim());
});

sw.addEventListener('push', (event) => {
	let payload = { title: 'Lectures on Tap', body: 'New update available.', url: '/' };

	if (event.data) {
		try {
			payload = event.data.json();
		} catch {
			payload.body = event.data.text();
		}
	}

	const options = {
		body: payload.body,
		data: { url: payload.url || '/' },
		icon: '/icons/icon-192.svg',
		badge: '/icons/icon-192.svg',
		requireInteraction: true,
		tag: 'notification-hub'
	};

	const recordPromise = caches
		.open('push-debug')
		.then((cache) =>
			cache.put(
				'/__last_push',
				new Response(
					JSON.stringify({
						at: Date.now(),
						payload
					})
				)
			)
		)
		.catch(() => Promise.resolve());

	event.waitUntil(
		Promise.all([
			sw.registration.showNotification(payload.title || 'Notification Hub', options),
			recordPromise
		])
	);
});

sw.addEventListener('notificationclick', (event) => {
	event.notification.close();
	const url = event.notification.data?.url || '/';

	event.waitUntil(
		sw.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
			for (const client of clients) {
				if ('focus' in client && client.url.includes(url)) {
					return client.focus();
				}
			}

			if (sw.clients.openWindow) {
				return sw.clients.openWindow(url);
			}
		})
	);
});
