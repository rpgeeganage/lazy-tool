package connectors

import (
	"fmt"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Rejecting calls
	CircuitHalfOpen                     // Allowing one probe call
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerOpts configures a per-source circuit breaker.
type CircuitBreakerOpts struct {
	// MaxFailures is the number of consecutive failures before tripping. 0 disables the breaker.
	MaxFailures int
	// OpenDuration is how long the circuit stays open before transitioning to half-open.
	OpenDuration time.Duration
}

// CircuitBreaker implements a simple per-source circuit breaker.
type CircuitBreaker struct {
	mu           sync.Mutex
	opts         CircuitBreakerOpts
	state        CircuitState
	failures     int
	lastFailedAt time.Time
}

// NewCircuitBreaker creates a circuit breaker. If opts.MaxFailures is 0, the breaker is effectively disabled.
func NewCircuitBreaker(opts CircuitBreakerOpts) *CircuitBreaker {
	if opts.OpenDuration <= 0 {
		opts.OpenDuration = 30 * time.Second
	}
	return &CircuitBreaker{opts: opts, state: CircuitClosed}
}

// Allow checks whether a call is permitted. Returns an error if the circuit is open.
func (cb *CircuitBreaker) Allow() error {
	if cb.opts.MaxFailures <= 0 {
		return nil // disabled
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case CircuitClosed:
		return nil
	case CircuitHalfOpen:
		return nil // allow one probe
	case CircuitOpen:
		if time.Since(cb.lastFailedAt) >= cb.opts.OpenDuration {
			cb.state = CircuitHalfOpen
			return nil
		}
		return fmt.Errorf("circuit open for source (tripped after %d consecutive failures, cooldown %s remaining)",
			cb.opts.MaxFailures, (cb.opts.OpenDuration - time.Since(cb.lastFailedAt)).Truncate(time.Second))
	}
	return nil
}

// RecordSuccess resets the failure count and closes the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	if cb.opts.MaxFailures <= 0 {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = CircuitClosed
}

// RecordFailure increments the failure count and potentially trips the circuit.
func (cb *CircuitBreaker) RecordFailure() {
	if cb.opts.MaxFailures <= 0 {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailedAt = time.Now()
	if cb.failures >= cb.opts.MaxFailures {
		cb.state = CircuitOpen
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	if cb.opts.MaxFailures <= 0 {
		return CircuitClosed
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Check for automatic half-open transition.
	if cb.state == CircuitOpen && time.Since(cb.lastFailedAt) >= cb.opts.OpenDuration {
		cb.state = CircuitHalfOpen
	}
	return cb.state
}

// ConsecutiveFailures returns the current failure count.
func (cb *CircuitBreaker) ConsecutiveFailures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}
