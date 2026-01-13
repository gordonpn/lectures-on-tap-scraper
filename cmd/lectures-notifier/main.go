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

	"github.com/redis/go-redis/v9"
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
			log.Println("redis dedupe disabled: REDIS_ADDR not set")
		}
		return nil
	}
	password := os.Getenv("REDIS_PASSWORD")
	log.Printf("redis dedupe enabled at %s", addr)
	return redis.NewClient(&redis.Options{Addr: addr, Password: password})
}

func fetchAllLiveEvents(client *http.Client, orgID, token string) ([]event, error) {
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

			if err == nil {
				break
			}
			if attempt < maxRetries {
				waitTime := time.Duration(1<<uint(attempt-1)) * time.Second
				log.Printf("error making request to EventBrite (attempt %d): %v, retrying in %v", attempt, err, waitTime)
				time.Sleep(waitTime)
			} else {
				log.Printf("error making request to EventBrite after %d attempts: %v", maxRetries, err)
				return nil, err
			}
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := fmt.Errorf("eventbrite status %d: %s", resp.StatusCode, string(body))
			log.Printf("error response from EventBrite: %v", err)
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
			log.Println("no more pages available")
			break
		}
		page++
	}

	log.Printf("successfully fetched all %d live events", len(all))
	return all, nil
}

func publishNtfy(client *http.Client, topicURL, msg, token string) error {
	log.Printf("publishing notification to ntfy topic (message size: %d bytes)", len(msg))
	req, _ := http.NewRequest("POST", topicURL, bytes.NewBufferString(msg))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		log.Println("bearer token added to ntfy request")
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("error posting to ntfy: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("ntfy status %d: %s", resp.StatusCode, string(b))
		log.Printf("error response from ntfy: %v", err)
		return err
	}
	return nil
}

func main() {
	log.Println("starting lectures-notifier")
	isLocal := os.Getenv("NTFY_TOPIC_URL") == ""
	if isLocal {
		log.Println("running in local mode")
	} else {
		log.Println("running in production mode")
		sleepDuration := time.Duration(rand.Intn(41)+10) * time.Second
		log.Printf("sleeping for %v before proceeding", sleepDuration)
		time.Sleep(sleepDuration)
	}

	log.Println("loading configuration from environment variables")
	orgID := mustEnv("EVENTBRITE_ORGANIZER_ID")
	token := mustEnv("EVENTBRITE_TOKEN")
	log.Printf("loaded organizer ID: %s", orgID)

	var ntfyTopicURL, ntfyToken string
	if !isLocal {
		ntfyTopicURL = mustEnv("NTFY_TOPIC_URL")
		log.Printf("loaded ntfy topic URL: %s", ntfyTopicURL)

		// Token required for production, optional for local/docker-compose
		isLocalNtfy := strings.Contains(ntfyTopicURL, "localhost") || strings.Contains(ntfyTopicURL, "ntfy:80")
		if isLocalNtfy {
			ntfyToken = strings.TrimSpace(os.Getenv("NTFY_TOKEN"))
			if ntfyToken != "" {
				log.Println("ntfy bearer token configured")
			} else {
				log.Println("ntfy bearer token not set (optional for local ntfy)")
			}
		} else {
			ntfyToken = mustEnv("NTFY_TOKEN")
			log.Println("ntfy bearer token configured")
		}
	}

	httpClient := &http.Client{Timeout: 45 * time.Second}
	all, err := fetchAllLiveEvents(httpClient, orgID, token)
	if err != nil {
		log.Fatalf("failed to fetch events: %v", err)
	}

	ctx := context.Background()
	redisClient := newRedisClient(isLocal)

	var dedupeCfg dedupeConfig
	if redisClient != nil {
		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Printf("redis ping failed, disabling dedupe: %v", err)
			redisClient = nil
		} else {
			dedupeCfg = dedupeConfig{
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
			log.Printf("redis dedupe config: maxTTL=%v reminderCooldown=%v deleteOnSoldOut=%v extraBuffer=%v minTTL=%v",
				dedupeCfg.ttlCap, dedupeCfg.reminderCooldown, dedupeCfg.deleteOnSoldOut, dedupeCfg.extraBuffer, dedupeCfg.minTTL)
		}
	}

	now := time.Now()
	var notifyEvents []event
	availableCount := 0

	for _, e := range all {
		redisKey := ""
		if redisClient != nil {
			redisKey = dedupeKey(e.ID)
		}

		available := isTicketsAvailable(e)
		if !available {
			if redisClient != nil && dedupeCfg.deleteOnSoldOut {
				deleted, err := redisClient.Del(ctx, redisKey).Result()
				if err != nil {
					log.Printf("redis delete failed for %s (event %s): %v", redisKey, e.ID, err)
				} else if deleted > 0 {
					log.Printf("redis deleted key %s for sold-out event %s (%s)", redisKey, e.ID, e.Name.Text)
				}
			}
			continue
		}

		availableCount++
		startTime, hasStart := parseEventStart(e)
		if hasStart && startTime.Before(now) {
			continue
		}

		shouldNotify := true
		if redisClient != nil {
			ttl := dedupeTTL(startTime, hasStart, dedupeCfg)
			set, err := redisClient.SetNX(ctx, redisKey, "1", ttl).Result()
			if err != nil {
				log.Printf("redis setnx failed for %s (event %s): %v (proceeding to notify)", redisKey, e.ID, err)
			} else if set {
				log.Printf("redis set key %s with TTL %v for event %s (%s)", redisKey, ttl, e.ID, e.Name.Text)
			} else {
				log.Printf("redis dedupe skip: key %s already exists for event %s (%s)", redisKey, e.ID, e.Name.Text)
				shouldNotify = false
			}
		}

		if shouldNotify {
			notifyEvents = append(notifyEvents, e)
		}
	}

	log.Printf("found %d events with available tickets (%d new)", availableCount, len(notifyEvents))
	if len(notifyEvents) == 0 {
		log.Println("no new events to notify, exiting")
		return
	}

	for _, e := range notifyEvents {
		var timeStr string
		if len(e.Start.Local) >= len("2006-01-02T15:04:05") {
			t, err := time.Parse("2006-01-02T15:04:05", e.Start.Local)
			if err == nil {
				timeStr = t.Format("Mon, Jan 2 at 15:04")
			}
		}
		city := ""
		if e.Venue != nil {
			city = e.Venue.Address.City
		}
		msg := fmt.Sprintf("%s %s (%s) %s", city, e.Name.Text, timeStr, e.URL)

		if isLocal {
			log.Println("local mode: printing message to stdout")
			log.Println(msg)
		} else {
			if err := publishNtfy(httpClient, ntfyTopicURL, msg, ntfyToken); err != nil {
				log.Printf("failed to publish notification for event %s: %v", e.ID, err)
				continue
			}
			log.Printf("notification published successfully for event %s (%d bytes)", e.ID, len(msg))
		}
	}
}
