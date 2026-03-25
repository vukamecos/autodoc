// Package circuitbreaker provides a circuit breaker pattern implementation
// for protecting ACP/Ollama calls from cascading failures.
package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed allows requests to pass through.
	StateClosed State = iota
	// StateOpen rejects requests immediately (fail-fast).
	StateOpen
	// StateHalfOpen allows a test request to check if service recovered.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config configures the circuit breaker behavior.
type Config struct {
	// FailureThreshold is the number of consecutive failures before opening the circuit.
	// Default: 5
	FailureThreshold uint32

	// SuccessThreshold is the number of consecutive successes in half-open state
	// before closing the circuit. Default: 2
	SuccessThreshold uint32

	// Timeout is the duration the circuit stays open before transitioning to half-open.
	// Default: 30 seconds
	Timeout time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
	}
}

// CircuitBreaker protects a function from cascading failures.
type CircuitBreaker struct {
	config Config

	mu                sync.RWMutex
	state             State
	failures          uint32
	successes         uint32
	lastFailureTime   time.Time
	lastStateChange   time.Time

	// onStateChange is called when state transitions (for logging/metrics)
	onStateChange func(from, to State)
}

// New creates a new CircuitBreaker with the given config.
func New(config Config) *CircuitBreaker {
	return &CircuitBreaker{
		config:        config,
		state:         StateClosed,
		lastStateChange: time.Now(),
	}
}

// NewWithCallback creates a CircuitBreaker with a state change callback.
func NewWithCallback(config Config, onStateChange func(from, to State)) *CircuitBreaker {
	cb := New(config)
	cb.onStateChange = onStateChange
	return cb
}

// ErrOpenCircuit is returned when the circuit is open.
var ErrOpenCircuit = errors.New("circuit breaker is open")

// Execute runs the given function if the circuit allows it.
// If the circuit is open, it returns ErrOpenCircuit immediately.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	state := cb.currentState()

	switch state {
	case StateOpen:
		return ErrOpenCircuit

	case StateHalfOpen:
		// In half-open state, we allow the request through
		// but track if it succeeds
		err := fn()
		cb.recordResult(err)
		return err

	case StateClosed:
		// Normal operation
		err := fn()
		cb.recordResult(err)
		return err

	default:
		return fmt.Errorf("unknown circuit breaker state: %v", state)
	}
}

// currentState returns the current state, handling timeout transitions.
func (cb *CircuitBreaker) currentState() State {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailureTime
	cb.mu.RUnlock()

	// Only transition if circuit has been open longer than timeout
	if state == StateOpen && !lastFailure.IsZero() && time.Since(lastFailure) > cb.config.Timeout {
		// Transition to half-open
		cb.mu.Lock()
		// Double-check after acquiring lock
		if cb.state == StateOpen && !cb.lastFailureTime.IsZero() && time.Since(cb.lastFailureTime) > cb.config.Timeout {
			cb.transitionTo(StateHalfOpen)
		}
		state = cb.state
		cb.mu.Unlock()
	}

	return state
}

// recordResult updates the circuit breaker state based on the result.
func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		if err != nil {
			cb.failures++
			cb.lastFailureTime = time.Now()
			if cb.failures >= cb.config.FailureThreshold {
				cb.transitionTo(StateOpen)
			}
		} else {
			cb.failures = 0 // Reset on success
		}

	case StateHalfOpen:
		if err != nil {
			// Back to open
			cb.failures = 1
			cb.successes = 0
			cb.lastFailureTime = time.Now()
			cb.transitionTo(StateOpen)
		} else {
			cb.successes++
			if cb.successes >= cb.config.SuccessThreshold {
				// Success threshold reached, close the circuit
				cb.failures = 0
				cb.successes = 0
				cb.transitionTo(StateClosed)
			}
		}

	case StateOpen:
		// Should not happen, but handle gracefully
		if err != nil {
			cb.lastFailureTime = time.Now()
		}
	}
}

// transitionTo changes the state and triggers the callback if set.
func (cb *CircuitBreaker) transitionTo(newState State) {
	oldState := cb.state
	if oldState == newState {
		return
	}

	cb.state = newState
	cb.lastStateChange = time.Now()

	if cb.onStateChange != nil {
		cb.onStateChange(oldState, newState)
	}
}

// State returns the current state (for monitoring).
func (cb *CircuitBreaker) State() State {
	return cb.currentState()
}

// Stats returns current statistics (for monitoring).
func (cb *CircuitBreaker) Stats() (state State, failures, successes uint32, stateChangedAgo time.Duration) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state, cb.failures, cb.successes, time.Since(cb.lastStateChange)
}

// Reset forces the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionTo(StateClosed)
	cb.failures = 0
	cb.successes = 0
}
