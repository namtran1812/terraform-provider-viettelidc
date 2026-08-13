package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit breaker open")

type CircuitBreaker struct {
	mu               sync.Mutex
	failures         int
	failureThreshold int
	openUntil        time.Time
	resetTimeout     time.Duration
}

func NewCircuitBreaker(
	failureThreshold int,
	resetTimeout time.Duration,
) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		resetTimeout:     resetTimeout,
	}
}

func (c *CircuitBreaker) Execute(fn func() error) error {
	c.mu.Lock()

	if time.Now().Before(c.openUntil) {
		c.mu.Unlock()
		return ErrCircuitOpen
	}

	c.mu.Unlock()

	err := fn()

	c.mu.Lock()
	defer c.mu.Unlock()

	if err == nil {
		c.failures = 0
		c.openUntil = time.Time{}
		return nil
	}

	c.failures++

	if c.failures >= c.failureThreshold {
		c.openUntil = time.Now().Add(c.resetTimeout)
	}

	return err
}

func Retry(
	ctx context.Context,
	attempts int,
	baseDelay time.Duration,
	fn func() error,
) error {
	var err error

	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err = fn()
		if err == nil {
			return nil
		}

		if attempt == attempts-1 {
			break
		}

		delay := baseDelay * time.Duration(1<<attempt)

		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return err
}
