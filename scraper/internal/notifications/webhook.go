package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type WebhookNotifier struct {
	client *http.Client
	url    string
	token  string
}

type webhookPayload struct {
	EventID string `json:"event_id"`
	Body    string `json:"body"`
	State   string `json:"state,omitempty"`
}

func NewWebhookNotifier(client *http.Client, url, token string) *WebhookNotifier {
	return &WebhookNotifier{client: client, url: strings.TrimSpace(url), token: strings.TrimSpace(token)}
}

func (w *WebhookNotifier) Name() string {
	return "webhook"
}

func (w *WebhookNotifier) Notify(ctx context.Context, n Notification) error {
	payload, err := json.Marshal(webhookPayload{EventID: n.EventID, Body: n.Body, State: strings.TrimSpace(n.State)})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", w.url, bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	if w.token != "" {
		req.Header.Set("Authorization", "Bearer "+w.token)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
