package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gordonpn/lectures-on-tap-scraper/internal/metrics"
	"github.com/gordonpn/lectures-on-tap-scraper/internal/notifications"
	"github.com/redis/go-redis/v9"
)

const (
	maxRedisAttempts = 3
	redisBaseDelay   = 1 * time.Second
)

type ebResp struct {
	Events     []event `json:"events"`
	Pagination struct {
		HasMoreItems bool `json:"has_more_items"`
		PageCount    int  `json:"page_count"`
	} `json:"pagination"`
}

type event struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Name struct {
		Text string `json:"text"`
	} `json:"name"`
	Start struct {
		Local string `json:"local"` // "YYYY-MM-DDTHH:MM:SS"
	} `json:"start"`
	Venue *struct {
		Address struct {
			Address1                string `json:"address_1"`
			Address2                string `json:"address_2"`
			City                    string `json:"city"`
			Region                  string `json:"region"`
			LocalizedAddressDisplay string `json:"localized_address_display"`
			PostalCode              string `json:"postal_code"`
		} `json:"address"`
	} `json:"venue"`
	TicketAvailability *struct {
		HasAvailableTickets *bool `json:"has_available_tickets"`
	} `json:"ticket_availability"`
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func mustEnv(k string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		log.Fatalf("missing env var: %s", k)
	}
	return v
}

func isTicketsAvailable(e event) bool {
	if e.TicketAvailability == nil || e.TicketAvailability.HasAvailableTickets == nil {
		return false
	}
	return *e.TicketAvailability.HasAvailableTickets
}

func envBool(key string, defaultVal bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return defaultVal
	}
}

func envDurationHours(key string, defaultVal time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	h, err := strconv.Atoi(v)
	if err != nil || h <= 0 {
		return defaultVal
	}
	return time.Duration(h) * time.Hour
}

func parseEventStart(e event) (time.Time, bool) {
	if len(e.Start.Local) < len("2006-01-02T15:04:05") {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02T15:04:05", e.Start.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

type dedupeConfig struct {
	ttlCap           time.Duration
	reminderCooldown time.Duration
	deleteOnSoldOut  bool
	extraBuffer      time.Duration
	minTTL           time.Duration
}

func dedupeKey(eventID string) string {
	return "lot:event:" + eventID + ":notified"
}

func dedupeTTL(start time.Time, hasStart bool, cfg dedupeConfig) time.Duration {
	ttl := cfg.ttlCap
	if hasStart {
		ttl = time.Until(start) + cfg.extraBuffer
		if ttl < cfg.minTTL {
			ttl = cfg.minTTL
		}
		if ttl > cfg.ttlCap {
			ttl = cfg.ttlCap
		}
	}
	if cfg.reminderCooldown > 0 && ttl > cfg.reminderCooldown {
		ttl = cfg.reminderCooldown
	}
	return ttl
}

func newRedisClient(isLocal bool) *redis.Client {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		if !isLocal {
			log.Printf("redis dedupe disabled: REDIS_ADDR not set (isLocal=%t)", isLocal)
		}
		return nil
	}
	password := os.Getenv("REDIS_PASSWORD")
	log.Printf("redis dedupe enabled at %s", addr)
	return redis.NewClient(&redis.Options{Addr: addr, Password: password})
}

// retryRedisConnection attempts to establish and verify a Redis connection with extensive retries
func retryRedisConnection(ctx context.Context, redisClient *redis.Client, maxAttempts int, baseDelay time.Duration, m *metrics.Metrics) (*redis.Client, error) {
	if redisClient == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := redisClient.Ping(ctx).Err()
		if err == nil {
			log.Printf("redis ping successful on attempt %d/%d", attempt, maxAttempts)
			m.RecordRedisConnectionRetries(attempt)
			return redisClient, nil
		}

		log.Printf("redis ping failed (attempt %d/%d): %v", attempt, maxAttempts, err)
		m.RecordRedisConnectionError()

		if attempt < maxAttempts {
			// Calculate exponential backoff with jitter
			backoff := baseDelay * time.Duration(1<<uint(attempt-1))
			jitter := time.Duration(rand.Int63n(int64(baseDelay)))
			wait := backoff + jitter

			log.Printf("waiting %v before retry (attempt %d/%d)", wait, attempt, maxAttempts)
			time.Sleep(wait)
		}
	}

	return nil, fmt.Errorf("redis connection failed after %d attempts", maxAttempts)
}

func fetchPage(ctx context.Context, client *http.Client, orgID, token string, page int, m *metrics.Metrics) ([]event, int, error) {
	url := fmt.Sprintf(
		"https://www.eventbriteapi.com/v3/organizers/%s/events/?status=live&expand=venue,ticket_availability&page=%d",
		orgID, page,
	)
	log.Printf("fetching page %d from EventBrite", page)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	var resp *http.Response
	var err error
	maxRetries := 4
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		startTime := time.Now()
		resp, err = client.Do(req)
		elapsed := time.Since(startTime)
		log.Printf("EventBrite request attempt %d for page %d took %v", attempt, page, elapsed)
		m.RecordEventBriteFetchPageDuration(elapsed)

		if err == nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				break
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			err = fmt.Errorf("eventbrite status %d: %s", resp.StatusCode, string(body))

			if resp.StatusCode != 429 && (resp.StatusCode >= 400 && resp.StatusCode < 500) {
				log.Printf("permanent error from EventBrite for page %d: %v", page, err)
				m.RecordEventBriteFetch(0, err)
				return nil, 0, err
			}
		}

		if attempt < maxRetries {
			waitTime := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("error making request to EventBrite for page %d (attempt %d): %v, retrying in %v", page, attempt, err, waitTime)
			
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(waitTime):
			}
		} else {
			log.Printf("error making request to EventBrite for page %d after %d attempts: %v", page, maxRetries, err)
			m.RecordEventBriteFetch(0, err)
			return nil, 0, err
		}
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Printf("error reading EventBrite response body for page %d: %v", page, err)
		return nil, 0, err
	}

	var r ebResp
	if err := json.Unmarshal(body, &r); err != nil {
		log.Printf("error parsing EventBrite response for page %d: %v", page, err)
		return nil, 0, err
	}

	return r.Events, r.Pagination.PageCount, nil
}

func fetchAllLiveEvents(ctx context.Context, client *http.Client, orgID, token string, m *metrics.Metrics) ([]event, error) {
	log.Printf("starting to fetch live events from EventBrite for organizer %s", orgID)
	
	firstPageEvents, pageCount, err := fetchPage(ctx, client, orgID, token, 1, m)
	if err != nil {
		return nil, err
	}
	
	log.Printf("fetched %d events from page 1, total pages: %d", len(firstPageEvents), pageCount)
	all := firstPageEvents

	if pageCount > 1 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		errs := make(chan error, pageCount-1)

		for p := 2; p <= pageCount; p++ {
			wg.Add(1)
			go func(page int) {
				defer wg.Done()
				events, _, fetchErr := fetchPage(ctx, client, orgID, token, page, m)
				if fetchErr != nil {
					errs <- fetchErr
					return
				}
				mu.Lock()
				all = append(all, events...)
				mu.Unlock()
				log.Printf("fetched %d events from page %d", len(events), page)
			}(p)
		}
		
		wg.Wait()
		close(errs)

		if len(errs) > 0 {
			return nil, <-errs // Return the first error encountered
		}
	}

	log.Printf("successfully fetched all %d live events", len(all))
	return all, nil
}

type appConfig struct {
	isLocal             bool
	orgID               string
	token               string
	ntfyTopicURL        string
	ntfyToken           string
	discordEnabled      bool
	discordWebhookURL   string
	healthchecksPingURL string
}

func logModeAndSleep(isLocal bool) {
	if isLocal {
		log.Printf("running in local mode (isLocal=%t)", isLocal)
		return
	}
	log.Printf("running in production mode (isLocal=%t)", isLocal)
	sleepDuration := time.Duration(rand.Intn(41)+10) * time.Second
	log.Printf("sleeping for %v before proceeding", sleepDuration)
	time.Sleep(sleepDuration)
}

func loadConfig(isLocal bool) appConfig {
	cfg := appConfig{isLocal: isLocal}
	log.Printf("loading configuration from environment variables (isLocal=%t)", isLocal)
	cfg.orgID = mustEnv("EVENTBRITE_ORGANIZER_ID")
	cfg.token = mustEnv("EVENTBRITE_TOKEN")
	log.Printf("loaded organizer ID: %s", cfg.orgID)

	cfg.healthchecksPingURL = strings.TrimSpace(os.Getenv("HEALTHCHECKS_PING_URL"))
	if cfg.healthchecksPingURL != "" {
		log.Printf("healthchecks ping URL configured")
	}

	if isLocal {
		return cfg
	}

	cfg.ntfyTopicURL = mustEnv("NTFY_TOPIC_URL")
	log.Printf("loaded ntfy topic URL: %s", cfg.ntfyTopicURL)

	// Token required for production, optional for local/docker-compose
	isLocalNtfy := strings.Contains(cfg.ntfyTopicURL, "localhost") || strings.Contains(cfg.ntfyTopicURL, "ntfy:80")
	if isLocalNtfy {
		cfg.ntfyToken = strings.TrimSpace(os.Getenv("NTFY_TOKEN"))
		if cfg.ntfyToken != "" {
			log.Printf("ntfy bearer token configured (localNtfy=%t)", isLocalNtfy)
		} else {
			log.Printf("ntfy bearer token not set (optional for local ntfy, localNtfy=%t)", isLocalNtfy)
		}
	} else {
		cfg.ntfyToken = mustEnv("NTFY_TOKEN")
		log.Printf("ntfy bearer token configured (localNtfy=%t)", isLocalNtfy)
	}

	cfg.discordEnabled = envBool("ENABLE_DISCORD_NOTIFIER", false)
	if cfg.discordEnabled {
		cfg.discordWebhookURL = mustEnv("DISCORD_WEBHOOK_URL")
		log.Printf("discord notifier enabled")
	}

	return cfg
}

func pingHealthchecks(ctx context.Context, client *http.Client, baseURL, suffix string, maxRetries int) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return
	}
	if maxRetries < 1 {
		maxRetries = 1
	}
	url := strings.TrimRight(base, "/")
	if suffix != "" {
		url = url + "/" + suffix
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("healthchecks ping %q failed (attempt %d/%d): %v", suffix, attempt, maxRetries, err)
		} else {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
			log.Printf("healthchecks ping %q returned status %d (attempt %d/%d)", suffix, resp.StatusCode, attempt, maxRetries)
		}
		if attempt < maxRetries {
			wait := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("healthchecks ping %q retrying in %v", suffix, wait)
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}
}

func runNotifier(ctx context.Context, httpClient *http.Client, cfg appConfig, isLocal bool, m *metrics.Metrics) error {
	all, err := fetchAllLiveEvents(ctx, httpClient, cfg.orgID, cfg.token, m)
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}
	m.RecordEventsProcessed(len(all))

	redisClient, dedupeCfg := initRedis(ctx, isLocal, m)
	redisBroken := redisClient == nil && strings.TrimSpace(os.Getenv("REDIS_ADDR")) != ""

	var primaryNotifier notifications.Notifier
	var secondaryNotifiers []notifications.Notifier
	if !isLocal {
		primaryNotifier, secondaryNotifiers = buildNotifiers(httpClient, cfg, m)
	}

	now := time.Now()
	notifyEvents, availableCount := filterEvents(ctx, all, redisClient, dedupeCfg, now, m)
	m.RecordEventsAvailable(availableCount)

	log.Printf("found %d events with available tickets (%d new)", availableCount, len(notifyEvents))
	if len(notifyEvents) == 0 {
		log.Printf("no new events to notify, exiting (availableCount=%d)", availableCount)
		return nil
	}

	for _, e := range notifyEvents {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("notifier stopped early: %w", err)
		}

		if !redisBroken && redisClient == nil {
			redisClient = ensureRedisForNotification(ctx, isLocal, redisClient, m)
			if redisClient == nil {
				redisBroken = true
			}
		}

		msg := formatEventMessage(e)
		if isLocal {
			log.Printf("local mode: printing message to stdout (event=%s bytes=%d)", e.ID, len(msg))
			log.Println(msg)
			continue
		}
		publishEventNotifications(ctx, primaryNotifier, secondaryNotifiers, e, msg, m)
	}

	return nil
}

func buildDedupeConfig() dedupeConfig {
	dedupeCfg := dedupeConfig{
		ttlCap:           envDurationHours("DEDUP_MAX_TTL_HOURS", 14*24*time.Hour),
		reminderCooldown: envDurationHours("DEDUP_REMINDER_HOURS", 0),
		deleteOnSoldOut:  envBool("DEDUP_DELETE_ON_SOLD_OUT", true),
		extraBuffer:      envDurationHours("DEDUP_EXTRA_BUFFER_HOURS", time.Hour),
		minTTL:           envDurationHours("DEDUP_MIN_TTL_HOURS", time.Hour),
	}
	if dedupeCfg.ttlCap <= 0 {
		dedupeCfg.ttlCap = 14 * 24 * time.Hour
	}
	if dedupeCfg.minTTL <= 0 {
		dedupeCfg.minTTL = time.Hour
	}
	return dedupeCfg
}

func initRedis(ctx context.Context, isLocal bool, m *metrics.Metrics) (*redis.Client, dedupeConfig) {
	redisClient := newRedisClient(isLocal)
	if redisClient == nil {
		return nil, dedupeConfig{}
	}

	log.Printf("attempting to establish redis connection with extensive retries (maxAttempts=%d baseDelay=%v)", maxRedisAttempts, redisBaseDelay)
	verifiedClient, err := retryRedisConnection(ctx, redisClient, maxRedisAttempts, redisBaseDelay, m)
	if err != nil {
		log.Printf("redis connection failed after extensive retries, disabling dedupe: %v", err)
		return nil, dedupeConfig{}
	}

	dedupeCfg := buildDedupeConfig()
	log.Printf("redis dedupe config: maxTTL=%v reminderCooldown=%v deleteOnSoldOut=%v extraBuffer=%v minTTL=%v",
		dedupeCfg.ttlCap, dedupeCfg.reminderCooldown, dedupeCfg.deleteOnSoldOut, dedupeCfg.extraBuffer, dedupeCfg.minTTL)
	return verifiedClient, dedupeCfg
}

func filterEvents(ctx context.Context, events []event, redisClient *redis.Client, dedupeCfg dedupeConfig, now time.Time, m *metrics.Metrics) ([]event, int) {
	var notifyEvents []event
	availableCount := 0

	for _, e := range events {
		redisKey := ""
		if redisClient != nil {
			redisKey = dedupeKey(e.ID)
		}

		available := isTicketsAvailable(e)
		if !available {
			m.RecordEventSoldOut()
			if redisClient != nil && dedupeCfg.deleteOnSoldOut {
				deleted, err := redisClient.Del(ctx, redisKey).Result()
				if err != nil {
					log.Printf("redis delete failed for %s (event %s): %v", redisKey, e.ID, err)
					m.RecordRedisOperationError()
				} else if deleted > 0 {
					log.Printf("redis deleted key %s for sold-out event %s (%s)", redisKey, e.ID, e.Name.Text)
				}
			}
			continue
		}

		availableCount++
		startTime, hasStart := parseEventStart(e)
		if !hasStart {
			m.RecordEventWithoutStartTime()
		}
		if hasStart && startTime.Before(now) {
			continue
		}

		shouldNotify := true
		if redisClient != nil {
			ttl := dedupeTTL(startTime, hasStart, dedupeCfg)
			set, err := redisClient.SetNX(ctx, redisKey, "1", ttl).Result()
			if err != nil {
				log.Printf("redis setnx failed for %s (event %s): %v (proceeding to notify)", redisKey, e.ID, err)
				m.RecordRedisOperationError()
			} else if set {
				log.Printf("redis set key %s with TTL %v for event %s (%s)", redisKey, ttl, e.ID, e.Name.Text)
			} else {
				log.Printf("redis dedupe skip: key %s already exists for event %s (%s)", redisKey, e.ID, e.Name.Text)
				shouldNotify = false
				m.RecordEventDeduplicated()
			}
		}

		if shouldNotify {
			notifyEvents = append(notifyEvents, e)
		}
	}

	return notifyEvents, availableCount
}

func ensureRedisForNotification(ctx context.Context, isLocal bool, redisClient *redis.Client, m *metrics.Metrics) *redis.Client {
	if redisClient != nil {
		return redisClient
	}
	log.Printf("redis unavailable, attempting reconnection before sending notification")
	tempClient := newRedisClient(isLocal)
	if tempClient == nil {
		log.Printf("redis still unavailable, skipping notification")
		m.RecordRedisConnectionError()
		return nil
	}
	verifiedClient, err := retryRedisConnection(ctx, tempClient, maxRedisAttempts, redisBaseDelay, m)
	if err != nil {
		log.Printf("redis reconnection failed, proceeding without dedupe: %v", err)
		return nil
	}
	log.Printf("redis connection restored; continuing with notifications")
	return verifiedClient
}

func formatEventMessage(e event) string {
	timeStr := ""
	if t, ok := parseEventStart(e); ok {
		timeStr = t.Format("Mon, Jan 2 at 15:04")
	}
	city := ""
	if e.Venue != nil {
		city = e.Venue.Address.City
	}
	return fmt.Sprintf("%s %s (%s) %s", city, e.Name.Text, timeStr, e.URL)
}

func buildNotifiers(httpClient *http.Client, cfg appConfig, m *metrics.Metrics) (notifications.Notifier, []notifications.Notifier) {
	primary := notifications.NewNtfyNotifier(httpClient, cfg.ntfyTopicURL, cfg.ntfyToken, m)
	var secondary []notifications.Notifier
	if cfg.discordEnabled {
		secondary = append(secondary, notifications.NewDiscordNotifier(httpClient, cfg.discordWebhookURL))
	}
	return primary, secondary
}

func publishEventNotifications(ctx context.Context, primary notifications.Notifier, secondary []notifications.Notifier, e event, msg string, m *metrics.Metrics) {
	state := ""
	if e.Venue != nil {
		state = e.Venue.Address.Region
	}
	n := notifications.Notification{EventID: e.ID, Body: msg, State: state, URL: strings.TrimSpace(e.URL)}

	allNotifiers := append([]notifications.Notifier{primary}, secondary...)
	
	// Create a wait group to wait for all notifications for this event
	var wg sync.WaitGroup
	for _, notifier := range allNotifiers {
		wg.Add(1)
		go func(ntf notifications.Notifier) {
			defer wg.Done()
			if err := ntf.Notify(ctx, n); err != nil {
				log.Printf("failed to publish notification via %s for event %s: %v", ntf.Name(), e.ID, err)
			} else if ntf.Name() == primary.Name() {
				// Record metrics only for primary (or maybe all, but following existing pattern)
				m.RecordEventNotified()
			}
		}(notifier)
	}
	wg.Wait()
}

func main() {
	log.Printf("starting lectures-notifier (pid=%d)", os.Getpid())
	isLocal := os.Getenv("NTFY_TOPIC_URL") == ""
	logModeAndSleep(isLocal)
	cfg := loadConfig(isLocal)
	httpClient := &http.Client{Timeout: 45 * time.Second}
	metricsClient := metrics.InitializeMetricsFromEnv(isLocal)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	ctx, timeoutCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer func() {
		timeoutCancel()
		cancel()
	}()

	startTime := time.Now()
	metricsClient.RecordExecutionStart(ctx)

	var runErr error
	var panicVal interface{}

	// Robust exit handler to catch panics/errors and push final execution status
	defer func() {
		duration := time.Since(startTime)
		if r := recover(); r != nil {
			panicVal = r
			runErr = fmt.Errorf("panic: %v", r)
		}

		// Use a separate context to ensure pushing and pinging occur even if main context expired
		reportCtx, reportCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer reportCancel()

		if runErr != nil {
			metricsClient.RecordExecutionFailure(reportCtx, duration, runErr.Error())
			_ = metricsClient.Push(reportCtx)
			pingHealthchecks(reportCtx, httpClient, cfg.healthchecksPingURL, "fail", 3)

			if panicVal != nil {
				log.Fatalf("notifier panicked: %v", panicVal)
			} else {
				log.Fatalf("notifier run failed: %v", runErr)
			}
		} else {
			metricsClient.RecordExecutionSuccess(reportCtx, duration)
			_ = metricsClient.Push(reportCtx)
			pingHealthchecks(reportCtx, httpClient, cfg.healthchecksPingURL, "", 3)
		}
	}()

	pingHealthchecks(ctx, httpClient, cfg.healthchecksPingURL, "start", 3)
	runErr = runNotifier(ctx, httpClient, cfg, isLocal, metricsClient)
}
