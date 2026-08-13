package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"time"

	monitoringv1 "github.com/vmware/terraform-provider-vcd/v3/gen/monitoring/v1"
	"github.com/vmware/terraform-provider-vcd/v3/internal/cache"
	"github.com/vmware/terraform-provider-vcd/v3/internal/config"
	"github.com/vmware/terraform-provider-vcd/v3/internal/database"
	"github.com/vmware/terraform-provider-vcd/v3/internal/grpcmonitoring"
	"github.com/vmware/terraform-provider-vcd/v3/internal/monitoring"
	"github.com/vmware/terraform-provider-vcd/v3/internal/search"
	"google.golang.org/grpc"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := database.EnsureSchema(ctx, pool); err != nil {
		logger.Error("schema setup failed", "error", err)
		os.Exit(1)
	}

	redisCache := cache.New(cfg.RedisAddr)
	defer redisCache.Close()
	postgresStore := database.NewServerStore(pool)
	store := cache.NewServerStore(postgresStore, redisCache, 5*time.Minute, logger)

	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		logger.Error("listen failed", "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	monitoringv1.RegisterMonitoringServiceServer(grpcServer, grpcmonitoring.New(
		monitoring.Checker{Timeout: 2 * time.Second, Workers: 256},
		postgresStore,
		store,
		search.New(cfg.ElasticURL),
		logger,
	))

	logger.Info("monitoring grpc server started", "addr", ":50051")
	if err := grpcServer.Serve(listener); err != nil {
		logger.Error("grpc server failed", "error", err)
		os.Exit(1)
	}
}
