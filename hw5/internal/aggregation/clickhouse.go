package aggregation

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

type ClickHouseStore struct {
	db *sql.DB
}

func OpenClickHouse(ctx context.Context, dsn string) (*ClickHouseStore, error) {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return &ClickHouseStore{db: db}, nil
}

func (s *ClickHouseStore) Close() error {
	return s.db.Close()
}

func (s *ClickHouseStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *ClickHouseStore) Recalculate(ctx context.Context, date time.Time, version int64, computedAt time.Time) (int64, error) {
	date = day(date)
	var processed int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT count()
		FROM unique_events
		WHERE event_date = ?
	`, date).Scan(&processed); err != nil {
		return 0, fmt.Errorf("count raw events: %w", err)
	}

	queries := []struct {
		name string
		sql  string
		args []any
	}{
		{
			name: "daily metrics",
			sql: `
				INSERT INTO daily_metrics_runs
				SELECT
					toDate(?) AS metric_date,
                    toUInt64(?) AS run_version,
					uniqExact(user_id) AS dau,
					if(
						countIf(event_type = 'VIEW_FINISHED') = 0,
						0,
						avgIf(toFloat64(progress_seconds), event_type = 'VIEW_FINISHED')
					) AS average_watch_seconds,
					countIf(event_type = 'VIEW_STARTED') AS views_started,
					countIf(event_type = 'VIEW_FINISHED') AS views_finished,
					if(views_started = 0, 0, views_finished / views_started) AS view_conversion,
					toDateTime64(?, 3, 'UTC') AS computed_at
				FROM unique_events
				WHERE event_date = ?
			`,
			args: []any{date, version, computedAt.Format("2006-01-02 15:04:05.000"), date},
		},
		{
			name: "movie views",
			sql: `
				INSERT INTO movie_daily_views_runs
				SELECT
					toDate(?) AS metric_date,
                    toUInt64(?) AS run_version,
					movie_id,
					countIf(event_type = 'VIEW_FINISHED') AS view_count,
					toDateTime64(?, 3, 'UTC') AS computed_at
				FROM unique_events
				WHERE event_date = ?
				GROUP BY movie_id
			`,
			args: []any{date, version, computedAt.Format("2006-01-02 15:04:05.000"), date},
		},
		{
			name: "retention cohorts",
			sql: `
                INSERT INTO retention_cohorts_runs
                WITH
                    first_activity AS (
                        SELECT user_id, min(event_date) AS cohort_date
                        FROM unique_events
                        WHERE event_type IN ('VIEW_STARTED', 'VIEW_FINISHED')
                        GROUP BY user_id
                    ),
                    cohort_activity AS (
                        SELECT u.user_id, toUInt64(0) AS join_key,
                            groupUniqArray(e.event_date) AS activity_dates
                        FROM first_activity AS u
                        INNER JOIN unique_events AS e ON u.user_id = e.user_id
                        WHERE u.cohort_date = toDate(?)
                            AND e.event_type IN ('VIEW_STARTED', 'VIEW_FINISHED')
                            AND e.event_date BETWEEN toDate(?) AND addDays(toDate(?), 7)
                        GROUP BY u.user_id
                    )
                SELECT
                    toDate(?) AS cohort_date,
                    toUInt64(?) AS run_version,
                    toUInt8(days.number) AS day_number,
                    countIf(c.user_id != '' AND has(c.activity_dates, addDays(cohort_date, days.number))) AS returned_users,
                    countIf(c.user_id != '') AS cohort_size,
                    if(addDays(cohort_date, days.number) > toDate(?), NULL,
                        if(cohort_size = 0, 0, returned_users / cohort_size)) AS retention_rate,
                    toDateTime64(?, 3, 'UTC') AS computed_at
                FROM numbers(8) AS days
                LEFT JOIN cohort_activity AS c ON c.join_key = days.number * 0
                GROUP BY days.number
            `,
			args: []any{date, date, date, date, version, computedAt, computedAt.Format("2006-01-02 15:04:05.000")},
		},
	}
	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query.sql, query.args...); err != nil {
			return 0, fmt.Errorf("calculate %s: %w", query.name, err)
		}
	}
	return processed, nil
}

func (s *ClickHouseStore) Metrics(ctx context.Context, date time.Time, version int64) ([]Metric, error) {
	date = day(date)
	rows, err := s.db.QueryContext(ctx, `
		SELECT metric_name, dimension_key, dimension_value, metric_value, computed_at
		FROM
		(
			SELECT 'dau' AS metric_name, '' AS dimension_key, '' AS dimension_value,
				toFloat64(dau) AS metric_value, computed_at
			FROM daily_metrics_runs FINAL WHERE metric_date = ? AND run_version = ?
			UNION ALL
			SELECT 'average_watch_seconds', '', '', average_watch_seconds, computed_at
			FROM daily_metrics_runs FINAL WHERE metric_date = ? AND run_version = ?
			UNION ALL
			SELECT 'view_conversion', '', '', view_conversion, computed_at
			FROM daily_metrics_runs FINAL WHERE metric_date = ? AND run_version = ?
			UNION ALL
			SELECT 'top_movie_views', 'movie_id', movie_id, toFloat64(view_count), computed_at
			FROM movie_daily_views_runs FINAL WHERE metric_date = ? AND run_version = ? AND view_count > 0
			UNION ALL
			SELECT concat('retention_d', toString(day_number)), '', '', retention_rate, computed_at
			FROM retention_cohorts_runs FINAL WHERE cohort_date = ? AND run_version = ? AND day_number IN (1, 7)
		)
		ORDER BY metric_name, dimension_value
	`, date, version, date, version, date, version, date, version, date, version)
	if err != nil {
		return nil, fmt.Errorf("query calculated metrics: %w", err)
	}
	defer rows.Close()

	metrics := make([]Metric, 0)
	for rows.Next() {
		var metric Metric
		metric.Date = date
		if err := rows.Scan(
			&metric.Name,
			&metric.DimensionKey,
			&metric.DimensionValue,
			&metric.Value,
			&metric.ComputedAt,
		); err != nil {
			return nil, fmt.Errorf("scan calculated metric: %w", err)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate calculated metrics: %w", err)
	}
	return metrics, nil
}

func (s *ClickHouseStore) Publish(ctx context.Context, date time.Time, version int64, computedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO aggregate_runs (metric_date, run_version, computed_at) VALUES (?, ?, toDateTime64(?, 3, 'UTC'))`, day(date), version, computedAt.Format("2006-01-02 15:04:05.000"))
	if err != nil {
		return fmt.Errorf("publish aggregate version: %w", err)
	}
	return nil
}

func day(value time.Time) time.Time {
	year, month, date := value.UTC().Date()
	return time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
}
