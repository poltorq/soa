#!/usr/bin/env python3
import re
import time
import urllib.error
from e2e import BASE, api, ch, docker, event, poll


def main():
    status = docker("exec", "-T", "kafka-1", "/opt/kafka/bin/kafka-metadata-quorum.sh",
                    "--bootstrap-server", "kafka-1:9092", "describe", "--status")
    leader = int(re.search(r"LeaderId:\s*(\d+)", status).group(1))
    broker = f"kafka-{leader}"
    item = event("failover", "M", "VIEW_STARTED", progress=0)
    try:
        docker("stop", broker)
        delivered = False
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            try:
                response = api(BASE + "/events", item, status=201)
                assert response["event_id"] == item["event_id"]
                delivered = True
                break
            except (AssertionError, urllib.error.URLError, TimeoutError):
                time.sleep(1)
        assert delivered, "ОТКАЗ: Kafka не приняла событие после остановки одного брокера"
        poll(lambda: ch(f"SELECT event_id FROM unique_events WHERE event_id=toUUID('{item['event_id']}')"),
             "событие после остановки брокера")
        print(f"HEALTH: {broker} остановлен; HTTP => Kafka => ClickHouse", flush=True)
    finally:
        docker("start", broker)
        def restored():
            state = docker("ps", "--format", "json", broker)
            return '"Health":"healthy"' in state.replace(" ", "")
        poll(restored, "восстановление остановленного брокера")
    print("HEALTH: Kafka восстановлена", flush=True)


if __name__ == "__main__":
    main()
