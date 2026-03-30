package connectors

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_concurrentRecordAndAllow(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 5, OpenDuration: 50 * time.Millisecond})
	var wg sync.WaitGroup
	const goroutines = 20
	const iterations = 200

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = cb.Allow()
				_ = cb.State()
				_ = cb.ConsecutiveFailures()
				if i%3 == 0 {
					cb.RecordFailure()
				} else {
					cb.RecordSuccess()
				}
			}
		}(g)
	}
	wg.Wait()
	// No assertion on final state — this test is about detecting races via -race.
}

func TestCircuitBreaker_concurrentStateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerOpts{MaxFailures: 1, OpenDuration: 5 * time.Millisecond})
	var wg sync.WaitGroup

	// One goroutine trips the breaker repeatedly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cb.RecordFailure()
			time.Sleep(time.Millisecond)
		}
	}()

	// Another goroutine resets it repeatedly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cb.RecordSuccess()
			time.Sleep(time.Millisecond)
		}
	}()

	// Another goroutine reads state.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = cb.Allow()
			_ = cb.State()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
}

func TestFactory_CircuitBreakerFor_concurrent(t *testing.T) {
	f := NewFactory(FactoryOpts{
		Timeout: 5 * time.Second,
		CircuitBreaker: CircuitBreakerOpts{
			MaxFailures:  3,
			OpenDuration: 30 * time.Second,
		},
	})
	defer func() { _ = f.Close() }()

	var wg sync.WaitGroup
	sourceIDs := []string{"src-a", "src-b", "src-c", "src-d"}

	for _, id := range sourceIDs {
		for g := 0; g < 10; g++ {
			wg.Add(1)
			go func(sourceID string) {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					cb := f.CircuitBreakerFor(sourceID)
					if cb == nil {
						t.Error("expected non-nil circuit breaker")
						return
					}
					_ = cb.Allow()
					if i%2 == 0 {
						cb.RecordFailure()
					} else {
						cb.RecordSuccess()
					}
				}
			}(id)
		}
	}
	wg.Wait()

	// Verify each source got exactly one breaker instance.
	for _, id := range sourceIDs {
		cb1 := f.CircuitBreakerFor(id)
		cb2 := f.CircuitBreakerFor(id)
		if cb1 != cb2 {
			t.Errorf("source %q: different breaker instances returned", id)
		}
	}
}
