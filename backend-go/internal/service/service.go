package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"

	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/config"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/domain"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/push"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/ratelimit"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/store"
)

type Service struct {
	config     config.Config
	repository store.Repository
	limiter    *ratelimit.Limiter
	dispatcher *push.Dispatcher
}

func New(config config.Config, repository store.Repository, limiter *ratelimit.Limiter, dispatcher *push.Dispatcher) *Service {
	return &Service{
		config:     config,
		repository: repository,
		limiter:    limiter,
		dispatcher: dispatcher,
	}
}

func (service *Service) AllowSubscribe(ip string) bool {
	return service.limiter.Allow(ip)
}

func (service *Service) ValidateUICode(code string) bool {
	return secureCompare(service.config.HubUICode, code)
}

func (service *Service) ValidateHubSecret(secret string) bool {
	return secureCompare(service.config.HubSecret, secret)
}

func (service *Service) Subscribe(ctx context.Context, subscription domain.Subscription) (bool, error) {
	return service.repository.UpsertSubscription(ctx, subscription)
}

func (service *Service) Unsubscribe(ctx context.Context, endpoint string) error {
	return service.repository.DeleteByEndpoint(ctx, endpoint)
}

func (service *Service) SubscriptionsMe(ctx context.Context, endpoint string) (string, []string, error) {
	topics, found, err := service.repository.GetTopicsByEndpoint(ctx, endpoint)
	if err != nil {
		return "", nil, err
	}
	if !found {
		return "inactive", []string{}, nil
	}
	return "active", topics, nil
}

func (service *Service) TriggerTopic(ctx context.Context, topic string, payload []byte, dryRun bool) (int, error) {
	targets, err := service.repository.ListForTopic(ctx, domain.NormalizeTopic(topic))
	if err != nil {
		return 0, err
	}

	if !dryRun {
		go service.dispatcher.EnqueueMany(targets, payload)
	}

	return len(targets), nil
}

func (service *Service) TriggerSelf(ctx context.Context, endpoint string) (int, error) {
	target, found, err := service.repository.GetSubscriptionByEndpoint(ctx, endpoint)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}

	payload, err := json.Marshal(map[string]string{
		"title": "Test notification",
		"body":  "Your Notification Hub is wired up.",
		"url":   "/",
	})
	if err != nil {
		return 0, err
	}

	go service.dispatcher.Enqueue(target, payload)
	return 1, nil
}

func secureCompare(expected, actual string) bool {
	if len(expected) == 0 || len(actual) == 0 {
		return false
	}
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}
