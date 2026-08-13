package providerinventory

import "context"

// StaticSource is a deterministic provider inventory implementation used for
// local development and integration testing. Production provider adapters can
// implement the same Source interface.
type StaticSource struct {
	Resources []Resource
}

func (s StaticSource) ListResources(context.Context) ([]Resource, error) {
	out := make([]Resource, len(s.Resources))
	copy(out, s.Resources)
	return out, nil
}
