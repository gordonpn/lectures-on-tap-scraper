# Runbook: Lectures on Tap Scraper

This document outlines common failure modes, debugging steps, and operational procedures for the `lectures-notifier`.

## System Architecture
The system is a Go application deployed as a **Kubernetes CronJob**. It fetches data from EventBrite, deduplicates notifications using Redis, and sends alerts via `ntfy` and Discord.

## Monitoring & Alerts

### Healthchecks.io
We use [healthchecks.io](https://healthchecks.io) to monitor the execution of the CronJob.

*   **Signals Sent:**
    *   `/start`: Sent when the application begins execution.
    *   Success (no suffix): Sent when the application completes successfully.
*   **Removed Failure Signal:** We intentionally **do not** send a `/fail` signal. This allows the Kubernetes `restartPolicy: OnFailure` to retry transient errors without triggering false positive alerts. An alert is only triggered if the "Success" signal fails to arrive within the grace period.
*   **Configuration:**
    *   **Type:** Cron
    *   **Schedule:** `*/5 10-18 * * *` (Matches the K8s CronJob schedule)
    *   **Timezone:** `America/New_York`

### Metrics
The application pushes metrics to a Prometheus Pushgateway.
*   **Job Name:** `lectures-notifier`
*   **Key Metrics:**
    *   `events_processed_total`: Number of events fetched from EventBrite.
    *   `events_available_total`: Number of events with tickets available.
    *   `events_notified_total`: Number of notifications successfully sent.
    *   `eventbrite_fetch_duration_seconds`: Histogram of API response times.
    *   `redis_connection_errors_total`: Count of failed Redis connection attempts.

## Common Troubleshooting

### 1. Transient Alerts from Healthchecks.io
If you receive alerts that resolve quickly:
*   **Cause:** Usually transient network issues with EventBrite or Redis.
*   **Check:** Verify the pod logs in Kubernetes. Look for "retrying" messages.
*   **Action:** Ensure the Healthchecks.io schedule matches the CronJob schedule and has an appropriate grace period (e.g., 2 minutes).

### 2. TooManyMissedTimes / CronJob Lock-up
If the CronJob stops triggering and you see `Warning: TooManyMissedTimes` in `kubectl describe cronjob`:
*   **Cause:** Kubernetes stops scheduling a CronJob if it misses >100 runs. This often happens with restricted schedules (e.g., `10-18`) because the overnight gap exceeds the 100-missed-run threshold for a 5-minute schedule.
*   **Fix:** The `startingDeadlineSeconds: 300` setting in the CronJob spec limits the lookback window, preventing the controller from counting the overnight gap as missed runs.
*   **Action:** If the job is stuck, you may need to delete and recreate the CronJob manifest.

### 3. EventBrite API Errors
The scraper retries EventBrite requests up to 4 times with exponential backoff for:
*   HTTP 429 (Rate Limited)
*   HTTP 5xx (Server Error)

**Debugging:**
*   Check logs for `EventBrite request attempt X took ...` or `error response from EventBrite`.
*   If you see `permanent error from EventBrite: eventbrite status 401`, check if `EVENTBRITE_TOKEN` has expired or is invalid.

### 3. Redis / Deduplication Issues
If duplicate notifications are being sent, or if the logs show `redis connection failed`:
*   The system is designed to **fail open**. If Redis is unavailable after 10 retry attempts, the scraper will continue but will not deduplicate (i.e., it might send duplicate notifications).
*   **Action:** Check the status of the Redis service (`docker-compose` or K8s service).

### 4. Kubernetes Debugging
To check the status of the CronJob:

```bash
# List CronJobs
kubectl get cronjobs

# View history of recent Jobs
kubectl get jobs

# Check logs for the latest run
kubectl logs -l job-name=<job-name>
```

## Local Debugging

To run a single iteration locally with your current `.env` configuration:

```bash
task scraper:run
```

To simulate a Kubernetes run using Docker Desktop:

```bash
task scraper:k8s:docker-desktop-smoke
```

## Environment Variables
*   `HEALTHCHECKS_PING_URL`: The base URL for healthchecks.io pings.
*   `EVENTBRITE_TOKEN`: API token for EventBrite.
*   `REDIS_ADDR`: Address of the Redis instance.
*   `PUSHGATEWAY_URL`: URL for the Prometheus Pushgateway.
