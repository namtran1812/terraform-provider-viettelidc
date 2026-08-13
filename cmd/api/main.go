package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	monitoringv1 "github.com/vmware/terraform-provider-vcd/v3/gen/monitoring/v1"
	reportingv1 "github.com/vmware/terraform-provider-vcd/v3/gen/reporting/v1"
	"github.com/vmware/terraform-provider-vcd/v3/internal/audit"
	"github.com/vmware/terraform-provider-vcd/v3/internal/auth"
	"github.com/vmware/terraform-provider-vcd/v3/internal/cache"
	"github.com/vmware/terraform-provider-vcd/v3/internal/config"
	"github.com/vmware/terraform-provider-vcd/v3/internal/database"
	"github.com/vmware/terraform-provider-vcd/v3/internal/logging"
	metricspkg "github.com/vmware/terraform-provider-vcd/v3/internal/metrics"
	"github.com/vmware/terraform-provider-vcd/v3/internal/ratelimit"
	"github.com/vmware/terraform-provider-vcd/v3/internal/resilience"
	"github.com/vmware/terraform-provider-vcd/v3/internal/search"
	srv "github.com/vmware/terraform-provider-vcd/v3/internal/server"
	"github.com/vmware/terraform-provider-vcd/v3/internal/tlsconfig"
	"google.golang.org/grpc"
)

func main() {
	cfg := config.Load()
	logger := logging.Stdout()
	ctx := context.Background()

	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := database.EnsureSchema(ctx, pool); err != nil {
		logger.Error("postgres schema setup failed", "error", err)
		os.Exit(1)
	}

	if err := audit.EnsureSchema(ctx, pool); err != nil {
		logger.Error("audit schema setup failed", "error", err)
		os.Exit(1)
	}

	auditStore := audit.New(pool)

	redisCache := cache.New(cfg.RedisAddr)
	defer redisCache.Close()
	if err := redisCache.Ping(ctx); err != nil {
		logger.Warn("redis unavailable; requests will continue using postgres", "error", err)
	}

	postgresStore := database.NewServerStore(pool)
	store := cache.NewServerStore(postgresStore, redisCache, 5*time.Minute, logger)
	elastic := search.New(cfg.ElasticURL)

	monitoringCreds, err := tlsconfig.ClientCredentials(
		"certs/api.crt",
		"certs/api.key",
		"certs/ca.crt",
		"monitoring.internal",
	)
	if err != nil {
		logger.Error("monitoring TLS setup failed", "error", err)
		os.Exit(1)
	}

	monitoringConn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(monitoringCreds),
	)
	if err != nil {
		logger.Error("monitoring grpc client setup failed", "error", err)
		os.Exit(1)
	}
	defer monitoringConn.Close()
	reportingCreds, err := tlsconfig.ClientCredentials(
		"certs/api.crt",
		"certs/api.key",
		"certs/ca.crt",
		"reporting.internal",
	)
	if err != nil {
		logger.Error("reporting TLS setup failed", "error", err)
		os.Exit(1)
	}

	reportingConn, err := grpc.NewClient(
		"localhost:50052",
		grpc.WithTransportCredentials(reportingCreds),
	)
	if err != nil {
		logger.Error("reporting grpc client setup failed", "error", err)
		os.Exit(1)
	}
	defer reportingConn.Close()

	monitoringClient := monitoringv1.NewMonitoringServiceClient(monitoringConn)
	reportingClient := reportingv1.NewReportingServiceClient(reportingConn)

	monitoringBreaker := resilience.NewCircuitBreaker(
		3,
		10*time.Second,
	)

	reportingBreaker := resilience.NewCircuitBreaker(
		3,
		10*time.Second,
	)

	r := gin.New()

	metricsRegistry := prometheus.NewRegistry()
	appMetrics := metricspkg.New(metricsRegistry)

	rateLimiter := ratelimit.New(
		25,
		50,
		10*time.Minute,
	)

	r.Use(
		gin.Logger(),
		gin.Recovery(),
		appMetrics.HTTPMiddleware(),
		rateLimiter.Middleware(),
	)

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			rateLimiter.Cleanup()
		}
	}()

	r.GET(
		"/metrics",
		gin.WrapH(
			promhttp.HandlerFor(
				metricsRegistry,
				promhttp.HandlerOpts{},
			),
		),
	)

	r.GET("/healthz", func(c *gin.Context) {
		if err := pool.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "postgres": "down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/auth/token", func(c *gin.Context) {
		var request struct {
			Subject string `json:"subject"`
			Role    string `json:"role"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}

		if request.Subject == "" {
			request.Subject = "operator"
		}

		if request.Role == "" {
			request.Role = auth.RoleOperator
		}

		token, err := auth.Issue(
			cfg.JWTSecret,
			request.Subject,
			request.Role,
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token": token,
			"role":  request.Role,
		})
	})

	v1 := r.Group("/v1", auth.Middleware(cfg.JWTSecret))

	v1.GET(
		"/audit",
		auth.RequireRole(auth.RoleAdmin),
		func(c *gin.Context) {
			events, err := auditStore.List(c.Request.Context(), 100)
			if err != nil {
				c.JSON(
					http.StatusInternalServerError,
					gin.H{"error": err.Error()},
				)
				return
			}

			c.JSON(http.StatusOK, events)
		},
	)

	v1.POST("/servers", auth.RequireRole(auth.RoleOperator, auth.RoleAdmin), func(c *gin.Context) {
		var s srv.Server
		if err := c.ShouldBindJSON(&s); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		if s.ID == "" {
			s.ID = uuid.NewString()
		}
		if s.Status == "" {
			s.Status = "unknown"
		}

		out, err := store.Create(c.Request.Context(), s)

		recordAudit(
			c.Request.Context(),
			logger,
			auditStore,
			auth.Subject(c),
			auth.Role(c),
			"server.create",
			"server",
			s.ID,
			err == nil,
		)

		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		indexBestEffort(c.Request.Context(), logger, elastic, "servers", out.ID, out)
		c.JSON(http.StatusCreated, out)
	})

	v1.GET("/servers", auth.RequireRole(auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin), func(c *gin.Context) {
		out, err := store.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	})

	v1.GET("/servers/:id", auth.RequireRole(auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin), func(c *gin.Context) {
		out, err := store.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		c.JSON(http.StatusOK, out)
	})

	v1.PATCH("/servers/:id", auth.RequireRole(auth.RoleOperator, auth.RoleAdmin), func(c *gin.Context) {
		var patch srv.Server
		if err := c.ShouldBindJSON(&patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}

		serverID := c.Param("id")
		out, err := store.Update(c.Request.Context(), serverID, patch)

		recordAudit(
			c.Request.Context(),
			logger,
			auditStore,
			auth.Subject(c),
			auth.Role(c),
			"server.update",
			"server",
			serverID,
			err == nil,
		)

		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}

		indexBestEffort(c.Request.Context(), logger, elastic, "servers", out.ID, out)
		c.JSON(http.StatusOK, out)
	})

	v1.POST("/servers/:id/check", auth.RequireRole(auth.RoleOperator, auth.RoleAdmin), func(c *gin.Context) {
		serverID := c.Param("id")

		s, err := store.Get(c.Request.Context(), serverID)
		if err != nil {
			recordAudit(
				c.Request.Context(),
				logger,
				auditStore,
				auth.Subject(c),
				auth.Role(c),
				"server.check",
				"server",
				serverID,
				false,
			)

			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}

		var resp *monitoringv1.CheckResponse

		err = monitoringBreaker.Execute(func() error {
			return resilience.Retry(
				c.Request.Context(),
				3,
				50*time.Millisecond,
				func() error {
					var callErr error

					grpcStart := time.Now()

					resp, callErr = monitoringClient.Check(
						c.Request.Context(),
						&monitoringv1.CheckRequest{
							ServerId: s.ID,
							Address:  s.Address,
						},
					)

					appMetrics.ObserveGRPC(
						"monitoring",
						"Check",
						grpcStart,
						callErr,
					)

					return callErr
				},
			)
		})

		recordAudit(
			c.Request.Context(),
			logger,
			auditStore,
			auth.Subject(c),
			auth.Role(c),
			"server.check",
			"server",
			serverID,
			err == nil,
		)

		if err != nil {
			if errors.Is(err, resilience.ErrCircuitOpen) {
				appMetrics.CircuitOpen.Inc()
				c.JSON(
					http.StatusServiceUnavailable,
					gin.H{
						"error":  "monitoring circuit breaker open",
						"detail": err.Error(),
					},
				)
				return
			}

			c.JSON(
				http.StatusBadGateway,
				gin.H{
					"error":  "monitoring service unavailable",
					"detail": err.Error(),
				},
			)
			return
		}

		c.JSON(http.StatusOK, resp)
	})

	v1.GET("/servers/:id/report", auth.RequireRole(auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin), func(c *gin.Context) {
		to := time.Now().UTC()
		from := to.Add(-24 * time.Hour)
		var resp *reportingv1.UptimeResponse

		err := reportingBreaker.Execute(func() error {
			return resilience.Retry(
				c.Request.Context(),
				3,
				50*time.Millisecond,
				func() error {
					var callErr error

					grpcStart := time.Now()

					resp, callErr = reportingClient.GetUptime(
						c.Request.Context(),
						&reportingv1.UptimeRequest{
							ServerId: c.Param("id"),
							From:     from.Format(time.RFC3339),
							To:       to.Format(time.RFC3339),
						},
					)

					appMetrics.ObserveGRPC(
						"reporting",
						"GetUptime",
						grpcStart,
						callErr,
					)

					return callErr
				},
			)
		})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "reporting service unavailable", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	})

	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("api server started", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

func recordAudit(
	ctx context.Context,
	logger *slog.Logger,
	store *audit.Store,
	actor string,
	role string,
	action string,
	resource string,
	resourceID string,
	success bool,
) {
	err := store.Record(
		ctx,
		audit.Event{
			Actor:      actor,
			Role:       role,
			Action:     action,
			Resource:   resource,
			ResourceID: resourceID,
			Success:    success,
		},
	)

	if err != nil {
		logger.Warn(
			"audit event persistence failed",
			"actor", actor,
			"action", action,
			"resource", resource,
			"resource_id", resourceID,
			"error", err,
		)
	}
}

func indexBestEffort(ctx context.Context, logger *slog.Logger, client *search.Client, index, id string, value any) {
	if err := client.Index(ctx, index, id, value); err != nil {
		logger.Warn("elasticsearch indexing failed", "index", index, "id", id, "error", err)
	}
}
