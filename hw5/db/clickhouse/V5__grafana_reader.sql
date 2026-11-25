CREATE USER IF NOT EXISTS grafana IDENTIFIED WITH sha256_password BY 'grafana';
ALTER USER grafana SETTINGS readonly = 1;
GRANT SELECT ON movie_analytics.daily_metrics_current TO grafana;
GRANT SELECT ON movie_analytics.movie_daily_views_current TO grafana;
GRANT SELECT ON movie_analytics.retention_cohorts_current TO grafana;
GRANT SELECT ON movie_analytics.daily_metrics_runs TO grafana;
GRANT SELECT ON movie_analytics.movie_daily_views_runs TO grafana;
GRANT SELECT ON movie_analytics.retention_cohorts_runs TO grafana;
GRANT SELECT ON movie_analytics.aggregate_runs TO grafana;
