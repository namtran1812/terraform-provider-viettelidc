package grpcreporting

import (
	"context"
	"time"

	reportingv1 "github.com/vmware/terraform-provider-vcd/v3/gen/reporting/v1"
	"github.com/vmware/terraform-provider-vcd/v3/internal/database"
)

type Service struct {
	reportingv1.UnimplementedReportingServiceServer
	db *database.ServerStore
}

func New(db *database.ServerStore) *Service {
	return &Service{db: db}
}

func (s *Service) GetUptime(ctx context.Context, req *reportingv1.UptimeRequest) (*reportingv1.UptimeResponse, error) {
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)

	if req.GetFrom() != "" {
		parsed, err := time.Parse(time.RFC3339, req.GetFrom())
		if err != nil {
			return nil, err
		}
		from = parsed
	}
	if req.GetTo() != "" {
		parsed, err := time.Parse(time.RFC3339, req.GetTo())
		if err != nil {
			return nil, err
		}
		to = parsed
	}

	report, err := s.db.UptimeReport(ctx, req.GetServerId(), from, to)
	if err != nil {
		return nil, err
	}

	return &reportingv1.UptimeResponse{
		ServerId:      report.ServerID,
		Checks:        int64(report.Checks),
		Successful:    int64(report.Successful),
		UptimePercent: report.UptimePercent,
	}, nil
}
