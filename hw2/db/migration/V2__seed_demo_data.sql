insert into users (id, email, password_hash, role)
values
    ('00000000-0000-0000-0000-000000000001', 'admin@example.com',
        crypt('admin12345', gen_salt('bf')), 'ADMIN'),
    ('00000000-0000-0000-0000-000000000002', 'seller@example.com',
        crypt('seller12345', gen_salt('bf')), 'SELLER'),
    ('00000000-0000-0000-0000-000000000003', 'user@example.com',
        crypt('user12345', gen_salt('bf')), 'USER');

insert into products (
    id, name, description, price, stock, category, status, seller_id
)
values
    ('10000000-0000-0000-0000-000000000001', 'Laptop Pro', 'Demo active product',
        120000.00, 10, 'electronics', 'ACTIVE',
        '00000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000002', 'Wireless Headphones', 'Demo active product',
        5000.00, 20, 'electronics', 'ACTIVE',
        '00000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000003', 'Archived Keyboard', null,
        3000.00, 5, 'electronics', 'INACTIVE',
        '00000000-0000-0000-0000-000000000002');

insert into promo_codes (
    id, code, discount_type, discount_value, min_order_amount, max_uses,
    valid_from, valid_until, active
)
values
    ('20000000-0000-0000-0000-000000000001', 'WELCOME10', 'PERCENTAGE',
        10.00, 1000.00, 100,
        '2020-01-01T00:00:00Z', '2099-01-01T00:00:00Z', true),
    ('20000000-0000-0000-0000-000000000002', 'FIXED500', 'FIXED_AMOUNT',
        500.00, 2000.00, 100,
        '2020-01-01T00:00:00Z', '2099-01-01T00:00:00Z', true);

