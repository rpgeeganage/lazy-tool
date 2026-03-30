package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"lazy-tool/internal/connectors"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/vector"
	"lazy-tool/pkg/models"
)

type stubFactory struct {
	closeErr error
	closed   int
}

func (f *stubFactory) New(ctx context.Context, src models.Source) (connectors.Connector, error) {
	_, _ = ctx, src
	panic("stubFactory.New not used in Close tests")
}

func (f *stubFactory) CircuitBreakerFor(sourceID string) *connectors.CircuitBreaker {
	return nil
}

func (f *stubFactory) Close() error {
	f.closed++
	return f.closeErr
}

func TestStack_Close_nilFields(t *testing.T) {
	var s Stack
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStack_Close_order_invokes_factory_before_vector_and_store(t *testing.T) {
	ctx := context.Background()
	vi, err := vector.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "stack-close.db"))
	if err != nil {
		t.Fatal(err)
	}
	fact := &stubFactory{}
	s := &Stack{Factory: fact, Vec: vi, Store: st}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if fact.closed != 1 {
		t.Fatalf("factory Close calls = %d", fact.closed)
	}
	if _, qerr := vi.Query(ctx, []float32{1, 0, 0}, 5, ""); !errors.Is(qerr, vector.ErrClosed) {
		t.Fatalf("vector Close: %v", qerr)
	}
	if _, gerr := st.GetCapability(ctx, "missing"); gerr == nil {
		t.Fatal("expected store closed")
	}
}

func TestStack_Close_factoryError_still_closes_downstream(t *testing.T) {
	ctx := context.Background()
	vi, err := vector.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "stack-close2.db"))
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("factory failed")
	s := &Stack{Factory: &stubFactory{closeErr: want}, Vec: vi, Store: st}
	if err := s.Close(); !errors.Is(err, want) {
		t.Fatalf("Close err = %v want %v", err, want)
	}
	if _, qerr := vi.Query(ctx, []float32{1, 0, 0}, 5, ""); !errors.Is(qerr, vector.ErrClosed) {
		t.Fatalf("vector not closed: %v", qerr)
	}
	if _, gerr := st.GetCapability(ctx, "missing"); gerr == nil {
		t.Fatal("expected store closed after factory error")
	}
}
