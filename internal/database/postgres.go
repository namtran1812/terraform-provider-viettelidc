package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vmware/terraform-provider-vcd/v3/internal/reporting"
	"github.com/vmware/terraform-provider-vcd/v3/internal/server"
)

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

const Schema = `
CREATE TABLE IF NOT EXISTS servers(
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS health_checks(
    id BIGSERIAL PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    up BOOLEAN NOT NULL,
    latency_ms BIGINT NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL,
    error TEXT
);
CREATE INDEX IF NOT EXISTS idx_checks_server_time
    ON health_checks(server_id, checked_at DESC);
`

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, Schema)
	return err
}

type ServerStore struct {
	pool *pgxpool.Pool
}

func NewServerStore(pool *pgxpool.Pool) *ServerStore {
	return &ServerStore{pool: pool}
}

func (s *ServerStore) Create(ctx context.Context, v server.Server) (server.Server, error) {
	if v.Status == "" {
		v.Status = "unknown"
	}
	err := s.pool.QueryRow(ctx, `
        INSERT INTO servers(id, name, address, status)
        VALUES($1, $2, $3, $4)
        RETURNING updated_at`,
		v.ID, v.Name, v.Address, v.Status,
	).Scan(&v.UpdatedAt)
	return v, err
}

func (s *ServerStore) Get(ctx context.Context, id string) (server.Server, error) {
	var v server.Server
	err := s.pool.QueryRow(ctx, `
        SELECT id, name, address, status, updated_at
        FROM servers
        WHERE id = $1`, id,
	).Scan(&v.ID, &v.Name, &v.Address, &v.Status, &v.UpdatedAt)
	return v, err
}

func (s *ServerStore) Update(ctx context.Context, id string, v server.Server) (server.Server, error) {
	var out server.Server
	err := s.pool.QueryRow(ctx, `
        UPDATE servers
        SET name = COALESCE(NULLIF($2, ''), name),
            address = COALESCE(NULLIF($3, ''), address),
            status = COALESCE(NULLIF($4, ''), status),
            updated_at = now()
        WHERE id = $1
        RETURNING id, name, address, status, updated_at`,
		id, v.Name, v.Address, v.Status,
	).Scan(&out.ID, &out.Name, &out.Address, &out.Status, &out.UpdatedAt)
	return out, err
}

func (s *ServerStore) List(ctx context.Context) ([]server.Server, error) {
	rows, err := s.pool.Query(ctx, `
        SELECT id, name, address, status, updated_at
        FROM servers
        ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]server.Server, 0)
	for rows.Next() {
		var v server.Server
		if err := rows.Scan(&v.ID, &v.Name, &v.Address, &v.Status, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *ServerStore) RecordHealthCheck(
	ctx context.Context,
	serverID string,
	up bool,
	latencyMS int64,
	checkedAt time.Time,
	checkErr string,
) error {
	_, err := s.pool.Exec(ctx, `
        INSERT INTO health_checks(server_id, up, latency_ms, checked_at, error)
        VALUES($1, $2, $3, $4, NULLIF($5, ''))`,
		serverID, up, latencyMS, checkedAt, checkErr,
	)
	return err
}

func (s *ServerStore) UptimeReport(ctx context.Context, serverID string, from, to time.Time) (reporting.UptimeReport, error) {
	var checks, successful int
	err := s.pool.QueryRow(ctx, `
        SELECT COUNT(*), COUNT(*) FILTER (WHERE up)
        FROM health_checks
        WHERE server_id = $1
          AND checked_at >= $2
          AND checked_at <= $3`,
		serverID, from, to,
	).Scan(&checks, &successful)
	if err != nil {
		return reporting.UptimeReport{}, err
	}

	uptime := 0.0
	if checks > 0 {
		uptime = 100 * float64(successful) / float64(checks)
	}
	return reporting.UptimeReport{
		ServerID:      serverID,
		From:          from,
		To:            to,
		Checks:        checks,
		Successful:    successful,
		UptimePercent: uptime,
	}, nil
}
