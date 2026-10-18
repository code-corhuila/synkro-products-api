package persistence

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Tier 2 integration tests for the stock adapters. As in the catalog
// tests, nothing is cleaned up: every test makes its own products and
// keys, and the adapter only needs V003's grants.

func stockOf(t *testing.T, pool *pgxpool.Pool, productID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctxT(), `SELECT stock FROM products_schema.product WHERE product_id = $1`, productID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctxT(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func adjustmentRows(t *testing.T, pool *pgxpool.Pool, productID string) int {
	return count(t, pool, `SELECT count(*) FROM products_schema.stock_adjustment WHERE product_id = $1`, productID)
}

func reservationRows(t *testing.T, pool *pgxpool.Pool, id string) int {
	return count(t, pool, `SELECT count(*) FROM products_schema.stock_reservation WHERE reservation_id = $1`, id)
}

// stockedProduct creates a product with the given stock.
func stockedProduct(t *testing.T, db *Postgres, pool *pgxpool.Pool, stock int) model.Product {
	t.Helper()
	cat := mustCategory(t, db.Categories(), "Cat "+uniq())
	p := mustProduct(t, db.Products(), "Item "+uniq(), cat.ID)
	setStock(t, pool, p.ID, stock)
	p.Stock = stock
	return p
}

func newAdjustment(t *testing.T, productID string, delta int) model.StockAdjustment {
	t.Helper()
	a, err := model.NewStockAdjustment(newID(), productID, delta, "recount", newID())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// newReservation builds a reservation the way the use case does: line
// ids, no prices (the adapter must read them).
func newReservation(t *testing.T, lines ...model.ReservationLine) model.StockReservation {
	t.Helper()
	for i := range lines {
		lines[i].LineID = newID()
	}
	r, err := model.NewStockReservation(newID(), lines)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func want(productID string, qty int) model.ReservationLine {
	return model.ReservationLine{ProductID: productID, Quantity: qty}
}

// ─── Adjustments ───────────────────────────────────────────────────────

func TestPostgres_StockAdjustment_AppliesPositiveAndNegativeDeltas(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)

	a1 := newAdjustment(t, p.ID, 5)
	got, current, created, err := db.StockAdjustments().CreateOnce(ctxT(), newKey(), a1)
	if err != nil || !created || current != 15 {
		t.Fatalf("restock: current=%d created=%v err=%v", current, created, err)
	}
	if got.ID != a1.ID || got.ProductID != p.ID || got.Delta != 5 || got.Reason != "recount" || got.AdjustedBy != a1.AdjustedBy || got.AdjustedAt.IsZero() {
		t.Errorf("adjustment: %+v", got)
	}
	if got.StockAfter == nil || *got.StockAfter != 15 {
		t.Errorf("StockAfter %v, want 15", got.StockAfter)
	}

	_, current, _, err = db.StockAdjustments().CreateOnce(ctxT(), newKey(), newAdjustment(t, p.ID, -15))
	if err != nil || current != 0 || stockOf(t, pool, p.ID) != 0 {
		t.Errorf("down to exactly zero: current=%d stored=%d err=%v", current, stockOf(t, pool, p.ID), err)
	}
	if n := adjustmentRows(t, pool, p.ID); n != 2 {
		t.Errorf("expected 2 adjustment rows, got %d", n)
	}
}

func TestPostgres_StockAdjustment_BelowZeroChangesNothingAndReleasesTheKey(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	key := newKey()

	_, _, _, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -3))
	if !errors.Is(err, out.ErrInsufficientStock) {
		t.Fatalf("expected ErrInsufficientStock, got %v", err)
	}
	if got := stockOf(t, pool, p.ID); got != 2 {
		t.Errorf("stock %d, want 2", got)
	}
	if n := adjustmentRows(t, pool, p.ID); n != 0 {
		t.Errorf("%d adjustment rows, want 0", n)
	}
	if n := keyRows(t, pool, key); n != 0 {
		t.Errorf("the failed attempt kept its idempotency key (%d rows)", n)
	}

	// The key was not spent: once stock allows it, the same key works.
	setStock(t, pool, p.ID, 10)
	if _, _, created, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -3)); err != nil || !created {
		t.Errorf("retry with the same key: created=%v err=%v", created, err)
	}
}

func TestPostgres_StockAdjustment_ReplayReturnsTheOriginalAndAppliesNothingAgain(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	key := newKey()

	first, _, created, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -4))
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	// The replay carries a different adjustment (and stock has since
	// dropped so far that it would not even fit): it must be ignored.
	setStock(t, pool, p.ID, 1)
	again, current, created, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -9))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created || again.ID != first.ID || again.Delta != -4 || !again.AdjustedAt.Equal(first.AdjustedAt) {
		t.Errorf("replay: created=%v %+v, want the original %+v", created, again, first)
	}
	// currentStock is what this adjustment left (10-4), not the 1 now in the product.
	if current != 6 || stockOf(t, pool, p.ID) != 1 {
		t.Errorf("replay: current=%d stored=%d, want 6 and 1 (nothing re-applied)", current, stockOf(t, pool, p.ID))
	}
	if n := adjustmentRows(t, pool, p.ID); n != 1 {
		t.Errorf("%d adjustment rows, want 1", n)
	}
}

func TestPostgres_StockAdjustment_UnknownProductIsNotFoundAndReleasesTheKey(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	key := newKey()

	_, _, _, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, newID(), 1))
	if !errors.Is(err, out.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if n := keyRows(t, pool, key); n != 0 {
		t.Errorf("the failed attempt kept its idempotency key (%d rows)", n)
	}
	// A malformed id cannot name a row either.
	bad := newAdjustment(t, "not-a-uuid", 1)
	if _, _, _, err := db.StockAdjustments().CreateOnce(ctxT(), newKey(), bad); !errors.Is(err, out.ErrNotFound) {
		t.Errorf("malformed product id: got %v", err)
	}
}

func TestPostgres_StockAdjustment_KeyUsedForAnotherResourceTypeIsRejected(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	key := newKey()
	if _, _, err := db.StockReservations().CreateOnce(ctxT(), key, newReservation(t, want(p.ID, 1))); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, 1))
	if !errors.Is(err, out.ErrIdempotencyKeyReused) {
		t.Fatalf("expected ErrIdempotencyKeyReused, got %v", err)
	}
	if got := stockOf(t, pool, p.ID); got != 9 {
		t.Errorf("stock %d, want 9 (only the reservation applied)", got)
	}
}

// ─── Reservations ──────────────────────────────────────────────────────

func TestPostgres_StockReservation_DecrementsEveryLineAndFreezesThePriceFromTheRow(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p1 := stockedProduct(t, db, pool, 10) // price 459900 (mustProduct)
	p2 := stockedProduct(t, db, pool, 5)

	// A stale price from the caller must be ignored: the adapter reads it.
	l1 := want(p1.ID, 3)
	l1.UnitPriceCents = 1
	r := newReservation(t, l1, want(p2.ID, 5))

	got, created, err := db.StockReservations().CreateOnce(ctxT(), newKey(), r)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if got.ID != r.ID || got.Status != model.StatusReserved || got.CreatedAt.IsZero() || got.ReleasedAt != nil || len(got.Lines) != 2 {
		t.Fatalf("reservation: %+v", got)
	}
	for i, l := range got.Lines {
		if l.LineID != r.Lines[i].LineID || l.ProductID != r.Lines[i].ProductID || l.Quantity != r.Lines[i].Quantity || l.UnitPriceCents != 4599_00 {
			t.Errorf("line %d: %+v", i, l)
		}
	}
	if stockOf(t, pool, p1.ID) != 7 || stockOf(t, pool, p2.ID) != 0 {
		t.Errorf("stock p1=%d p2=%d, want 7 and 0", stockOf(t, pool, p1.ID), stockOf(t, pool, p2.ID))
	}

	found, err := db.StockReservations().FindByID(ctxT(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != got.ID || found.Status != got.Status || !found.CreatedAt.Equal(got.CreatedAt) || len(found.Lines) != 2 ||
		found.Lines[0] != got.Lines[0] || found.Lines[1] != got.Lines[1] {
		t.Errorf("FindByID %+v differs from the created %+v (line order must be kept)", found, got)
	}
}

// The most important behavior of the story, at the real-transaction tier:
// one affordable line and one that is not. Neither product may lose
// stock, and no row may be left behind.
func TestPostgres_StockReservation_MixedAvailabilityReservesNothing(t *testing.T) {
	for name, order := range map[string]bool{"affordable first": true, "unaffordable first": false} {
		t.Run(name, func(t *testing.T) {
			pool := openDB(t)
			db := NewPostgres(pool)
			ok := stockedProduct(t, db, pool, 10)
			short := stockedProduct(t, db, pool, 3)
			key := newKey()

			lines := []model.ReservationLine{want(ok.ID, 2), want(short.ID, 9)}
			failing := 1
			if !order {
				lines[0], lines[1] = lines[1], lines[0]
				failing = 0
			}
			r := newReservation(t, lines...)

			_, _, err := db.StockReservations().CreateOnce(ctxT(), key, r)
			var le *out.LineError
			if !errors.Is(err, out.ErrInsufficientStock) || !errors.As(err, &le) || le.ProductID != short.ID || le.Index != failing {
				t.Fatalf("expected a LineError(ErrInsufficientStock) on line %d for the short product, got %#v", failing, err)
			}
			if stockOf(t, pool, ok.ID) != 10 || stockOf(t, pool, short.ID) != 3 {
				t.Errorf("stock ok=%d short=%d, want 10 and 3: nothing may be reserved", stockOf(t, pool, ok.ID), stockOf(t, pool, short.ID))
			}
			if n := reservationRows(t, pool, r.ID); n != 0 {
				t.Errorf("%d reservation rows left behind", n)
			}
			if n := count(t, pool, `SELECT count(*) FROM products_schema.stock_reservation_line WHERE reservation_id = $1`, r.ID); n != 0 {
				t.Errorf("%d reservation line rows left behind", n)
			}
			if n := keyRows(t, pool, key); n != 0 {
				t.Errorf("the failed attempt kept its idempotency key (%d rows)", n)
			}
		})
	}
}

func TestPostgres_StockReservation_InactiveOrUnknownProductReservesNothing(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	ok := stockedProduct(t, db, pool, 10)
	off := stockedProduct(t, db, pool, 10)
	if _, err := pool.Exec(ctxT(), `UPDATE products_schema.product SET active = false WHERE product_id = $1`, off.ID); err != nil {
		t.Fatal(err)
	}

	for name, bad := range map[string]string{"inactive": off.ID, "unknown": newID(), "malformed": "not-a-uuid"} {
		_, _, err := db.StockReservations().CreateOnce(ctxT(), newKey(), newReservation(t, want(ok.ID, 1), want(bad, 1)))
		var le *out.LineError
		if !errors.Is(err, out.ErrProductUnavailable) || !errors.As(err, &le) || le.Index != 1 || le.ProductID != bad {
			t.Errorf("%s: expected a LineError(ErrProductUnavailable) on line 1, got %#v", name, err)
		}
	}
	if stockOf(t, pool, ok.ID) != 10 || stockOf(t, pool, off.ID) != 10 {
		t.Errorf("stock ok=%d off=%d, want 10 and 10", stockOf(t, pool, ok.ID), stockOf(t, pool, off.ID))
	}
}

func TestPostgres_StockReservation_FrozenPriceDoesNotMoveWhenTheProductPriceChanges(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	r := newReservation(t, want(p.ID, 2))
	if _, _, err := db.StockReservations().CreateOnce(ctxT(), newKey(), r); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctxT(), `UPDATE products_schema.product SET price_cents = 999999 WHERE product_id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}

	got, err := db.StockReservations().FindByID(ctxT(), r.ID)
	if err != nil || got.Lines[0].UnitPriceCents != 4599_00 {
		t.Fatalf("frozen price moved: %+v err=%v", got.Lines, err)
	}

	// A new reservation sees the new price: the price is read per call.
	r2 := newReservation(t, want(p.ID, 1))
	got2, _, err := db.StockReservations().CreateOnce(ctxT(), newKey(), r2)
	if err != nil || got2.Lines[0].UnitPriceCents != 999999 {
		t.Errorf("second reservation: %+v err=%v, want price 999999", got2.Lines, err)
	}
}

func TestPostgres_StockReservation_ReplayReturnsTheOriginalAndChangesNothing(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	key := newKey()
	first, _, err := db.StockReservations().CreateOnce(ctxT(), key, newReservation(t, want(p.ID, 4)))
	if err != nil {
		t.Fatal(err)
	}

	second := newReservation(t, want(p.ID, 6))
	again, created, err := db.StockReservations().CreateOnce(ctxT(), key, second)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created || again.ID != first.ID || len(again.Lines) != 1 || again.Lines[0] != first.Lines[0] {
		t.Errorf("replay: created=%v %+v, want the original %+v", created, again, first)
	}
	if got := stockOf(t, pool, p.ID); got != 6 {
		t.Errorf("stock %d, want 6 (reserved once)", got)
	}
	if n := reservationRows(t, pool, second.ID); n != 0 {
		t.Errorf("the replay inserted a second reservation")
	}
}

func TestPostgres_StockReservation_ReleaseRestoresStockExactlyOnce(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p1 := stockedProduct(t, db, pool, 10)
	p2 := stockedProduct(t, db, pool, 4)
	r := newReservation(t, want(p1.ID, 3), want(p2.ID, 4))
	if _, _, err := db.StockReservations().CreateOnce(ctxT(), newKey(), r); err != nil {
		t.Fatal(err)
	}

	first, err := db.StockReservations().Release(ctxT(), r.ID)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if first.Status != model.StatusReleased || first.ReleasedAt == nil || len(first.Lines) != 2 {
		t.Fatalf("released: %+v", first)
	}
	if stockOf(t, pool, p1.ID) != 10 || stockOf(t, pool, p2.ID) != 4 {
		t.Fatalf("after release p1=%d p2=%d, want 10 and 4", stockOf(t, pool, p1.ID), stockOf(t, pool, p2.ID))
	}

	second, err := db.StockReservations().Release(ctxT(), r.ID)
	if err != nil {
		t.Fatalf("second release: %v", err)
	}
	if second.Status != model.StatusReleased || !second.ReleasedAt.Equal(*first.ReleasedAt) {
		t.Errorf("second release changed the resource: %+v vs %+v", second, first)
	}
	if stockOf(t, pool, p1.ID) != 10 || stockOf(t, pool, p2.ID) != 4 {
		t.Errorf("a second release restored stock again: p1=%d p2=%d", stockOf(t, pool, p1.ID), stockOf(t, pool, p2.ID))
	}
}

func TestPostgres_StockReservation_UnknownOrMalformedIDIsNotFound(t *testing.T) {
	db := NewPostgres(openDB(t))
	for _, id := range []string{newID(), "not-a-uuid"} {
		if _, err := db.StockReservations().FindByID(ctxT(), id); !errors.Is(err, out.ErrNotFound) {
			t.Errorf("FindByID(%s): got %v", id, err)
		}
		if _, err := db.StockReservations().Release(ctxT(), id); !errors.Is(err, out.ErrNotFound) {
			t.Errorf("Release(%s): got %v", id, err)
		}
	}
}

// The point of stock_after: a replay answers with the stock THIS adjustment
// left, however much the product has moved since (another adjustment, a
// reservation) — "the same resource", not a fresh reading.
func TestPostgres_StockAdjustment_ReplayReturnsTheStockRecordedAtAdjustmentTime(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	key := newKey()

	first, current, created, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -4))
	if err != nil || !created || current != 6 {
		t.Fatalf("first: current=%d created=%v err=%v", current, created, err)
	}
	if n := count(t, pool, `SELECT stock_after FROM products_schema.stock_adjustment WHERE adjustment_id = $1`, first.ID); n != 6 {
		t.Fatalf("stock_after column holds %d, want 6", n)
	}

	// The product moves on: another adjustment, then a reservation.
	if _, cur, _, err := db.StockAdjustments().CreateOnce(ctxT(), newKey(), newAdjustment(t, p.ID, 20)); err != nil || cur != 26 {
		t.Fatalf("second adjustment: current=%d err=%v", cur, err)
	}
	if _, _, err := db.StockReservations().CreateOnce(ctxT(), newKey(), newReservation(t, want(p.ID, 3))); err != nil {
		t.Fatal(err)
	}
	if got := stockOf(t, pool, p.ID); got != 23 {
		t.Fatalf("setup: product stock %d, want 23", got)
	}

	again, current, created, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -1))
	if err != nil || created {
		t.Fatalf("replay: created=%v err=%v", created, err)
	}
	if current != 6 {
		t.Errorf("replay currentStock %d, want 6 (recorded at adjustment time), not the product's 23 now", current)
	}
	if again.StockAfter == nil || *again.StockAfter != 6 {
		t.Errorf("replay StockAfter %v, want 6", again.StockAfter)
	}
}

// The one backward-compatibility case: an adjustment written before V017
// has stock_after NULL, and its replay can only fall back to the
// product's stock now.
func TestPostgres_StockAdjustment_ReplayOfARowWithoutStockAfterFallsBackToCurrentStock(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 10)
	key, adjID := newKey(), newID()
	if _, err := pool.Exec(ctxT(), `
		INSERT INTO products_schema.idempotency_key (idempotency_key, resource_type, resource_id)
		VALUES ($1, 'STOCK_ADJUSTMENT', $2)`, key, adjID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctxT(), `
		INSERT INTO products_schema.stock_adjustment (adjustment_id, product_id, delta, reason, adjusted_by)
		VALUES ($1, $2, -2, 'legacy', $3)`, adjID, p.ID, newID()); err != nil {
		t.Fatal(err)
	}

	again, current, created, err := db.StockAdjustments().CreateOnce(ctxT(), key, newAdjustment(t, p.ID, -1))
	if err != nil || created || again.ID != adjID {
		t.Fatalf("replay: id=%s created=%v err=%v", again.ID, created, err)
	}
	if again.StockAfter != nil {
		t.Errorf("StockAfter %v, want nil for a legacy row", *again.StockAfter)
	}
	if current != 10 {
		t.Errorf("currentStock %d, want 10 (the product's stock now)", current)
	}
}
