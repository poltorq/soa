#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
FLIGHT_ID="10000000-0000-0000-0000-000000000001"
LIMITED_FLIGHT_ID="10000000-0000-0000-0000-000000000002"
USER_ID="20000000-0000-0000-0000-000000000001"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

request() {
  local expected="$1"
  local name="$2"
  shift 2
  local body="$TMP_DIR/body.json"
  local status
  status="$(curl --silent --show-error --output "$body" --write-out '%{http_code}' "$@")"
  if [[ "$status" != "$expected" ]]; then
    echo "FAIL: $name: expected HTTP $expected, got $status"
    cat "$body"
    exit 1
  fi
  echo "PASS: $name -> HTTP $status" >&2
  cat "$body"
}

echo "Waiting for Booking Service..."
for _ in {1..60}; do
  if curl --silent --fail "$BASE_URL/health" >/dev/null; then
    break
  fi
  sleep 1
done
request 200 "health" "$BASE_URL/health" >/dev/null

SEARCH_JSON="$(request 200 "search flights" \
  "$BASE_URL/flights?origin=SVO&destination=LED&date=2026-04-01")"
jq -e '.items | length == 2 and all(.status == "SCHEDULED")' <<<"$SEARCH_JSON" >/dev/null
echo "PASS: search returns two scheduled seed flights"
request 200 "cached search flights" \
  "$BASE_URL/flights?origin=SVO&destination=LED&date=2026-04-01" >/dev/null

SEARCH_TTL="$(docker compose exec -T redis-master redis-cli -a redispass --raw \
  TTL search:SVO:LED:2026-04-01 2>/dev/null)"
if (( SEARCH_TTL < 1 || SEARCH_TTL > 300 )); then
  echo "FAIL: expected search cache TTL between 1 and 300, got $SEARCH_TTL"
  exit 1
fi
CACHE_LOGS="$(docker compose logs --no-color flight-service)"
grep -q '"msg":"cache miss"' <<<"$CACHE_LOGS" || { echo "FAIL: cache miss log is absent"; exit 1; }
grep -q '"msg":"cache hit"' <<<"$CACHE_LOGS" || { echo "FAIL: cache hit log is absent"; exit 1; }
echo "PASS: cache-aside logs miss/hit and stores the search with TTL"

REPLICA_COUNT="$(docker compose exec -T redis-sentinel redis-cli -p 26379 --raw \
  SENTINEL replicas flight-cache | grep -c '^name$')"
(( REPLICA_COUNT >= 1 )) || { echo "FAIL: Redis Sentinel has no replica"; exit 1; }
echo "PASS: Redis Sentinel sees a replica"

request 200 "get flight" "$BASE_URL/flights/$FLIGHT_ID" >/dev/null

BOOKING_JSON="$(request 201 "create booking" \
  -H 'Content-Type: application/json' \
  -d "{\"user_id\":\"$USER_ID\",\"flight_id\":\"$FLIGHT_ID\",\"passenger_name\":\"Anna Petrova\",\"passenger_email\":\"anna@example.com\",\"seat_count\":2}" \
  "$BASE_URL/bookings")"
BOOKING_ID="$(jq -er '.id' <<<"$BOOKING_JSON")"
jq -e '.status == "CONFIRMED" and .total_price_cents == 1700000' <<<"$BOOKING_JSON" >/dev/null
echo "PASS: price snapshot equals seat_count multiplied by flight price"

request 200 "get booking" "$BASE_URL/bookings/$BOOKING_ID" >/dev/null
LIST_JSON="$(request 200 "list user bookings" "$BASE_URL/bookings?user_id=$USER_ID")"
jq -e --arg id "$BOOKING_ID" '.items | any(.id == $id)' <<<"$LIST_JSON" >/dev/null
echo "PASS: booking appears in the user list"

AVAILABLE="$(docker compose exec -T flight-postgres psql -U flight -d flight -Atc \
  "SELECT available_seats FROM flights WHERE id = '$FLIGHT_ID'")"
[[ "$AVAILABLE" == "178" ]] || { echo "FAIL: expected 178 available seats, got $AVAILABLE"; exit 1; }
echo "PASS: seats were atomically reserved in Flight Service database"

BOOKING_COUNT_BEFORE="$(docker compose exec -T booking-postgres psql -U booking -d booking -Atc \
  'SELECT count(*) FROM bookings')"
request 409 "insufficient seats" \
  -H 'Content-Type: application/json' \
  -d "{\"user_id\":\"$USER_ID\",\"flight_id\":\"$LIMITED_FLIGHT_ID\",\"passenger_name\":\"Anna Petrova\",\"passenger_email\":\"anna@example.com\",\"seat_count\":4}" \
  "$BASE_URL/bookings" >/dev/null
BOOKING_COUNT_AFTER="$(docker compose exec -T booking-postgres psql -U booking -d booking -Atc \
  'SELECT count(*) FROM bookings')"
[[ "$BOOKING_COUNT_AFTER" == "$BOOKING_COUNT_BEFORE" ]] || {
  echo "FAIL: failed reservation created a booking"
  exit 1
}
echo "PASS: failed reservation leaves Booking PostgreSQL unchanged"

CANCELLED_JSON="$(request 200 "cancel booking" -X POST "$BASE_URL/bookings/$BOOKING_ID/cancel")"
jq -e '.status == "CANCELLED"' <<<"$CANCELLED_JSON" >/dev/null

DATABASE_STATE="$(docker compose exec -T flight-postgres psql -U flight -d flight -Atc \
  "SELECT f.available_seats || '|' || r.status FROM flights f JOIN seat_reservations r ON r.flight_id = f.id WHERE r.booking_id = '$BOOKING_ID'")"
[[ "$DATABASE_STATE" == "180|RELEASED" ]] || {
  echo "FAIL: expected restored seats and RELEASED reservation, got $DATABASE_STATE"
  exit 1
}
echo "PASS: cancellation restored seats and released the reservation"
echo "All E2E checks passed."

