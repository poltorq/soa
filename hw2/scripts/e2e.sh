#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
PRODUCT_ID="10000000-0000-0000-0000-000000000002"
INACTIVE_PRODUCT_ID="10000000-0000-0000-0000-000000000003"
MISSING_ID="ffffffff-ffff-ffff-ffff-ffffffffffff"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

request() {
  local expected="$1"
  local name="$2"
  shift 2
  local body="$TMP_DIR/body.json"
  local headers="$TMP_DIR/headers.txt"
  local status
  status="$(curl --silent --show-error --output "$body" --dump-header "$headers" --write-out '%{http_code}' "$@")"
  if [[ "$status" != "$expected" ]]; then
    echo "FAIL: $name: expected HTTP $expected, got $status"
    cat "$body"
    exit 1
  fi
  if ! grep -qi '^X-Request-Id:' "$headers"; then
    echo "FAIL: $name: X-Request-Id is missing"
    exit 1
  fi
  echo "PASS: $name -> HTTP $status" >&2
  cat "$body"
}

request_error() {
  local expected="$1"
  local code="$2"
  local name="$3"
  shift 3
  local response
  response="$(request "$expected" "$name" "$@")"
  if ! jq -e --arg code "$code" \
    '.error_code == $code and (.message | type == "string" and length > 0)' <<<"$response" >/dev/null; then
    echo "FAIL: $name: expected JSON error $code, got $response" >&2
    exit 1
  fi
  printf '%s\n' "$response"
}

assert_json() {
  local name="$1"
  local response="$2"
  local expression="$3"
  if ! jq -e "$expression" <<<"$response" >/dev/null; then
    echo "FAIL: $name: $response" >&2
    exit 1
  fi
  echo "PASS: $name" >&2
}

sql() {
  docker compose exec -T postgres psql -U marketplace -d marketplace -v ON_ERROR_STOP=1 -Atc "$1"
}

assert_sql() {
  local name="$1"
  local expected="$2"
  local query="$3"
  local actual
  actual="$(sql "$query")"
  if [[ "$actual" != "$expected" ]]; then
    echo "FAIL: $name: expected $expected, got $actual" >&2
    exit 1
  fi
  echo "PASS: $name" >&2
}

echo "Waiting for API..."
for _ in {1..60}; do
  if curl --silent --fail "$BASE_URL/health" >/dev/null; then
    break
  fi
  sleep 1
done
request 200 "health" "$BASE_URL/health" >/dev/null

request_error 401 TOKEN_INVALID "protected endpoint requires JWT" \
  -H 'Authorization: Bearer invalid' \
  "$BASE_URL/products" >/dev/null

USER_LOGIN="$(request 200 "user login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"user12345"}' \
  "$BASE_URL/auth/login")"
USER_TOKEN="$(printf '%s\n' "$USER_LOGIN" | tail -n 1 | jq -er '.access_token')"
REFRESH_TOKEN="$(printf '%s\n' "$USER_LOGIN" | tail -n 1 | jq -er '.refresh_token')"

REFRESH_JSON="$(request 200 "refresh token pair" \
  -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}" \
  "$BASE_URL/auth/refresh")"
USER_TOKEN="$(printf '%s\n' "$REFRESH_JSON" | tail -n 1 | jq -er '.access_token')"

SELLER_LOGIN="$(request 200 "seller login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"seller@example.com","password":"seller12345"}' \
  "$BASE_URL/auth/login")"
SELLER_TOKEN="$(printf '%s\n' "$SELLER_LOGIN" | tail -n 1 | jq -er '.access_token')"

OTHER_SELLER_JSON="$(request 201 "register another SELLER" \
  -H 'Content-Type: application/json' \
  -d '{"email":"e2e-seller@example.com","password":"password123","role":"SELLER"}' \
  "$BASE_URL/auth/register")"
OTHER_SELLER_TOKEN="$(printf '%s\n' "$OTHER_SELLER_JSON" | tail -n 1 | jq -er '.access_token')"

ADMIN_LOGIN="$(request 200 "admin login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"admin12345"}' \
  "$BASE_URL/auth/login")"
ADMIN_TOKEN="$(printf '%s\n' "$ADMIN_LOGIN" | tail -n 1 | jq -er '.access_token')"

LIST_JSON="$(request 200 "filtered product list" \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/products?page=0&size=20&status=ACTIVE&category=electronics")"
assert_json "filter and totalElements match the seed data" "$LIST_JSON" \
  '.totalElements == 2 and .page == 0 and .size == 20 and (.items | length == 2) and (.items | all(.status == "ACTIVE" and .category == "electronics"))'
LIST_JSON="$(request 200 "default pagination" -H "Authorization: Bearer $USER_TOKEN" "$BASE_URL/products")"
assert_json "page and size defaults" "$LIST_JSON" '.page == 0 and .size == 20 and .totalElements == 3'
for read_token in "$SELLER_TOKEN" "$ADMIN_TOKEN"; do
  request 200 "SELLER and ADMIN can read any product" -H "Authorization: Bearer $read_token" "$BASE_URL/products/$PRODUCT_ID" >/dev/null
  request 200 "SELLER and ADMIN can list products" -H "Authorization: Bearer $read_token" "$BASE_URL/products" >/dev/null
done

request_error 403 ACCESS_DENIED "USER cannot create products" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Forbidden","price":10,"stock":1,"category":"test","status":"ACTIVE"}' \
  "$BASE_URL/products" >/dev/null

PRODUCT_JSON="$(request 201 "SELLER creates product" \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"E2E Product","description":"created by test","price":99.99,"stock":4,"category":"test","status":"ACTIVE"}' \
  "$BASE_URL/products")"
NEW_PRODUCT_ID="$(printf '%s\n' "$PRODUCT_JSON" | tail -n 1 | jq -er '.id')"

request 200 "get product by id" \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/products/$NEW_PRODUCT_ID" >/dev/null

request 200 "SELLER updates own product" \
  -X PUT \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"E2E Product Updated","price":109.99,"stock":5,"category":"test","status":"ACTIVE"}' \
  "$BASE_URL/products/$NEW_PRODUCT_ID" >/dev/null

request_error 403 ACCESS_DENIED "SELLER cannot update another seller product" \
  -X PUT \
  -H "Authorization: Bearer $OTHER_SELLER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Forbidden Update","price":109.99,"stock":5,"category":"test","status":"ACTIVE"}' \
  "$BASE_URL/products/$NEW_PRODUCT_ID" >/dev/null

request 204 "SELLER archives own product" \
  -X DELETE \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  "$BASE_URL/products/$NEW_PRODUCT_ID" >/dev/null

ARCHIVED_JSON="$(request 200 "archived product remains readable" \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/products/$NEW_PRODUCT_ID")"
[[ "$(printf '%s\n' "$ARCHIVED_JSON" | tail -n 1 | jq -r '.status')" == "ARCHIVED" ]] || {
  echo "FAIL: product was not soft-deleted"
  exit 1
}

request 201 "SELLER creates promo code" \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"code":"E2ETEST","discount_type":"PERCENTAGE","discount_value":15,"min_order_amount":1000,"max_uses":5,"valid_from":"2020-01-01T00:00:00Z","valid_until":"2099-01-01T00:00:00Z","active":true}' \
  "$BASE_URL/promo-codes" >/dev/null

request_error 403 ACCESS_DENIED "USER cannot create promo codes" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"code":"NOACCESS","discount_type":"FIXED_AMOUNT","discount_value":10,"min_order_amount":0,"max_uses":1,"valid_from":"2020-01-01T00:00:00Z","valid_until":"2099-01-01T00:00:00Z","active":true}' \
  "$BASE_URL/promo-codes" >/dev/null

request_error 400 VALIDATION_ERROR "contract validation runs first" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"items":[]}' \
  "$BASE_URL/orders" >/dev/null

request_error 400 VALIDATION_ERROR "duplicate products are rejected" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1},{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}]}" \
  "$BASE_URL/orders" >/dev/null

request_error 409 PRODUCT_INACTIVE "inactive product is rejected" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$INACTIVE_PRODUCT_ID\",\"quantity\":1}]}" \
  "$BASE_URL/orders" >/dev/null

request_error 404 PRODUCT_NOT_FOUND "missing product is rejected" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$MISSING_ID\",\"quantity\":1}]}" \
  "$BASE_URL/orders" >/dev/null

request_error 409 INSUFFICIENT_STOCK "insufficient stock is rejected" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":999}]}" \
  "$BASE_URL/orders" >/dev/null

request_error 422 PROMO_CODE_INVALID "invalid promo code is rejected" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}],\"promo_code\":\"UNKNOWN1\"}" \
  "$BASE_URL/orders" >/dev/null

request 200 "ADMIN reactivates archived product" \
  -X PUT \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"E2E Product Updated","price":109.99,"stock":5,"category":"test","status":"ACTIVE"}' \
  "$BASE_URL/products/$NEW_PRODUCT_ID" >/dev/null

request_error 422 PROMO_CODE_MIN_AMOUNT "promo minimum amount is enforced" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$NEW_PRODUCT_ID\",\"quantity\":1}],\"promo_code\":\"WELCOME10\"}" \
  "$BASE_URL/orders" >/dev/null

request 204 "ADMIN archives any product" \
  -X DELETE \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE_URL/products/$NEW_PRODUCT_ID" >/dev/null

ORDER_JSON="$(request 201 "create order and reserve stock" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}],\"promo_code\":\"WELCOME10\"}" \
  "$BASE_URL/orders")"
ORDER_ID="$(printf '%s\n' "$ORDER_JSON" | tail -n 1 | jq -er '.id')"

request_error 429 ORDER_LIMIT_EXCEEDED "create-order rate limit" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}]}" \
  "$BASE_URL/orders" >/dev/null

docker compose exec -T postgres psql -U marketplace -d marketplace -v ON_ERROR_STOP=1 -c \
  "UPDATE user_operations SET created_at = now() - interval '2 minutes' WHERE user_id = '00000000-0000-0000-0000-000000000003' AND operation_type = 'CREATE_ORDER'" >/dev/null
request_error 409 ORDER_HAS_ACTIVE "second active order is rejected" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}]}" \
  "$BASE_URL/orders" >/dev/null

request 200 "owner reads order" \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/orders/$ORDER_ID" >/dev/null

request_error 403 ACCESS_DENIED "SELLER cannot read orders" \
  -H "Authorization: Bearer $SELLER_TOKEN" \
  "$BASE_URL/orders/$ORDER_ID" >/dev/null

OTHER_JSON="$(request 201 "register another USER" \
  -H 'Content-Type: application/json' \
  -d '{"email":"e2e-owner@example.com","password":"password123","role":"USER"}' \
  "$BASE_URL/auth/register")"
OTHER_TOKEN="$(printf '%s\n' "$OTHER_JSON" | tail -n 1 | jq -er '.access_token')"

request_error 403 ORDER_OWNERSHIP_VIOLATION "another USER cannot read the order" \
  -H "Authorization: Bearer $OTHER_TOKEN" \
  "$BASE_URL/orders/$ORDER_ID" >/dev/null

request 200 "owner updates CREATED order" \
  -X PUT \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":3}]}" \
  "$BASE_URL/orders/$ORDER_ID" >/dev/null

request_error 429 ORDER_LIMIT_EXCEEDED "update-order rate limit" \
  -X PUT \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":2}]}" \
  "$BASE_URL/orders/$ORDER_ID" >/dev/null

request 200 "cancel restores stock and promo usage" \
  -X POST \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/orders/$ORDER_ID/cancel" >/dev/null

request_error 404 ORDER_NOT_FOUND "missing order returns ORDER_NOT_FOUND" \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/orders/$MISSING_ID" >/dev/null

ADMIN_ORDER_JSON="$(request 201 "ADMIN creates own order" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}]}" \
  "$BASE_URL/orders")"
ADMIN_ORDER_ID="$(printf '%s\n' "$ADMIN_ORDER_JSON" | tail -n 1 | jq -er '.id')"

request_error 409 INVALID_STATE_TRANSITION "invalid state transition is rejected" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"PAID"}' \
  "$BASE_URL/orders/$ADMIN_ORDER_ID/status" >/dev/null

request 200 "valid state transition" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"PAYMENT_PENDING"}' \
  "$BASE_URL/orders/$ADMIN_ORDER_ID/status" >/dev/null

request 200 "cancel PAYMENT_PENDING order" \
  -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE_URL/orders/$ADMIN_ORDER_ID/cancel" >/dev/null

STOCK="$(docker compose exec -T postgres psql -U marketplace -d marketplace -Atc \
  "SELECT stock FROM products WHERE id = '$PRODUCT_ID'")"
PROMO_USES="$(docker compose exec -T postgres psql -U marketplace -d marketplace -Atc \
  "SELECT current_uses FROM promo_codes WHERE code = 'WELCOME10'")"
[[ "$STOCK" == "20" ]] || { echo "FAIL: stock was not restored: $STOCK"; exit 1; }
[[ "$PROMO_USES" == "0" ]] || { echo "FAIL: promo usage was not restored: $PROMO_USES"; exit 1; }

echo "PASS: database invariants (stock=20, promo uses=0)"

# Validation and order edge cases.
for path in /products/not-a-uuid /orders/not-a-uuid; do
  ERROR_JSON="$(request_error 400 VALIDATION_ERROR "invalid UUID has a JSON error: $path" \
    -H "Authorization: Bearer $USER_TOKEN" "$BASE_URL$path")"
  assert_json "invalid UUID identifies id" "$ERROR_JSON" '.details.violations | any(.field == "id")'
done

request_error 400 VALIDATION_ERROR "page overflow is rejected" \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/products?page=9223372036854775807&size=100" >/dev/null
PAGE_JSON="$(request 200 "maximum page avoids offset overflow" \
  -H "Authorization: Bearer $USER_TOKEN" \
  "$BASE_URL/products?page=2147483647&size=100")"
assert_json "maximum page is empty and preserves pagination" "$PAGE_JSON" \
  '.page == 2147483647 and .size == 100 and (.items | length == 0)'

for payload in \
  '{"name":"Overflow","price":10000000000,"stock":1,"category":"test","status":"ACTIVE"}' \
  '{"name":"Overflow","price":1,"stock":2147483648,"category":"test","status":"ACTIVE"}'; do
  request_error 400 VALIDATION_ERROR "numeric overflow is rejected before SQL" \
    -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
    -d "$payload" "$BASE_URL/products" >/dev/null
done

ERROR_JSON="$(request_error 400 VALIDATION_ERROR "all invalid product fields are reported" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"","price":0,"stock":-1,"category":"","status":"ACTIVE"}' "$BASE_URL/products")"
assert_json "four field violations" "$ERROR_JSON" \
  '[.details.violations[].field] | sort == ["category","name","price","stock"]'

ERROR_JSON="$(request_error 400 VALIDATION_ERROR "nested quantity violations" \
  -H "Authorization: Bearer $USER_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":0}]}" "$BASE_URL/orders")"
assert_json "nested field path includes index" "$ERROR_JSON" \
  '.details.violations | any(.field == "items[0].quantity")'

UNICODE_INVALID="$(jq -nc '{email:"e2e-utf8@example.com",password:("я" * 40),role:"USER"}')"
request_error 400 VALIDATION_ERROR "UTF-8 password above 72 bytes is rejected" \
  -H 'Content-Type: application/json' -d "$UNICODE_INVALID" "$BASE_URL/auth/register" >/dev/null
request_error 400 VALIDATION_ERROR "login also enforces the UTF-8 byte limit" \
  -H 'Content-Type: application/json' -d "$UNICODE_INVALID" "$BASE_URL/auth/login" >/dev/null

UNICODE_VALID="$(jq -nc '{email:"e2e-utf8@example.com",password:("я" * 36),role:"USER"}')"
UTF8_JSON="$(request 201 "72-byte UTF-8 password is accepted" \
  -H 'Content-Type: application/json' -d "$UNICODE_VALID" "$BASE_URL/auth/register")"
UTF8_TOKEN="$(jq -er '.access_token' <<<"$UTF8_JSON")"
request 200 "72-byte UTF-8 password can log in" \
  -H 'Content-Type: application/json' -d "$UNICODE_VALID" "$BASE_URL/auth/login" >/dev/null

EXPENSIVE_JSON="$(request 201 "create high-price product within database range" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Expensive","price":6000000000,"stock":2,"category":"test","status":"ACTIVE"}' "$BASE_URL/products")"
EXPENSIVE_ID="$(jq -er '.id' <<<"$EXPENSIVE_JSON")"
ERROR_JSON="$(request_error 400 VALIDATION_ERROR "order subtotal overflow returns 400" \
  -H "Authorization: Bearer $UTF8_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$EXPENSIVE_ID\",\"quantity\":2}]}" "$BASE_URL/orders")"
assert_json "amount overflow identifies items" "$ERROR_JSON" \
  '.details.violations | any(.field == "items")'
assert_sql "failed create keeps stock" "2" "SELECT stock FROM products WHERE id = '$EXPENSIVE_ID'"
assert_sql "failed create leaves no order or operation" "0|0" \
  "SELECT (SELECT count(*) FROM orders WHERE user_id = u.id), (SELECT count(*) FROM user_operations WHERE user_id = u.id) FROM users u WHERE email = 'e2e-utf8@example.com'"

SNAPSHOT_PRODUCT="$(request 201 "create snapshot product" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Snapshot","price":100,"stock":10,"category":"test","status":"ACTIVE"}' "$BASE_URL/products")"
SNAPSHOT_ID="$(jq -er '.id' <<<"$SNAPSHOT_PRODUCT")"
ADDED_PRODUCT="$(request 201 "create product to add later" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Added","price":50,"stock":5,"category":"test","status":"ACTIVE"}' "$BASE_URL/products")"
ADDED_ID="$(jq -er '.id' <<<"$ADDED_PRODUCT")"
request 201 "create promo for minimum amount recalculation" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"E2EMIN","discount_type":"FIXED_AMOUNT","discount_value":10,"min_order_amount":150,"max_uses":10,"valid_from":"2020-01-01T00:00:00Z","valid_until":"2099-01-01T00:00:00Z","active":true}' "$BASE_URL/promo-codes" >/dev/null

SNAPSHOT_ORDER="$(request 201 "order records original price and discount" \
  -H "Authorization: Bearer $UTF8_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":2}],\"promo_code\":\"E2EMIN\"}" "$BASE_URL/orders")"
SNAPSHOT_ORDER_ID="$(jq -er '.id' <<<"$SNAPSHOT_ORDER")"
assert_json "initial snapshot totals" "$SNAPSHOT_ORDER" \
  '.total_amount == 190 and .discount_amount == 10 and .items[0].price_at_order == 100'

request_error 403 ACCESS_DENIED "USER cannot update products" -X PUT \
  -H "Authorization: Bearer $USER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Forbidden","price":100,"stock":8,"category":"test","status":"ACTIVE"}' "$BASE_URL/products/$SNAPSHOT_ID" >/dev/null
request_error 403 ACCESS_DENIED "USER cannot delete products" -X DELETE \
  -H "Authorization: Bearer $USER_TOKEN" "$BASE_URL/products/$SNAPSHOT_ID" >/dev/null
request_error 403 ACCESS_DENIED "SELLER cannot create orders" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":1}]}" "$BASE_URL/orders" >/dev/null
request_error 403 ACCESS_DENIED "SELLER cannot update orders" -X PUT \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":1}]}" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID" >/dev/null
request_error 403 ACCESS_DENIED "SELLER cannot cancel orders" -X POST \
  -H "Authorization: Bearer $SELLER_TOKEN" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID/cancel" >/dev/null
request_error 403 ORDER_OWNERSHIP_VIOLATION "USER cannot update another user order" -X PUT \
  -H "Authorization: Bearer $USER_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":1}]}" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID" >/dev/null
request_error 403 ORDER_OWNERSHIP_VIOLATION "USER cannot cancel another user order" -X POST \
  -H "Authorization: Bearer $USER_TOKEN" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID/cancel" >/dev/null
request 200 "ADMIN reads another user order" -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID" >/dev/null
request 200 "ADMIN updates another user order" -X PUT \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":2}]}" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID" >/dev/null
ADMIN_PRODUCT="$(request 201 "ADMIN creates a product" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Admin product","price":1,"stock":0,"category":"test","status":"ACTIVE"}' "$BASE_URL/products")"
assert_json "product owner comes from authenticated identity" "$ADMIN_PRODUCT" \
  '.seller_id == "00000000-0000-0000-0000-000000000001"'

request_error 400 VALIDATION_ERROR "update subtotal overflow rolls back" -X PUT \
  -H "Authorization: Bearer $UTF8_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$EXPENSIVE_ID\",\"quantity\":2}]}" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID" >/dev/null
assert_sql "failed update preserves order, stock and promo" "190.00|10.00|8|2|1|0" \
  "SELECT o.total_amount, o.discount_amount, (SELECT stock FROM products WHERE id = '$SNAPSHOT_ID'), (SELECT stock FROM products WHERE id = '$EXPENSIVE_ID'), (SELECT current_uses FROM promo_codes WHERE code = 'E2EMIN'), (SELECT count(*) FROM user_operations WHERE user_id = o.user_id AND operation_type = 'UPDATE_ORDER') FROM orders o WHERE id = '$SNAPSHOT_ORDER_ID'"

request 200 "catalogue price changes" -X PUT \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Snapshot","price":200,"stock":8,"category":"test","status":"ACTIVE"}' "$BASE_URL/products/$SNAPSHOT_ID" >/dev/null
SNAPSHOT_ORDER="$(request 200 "read keeps price snapshot" \
  -H "Authorization: Bearer $UTF8_TOKEN" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID")"
assert_json "read ignores catalogue price changes" "$SNAPSHOT_ORDER" \
  '.total_amount == 190 and .items[0].price_at_order == 100'

SNAPSHOT_ORDER="$(request 200 "update preserves old prices and snapshots added products" -X PUT \
  -H "Authorization: Bearer $UTF8_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":1},{\"product_id\":\"$ADDED_ID\",\"quantity\":1}]}" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID")"
assert_json "mixed old and new snapshot totals" "$SNAPSHOT_ORDER" \
  ".total_amount == 140 and .discount_amount == 10 and (.items | any(.product_id == \"$SNAPSHOT_ID\" and .price_at_order == 100)) and (.items | any(.product_id == \"$ADDED_ID\" and .price_at_order == 50))"

sql "UPDATE user_operations SET created_at = now() - interval '2 minutes' WHERE user_id = (SELECT id FROM users WHERE email = 'e2e-utf8@example.com') AND operation_type = 'UPDATE_ORDER'" >/dev/null
SNAPSHOT_ORDER="$(request 200 "smaller order releases promo below minimum" -X PUT \
  -H "Authorization: Bearer $UTF8_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":1}]}" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID")"
assert_json "promo removed after recalculation" "$SNAPSHOT_ORDER" \
  '.total_amount == 100 and .discount_amount == 0 and .promo_code == null and .items[0].price_at_order == 100'
assert_sql "promo usage released in database" "0|t" \
  "SELECT p.current_uses, o.promo_code_id IS NULL FROM promo_codes p CROSS JOIN orders o WHERE p.code = 'E2EMIN' AND o.id = '$SNAPSHOT_ORDER_ID'"
request 200 "cancel snapshot order" -X POST \
  -H "Authorization: Bearer $UTF8_TOKEN" "$BASE_URL/orders/$SNAPSHOT_ORDER_ID/cancel" >/dev/null
assert_sql "all snapshot product stock restored" "10|5" \
  "SELECT (SELECT stock FROM products WHERE id = '$SNAPSHOT_ID'), (SELECT stock FROM products WHERE id = '$ADDED_ID')"

request 201 "ADMIN creates promo above seventy percent" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"E2E90","discount_type":"PERCENTAGE","discount_value":90,"min_order_amount":0,"max_uses":10,"valid_from":"2020-01-01T00:00:00Z","valid_until":"2099-01-01T00:00:00Z","active":true}' "$BASE_URL/promo-codes" >/dev/null
sql "UPDATE user_operations SET created_at = now() - interval '2 minutes' WHERE user_id = (SELECT id FROM users WHERE email = 'e2e-utf8@example.com') AND operation_type = 'CREATE_ORDER'" >/dev/null
PERCENT_ORDER="$(request 201 "percentage above seventy keeps full price" \
  -H "Authorization: Bearer $UTF8_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$SNAPSHOT_ID\",\"quantity\":1}],\"promo_code\":\"E2E90\"}" "$BASE_URL/orders")"
assert_json "ninety percent promo gives zero discount" "$PERCENT_ORDER" \
  '.total_amount == 200 and .discount_amount == 0'
PERCENT_ORDER_ID="$(jq -er '.id' <<<"$PERCENT_ORDER")"
request 200 "ADMIN cancels another user order" -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE_URL/orders/$PERCENT_ORDER_ID/cancel" >/dev/null
assert_sql "full-price promo cancellation releases usage and stock" "0|10" \
  "SELECT current_uses, (SELECT stock FROM products WHERE id = '$SNAPSHOT_ID') FROM promo_codes WHERE code = 'E2E90'"

request_error 404 ENDPOINT_NOT_FOUND "unknown route has JSON 404" \
  -H "Authorization: Bearer $USER_TOKEN" "$BASE_URL/missing-endpoint" >/dev/null
request_error 405 METHOD_NOT_ALLOWED "unsupported method has JSON 405" -X PATCH \
  -H "Authorization: Bearer $USER_TOKEN" "$BASE_URL/products" >/dev/null
request_error 400 VALIDATION_ERROR "invalid email is rejected by the contract" \
  -H 'Content-Type: application/json' -d '{"email":"not-an-email","password":"password123","role":"USER"}' "$BASE_URL/auth/register" >/dev/null
request_error 400 VALIDATION_ERROR "ADMIN cannot be self-assigned" \
  -H 'Content-Type: application/json' -d '{"email":"e2e-escalation@example.com","password":"password123","role":"ADMIN"}' "$BASE_URL/auth/register" >/dev/null
request_error 400 VALIDATION_ERROR "concatenated JSON is rejected" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Bad JSON","price":1,"stock":0,"category":"test","status":"ACTIVE"} {}' "$BASE_URL/products" >/dev/null

request 201 "ADMIN creates a fifty percent promo" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"E2EHALF","discount_type":"PERCENTAGE","discount_value":50,"min_order_amount":0,"max_uses":1,"valid_from":"2020-01-01T00:00:00Z","valid_until":"2099-01-01T00:00:00Z","active":true}' "$BASE_URL/promo-codes" >/dev/null
sql "UPDATE user_operations SET created_at = now() - interval '2 minutes' WHERE user_id = (SELECT id FROM users WHERE email = 'e2e-utf8@example.com') AND operation_type = 'CREATE_ORDER'" >/dev/null
LARGE_ORDER="$(request 201 "large subtotal with representable discounted amounts" \
  -H "Authorization: Bearer $UTF8_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$EXPENSIVE_ID\",\"quantity\":2}],\"promo_code\":\"E2EHALF\"}" "$BASE_URL/orders")"
LARGE_ORDER_ID="$(jq -er '.id' <<<"$LARGE_ORDER")"
assert_json "both large amounts fit NUMERIC(12,2)" "$LARGE_ORDER" \
  '.total_amount == 6000000000 and .discount_amount == 6000000000'
request_error 400 VALIDATION_ERROR "stock update leaves capacity for reservations" -X PUT \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Expensive","price":6000000000,"stock":2147483647,"category":"test","status":"ACTIVE"}' "$BASE_URL/products/$EXPENSIVE_ID" >/dev/null
assert_sql "rejected restock keeps reserved product unchanged" "0" "SELECT stock FROM products WHERE id = '$EXPENSIVE_ID'"
request 200 "restock at the safe boundary" -X PUT \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Expensive","price":6000000000,"stock":2147483645,"category":"test","status":"ACTIVE"}' "$BASE_URL/products/$EXPENSIVE_ID" >/dev/null
request 200 "cancel at the stock boundary" -X POST \
  -H "Authorization: Bearer $UTF8_TOKEN" "$BASE_URL/orders/$LARGE_ORDER_ID/cancel" >/dev/null
assert_sql "cancellation reaches maximum stock without overflow" "2147483647" "SELECT stock FROM products WHERE id = '$EXPENSIVE_ID'"

STATE_ORDER="$(request 201 "create order for the full state machine" \
  -H "Authorization: Bearer $OTHER_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}]}" "$BASE_URL/orders")"
STATE_ORDER_ID="$(jq -er '.id' <<<"$STATE_ORDER")"
request_error 403 ACCESS_DENIED "USER cannot advance order status" \
  -H "Authorization: Bearer $OTHER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"status":"PAYMENT_PENDING"}' "$BASE_URL/orders/$STATE_ORDER_ID/status" >/dev/null
request_error 403 ACCESS_DENIED "SELLER cannot advance order status" \
  -H "Authorization: Bearer $SELLER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"status":"PAYMENT_PENDING"}' "$BASE_URL/orders/$STATE_ORDER_ID/status" >/dev/null
for state in PAYMENT_PENDING PAID SHIPPED COMPLETED; do
  STATE_ORDER="$(request 200 "valid state transition to $state" \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
    -d "{\"status\":\"$state\"}" "$BASE_URL/orders/$STATE_ORDER_ID/status")"
  assert_json "response has status $state" "$STATE_ORDER" ".status == \"$state\""
  request_error 409 INVALID_STATE_TRANSITION "items cannot change in $state" -X PUT \
    -H "Authorization: Bearer $OTHER_TOKEN" -H 'Content-Type: application/json' \
    -d "{\"items\":[{\"product_id\":\"$PRODUCT_ID\",\"quantity\":1}]}" "$BASE_URL/orders/$STATE_ORDER_ID" >/dev/null
  if [[ "$state" != "PAYMENT_PENDING" ]]; then
    request_error 409 INVALID_STATE_TRANSITION "cannot cancel $state" -X POST \
      -H "Authorization: Bearer $OTHER_TOKEN" "$BASE_URL/orders/$STATE_ORDER_ID/cancel" >/dev/null
  fi
done
request_error 409 INVALID_STATE_TRANSITION "terminal status cannot be reversed" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"status":"CREATED"}' "$BASE_URL/orders/$STATE_ORDER_ID/status" >/dev/null
assert_sql "completed order retains sold stock" "19|COMPLETED" \
  "SELECT (SELECT stock FROM products WHERE id = '$PRODUCT_ID'), status FROM orders WHERE id = '$STATE_ORDER_ID'"

echo "All E2E checks passed."
