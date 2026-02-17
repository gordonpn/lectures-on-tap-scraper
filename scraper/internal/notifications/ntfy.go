package notifications

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gordonpn/lectures-on-tap-scraper/internal/metrics"
)

type NtfyNotifier struct {
	client   *http.Client
	topicURL string
	token    string
	metrics  *metrics.Metrics
}

func NewNtfyNotifier(client *http.Client, topicURL, token string, m *metrics.Metrics) *NtfyNotifier {
	return &NtfyNotifier{client: client, topicURL: strings.TrimSpace(topicURL), token: strings.TrimSpace(token), metrics: m}
}

func (n *NtfyNotifier) Name() string {
	return "ntfy"
}

func (n *NtfyNotifier) Notify(ctx context.Context, note Notification) error {
	if err := n.publish(ctx, n.topicURL, note.Body); err != nil {
		return err
	}

	stateSlug := stateTopicSlug(note.State)
	if stateSlug == "" {
		if strings.TrimSpace(note.State) != "" {
			log.Printf("skipping state-specific ntfy publish for event %s: derived empty state slug", note.EventID)
		}
		return nil
	}

	base := strings.TrimSuffix(n.topicURL, "-")
	stateTopicURL := fmt.Sprintf("%s-%s", base, stateSlug)
	if err := n.publish(ctx, stateTopicURL, note.Body); err != nil {
		return fmt.Errorf("state-specific publish failed for state=%s: %w", strings.ToLower(strings.TrimSpace(note.State)), err)
	}
	return nil
}

func (n *NtfyNotifier) publish(ctx context.Context, topicURL, msg string) error {
	log.Printf("publishing notification to ntfy topic=%s (message size: %d bytes)", topicURL, len(msg))

	const maxAttempts = 5
	baseDelay := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, "POST", topicURL, bytes.NewBufferString(msg))
		if n.token != "" {
			req.Header.Set("Authorization", "Bearer "+n.token)
		}
		req.Header.Set("Priority", "max")

		startTime := time.Now()
		resp, err := n.client.Do(req)
		elapsed := time.Since(startTime)

		if err != nil {
			log.Printf("error posting to ntfy (attempt %d/%d): %v", attempt, maxAttempts, err)
			n.metrics.RecordNtfyPublish(elapsed, err)
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
			n.metrics.RecordNtfyPublish(elapsed, fmt.Errorf("rate limited"))
			if attempt == maxAttempts {
				return fmt.Errorf("ntfy rate limited after %d attempts: %s", maxAttempts, string(body))
			}
			time.Sleep(wait)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := fmt.Errorf("ntfy status %d: %s", resp.StatusCode, string(body))
			log.Printf("error response from ntfy: %v", err)
			n.metrics.RecordNtfyPublish(elapsed, err)
			return err
		}

		n.metrics.RecordNtfyPublish(elapsed, nil)
		log.Printf("ntfy publish ok | topic=%s | bytes=%d | msg=%s", topicURL, len(msg), msg)
		return nil
	}

	return fmt.Errorf("ntfy publish failed after %d attempts", maxAttempts)
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
