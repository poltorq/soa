# Flight Booking

Распределённая система бронирования авиабилетов из двух микросервисов.

```text
Клиент
  │ REST :8080
  ▼
Booking Service ───── gRPC :9090 ─────► Flight Service
  │                                       │          │
  ▼                                       ▼          ▼
Booking PostgreSQL                  Flight PostgreSQL Redis Sentinel
```

Booking Service отвечает за пассажира, итоговую цену и статус бронирования. Flight Service отвечает за рейсы, остатки мест и резервации. У сервисов разные PostgreSQL: чужая база не читается напрямую.

### Где находится логика

- `cmd/*/main.go` — только сборка зависимостей и запуск сервисов.
- `internal/booking/server.go` — принимает REST-запросы и формирует HTTP-ответы.
- `internal/booking/service.go` — выполняет сценарии создания и отмены бронирования.
- `internal/booking/flight_client.go` — вызывает Flight Service по gRPC.
- `internal/booking/store.go` — работает только с Booking PostgreSQL.
- `internal/flight/service.go` — выполняет gRPC-сценарии Flight Service.
- `internal/flight/validation.go` и `mapping.go` — проверяют protobuf-запросы и преобразуют модели.
- `internal/flight/store.go` — выполняет SQL и транзакции Flight PostgreSQL.
- `internal/flight/cache.go` — работает только с Redis.
- `internal/resilience` — содержит retry и circuit breaker для gRPC-клиента.

Направление вызовов в Booking Service: `HTTP server → Service → Store / FlightClient`. HTTP-слой не обращается к БД или gRPC-клиенту напрямую.

## Запуск

Требуются Docker, Docker Compose, `curl` и `jq`.

```bash
cd /Users/poltorq/Downloads/soa_1/hw3
docker compose up --build -d
docker compose ps -a
```

Нормальное состояние:

- `booking-service`, `flight-service`, обе PostgreSQL и три Redis-контейнера — `healthy`;
- `booking-flyway` и `flight-flyway` — `Exited (0)`, потому что миграции завершились успешно.

Проверка:

```bash
curl http://localhost:8080/health
```

Остановка без удаления данных:

```bash
docker compose down
```

Полный сброс БД и Redis:

```bash
docker compose down -v --remove-orphans
docker compose up --build -d
```

## Быстрая демонстрация

Поиск рейсов:

```bash
curl 'http://localhost:8080/flights?origin=SVO&destination=LED&date=2026-04-01' | jq
```

Получение рейса:

```bash
curl http://localhost:8080/flights/10000000-0000-0000-0000-000000000001 | jq
```

Создание бронирования:

```bash
curl -X POST http://localhost:8080/bookings \
  -H 'Content-Type: application/json' \
  -d '{
    "user_id": "20000000-0000-0000-0000-000000000001",
    "flight_id": "10000000-0000-0000-0000-000000000001",
    "passenger_name": "Anna Petrova",
    "passenger_email": "anna@example.com",
    "seat_count": 2
  }' | jq
```

В ответе нужно скопировать `id`. Получение и отмена:

```bash
curl http://localhost:8080/bookings/BOOKING_ID | jq
curl -X POST http://localhost:8080/bookings/BOOKING_ID/cancel | jq
```

Список бронирований пользователя:

```bash
curl 'http://localhost:8080/bookings?user_id=20000000-0000-0000-0000-000000000001' | jq
```

REST-контракт находится в [api/openapi.yaml](api/openapi.yaml). Его можно импортировать в Postman.

## Создание бронирования

```text
POST /bookings
      │
      ├─ GetFlight ───────────────► узнать цену
      │
      ├─ ReserveSeats ────────────► транзакционно уменьшить остаток
      │                              и создать SeatReservation
      │
      └─ INSERT booking ──────────► сохранить CONFIRMED и снимок цены
```

`total_price_cents = seat_count × price_cents`. Цена хранится в целых копейках, поэтому вычисление не получает ошибок двоичной арифметики `float`.

Если `ReserveSeats` завершился ошибкой, Booking Service не пишет бронирование. Если PostgreSQL Booking Service отказал уже после успешной резервации, выполняется компенсирующий `ReleaseReservation`.

## Защита от двойного бронирования

Flight Service выполняет в одной PostgreSQL-транзакции:

1. блокировку операции по `booking_id`;
2. `SELECT ... FOR UPDATE` строки рейса;
3. проверку статуса и остатка;
4. уменьшение `available_seats`;
5. создание `seat_reservations`;
6. `COMMIT`.

Уникальное ограничение на `seat_reservations.booking_id` и проверка существующей резервации делают `ReserveSeats` идемпотентным. Повтор с теми же параметрами возвращает прежний результат и не списывает места ещё раз.

`ReleaseReservation` также идемпотентен: повторная отмена не возвращает места дважды.

## gRPC

Контракт находится в [proto/flight/v1/flight.proto](proto/flight/v1/flight.proto). В нём есть бизнес-методы:

- `SearchFlights`;
- `GetFlight`;
- `ReserveSeats`;
- `ReleaseReservation`;
- `UpdateFlight`.

Даты передаются через `google.protobuf.Timestamp`, статусы — через `enum`. Go-код клиента и сервера генерируется командой:

```bash
make generate
```

Для генерации используется одноразовый Docker-контейнер с `protoc`; устанавливать `protoc` на компьютер не нужно. Сгенерированные файлы находятся в `gen/flight/v1`.

Flight Service возвращает стандартные коды gRPC:

- `INVALID_ARGUMENT` — неправильные параметры;
- `NOT_FOUND` — рейс или резервация не найдены;
- `RESOURCE_EXHAUSTED` — мест недостаточно;
- `FAILED_PRECONDITION` — рейс нельзя резервировать в текущем статусе;
- `UNAUTHENTICATED` — отсутствует или неверен межсервисный ключ;
- `UNAVAILABLE` — сервис недоступен или circuit breaker открыт.

## Межсервисная аутентификация

Booking Service кладёт API key в gRPC metadata:

```text
x-api-key: local-flight-service-key
```

Flight Service проверяет ключ в unary interceptor до вызова метода. Значение передаётся через `GRPC_API_KEY` в Docker Compose, а не хранится в бизнес-коде.

## Redis и cache-aside

Flight Service использует Redis через Sentinel-aware клиент. В Compose работают:

- `redis-master`;
- `redis-replica`;
- `redis-sentinel`.

Ключи:

```text
flight:{id}
search:{origin}:{destination}:{date}
```

Для даты без фильтра используется `any`. TTL по умолчанию — 5 минут.

Чтение работает по схеме cache-aside:

```text
Redis hit  ─► вернуть данные
Redis miss ─► PostgreSQL ─► записать в Redis с TTL ─► вернуть данные
```

`UpdateFlight`, `ReserveSeats` и `ReleaseReservation` удаляют ключ рейса и результаты поиска. Ошибка Redis не ломает основной запрос: сервис пишет предупреждение и читает PostgreSQL.

Увидеть `miss`, затем `hit`:

```bash
curl http://localhost:8080/flights/10000000-0000-0000-0000-000000000001 >/dev/null
curl http://localhost:8080/flights/10000000-0000-0000-0000-000000000001 >/dev/null
docker compose logs flight-service | grep 'cache '
```

Проверить TTL:

```bash
docker compose exec redis-master redis-cli -a redispass TTL \
  flight:10000000-0000-0000-0000-000000000001
```

## Retry и circuit breaker

Retry реализован gRPC client interceptor:

- не больше 3 попыток;
- задержки 100 ms, 200 ms;
- каждая попытка получает собственный deadline;
- повторяются только `UNAVAILABLE` и `DEADLINE_EXCEEDED`;
- бизнес-ошибки `INVALID_ARGUMENT`, `NOT_FOUND`, `RESOURCE_EXHAUSTED` не повторяются.

Circuit breaker также реализован interceptor. Он считает временные сбои в скользящем окне:

```text
CLOSED ── порог ошибок ──► OPEN
  ▲                         │
  └── успешная проба ◄── HALF_OPEN
```

В `OPEN` запрос сразу получает HTTP `503`, не ожидая сетевого таймаута. Порог, размер окна и время открытия задаются переменными окружения. Переходы состояний записываются в лог.

## Базы данных

Схема и объяснение 3NF находятся в [docs/ER_DIAGRAM.md](docs/ER_DIAGRAM.md).

Открыть Booking PostgreSQL:

```bash
docker compose exec booking-postgres psql -U booking -d booking
```

Полезные запросы:

```sql
\dt
\d bookings
SELECT id, user_id, flight_id, seat_count, total_price_cents, status FROM bookings;
```

Открыть Flight PostgreSQL:

```bash
docker compose exec flight-postgres psql -U flight -d flight
```

```sql
\dt
\d flights
\d seat_reservations
SELECT id, flight_number, origin, destination, available_seats, price_cents, status FROM flights;
SELECT booking_id, flight_id, seat_count, status FROM seat_reservations;
```

## Проверки

Unit-тесты, `go vet` и race detector:

```bash
make check
```

Конкурентное резервирование на отдельной тестовой Flight PostgreSQL:

```bash
TEST_FLIGHT_DATABASE_URL='postgres://flight:flight@localhost:5434/flight?sslmode=disable' \
  make integration
```

Тест создаёт временный рейс, одновременно запрашивает последние места, проверяет идемпотентный повтор и удаляет свои строки.

Полный сценарий через REST → gRPC → обе PostgreSQL:

```bash
make reset
make e2e
```

Сценарий проверяет поиск, чтение рейса, фиксацию цены, резервирование мест, недостаток мест, чтение бронирования, список пользователя, отмену, возврат мест и состояние резервации в БД.

## Основные настройки

| Переменная | Назначение | Значение в Compose |
|---|---|---|
| `GRPC_API_KEY` | межсервисный ключ | `local-flight-service-key` |
| `CACHE_TTL` | срок жизни кеша | `5m` |
| `GRPC_TIMEOUT` | deadline одной попытки | `2s` |
| `GRPC_MAX_ATTEMPTS` | общее число попыток | `3` |
| `GRPC_RETRY_BACKOFF` | начальная задержка retry | `100ms` |
| `BREAKER_FAILURE_THRESHOLD` | ошибок для открытия | `5` |
| `BREAKER_WINDOW_SIZE` | размер окна результатов | `10` |
| `BREAKER_OPEN_TIMEOUT` | время в OPEN | `10s` |

