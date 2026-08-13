package monitoring

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCheckerHealthyTarget(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	checker := Checker{
		Timeout: time.Second,
		Workers: 1,
	}

	result := checker.Check(
		context.Background(),
		"healthy-server",
		listener.Addr().String(),
	)

	if !result.Up {
		t.Fatalf("expected healthy target to be up: %s", result.Error)
	}
}

func TestCheckerUnavailableTarget(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := listener.Addr().String()
	_ = listener.Close()

	checker := Checker{
		Timeout: 100 * time.Millisecond,
		Workers: 1,
	}

	result := checker.Check(
		context.Background(),
		"unavailable-server",
		address,
	)

	if result.Up {
		t.Fatal("expected unavailable target to be down")
	}
}

func TestCheckAllTenThousandTargets(t *testing.T) {
	const count = 10000

	targets := make([]Target, count)

	for i := range targets {
		targets[i] = Target{
			ID:      "server",
			Address: "127.0.0.1:1",
		}
	}

	checker := Checker{
		Timeout: 100 * time.Millisecond,
		Workers: 128,
	}

	results := checker.CheckAll(context.Background(), targets)

	if len(results) != count {
		t.Fatalf("expected %d results, got %d", count, len(results))
	}
}
