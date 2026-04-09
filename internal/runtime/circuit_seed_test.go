package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

type seedFactory struct {
	seeded map[string]connectors.CircuitSnapshot
}

func (f *seedFactory) New(ctx context.Context, src models.Source) (connectors.Connector, error) {
	_, _ = ctx, src
	return nil, nil
}

func (f *seedFactory) CircuitBreakerFor(sourceID string) *connectors.CircuitBreaker {
	_ = sourceID
	return nil
}

func (f *seedFactory) SeedCircuitBreaker(sourceID string, state connectors.CircuitState, failures int, lastFailedAt time.Time) {
	if f.seeded == nil {
		f.seeded = make(map[string]connectors.CircuitSnapshot)
	}
	f.seeded[sourceID] = connectors.CircuitSnapshot{State: state, Failures: failures, LastFailedAt: lastFailedAt}
}

func (f *seedFactory) Close() error { return nil }

func TestSeedCircuitBreakers_LoadsPersistedState(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	failedAt := time.Date(2026, 4, 9, 10, 0, 0, 0, time.UTC)
	if err := st.UpdateCircuitState(ctx, "src1", "open", 2, &failedAt); err != nil {
		t.Fatal(err)
	}
	reg, err := app.NewSourceRegistry([]models.Source{{ID: "src1", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}})
	if err != nil {
		t.Fatal(err)
	}
	fact := &seedFactory{}
	seedCircuitBreakers(st, reg, fact)
	snap, ok := fact.seeded["src1"]
	if !ok {
		t.Fatal("expected seeded circuit state")
	}
	if snap.State != connectors.CircuitOpen || snap.Failures != 2 || !snap.LastFailedAt.Equal(failedAt) {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
}
