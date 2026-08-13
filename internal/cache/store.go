package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vmware/terraform-provider-vcd/v3/internal/server"
)

type ServerStore struct {
	next   server.Store
	cache  *Cache
	ttl    time.Duration
	logger *slog.Logger
}

func NewServerStore(next server.Store, cache *Cache, ttl time.Duration, logger *slog.Logger) *ServerStore {
	return &ServerStore{next: next, cache: cache, ttl: ttl, logger: logger}
}

func serverKey(id string) string {
	return fmt.Sprintf("server:%s", id)
}

func (s *ServerStore) Create(ctx context.Context, v server.Server) (server.Server, error) {
	out, err := s.next.Create(ctx, v)
	if err != nil {
		return server.Server{}, err
	}
	if err := s.cache.SetJSON(ctx, serverKey(out.ID), out, s.ttl); err != nil {
		s.logger.Warn("redis cache write failed", "server_id", out.ID, "error", err)
	}
	return out, nil
}

func (s *ServerStore) Get(ctx context.Context, id string) (server.Server, error) {
	var cached server.Server
	if err := s.cache.GetJSON(ctx, serverKey(id), &cached); err == nil {
		return cached, nil
	}

	out, err := s.next.Get(ctx, id)
	if err != nil {
		return server.Server{}, err
	}
	if err := s.cache.SetJSON(ctx, serverKey(id), out, s.ttl); err != nil {
		s.logger.Warn("redis cache fill failed", "server_id", id, "error", err)
	}
	return out, nil
}

func (s *ServerStore) Update(ctx context.Context, id string, v server.Server) (server.Server, error) {
	out, err := s.next.Update(ctx, id, v)
	if err != nil {
		return server.Server{}, err
	}
	if err := s.cache.SetJSON(ctx, serverKey(id), out, s.ttl); err != nil {
		s.logger.Warn("redis cache refresh failed", "server_id", id, "error", err)
	}
	return out, nil
}

func (s *ServerStore) List(ctx context.Context) ([]server.Server, error) {
	return s.next.List(ctx)
}
