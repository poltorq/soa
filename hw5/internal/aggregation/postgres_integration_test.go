//go:build integration

package aggregation

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func integrationPostgres(t *testing.T) (*PostgresStore, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	url := os.Getenv("POSTGRES_TEST_URL")
	if url == "" {
		url = "postgres://metrics_writer:metrics-local-secret@localhost:5435/analytics?sslmode=disable"
	}
	store, err := OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store, ctx
}
func integrationMetrics(date time.Time, value float64) []Metric {
	result := []Metric{}
	computedAt := time.Now().UTC().Truncate(time.Millisecond)
	for _, name := range []string{"dau", "average_watch_seconds", "view_conversion", "retention_d1", "retention_d7"} {
		v := value
		result = append(result, Metric{Date: date, Name: name, Value: &v, ComputedAt: computedAt})
	}
	return result
}
func TestPostgresAtomicReplacementAndRollback(t *testing.T) {
	store, ctx := integrationPostgres(t)
	date := time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = store.pool.Exec(cleanup, "DELETE FROM daily_metric_values WHERE metric_date=$1", date)
		_, _ = store.pool.Exec(cleanup, "DELETE FROM daily_exports WHERE metric_date=$1", date)
	})
	seed := integrationMetrics(date, 1)
	seed = append(seed, Metric{Date: date, Name: "top_movie_views", DimensionKey: "movie_id", DimensionValue: "stale", Value: seed[0].Value, ComputedAt: seed[0].ComputedAt})
	if err := store.WithDateLock(ctx, date, func(tx metricsTransaction) error { return tx.Save(ctx, date, seed) }); err != nil {
		t.Fatal(err)
	}
	if err := store.WithDateLock(ctx, date, func(tx metricsTransaction) error { return tx.Save(ctx, date, integrationMetrics(date, 2)) }); err != nil {
		t.Fatal(err)
	}
	assertSaved := func() {
		t.Helper()
		if err := store.WithDateLock(ctx, date, func(tx metricsTransaction) error {
			got, err := tx.Metrics(ctx, date)
			if err != nil {
				return err
			}
			if len(got) != 5 {
				t.Fatalf("stale dimension survived: %+v", got)
			}
			for _, metric := range got {
				if metric.Value == nil || *metric.Value != 2 {
					t.Fatalf("partial write: %+v", metric)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertSaved()
	failure := errors.New("simulated failure after writes")
	err := store.WithDateLock(ctx, date, func(tx metricsTransaction) error {
		if err := tx.Save(ctx, date, integrationMetrics(date, 3)); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	assertSaved()
}
func TestPostgresDateLockCancellationAndRelease(t *testing.T) {
	store, ctx := integrationPostgres(t)
	date := time.Date(2098, 1, 2, 0, 0, 0, 0, time.UTC)
	locked := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.WithDateLock(ctx, date, func(metricsTransaction) error { close(locked); <-release; return nil })
	}()
	<-locked
	waitCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	err := store.WithDateLock(waitCtx, date, func(metricsTransaction) error { t.Error("concurrent transaction entered a locked date"); return nil })
	cancel()
	close(release)
	if err == nil {
		t.Fatal("second transaction did not wait for lock")
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := store.WithDateLock(ctx, date, func(metricsTransaction) error { return nil }); err != nil {
		t.Fatalf("lock leaked after cancellation: %v", err)
	}
}
