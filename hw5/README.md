# Online Cinema Analytics

## 1. Назначение

Система принимает события онлайн-кинотеатра, доставляет их через Kafka, сохраняет в ClickHouse, рассчитывает метрики, записывает результаты в PostgreSQL и выгружает CSV в S3.

СОСТАВ: Go, Kafka 3.9.0, Schema Registry 8.1.0, ClickHouse, PostgreSQL, Grafana 12.4.16 и SeaweedFS 4.55.

```text
HTTP / generator
       │ JSON
       ▼
Movie Service ── Avro, key=user_id ──► Kafka (3 brokers, 3 partitions, RF=3, min ISR=2)
                                            │
                                            ▼
                                ClickHouse Kafka Engine
                                            │ Materialized View
                                            ▼
                                   raw_events → unique_events (дедупликация)
                                            │
                                            ▼
Aggregation Service ──► ClickHouse aggregates ──► PostgreSQL ──► S3
                              │
                              └───────────────────────────────► Grafana
```

Ключ Kafka — `user_id`. События одного пользователя поступают в одну партицию и сохраняют порядок публикации. События разных пользователей распределяются между тремя партициями.

## 2. Состав файлов

- `schema/movie_event.avsc` — Avro-контракт события версии 1.
- `internal/event` — модель события и проверка обязательных полей.
- `internal/producer` — регистрация схемы, Avro-кодирование, публикация и генератор реалистичных сессий.
- `cmd/movie-service` — сборка зависимостей и HTTP-сервер producer.
- `db/clickhouse` — Kafka Engine, raw MergeTree, Materialized View и таблицы агрегатов.
- `internal/aggregation` — версии агрегатов, транзакционная замена даты в PostgreSQL, блокировки пересчёта и CSV-экспорт с сохраняемой очередью.
- `cmd/aggregation-service` — HTTP API и запуск расчёта по расписанию.
- `db/postgres` — схема готовых метрик.
- `grafana` — автоматически подключаемый datasource и готовый dashboard.
- `scripts/e2e.sh` — проверка полного пути события.

## 3. Подготовка к запуску

ТРЕБУЕТСЯ: Docker, Docker Compose, `curl`, не менее 6 GiB памяти и 4 CPU для Docker VM.

Для Colima выполнить:

```bash
colima stop
colima start --memory 6 --cpu 4
```

КОНТРОЛЬ: команда `docker info` завершается без ошибки.

## 4. Запуск

Выполнить:

```bash
cd hw5
docker compose up --build -d --wait
docker compose ps -a
```

КОНТРОЛЬ:

- рабочие контейнеры имеют состояние `healthy`;
- `kafka-init`, `clickhouse-migrate` и `postgres-migrate` имеют состояние `Exited (0)`;
- состояние `Exited (0)` для служебных контейнеров является штатным.

Проверить HTTP-сервисы:

```bash
curl http://localhost:8080/health
curl http://localhost:8081/health
```

Ожидаемый результат: оба ответа содержат `"status":"ok"`.

ТОЧКИ ДОСТУПА:

- Grafana: <http://localhost:3000>, логин и пароль `admin` / `admin`;
- S3 API (SeaweedFS): <http://localhost:4566>, ключ `analytics`, секрет `analytics-local-secret`;
- Schema Registry: <http://localhost:8082>.

## 5. Остановка

Для остановки с сохранением данных выполнить:

```bash
docker compose down
```

ВНИМАНИЕ: следующая команда удаляет все данные проекта:

```bash
docker compose down -v --remove-orphans
docker compose up --build -d --wait
```

## 6. Отправка события

Выполнить:

```bash
curl -X POST http://localhost:8080/events \
  -H 'Content-Type: application/json' \
  -d '{
    "user_id": "user-101",
    "movie_id": "movie-7",
    "event_type": "VIEW_STARTED",
    "device_type": "TV",
    "session_id": "session-101",
    "progress_seconds": 0
  }'
```

КОНТРОЛЬ: сервер возвращает HTTP 201 и `event_id`.

Допустимые типы событий: `VIEW_STARTED`, `VIEW_FINISHED`, `VIEW_PAUSED`, `VIEW_RESUMED`, `LIKED`, `SEARCHED`.

Допустимые устройства: `MOBILE`, `DESKTOP`, `TV`, `TABLET`.

Генератор включён в Compose и раз в 10 секунд публикует корректную последовательность просмотра. Интервал и включение задаются `GENERATOR_INTERVAL` и `GENERATOR_ENABLED`.

## 7. Агрегация и экспорт

Сервис раз в минуту пересчитывает последние девять UTC-дней. Окно обновляет D1/D7 и включает пересчёт после закрытия полного дня D7. Интервал настраивается через `AGGREGATION_INTERVAL`.

Экспорт закрытых дат выполняется при старте, после планового пересчёта и по `EXPORT_INTERVAL` (24h по умолчанию в Compose). PostgreSQL хранит очередь всех ожидающих дат: пропущенные выгрузки и обновлённые исторические метрики автоматически повторяются, в том числе после перезапуска.

Для ручного расчёта выполнить:

```bash
curl -X POST 'http://localhost:8081/aggregate?date=2026-04-01'
```

КОНТРОЛЬ: в логах появляется `aggregation completed`.

Для ручного экспорта выполнить:

```bash
curl -X POST 'http://localhost:8081/export?date=2026-04-01'
```

Файл перезаписывается по одному адресу:

```text
s3://movie-analytics/daily/2026-04-01/aggregates.csv
```

Повторные доставки исключаются по `event_id` в `unique_events`. Каждый пересчёт получает возрастающий `run_version`; представления ClickHouse `*_current` читают только полностью опубликованную версию. PostgreSQL атомарно заменяет всю дату под advisory lock, поэтому исчезнувшие строки тоже удаляются. Повторный экспорт перезаписывает тот же S3-ключ.

РАСЧЁТНЫЕ ПОКАЗАТЕЛИ:

- DAU через `uniqExact(user_id)`;
- среднее время завершённого просмотра через `avgIf`;
- топ фильмов через `countIf` и `GROUP BY movie_id`;
- конверсия `VIEW_FINISHED / VIEW_STARTED`;
- retention D1/D7 и таблица когорт Day 0–Day 7; будущий день — NULL, а не 0.

Когорта определяется первым просмотром, возврат — просмотром в точную дату D1/D7. Поиск и лайк учитываются в DAU, но не в retention. Дневная конверсия может превышать 100% при просмотрах через полночь. Для безопасных HTTP-повторов клиент должен передавать неизменное событие с заранее созданным `event_id`.

## 8. Контроль данных

Для просмотра последних событий ClickHouse выполнить:

```bash
docker compose exec clickhouse clickhouse-client \
  --user analytics --password analytics --database movie_analytics \
  --query 'SELECT event_id,user_id,movie_id,event_type,timestamp,device_type,session_id,progress_seconds FROM raw_events ORDER BY timestamp DESC LIMIT 20'
```

Для просмотра агрегатов ClickHouse выполнить:

```bash
docker compose exec clickhouse clickhouse-client \
  --user analytics --password analytics --database movie_analytics \
  --query 'SELECT * FROM daily_metrics_current ORDER BY metric_date'

docker compose exec clickhouse clickhouse-client \
  --user analytics --password analytics --database movie_analytics \
  --query 'SELECT * FROM retention_cohorts_current ORDER BY cohort_date,day_number'
```

Для просмотра метрик PostgreSQL выполнить:

```bash
docker compose exec postgres psql -U analytics -d analytics -c \
  'SELECT metric_date,metric_name,dimension_value,metric_value,computed_at FROM daily_metric_values ORDER BY metric_date,metric_name,dimension_value;'
```

Для проверки Kafka topic выполнить:

```bash
docker compose exec kafka-1 /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server kafka-1:9092 --describe --topic movie-events
```

КОНТРОЛЬ: `PartitionCount: 3`, `ReplicationFactor: 3`, `min.insync.replicas=2`.

Для просмотра версий схемы выполнить:

```bash
curl http://localhost:8082/subjects/movie-events-value/versions
```

## 9. Grafana

Dashboard `Online Cinema Analytics` создаётся автоматически и читает только агрегатные таблицы ClickHouse. В нём есть:

- `Retention Cohort Heatmap` с Day 0–Day 7;
- DAU;
- конверсия просмотра;
- топ фильмов.

## 10. Проверки

Для проверки кода выполнить:

```bash
make check
```

КОНТРОЛЬ: команда завершается с кодом 0.

Для проверки полного маршрута данных выполнить:

```bash
make e2e
```

ПРОВЕРЯЕТСЯ: приём события, Avro, Kafka, ClickHouse, дедупликация, DLQ, метрики, PostgreSQL, S3, Schema Registry, репликация и запросы Grafana.

Для проверки ClickHouse и PostgreSQL выполнить:

```bash
make integration
```

Для проверки отказа активного Kafka-брокера выполнить:

```bash
make resilience
```

КОНТРОЛЬ: событие проходит при остановленном активном брокере; остановленный узел возвращается в состояние `healthy`. Постоянные volumes не удаляются.

