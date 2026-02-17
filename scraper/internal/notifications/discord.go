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

type DiscordNotifier struct {
	client     *http.Client
	webhookURL string
}

type discordPayload struct {
	Content string `json:"content"`
}

func NewDiscordNotifier(client *http.Client, webhookURL string) *DiscordNotifier {
	return &DiscordNotifier{client: client, webhookURL: strings.TrimSpace(webhookURL)}
}

func (d *DiscordNotifier) Name() string {
	return "discord"
}

func (d *DiscordNotifier) Notify(ctx context.Context, n Notification) error {
	payload, err := json.Marshal(discordPayload{Content: n.Body})
	if err != nil {
		return fmt.Errorf("marshal discord payload: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", d.webhookURL, bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("post discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
