//go:build integration

package aggregation

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClickHouseDeduplicationMetricsAndVersionPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	dsn := os.Getenv("CLICKHOUSE_TEST_DSN")
	if dsn == "" {
		dsn = "clickhouse://aggregator:aggregate-local-secret@localhost:9002/movie_analytics"
	}
	source, err := OpenClickHouse(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	adminDSN := os.Getenv("CLICKHOUSE_TEST_ADMIN_DSN")
	if adminDSN == "" {
		adminDSN = "clickhouse://analytics:analytics@localhost:9002/movie_analytics"
	}
	admin, err := OpenClickHouse(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	date := time.Date(2098, 2, 1, 0, 0, 0, 0, time.UTC)
	computedAt := date.AddDate(0, 0, 8).Add(123 * time.Millisecond)
	prefix := "integration-" + uuid.NewString()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.db.ExecContext(cleanup, "ALTER TABLE raw_events DELETE WHERE startsWith(user_id, ?) SETTINGS mutations_sync=2", prefix); err != nil {
			t.Error(err)
		}
		for _, table := range []string{"daily_metrics_runs", "movie_daily_views_runs", "aggregate_runs", "retention_cohorts_runs"} {
			column := "metric_date"
			if table == "retention_cohorts_runs" {
				column = "cohort_date"
			}
			if _, err := admin.db.ExecContext(cleanup, "ALTER TABLE "+table+" DELETE WHERE "+column+" = ? SETTINGS mutations_sync=2", date); err != nil {
				t.Error(err)
			}
		}
	})
	insert := func(id, user, movie, kind string, offset int, progress any) {
		t.Helper()
		_, err := admin.db.ExecContext(ctx, `INSERT INTO raw_events
   (event_id,user_id,movie_id,event_type,timestamp,device_type,session_id,progress_seconds)
   VALUES (toUUID(?),?,?,?,?, 'TV',?,?)`, id, prefix+user, movie, kind, date.AddDate(0, 0, offset), prefix+user, progress)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert(uuid.NewString(), "A", "M", "VIEW_STARTED", 0, 0)
	finish := uuid.NewString()
	insert(finish, "A", "M", "VIEW_FINISHED", 0, 120)
	insert(finish, "A", "M", "VIEW_FINISHED", 0, 120)
	insert(uuid.NewString(), "B", "M", "VIEW_STARTED", 0, 0)
	insert(uuid.NewString(), "B", "M", "VIEW_FINISHED", 0, 240)
	insert(uuid.NewString(), "C", "N", "VIEW_STARTED", 0, 0)
	insert(uuid.NewString(), "A", "M", "VIEW_STARTED", 1, 0)
	insert(uuid.NewString(), "B", "M", "VIEW_STARTED", 7, 0)
	insert(uuid.NewString(), "B", "M", "SEARCHED", -1, nil)
	if _, err := source.Recalculate(ctx, date, 1, computedAt); err != nil {
		t.Fatal(err)
	}
	metrics, err := source.Metrics(ctx, date, 1)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]float64{"dau": 3, "average_watch_seconds": 180, "view_conversion": 2.0 / 3, "top_movie_views": 2, "retention_d1": 1.0 / 3, "retention_d7": 1.0 / 3}
	if len(metrics) != len(expected) {
		t.Fatalf("metrics=%+v", metrics)
	}
	for _, metric := range metrics {
		want, exists := expected[metric.Name]
		if !exists || metric.Value == nil || math.Abs(*metric.Value-want) > 1e-9 {
			t.Fatalf("%s=%v want %v", metric.Name, metric.Value, want)
		}
		if !metric.ComputedAt.Equal(computedAt) {
			t.Fatalf("timestamp lost milliseconds: %v != %v", metric.ComputedAt, computedAt)
		}
	}
	var visible uint64
	if err := source.db.QueryRowContext(ctx, "SELECT count() FROM daily_metrics_current WHERE metric_date=?", date).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatal("unpublished calculation became visible")
	}
	if err := source.Publish(ctx, date, 1, computedAt); err != nil {
		t.Fatal(err)
	}
	if err := source.db.QueryRowContext(ctx, "SELECT count() FROM daily_metrics_current WHERE metric_date=?", date).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 1 {
		t.Fatal("published daily snapshot missing")
	}
	for _, user := range []string{"A", "B", "C"} {
		insert(uuid.NewString(), user, "M", "VIEW_STARTED", -2, 0)
	}
	if _, err := source.Recalculate(ctx, date, 2, computedAt.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := source.Publish(ctx, date, 2, computedAt.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := source.db.QueryRowContext(ctx, "SELECT count() FROM retention_cohorts_current WHERE cohort_date=?", date).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatal("empty cohort kept stale rows")
	}
	if _, err := source.Recalculate(ctx, date, 3, date); err != nil {
		t.Fatal(err)
	}
	immature, err := source.Metrics(ctx, date, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range immature {
		if (metric.Name == "retention_d1" || metric.Name == "retention_d7") && metric.Value != nil {
			t.Fatalf("future retention=%+v", metric)
		}
	}
}
