package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gordonpn/lectures-on-tap-scraper/internal/metrics"
	"github.com/redis/go-redis/v9"
)

const (
	maxRedisAttempts = 10
	redisBaseDelay   = 2 * time.Second
)

type ebResp struct {
	Events     []event `json:"events"`
	Pagination struct {
		HasMoreItems bool `json:"has_more_items"`
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

func fetchAllLiveEvents(client *http.Client, orgID, token string, m *metrics.Metrics) ([]event, error) {
	log.Printf("starting to fetch live events from EventBrite for organizer %s", orgID)
	var all []event
	page := 1

	for {
		url := fmt.Sprintf(
			"https://www.eventbriteapi.com/v3/organizers/%s/events/?status=live&expand=venue,ticket_availability&page=%d",
			orgID, page,
		)
		log.Printf("fetching page %d from EventBrite", page)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		var resp *http.Response
		var err error
		maxRetries := 3
		for attempt := 1; attempt <= maxRetries; attempt++ {
			startTime := time.Now()
			resp, err = client.Do(req)
			elapsed := time.Since(startTime)
			log.Printf("EventBrite request attempt %d took %v", attempt, elapsed)
			m.RecordEventBriteFetchPageDuration(elapsed)

			if err == nil {
				break
			}
			if attempt < maxRetries {
				waitTime := time.Duration(1<<uint(attempt-1)) * time.Second
				log.Printf("error making request to EventBrite (attempt %d): %v, retrying in %v", attempt, err, waitTime)
				time.Sleep(waitTime)
			} else {
				log.Printf("error making request to EventBrite after %d attempts: %v", maxRetries, err)
				m.RecordEventBriteFetch(0, err)
				return nil, err
			}
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := fmt.Errorf("eventbrite status %d: %s", resp.StatusCode, string(body))
			log.Printf("error response from EventBrite: %v", err)
			m.RecordEventBriteFetch(0, err)
			return nil, err
		}

		var r ebResp
		if err := json.Unmarshal(body, &r); err != nil {
			log.Printf("error parsing EventBrite response: %v", err)
			return nil, err
		}

		log.Printf("fetched %d events from page %d", len(r.Events), page)
		all = append(all, r.Events...)
		if !r.Pagination.HasMoreItems {
			log.Printf("no more pages available (page=%d)", page)
			break
		}
		page++
	}

	log.Printf("successfully fetched all %d live events", len(all))
	return all, nil
}

func retryAfterDelay(header string, attempt int, base time.Duration) time.Duration {
	if header != "" {
		if secs, err := strconv.Atoi(header); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(header); err == nil {
			d := time.Until(t)
			if d > 0 {
				return d
			}
		}
	}

	backoff := base * time.Duration(1<<uint(attempt-1))
	jitter := time.Duration(rand.Int63n(int64(base)))
	return backoff + jitter
}

func publishNtfy(client *http.Client, topicURL, msg, token string, m *metrics.Metrics) error {
	log.Printf("publishing notification to ntfy topic (message size: %d bytes)", len(msg))

	const maxAttempts = 5
	baseDelay := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, _ := http.NewRequest("POST", topicURL, bytes.NewBufferString(msg))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Priority", "max")

		startTime := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(startTime)

		if err != nil {
			log.Printf("error posting to ntfy (attempt %d/%d): %v", attempt, maxAttempts, err)
			m.RecordNtfyPublish(elapsed, err)
			if attempt == maxAttempts {
				return err
			}
			wait := retryAfterDelay("", attempt, baseDelay)
			time.Sleep(wait)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfterDelay(resp.Header.Get("Retry-After"), attempt, baseDelay)
			log.Printf("ntfy rate limited (attempt %d/%d), waiting %v before retry: %s", attempt, maxAttempts, wait, string(body))
			m.RecordNtfyPublish(elapsed, fmt.Errorf("rate limited"))
			if attempt == maxAttempts {
				return fmt.Errorf("ntfy rate limited after %d attempts: %s", maxAttempts, string(body))
			}
			time.Sleep(wait)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := fmt.Errorf("ntfy status %d: %s", resp.StatusCode, string(body))
			log.Printf("error response from ntfy: %v", err)
			m.RecordNtfyPublish(elapsed, err)
			return err
		}

		m.RecordNtfyPublish(elapsed, nil)
		return nil
	}

	return fmt.Errorf("ntfy publish failed after %d attempts", maxAttempts)
}

type appConfig struct {
	isLocal             bool
	orgID               string
	token               string
	ntfyTopicURL        string
	ntfyToken           string
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
		return cfg
	}

	cfg.ntfyToken = mustEnv("NTFY_TOKEN")
	log.Printf("ntfy bearer token configured (localNtfy=%t)", isLocalNtfy)
	return cfg
}

func pingHealthchecks(client *http.Client, baseURL, suffix string) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return
	}
	url := strings.TrimRight(base, "/")
	if suffix != "" {
		url = url + "/" + suffix
	}

	req, _ := http.NewRequest("GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("healthchecks ping %q failed: %v", suffix, err)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("healthchecks ping %q returned status %d", suffix, resp.StatusCode)
	}
}

func runNotifier(httpClient *http.Client, cfg appConfig, isLocal bool, m *metrics.Metrics) error {
	all, err := fetchAllLiveEvents(httpClient, cfg.orgID, cfg.token, m)
	if err != nil {
		return fmt.Errorf("failed to fetch events: %w", err)
	}
	m.RecordEventsProcessed(len(all))

	ctx := context.Background()
	redisClient, dedupeCfg := initRedis(ctx, isLocal, m)
	now := time.Now()
	notifyEvents, availableCount := filterEvents(ctx, all, redisClient, dedupeCfg, now, m)
	m.RecordEventsAvailable(availableCount)

	log.Printf("found %d events with available tickets (%d new)", availableCount, len(notifyEvents))
	if len(notifyEvents) == 0 {
		log.Printf("no new events to notify, exiting (availableCount=%d)", availableCount)
		return nil
	}

	for _, e := range notifyEvents {
		redisClient = ensureRedisForNotification(ctx, isLocal, redisClient, m)
		if redisClient == nil {
			continue
		}
		msg := formatEventMessage(e)
		if isLocal {
			log.Printf("local mode: printing message to stdout (event=%s bytes=%d)", e.ID, len(msg))
			log.Println(msg)
			continue
		}
		publishEventNotifications(httpClient, cfg, e, msg, m)
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
		log.Printf("redis reconnection failed, skipping notification: %v", err)
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

func stateTopicSlug(state string) string {
	stateLower := strings.ToLower(strings.TrimSpace(state))
	if stateLower == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range stateLower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func publishEventNotifications(client *http.Client, cfg appConfig, e event, msg string, m *metrics.Metrics) {
	if err := publishNtfy(client, cfg.ntfyTopicURL, msg, cfg.ntfyToken, m); err != nil {
		log.Printf("failed to publish notification for event %s: %v", e.ID, err)
		return
	}
	m.RecordEventNotified()
	log.Printf("ntfy publish ok | topic=%s | bytes=%d | msg=%s", cfg.ntfyTopicURL, len(msg), msg)

	state := ""
	if e.Venue != nil {
		state = e.Venue.Address.Region
	}
	stateSlug := stateTopicSlug(state)
	if stateSlug == "" {
		if strings.TrimSpace(state) != "" {
			log.Printf("skipping state-specific publish for event %s: derived empty state slug", e.ID)
		}
		return
	}

	base := strings.TrimSuffix(cfg.ntfyTopicURL, "-")
	stateTopicURL := fmt.Sprintf("%s-%s", base, stateSlug)
	if err := publishNtfy(client, stateTopicURL, msg, cfg.ntfyToken, m); err != nil {
		log.Printf("failed to publish state-specific notification for event %s (state=%s): %v", e.ID, strings.ToLower(strings.TrimSpace(state)), err)
		return
	}
	log.Printf("ntfy publish ok | topic=%s | state=%s | bytes=%d | msg=%s", stateTopicURL, strings.ToLower(strings.TrimSpace(state)), len(msg), msg)
}

func main() {
	log.Printf("starting lectures-notifier (pid=%d)", os.Getpid())
	isLocal := os.Getenv("NTFY_TOPIC_URL") == ""
	logModeAndSleep(isLocal)
	cfg := loadConfig(isLocal)
	httpClient := &http.Client{Timeout: 45 * time.Second}
	metricsClient := metrics.InitializeMetricsFromEnv(isLocal)
	ctx := context.Background()
	startTime := time.Now()
	metricsClient.RecordExecutionStart(ctx)

	pingHealthchecks(httpClient, cfg.healthchecksPingURL, "start")
	if err := runNotifier(httpClient, cfg, isLocal, metricsClient); err != nil {
		pingHealthchecks(httpClient, cfg.healthchecksPingURL, "fail")
		metricsClient.RecordExecutionFailure(ctx, time.Since(startTime), err.Error())
		_ = metricsClient.Push(ctx)
		log.Fatalf("notifier run failed: %v", err)
	}
	metricsClient.RecordExecutionSuccess(ctx, time.Since(startTime))
	_ = metricsClient.Push(ctx)
	pingHealthchecks(httpClient, cfg.healthchecksPingURL, "")
}
