# Notification Hub

A small PWA + Phoenix API for managing a single-user push-notification hub.

## Quick start (local)

1. Copy env file and fill secrets:

```sh
cp .env.example .env
```

2. Start dependencies:

```sh
task docker:up
```

This uses the root docker-compose.yml file.

3. Run the API:

```sh
task dev:backend
```

4. Run the frontend dev server (optional):

```sh
task dev:frontend
```

Phoenix runs at http://localhost:4000 and serves built PWA assets from `backend/priv/static`.

## Build the PWA for Phoenix

```sh
task build
```

This outputs a static SPA into `backend/priv/static` and then digests assets.

## Configuration

Required env vars:

- `HUB_PUBLIC_ORIGIN` (e.g. `https://lectures.gordon-pn.com`)
- `HUB_UI_CODE` (single-user access code)
- `HUB_SECRET` (shared secret for `/api/trigger`)
- `VAPID_PUBLIC_KEY`
- `VAPID_PRIVATE_KEY`
- `VITE_VAPID_PUBLIC_KEY` (for the frontend bundle)
- `SECRET_KEY_BASE`
- `DATABASE_URL`

Optional:

- `VAPID_SUBJECT` (defaults to `HUB_PUBLIC_ORIGIN`)
- `REDIS_URL` or `REDIS_ADDR` (for `/api/latest-scrape`)

Generate VAPID keys once and store them in env vars (never regenerate on boot). For example:

`VITE_VAPID_PUBLIC_KEY` should match `VAPID_PUBLIC_KEY` so the browser can subscribe.

```sh
npx web-push generate-vapid-keys
```

## API

- `POST /api/subscribe` accepts `{ subscription, topic, ui_code }`
- `POST /api/unsubscribe` accepts `{ endpoint }`
- `GET /api/subscriptions/me?endpoint=...`
- `POST /api/trigger` (requires `X-Hub-Secret`)
- `POST /api/trigger-self` (requires `ui_code`, for UI test button)
- `GET /api/latest-scrape`
- `GET /healthz`

## Production (k3s)

These manifests are sample starting points. Set secrets via K3s secrets and point
`DATABASE_URL` to your external Postgres instance.

- `k8s/deployment.yaml`
- `k8s/service.yaml`
- `k8s/ingress.yaml`
