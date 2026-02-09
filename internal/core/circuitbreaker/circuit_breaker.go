package circuitbreaker

import (
	"context"
	"errors"
	"time"

	"github.com/sony/gobreaker"
)

var (
	ErrCircuitOpen = errors.New("circuit breaker is open")
)

type CircuitBreaker struct {
	cb *gobreaker.CircuitBreaker
}

// New creates a new circuit breaker with default settings
func New(name string) *CircuitBreaker {
	settings := gobreaker.Settings{
		Name:        name,
		MaxRequests: 3,
		Interval:    time.Second * 60,
		Timeout:     time.Second * 30,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Log state changes
			// logger.Info("Circuit breaker state changed", "name", name, "from", from, "to", to)
		},
	}

	return &CircuitBreaker{
		cb: gobreaker.NewCircuitBreaker(settings),
	}
}

// Execute runs the function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	_, err := cb.cb.Execute(func() (interface{}, error) {
		return nil, fn()
	})

	if err == gobreaker.ErrOpenState {
		return ErrCircuitOpen
	}

	return err
}

// ExecuteWithFallback runs the function with circuit breaker and fallback
func (cb *CircuitBreaker) ExecuteWithFallback(ctx context.Context, fn func() error, fallback func() error) error {
	err := cb.Execute(ctx, fn)
	if err == ErrCircuitOpen && fallback != nil {
		return fallback()
	}
	return err
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() gobreaker.State {
	return cb.cb.State()
}
