INSERT INTO movie_analytics.daily_metrics_runs
SELECT metric_date, toUInt64(0), dau, average_watch_seconds, views_started,
    views_finished, view_conversion, computed_at
FROM movie_analytics.daily_metrics FINAL
WHERE metric_date NOT IN (SELECT metric_date FROM movie_analytics.aggregate_runs FINAL);

INSERT INTO movie_analytics.movie_daily_views_runs
SELECT metric_date, toUInt64(0), movie_id, view_count, computed_at
FROM movie_analytics.movie_daily_views FINAL
WHERE metric_date NOT IN (SELECT metric_date FROM movie_analytics.aggregate_runs FINAL);

INSERT INTO movie_analytics.retention_cohorts_runs
SELECT cohort_date, toUInt64(0), day_number, returned_users, cohort_size,
    retention_rate, computed_at
FROM movie_analytics.retention_cohorts FINAL
WHERE cohort_date NOT IN (SELECT metric_date FROM movie_analytics.aggregate_runs FINAL);

INSERT INTO movie_analytics.aggregate_runs
SELECT metric_date, toUInt64(0), computed_at
FROM movie_analytics.daily_metrics FINAL
WHERE metric_date NOT IN (SELECT metric_date FROM movie_analytics.aggregate_runs FINAL);
