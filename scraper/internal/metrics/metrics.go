package metrics

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

// Metrics holds all Prometheus metrics for the notifier using the idiomatic batch pattern.
// All execution metrics are recorded at the end of each run to avoid zombie/stale metrics in the Pushgateway.
type Metrics struct {
	// Execution and status metrics
	LastSuccessTimestamp   prometheus.Gauge
	LastRunSuccess         prometheus.Gauge
	LastRunDurationSeconds  prometheus.Gauge
	ExecutionDurationSecs   prometheus.Histogram

	// Event processing volume metrics for the last run
	LastRunItemsProcessed        prometheus.Gauge
	LastRunItemsAvailable        prometheus.Gauge
	LastRunItemsNotified         prometheus.Gauge
	LastRunItemsDeduplicated     prometheus.Gauge
	LastRunItemsSoldOut          prometheus.Gauge
	LastRunItemsWithoutStartTime prometheus.Gauge

	// Redis metrics for the last run
	LastRunRedisConnectionErrors  prometheus.Gauge
	LastRunRedisOperationErrors   prometheus.Gauge
	LastRunRedisConnectionRetries prometheus.Histogram

	// API and external service metrics for the last run
	LastRunEventBriteFetchErrors       prometheus.Gauge
	LastRunEventBriteFetchDurationSecs prometheus.Histogram
	LastRunEventBritePagesFetched      prometheus.Gauge

	LastRunNtfyPublishErrors       prometheus.Gauge
	LastRunNtfyPublishDurationSecs prometheus.Histogram
	LastRunNtfyPublishes           prometheus.Gauge

	registry *prometheus.Registry
	pusher   *push.Pusher
}

// NewMetrics creates a new Metrics instance configured with the batch job metrics.
func NewMetrics(pushgatewayURL, jobName string) *Metrics {
	m := &Metrics{
		LastSuccessTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful execution",
		}),
		LastRunSuccess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_success",
			Help: "Result of the last execution: 1 for success, 0 for failure",
		}),
		LastRunDurationSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_duration_seconds",
			Help: "Duration of the last execution in seconds",
		}),
		ExecutionDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "scraper_execution_duration_seconds",
			Help:    "Execution duration distribution across runs",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}),

		LastRunItemsProcessed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_items_processed_total",
			Help: "Total number of events processed in the last execution",
		}),
		LastRunItemsAvailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_items_available_total",
			Help: "Number of events with available tickets in the last execution",
		}),
		LastRunItemsNotified: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_items_notified_total",
			Help: "Number of events notified in the last execution",
		}),
		LastRunItemsDeduplicated: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_items_deduplicated_total",
			Help: "Number of events deduplicated in the last execution",
		}),
		LastRunItemsSoldOut: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_items_sold_out_total",
			Help: "Number of events sold out in the last execution",
		}),
		LastRunItemsWithoutStartTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_items_without_start_time_total",
			Help: "Number of events without start time in the last execution",
		}),

		LastRunRedisConnectionErrors: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_redis_connection_errors_total",
			Help: "Number of Redis connection errors in the last execution",
		}),
		LastRunRedisOperationErrors: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_redis_operation_errors_total",
			Help: "Number of Redis operation errors in the last execution",
		}),
		LastRunRedisConnectionRetries: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "scraper_last_run_redis_connection_retries",
			Help:    "Distribution of Redis connection retry counts",
			Buckets: []float64{1, 2, 3, 5},
		}),

		LastRunEventBriteFetchErrors: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_eventbrite_fetch_errors_total",
			Help: "Number of EventBrite API fetch errors in the last execution",
		}),
		LastRunEventBriteFetchDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "scraper_last_run_eventbrite_fetch_duration_seconds",
			Help:    "Distribution of EventBrite page fetch duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		}),
		LastRunEventBritePagesFetched: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_eventbrite_pages_fetched_total",
			Help: "Number of EventBrite API pages fetched in the last execution",
		}),

		LastRunNtfyPublishErrors: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_ntfy_publish_errors_total",
			Help: "Number of ntfy publish errors in the last execution",
		}),
		LastRunNtfyPublishDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "scraper_last_run_ntfy_publish_duration_seconds",
			Help:    "Distribution of ntfy publish duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5},
		}),
		LastRunNtfyPublishes: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "scraper_last_run_ntfy_publishes_total",
			Help: "Number of successful ntfy publishes in the last execution",
		}),
	}

	m.registry = prometheus.NewRegistry()
	m.registry.MustRegister(
		m.LastSuccessTimestamp,
		m.LastRunSuccess,
		m.LastRunDurationSeconds,
		m.ExecutionDurationSecs,
		m.LastRunItemsProcessed,
		m.LastRunItemsAvailable,
		m.LastRunItemsNotified,
		m.LastRunItemsDeduplicated,
		m.LastRunItemsSoldOut,
		m.LastRunItemsWithoutStartTime,
		m.LastRunRedisConnectionErrors,
		m.LastRunRedisOperationErrors,
		m.LastRunRedisConnectionRetries,
		m.LastRunEventBriteFetchErrors,
		m.LastRunEventBriteFetchDurationSecs,
		m.LastRunEventBritePagesFetched,
		m.LastRunNtfyPublishErrors,
		m.LastRunNtfyPublishDurationSecs,
		m.LastRunNtfyPublishes,
	)

	// Set up pusher if URL is provided
	if pushgatewayURL != "" && jobName != "" {
		m.pusher = push.New(pushgatewayURL, jobName).
			Gatherer(m.registry)
	}

	return m
}

// RecordExecutionStart records the start of an execution.
func (m *Metrics) RecordExecutionStart(ctx context.Context) {
	if m == nil {
		return
	}
	m.LastRunSuccess.Set(0)
	log.Printf("metrics: execution started")
}

// RecordExecutionSuccess records a successful execution.
func (m *Metrics) RecordExecutionSuccess(ctx context.Context, duration time.Duration) {
	if m == nil {
		return
	}
	m.LastRunSuccess.Set(1)
	m.LastSuccessTimestamp.SetToCurrentTime()
	m.LastRunDurationSeconds.Set(duration.Seconds())
	m.ExecutionDurationSecs.Observe(duration.Seconds())
	log.Printf("metrics: execution successful (duration: %v)", duration)
}

// RecordExecutionFailure records a failed execution.
func (m *Metrics) RecordExecutionFailure(ctx context.Context, duration time.Duration, errorMsg string) {
	if m == nil {
		return
	}
	m.LastRunSuccess.Set(0)
	m.LastRunDurationSeconds.Set(duration.Seconds())
	m.ExecutionDurationSecs.Observe(duration.Seconds())
	log.Printf("metrics: execution failed (duration: %v, error: %s)", duration, errorMsg)
}

// RecordEventsProcessed records the number of events processed.
func (m *Metrics) RecordEventsProcessed(count int) {
	if m == nil {
		return
	}
	m.LastRunItemsProcessed.Add(float64(count))
}

// RecordEventsAvailable records events with available tickets.
func (m *Metrics) RecordEventsAvailable(count int) {
	if m == nil {
		return
	}
	m.LastRunItemsAvailable.Add(float64(count))
}

// RecordEventNotified records an event that was notified.
func (m *Metrics) RecordEventNotified() {
	if m == nil {
		return
	}
	m.LastRunItemsNotified.Inc()
}

// RecordEventDeduplicated records an event that was deduplicated.
func (m *Metrics) RecordEventDeduplicated() {
	if m == nil {
		return
	}
	m.LastRunItemsDeduplicated.Inc()
}

// RecordEventSoldOut records an event that became sold out.
func (m *Metrics) RecordEventSoldOut() {
	if m == nil {
		return
	}
	m.LastRunItemsSoldOut.Inc()
}

// RecordEventWithoutStartTime records an event without start time.
func (m *Metrics) RecordEventWithoutStartTime() {
	if m == nil {
		return
	}
	m.LastRunItemsWithoutStartTime.Inc()
}

// RecordEventBriteFetch records an EventBrite fetch operation.
func (m *Metrics) RecordEventBriteFetch(duration time.Duration, err error) {
	if m == nil {
		return
	}
	if duration > 0 {
		m.LastRunEventBriteFetchDurationSecs.Observe(duration.Seconds())
	}
	if err != nil {
		m.LastRunEventBriteFetchErrors.Inc()
		log.Printf("metrics: EventBrite fetch error: %v", err)
	}
	m.LastRunEventBritePagesFetched.Inc()
}

// RecordEventBriteFetchPageDuration records duration for fetching a specific page.
func (m *Metrics) RecordEventBriteFetchPageDuration(duration time.Duration) {
	if m == nil {
		return
	}
	m.LastRunEventBriteFetchDurationSecs.Observe(duration.Seconds())
	m.LastRunEventBritePagesFetched.Inc()
}

// RecordNtfyPublish records an ntfy publish operation.
func (m *Metrics) RecordNtfyPublish(duration time.Duration, err error) {
	if m == nil {
		return
	}
	m.LastRunNtfyPublishDurationSecs.Observe(duration.Seconds())
	if err != nil {
		m.LastRunNtfyPublishErrors.Inc()
		log.Printf("metrics: ntfy publish error: %v", err)
	} else {
		m.LastRunNtfyPublishes.Inc()
	}
}

// RecordRedisConnectionError records a Redis connection error.
func (m *Metrics) RecordRedisConnectionError() {
	if m == nil {
		return
	}
	m.LastRunRedisConnectionErrors.Inc()
}

// RecordRedisOperationError records a Redis operation error.
func (m *Metrics) RecordRedisOperationError() {
	if m == nil {
		return
	}
	m.LastRunRedisOperationErrors.Inc()
}

// RecordRedisConnectionRetries records the number of retries for Redis connection.
func (m *Metrics) RecordRedisConnectionRetries(attempts int) {
	if m == nil {
		return
	}
	m.LastRunRedisConnectionRetries.Observe(float64(attempts))
}

// Push pushes all metrics to the Pushgateway.
func (m *Metrics) Push(ctx context.Context) error {
	if m == nil || m.pusher == nil {
		return nil
	}

	log.Printf("pushing metrics to Pushgateway")
	if err := m.pusher.PushContext(ctx); err != nil {
		log.Printf("metrics: failed to push to Pushgateway: %v", err)
		return fmt.Errorf("failed to push metrics to Pushgateway: %w", err)
	}
	log.Printf("metrics: successfully pushed to Pushgateway")
	return nil
}

// InitializeMetricsFromEnv creates and configures metrics from environment variables.
func InitializeMetricsFromEnv(isLocal bool) *Metrics {
	if isLocal {
		log.Printf("metrics: running in local mode, Pushgateway disabled")
		return NewMetrics("", "")
	}

	pushgatewayURL := os.Getenv("PROMETHEUS_PUSHGATEWAY_URL")
	if pushgatewayURL == "" {
		log.Printf("metrics: PROMETHEUS_PUSHGATEWAY_URL not set, metrics collection enabled but push disabled")
		return NewMetrics("", "")
	}

	jobName := os.Getenv("PROMETHEUS_JOB_NAME")
	if jobName == "" {
		jobName = "lectures-notifier"
	}

	hostname, _ := os.Hostname()
	groupingKey := os.Getenv("PROMETHEUS_GROUPING_KEY")
	if groupingKey == "" {
		groupingKey = hostname
	}

	log.Printf("metrics: Pushgateway URL: %s, Job: %s, Instance: %s", pushgatewayURL, jobName, groupingKey)
	m := NewMetrics(pushgatewayURL, jobName)

	if groupingKey != "" {
		m.pusher = m.pusher.Grouping("instance", groupingKey)
	}

	return m
}
