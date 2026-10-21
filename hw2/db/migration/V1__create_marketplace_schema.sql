create extension if not exists pgcrypto;

create type user_role as enum ('USER', 'SELLER', 'ADMIN');
create type product_status as enum ('ACTIVE', 'INACTIVE', 'ARCHIVED');
create type order_status as enum (
    'CREATED',
    'PAYMENT_PENDING',
    'PAID',
    'SHIPPED',
    'COMPLETED',
    'CANCELED'
);
create type discount_type as enum ('PERCENTAGE', 'FIXED_AMOUNT');
create type operation_type as enum ('CREATE_ORDER', 'UPDATE_ORDER');

create table users (
    id uuid primary key default gen_random_uuid(),
    email varchar(320) not null unique,
    password_hash varchar(255) not null,
    role user_role not null,
    created_at timestamptz not null default now()
);

create table products (
    id uuid primary key default gen_random_uuid(),
    name varchar(255) not null check (char_length(name) between 1 and 255),
    description varchar(4000),
    price numeric(12, 2) not null check (price > 0),
    stock integer not null check (stock >= 0),
    category varchar(100) not null check (char_length(category) between 1 and 100),
    status product_status not null,
    seller_id uuid not null references users(id),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index idx_products_status on products(status);
create index idx_products_category on products(category);
create index idx_products_seller_id on products(seller_id);

create table promo_codes (
    id uuid primary key default gen_random_uuid(),
    code varchar(20) not null unique check (code ~ '^[A-Z0-9_]{4,20}$'),
    discount_type discount_type not null,
    discount_value numeric(12, 2) not null check (discount_value > 0),
    min_order_amount numeric(12, 2) not null check (min_order_amount >= 0),
    max_uses integer not null check (max_uses > 0),
    current_uses integer not null default 0
        check (current_uses >= 0 and current_uses <= max_uses),
    valid_from timestamptz not null,
    valid_until timestamptz not null,
    active boolean not null default true,
    check (valid_until > valid_from),
    check (discount_type <> 'PERCENTAGE' or discount_value <= 100)
);

create table orders (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references users(id),
    status order_status not null,
    promo_code_id uuid references promo_codes(id),
    total_amount numeric(12, 2) not null check (total_amount >= 0),
    discount_amount numeric(12, 2) not null default 0 check (discount_amount >= 0),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index idx_orders_one_active_per_user
    on orders(user_id)
    where status in ('CREATED', 'PAYMENT_PENDING');
create index idx_orders_user_id on orders(user_id);

create table order_items (
    id uuid primary key default gen_random_uuid(),
    order_id uuid not null references orders(id) on delete cascade,
    product_id uuid not null references products(id),
    quantity integer not null check (quantity between 1 and 999),
    price_at_order numeric(12, 2) not null check (price_at_order > 0),
    unique (order_id, product_id)
);

create table user_operations (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references users(id),
    operation_type operation_type not null,
    created_at timestamptz not null default now()
);

create index idx_user_operations_lookup
    on user_operations(user_id, operation_type, created_at desc);

