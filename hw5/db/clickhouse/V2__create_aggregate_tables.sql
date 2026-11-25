CREATE TABLE IF NOT EXISTS movie_analytics.daily_metrics
(
    metric_date Date,
    dau UInt64,
    average_watch_seconds Float64,
    views_started UInt64,
    views_finished UInt64,
    view_conversion Float64,
    computed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(computed_at)
PARTITION BY toYYYYMM(metric_date)
ORDER BY metric_date;

CREATE TABLE IF NOT EXISTS movie_analytics.movie_daily_views
(
    metric_date Date,
    movie_id String,
    view_count UInt64,
    computed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(computed_at)
PARTITION BY toYYYYMM(metric_date)
ORDER BY (metric_date, movie_id);

CREATE TABLE IF NOT EXISTS movie_analytics.retention_cohorts
(
    cohort_date Date,
    day_number UInt8,
    returned_users UInt64,
    cohort_size UInt64,
    retention_rate Float64,
    computed_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(computed_at)
PARTITION BY toYYYYMM(cohort_date)
ORDER BY (cohort_date, day_number);
