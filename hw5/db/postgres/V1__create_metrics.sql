CREATE TABLE IF NOT EXISTS daily_metric_values (
    metric_date DATE NOT NULL,
    metric_name TEXT NOT NULL,
    dimension_key TEXT NOT NULL DEFAULT '',
    dimension_value TEXT NOT NULL DEFAULT '',
    metric_value DOUBLE PRECISION NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (metric_date, metric_name, dimension_key, dimension_value)
);

CREATE INDEX IF NOT EXISTS daily_metric_values_date_idx
    ON daily_metric_values (metric_date, metric_name);
