package firewall

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetry_Success(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = time.Millisecond

	count := 0
	err := Retry(context.Background(), cfg, func() error {
		count++
		return nil
	})

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 attempt, got %d", count)
	}
}

func TestRetry_FailThenSuccess(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = time.Millisecond
	cfg.MaxAttempts = 3

	count := 0
	err := Retry(context.Background(), cfg, func() error {
		count++
		if count < 2 {
			return errors.New("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 attempts, got %d", count)
	}
}

func TestRetry_FailMaxAttempts(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = time.Millisecond
	cfg.MaxAttempts = 3

	expectedErr := errors.New("permanent error")
	count := 0

	err := Retry(context.Background(), cfg, func() error {
		count++
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if count != 3 {
		t.Errorf("expected 3 attempts, got %d", count)
	}
}

func TestRetry_NonRetryable(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = time.Millisecond
	cfg.RetryableErrors = []error{ErrTemporary}

	count := 0
	err := Retry(context.Background(), cfg, func() error {
		count++
		return errors.New("fatal problem")
	})

	if err == nil {
		t.Error("expected error")
	}
	if count != 1 {
		t.Errorf("expected 1 attempt, got %d", count)
	}
}

func TestRetry_ContextCancel(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = 100 * time.Millisecond // Long enough to cancel

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately/soon
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, cfg, func() error {
		return errors.New("fail")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context canceled, got %v", err)
	}
}

func TestRetryWithResult(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.InitialDelay = time.Millisecond

	res, err := RetryWithResult(context.Background(), cfg, func() (int, error) {
		return 42, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != 42 {
		t.Errorf("expected 42, got %d", res)
	}
}

func TestWrapTemporary(t *testing.T) {
	base := errors.New("foo")
	wrapped := WrapTemporary(base)

	if !errors.Is(wrapped, ErrTemporary) {
		t.Error("should match ErrTemporary")
	}

	if wrapped.Error() != "foo" {
		t.Error("should preserve error message")
	}

	if errors.Unwrap(wrapped) != base {
		t.Error("should unwrap to base")
	}
}
