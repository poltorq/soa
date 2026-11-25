#!/bin/sh
set -eu
kafka_bin=/opt/kafka/bin
bootstrap=kafka-1:9092
"$kafka_bin/kafka-topics.sh" --bootstrap-server "$bootstrap" --create --if-not-exists \
  --topic movie-events --partitions 3 --replication-factor 3 --config min.insync.replicas=2

description=$("$kafka_bin/kafka-topics.sh" --bootstrap-server "$bootstrap" --describe --topic movie-events)
if ! printf '%s\n' "$description" | grep -q 'ReplicationFactor: 3'; then
  printf '%s\n' "$description" | grep -q 'PartitionCount: 3' || {
    echo 'ОТКАЗ: для назначения реплик требуется topic с тремя партициями' >&2
    exit 1
  }
  cat > /tmp/movie-events-replicas.json <<'JSON'
{"version":1,"partitions":[{"topic":"movie-events","partition":0,"replicas":[1,2,3]},{"topic":"movie-events","partition":1,"replicas":[2,3,1]},{"topic":"movie-events","partition":2,"replicas":[3,1,2]}]}
JSON
  "$kafka_bin/kafka-reassign-partitions.sh" --bootstrap-server "$bootstrap" \
    --reassignment-json-file /tmp/movie-events-replicas.json --execute
  attempt=0
  while :; do
    result=$("$kafka_bin/kafka-reassign-partitions.sh" --bootstrap-server "$bootstrap" \
      --reassignment-json-file /tmp/movie-events-replicas.json --verify)
    if ! printf '%s\n' "$result" | grep -q 'still in progress'; then break; fi
    attempt=$((attempt + 1))
    [ "$attempt" -lt 30 ] || { echo 'ОТКАЗ: превышено время назначения реплик Kafka' >&2; exit 1; }
    sleep 2
  done
fi
"$kafka_bin/kafka-configs.sh" --bootstrap-server "$bootstrap" --alter \
  --entity-type topics --entity-name movie-events --add-config min.insync.replicas=2
