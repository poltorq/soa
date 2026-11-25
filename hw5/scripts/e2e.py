#!/usr/bin/env python3
import base64
import collections
import concurrent.futures
import csv
import datetime as dt
import hashlib
import hmac
import io
import json
import math
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parent.parent
UTC = dt.timezone.utc
BASE = os.getenv("BASE_URL", "http://localhost:8080")
AGG = os.getenv("AGGREGATION_URL", "http://localhost:8081")
CH = os.getenv("CLICKHOUSE_URL", "http://localhost:8123")
REGISTRY = os.getenv("SCHEMA_REGISTRY_URL", "http://localhost:8082")
GRAFANA = os.getenv("GRAFANA_URL", "http://localhost:3000")
S3 = os.getenv("S3_URL", "http://localhost:4566")
RUN = str(uuid.uuid4())
TODAY = dt.datetime.now(UTC).date()
DAY = TODAY - dt.timedelta(days=14)


def http(url, data=None, headers=None, method=None, status=200):
    request = urllib.request.Request(url, data=data, headers=headers or {}, method=method)
    try:
        with urllib.request.urlopen(request, timeout=40) as response:
            actual, body = response.status, response.read()
    except urllib.error.HTTPError as error:
        actual, body = error.code, error.read()
    assert actual == status, f"{url}: status {actual}, expected {status}: {body.decode(errors='replace')}"
    return body


def api(url, payload=None, method=None, status=200):
    body = None if payload is None else json.dumps(payload).encode()
    return json.loads(http(url, body, {"Content-Type": "application/json"}, method, status))


def docker(*args, input_text=None):
    return subprocess.run(["docker", "compose", *args], cwd=ROOT, input=input_text,
                          text=True, capture_output=True, check=True, timeout=90).stdout.strip()


def ch(query, user="analytics", password="analytics"):
    auth = base64.b64encode(f"{user}:{password}".encode()).decode()
    body = http(CH + "/?database=movie_analytics", (query + " FORMAT JSONEachRow").encode(),
                {"Authorization": "Basic " + auth})
    return [json.loads(line) for line in body.splitlines() if line]


def pg(query):
    return docker("exec", "-T", "postgres", "psql", "-U", "analytics", "-d", "analytics",
                  "-v", "ON_ERROR_STOP=1", "-Atc", query)


def pg_rows(query):
    return json.loads(pg("SELECT coalesce(json_agg(t), '[]'::json) FROM (" + query + ") t"))


def poll(fn, description):
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if fn():
            return
        time.sleep(1)
    raise AssertionError("timeout: " + description)


def event(user, movie, event_type, offset=0, progress=None):
    date = DAY + dt.timedelta(days=offset)
    item = dict(event_id=str(uuid.uuid4()), user_id=f"e2e-{RUN}-{user}", movie_id=f"e2e-{RUN}-{movie}",
                event_type=event_type, timestamp=f"{date}T12:00:00.123Z", device_type="DESKTOP",
                session_id=f"e2e-{RUN}-{user}-{movie}-{offset}")
    if progress is not None:
        item["progress_seconds"] = progress
    return item


def post(item):
    response = api(BASE + "/events", item, status=201)
    assert response["event_id"] == item["event_id"]


def recalculate(date):
    return api(AGG + "/aggregate?date=" + str(date), method="POST")


def parse_timestamp(value):
    parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    return parsed.replace(tzinfo=UTC) if parsed.tzinfo is None else parsed.astimezone(UTC)


def raw_events():
    return ch("SELECT toString(event_id) AS event_id, user_id, movie_id, event_type, timestamp, "
              "device_type, session_id, progress_seconds FROM raw_events ORDER BY ingested_at")


def reference_metrics(date):
    unique = {}
    for item in raw_events():
        unique.setdefault(item["event_id"], item)
    items = list(unique.values())
    selected = [item for item in items if parse_timestamp(item["timestamp"]).date() == date]
    starts = sum(item["event_type"] == "VIEW_STARTED" for item in selected)
    finished = [item for item in selected if item["event_type"] == "VIEW_FINISHED"]
    expected = {("dau", "", ""): len({item["user_id"] for item in selected}),
                ("average_watch_seconds", "", ""): sum(item["progress_seconds"] for item in finished) / len(finished) if finished else 0,
                ("view_conversion", "", ""): len(finished) / starts if starts else 0}
    for movie, count in collections.Counter(item["movie_id"] for item in finished).items():
        expected[("top_movie_views", "movie_id", movie)] = count
    first, active = {}, collections.defaultdict(set)
    for item in items:
        if item["event_type"] not in ("VIEW_STARTED", "VIEW_FINISHED"):
            continue
        item_date = parse_timestamp(item["timestamp"]).date()
        user = item["user_id"]
        first[user] = min(first.get(user, item_date), item_date)
        active[user].add(item_date)
    cohort = {user for user, first_date in first.items() if first_date == date}
    rates = {}
    for day in range(8):
        target = date + dt.timedelta(days=day)
        rates[day] = None if target > TODAY else sum(target in active[user] for user in cohort) / len(cohort) if cohort else 0
    for day in (1, 7):
        expected[(f"retention_d{day}", "", "")] = rates[day]
    return expected, cohort, rates


def assert_metrics(date):
    expected, cohort, rates = reference_metrics(date)
    rows = pg_rows(f"SELECT metric_name, dimension_key, dimension_value, metric_value, computed_at FROM daily_metric_values WHERE metric_date = '{date}'")
    actual = {(r["metric_name"], r["dimension_key"], r["dimension_value"]): r["metric_value"] for r in rows}
    assert actual.keys() == expected.keys(), ("metric set mismatch", actual, expected)
    for key, value in expected.items():
        assert (actual[key] is None if value is None else actual[key] is not None and math.isclose(actual[key], value, abs_tol=1e-9)), (key, actual[key], value)
    assert len({r["computed_at"] for r in rows}) == 1, "mixed computation versions in PostgreSQL"
    cohorts = ch(f"SELECT day_number, cohort_size, retention_rate FROM retention_cohorts_current WHERE cohort_date = '{date}' ORDER BY day_number")
    assert len(cohorts) == (8 if cohort else 0), ("stale/absent cohort", cohorts, cohort)
    for row in cohorts:
        assert int(row["cohort_size"]) == len(cohort)
        value = rates[row["day_number"]]
        assert row["retention_rate"] is None if value is None else math.isclose(row["retention_rate"], value, abs_tol=1e-9)
    daily = ch(f"SELECT dau, view_conversion FROM daily_metrics_current WHERE metric_date = '{date}'")
    assert len(daily) == 1 and int(daily[0]["dau"]) == expected[("dau", "", "")]
    assert math.isclose(daily[0]["view_conversion"], expected[("view_conversion", "", "")], abs_tol=1e-9)
    return actual


def s3_get(path):
    now = dt.datetime.now(UTC)
    amz_date, date = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
    host = urllib.parse.urlsplit(S3).netloc
    digest = hashlib.sha256(b"").hexdigest()
    headers = f"host:{host}\nx-amz-content-sha256:{digest}\nx-amz-date:{amz_date}\n"
    signed = "host;x-amz-content-sha256;x-amz-date"
    canonical = "GET\n" + path + "\n\n" + headers + "\n" + signed + "\n" + digest
    scope = date + "/us-east-1/s3/aws4_request"
    to_sign = "AWS4-HMAC-SHA256\n" + amz_date + "\n" + scope + "\n" + hashlib.sha256(canonical.encode()).hexdigest()
    key = ("AWS4" + os.getenv("S3_SECRET_KEY", "analytics-local-secret")).encode()
    for part in (date, "us-east-1", "s3", "aws4_request"):
        key = hmac.new(key, part.encode(), hashlib.sha256).digest()
    signature = hmac.new(key, to_sign.encode(), hashlib.sha256).hexdigest()
    auth = "AWS4-HMAC-SHA256 Credential=" + os.getenv("S3_ACCESS_KEY", "analytics") + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + signature
    return http(S3 + path, headers={"x-amz-content-sha256": digest, "x-amz-date": amz_date, "Authorization": auth})


def main():
    api(BASE + "/health")
    api(AGG + "/health")
    items = [event("A", "M", "SEARCHED", -1), event("A", "M", "VIEW_STARTED", progress=0),
             event("A", "M", "VIEW_FINISHED", progress=120), event("B", "N", "VIEW_STARTED", progress=0),
             event("C", "M", "VIEW_STARTED", progress=0), event("C", "M", "VIEW_FINISHED", progress=360),
             event("D", "N", "SEARCHED"), event("A", "M", "VIEW_STARTED", 1, 0),
             event("A", "M", "VIEW_FINISHED", 1, 200), event("C", "M", "LIKED", 1),
             event("B", "N", "SEARCHED", 1), event("A", "M", "VIEW_STARTED", 7, 0),
             event("B", "N", "VIEW_STARTED", 7, 0)]
    items[0]["timestamp"] = items[0]["timestamp"].replace("12:00:00.123Z", "15:00:00.123+03:00")
    for item in items:
        post(item)
    post(items[2])
    ids = {item["event_id"] for item in items}
    poll(lambda: ids <= {r["event_id"] for r in raw_events()}, "all fixture events ingested")
    stored = {r["event_id"]: r for r in raw_events() if r["event_id"] in ids}
    for item in items:
        row = stored[item["event_id"]]
        for field in ("event_id", "user_id", "movie_id", "event_type", "device_type", "session_id", "progress_seconds"):
            assert row[field] == item.get(field), (field, row, item)
        assert parse_timestamp(row["timestamp"]) == parse_timestamp(item["timestamp"])
    assert int(ch(f"SELECT count() AS n FROM unique_events WHERE event_id = toUUID('{items[2]['event_id']}')")[0]["n"]) == 1
    print("КОНТРОЛЬ ИСПРАВЕН: события приняты, поля и дедупликация проверены", flush=True)

    for patch in ({"event_type": "SKIPPED"}, {"progress_seconds": -1}, {"timestamp": "2200-01-01T00:00:00Z"}, {"unknown": True}):
        api(BASE + "/events", {**items[1], **patch}, status=400)
    api(AGG + "/aggregate?date=2200-01-01", method="POST", status=400)

    marker = "e2e-broken-" + RUN
    docker("exec", "-T", "kafka-1", "/opt/kafka/bin/kafka-console-producer.sh", "--bootstrap-server", "kafka-1:9092,kafka-2:9092,kafka-3:9092",
           "--topic", "movie-events", "--producer-property", "acks=all", input_text=marker + "\n")
    poll(lambda: ch(f"SELECT error FROM event_dead_letters WHERE position(raw_message, '{marker}') > 0"), "malformed Avro in DLQ")
    post(event("after-DLQ", "M", "LIKED", 2))
    poll(lambda: ch(f"SELECT event_id FROM raw_events WHERE user_id = 'e2e-{RUN}-after-DLQ'"), "ingestion continues after bad message")
    print("КОНТРОЛЬ ИСПРАВЕН: валидация и DLQ работают", flush=True)

    for date in (DAY, DAY + dt.timedelta(days=1), DAY + dt.timedelta(days=7)):
        recalculate(date)
        assert_metrics(date)
    recalculate(TODAY)
    immature = pg_rows(f"SELECT metric_value FROM daily_metric_values WHERE metric_date = '{TODAY}' AND metric_name IN ('retention_d1','retention_d7')")
    assert len(immature) == 2 and all(row["metric_value"] is None for row in immature), "future retention must be NULL"
    stale = "e2e-stale-" + RUN
    pg(f"INSERT INTO daily_metric_values VALUES ('{DAY}','top_movie_views','movie_id','{stale}',999,now())")
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        list(pool.map(recalculate, [DAY] * 3))
    assert_metrics(DAY)
    assert pg(f"SELECT count(*) FROM daily_metric_values WHERE dimension_value = '{stale}'") == "0"
    late = event("B", "N", "VIEW_STARTED", -2, 0)
    post(late)
    poll(lambda: ch(f"SELECT event_id FROM raw_events WHERE event_id=toUUID('{late['event_id']}')"), "late viewing")
    recalculate(DAY)
    expected = assert_metrics(DAY)
    print("КОНТРОЛЬ ИСПРАВЕН: метрики и повторный расчёт проверены", flush=True)

    export_url = AGG + "/export?date=" + str(DAY)
    api(export_url, method="POST")
    path = f"/movie-analytics/daily/{DAY}/aggregates.csv"
    first = s3_get(path)
    api(export_url, method="POST")
    assert first == s3_get(path), "repeat export changed an unchanged snapshot"
    rows = list(csv.DictReader(io.StringIO(first.decode())))
    actual = {(r["metric_name"], r["dimension_key"], r["dimension_value"]): None if r["metric_value"] == "" else float(r["metric_value"]) for r in rows}
    assert actual.keys() == expected.keys()
    for key, value in expected.items():
        assert actual[key] is None if value is None else math.isclose(actual[key], value, abs_tol=1e-9)
    assert all(row["metric_date"] == str(DAY) and row["computed_at"] for row in rows)
    yesterday = TODAY - dt.timedelta(days=1)
    poll(lambda: pg(f"SELECT count(*) FROM daily_exports WHERE metric_date='{yesterday}' AND NOT needs_export") == "1", "automatic yesterday export")
    s3_get(f"/movie-analytics/daily/{yesterday}/aggregates.csv")
    print("КОНТРОЛЬ ИСПРАВЕН: экспорт S3 проверен", flush=True)

    versions = api(REGISTRY + "/subjects/movie-events-value/versions")
    assert 1 in versions
    schema = api(REGISTRY + "/subjects/movie-events-value/versions/latest")
    assert schema["version"] >= 1
    assert schema.get("schemaType", "AVRO") == "AVRO"
    assert api(REGISTRY + "/config/movie-events-value")["compatibilityLevel"] == "BACKWARD_TRANSITIVE"
    description = docker("exec", "-T", "kafka-1", "/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "kafka-1:9092", "--describe", "--topic", "movie-events")
    assert "PartitionCount: 3" in description and "ReplicationFactor: 3" in description and "min.insync.replicas=2" in description

    auth = {"Authorization": "Basic " + base64.b64encode(b"admin:admin").decode()}
    health = json.loads(http(GRAFANA + "/api/datasources/uid/clickhouse/health", headers=auth))
    assert health["status"] == "OK", health
    dashboard = json.loads(http(GRAFANA + "/api/dashboards/uid/movie-analytics", headers=auth))["dashboard"]
    assert len(dashboard["panels"]) >= 3
    for panel in dashboard["panels"]:
        for target in panel["targets"]:
            assert "raw_events" not in target["rawSql"]
            query = re.sub(r"\$__dateFilter\((\w+)\)", rf"\1 BETWEEN toDate('{DAY - dt.timedelta(days=2)}') AND today()", target["rawSql"])
            ch(query, "grafana", "grafana")
    print("КОНТРОЛЬ ИСПРАВЕН: Schema Registry, Kafka и Grafana проверены", flush=True)
    print("ПРОВЕРКА ЗАВЕРШЕНА: маршрут E2E исправен", flush=True)


if __name__ == "__main__":
    main()
