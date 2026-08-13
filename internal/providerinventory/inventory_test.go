package providerinventory

import (
	"context"
	"testing"

	"github.com/vmware/terraform-provider-vcd/v3/internal/server"
)

func TestSyncCreatesProviderResources(t *testing.T) {
	ctx := context.Background()
	store := server.NewMemoryStore()

	source := StaticSource{
		Resources: []Resource{
			{
				ID:      "provider-vm-001",
				Name:    "application-server",
				Address: "10.0.0.10:443",
				Type:    "vm",
			},
			{
				ID:      "provider-vm-002",
				Name:    "database-server",
				Address: "10.0.0.11:5432",
				Type:    "vm",
			},
		},
	}

	result, err := Sync(ctx, source, store)
	if err != nil {
		t.Fatal(err)
	}

	if result.Discovered != 2 {
		t.Fatalf("expected 2 discovered resources, got %d", result.Discovered)
	}

	if result.Created != 2 {
		t.Fatalf("expected 2 created resources, got %d", result.Created)
	}

	servers, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(servers) != 2 {
		t.Fatalf("expected 2 monitored servers, got %d", len(servers))
	}
}

func TestSyncUpdatesExistingResource(t *testing.T) {
	ctx := context.Background()
	store := server.NewMemoryStore()

	_, err := store.Create(ctx, server.Server{
		ID:      "provider-vm-001",
		Name:    "old-name",
		Address: "10.0.0.1:443",
		Status:  "up",
	})
	if err != nil {
		t.Fatal(err)
	}

	source := StaticSource{
		Resources: []Resource{
			{
				ID:      "provider-vm-001",
				Name:    "new-name",
				Address: "10.0.0.2:443",
				Type:    "vm",
			},
		},
	}

	result, err := Sync(ctx, source, store)
	if err != nil {
		t.Fatal(err)
	}

	if result.Updated != 1 {
		t.Fatalf("expected 1 updated resource, got %d", result.Updated)
	}

	got, err := store.Get(ctx, "provider-vm-001")
	if err != nil {
		t.Fatal(err)
	}

	if got.Name != "new-name" {
		t.Fatalf("expected updated name, got %q", got.Name)
	}

	if got.Address != "10.0.0.2:443" {
		t.Fatalf("expected updated address, got %q", got.Address)
	}

	if got.Status != "up" {
		t.Fatalf("expected health status to be preserved, got %q", got.Status)
	}
}

func TestSyncRejectsInvalidProviderResource(t *testing.T) {
	ctx := context.Background()
	store := server.NewMemoryStore()

	source := StaticSource{
		Resources: []Resource{
			{
				ID:   "provider-vm-001",
				Name: "missing-address",
				Type: "vm",
			},
		},
	}

	result, err := Sync(ctx, source, store)
	if err != nil {
		t.Fatal(err)
	}

	if result.Failed != 1 {
		t.Fatalf("expected 1 failed resource, got %d", result.Failed)
	}
}
