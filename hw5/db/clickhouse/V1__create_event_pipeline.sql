CREATE DATABASE IF NOT EXISTS movie_analytics;

CREATE TABLE IF NOT EXISTS movie_analytics.raw_events
(
    event_id UUID,
    user_id String,
    movie_id String,
    event_type LowCardinality(String),
    timestamp DateTime64(3, 'UTC'),
    event_date Date MATERIALIZED toDate(timestamp),
    device_type LowCardinality(String),
    session_id String,
    progress_seconds Nullable(Int32),
    ingested_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_date, user_id, timestamp, event_id);

CREATE TABLE IF NOT EXISTS movie_analytics.movie_events_queue
(
    event_id UUID,
    user_id String,
    movie_id String,
    event_type String,
    timestamp DateTime64(3, 'UTC'),
    device_type String,
    session_id String,
    progress_seconds Nullable(Int32)
)
ENGINE = Kafka
SETTINGS
    kafka_broker_list = 'kafka-1:9092,kafka-2:9092',
    kafka_topic_list = 'movie-events',
    kafka_group_name = 'clickhouse-movie-events-v1',
    kafka_format = 'AvroConfluent',
    format_avro_schema_registry_url = 'http://schema-registry:8081',
    kafka_num_consumers = 1,
    kafka_thread_per_consumer = 1,
    kafka_handle_error_mode = 'stream';

CREATE MATERIALIZED VIEW IF NOT EXISTS movie_analytics.movie_events_ingest
TO movie_analytics.raw_events
AS
SELECT
    event_id,
    user_id,
    movie_id,
    event_type,
    timestamp,
    device_type,
    session_id,
    progress_seconds
FROM movie_analytics.movie_events_queue;
