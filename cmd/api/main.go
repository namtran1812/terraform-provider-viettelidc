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
	monitoringv1 "github.com/vmware/terraform-provider-vcd/v3/gen/monitoring/v1"
	reportingv1 "github.com/vmware/terraform-provider-vcd/v3/gen/reporting/v1"
	"github.com/vmware/terraform-provider-vcd/v3/internal/auth"
	"github.com/vmware/terraform-provider-vcd/v3/internal/cache"
	"github.com/vmware/terraform-provider-vcd/v3/internal/config"
	"github.com/vmware/terraform-provider-vcd/v3/internal/database"
	"github.com/vmware/terraform-provider-vcd/v3/internal/logging"
	"github.com/vmware/terraform-provider-vcd/v3/internal/search"
	srv "github.com/vmware/terraform-provider-vcd/v3/internal/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

	redisCache := cache.New(cfg.RedisAddr)
	defer redisCache.Close()
	if err := redisCache.Ping(ctx); err != nil {
		logger.Warn("redis unavailable; requests will continue using postgres", "error", err)
	}

	postgresStore := database.NewServerStore(pool)
	store := cache.NewServerStore(postgresStore, redisCache, 5*time.Minute, logger)
	elastic := search.New(cfg.ElasticURL)

	monitoringConn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("monitoring grpc client setup failed", "error", err)
		os.Exit(1)
	}
	defer monitoringConn.Close()
	reportingConn, err := grpc.NewClient("localhost:50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("reporting grpc client setup failed", "error", err)
		os.Exit(1)
	}
	defer reportingConn.Close()

	monitoringClient := monitoringv1.NewMonitoringServiceClient(monitoringConn)
	reportingClient := reportingv1.NewReportingServiceClient(reportingConn)

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		if err := pool.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "postgres": "down"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/auth/token", func(c *gin.Context) {
		token, err := auth.Issue(cfg.JWTSecret, "operator")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	v1 := r.Group("/v1", auth.Middleware(cfg.JWTSecret))

	v1.POST("/servers", func(c *gin.Context) {
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
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		indexBestEffort(c.Request.Context(), logger, elastic, "servers", out.ID, out)
		c.JSON(http.StatusCreated, out)
	})

	v1.GET("/servers", func(c *gin.Context) {
		out, err := store.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	})

	v1.GET("/servers/:id", func(c *gin.Context) {
		out, err := store.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		c.JSON(http.StatusOK, out)
	})

	v1.PATCH("/servers/:id", func(c *gin.Context) {
		var patch srv.Server
		if err := c.ShouldBindJSON(&patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		out, err := store.Update(c.Request.Context(), c.Param("id"), patch)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		indexBestEffort(c.Request.Context(), logger, elastic, "servers", out.ID, out)
		c.JSON(http.StatusOK, out)
	})

	v1.POST("/servers/:id/check", func(c *gin.Context) {
		s, err := store.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		resp, err := monitoringClient.Check(c.Request.Context(), &monitoringv1.CheckRequest{ServerId: s.ID, Address: s.Address})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "monitoring service unavailable", "detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	})

	v1.GET("/servers/:id/report", func(c *gin.Context) {
		to := time.Now().UTC()
		from := to.Add(-24 * time.Hour)
		resp, err := reportingClient.GetUptime(c.Request.Context(), &reportingv1.UptimeRequest{
			ServerId: c.Param("id"),
			From:     from.Format(time.RFC3339),
			To:       to.Format(time.RFC3339),
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

func indexBestEffort(ctx context.Context, logger *slog.Logger, client *search.Client, index, id string, value any) {
	if err := client.Index(ctx, index, id, value); err != nil {
		logger.Warn("elasticsearch indexing failed", "index", index, "id", id, "error", err)
	}
}
