package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"lazy-tool/internal/connectors"
	"lazy-tool/pkg/models"
)

func TestRecordCircuitAndTrace_FailureIncrements(t *testing.T) {
	cb := connectors.NewCircuitBreaker(connectors.CircuitBreakerOpts{MaxFailures: 5, OpenDuration: time.Hour})
	pr := proxyResult{
		rec: models.CapabilityRecord{SourceID: "s1", OriginalName: "tool"},
		cb:  cb,
	}
	ctx := context.Background()
	log := slog.Default()
	recordCircuitAndTrace(pr, log, ctx, "s1__tool", fmt.Errorf("upstream error"), "")
	if cb.ConsecutiveFailures() != 1 {
		t.Fatalf("expected 1 failure, got %d", cb.ConsecutiveFailures())
	}
}

func TestRecordCircuitAndTrace_SuccessKeepsClosed(t *testing.T) {
	cb := connectors.NewCircuitBreaker(connectors.CircuitBreakerOpts{MaxFailures: 3, OpenDuration: time.Hour})
	pr := proxyResult{
		rec: models.CapabilityRecord{SourceID: "s1", OriginalName: "tool"},
		cb:  cb,
	}
	ctx := context.Background()
	log := slog.Default()
	recordCircuitAndTrace(pr, log, ctx, "s1__tool", nil, "")
	if cb.State() != connectors.CircuitClosed {
		t.Fatalf("expected closed, got %v", cb.State())
	}
	if cb.ConsecutiveFailures() != 0 {
		t.Fatalf("expected 0 failures, got %d", cb.ConsecutiveFailures())
	}
}

func TestRecordCircuitAndTrace_TripsCircuit(t *testing.T) {
	cb := connectors.NewCircuitBreaker(connectors.CircuitBreakerOpts{MaxFailures: 2, OpenDuration: time.Hour})
	pr := proxyResult{
		rec: models.CapabilityRecord{SourceID: "s1", OriginalName: "tool"},
		cb:  cb,
	}
	ctx := context.Background()
	log := slog.Default()
	recordCircuitAndTrace(pr, log, ctx, "s1__tool", fmt.Errorf("fail1"), "")
	recordCircuitAndTrace(pr, log, ctx, "s1__tool", fmt.Errorf("fail2"), "")
	if cb.State() != connectors.CircuitOpen {
		t.Fatalf("expected open, got %v", cb.State())
	}
}
