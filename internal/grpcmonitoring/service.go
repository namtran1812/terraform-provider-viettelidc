package grpcmonitoring

import (
	"context"
	"io"
	"log/slog"

	"github.com/google/uuid"
	monitoringv1 "github.com/vmware/terraform-provider-vcd/v3/gen/monitoring/v1"
	"github.com/vmware/terraform-provider-vcd/v3/internal/database"
	"github.com/vmware/terraform-provider-vcd/v3/internal/monitoring"
	"github.com/vmware/terraform-provider-vcd/v3/internal/search"
	"github.com/vmware/terraform-provider-vcd/v3/internal/server"
)

type Service struct {
	monitoringv1.UnimplementedMonitoringServiceServer
	checker monitoring.Checker
	db      *database.ServerStore
	store   server.Store
	search  *search.Client
	logger  *slog.Logger
}

func New(checker monitoring.Checker, db *database.ServerStore, store server.Store, searchClient *search.Client, logger *slog.Logger) *Service {
	return &Service{checker: checker, db: db, store: store, search: searchClient, logger: logger}
}

func (s *Service) Check(ctx context.Context, req *monitoringv1.CheckRequest) (*monitoringv1.CheckResponse, error) {
	result := s.checker.Check(ctx, req.GetServerId(), req.GetAddress())

	if err := s.db.RecordHealthCheck(ctx, result.ServerID, result.Up, result.LatencyMS, result.CheckedAt, result.Error); err != nil {
		return nil, err
	}

	status := "down"
	if result.Up {
		status = "up"
	}
	updated, err := s.store.Update(ctx, result.ServerID, server.Server{Status: status})
	if err != nil {
		return nil, err
	}

	if s.search != nil {
		if err := s.search.Index(ctx, "health-checks", uuid.NewString(), result); err != nil {
			s.logger.Warn("health check indexing failed", "server_id", result.ServerID, "error", err)
		}
		if err := s.search.Index(ctx, "servers", updated.ID, updated); err != nil {
			s.logger.Warn("server indexing failed", "server_id", updated.ID, "error", err)
		}
	}

	return &monitoringv1.CheckResponse{
		ServerId:  result.ServerID,
		Up:        result.Up,
		LatencyMs: result.LatencyMS,
		CheckedAt: result.CheckedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Error:     result.Error,
	}, nil
}

func (s *Service) CheckBatch(stream monitoringv1.MonitoringService_CheckBatchServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		resp, err := s.Check(stream.Context(), req)
		if err != nil {
			return err
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}
