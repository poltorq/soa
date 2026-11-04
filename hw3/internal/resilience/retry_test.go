package resilience

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRetryRetriesUnavailable(t *testing.T) {
	interceptor := RetryUnaryClientInterceptor(3, time.Millisecond, time.Second)
	attempts := 0
	err := interceptor(context.Background(), "/test", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			attempts++
			if attempts < 3 {
				return status.Error(codes.Unavailable, "temporary")
			}
			return nil
		})
	if err != nil || attempts != 3 {
		t.Fatalf("expected success after 3 attempts, attempts=%d err=%v", attempts, err)
	}
}

func TestRetryDoesNotRetryBusinessError(t *testing.T) {
	interceptor := RetryUnaryClientInterceptor(3, time.Millisecond, time.Second)
	attempts := 0
	err := interceptor(context.Background(), "/test", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			attempts++
			return status.Error(codes.NotFound, "missing")
		})
	if status.Code(err) != codes.NotFound || attempts != 1 {
		t.Fatalf("expected one attempt and NOT_FOUND, attempts=%d err=%v", attempts, err)
	}
}

func TestBreakerOpensAndRecovers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	breaker := NewBreaker(2, 3, time.Second, logger)
	now := time.Now()
	breaker.now = func() time.Time { return now }
	interceptor := breaker.UnaryClientInterceptor()
	failing := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		return status.Error(codes.Unavailable, "down")
	}
	for range 2 {
		_ = interceptor(context.Background(), "/test", nil, nil, nil, failing)
	}
	if breaker.State() != StateOpen {
		t.Fatalf("expected OPEN, got %s", breaker.State())
	}
	called := false
	err := interceptor(context.Background(), "/test", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			called = true
			return nil
		})
	if status.Code(err) != codes.Unavailable || called {
		t.Fatal("open breaker must reject without invoking the service")
	}
	now = now.Add(2 * time.Second)
	err = interceptor(context.Background(), "/test", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error { return nil })
	if err != nil || breaker.State() != StateClosed {
		t.Fatalf("expected recovery to CLOSED, state=%s err=%v", breaker.State(), err)
	}
}

func TestRetryReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	interceptor := RetryUnaryClientInterceptor(3, time.Second, time.Second)
	err := interceptor(ctx, "/test", nil, nil, nil,
		func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
			return status.Error(codes.Unavailable, "temporary")
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestContextDeadlineIsTransient(t *testing.T) {
	if !retryable(context.DeadlineExceeded) {
		t.Fatal("context deadline must be treated as a transient failure")
	}
}

func TestRetryUsesANewDeadlineForEveryAttempt(t *testing.T) {
	interceptor := RetryUnaryClientInterceptor(3, time.Millisecond, time.Millisecond)
	attempts := 0
	err := interceptor(context.Background(), "/test", nil, nil, nil,
		func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			attempts++
			<-ctx.Done()
			return ctx.Err()
		})
	if !errors.Is(err, context.DeadlineExceeded) || attempts != 3 {
		t.Fatalf("expected three timed-out attempts, attempts=%d err=%v", attempts, err)
	}
}
