package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"marketplace-hw2/internal/fault"

	"github.com/google/uuid"
)

type integrationFixture struct {
	t        *testing.T
	ctx      context.Context
	store    *Store
	seller   uuid.UUID
	buyer    uuid.UUID
	users    []uuid.UUID
	products []uuid.UUID
	promos   []uuid.UUID
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to a dedicated database migrated by Flyway")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	f := &integrationFixture{t: t, ctx: ctx, store: s}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		// Leave seed data and records from other tests alone.
		for _, statement := range []struct {
			query string
			ids   []uuid.UUID
		}{
			{`DELETE FROM user_operations WHERE user_id = ANY($1::uuid[])`, f.users},
			{`DELETE FROM order_items WHERE order_id IN (SELECT id FROM orders WHERE user_id = ANY($1::uuid[]))`, f.users},
			{`DELETE FROM orders WHERE user_id = ANY($1::uuid[])`, f.users},
			{`DELETE FROM products WHERE id = ANY($1::uuid[])`, f.products},
			{`DELETE FROM promo_codes WHERE id = ANY($1::uuid[])`, f.promos},
			{`DELETE FROM users WHERE id = ANY($1::uuid[])`, f.users},
		} {
			if _, err := tx.Exec(ctx, statement.query, statement.ids); err != nil {
				t.Error(err)
				return
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Error(err)
		}
	})
	f.seller = f.user("SELLER")
	f.buyer = f.user("USER")
	return f
}

func (f *integrationFixture) user(role string) uuid.UUID {
	f.t.Helper()
	user, err := f.store.CreateUser(f.ctx, uuid.NewString()+"@integration.test", "unused-test-hash", role)
	if err != nil {
		f.t.Fatal(err)
	}
	f.users = append(f.users, user.ID)
	return user.ID
}

func (f *integrationFixture) product(price int64, stock int) Product {
	f.t.Helper()
	product, err := f.store.CreateProduct(f.ctx, f.seller, ProductInput{
		Name: "Integration", PriceCents: price, Stock: stock, Category: uuid.NewString(), Status: "ACTIVE",
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.products = append(f.products, product.ID)
	return product
}

func (f *integrationFixture) promo(percentage float64, uses int) PromoCode {
	f.t.Helper()
	code := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:16]
	promo, err := f.store.CreatePromoCode(f.ctx, PromoCodeInput{
		Code:          "IT_" + code,
		DiscountType:  "PERCENTAGE",
		DiscountValue: percentage,
		MaxUses:       uses,
		ValidFrom:     time.Now().Add(-time.Hour),
		ValidUntil:    time.Now().Add(time.Hour),
		Active:        true,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.promos = append(f.promos, promo.ID)
	return promo
}

func (f *integrationFixture) assertStock(id uuid.UUID, stock int) {
	f.t.Helper()
	product, err := f.store.Product(f.ctx, id)
	if err != nil || product.Stock != stock {
		f.t.Fatalf("stock = %d (%v), want %d", product.Stock, err, stock)
	}
}

func faultCode(err error) string {
	if err == nil {
		return ""
	}
	var business *fault.Error
	if errors.As(err, &business) {
		return business.Code
	}
	return err.Error()
}

func TestIntegrationRollbackAndCancel(t *testing.T) {
	f := newIntegrationFixture(t)
	product := f.product(10000, 3)
	items := []OrderItemInput{{ProductID: product.ID, Quantity: 2}}
	invalid := "MISSING_" + uuid.NewString()
	if _, err := f.store.CreateOrder(f.ctx, f.buyer, items, &invalid, time.Minute); faultCode(err) != "PROMO_CODE_INVALID" {
		t.Fatalf("invalid promo: %v", err)
	}
	f.assertStock(product.ID, 3)
	promo := f.promo(10, 1)
	order, err := f.store.CreateOrder(f.ctx, f.buyer, items, &promo.Code, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, order.ID, f.buyer, "USER", []OrderItemInput{{ProductID: product.ID, Quantity: 4}}, time.Minute); faultCode(err) != "INSUFFICIENT_STOCK" {
		t.Fatalf("invalid update: %v", err)
	}
	f.assertStock(product.ID, 1)
	stored, err := f.store.Order(f.ctx, order.ID, f.buyer, "USER")
	if err != nil || len(stored.Items) != 1 || stored.Items[0].Quantity != 2 || stored.TotalAmountCents != 18000 || !stored.UpdatedAt.Equal(order.UpdatedAt) {
		t.Fatalf("order changed on rollback: %+v (%v)", stored, err)
	}
	var operations int
	if err := f.store.pool.QueryRow(f.ctx, `SELECT count(*) FROM user_operations WHERE user_id = $1`, f.buyer).Scan(&operations); err != nil || operations != 1 {
		t.Fatalf("failed requests recorded operations: %d (%v)", operations, err)
	}
	if _, err := f.store.CancelOrder(f.ctx, order.ID, f.buyer, "USER"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CancelOrder(f.ctx, order.ID, f.buyer, "USER"); faultCode(err) != "INVALID_STATE_TRANSITION" {
		t.Fatalf("repeated cancel: %v", err)
	}
	f.assertStock(product.ID, 3)
	var uses int
	if err := f.store.pool.QueryRow(f.ctx, `SELECT current_uses FROM promo_codes WHERE id = $1`, promo.ID).Scan(&uses); err != nil || uses != 0 {
		t.Fatalf("cancel did not release promo: %d (%v)", uses, err)
	}
}

func TestIntegrationConcurrentReservations(t *testing.T) {
	for _, limitedPromo := range []bool{false, true} {
		t.Run(fmt.Sprintf("promo=%v", limitedPromo), func(t *testing.T) {
			f := newIntegrationFixture(t)
			stock, successful, rejected := 3, 3, "INSUFFICIENT_STOCK"
			var code *string
			var promo PromoCode
			if limitedPromo {
				stock, successful, rejected = 8, 2, "PROMO_CODE_INVALID"
				promo = f.promo(10, 2)
				code = &promo.Code
			}
			product := f.product(10000, stock)
			users := make([]uuid.UUID, 8)
			for i := range users {
				users[i] = f.user("USER")
			}
			type result struct {
				order Order
				err   error
			}
			results := make(chan result, len(users))
			var wg sync.WaitGroup
			for _, user := range users {
				wg.Go(func() {
					order, err := f.store.CreateOrder(f.ctx, user, []OrderItemInput{{ProductID: product.ID, Quantity: 1}}, code, time.Minute)
					results <- result{order, err}
				})
			}
			wg.Wait()
			close(results)
			orders := make([]Order, 0)
			for result := range results {
				if result.err == nil {
					orders = append(orders, result.order)
				} else if faultCode(result.err) != rejected {
					t.Errorf("unexpected concurrent error: %v", result.err)
				}
			}
			if len(orders) != successful {
				t.Fatalf("successful orders = %d, want %d", len(orders), successful)
			}
			f.assertStock(product.ID, stock-successful)
			if limitedPromo {
				var uses int
				if err := f.store.pool.QueryRow(f.ctx, `SELECT current_uses FROM promo_codes WHERE id = $1`, promo.ID).Scan(&uses); err != nil || uses != successful {
					t.Fatalf("promo uses = %d (%v)", uses, err)
				}
			}
			for _, order := range orders {
				if _, err := f.store.CancelOrder(f.ctx, order.ID, order.UserID, "USER"); err != nil {
					t.Fatal(err)
				}
			}
			f.assertStock(product.ID, stock)
		})
	}
}

func TestIntegrationDiscountedAmountsAndReservedStock(t *testing.T) {
	f := newIntegrationFixture(t)
	product := f.product(600_000_000_000, 2)
	promo := f.promo(50, 1)
	items := []OrderItemInput{{ProductID: product.ID, Quantity: 2}}
	order, err := f.store.CreateOrder(f.ctx, f.buyer, items, &promo.Code, time.Minute)
	if err != nil || order.TotalAmountCents != 600_000_000_000 || order.DiscountAmountCents != 600_000_000_000 {
		t.Fatalf("discounted amounts should fit: %+v (%v)", order, err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, order.ID, f.buyer, "USER", items, time.Minute); err != nil {
		t.Fatalf("existing promo reservation must not count as another use: %v", err)
	}
	input := ProductInput{Name: product.Name, PriceCents: product.PriceCents, Category: product.Category, Status: "ACTIVE", Stock: 2147483647}
	if _, err := f.store.UpdateProduct(f.ctx, product.ID, f.seller, "SELLER", input); faultCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("must preserve capacity for reserved stock: %v", err)
	}
	f.assertStock(product.ID, 0)
	input.Stock -= 2
	if _, err := f.store.UpdateProduct(f.ctx, product.ID, f.seller, "SELLER", input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CancelOrder(f.ctx, order.ID, f.buyer, "USER"); err != nil {
		t.Fatalf("cancellation at stock boundary failed: %v", err)
	}
	f.assertStock(product.ID, 2147483647)
}

func TestIntegrationAdminOperationBelongsToActor(t *testing.T) {
	f := newIntegrationFixture(t)
	admin, secondBuyer := f.user("ADMIN"), f.user("USER")
	product := f.product(10000, 10)
	items := []OrderItemInput{{ProductID: product.ID, Quantity: 1}}
	first, err := f.store.CreateOrder(f.ctx, f.buyer, items, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.store.CreateOrder(f.ctx, secondBuyer, items, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, first.ID, admin, "ADMIN", items, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, second.ID, admin, "ADMIN", items, time.Minute); faultCode(err) != "ORDER_LIMIT_EXCEEDED" {
		t.Fatalf("admin must share one update rate limit across orders: %v", err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, first.ID, f.buyer, "USER", items, time.Minute); err != nil {
		t.Fatalf("admin operation must not consume the owner's rate limit: %v", err)
	}
}

func TestIntegrationConcurrentOrderReads(t *testing.T) {
	f := newIntegrationFixture(t)
	product := f.product(10000, 10)
	order, err := f.store.CreateOrder(f.ctx, f.buyer, []OrderItemInput{{ProductID: product.ID, Quantity: 1}}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	errors := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 80 {
			if _, err := f.store.UpdateOrder(f.ctx, order.ID, f.buyer, "USER", []OrderItemInput{{ProductID: product.ID, Quantity: 1 + i%2}}, 0); err != nil {
				errors <- err
				return
			}
		}
	})
	for range 2 {
		wg.Go(func() {
			for range 160 {
				stored, err := f.store.Order(f.ctx, order.ID, f.buyer, "USER")
				if err != nil {
					errors <- err
					return
				}
				var subtotal int64
				for _, item := range stored.Items {
					subtotal += item.PriceAtOrderCents * int64(item.Quantity)
				}
				if subtotal != stored.TotalAmountCents {
					errors <- fmt.Errorf("inconsistent response: %+v", stored)
					return
				}
			}
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestIntegrationConcurrentUserLimitAndCancel(t *testing.T) {
	f := newIntegrationFixture(t)
	product := f.product(10000, 3)
	items := []OrderItemInput{{ProductID: product.ID, Quantity: 1}}
	type result struct {
		order Order
		err   error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			order, err := f.store.CreateOrder(f.ctx, f.buyer, items, nil, time.Minute)
			results <- result{order, err}
		})
	}
	wg.Wait()
	close(results)
	var created Order
	successes := 0
	for result := range results {
		if result.err == nil {
			created = result.order
			successes++
		} else if faultCode(result.err) != "ORDER_LIMIT_EXCEEDED" {
			t.Error(result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("created %d orders, want one", successes)
	}
	f.assertStock(product.ID, 2)
	if _, err := f.store.pool.Exec(f.ctx, `UPDATE user_operations SET created_at = clock_timestamp() - interval '2 minutes' WHERE user_id = $1`, f.buyer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateOrder(f.ctx, f.buyer, items, nil, time.Minute); faultCode(err) != "ORDER_HAS_ACTIVE" {
		t.Fatalf("active check after rate limit: %v", err)
	}
	canceled := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := f.store.CancelOrder(f.ctx, created.ID, f.buyer, "USER"); canceled <- err })
	}
	wg.Wait()
	close(canceled)
	successes = 0
	for err := range canceled {
		if err == nil {
			successes++
		} else if faultCode(err) != "INVALID_STATE_TRANSITION" {
			t.Error(err)
		}
	}
	if successes != 1 {
		t.Fatalf("canceled %d times, want one", successes)
	}
	f.assertStock(product.ID, 3)
}

func TestIntegrationProductTimestamps(t *testing.T) {
	f := newIntegrationFixture(t)
	started := time.Now().Add(-time.Second)
	product := f.product(10000, 3)
	if product.CreatedAt.Before(started) || !product.CreatedAt.Equal(product.UpdatedAt) {
		t.Fatalf("invalid creation timestamps: %+v", product)
	}
	updated, err := f.store.UpdateProduct(f.ctx, product.ID, f.seller, "SELLER", ProductInput{
		Name: "Updated", PriceCents: product.PriceCents, Stock: product.Stock, Category: product.Category, Status: product.Status,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.UpdatedAt.After(product.UpdatedAt) || !updated.CreatedAt.Equal(product.CreatedAt) {
		t.Fatalf("invalid update timestamps: %+v", updated)
	}
	if err := f.store.ArchiveProduct(f.ctx, product.ID, f.seller, "SELLER"); err != nil {
		t.Fatal(err)
	}
	archived, err := f.store.Product(f.ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.UpdatedAt.After(updated.UpdatedAt) || !archived.CreatedAt.Equal(product.CreatedAt) || archived.Status != "ARCHIVED" {
		t.Fatalf("invalid archive timestamps: %+v", archived)
	}
}

func TestIntegrationOrderTimestamps(t *testing.T) {
	f := newIntegrationFixture(t)
	first := f.product(10000, 3)
	second := f.product(20000, 3)
	items := []OrderItemInput{{ProductID: first.ID, Quantity: 1}}
	order, err := f.store.CreateOrder(f.ctx, f.buyer, items, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !order.CreatedAt.Equal(order.UpdatedAt) || order.CreatedAt.Before(first.CreatedAt) {
		t.Fatalf("invalid order creation timestamps: %+v", order)
	}
	checkProduct := func(previous Product, expected time.Time) Product {
		t.Helper()
		current, err := f.store.Product(f.ctx, previous.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !current.UpdatedAt.After(previous.UpdatedAt) || !current.UpdatedAt.Equal(expected) || !current.CreatedAt.Equal(previous.CreatedAt) {
			t.Fatalf("invalid stock update timestamps: %+v", current)
		}
		return current
	}
	checkOrder := func(previous, current Order) {
		t.Helper()
		stored, err := f.store.Order(f.ctx, current.ID, f.buyer, "USER")
		if err != nil {
			t.Fatal(err)
		}
		if !current.UpdatedAt.After(previous.UpdatedAt) || !current.CreatedAt.Equal(previous.CreatedAt) || !stored.UpdatedAt.Equal(current.UpdatedAt) {
			t.Fatalf("invalid order update timestamps: %+v", current)
		}
	}
	first = checkProduct(first, order.UpdatedAt)
	updated, err := f.store.UpdateOrder(f.ctx, order.ID, f.buyer, "USER", []OrderItemInput{{ProductID: second.ID, Quantity: 2}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	checkOrder(order, updated)
	checkProduct(first, updated.UpdatedAt)
	second = checkProduct(second, updated.UpdatedAt)
	canceled, err := f.store.CancelOrder(f.ctx, order.ID, f.buyer, "USER")
	if err != nil {
		t.Fatal(err)
	}
	checkOrder(updated, canceled)
	checkProduct(second, canceled.UpdatedAt)

	order, err = f.store.CreateOrder(f.ctx, f.buyer, items, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"PAYMENT_PENDING", "PAID", "SHIPPED", "COMPLETED"} {
		next, err := f.store.TransitionOrder(f.ctx, order.ID, status)
		if err != nil {
			t.Fatal(err)
		}
		checkOrder(order, next)
		order = next
	}
}

func TestIntegrationDisabledRateLimit(t *testing.T) {
	f := newIntegrationFixture(t)
	product := f.product(10000, 3)
	items := []OrderItemInput{{ProductID: product.ID, Quantity: 1}}
	order, err := f.store.CreateOrder(f.ctx, f.buyer, items, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, order.ID, f.buyer, "USER", items, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(f.ctx, `
		UPDATE user_operations SET created_at = clock_timestamp() + interval '1 hour'
		WHERE user_id = $1
	`, f.buyer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, order.ID, f.buyer, "USER", items, 0); err != nil {
		t.Fatalf("disabled limit rejected update: %v", err)
	}
	if _, err := f.store.UpdateOrder(f.ctx, order.ID, f.buyer, "USER", items, time.Minute); faultCode(err) != "ORDER_LIMIT_EXCEEDED" {
		t.Fatalf("positive limit must still apply: %v", err)
	}
	if _, err := f.store.CancelOrder(f.ctx, order.ID, f.buyer, "USER"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateOrder(f.ctx, f.buyer, items, nil, 0); err != nil {
		t.Fatalf("disabled limit rejected create: %v", err)
	}
}
