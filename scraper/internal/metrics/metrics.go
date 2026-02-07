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

// Metrics holds all Prometheus metrics for the notifier
type Metrics struct {
	// Execution tracking
	ExecutionStartTime     prometheus.Counter
	ExecutionSuccess       prometheus.Counter
	ExecutionFailure       prometheus.Counter
	ExecutionDurationSecs  prometheus.Histogram
	LastExecutionTimestamp prometheus.Gauge

	// Event processing metrics
	EventsProcessedTotal        prometheus.Counter
	EventsAvailableTotal        prometheus.Counter
	EventsNotifiedTotal         prometheus.Counter
	EventsDeduplicatedTotal     prometheus.Counter
	EventsSoldOutTotal          prometheus.Counter
	EventsWithoutStartTimeTotal prometheus.Counter

	// API and external service metrics
	EventBriteFetchErrorsTotal  prometheus.Counter
	EventBriteFetchDurationSecs prometheus.Histogram
	EventBritePagesFetchedTotal prometheus.Counter

	NtfyPublishErrorsTotal  prometheus.Counter
	NtfyPublishDurationSecs prometheus.Histogram
	NtfyPublishesTotal      prometheus.Counter

	// Redis metrics
	RedisConnectionErrorsTotal prometheus.Counter
	RedisOperationErrorsTotal  prometheus.Counter
	RedisConnectionRetries     prometheus.Histogram

	// Error tracking
	ErrorsTotal prometheus.Counter

	// Last run info
	LastRunStatus    prometheus.Gauge // 0 = unknown, 1 = success, 2 = failure
	LastErrorMessage prometheus.Gauge

	registry *prometheus.Registry
	pusher   *push.Pusher
}

// NewMetrics creates a new Metrics instance
func NewMetrics(pushgatewayURL, jobName string) *Metrics {
	m := &Metrics{
		ExecutionStartTime: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_execution_start_total",
			Help: "Total number of times the notifier started execution",
		}),
		ExecutionSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_execution_success_total",
			Help: "Total number of successful notifier executions",
		}),
		ExecutionFailure: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_execution_failure_total",
			Help: "Total number of failed notifier executions",
		}),
		ExecutionDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "lectures_notifier_execution_duration_seconds",
			Help:    "Duration of notifier execution in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
		}),
		LastExecutionTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lectures_notifier_last_execution_timestamp_seconds",
			Help: "Unix timestamp of the last execution",
		}),

		// Event processing
		EventsProcessedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_events_processed_total",
			Help: "Total number of events processed from EventBrite",
		}),
		EventsAvailableTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_events_available_total",
			Help: "Total number of events with available tickets",
		}),
		EventsNotifiedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_events_notified_total",
			Help: "Total number of events that were notified",
		}),
		EventsDeduplicatedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_events_deduplicated_total",
			Help: "Total number of events skipped due to deduplication",
		}),
		EventsSoldOutTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_events_sold_out_total",
			Help: "Total number of events that became sold out",
		}),
		EventsWithoutStartTimeTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_events_without_start_time_total",
			Help: "Total number of events missing start time information",
		}),

		// EventBrite API
		EventBriteFetchErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_eventbrite_fetch_errors_total",
			Help: "Total number of EventBrite fetch errors",
		}),
		EventBriteFetchDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "lectures_notifier_eventbrite_fetch_duration_seconds",
			Help:    "Duration of EventBrite fetch requests in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		}),
		EventBritePagesFetchedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_eventbrite_pages_fetched_total",
			Help: "Total number of EventBrite API pages fetched",
		}),

		// Ntfy notifications
		NtfyPublishErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_ntfy_publish_errors_total",
			Help: "Total number of ntfy publish errors",
		}),
		NtfyPublishDurationSecs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "lectures_notifier_ntfy_publish_duration_seconds",
			Help:    "Duration of ntfy publish requests in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		}),
		NtfyPublishesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_ntfy_publishes_total",
			Help: "Total number of successful ntfy publishes",
		}),

		// Redis
		RedisConnectionErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_redis_connection_errors_total",
			Help: "Total number of Redis connection errors",
		}),
		RedisOperationErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_redis_operation_errors_total",
			Help: "Total number of Redis operation errors (SetNX, Delete, etc.)",
		}),
		RedisConnectionRetries: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "lectures_notifier_redis_connection_retries",
			Help:    "Number of attempts to establish Redis connection",
			Buckets: []float64{1, 2, 3, 5, 10},
		}),

		// General errors
		ErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lectures_notifier_errors_total",
			Help: "Total number of errors encountered",
		}),

		// Last run status
		LastRunStatus: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lectures_notifier_last_run_status",
			Help: "Last run status: 0=unknown, 1=success, 2=failure",
		}),
		LastErrorMessage: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lectures_notifier_last_error_message_code",
			Help: "Last error code for debugging (hash of error message)",
		}),
	}

	m.registry = prometheus.NewRegistry()
	m.registry.MustRegister(
		m.ExecutionStartTime,
		m.ExecutionSuccess,
		m.ExecutionFailure,
		m.ExecutionDurationSecs,
		m.LastExecutionTimestamp,
		m.EventsProcessedTotal,
		m.EventsAvailableTotal,
		m.EventsNotifiedTotal,
		m.EventsDeduplicatedTotal,
		m.EventsSoldOutTotal,
		m.EventsWithoutStartTimeTotal,
		m.EventBriteFetchErrorsTotal,
		m.EventBriteFetchDurationSecs,
		m.EventBritePagesFetchedTotal,
		m.NtfyPublishErrorsTotal,
		m.NtfyPublishDurationSecs,
		m.NtfyPublishesTotal,
		m.RedisConnectionErrorsTotal,
		m.RedisOperationErrorsTotal,
		m.RedisConnectionRetries,
		m.ErrorsTotal,
		m.LastRunStatus,
		m.LastErrorMessage,
	)

	// Set up pusher if URL is provided
	if pushgatewayURL != "" && jobName != "" {
		m.pusher = push.New(pushgatewayURL, jobName).
			Gatherer(m.registry)
	}

	return m
}

// RecordExecutionStart records the start of an execution
func (m *Metrics) RecordExecutionStart(ctx context.Context) {
	if m == nil {
		return
	}
	m.ExecutionStartTime.Inc()
	m.LastExecutionTimestamp.SetToCurrentTime()
	log.Printf("metrics: execution started")
}

// RecordExecutionSuccess records a successful execution
func (m *Metrics) RecordExecutionSuccess(ctx context.Context, duration time.Duration) {
	if m == nil {
		return
	}
	m.ExecutionSuccess.Inc()
	m.ExecutionDurationSecs.Observe(duration.Seconds())
	m.LastRunStatus.Set(1)
	m.LastExecutionTimestamp.SetToCurrentTime()
	log.Printf("metrics: execution successful (duration: %v)", duration)
}

// RecordExecutionFailure records a failed execution
func (m *Metrics) RecordExecutionFailure(ctx context.Context, duration time.Duration, errorMsg string) {
	if m == nil {
		return
	}
	m.ExecutionFailure.Inc()
	m.ExecutionDurationSecs.Observe(duration.Seconds())
	m.LastRunStatus.Set(2)
	m.LastExecutionTimestamp.SetToCurrentTime()
	m.ErrorsTotal.Inc()
	m.LastErrorMessage.Set(float64(hashString(errorMsg)))
	log.Printf("metrics: execution failed (duration: %v, error: %s)", duration, errorMsg)
}

// RecordEventsProcessed records the number of events processed
func (m *Metrics) RecordEventsProcessed(count int) {
	if m == nil {
		return
	}
	for i := 0; i < count; i++ {
		m.EventsProcessedTotal.Inc()
	}
}

// RecordEventsAvailable records events with available tickets
func (m *Metrics) RecordEventsAvailable(count int) {
	if m == nil {
		return
	}
	for i := 0; i < count; i++ {
		m.EventsAvailableTotal.Inc()
	}
}

// RecordEventNotified records an event that was notified
func (m *Metrics) RecordEventNotified() {
	if m == nil {
		return
	}
	m.EventsNotifiedTotal.Inc()
}

// RecordEventDeduplicated records an event that was deduplicated
func (m *Metrics) RecordEventDeduplicated() {
	if m == nil {
		return
	}
	m.EventsDeduplicatedTotal.Inc()
}

// RecordEventSoldOut records an event that became sold out
func (m *Metrics) RecordEventSoldOut() {
	if m == nil {
		return
	}
	m.EventsSoldOutTotal.Inc()
}

// RecordEventWithoutStartTime records an event without start time
func (m *Metrics) RecordEventWithoutStartTime() {
	if m == nil {
		return
	}
	m.EventsWithoutStartTimeTotal.Inc()
}

// RecordEventBriteFetch records an EventBrite fetch operation
func (m *Metrics) RecordEventBriteFetch(duration time.Duration, err error) {
	if m == nil {
		return
	}
	m.EventBriteFetchDurationSecs.Observe(duration.Seconds())
	if err != nil {
		m.EventBriteFetchErrorsTotal.Inc()
		m.ErrorsTotal.Inc()
		log.Printf("metrics: EventBrite fetch error recorded")
	}
	m.EventBritePagesFetchedTotal.Inc()
}

// RecordEventBriteFetchPageDuration records duration for fetching a specific page
func (m *Metrics) RecordEventBriteFetchPageDuration(duration time.Duration) {
	if m == nil {
		return
	}
	m.EventBriteFetchDurationSecs.Observe(duration.Seconds())
	m.EventBritePagesFetchedTotal.Inc()
}

// RecordNtfyPublish records an ntfy publish operation
func (m *Metrics) RecordNtfyPublish(duration time.Duration, err error) {
	if m == nil {
		return
	}
	m.NtfyPublishDurationSecs.Observe(duration.Seconds())
	if err != nil {
		m.NtfyPublishErrorsTotal.Inc()
		m.ErrorsTotal.Inc()
		log.Printf("metrics: ntfy publish error recorded")
	} else {
		m.NtfyPublishesTotal.Inc()
	}
}

// RecordRedisConnectionError records a Redis connection error
func (m *Metrics) RecordRedisConnectionError() {
	if m == nil {
		return
	}
	m.RedisConnectionErrorsTotal.Inc()
	m.ErrorsTotal.Inc()
	log.Printf("metrics: Redis connection error recorded")
}

// RecordRedisOperationError records a Redis operation error
func (m *Metrics) RecordRedisOperationError() {
	if m == nil {
		return
	}
	m.RedisOperationErrorsTotal.Inc()
	m.ErrorsTotal.Inc()
	log.Printf("metrics: Redis operation error recorded")
}

// RecordRedisConnectionRetries records the number of retries for Redis connection
func (m *Metrics) RecordRedisConnectionRetries(attempts int) {
	if m == nil {
		return
	}
	m.RedisConnectionRetries.Observe(float64(attempts))
	log.Printf("metrics: Redis connection retries recorded (attempts: %d)", attempts)
}

// Push pushes all metrics to the Pushgateway
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

// hashString creates a simple hash of a string for error tracking
func hashString(s string) uint64 {
	h := uint64(5381)
	for _, c := range s {
		h = ((h << 5) + h) + uint64(c)
	}
	return h
}

// InitializeMetricsFromEnv creates and configures metrics from environment variables
func InitializeMetricsFromEnv(isLocal bool) *Metrics {
	if isLocal {
		log.Printf("metrics: running in local mode, Pushgateway disabled")
		return NewMetrics("", "")
	}

	pushgatewayURL := os.Getenv("PROMETHEUS_PUSHGATEWAY_URL")
	if pushgatewayURL == "" {
		log.Printf("metrics: PROMETHEUS_PUSHGATEWAY_URL not set, metrics collection enabled but push disabled")
		// Still create metrics for collection, but don't push
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

	// Add instance label if configured
	if groupingKey != "" {
		m.pusher = m.pusher.Grouping("instance", groupingKey)
	}

	return m
}
