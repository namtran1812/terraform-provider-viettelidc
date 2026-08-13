package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerOpens(t *testing.T) {
	breaker := NewCircuitBreaker(2, time.Second)

	failure := errors.New("dependency down")

	if err := breaker.Execute(func() error {
		return failure
	}); !errors.Is(err, failure) {
		t.Fatal("expected dependency error")
	}

	if err := breaker.Execute(func() error {
		return failure
	}); !errors.Is(err, failure) {
		t.Fatal("expected dependency error")
	}

	if err := breaker.Execute(func() error {
		return nil
	}); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected circuit open, got %v", err)
	}
}

func TestCircuitBreakerResetsOnSuccess(t *testing.T) {
	breaker := NewCircuitBreaker(2, time.Second)

	_ = breaker.Execute(func() error {
		return errors.New("temporary failure")
	})

	if err := breaker.Execute(func() error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := breaker.Execute(func() error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRetryEventuallySucceeds(t *testing.T) {
	attempts := 0

	err := Retry(
		context.Background(),
		3,
		time.Millisecond,
		func() error {
			attempts++

			if attempts < 3 {
				return errors.New("temporary failure")
			}

			return nil
		},
	)

	if err != nil {
		t.Fatal(err)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
