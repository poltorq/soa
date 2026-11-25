package aggregation

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }
type metricsTransaction interface {
	NextVersion(context.Context) (int64, error)
	Save(context.Context, time.Time, []Metric) error
	Metrics(context.Context, time.Time) ([]Metric, error)
	MarkExported(context.Context, time.Time) error
}
type postgresTransaction struct{ tx pgx.Tx }

func OpenPostgres(ctx context.Context, url string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}
func (s *PostgresStore) Close()                         { s.pool.Close() }
func (s *PostgresStore) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *PostgresStore) WithDateLock(ctx context.Context, date time.Time, operation func(metricsTransaction) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin date transaction: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	key := day(date).Unix() / 86400
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", int32(74205), int32(key)); err != nil {
		return fmt.Errorf("lock metric date: %w", err)
	}
	if err := operation(&postgresTransaction{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit date transaction: %w", err)
	}
	return nil
}

func (t *postgresTransaction) NextVersion(ctx context.Context) (int64, error) {
	var version int64
	if err := t.tx.QueryRow(ctx, "SELECT nextval('aggregate_run_versions')").Scan(&version); err != nil {
		return 0, fmt.Errorf("allocate aggregate version: %w", err)
	}
	return version, nil
}

func (t *postgresTransaction) Save(ctx context.Context, date time.Time, metrics []Metric) error {
	date = day(date)
	if err := validateMetrics(date, metrics); err != nil {
		return err
	}
	if _, err := t.tx.Exec(ctx, "DELETE FROM daily_metric_values WHERE metric_date = $1", date); err != nil {
		return fmt.Errorf("replace metric date: %w", err)
	}
	for _, metric := range metrics {
		_, err := t.tx.Exec(ctx, `
   INSERT INTO daily_metric_values
    (metric_date, metric_name, dimension_key, dimension_value, metric_value, computed_at)
   VALUES ($1,$2,$3,$4,$5,$6)
   ON CONFLICT (metric_date, metric_name, dimension_key, dimension_value)
   DO UPDATE SET metric_value = EXCLUDED.metric_value, computed_at = EXCLUDED.computed_at
  `, date, metric.Name, metric.DimensionKey, metric.DimensionValue, metric.Value, metric.ComputedAt)
		if err != nil {
			return fmt.Errorf("upsert metric %s: %w", metric.Name, err)
		}
	}
	if _, err := t.tx.Exec(ctx, `INSERT INTO daily_exports (metric_date, needs_export) VALUES ($1, TRUE)
  ON CONFLICT (metric_date) DO UPDATE SET needs_export = TRUE`, date); err != nil {
		return fmt.Errorf("enqueue export: %w", err)
	}
	return nil
}

func validateMetrics(date time.Time, metrics []Metric) error {
	if len(metrics) == 0 {
		return fmt.Errorf("empty metric snapshot")
	}
	seen := make(map[[3]string]bool, len(metrics))
	required := map[string]bool{"dau": false, "average_watch_seconds": false, "view_conversion": false, "retention_d1": false, "retention_d7": false}
	for _, metric := range metrics {
		key := [3]string{metric.Name, metric.DimensionKey, metric.DimensionValue}
		if !metric.Date.Equal(date) || metric.Name == "" || metric.ComputedAt.IsZero() || seen[key] {
			return fmt.Errorf("invalid or duplicate metric %q", metric.Name)
		}
		if metric.Value != nil && (math.IsNaN(*metric.Value) || math.IsInf(*metric.Value, 0)) {
			return fmt.Errorf("non-finite metric %q", metric.Name)
		}
		seen[key] = true
		if metric.DimensionKey == "" && metric.DimensionValue == "" {
			required[metric.Name] = true
		}
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("missing required metric %q", name)
		}
	}
	return nil
}

func (t *postgresTransaction) Metrics(ctx context.Context, date time.Time) ([]Metric, error) {
	rows, err := t.tx.Query(ctx, `SELECT metric_date, metric_name, dimension_key, dimension_value,
  metric_value, computed_at FROM daily_metric_values WHERE metric_date = $1
  ORDER BY metric_name, dimension_value`, day(date))
	if err != nil {
		return nil, fmt.Errorf("query PostgreSQL metrics: %w", err)
	}
	defer rows.Close()
	metrics := make([]Metric, 0)
	for rows.Next() {
		var metric Metric
		if err := rows.Scan(&metric.Date, &metric.Name, &metric.DimensionKey, &metric.DimensionValue, &metric.Value, &metric.ComputedAt); err != nil {
			return nil, fmt.Errorf("scan PostgreSQL metric: %w", err)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PostgreSQL metrics: %w", err)
	}
	return metrics, nil
}

func (s *PostgresStore) PendingExports(ctx context.Context, before time.Time) ([]time.Time, error) {
	rows, err := s.pool.Query(ctx, "SELECT metric_date FROM daily_exports WHERE needs_export AND metric_date < $1 ORDER BY metric_date", day(before))
	if err != nil {
		return nil, fmt.Errorf("read export backlog: %w", err)
	}
	defer rows.Close()
	dates := make([]time.Time, 0)
	for rows.Next() {
		var date time.Time
		if err := rows.Scan(&date); err != nil {
			return nil, err
		}
		dates = append(dates, date)
	}
	return dates, rows.Err()
}
func (t *postgresTransaction) MarkExported(ctx context.Context, date time.Time) error {
	_, err := t.tx.Exec(ctx, "UPDATE daily_exports SET needs_export = FALSE, exported_at = now() WHERE metric_date = $1", day(date))
	return err
}
