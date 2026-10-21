package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"marketplace-hw2/internal/fault"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	operationCreate = "CREATE_ORDER"
	operationUpdate = "UPDATE_ORDER"
)

func (s *Store) CreateOrder(
	ctx context.Context,
	userID uuid.UUID,
	items []OrderItemInput,
	promoCode *string,
	rateLimit time.Duration,
) (Order, error) {
	if err := validateUniqueItems(items); err != nil {
		return Order{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Order{}, fmt.Errorf("begin create order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockUser(ctx, tx, userID); err != nil {
		return Order{}, err
	}
	if err := checkRateLimit(ctx, tx, userID, operationCreate, rateLimit); err != nil {
		return Order{}, err
	}

	var hasActive bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM orders
			WHERE user_id = $1 AND status IN ('CREATED', 'PAYMENT_PENDING')
		)
	`, userID).Scan(&hasActive); err != nil {
		return Order{}, fmt.Errorf("check active order: %w", err)
	}
	if hasActive {
		return Order{}, fault.New("ORDER_HAS_ACTIVE", "user already has an active order")
	}

	products, err := lockProducts(ctx, tx, productIDs(items))
	if err != nil {
		return Order{}, err
	}
	if err := validateProducts(items, products, nil); err != nil {
		return Order{}, err
	}

	subtotal, err := orderSubtotal(items, products)
	if err != nil {
		return Order{}, err
	}
	discount := int64(0)
	var promo *PromoCode
	if promoCode != nil {
		promo, err = lockPromoByCode(ctx, tx, *promoCode)
		if err != nil {
			return Order{}, err
		}
		if err := validateNewPromo(*promo, subtotal, time.Now()); err != nil {
			return Order{}, err
		}
		discount = promoDiscount(*promo, subtotal)
	}
	if err := validateOrderAmounts(subtotal, discount); err != nil {
		return Order{}, err
	}
	if promo != nil {
		if _, err := tx.Exec(ctx, `UPDATE promo_codes SET current_uses = current_uses + 1 WHERE id = $1`, promo.ID); err != nil {
			return Order{}, fmt.Errorf("increment promo code usage: %w", err)
		}
	}

	order := Order{
		ID:                  uuid.New(),
		UserID:              userID,
		Status:              "CREATED",
		TotalAmountCents:    subtotal - discount,
		DiscountAmountCents: discount,
		Items:               make([]OrderItem, 0, len(items)),
	}
	now := time.Now().UTC()
	if promo != nil {
		order.PromoCodeID = &promo.ID
		order.PromoCode = &promo.Code
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO orders (id, user_id, status, promo_code_id, total_amount, discount_amount, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING created_at, updated_at
	`, order.ID, order.UserID, order.Status, order.PromoCodeID,
		decimalString(order.TotalAmountCents), decimalString(order.DiscountAmountCents), now,
	).Scan(&order.CreatedAt, &order.UpdatedAt); err != nil {
		return Order{}, fmt.Errorf("insert order: %w", err)
	}

	for _, item := range items {
		product := products[item.ProductID]
		if _, err := tx.Exec(ctx, `UPDATE products SET stock = stock - $2, updated_at = $3 WHERE id = $1`, item.ProductID, item.Quantity, now); err != nil {
			return Order{}, fmt.Errorf("reserve product %s: %w", item.ProductID, err)
		}
		orderItem := OrderItem{
			ID:                uuid.New(),
			ProductID:         item.ProductID,
			Quantity:          item.Quantity,
			PriceAtOrderCents: product.PriceCents,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_items (id, order_id, product_id, quantity, price_at_order)
			VALUES ($1, $2, $3, $4, $5)
		`, orderItem.ID, order.ID, orderItem.ProductID, orderItem.Quantity,
			decimalString(orderItem.PriceAtOrderCents)); err != nil {
			return Order{}, fmt.Errorf("insert order item: %w", err)
		}
		order.Items = append(order.Items, orderItem)
	}
	if err := recordOperation(ctx, tx, userID, operationCreate); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit create order: %w", err)
	}
	return order, nil
}

func (s *Store) Order(ctx context.Context, id, actorID uuid.UUID, role string) (Order, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Order{}, fmt.Errorf("begin order read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	order, err := loadOrder(ctx, tx, id)
	if err != nil {
		return Order{}, err
	}
	if role != "ADMIN" && order.UserID != actorID {
		return Order{}, fault.New("ORDER_OWNERSHIP_VIOLATION", "order belongs to another user")
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit order read: %w", err)
	}
	return order, nil
}

func (s *Store) UpdateOrder(
	ctx context.Context,
	id, actorID uuid.UUID,
	role string,
	items []OrderItemInput,
	rateLimit time.Duration,
) (Order, error) {
	if err := validateUniqueItems(items); err != nil {
		return Order{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Order{}, fmt.Errorf("begin update order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := loadOrderForUpdate(ctx, tx, id)
	if err != nil {
		return Order{}, err
	}
	if role != "ADMIN" && order.UserID != actorID {
		return Order{}, fault.New("ORDER_OWNERSHIP_VIOLATION", "order belongs to another user")
	}
	if order.Status != "CREATED" {
		return Order{}, fault.New("INVALID_STATE_TRANSITION", "only a CREATED order can be updated")
	}
	if err := lockUser(ctx, tx, actorID); err != nil {
		return Order{}, err
	}
	if err := checkRateLimit(ctx, tx, actorID, operationUpdate, rateLimit); err != nil {
		return Order{}, err
	}

	oldQuantities := make(map[uuid.UUID]int, len(order.Items))
	allIDs := make([]uuid.UUID, 0, len(order.Items)+len(items))
	for _, item := range order.Items {
		oldQuantities[item.ProductID] += item.Quantity
		allIDs = append(allIDs, item.ProductID)
	}
	allIDs = append(allIDs, productIDs(items)...)
	products, err := lockProducts(ctx, tx, allIDs)
	if err != nil {
		return Order{}, err
	}
	if err := validateProducts(items, products, oldQuantities); err != nil {
		return Order{}, err
	}

	preserveOrderPrices(products, order.Items)
	subtotal, err := orderSubtotal(items, products)
	if err != nil {
		return Order{}, err
	}
	discount := int64(0)
	if order.PromoCodeID != nil {
		promo, err := lockPromoByID(ctx, tx, *order.PromoCodeID)
		if err != nil {
			return Order{}, err
		}
		if !promo.Active || time.Now().Before(promo.ValidFrom) || time.Now().After(promo.ValidUntil) {
			return Order{}, fault.New("PROMO_CODE_INVALID", "promo code is inactive or outside its validity period")
		}
		if subtotal < promo.MinOrderAmountCents {
			if _, err := tx.Exec(ctx, `UPDATE promo_codes SET current_uses = current_uses - 1 WHERE id = $1`, promo.ID); err != nil {
				return Order{}, fmt.Errorf("release promo code usage: %w", err)
			}
			order.PromoCodeID = nil
			order.PromoCode = nil
		} else {
			discount = promoDiscount(promo, subtotal)
		}
	}
	if err := validateOrderAmounts(subtotal, discount); err != nil {
		return Order{}, err
	}

	newQuantities := make(map[uuid.UUID]int, len(items))
	for _, item := range items {
		newQuantities[item.ProductID] = item.Quantity
	}
	now := time.Now().UTC()
	for productID, product := range products {
		newStock := product.Stock + oldQuantities[productID] - newQuantities[productID]
		if _, err := tx.Exec(ctx, `UPDATE products SET stock = $2, updated_at = $3 WHERE id = $1`, productID, newStock, now); err != nil {
			return Order{}, fmt.Errorf("update product stock %s: %w", productID, err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM order_items WHERE order_id = $1`, order.ID); err != nil {
		return Order{}, fmt.Errorf("delete old order items: %w", err)
	}
	order.Items = make([]OrderItem, 0, len(items))
	for _, item := range items {
		orderItem := OrderItem{
			ID:                uuid.New(),
			ProductID:         item.ProductID,
			Quantity:          item.Quantity,
			PriceAtOrderCents: products[item.ProductID].PriceCents,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_items (id, order_id, product_id, quantity, price_at_order)
			VALUES ($1, $2, $3, $4, $5)
		`, orderItem.ID, order.ID, orderItem.ProductID, orderItem.Quantity,
			decimalString(orderItem.PriceAtOrderCents)); err != nil {
			return Order{}, fmt.Errorf("insert updated order item: %w", err)
		}
		order.Items = append(order.Items, orderItem)
	}
	order.DiscountAmountCents = discount
	order.TotalAmountCents = subtotal - discount
	if err := tx.QueryRow(ctx, `
		UPDATE orders
		SET promo_code_id = $2, total_amount = $3, discount_amount = $4, updated_at = $5
		WHERE id = $1
		RETURNING updated_at
	`, order.ID, order.PromoCodeID, decimalString(order.TotalAmountCents),
		decimalString(order.DiscountAmountCents), now).Scan(&order.UpdatedAt); err != nil {
		return Order{}, fmt.Errorf("update order: %w", err)
	}
	if err := recordOperation(ctx, tx, actorID, operationUpdate); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit update order: %w", err)
	}
	return order, nil
}

func (s *Store) CancelOrder(ctx context.Context, id, actorID uuid.UUID, role string) (Order, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Order{}, fmt.Errorf("begin cancel order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := loadOrderForUpdate(ctx, tx, id)
	if err != nil {
		return Order{}, err
	}
	if role != "ADMIN" && order.UserID != actorID {
		return Order{}, fault.New("ORDER_OWNERSHIP_VIOLATION", "order belongs to another user")
	}
	if order.Status != "CREATED" && order.Status != "PAYMENT_PENDING" {
		return Order{}, fault.New("INVALID_STATE_TRANSITION", "order cannot be canceled from its current state")
	}
	if err := lockUser(ctx, tx, order.UserID); err != nil {
		return Order{}, err
	}
	if _, err := lockProducts(ctx, tx, orderItemProductIDs(order.Items)); err != nil {
		return Order{}, err
	}
	now := time.Now().UTC()
	for _, item := range order.Items {
		if _, err := tx.Exec(ctx, `UPDATE products SET stock = stock + $2, updated_at = $3 WHERE id = $1`, item.ProductID, item.Quantity, now); err != nil {
			return Order{}, fmt.Errorf("restore product %s: %w", item.ProductID, err)
		}
	}
	if order.PromoCodeID != nil {
		if _, err := lockPromoByID(ctx, tx, *order.PromoCodeID); err != nil {
			return Order{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE promo_codes SET current_uses = greatest(current_uses - 1, 0) WHERE id = $1
		`, *order.PromoCodeID); err != nil {
			return Order{}, fmt.Errorf("release promo code usage: %w", err)
		}
	}
	if err := tx.QueryRow(ctx, `
		UPDATE orders SET status = 'CANCELED', updated_at = $2 WHERE id = $1 RETURNING updated_at
	`, order.ID, now).Scan(&order.UpdatedAt); err != nil {
		return Order{}, fmt.Errorf("cancel order: %w", err)
	}
	order.Status = "CANCELED"
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit cancel order: %w", err)
	}
	return order, nil
}

func (s *Store) TransitionOrder(ctx context.Context, id uuid.UUID, nextStatus string) (Order, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Order{}, fmt.Errorf("begin transition order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	order, err := loadOrderForUpdate(ctx, tx, id)
	if err != nil {
		return Order{}, err
	}
	if !validTransition(order.Status, nextStatus) {
		return Order{}, fault.WithDetails(
			"INVALID_STATE_TRANSITION",
			"order state transition is not allowed",
			map[string]any{"current_status": order.Status, "requested_status": nextStatus},
		)
	}
	if err := tx.QueryRow(ctx, `
		UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1 RETURNING updated_at
	`, id, nextStatus, time.Now().UTC()).Scan(&order.UpdatedAt); err != nil {
		return Order{}, fmt.Errorf("transition order: %w", err)
	}
	order.Status = nextStatus
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit transition order: %w", err)
	}
	return order, nil
}

func lockUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, userID.String()); err != nil {
		return fmt.Errorf("lock user operations: %w", err)
	}
	return nil
}

func checkRateLimit(ctx context.Context, tx pgx.Tx, userID uuid.UUID, operation string, limit time.Duration) error {
	if limit <= 0 {
		return nil
	}
	var last, databaseNow time.Time
	err := tx.QueryRow(ctx, `
		SELECT created_at, clock_timestamp()
		FROM user_operations
		WHERE user_id = $1 AND operation_type = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, operation).Scan(&last, &databaseNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read last user operation: %w", err)
	}
	remaining := limit - databaseNow.Sub(last)
	if remaining > 0 {
		return fault.WithDetails("ORDER_LIMIT_EXCEEDED", "order operation rate limit exceeded", map[string]any{
			"retry_after_seconds": int(math.Ceil(remaining.Seconds())),
		})
	}
	return nil
}

func recordOperation(ctx context.Context, tx pgx.Tx, userID uuid.UUID, operation string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_operations (user_id, operation_type, created_at) VALUES ($1, $2, clock_timestamp())
	`, userID, operation); err != nil {
		return fmt.Errorf("record user operation: %w", err)
	}
	return nil
}

func productIDs(items []OrderItemInput) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductID)
	}
	return ids
}

func orderItemProductIDs(items []OrderItem) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductID)
	}
	return ids
}

func lockProducts(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Product, error) {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		unique[id] = struct{}{}
	}
	ordered := make([]uuid.UUID, 0, len(unique))
	for id := range unique {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	rows, err := tx.Query(ctx, `
		SELECT id, name, description, price::text, stock, category, status, seller_id, created_at, updated_at
		FROM products
		WHERE id = ANY($1::uuid[])
		ORDER BY id
		FOR UPDATE
	`, ordered)
	if err != nil {
		return nil, fmt.Errorf("lock products: %w", err)
	}
	defer rows.Close()
	products := make(map[uuid.UUID]Product, len(ordered))
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("scan locked product: %w", err)
		}
		products[product.ID] = product
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked products: %w", err)
	}
	return products, nil
}

func lockPromoByCode(ctx context.Context, tx pgx.Tx, code string) (*PromoCode, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, code, discount_type, discount_value::text, min_order_amount::text,
			max_uses, current_uses, valid_from, valid_until, active
		FROM promo_codes WHERE code = $1 FOR UPDATE
	`, code)
	promo, err := scanPromo(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fault.New("PROMO_CODE_INVALID", "promo code was not found")
	}
	if err != nil {
		return nil, fmt.Errorf("lock promo code: %w", err)
	}
	return &promo, nil
}

func lockPromoByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (PromoCode, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, code, discount_type, discount_value::text, min_order_amount::text,
			max_uses, current_uses, valid_from, valid_until, active
		FROM promo_codes WHERE id = $1 FOR UPDATE
	`, id)
	promo, err := scanPromo(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return PromoCode{}, fault.New("PROMO_CODE_INVALID", "promo code was not found")
	}
	if err != nil {
		return PromoCode{}, fmt.Errorf("lock promo code: %w", err)
	}
	return promo, nil
}

type orderQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadOrder(ctx context.Context, db orderQuerier, id uuid.UUID) (Order, error) {
	return loadOrderQuery(ctx, db, id, false)
}

func loadOrderForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Order, error) {
	return loadOrderQuery(ctx, tx, id, true)
}

func loadOrderQuery(ctx context.Context, db orderQuerier, id uuid.UUID, forUpdate bool) (Order, error) {
	query := `
		SELECT o.id, o.user_id, o.status, o.promo_code_id, p.code,
			o.total_amount::text, o.discount_amount::text, o.created_at, o.updated_at
		FROM orders o
		LEFT JOIN promo_codes p ON p.id = o.promo_code_id
		WHERE o.id = $1`
	if forUpdate {
		query += " FOR UPDATE OF o"
	}
	var order Order
	var total string
	var discount string
	err := db.QueryRow(ctx, query, id).Scan(
		&order.ID,
		&order.UserID,
		&order.Status,
		&order.PromoCodeID,
		&order.PromoCode,
		&total,
		&discount,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fault.New("ORDER_NOT_FOUND", "order not found")
	}
	if err != nil {
		return Order{}, fmt.Errorf("get order: %w", err)
	}
	order.TotalAmountCents, err = parseDecimal(total)
	if err != nil {
		return Order{}, err
	}
	order.DiscountAmountCents, err = parseDecimal(discount)
	if err != nil {
		return Order{}, err
	}
	rows, err := db.Query(ctx, `
		SELECT id, product_id, quantity, price_at_order::text
		FROM order_items
		WHERE order_id = $1
		ORDER BY id
	`, id)
	if err != nil {
		return Order{}, fmt.Errorf("list order items: %w", err)
	}
	defer rows.Close()
	order.Items = make([]OrderItem, 0)
	for rows.Next() {
		var item OrderItem
		var price string
		if err := rows.Scan(&item.ID, &item.ProductID, &item.Quantity, &price); err != nil {
			return Order{}, fmt.Errorf("scan order item: %w", err)
		}
		item.PriceAtOrderCents, err = parseDecimal(price)
		if err != nil {
			return Order{}, err
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Order{}, fmt.Errorf("iterate order items: %w", err)
	}
	return order, nil
}
