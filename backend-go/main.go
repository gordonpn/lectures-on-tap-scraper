package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/config"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/httpapi"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/push"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/ratelimit"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/service"
	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	ctx := context.Background()
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connect failed: %v", err)
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctx); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	repository := store.NewPostgres(dbPool)
	limiter := ratelimit.New(cfg.SubscribeRateLimit, cfg.SubscribeWindow)

	dispatcher := push.New(push.Config{
		WorkerCount:        cfg.WorkerCount,
		QueueSize:          cfg.QueueSize,
		MaxRetries:         cfg.MaxRetries,
		RetryBaseBackoffMS: cfg.RetryBaseBackoffMS,
		TTLSeconds:         cfg.TTLSeconds,
		VAPIDPublicKey:     cfg.VAPIDPublicKey,
		VAPIDPrivateKey:    cfg.VAPIDPrivateKey,
		VAPIDSubject:       cfg.VAPIDSubject,
	}, repository.DeleteByEndpoint)
	dispatcher.Start()
	defer dispatcher.Stop()

	appService := service.New(cfg, repository, limiter, dispatcher)
	router := httpapi.NewRouter(appService)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("backend-go listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server failed: %v", err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
