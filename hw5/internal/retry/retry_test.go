package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryStopsOnSuccess(t *testing.T) {
	calls := 0
	err := Do(context.Background(), 3, time.Nanosecond, func() error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestCancellationInterruptsLongBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := Do(ctx, 3, time.Hour, func() error {
		calls++
		cancel()
		return errors.New("unavailable")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestCanceledContextDoesNotCallOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Do(ctx, 3, time.Second, func() error { t.Fatal("called after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
