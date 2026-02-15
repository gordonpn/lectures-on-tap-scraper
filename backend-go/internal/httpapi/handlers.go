package httpapi

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/domain"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/service"
)

type Handlers struct {
	service *service.Service
}

type pushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type subscribeRequest struct {
	Subscription *pushSubscription `json:"subscription"`
	Topic        string            `json:"topic"`
	UICode       string            `json:"ui_code"`

	Endpoint string `json:"endpoint"`
	P256DH   string `json:"p256dh"`
	Auth     string `json:"auth"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type unsubscribeRequest struct {
	Endpoint     string            `json:"endpoint"`
	Subscription *pushSubscription `json:"subscription"`
}

type triggerRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
	URL   *string `json:"url"`
	Topic *string `json:"topic"`
}

type triggerSelfRequest struct {
	UICode   *string `json:"ui_code"`
	Endpoint *string `json:"endpoint"`
}

func NewRouter(service *service.Service) http.Handler {
	handlers := &Handlers{service: service}
	router := chi.NewRouter()

	router.Get("/healthz", handlers.healthz)
	router.Route("/api", func(r chi.Router) {
		r.Post("/subscribe", handlers.subscribe)
		r.Post("/unsubscribe", handlers.unsubscribe)
		r.Get("/subscriptions/me", handlers.subscriptionMe)
		r.Post("/trigger-self", handlers.triggerSelf)

		r.With(handlers.hubSecretAuth).Post("/trigger", handlers.trigger)
	})

	return router
}

func (handlers *Handlers) hubSecretAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := request.Header.Get("X-Hub-Secret")
		if !handlers.service.ValidateHubSecret(header) {
			writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (handlers *Handlers) healthz(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte("ok"))
}

func (handlers *Handlers) subscribe(writer http.ResponseWriter, request *http.Request) {
	clientIP := requestIP(request)
	if !handlers.service.AllowSubscribe(clientIP) {
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}

	var payload subscribeRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_subscription"})
		return
	}

	if !handlers.service.ValidateUICode(payload.UICode) {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "invalid_access_code"})
		return
	}

	topic := domain.NormalizeTopic(payload.Topic)
	subscription, ok := buildSubscription(payload, topic)
	if !ok {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_subscription"})
		return
	}

	created, err := handlers.service.Subscribe(request.Context(), subscription)
	if err != nil {
		log.Printf("subscribe upsert failed: %v", err)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	statusCode := http.StatusOK
	if created {
		statusCode = http.StatusCreated
	}

	writeJSON(writer, statusCode, map[string]any{"status": "active", "topics": subscription.Topics})
}

func (handlers *Handlers) unsubscribe(writer http.ResponseWriter, request *http.Request) {
	var payload unsubscribeRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "missing_endpoint"})
		return
	}

	endpoint := strings.TrimSpace(payload.Endpoint)
	if endpoint == "" && payload.Subscription != nil {
		endpoint = strings.TrimSpace(payload.Subscription.Endpoint)
	}
	if endpoint == "" {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "missing_endpoint"})
		return
	}

	if err := handlers.service.Unsubscribe(request.Context(), endpoint); err != nil {
		log.Printf("unsubscribe delete failed: %v", err)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(writer, http.StatusOK, map[string]string{"status": "inactive"})
}

func (handlers *Handlers) subscriptionMe(writer http.ResponseWriter, request *http.Request) {
	endpoint := strings.TrimSpace(request.URL.Query().Get("endpoint"))
	if endpoint == "" {
		writeJSON(writer, http.StatusOK, map[string]any{"status": "inactive", "topics": []string{}})
		return
	}

	status, topics, err := handlers.service.SubscriptionsMe(request.Context(), endpoint)
	if err != nil {
		log.Printf("subscriptions/me query failed: %v", err)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"status": status, "topics": topics})
}

func (handlers *Handlers) trigger(writer http.ResponseWriter, request *http.Request) {
	var payload triggerRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_payload"})
		return
	}

	if payload.Title == nil || payload.Body == nil || payload.URL == nil {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_payload"})
		return
	}

	topic := domain.NormalizeTopic(optionalString(payload.Topic))
	payloadBytes, err := json.Marshal(map[string]string{"title": *payload.Title, "body": *payload.Body, "url": *payload.URL})
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	targets, err := handlers.service.TriggerTopic(request.Context(), topic, payloadBytes, isDryRun(request))
	if err != nil {
		log.Printf("trigger failed: %v", err)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	if isDryRun(request) {
		writeJSON(writer, http.StatusOK, map[string]any{"dry_run": true, "topic": topic, "targets": targets})
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"status": "queued", "topic": topic, "targets": targets})
}

func (handlers *Handlers) triggerSelf(writer http.ResponseWriter, request *http.Request) {
	var payload triggerSelfRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "invalid_access_code"})
		return
	}

	if payload.UICode == nil || payload.Endpoint == nil || !handlers.service.ValidateUICode(*payload.UICode) {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "invalid_access_code"})
		return
	}

	targets, err := handlers.service.TriggerSelf(request.Context(), *payload.Endpoint)
	if err != nil {
		log.Printf("trigger-self failed: %v", err)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"status": "queued", "targets": targets})
}

func buildSubscription(request subscribeRequest, topic string) (domain.Subscription, bool) {
	var endpoint, p256dh, auth string

	if request.Subscription != nil {
		endpoint = strings.TrimSpace(request.Subscription.Endpoint)
		p256dh = strings.TrimSpace(request.Subscription.Keys.P256DH)
		auth = strings.TrimSpace(request.Subscription.Keys.Auth)
	}

	if endpoint == "" {
		endpoint = strings.TrimSpace(request.Endpoint)
	}
	if p256dh == "" {
		p256dh = strings.TrimSpace(firstNonEmpty(request.P256DH, request.Keys.P256DH))
	}
	if auth == "" {
		auth = strings.TrimSpace(firstNonEmpty(request.Auth, request.Keys.Auth))
	}

	if endpoint == "" || p256dh == "" || auth == "" {
		return domain.Subscription{}, false
	}

	return domain.Subscription{Endpoint: endpoint, P256DH: p256dh, Auth: auth, Topics: []string{topic}}, true
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func isDryRun(request *http.Request) bool {
	value := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("dry_run")))
	return value == "true" || value == "1" || value == "yes"
}

func requestIP(request *http.Request) string {
	forwardedFor := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-For"), ",")[0])
	if forwardedFor != "" {
		return forwardedFor
	}

	realIP := strings.TrimSpace(request.Header.Get("X-Real-IP"))
	if realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
