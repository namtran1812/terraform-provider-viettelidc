package server

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Server struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Store interface {
	Create(context.Context, Server) (Server, error)
	Get(context.Context, string) (Server, error)
	Update(context.Context, string, Server) (Server, error)
	List(context.Context) ([]Server, error)
}
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]Server
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{data: map[string]Server{}} }
func (s *MemoryStore) Create(_ context.Context, v Server) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[v.ID]; ok {
		return Server{}, errors.New("server exists")
	}
	v.UpdatedAt = time.Now().UTC()
	s.data[v.ID] = v
	return v, nil
}
func (s *MemoryStore) Get(_ context.Context, id string) (Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[id]
	if !ok {
		return Server{}, errors.New("server not found")
	}
	return v, nil
}
func (s *MemoryStore) Update(_ context.Context, id string, v Server) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return Server{}, errors.New("server not found")
	}
	v.ID = id
	v.UpdatedAt = time.Now().UTC()
	s.data[id] = v
	return v, nil
}
func (s *MemoryStore) List(_ context.Context) ([]Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Server, 0, len(s.data))
	for _, v := range s.data {
		out = append(out, v)
	}
	return out, nil
}
