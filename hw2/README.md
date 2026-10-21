# Marketplace API

Contract-first REST API на Go для товаров, заказов и промокодов.

## Почему такая архитектура

Выбран **модульный монолит**, а не микросервисы. Бизнес-транзакция заказа должна атомарно изменить заказ, остатки, счетчик промокода и журнал операций. Одна PostgreSQL и одна транзакция дают простую и надежную гарантию: либо применятся все изменения, либо ни одно. Разделение на сетевые сервисы потребовало бы брокера, outbox/saga, идемпотентности и eventual consistency, но не принесло бы пользы при текущем масштабе.

Границы модулей уже выделены: HTTP/OpenAPI в `internal/server`, бизнес-операции и SQL в `internal/store`, JWT в `internal/auth`, конфигурация в `internal/config`. Если независимое масштабирование действительно понадобится, эти границы можно превратить в сервисы без преждевременного усложнения проекта.

Поток запроса: `client -> chi middleware -> OpenAPI validation -> generated strict handler -> store transaction -> PostgreSQL`. DTO и интерфейсы HTTP генерируются из спецификации, а доменные структуры БД не протекают в API.

## Технологии

- **Go 1.26**: основной язык, стандартный `net/http`, контексты и graceful shutdown.
- **OpenAPI 3.0 + oapi-codegen**: контракт, валидация, DTO и strict server interfaces.
- **chi**: легкий HTTP-router и middleware chain.
- **PostgreSQL 16 + pgx**: хранение, транзакции, `FOR UPDATE`, advisory locks и ограничения целостности.
- **Flyway**: версионированные миграции перед запуском приложения.
- **JWT**: access token на 20 минут и refresh token на 14 дней.
- **Docker Compose**: одинаковый запуск PostgreSQL, Flyway и API одной командой.
- **slog JSON**: структурированный лог каждого API-запроса.

## Быстрый запуск

Нужны Docker с Compose v2, `curl` и `jq`. Для Colima сначала выполните `colima start`.

```bash
cd hw2
docker compose up --build -d
docker compose ps
docker compose logs -f api
```

Когда контейнер `api` станет `healthy`:

```bash
curl -i http://localhost:8080/health
```

Остановка без удаления данных: `docker compose down`. Полный сброс БД и повторный запуск: `make reset`. Команда `docker compose up --build -d` корректна только для Compose; если Docker пишет `unknown flag: --build`, проверьте `docker compose version` и не используйте старую команду `docker-compose`.

## Проверка через терминал

Получить токен обычного пользователя:

```bash
TOKEN=$(curl -s http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"user12345"}' | jq -r .access_token)
```

Получить товары и создать заказ:

```bash
curl -s 'http://localhost:8080/products?page=0&size=20&status=ACTIVE&category=electronics' \
  -H "Authorization: Bearer $TOKEN" | jq

curl -s http://localhost:8080/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"product_id":"10000000-0000-0000-0000-000000000002","quantity":2}],"promo_code":"WELCOME10"}' | jq
```

Посмотреть схему и данные напрямую в PostgreSQL:

```bash
docker compose exec postgres psql -U marketplace -d marketplace -c '\dt'
docker compose exec postgres psql -U marketplace -d marketplace -c \
  'SELECT id,name,price,stock,status,seller_id FROM products ORDER BY created_at;'
docker compose exec postgres psql -U marketplace -d marketplace -c \
  'SELECT id,user_id,status,total_amount,discount_amount,promo_code_id FROM orders ORDER BY created_at;'
docker compose exec postgres psql -U marketplace -d marketplace -c \
  'SELECT order_id,product_id,quantity,price_at_order FROM order_items;'
docker compose exec postgres psql -U marketplace -d marketplace -c \
  'SELECT user_id,operation_type,created_at FROM user_operations ORDER BY created_at;'
```

Полная автоматическая демонстрация ролей, валидации, заказа, rate limit, отмены и восстановления БД:

```bash
make reset
make e2e
```

## Тестовые пользователи

| Роль | Email | Пароль |
|---|---|---|
| ADMIN | `admin@example.com` | `admin12345` |
| SELLER | `seller@example.com` | `seller12345` |
| USER | `user@example.com` | `user12345` |

USER читает товары и работает только со своими заказами. SELLER управляет только своими товарами и создает промокоды, но не имеет доступа к заказам. ADMIN имеет полный доступ. Регистрация разрешает только USER и SELLER, поэтому клиент не может сам выдать себе ADMIN.

## OpenAPI и генерация

Источник истины: `api/openapi.yaml`. DTO вручную не дублируются. Генерируемый файл исключен из Git и создается одной командой:

```bash
go generate ./internal/api
```

То же выполняют `make generate`, сборка Docker и проверки Makefile. Спецификация содержит ограничения строк, UUID, enum, диапазоны количества, формат промокода, security scheme, роли и единый `ErrorResponse`.

## Заказы и транзакции

Создание заказа в одной транзакции блокирует операции пользователя и строки товаров, проверяет rate limit, активный заказ, статус и остатки, фиксирует `price_at_order`, резервирует склад, применяет промокод и пишет `user_operations`. Обновление сначала учитывает возврат старого резерва, затем резервирует новый состав. Отмена возвращает склад и использование промокода. Любая ошибка вызывает rollback.

Допустимые переходы: `CREATED -> PAYMENT_PENDING -> PAID -> SHIPPED -> COMPLETED`. Отмена разрешена из `CREATED` и `PAYMENT_PENDING`. Для конкуренции используются транзакционные advisory locks, `SELECT ... FOR UPDATE`, ограничения БД и частичный уникальный индекс одного активного заказа.

## Ошибки и наблюдаемость

Все ошибки имеют вид `{"error_code":"...","message":"...","details":{...}}`. Реализованы требуемые коды: `PRODUCT_NOT_FOUND`, `PRODUCT_INACTIVE`, `ORDER_NOT_FOUND`, `ORDER_LIMIT_EXCEEDED`, `ORDER_HAS_ACTIVE`, `INVALID_STATE_TRANSITION`, `INSUFFICIENT_STOCK`, `PROMO_CODE_INVALID`, `PROMO_CODE_MIN_AMOUNT`, `ORDER_OWNERSHIP_VIOLATION`, `VALIDATION_ERROR`, а также JWT/RBAC-коды.

OpenAPI middleware проверяет контракт до бизнес-логики. Каждый ответ содержит `X-Request-Id`. Каждый запрос дает JSON-лог с `request_id`, методом, endpoint, HTTP-статусом, длительностью, пользователем и временем; пароли и токены в mutation body заменяются на `***`.

## Локальная проверка качества

```bash
make check       # format, generation, vet, tests, race detector
make cover       # отчет покрытия
make build       # локальная сборка
docker compose config --quiet
```
