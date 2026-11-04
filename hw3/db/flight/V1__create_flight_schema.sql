create extension if not exists pgcrypto;

create type flight_status as enum ('SCHEDULED', 'DEPARTED', 'CANCELLED', 'COMPLETED');
create type reservation_status as enum ('ACTIVE', 'RELEASED', 'EXPIRED');

create table flights (
    id uuid primary key default gen_random_uuid(),
    flight_number varchar(10) not null check (flight_number ~ '^[A-Z0-9]{2,10}$'),
    airline varchar(120) not null check (char_length(airline) between 1 and 120),
    origin char(3) not null check (origin ~ '^[A-Z]{3}$'),
    destination char(3) not null check (destination ~ '^[A-Z]{3}$' and destination <> origin),
    departure_time timestamptz not null,
    arrival_time timestamptz not null check (arrival_time > departure_time),
    total_seats integer not null check (total_seats > 0),
    available_seats integer not null check (available_seats between 0 and total_seats),
    price_cents bigint not null check (price_cents > 0),
    status flight_status not null default 'SCHEDULED',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index uq_flights_number_departure_date
    on flights(flight_number, ((departure_time at time zone 'UTC')::date));
create index idx_flights_search
    on flights(origin, destination, ((departure_time at time zone 'UTC')::date), status);

create table seat_reservations (
    id uuid primary key default gen_random_uuid(),
    flight_id uuid not null references flights(id),
    booking_id uuid not null unique,
    seat_count integer not null check (seat_count > 0),
    status reservation_status not null default 'ACTIVE',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_seat_reservations_flight_id on seat_reservations(flight_id);

