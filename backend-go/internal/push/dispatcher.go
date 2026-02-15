package push

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/domain"
)

type DeleteByEndpointFunc func(context.Context, string) error

type Config struct {
	WorkerCount        int
	QueueSize          int
	MaxRetries         int
	RetryBaseBackoffMS int
	TTLSeconds         int
	VAPIDPublicKey     string
	VAPIDPrivateKey    string
	VAPIDSubject       string
}

type task struct {
	subscription domain.Subscription
	payload      []byte
}

type Dispatcher struct {
	config     Config
	deleteByEP DeleteByEndpointFunc
	queue      chan task
	waitGroup  sync.WaitGroup
}

func New(config Config, deleteByEndpoint DeleteByEndpointFunc) *Dispatcher {
	if config.WorkerCount < 1 {
		config.WorkerCount = 1
	}
	if config.QueueSize < 1 {
		config.QueueSize = 128
	}

	return &Dispatcher{
		config:     config,
		deleteByEP: deleteByEndpoint,
		queue:      make(chan task, config.QueueSize),
	}
}

func (dispatcher *Dispatcher) Start() {
	for range dispatcher.config.WorkerCount {
		dispatcher.waitGroup.Add(1)
		go func() {
			defer dispatcher.waitGroup.Done()
			for item := range dispatcher.queue {
				dispatcher.sendWithRetry(item)
			}
		}()
	}
}

func (dispatcher *Dispatcher) Stop() {
	close(dispatcher.queue)
	dispatcher.waitGroup.Wait()
}

func (dispatcher *Dispatcher) Enqueue(subscription domain.Subscription, payload []byte) {
	dispatcher.queue <- task{subscription: subscription, payload: payload}
}

func (dispatcher *Dispatcher) EnqueueMany(subscriptions []domain.Subscription, payload []byte) {
	for _, subscription := range subscriptions {
		dispatcher.Enqueue(subscription, payload)
	}
}

func (dispatcher *Dispatcher) sendWithRetry(item task) {
	if dispatcher.config.VAPIDPublicKey == "" || dispatcher.config.VAPIDPrivateKey == "" || dispatcher.config.VAPIDSubject == "" {
		log.Printf("push send skipped endpoint=%s err=missing_vapid_config", redactEndpoint(item.subscription.Endpoint))
		return
	}

	options := &webpush.Options{
		Subscriber:      dispatcher.config.VAPIDSubject,
		VAPIDPublicKey:  dispatcher.config.VAPIDPublicKey,
		VAPIDPrivateKey: dispatcher.config.VAPIDPrivateKey,
		TTL:             dispatcher.config.TTLSeconds,
		Urgency:         webpush.UrgencyHigh,
		Topic:           "lectures-on-tap",
	}

	subscription := &webpush.Subscription{
		Endpoint: item.subscription.Endpoint,
		Keys: webpush.Keys{
			P256dh: item.subscription.P256DH,
			Auth:   item.subscription.Auth,
		},
	}

	for attempt := 0; attempt <= dispatcher.config.MaxRetries; attempt++ {
		response, err := webpush.SendNotification(item.payload, subscription, options)
		if err != nil {
			if attempt < dispatcher.config.MaxRetries {
				time.Sleep(backoffDuration(dispatcher.config.RetryBaseBackoffMS, attempt))
				continue
			}
			log.Printf("push send error endpoint=%s err=%v", redactEndpoint(item.subscription.Endpoint), err)
			return
		}

		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()

		if response.StatusCode >= 200 && response.StatusCode <= 299 {
			return
		}

		if response.StatusCode == http.StatusGone {
			if err := dispatcher.deleteByEP(context.Background(), item.subscription.Endpoint); err != nil {
				log.Printf("failed deleting gone subscription endpoint=%s err=%v", redactEndpoint(item.subscription.Endpoint), err)
			}
			return
		}

		if response.StatusCode >= 500 && response.StatusCode <= 599 && attempt < dispatcher.config.MaxRetries {
			time.Sleep(backoffDuration(dispatcher.config.RetryBaseBackoffMS, attempt))
			continue
		}

		log.Printf("push send failed endpoint=%s status=%d", redactEndpoint(item.subscription.Endpoint), response.StatusCode)
		return
	}
}

func backoffDuration(baseMS, attempt int) time.Duration {
	if baseMS < 1 {
		baseMS = 1
	}
	delay := baseMS << attempt
	return time.Duration(delay) * time.Millisecond
}

func redactEndpoint(endpoint string) string {
	if endpoint == "" {
		return "unknown"
	}
	if strings.HasPrefix(endpoint, "https://") || strings.HasPrefix(endpoint, "http://") {
		parts := strings.Split(endpoint, "/")
		if len(parts) >= 3 {
			return parts[0] + "//" + parts[2]
		}
	}
	return "unknown"
}
