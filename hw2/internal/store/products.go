package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"marketplace-hw2/internal/fault"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateProduct(ctx context.Context, sellerID uuid.UUID, input ProductInput) (Product, error) {
	now := time.Now().UTC()
	row := s.pool.QueryRow(ctx, `
		INSERT INTO products (name, description, price, stock, category, status, seller_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		RETURNING id, name, description, price::text, stock, category, status, seller_id, created_at, updated_at
	`, input.Name, input.Description, decimalString(input.PriceCents), input.Stock, input.Category, input.Status, sellerID, now)
	product, err := scanProduct(row)
	if err != nil {
		return Product{}, fmt.Errorf("create product: %w", err)
	}
	return product, nil
}

func (s *Store) Product(ctx context.Context, id uuid.UUID) (Product, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, description, price::text, stock, category, status, seller_id, created_at, updated_at
		FROM products
		WHERE id = $1
	`, id)
	product, err := scanProduct(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, fault.New("PRODUCT_NOT_FOUND", "product not found")
	}
	if err != nil {
		return Product{}, fmt.Errorf("get product: %w", err)
	}
	return product, nil
}

func (s *Store) Products(ctx context.Context, filter ProductFilter) ([]Product, int64, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin product list read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if filter.Status != nil {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.Category != nil {
		args = append(args, *filter.Category)
		conditions = append(conditions, fmt.Sprintf("category = $%d", len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM products"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	args = append(args, filter.Size, int64(filter.Page)*int64(filter.Size))
	query := `SELECT id, name, description, price::text, stock, category, status, seller_id, created_at, updated_at
		FROM products` + where + fmt.Sprintf(" ORDER BY created_at, id LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	products := make([]Product, 0, filter.Size)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan product: %w", err)
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate products: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit product list read: %w", err)
	}
	return products, total, nil
}

func (s *Store) UpdateProduct(ctx context.Context, id, actorID uuid.UUID, role string, input ProductInput) (Product, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Product{}, fmt.Errorf("begin product update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existing, err := scanProduct(tx.QueryRow(ctx, `
		SELECT id, name, description, price::text, stock, category, status, seller_id, created_at, updated_at
		FROM products WHERE id = $1 FOR UPDATE
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, fault.New("PRODUCT_NOT_FOUND", "product not found")
	}
	if err != nil {
		return Product{}, err
	}
	if role == "SELLER" && existing.SellerID != actorID {
		return Product{}, fault.New("ACCESS_DENIED", "seller may update only their products")
	}
	// Leave room for stock returned by cancellations.
	var reserved int64
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(sum(i.quantity), 0) FROM order_items i
		JOIN orders o ON o.id = i.order_id
		WHERE i.product_id = $1 AND o.status IN ('CREATED', 'PAYMENT_PENDING')
	`, id).Scan(&reserved); err != nil {
		return Product{}, fmt.Errorf("read product reservations: %w", err)
	}
	if int64(input.Stock)+reserved > 2147483647 {
		return Product{}, fault.WithDetails("VALIDATION_ERROR", "stock leaves no capacity to return reserved items", map[string]any{
			"violations": []map[string]string{{"field": "stock", "message": "stock plus cancellable reservations must not exceed 2147483647"}},
		})
	}
	row := tx.QueryRow(ctx, `
		UPDATE products
		SET name = $2, description = $3, price = $4, stock = $5, category = $6, status = $7, updated_at = $8
		WHERE id = $1
		RETURNING id, name, description, price::text, stock, category, status, seller_id, created_at, updated_at
	`, id, input.Name, input.Description, decimalString(input.PriceCents), input.Stock, input.Category, input.Status, time.Now().UTC())
	product, err := scanProduct(row)
	if err != nil {
		return Product{}, fmt.Errorf("update product: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Product{}, fmt.Errorf("commit product update: %w", err)
	}
	return product, nil
}

func (s *Store) ArchiveProduct(ctx context.Context, id, actorID uuid.UUID, role string) error {
	existing, err := s.Product(ctx, id)
	if err != nil {
		return err
	}
	if role == "SELLER" && existing.SellerID != actorID {
		return fault.New("ACCESS_DENIED", "seller may archive only their products")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE products SET status = 'ARCHIVED', updated_at = $2 WHERE id = $1`, id, time.Now().UTC()); err != nil {
		return fmt.Errorf("archive product: %w", err)
	}
	return nil
}

type productScanner interface {
	Scan(dest ...any) error
}

func scanProduct(scanner productScanner) (Product, error) {
	var product Product
	var price string
	if err := scanner.Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&price,
		&product.Stock,
		&product.Category,
		&product.Status,
		&product.SellerID,
		&product.CreatedAt,
		&product.UpdatedAt,
	); err != nil {
		return Product{}, err
	}
	priceCents, err := parseDecimal(price)
	if err != nil {
		return Product{}, err
	}
	product.PriceCents = priceCents
	return product, nil
}
