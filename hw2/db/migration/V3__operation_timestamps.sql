alter table user_operations
    alter column created_at set default clock_timestamp();

create index idx_order_items_product_id on order_items(product_id);
