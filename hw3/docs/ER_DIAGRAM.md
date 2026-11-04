# Схема данных

Сервисы используют разные PostgreSQL. Между `bookings.flight_id` и `flights.id` нет внешнего ключа: таблицы находятся в разных базах, а существование рейса проверяется через gRPC до создания бронирования.

```mermaid
erDiagram
    FLIGHTS ||--o{ SEAT_RESERVATIONS : has

    FLIGHTS {
        uuid id PK
        varchar flight_number
        varchar airline
        char origin
        char destination
        timestamptz departure_time
        timestamptz arrival_time
        integer total_seats
        integer available_seats
        bigint price_cents
        flight_status status
        timestamptz created_at
        timestamptz updated_at
    }

    SEAT_RESERVATIONS {
        uuid id PK
        uuid flight_id FK
        uuid booking_id UK
        integer seat_count
        reservation_status status
        timestamptz created_at
        timestamptz updated_at
    }
```

```mermaid
erDiagram
    BOOKINGS {
        uuid id PK
        uuid user_id
        uuid flight_id
        varchar passenger_name
        varchar passenger_email
        integer seat_count
        bigint total_price_cents
        booking_status status
        timestamptz created_at
        timestamptz updated_at
    }
```

Схема находится в 3NF:

- каждая таблица описывает одну сущность;
- все неключевые поля зависят от первичного ключа целиком;
- резервация не дублирует сведения о рейсе;
- бронирование хранит только идентификатор рейса и снимок итоговой цены, необходимый по бизнес-правилу;
- `booking_id` в `seat_reservations` уникален, поэтому одному бронированию соответствует не более одной резервации.

Ограничения PostgreSQL не допускают отрицательных остатков, нулевой цены, нулевого количества мест, неверных IATA-кодов и прибытия раньше вылета. Пара номера рейса и UTC-даты вылета уникальна через expression index; отдельный дублирующий столбец даты не хранится.

