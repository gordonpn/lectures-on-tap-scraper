# Lectures on Tap scraper and notifier

Go scraper/notifier for Lectures on Tap events, with optional local `redis` + `ntfy` services.

![healthchecks.io](https://healthchecks.io/badge/63a82bd5-b473-430b-9745-22843eff864e/ckgqM2Fm-2.svg)

Subscription links:

- https://ntfy.gordon-pn.com/lectures-on-tap
- https://ntfy.gordon-pn.com/lectures-on-tap-ca
- https://ntfy.gordon-pn.com/lectures-on-tap-il
- https://ntfy.gordon-pn.com/lectures-on-tap-ma
- https://ntfy.gordon-pn.com/lectures-on-tap-ny

## Quick start (local)

1. Copy env file and set required values:

```sh
cp .env.example .env
```

2. Build and run the notifier directly:

```sh
task scraper:build
task scraper:run
```

3. Or start the Docker Compose stack (redis, ntfy, notifier):

```sh
task docker:up
```

Stop services:

```sh
task docker:down
```

## Kubernetes tasks

From repo root:

```sh
task scraper:k8s:create-secret
task scraper:k8s:docker-desktop-smoke
```

Optional: create the secret directly:

```sh
./scripts/k8s-create-secret.sh
```

## Local CronJob smoke test on Docker Desktop

Docker Desktop ships an upstream Kubernetes cluster that works fine for CronJob testing. With `kubectl` installed and Docker Desktop Kubernetes enabled:

```bash
task scraper:k8s:docker-desktop-smoke
```

Make sure Docker Desktop Kubernetes is enabled (Settings > Kubernetes). To use another cluster, override `CTX`.

What it does:
- switches context to the cluster
- creates/updates `lectures-notifier-secrets` in the namespace from `.env`
- applies everything under `k8s/` to `NAMESPACE`
- patches all CronJobs to a 1-minute schedule (limits job history to 1)
- patches container image to your `IMAGE`
- creates one ad-hoc Job from the first CronJob (override with `CJ_NAME=<your-cronjob>`)
- waits for pod readiness and tails Job logs

Requirements:
- `.env` file in the repository root with `EVENTBRITE_ORGANIZER_ID`, `EVENTBRITE_TOKEN`, and `NTFY_TOPIC_URL`
- Optional Discord destination (disabled by default): set `ENABLE_DISCORD_NOTIFIER=true` and `DISCORD_WEBHOOK_URL`
- Optional: `HEALTHCHECKS_PING_URL` to enable Healthchecks start/success/fail pings
- `lectures-notifier:main` image available locally (build with `docker build -t lectures-notifier:main scraper`) or override with `IMAGE=<your-image>`

Useful overrides:
- `CTX=<cluster-name>` — target cluster context
- `NAMESPACE=<ns>` — target namespace
- `IMAGE=<image>` — container image tag (defaults to `lectures-notifier:main`)
- `CJ_NAME=<cronjob-name>` — which CronJob to trigger

## Todo

- [x] Handle redis rate limit
- [x] Split topics by cities
- [x] Redis client retries
- [x] Add health checks
- [x] Add metrics
