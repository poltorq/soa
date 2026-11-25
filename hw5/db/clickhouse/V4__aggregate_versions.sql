CREATE TABLE IF NOT EXISTS movie_analytics.daily_metrics_runs
(
    metric_date Date,
    run_version UInt64,
    dau UInt64,
    average_watch_seconds Float64,
    views_started UInt64,
    views_finished UInt64,
    view_conversion Float64,
    computed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(metric_date)
ORDER BY (metric_date, run_version);

CREATE TABLE IF NOT EXISTS movie_analytics.movie_daily_views_runs
(
    metric_date Date,
    run_version UInt64,
    movie_id String,
    view_count UInt64,
    computed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(metric_date)
ORDER BY (metric_date, run_version, movie_id);

CREATE TABLE IF NOT EXISTS movie_analytics.retention_cohorts_runs
(
    cohort_date Date,
    run_version UInt64,
    day_number UInt8,
    returned_users UInt64,
    cohort_size UInt64,
    retention_rate Nullable(Float64),
    computed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(cohort_date)
ORDER BY (cohort_date, run_version, day_number);

CREATE TABLE IF NOT EXISTS movie_analytics.aggregate_runs
(
    metric_date Date,
    run_version UInt64,
    computed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(run_version)
PARTITION BY toYYYYMM(metric_date)
ORDER BY metric_date;

CREATE OR REPLACE VIEW movie_analytics.daily_metrics_current AS
SELECT d.* FROM (SELECT * FROM movie_analytics.daily_metrics_runs FINAL) AS d
INNER JOIN (SELECT * FROM movie_analytics.aggregate_runs FINAL) AS r
ON d.metric_date = r.metric_date AND d.run_version = r.run_version;

CREATE OR REPLACE VIEW movie_analytics.movie_daily_views_current AS
SELECT d.* FROM (SELECT * FROM movie_analytics.movie_daily_views_runs FINAL) AS d
INNER JOIN (SELECT * FROM movie_analytics.aggregate_runs FINAL) AS r
ON d.metric_date = r.metric_date AND d.run_version = r.run_version;

CREATE OR REPLACE VIEW movie_analytics.retention_cohorts_current AS
SELECT d.* FROM (SELECT * FROM movie_analytics.retention_cohorts_runs FINAL) AS d
INNER JOIN (SELECT * FROM movie_analytics.aggregate_runs FINAL) AS r
ON d.cohort_date = r.metric_date AND d.run_version = r.run_version
WHERE d.cohort_size > 0;
