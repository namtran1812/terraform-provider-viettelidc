package ratelimit

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type Manager struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     rate.Limit
	burst    int
	ttl      time.Duration
}

func New(requestsPerSecond float64, burst int, ttl time.Duration) *Manager {
	return &Manager{
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(requestsPerSecond),
		burst:    burst,
		ttl:      ttl,
	}
}

func (m *Manager) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()

		limiter := m.getLimiter(key)

		if !limiter.Allow() {
			c.AbortWithStatusJSON(
				http.StatusTooManyRequests,
				gin.H{"error": "rate limit exceeded"},
			)
			return
		}

		c.Next()
	}
}

func (m *Manager) getLimiter(key string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	if existing, ok := m.visitors[key]; ok {
		existing.lastSeen = now
		return existing.limiter
	}

	limiter := rate.NewLimiter(m.rate, m.burst)

	m.visitors[key] = &visitor{
		limiter:  limiter,
		lastSeen: now,
	}

	return limiter
}

func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-m.ttl)

	for key, visitor := range m.visitors {
		if visitor.lastSeen.Before(cutoff) {
			delete(m.visitors, key)
		}
	}
}
