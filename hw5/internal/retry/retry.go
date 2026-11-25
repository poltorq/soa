package retry

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

func Do(ctx context.Context, attempts int, base time.Duration, operation func() error) error {
	if attempts < 1 || attempts > 10 || base <= 0 {
		return fmt.Errorf("invalid retry policy")
	}
	delay := min(base, 30*time.Second)
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = operation()
		if lastErr == nil || attempt+1 == attempts {
			return lastErr
		}
		wait := delay/2 + time.Duration(rand.Int64N(int64(delay-delay/2)))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(2*delay, 30*time.Second)
	}
	return lastErr
}
