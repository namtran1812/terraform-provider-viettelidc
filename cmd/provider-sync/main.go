package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/vmware/terraform-provider-vcd/v3/internal/cache"
	"github.com/vmware/terraform-provider-vcd/v3/internal/database"
	"github.com/vmware/terraform-provider-vcd/v3/internal/providerinventory"
	"github.com/vmware/terraform-provider-vcd/v3/vcd"
)

func main() {
	ctx := context.Background()
	logger := slog.New(
		slog.NewJSONHandler(os.Stdout, nil),
	)

	databaseURL := env(
		"DATABASE_URL",
		"postgres://postgres:postgres@localhost:5432/monitor?sslmode=disable",
	)

	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		logger.Error(
			"postgres connection failed",
			"error",
			err,
		)
		os.Exit(1)
	}
	defer pool.Close()

	if err := database.EnsureSchema(ctx, pool); err != nil {
		logger.Error(
			"postgres schema setup failed",
			"error",
			err,
		)
		os.Exit(1)
	}

	redisCache := cache.New(
		env("REDIS_ADDR", "localhost:6379"),
	)
	defer redisCache.Close()

	postgresStore := database.NewServerStore(pool)

	store := cache.NewServerStore(
		postgresStore,
		redisCache,
		5*time.Minute,
		logger,
	)

	insecure, err := strconv.ParseBool(
		env("VCD_ALLOW_UNVERIFIED_SSL", "false"),
	)
	if err != nil {
		logger.Error(
			"invalid VCD_ALLOW_UNVERIFIED_SSL",
			"error",
			err,
		)
		os.Exit(1)
	}

	cfg := vcd.Config{
		User:            os.Getenv("VCD_USER"),
		Password:        os.Getenv("VCD_PASSWORD"),
		Token:           os.Getenv("VCD_TOKEN"),
		ApiToken:        os.Getenv("VCD_API_TOKEN"),
		SysOrg:          os.Getenv("VCD_SYSORG"),
		Org:             os.Getenv("VCD_ORG"),
		Vdc:             os.Getenv("VCD_VDC"),
		Href:            os.Getenv("VCD_URL"),
		InsecureFlag:    insecure,
		MaxRetryTimeout: 60,
	}

	if cfg.SysOrg == "" {
		cfg.SysOrg = cfg.Org
	}

	client, err := cfg.Client()
	if err != nil {
		logger.Error(
			"VCD authentication failed",
			"error",
			err,
		)
		os.Exit(1)
	}

	source := vcd.VCDInventorySource{
		Client: client,
		Org:    cfg.Org,
		Vdc:    cfg.Vdc,
		Port:   env("MONITOR_PORT", "22"),
	}

	result, err := providerinventory.Sync(
		ctx,
		source,
		store,
	)
	if err != nil {
		logger.Error(
			"provider inventory sync failed",
			"error",
			err,
		)
		os.Exit(1)
	}

	logger.Info(
		"provider inventory synchronized",
		"discovered",
		result.Discovered,
		"created",
		result.Created,
		"updated",
		result.Updated,
		"failed",
		result.Failed,
	)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
