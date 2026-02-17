# Lectures on Tap Scraper

Go scraper/notifier for Lectures on Tap events, with optional local `redis` + `ntfy` services.

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

## Kubernetes scripts

The scraper-specific Kubernetes manifests and helper scripts are in `scraper/k8s` and `scraper/scripts`.

From repo root, use namespaced tasks:

```sh
task scraper:k8s:create-secret
task scraper:k8s:docker-desktop-smoke
```

## More details

See `scraper/README.md` for full setup notes, topic URLs, and smoke-test workflow.
