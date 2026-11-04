package flight

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAPIKeyInterceptor(t *testing.T) {
	interceptor := APIKeyUnaryServerInterceptor("test-service-key")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "test-service-key"))
	called := false
	_, err := interceptor(ctx, nil, nil, func(context.Context, any) (any, error) {
		called = true
		return "ok", nil
	})
	if err != nil || !called {
		t.Fatalf("expected authorized handler call, called=%v err=%v", called, err)
	}
}

func TestAPIKeyInterceptorRejectsMissingKey(t *testing.T) {
	interceptor := APIKeyUnaryServerInterceptor("test-service-key")
	_, err := interceptor(context.Background(), nil, nil, func(context.Context, any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected UNAUTHENTICATED, got %v", err)
	}
}
