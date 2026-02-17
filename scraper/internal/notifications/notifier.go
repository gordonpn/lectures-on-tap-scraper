package notifications

import "context"

// Notification captures the destination-agnostic message payload.
type Notification struct {
	EventID string
	Body    string
	State   string
	URL     string
}

// Notifier publishes notifications to a single destination.
type Notifier interface {
	Name() string
	Notify(ctx context.Context, n Notification) error
}
