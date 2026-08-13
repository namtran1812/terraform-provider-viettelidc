package monitoring

import (
	"context"
	"net"
	"net/url"
	"sync"
	"time"
)

type Result struct {
	ServerID  string    `json:"server_id"`
	Up        bool      `json:"up"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}
type Checker struct {
	Timeout time.Duration
	Workers int
}

func (c Checker) Check(ctx context.Context, id, address string) Result {
	start := time.Now()
	host := address
	if u, e := url.Parse(address); e == nil && u.Host != "" {
		host = u.Host
	}
	conn, e := (&net.Dialer{Timeout: c.Timeout}).DialContext(ctx, "tcp", host)
	r := Result{ServerID: id, Up: e == nil, LatencyMS: time.Since(start).Milliseconds(), CheckedAt: time.Now().UTC()}
	if e != nil {
		r.Error = e.Error()
	} else {
		conn.Close()
	}
	return r
}

type Target struct{ ID, Address string }

func (c Checker) CheckAll(ctx context.Context, ts []Target) []Result {
	workers := c.Workers
	if workers < 1 {
		workers = 64
	}
	jobs := make(chan Target)
	out := make(chan Result, len(ts))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				out <- c.Check(ctx, t.ID, t.Address)
			}
		}()
	}
	go func() {
		for _, t := range ts {
			jobs <- t
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()
	rs := make([]Result, 0, len(ts))
	for r := range out {
		rs = append(rs, r)
	}
	return rs
}
