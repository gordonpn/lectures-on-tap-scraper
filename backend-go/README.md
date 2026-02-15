# backend-go

Go implementation of the notifications backend API.

## Environment

Required:

- `DATABASE_URL`
- `HUB_UI_CODE`
- `HUB_SECRET`
- `VAPID_PUBLIC_KEY`
- `VAPID_PRIVATE_KEY`
- `VAPID_SUBJECT` (or `HUB_PUBLIC_ORIGIN`)

Optional:

- `PORT` (default `4000`)
- `WORKER_COUNT` (default `10`)
- `QUEUE_SIZE` (default `1024`)
- `MAX_RETRIES` (default `3`)
- `RETRY_BASE_BACKOFF_MS` (default `400`)
- `PUSH_TTL_SECONDS` (default `1209600`)
- `SUBSCRIBE_RATE_LIMIT` (default `5`)
- `SUBSCRIBE_RATE_WINDOW_SECONDS` (default `60`)

Generate VAPID keys once and keep them in environment variables. Never regenerate keys on boot.

## Endpoints

- `POST /api/subscribe`
- `POST /api/unsubscribe`
- `GET /api/subscriptions/me`
- `POST /api/trigger-self` (requires `ui_code`, for self-test)
- `POST /api/trigger` (requires `X-Hub-Secret`)
- `GET /healthz`

## Run

```bash
go mod tidy
go run .
```

## Database migration

Apply:

`migrations/0001_create_push_subscriptions.sql`
