# Lectures on Tap Scraper

A Go-based scraper and notifier for "Lectures on Tap" events, designed to run as a scheduled task (CronJob). It fetches live events from EventBrite, filters for ticket availability, and sends notifications via `ntfy` and/or `Discord`.

## Architecture & Technologies

- **Language:** Go 1.25.5
- **Scraper:** Fetches events from EventBrite API.
- **Deduplication:** Uses **Redis** to track notified events and avoid duplicate alerts.
- **Observability:**
  - **Metrics:** Prometheus metrics pushed to a **Pushgateway**.
  - **Healthchecks:** Integration with **healthchecks.io** for monitoring execution status.
  - **Dashboards:** Tooling included to generate Grafana dashboards using the Grafana Foundation SDK.
- **Infrastructure:**
  - **Kubernetes:** Manifests for CronJobs and Secrets.
  - **Docker:** Dockerfile and Docker Compose stack for local development.

## Project Structure

- `scraper/cmd/lectures-notifier/`: Main entry point for the scraper/notifier.
- `scraper/cmd/grafana-dashboard/`: Tool to generate Grafana dashboard JSON.
- `scraper/internal/metrics/`: Prometheus metrics collection and Pushgateway integration.
- `scraper/internal/notifications/`: Modular notification system supporting `ntfy` and `Discord`.
- `scraper/k8s/`: Kubernetes CronJob manifests.
- `scripts/`: Helper scripts for Kubernetes secret management and smoke testing.
- `Taskfile.yml`: Root and scraper-specific task definitions.

## Building and Running

The project uses `task` for orchestration.

### Prerequisites

- Copy `.env.example` to `.env` and fill in the required EventBrite and notification credentials.

### Local Development

- **Build:** `task scraper:build`
- **Run:** `task scraper:run` (Runs the notifier once)
- **Local Stack:** `task docker:up` (Starts Redis, ntfy, and the notifier via Docker Compose)
- **Generate Dashboard:** `task scraper:grafana:dashboard:generate`

### Kubernetes

- **Create Secret:** `task scraper:k8s:create-secret`
- **Smoke Test:** `task scraper:k8s:docker-desktop-smoke` (Tests CronJob on local Docker Desktop K8s)
- **Deploy:** `task scraper:k8s:deploy-cronjobs`

## Development Conventions

- **Logic Separation:** Core logic is located in `scraper/internal/`. Use `internal/` packages for shared code.
- **Environment Variables:** All configuration is driven by environment variables. See `.env.example` for available options.
- **Logging:** Use the standard `log` package with `LstdFlags | Lshortfile`.
- **Metrics:** Instrument new features using the `internal/metrics` package. Ensure metrics are pushed via `metricsClient.Push(ctx)` before the application exits.
- **Task-Based Workflow:** Always use `Taskfile.yml` for common commands to ensure consistency across environments.
- **Error Handling:** Implement retries for external services (Redis, EventBrite) as seen in `main.go`.
- **Iterative Development:** Make granular commits as you progress through tasks, ensuring each commit follows the [Conventional Commits](https://www.conventionalcommits.org/) specification.
- **Project Context:** Always reference other Markdown files in the repository (e.g., `README.md`) to maintain a comprehensive understanding of the project's goals and history.
- **Project Instructions:** Proactively update `GEMINI.md` as the project evolves, ensuring architectural decisions, new workflows, and updated conventions are documented for future sessions.

## Reliability Post-Mortem (BOS-MTL Outage)

A multi-site outage (Internet failure in Boston) revealed several architectural vulnerabilities in the project:

- **Aggressive Redis Retries:** The `main.go` Redis connection logic attempts 10 retries with exponential backoff (totalling ~34 minutes). In a cross-site outage, this causes the pod to hang.
- **Concurrency Deadlock:** The Kubernetes `concurrencyPolicy: Forbid` in `cronjob.yaml` prevents new jobs from starting while a previous job is hung in the Redis retry loop, leading to a complete system stall.
- **Lack of Global Timeout:** The application lacks a top-level `context.WithTimeout`, meaning it can run indefinitely if multiple external services (Redis, ntfy, EventBrite) hit their individual timeouts/retries sequentially.
- **Synchronous Bottlenecks:** Notifications are published synchronously. If the notification service is slow or unreachable, it further delays the completion of the job and the success ping to Healthchecks.io.
- **Healthcheck Brittleness:** The "success" signal is only sent at the absolute end of execution. Any hang in the middle results in a false "DOWN" alert even if the node is technically healthy.
