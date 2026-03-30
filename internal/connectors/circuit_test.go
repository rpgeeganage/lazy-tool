package connectors

import (
	"testing"
	"time"
)

func TestCircuitBreaker_disabledWhenMaxFailuresZero(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 0})
	for i := 0; i < 100; i++ {
		cb.RecordFailure()
	}
	if err := cb.Allow(); err != nil {
		t.Fatalf("disabled breaker should always allow: %v", err)
	}
	if cb.State() != CircuitClosed {
		t.Fatalf("disabled breaker state: %v", cb.State())
	}
}

func TestCircuitBreaker_tripsAfterMaxFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 3, OpenDuration: 1 * time.Hour})
	cb.RecordFailure()
	cb.RecordFailure()
	if err := cb.Allow(); err != nil {
		t.Fatalf("should allow before max failures: %v", err)
	}
	cb.RecordFailure() // 3rd failure — trips
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %v", cb.State())
	}
	if err := cb.Allow(); err == nil {
		t.Fatal("should reject when open")
	}
}

func TestCircuitBreaker_successResets(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 2, OpenDuration: 1 * time.Hour})
	cb.RecordFailure()
	cb.RecordSuccess()
	if cb.ConsecutiveFailures() != 0 {
		t.Fatalf("failures should reset: %d", cb.ConsecutiveFailures())
	}
	cb.RecordFailure() // only 1 failure after reset
	if cb.State() != CircuitClosed {
		t.Fatalf("should still be closed: %v", cb.State())
	}
}

func TestCircuitBreaker_halfOpenAfterCooldown(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 1, OpenDuration: 10 * time.Millisecond})
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %v", cb.State())
	}
	time.Sleep(20 * time.Millisecond)
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open after cooldown, got %v", cb.State())
	}
	if err := cb.Allow(); err != nil {
		t.Fatalf("half-open should allow probe: %v", err)
	}
}

func TestCircuitBreaker_halfOpenSuccessCloses(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 1, OpenDuration: 10 * time.Millisecond})
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	_ = cb.Allow() // transitions to half-open
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Fatalf("expected closed after half-open success, got %v", cb.State())
	}
}

func TestCircuitBreaker_halfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 1, OpenDuration: 10 * time.Millisecond})
	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	_ = cb.Allow() // transitions to half-open
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected re-open after half-open failure, got %v", cb.State())
	}
}

func TestCircuitState_String(t *testing.T) {
	cases := []struct {
		s    CircuitState
		want string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}
