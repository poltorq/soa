package resilience

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func RetryUnaryClientInterceptor(maxAttempts int, initialBackoff, attemptTimeout time.Duration) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		var err error
		for attempt := 0; attempt < maxAttempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
			err = invoker(attemptCtx, method, req, reply, cc, opts...)
			cancel()
			if err == nil || !retryable(err) || attempt == maxAttempts-1 {
				return err
			}
			wait := initialBackoff * time.Duration(1<<attempt)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		return err
	}
}

func retryable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	code := status.Code(err)
	return code == codes.Unavailable || code == codes.DeadlineExceeded
}
