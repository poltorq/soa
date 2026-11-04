package resilience

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type BreakerState string

const (
	StateClosed   BreakerState = "CLOSED"
	StateOpen     BreakerState = "OPEN"
	StateHalfOpen BreakerState = "HALF_OPEN"
)

type Breaker struct {
	mu            sync.Mutex
	state         BreakerState
	threshold     int
	windowSize    int
	outcomes      []bool
	openTimeout   time.Duration
	openedAt      time.Time
	probeInFlight bool
	logger        *slog.Logger
	now           func() time.Time
}

func NewBreaker(threshold, windowSize int, openTimeout time.Duration, logger *slog.Logger) *Breaker {
	return &Breaker{
		state:       StateClosed,
		threshold:   threshold,
		windowSize:  windowSize,
		outcomes:    make([]bool, 0, windowSize),
		openTimeout: openTimeout,
		logger:      logger,
		now:         time.Now,
	}
}

func (b *Breaker) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if err := b.beforeCall(); err != nil {
			return err
		}
		err := invoker(ctx, method, req, reply, cc, opts...)
		b.afterCall(err)
		return err
	}
}

func (b *Breaker) State() BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) beforeCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return nil
	case StateOpen:
		if b.now().Sub(b.openedAt) < b.openTimeout {
			return status.Error(codes.Unavailable, "flight service circuit breaker is open")
		}
		b.transition(StateHalfOpen)
		b.probeInFlight = true
		return nil
	case StateHalfOpen:
		if b.probeInFlight {
			return status.Error(codes.Unavailable, "flight service circuit breaker is half-open")
		}
		b.probeInFlight = true
		return nil
	default:
		return status.Error(codes.Unavailable, "flight service circuit breaker is unavailable")
	}
}

func (b *Breaker) afterCall(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	failed := retryable(err)
	if b.state == StateHalfOpen {
		b.probeInFlight = false
		if failed {
			b.openedAt = b.now()
			b.transition(StateOpen)
			return
		}
		b.outcomes = b.outcomes[:0]
		b.transition(StateClosed)
		return
	}
	if b.state != StateClosed {
		return
	}
	b.outcomes = append(b.outcomes, failed)
	if len(b.outcomes) > b.windowSize {
		b.outcomes = b.outcomes[1:]
	}
	failures := 0
	for _, outcome := range b.outcomes {
		if outcome {
			failures++
		}
	}
	if failures >= b.threshold {
		b.openedAt = b.now()
		b.transition(StateOpen)
	}
}

func (b *Breaker) transition(next BreakerState) {
	previous := b.state
	if previous == next {
		return
	}
	b.state = next
	if next == StateOpen {
		b.outcomes = b.outcomes[:0]
	}
	b.logger.Info("circuit breaker state changed", "from", previous, "to", next)
}
