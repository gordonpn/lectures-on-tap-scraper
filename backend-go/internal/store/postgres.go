package store

import (
	"context"
	"errors"

	"github.com/gordonpn/lectures-on-tap-scraper/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	UpsertSubscription(ctx context.Context, subscription domain.Subscription) (bool, error)
	DeleteByEndpoint(ctx context.Context, endpoint string) error
	GetTopicsByEndpoint(ctx context.Context, endpoint string) ([]string, bool, error)
	GetSubscriptionByEndpoint(ctx context.Context, endpoint string) (domain.Subscription, bool, error)
	ListForTopic(ctx context.Context, topic string) ([]domain.Subscription, error)
}

type Postgres struct {
	db *pgxpool.Pool
}

func NewPostgres(db *pgxpool.Pool) *Postgres {
	return &Postgres{db: db}
}

func (repository *Postgres) UpsertSubscription(ctx context.Context, subscription domain.Subscription) (bool, error) {
	existsQuery := `SELECT EXISTS(SELECT 1 FROM push_subscriptions WHERE endpoint = $1)`
	var exists bool
	if err := repository.db.QueryRow(ctx, existsQuery, subscription.Endpoint).Scan(&exists); err != nil {
		return false, err
	}

	upsertQuery := `
		INSERT INTO push_subscriptions (endpoint, p256dh, auth, topics, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (endpoint)
		DO UPDATE SET p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth, topics = EXCLUDED.topics, updated_at = NOW()
	`

	_, err := repository.db.Exec(ctx, upsertQuery, subscription.Endpoint, subscription.P256DH, subscription.Auth, subscription.Topics)
	if err != nil {
		return false, err
	}

	return !exists, nil
}

func (repository *Postgres) DeleteByEndpoint(ctx context.Context, endpoint string) error {
	_, err := repository.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint)
	return err
}

func (repository *Postgres) GetTopicsByEndpoint(ctx context.Context, endpoint string) ([]string, bool, error) {
	var topics []string
	err := repository.db.QueryRow(ctx, `SELECT topics FROM push_subscriptions WHERE endpoint = $1`, endpoint).Scan(&topics)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return topics, true, nil
}

func (repository *Postgres) GetSubscriptionByEndpoint(ctx context.Context, endpoint string) (domain.Subscription, bool, error) {
	var subscription domain.Subscription
	err := repository.db.QueryRow(ctx, `SELECT endpoint, p256dh, auth, topics FROM push_subscriptions WHERE endpoint = $1`, endpoint).
		Scan(&subscription.Endpoint, &subscription.P256DH, &subscription.Auth, &subscription.Topics)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, false, nil
		}
		return domain.Subscription{}, false, err
	}
	return subscription, true, nil
}

func (repository *Postgres) ListForTopic(ctx context.Context, topic string) ([]domain.Subscription, error) {
	query := `
		SELECT endpoint, p256dh, auth, topics
		FROM push_subscriptions
		WHERE $1 = ANY(topics)
	`
	rows, err := repository.db.Query(ctx, query, topic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.Subscription, 0)
	for rows.Next() {
		item := domain.Subscription{}
		if err := rows.Scan(&item.Endpoint, &item.P256DH, &item.Auth, &item.Topics); err != nil {
			return nil, err
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
