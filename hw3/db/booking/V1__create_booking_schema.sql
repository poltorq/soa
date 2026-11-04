create extension if not exists pgcrypto;

create type booking_status as enum ('CONFIRMED', 'CANCELLED');

create table bookings (
    id uuid primary key,
    user_id uuid not null,
    flight_id uuid not null,
    passenger_name varchar(200) not null check (char_length(passenger_name) between 1 and 200),
    passenger_email varchar(320) not null,
    seat_count integer not null check (seat_count > 0),
    total_price_cents bigint not null check (total_price_cents > 0),
    status booking_status not null default 'CONFIRMED',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_bookings_user_id_created_at on bookings(user_id, created_at desc);

