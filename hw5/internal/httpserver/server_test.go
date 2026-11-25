package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestHealthURLUsesConfiguredPort(t *testing.T) {
	for _, test := range []struct{ address, want string }{
		{":9098", "http://127.0.0.1:9098/health"}, {"0.0.0.0:9098", "http://127.0.0.1:9098/health"},
		{"[::]:9098", "http://127.0.0.1:9098/health"}, {"127.0.0.1:1234", "http://127.0.0.1:1234/health"},
	} {
		got, err := HealthURL(test.address)
		if err != nil || got != test.want {
			t.Fatalf("%s => %s %v", test.address, got, err)
		}
	}
}
func TestRunWaitsForWorkersOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	started := make(chan struct{})
	worker := func(ctx context.Context) { close(started); <-ctx.Done(); close(stopped) }
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, &http.Server{Addr: "127.0.0.1:0"}, slog.New(slog.NewTextHandler(io.Discard, nil)), worker)
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-stopped:
		default:
			t.Fatal("Run returned before worker stopped")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
}
