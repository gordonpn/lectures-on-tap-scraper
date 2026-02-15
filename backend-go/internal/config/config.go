package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               string
	DatabaseURL        string
	HubUICode          string
	HubSecret          string
	VAPIDPublicKey     string
	VAPIDPrivateKey    string
	VAPIDSubject       string
	WorkerCount        int
	QueueSize          int
	MaxRetries         int
	RetryBaseBackoffMS int
	TTLSeconds         int

	SubscribeRateLimit int
	SubscribeWindow    time.Duration
}

func Load() (Config, error) {
	config := Config{
		Port:               getEnv("PORT", "4000"),
		DatabaseURL:        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		HubUICode:          strings.TrimSpace(os.Getenv("HUB_UI_CODE")),
		HubSecret:          strings.TrimSpace(os.Getenv("HUB_SECRET")),
		VAPIDPublicKey:     strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
		VAPIDPrivateKey:    strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
		VAPIDSubject:       strings.TrimSpace(firstNonEmpty(os.Getenv("VAPID_SUBJECT"), os.Getenv("HUB_PUBLIC_ORIGIN"))),
		WorkerCount:        getEnvInt("WORKER_COUNT", 10),
		QueueSize:          getEnvInt("QUEUE_SIZE", 1024),
		MaxRetries:         getEnvInt("MAX_RETRIES", 3),
		RetryBaseBackoffMS: getEnvInt("RETRY_BASE_BACKOFF_MS", 400),
		TTLSeconds:         getEnvInt("PUSH_TTL_SECONDS", 60*60*24*14),
		SubscribeRateLimit: getEnvInt("SUBSCRIBE_RATE_LIMIT", 5),
		SubscribeWindow:    time.Duration(getEnvInt("SUBSCRIBE_RATE_WINDOW_SECONDS", 60)) * time.Second,
	}

	if config.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if config.WorkerCount < 1 {
		config.WorkerCount = 1
	}
	if config.QueueSize < 1 {
		config.QueueSize = 128
	}

	return config, nil
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
