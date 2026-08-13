package audit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	ID         int64     `json:"id"`
	Actor      string    `json:"actor"`
	Role       string    `json:"role"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	Success    bool      `json:"success"`
	CreatedAt  time.Time `json:"created_at"`
}

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS audit_events (
			id BIGSERIAL PRIMARY KEY,
			actor TEXT NOT NULL,
			role TEXT NOT NULL,
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			resource_id TEXT NOT NULL DEFAULT '',
			success BOOLEAN NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_audit_events_created_at
			ON audit_events(created_at DESC);

		CREATE INDEX IF NOT EXISTS idx_audit_events_actor
			ON audit_events(actor);
	`)
	return err
}

func (s *Store) Record(ctx context.Context, event Event) error {
	_, err := s.pool.Exec(
		ctx,
		`
		INSERT INTO audit_events (
			actor,
			role,
			action,
			resource,
			resource_id,
			success
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		`,
		event.Actor,
		event.Role,
		event.Action,
		event.Resource,
		event.ResourceID,
		event.Success,
	)

	return err
}

func (s *Store) List(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	rows, err := s.pool.Query(
		ctx,
		`
		SELECT
			id,
			actor,
			role,
			action,
			resource,
			resource_id,
			success,
			created_at
		FROM audit_events
		ORDER BY created_at DESC
		LIMIT $1
		`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]Event, 0)

	for rows.Next() {
		var event Event

		if err := rows.Scan(
			&event.ID,
			&event.Actor,
			&event.Role,
			&event.Action,
			&event.Resource,
			&event.ResourceID,
			&event.Success,
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	return events, rows.Err()
}
