package providerinventory

import (
	"context"
	"fmt"
	"time"

	"github.com/vmware/terraform-provider-vcd/v3/internal/server"
)

// Resource represents the minimal infrastructure metadata needed by the
// monitoring control plane. Provider-specific objects are translated into
// this representation before synchronization.
type Resource struct {
	ID      string
	Name    string
	Address string
	Type    string
}

// Source discovers infrastructure resources managed by a provider.
type Source interface {
	ListResources(context.Context) ([]Resource, error)
}

// SyncResult summarizes an inventory synchronization.
type SyncResult struct {
	Discovered int `json:"discovered"`
	Created    int `json:"created"`
	Updated    int `json:"updated"`
	Failed     int `json:"failed"`
}

// Sync imports provider-managed infrastructure into the monitoring inventory.
//
// Existing resources are updated in place. Newly discovered resources are
// registered with an unknown health state so the monitoring service can
// subsequently probe them.
func Sync(ctx context.Context, source Source, store server.Store) (SyncResult, error) {
	resources, err := source.ListResources(ctx)
	if err != nil {
		return SyncResult{}, fmt.Errorf("discover provider resources: %w", err)
	}

	result := SyncResult{Discovered: len(resources)}

	for _, resource := range resources {
		if resource.ID == "" || resource.Address == "" {
			result.Failed++
			continue
		}

		current, err := store.Get(ctx, resource.ID)
		if err == nil {
			current.Name = resource.Name
			current.Address = resource.Address

			if _, err := store.Update(ctx, resource.ID, current); err != nil {
				result.Failed++
				continue
			}

			result.Updated++
			continue
		}

		_, err = store.Create(ctx, server.Server{
			ID:        resource.ID,
			Name:      resource.Name,
			Address:   resource.Address,
			Status:    "unknown",
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			result.Failed++
			continue
		}

		result.Created++
	}

	return result, nil
}
