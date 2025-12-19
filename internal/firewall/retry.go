package firewall

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	Jitter          bool
	RetryableErrors []error
}

// DefaultRetryConfig returns sensible defaults for network operations.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
}

// Retry executes a function with exponential backoff retry.
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Execute the function
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryable(err, cfg.RetryableErrors) {
			return err
		}

		// Don't sleep after the last attempt
		if attempt == cfg.MaxAttempts-1 {
			break
		}

		// Calculate delay with exponential backoff
		delay := calculateDelay(attempt, cfg)

		// Wait before retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return lastErr
}

// RetryWithResult executes a function that returns a result with retry.
func RetryWithResult[T any](ctx context.Context, cfg RetryConfig, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		var err error
		result, err = fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		if !isRetryable(err, cfg.RetryableErrors) {
			return result, err
		}

		if attempt == cfg.MaxAttempts-1 {
			break
		}

		delay := calculateDelay(attempt, cfg)
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(delay):
		}
	}

	return result, lastErr
}

func calculateDelay(attempt int, cfg RetryConfig) time.Duration {
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.BackoffFactor, float64(attempt))

	if cfg.Jitter {
		// Add up to 25% jitter
		jitter := delay * 0.25 * rand.Float64()
		delay += jitter
	}

	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	return time.Duration(delay)
}

func isRetryable(err error, retryableErrors []error) bool {
	// If no specific errors defined, retry all errors
	if len(retryableErrors) == 0 {
		return true
	}

	for _, retryable := range retryableErrors {
		if errors.Is(err, retryable) {
			return true
		}
	}

	return false
}

// Common retryable error types
var (
	ErrTemporary    = errors.New("temporary error")
	ErrNetworkError = errors.New("network error")
	ErrTimeout      = errors.New("timeout")
)

// WrapTemporary wraps an error as temporary/retryable.
func WrapTemporary(err error) error {
	return &temporaryError{err: err}
}

type temporaryError struct {
	err error
}

func (e *temporaryError) Error() string {
	return e.err.Error()
}

func (e *temporaryError) Unwrap() error {
	return e.err
}

func (e *temporaryError) Is(target error) bool {
	return target == ErrTemporary
}
