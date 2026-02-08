<script lang="ts">
	import { onMount } from 'svelte';
	const vapidPublicKey = import.meta.env.VITE_VAPID_PUBLIC_KEY ?? '';
	const accessCodeStorageKey = 'hub_access_code';

	let installed = false;
	let supportsPush = false;
	let accessCode = '';
	let status = 'inactive';
	let topics: string[] = [];
	let message = '';
	let endpoint = '';
	let lastScrape: unknown = null;
	let working = false;
	let permission = 'default';
	let lastPush: { at: number; payload: { title?: string; body?: string; url?: string } } | null =
		null;

	const isIos = () => /iphone|ipad|ipod/i.test(navigator.userAgent);
	const isStandalone = () =>
		window.matchMedia('(display-mode: standalone)').matches ||
		(window.navigator as any).standalone === true;

	onMount(async () => {
		installed = isStandalone();
		supportsPush = 'serviceWorker' in navigator && 'PushManager' in window;
		accessCode = localStorage.getItem(accessCodeStorageKey) ?? '';

		if (supportsPush) {
			await navigator.serviceWorker.register('/service-worker.js');
			await refreshStatus();
		}

		await fetchLatestScrape();
		await fetchLastPush();
	});

	async function refreshStatus() {
		const registration = await navigator.serviceWorker.ready;
		const subscription = await registration.pushManager.getSubscription();
		endpoint = subscription?.endpoint ?? '';
		permission = Notification.permission;

		if (!endpoint) {
			status = 'inactive';
			topics = [];
			return;
		}

		const response = await fetch(`/api/subscriptions/me?endpoint=${encodeURIComponent(endpoint)}`);
		if (response.ok) {
			const data = await response.json();
			status = data.status;
			topics = data.topics ?? [];
		}

		await fetchLastPush();
	}

	async function fetchLastPush() {
		if (!('caches' in window)) {
			return;
		}

		const cache = await caches.open('push-debug');
		const response = await cache.match('/__last_push');
		if (response) {
			lastPush = await response.json();
		}
	}

	async function subscribe() {
		message = '';
		working = true;
		localStorage.setItem(accessCodeStorageKey, accessCode);

		try {
			if (!installed) {
				message = 'Install the app to subscribe.';
				return;
			}

			if (!supportsPush) {
				message = 'Push is not supported in this browser.';
				return;
			}

			if (!vapidPublicKey) {
				message = 'Missing VAPID public key.';
				return;
			}

			permission = await Notification.requestPermission();
			if (permission !== 'granted') {
				message = 'Notifications were not granted.';
				return;
			}

			const registration = await navigator.serviceWorker.ready;
			let subscription = await registration.pushManager.getSubscription();

			if (!subscription) {
				subscription = await registration.pushManager.subscribe({
					userVisibleOnly: true,
					applicationServerKey: urlBase64ToUint8Array(vapidPublicKey)
				});
			}

			const response = await fetch('/api/subscribe', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify({
					subscription: subscription.toJSON(),
					topic: 'default',
					ui_code: accessCode
				})
			});

			if (!response.ok) {
				const error = await response.json();
				message = error.error ?? 'Failed to subscribe.';
				return;
			}

			message = 'Subscription updated.';
			await refreshStatus();
		} finally {
			working = false;
		}
	}

	async function localNotificationTest() {
		message = '';
		working = true;

		try {
			if (!installed) {
				message = 'Install the app to test notifications.';
				return;
			}

			const permissionResult = await Notification.requestPermission();
			permission = permissionResult;

			if (permissionResult !== 'granted') {
				message = 'Notifications were not granted.';
				return;
			}

			const registration = await navigator.serviceWorker.ready;
			await registration.showNotification('Local notification test', {
				body: 'This confirms iOS can display notifications.',
				icon: '/icons/icon-192.svg',
				badge: '/icons/icon-192.svg',
				tag: 'notification-hub-local'
			});
			message = 'Local notification displayed.';
			await fetchLastPush();
		} finally {
			working = false;
		}
	}

	async function testNotification() {
		message = '';
		working = true;
		localStorage.setItem(accessCodeStorageKey, accessCode);

		try {
			if (!installed) {
				message = 'Install the app to test notifications.';
				return;
			}

			if (!endpoint) {
				message = 'Subscribe first.';
				return;
			}

			const response = await fetch('/api/trigger-self', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify({
					endpoint,
					ui_code: accessCode
				})
			});

			if (!response.ok) {
				const error = await response.json();
				message = error.error ?? 'Failed to send test.';
				return;
			}

			message = 'Test notification queued.';
			await fetchLastPush();
		} finally {
			working = false;
		}
	}

	async function unsubscribe() {
		message = '';
		working = true;

		try {
			const registration = await navigator.serviceWorker.ready;
			const subscription = await registration.pushManager.getSubscription();
			const currentEndpoint = subscription?.endpoint ?? endpoint;

			if (!currentEndpoint) {
				message = 'No active subscription found.';
				return;
			}

			const response = await fetch('/api/unsubscribe', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify({ endpoint: currentEndpoint })
			});

			if (!response.ok) {
				const error = await response.json();
				message = error.error ?? 'Failed to unsubscribe.';
				return;
			}

			if (subscription) {
				await subscription.unsubscribe();
			}

			message = 'Unsubscribed.';
			await refreshStatus();
			await fetchLastPush();
		} finally {
			working = false;
		}
	}

	async function fetchLatestScrape() {
		const response = await fetch('/api/latest-scrape');
		if (response.ok) {
			lastScrape = await response.json();
		}
	}

	function urlBase64ToUint8Array(base64String: string) {
		const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
		const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
		const rawData = window.atob(base64);
		return Uint8Array.from([...rawData].map((char) => char.charCodeAt(0)));
	}

	function formatScrapeList(data: unknown) {
		if (Array.isArray(data)) return data;
		if (data && typeof data === 'object') {
			const maybeList = (data as Record<string, unknown>).items ||
				(data as Record<string, unknown>).deals ||
				(data as Record<string, unknown>).events;
			if (Array.isArray(maybeList)) return maybeList;
		}
		return null;
	}
</script>

<div class="app">
	<section class="hero">
		<h1>Notification Hub</h1>
		<p>
			Single-user push updates for your lectures scraper. Built as a PWA so iOS can
			receive notifications after Add to Home Screen.
		</p>

		{#if !installed}
			<p class="status">
				<span class="pill">Install required</span>
				{#if isIos()}
					Open Safari → Share → Add to Home Screen to enable push.
				{:else}
					Install this app to enable notifications.
				{/if}
			</p>
		{/if}

		{#if !supportsPush}
			<p class="status">This browser does not support Push APIs.</p>
		{/if}
	</section>

	<section class="grid">
		<div class="card">
			{#if installed}
				<div class="label">Access Code</div>
				<input
					class="input"
					type="password"
					bind:value={accessCode}
					placeholder="Enter Access Code"
				/>

				<div class="actions">
					<button class="button" on:click={subscribe} disabled={!supportsPush || working}>
						Subscribe to updates
					</button>
					<button class="button secondary" on:click={testNotification} disabled={working}>
						Test notification
					</button>
					<button class="button secondary" on:click={localNotificationTest} disabled={working}>
						Local notification test
					</button>
					<button
						class="button secondary"
						on:click={unsubscribe}
						disabled={working || status !== 'active'}
					>
						Unsubscribe
					</button>
				</div>

				<div class="status">
					Status: <strong>{status}</strong>
					{#if topics.length}
						<span class="monospace">topics: {topics.join(', ')}</span>
					{/if}
				</div>
				<div class="status">
					Permission: <strong>{permission}</strong>
					<span class="monospace">
						endpoint: {endpoint ? `${endpoint.slice(0, 42)}...` : 'none'}
					</span>
				</div>
				<div class="status">
					Last push:
					{#if lastPush}
						<strong>{new Date(lastPush.at).toLocaleString()}</strong>
					{:else}
						<strong>none yet</strong>
					{/if}
					<span class="monospace">
						{lastPush?.payload?.title ?? 'no title'}
					</span>
				</div>
				{#if message}
					<p class="status">{message}</p>
				{/if}
			{:else}
				<p class="status">Install the PWA to unlock subscriptions.</p>
			{/if}
		</div>

		<div class="card">
			<div class="label">Last known deals</div>
			{#if lastScrape}
				{#if formatScrapeList(lastScrape)}
					<ul class="list">
						{#each formatScrapeList(lastScrape) as item}
							<li>{typeof item === 'string' ? item : JSON.stringify(item)}</li>
						{/each}
					</ul>
				{:else}
					<pre class="monospace">{JSON.stringify(lastScrape, null, 2)}</pre>
				{/if}
			{:else}
				<p class="status">No scrape payload yet.</p>
			{/if}
		</div>
	</section>
</div>
