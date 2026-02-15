package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	limit  int
	window time.Duration

	mu       sync.Mutex
	attempts map[string][]time.Time
}

func New(limit int, window time.Duration) *Limiter {
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}

	return &Limiter{
		limit:    limit,
		window:   window,
		attempts: make(map[string][]time.Time),
	}
}

func (limiter *Limiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-limiter.window)

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	recent := limiter.attempts[key]
	pruned := recent[:0]
	for _, timestamp := range recent {
		if timestamp.After(cutoff) {
			pruned = append(pruned, timestamp)
		}
	}

	if len(pruned) >= limiter.limit {
		limiter.attempts[key] = pruned
		return false
	}

	limiter.attempts[key] = append(pruned, now)
	return true
}
