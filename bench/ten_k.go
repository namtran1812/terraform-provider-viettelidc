package main

import (
	"context"
	"fmt"
	"time"

	"github.com/vmware/terraform-provider-vcd/v3/internal/monitoring"
)

const (
	targetCount = 10000
	workers     = 128
	timeout     = 100 * time.Millisecond
)

func main() {
	targets := make([]monitoring.Target, targetCount)

	for i := range targets {
		targets[i] = monitoring.Target{
			ID:      fmt.Sprintf("server-%05d", i),
			Address: "127.0.0.1:1",
		}
	}

	checker := monitoring.Checker{
		Timeout: timeout,
		Workers: workers,
	}

	start := time.Now()
	results := checker.CheckAll(context.Background(), targets)
	elapsed := time.Since(start)

	up := 0
	for _, result := range results {
		if result.Up {
			up++
		}
	}

	fmt.Println("Viettel Infrastructure Monitoring Load Benchmark")
	fmt.Println("-----------------------------------------------")
	fmt.Printf("targets:       %d\n", len(results))
	fmt.Printf("workers:       %d\n", workers)
	fmt.Printf("up:            %d\n", up)
	fmt.Printf("down:          %d\n", len(results)-up)
	fmt.Printf("elapsed:       %s\n", elapsed)
	fmt.Printf("throughput:    %.0f checks/sec\n",
		float64(len(results))/elapsed.Seconds(),
	)
}
