CREATE OR REPLACE VIEW movie_analytics.unique_events AS
SELECT
    event_id,
    payload.1 AS user_id,
    payload.2 AS movie_id,
    payload.3 AS event_type,
    payload.4 AS timestamp,
    toDate(payload.4) AS event_date,
    payload.5 AS device_type,
    payload.6 AS session_id,
    payload.7 AS progress_seconds
FROM
(
    SELECT event_id,
        argMin(tuple(user_id, movie_id, event_type, timestamp, device_type, session_id, progress_seconds),
            ingested_at) AS payload
    FROM movie_analytics.raw_events
    GROUP BY event_id
);

CREATE TABLE IF NOT EXISTS movie_analytics.event_dead_letters
(
    topic String,
    partition_id UInt64,
    offset_id UInt64,
    raw_message String,
    error String,
    captured_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(captured_at)
ORDER BY (topic, partition_id, offset_id);

ALTER TABLE movie_analytics.movie_events_ingest MODIFY QUERY
SELECT event_id, user_id, movie_id, event_type, timestamp, device_type, session_id, progress_seconds
FROM movie_analytics.movie_events_queue
WHERE _error = '';

CREATE MATERIALIZED VIEW IF NOT EXISTS movie_analytics.movie_events_errors
TO movie_analytics.event_dead_letters AS
SELECT _topic AS topic, _partition AS partition_id, _offset AS offset_id,
    _raw_message AS raw_message, _error AS error
FROM movie_analytics.movie_events_queue
WHERE _error != '';
