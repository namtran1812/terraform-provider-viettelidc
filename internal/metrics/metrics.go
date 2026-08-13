package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec

	GRPCCalls    *prometheus.CounterVec
	GRPCDuration *prometheus.HistogramVec

	CircuitOpen prometheus.Counter
	AuditEvents *prometheus.CounterVec
}

func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		HTTPRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "infra_http_requests_total",
				Help: "Total HTTP requests processed by the control plane.",
			},
			[]string{"method", "path", "status"},
		),

		HTTPDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "infra_http_request_duration_seconds",
				Help:    "HTTP request duration.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),

		GRPCCalls: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "infra_grpc_calls_total",
				Help: "Total outbound gRPC calls.",
			},
			[]string{"service", "operation", "result"},
		),

		GRPCDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "infra_grpc_call_duration_seconds",
				Help:    "Outbound gRPC call duration.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"service", "operation"},
		),

		CircuitOpen: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "infra_circuit_open_total",
				Help: "Number of requests rejected because a circuit breaker was open.",
			},
		),

		AuditEvents: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "infra_audit_events_total",
				Help: "Total security audit events.",
			},
			[]string{"action", "role", "success"},
		),
	}

	reg.MustRegister(
		m.HTTPRequests,
		m.HTTPDuration,
		m.GRPCCalls,
		m.GRPCDuration,
		m.CircuitOpen,
		m.AuditEvents,
	)

	return m
}

func (m *Metrics) HTTPMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}

		m.HTTPRequests.WithLabelValues(
			c.Request.Method,
			path,
			strconv.Itoa(c.Writer.Status()),
		).Inc()

		m.HTTPDuration.WithLabelValues(
			c.Request.Method,
			path,
		).Observe(time.Since(start).Seconds())
	}
}

func (m *Metrics) ObserveGRPC(
	service string,
	operation string,
	start time.Time,
	err error,
) {
	result := "success"
	if err != nil {
		result = "error"
	}

	m.GRPCCalls.WithLabelValues(
		service,
		operation,
		result,
	).Inc()

	m.GRPCDuration.WithLabelValues(
		service,
		operation,
	).Observe(time.Since(start).Seconds())
}
