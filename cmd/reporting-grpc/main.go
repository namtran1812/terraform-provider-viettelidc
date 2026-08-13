package main

import (
	"context"
	"log/slog"
	"net"
	"os"

	reportingv1 "github.com/vmware/terraform-provider-vcd/v3/gen/reporting/v1"
	"github.com/vmware/terraform-provider-vcd/v3/internal/config"
	"github.com/vmware/terraform-provider-vcd/v3/internal/database"
	"github.com/vmware/terraform-provider-vcd/v3/internal/grpcreporting"
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

	listener, err := net.Listen("tcp", ":50052")
	if err != nil {
		logger.Error("listen failed", "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	reportingv1.RegisterReportingServiceServer(grpcServer, grpcreporting.New(database.NewServerStore(pool)))

	logger.Info("reporting grpc server started", "addr", ":50052")
	if err := grpcServer.Serve(listener); err != nil {
		logger.Error("grpc server failed", "error", err)
		os.Exit(1)
	}
}
