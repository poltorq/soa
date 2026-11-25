CREATE SEQUENCE IF NOT EXISTS aggregate_run_versions;

ALTER TABLE daily_metric_values ALTER COLUMN metric_value DROP NOT NULL;

CREATE TABLE IF NOT EXISTS daily_exports (
    metric_date DATE PRIMARY KEY,
    needs_export BOOLEAN NOT NULL DEFAULT TRUE,
    exported_at TIMESTAMPTZ
);

INSERT INTO daily_exports (metric_date)
SELECT DISTINCT metric_date FROM daily_metric_values
ON CONFLICT (metric_date) DO NOTHING;
