DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'metrics_writer') THEN
        CREATE ROLE metrics_writer LOGIN PASSWORD 'metrics-local-secret';
    END IF;
END
$$;
GRANT CONNECT ON DATABASE analytics TO metrics_writer;
GRANT USAGE ON SCHEMA public TO metrics_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON daily_metric_values, daily_exports TO metrics_writer;
GRANT USAGE, SELECT ON SEQUENCE aggregate_run_versions TO metrics_writer;
