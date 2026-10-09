package persistence

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Tier 2 integration tests for the stock-alert adapter. Two mechanisms are
// kept apart on purpose, and so are the tests that exercise them:
//
//   - the idempotency_key table collapses an exact retransmission (same key);
//   - the partial unique index uq_stock_alert_product_open is the business
//     rule — one OPEN alert per product — and must hold whatever the key.
//
// Every "different key" test below would pass only through the index.

const ixOpenAlert = "uq_stock_alert_product_open"

func newAlert(t *testing.T, productID string, stock int) model.StockAlert {
	t.Helper()
	a, err := model.OpenStockAlert(newID(), productID, stock)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func alertRows(t *testing.T, pool *pgxpool.Pool, productID, status string) int {
	return count(t, pool, `SELECT count(*) FROM products_schema.stock_alert WHERE product_id = $1 AND status = $2`, productID, status)
}

func keyTarget(t *testing.T, pool *pgxpool.Pool, key string) (resourceType, resourceID string) {
	t.Helper()
	err := pool.QueryRow(ctxT(), `SELECT resource_type, resource_id::text FROM products_schema.idempotency_key WHERE idempotency_key = $1`, key).Scan(&resourceType, &resourceID)
	if err != nil {
		t.Fatalf("reading key %q: %v", key, err)
	}
	return resourceType, resourceID
}

func mustOpen(t *testing.T, db *Postgres, productID string, stock int) model.StockAlert {
	t.Helper()
	got, created, err := db.StockAlerts().OpenOnce(ctxT(), newKey(), newAlert(t, productID, stock))
	if err != nil || !created {
		t.Fatalf("opening an alert: created=%v err=%v", created, err)
	}
	return got
}

func TestPostgres_StockAlert_OpenOnceStoresTheAlertAndItsKey(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	key := newKey()
	a := newAlert(t, p.ID, 2)

	got, created, err := db.StockAlerts().OpenOnce(ctxT(), key, a)

	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if got.ID != a.ID || got.ProductID != p.ID || got.Status != model.AlertStatusOpen || got.StockAtOpening != 2 || got.ResolvedAt != nil {
		t.Errorf("unexpected alert: %+v", got)
	}
	if got.OpenedAt.IsZero() || time.Since(got.OpenedAt) > time.Minute {
		t.Errorf("openedAt not filled by the store: %v", got.OpenedAt)
	}
	if rt, id := keyTarget(t, pool, key); rt != "STOCK_ALERT" || id != a.ID {
		t.Errorf("idempotency_key row: type=%s id=%s, want STOCK_ALERT %s", rt, id, a.ID)
	}
}

// Mechanism 1: the idempotency_key table, for an exact retransmission.
func TestPostgres_StockAlert_OpenOnceWithTheSameKeyReturnsTheOriginalAndWritesNothing(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	key := newKey()
	first, _, _ := db.StockAlerts().OpenOnce(ctxT(), key, newAlert(t, p.ID, 2))

	again, created, err := db.StockAlerts().OpenOnce(ctxT(), key, newAlert(t, p.ID, 1))

	if err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if again != first {
		t.Errorf("expected the original %+v, got %+v", first, again)
	}
	if n := alertRows(t, pool, p.ID, model.AlertStatusOpen); n != 1 {
		t.Errorf("expected 1 OPEN alert, got %d", n)
	}
	if n := keyRows(t, pool, key); n != 1 {
		t.Errorf("expected 1 idempotency_key row, got %d", n)
	}
}

// A retransmission returns the original as it is now, resolved or not.
func TestPostgres_StockAlert_ASameKeyReplayAfterResolutionReturnsTheResolvedAlert(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	key := newKey()
	first, _, _ := db.StockAlerts().OpenOnce(ctxT(), key, newAlert(t, p.ID, 2))
	if _, err := db.StockAlerts().Resolve(ctxT(), first.ID, time.Now()); err != nil {
		t.Fatal(err)
	}

	again, created, err := db.StockAlerts().OpenOnce(ctxT(), key, newAlert(t, p.ID, 2))

	if err != nil || created || again.ID != first.ID || again.Status != model.AlertStatusResolved {
		t.Errorf("created=%v err=%v alert=%+v", created, err, again)
	}
	if n := alertRows(t, pool, p.ID, model.AlertStatusOpen); n != 0 {
		t.Errorf("a replay must not reopen anything, found %d OPEN alerts", n)
	}
}

// Mechanism 2: the partial unique index. The key is different, so the
// idempotency_key table cannot be what stops the second alert.
func TestPostgres_StockAlert_ADifferentKeyForAProductWithAnOpenAlertReturnsThatAlert(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	first := mustOpen(t, db, p.ID, 2)
	dayTwoKey := newKey()

	got, created, err := db.StockAlerts().OpenOnce(ctxT(), dayTwoKey, newAlert(t, p.ID, 1))

	if err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if got != first {
		t.Errorf("expected the open alert %+v, got %+v", first, got)
	}
	if n := alertRows(t, pool, p.ID, model.AlertStatusOpen); n != 1 {
		t.Errorf("expected 1 OPEN alert, got %d", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM products_schema.stock_alert WHERE product_id = $1`, p.ID); n != 1 {
		t.Errorf("expected 1 alert row in total, got %d", n)
	}
	// The new key is spent on the existing alert, so retransmitting it
	// returns the same alert instead of looking like a new request.
	if rt, id := keyTarget(t, pool, dayTwoKey); rt != "STOCK_ALERT" || id != first.ID {
		t.Errorf("idempotency_key row: type=%s id=%s, want STOCK_ALERT %s", rt, id, first.ID)
	}
	again, created, err := db.StockAlerts().OpenOnce(ctxT(), dayTwoKey, newAlert(t, p.ID, 1))
	if err != nil || created || again.ID != first.ID {
		t.Errorf("retransmitting the day-two key: created=%v err=%v alert=%+v", created, err, again)
	}
}

// RESOLVED is final: the next drop is a new row, the old one is untouched.
func TestPostgres_StockAlert_AfterResolutionANewAlertIsOpened(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	first := mustOpen(t, db, p.ID, 2)
	resolved, _ := db.StockAlerts().Resolve(ctxT(), first.ID, time.Now())

	next, created, err := db.StockAlerts().OpenOnce(ctxT(), newKey(), newAlert(t, p.ID, 1))

	if err != nil || !created || next.ID == first.ID || next.Status != model.AlertStatusOpen || next.StockAtOpening != 1 {
		t.Fatalf("created=%v err=%v alert=%+v", created, err, next)
	}
	if n := alertRows(t, pool, p.ID, model.AlertStatusOpen); n != 1 {
		t.Errorf("expected 1 OPEN alert, got %d", n)
	}
	var status string
	var resolvedAt *time.Time
	if err := pool.QueryRow(ctxT(), `SELECT status, resolved_at FROM products_schema.stock_alert WHERE alert_id = $1`, first.ID).Scan(&status, &resolvedAt); err != nil {
		t.Fatal(err)
	}
	if status != model.AlertStatusResolved || resolvedAt == nil || !resolvedAt.Equal(*resolved.ResolvedAt) {
		t.Errorf("the old alert changed: status=%s resolvedAt=%v", status, resolvedAt)
	}
}

func TestPostgres_StockAlert_OpenOnceForAnUnknownProductIsNotFoundAndKeepsNoKey(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	key := newKey()

	_, _, err := db.StockAlerts().OpenOnce(ctxT(), key, newAlert(t, newID(), 1))

	if !errors.Is(err, out.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if n := keyRows(t, pool, key); n != 0 {
		t.Errorf("the failed open must roll its key back, found %d rows", n)
	}
	if _, _, err := db.StockAlerts().OpenOnce(ctxT(), newKey(), newAlert(t, "not-a-uuid", 1)); !errors.Is(err, out.ErrNotFound) {
		t.Errorf("a malformed product id: expected ErrNotFound, got %v", err)
	}
}

func TestPostgres_StockAlert_AKeySpentOnAnotherResourceTypeIsRefused(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	key := newKey()
	c2, _ := model.NewCategory(newID(), "Other "+uniq())
	if _, created, err := db.Categories().CreateOnce(ctxT(), key, c2); err != nil || !created {
		t.Fatalf("setup: created=%v err=%v", created, err)
	}

	_, _, err := db.StockAlerts().OpenOnce(ctxT(), key, newAlert(t, p.ID, 2))

	if !errors.Is(err, out.ErrIdempotencyKeyReused) {
		t.Errorf("expected ErrIdempotencyKeyReused, got %v", err)
	}
	if n := alertRows(t, pool, p.ID, model.AlertStatusOpen); n != 0 {
		t.Errorf("nothing may be written, found %d alerts", n)
	}
}

// The rule lives in the database itself, not only in this adapter: a
// second OPEN row for the same product is refused by the index.
func TestPostgres_StockAlert_ThePartialUniqueIndexRefusesASecondOpenRow(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	mustOpen(t, db, p.ID, 2)
	insert := `INSERT INTO products_schema.stock_alert (alert_id, product_id, status, stock_at_opening) VALUES ($1, $2, $3, 1)`

	_, err := pool.Exec(ctxT(), insert, newID(), p.ID, model.AlertStatusOpen)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation || pgErr.ConstraintName != ixOpenAlert {
		t.Fatalf("expected a unique violation on %s, got %v", ixOpenAlert, err)
	}
	// It is partial: a RESOLVED row for the same product is fine.
	if _, err := pool.Exec(ctxT(), `INSERT INTO products_schema.stock_alert (alert_id, product_id, status, stock_at_opening, resolved_at) VALUES ($1, $2, 'RESOLVED', 1, now())`, newID(), p.ID); err != nil {
		t.Errorf("a RESOLVED row must not be blocked: %v", err)
	}
}

// ─── Resolve ───────────────────────────────────────────────────────────

func TestPostgres_StockAlert_ResolveSetsStatusAndResolvedAt(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	opened := mustOpen(t, db, p.ID, 2)
	at := time.Now().UTC().Truncate(time.Microsecond)

	got, err := db.StockAlerts().Resolve(ctxT(), opened.ID, at)

	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.AlertStatusResolved || got.ResolvedAt == nil || !got.ResolvedAt.Equal(at) {
		t.Errorf("unexpected alert: %+v", got)
	}
	if got.ID != opened.ID || got.ProductID != p.ID || got.StockAtOpening != 2 || !got.OpenedAt.Equal(opened.OpenedAt) {
		t.Errorf("resolving changed more than status and resolvedAt: %+v vs %+v", opened, got)
	}
}

func TestPostgres_StockAlert_ResolvingAgainChangesNothing(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 2)
	opened := mustOpen(t, db, p.ID, 2)
	first, _ := db.StockAlerts().Resolve(ctxT(), opened.ID, time.Now())

	again, err := db.StockAlerts().Resolve(ctxT(), opened.ID, time.Now().Add(48*time.Hour))

	if err != nil {
		t.Fatalf("resolving a resolved alert must succeed, got %v", err)
	}
	if again.Status != model.AlertStatusResolved || !again.ResolvedAt.Equal(*first.ResolvedAt) {
		t.Errorf("resolvedAt moved: %v -> %v", first.ResolvedAt, again.ResolvedAt)
	}
}

func TestPostgres_StockAlert_ResolveOfAnUnknownOrMalformedIdIsNotFound(t *testing.T) {
	db := NewPostgres(openDB(t))
	for _, id := range []string{newID(), "not-a-uuid"} {
		if _, err := db.StockAlerts().Resolve(ctxT(), id, time.Now()); !errors.Is(err, out.ErrNotFound) {
			t.Errorf("%s: expected ErrNotFound, got %v", id, err)
		}
	}
}

// ─── List ──────────────────────────────────────────────────────────────

func TestPostgres_StockAlert_ListIsNewestFirstAndFiltersByStatus(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	a := mustOpen(t, db, stockedProduct(t, db, pool, 1).ID, 1)
	b := mustOpen(t, db, stockedProduct(t, db, pool, 1).ID, 1)
	c := mustOpen(t, db, stockedProduct(t, db, pool, 1).ID, 1)
	if _, err := db.StockAlerts().Resolve(ctxT(), b.ID, time.Now()); err != nil {
		t.Fatal(err)
	}

	all, total, err := db.StockAlerts().List(ctxT(), 1, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].ID != c.ID || all[1].ID != b.ID || all[2].ID != a.ID {
		t.Errorf("expected c, b, a (newest first), got %+v", all)
	}
	if want := count(t, pool, `SELECT count(*) FROM products_schema.stock_alert`); total != want {
		t.Errorf("total %d, want %d", total, want)
	}

	resolved := model.AlertStatusResolved
	only, total, err := db.StockAlerts().List(ctxT(), 1, 100, &resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range only {
		if x.Status != model.AlertStatusResolved || x.ResolvedAt == nil {
			t.Errorf("a RESOLVED filter returned %+v", x)
		}
	}
	if want := count(t, pool, `SELECT count(*) FROM products_schema.stock_alert WHERE status = 'RESOLVED'`); total != want {
		t.Errorf("RESOLVED total %d, want %d", total, want)
	}
}

func TestPostgres_StockAlert_ListPastTheLastPageIsEmptyButKeepsTheTotal(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	mustOpen(t, db, stockedProduct(t, db, pool, 1).ID, 1)

	items, total, err := db.StockAlerts().List(ctxT(), 100000, 100, nil)

	if err != nil || len(items) != 0 || items == nil || total < 1 {
		t.Errorf("items=%v total=%d err=%v", items, total, err)
	}
}

// ─── Concurrency ───────────────────────────────────────────────────────

// The worker's runs can overlap (a retry, two replicas) with different
// keys. Only the index can serialize them: every caller must get the same
// single alert, exactly one of them as the creator.
func TestPostgres_StockAlert_ConcurrentOpensWithDifferentKeysYieldOneOpenAlert(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	const callers = 20
	for round := range 3 {
		p := stockedProduct(t, db, pool, 1)
		keys := make([]string, callers)
		ids := make([]string, callers)
		createdBy := make([]bool, callers)
		errs := make([]error, callers)

		race(callers, func(i int) {
			keys[i] = newKey()
			got, created, err := db.StockAlerts().OpenOnce(ctxT(), keys[i], newAlert(t, p.ID, 1))
			ids[i], createdBy[i], errs[i] = got.ID, created, err
		})

		creators := 0
		for i := range callers {
			if errs[i] != nil {
				t.Fatalf("round %d, caller %d: %v", round, i, errs[i])
			}
			if createdBy[i] {
				creators++
			}
			if ids[i] != ids[0] {
				t.Errorf("round %d: callers saw different alerts: %s vs %s", round, ids[i], ids[0])
			}
			if _, target := keyTarget(t, pool, keys[i]); target != ids[0] {
				t.Errorf("round %d: key %d points at %s, want %s", round, i, target, ids[0])
			}
		}
		if creators != 1 {
			t.Errorf("round %d: expected exactly 1 creator, got %d", round, creators)
		}
		if n := count(t, pool, `SELECT count(*) FROM products_schema.stock_alert WHERE product_id = $1`, p.ID); n != 1 {
			t.Errorf("round %d: expected 1 alert row, got %d", round, n)
		}
	}
}

func TestPostgres_StockAlert_ConcurrentOpensWithTheSameKeyYieldOneAlert(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	p := stockedProduct(t, db, pool, 1)
	key := newKey()
	const callers = 12
	ids := make([]string, callers)
	createdBy := make([]bool, callers)
	errs := make([]error, callers)

	race(callers, func(i int) {
		got, created, err := db.StockAlerts().OpenOnce(ctxT(), key, newAlert(t, p.ID, 1))
		ids[i], createdBy[i], errs[i] = got.ID, created, err
	})

	creators := 0
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if createdBy[i] {
			creators++
		}
		if ids[i] != ids[0] {
			t.Errorf("callers saw different alerts: %s vs %s", ids[i], ids[0])
		}
	}
	if creators != 1 || keyRows(t, pool, key) != 1 || alertRows(t, pool, p.ID, model.AlertStatusOpen) != 1 {
		t.Errorf("creators=%d keys=%d open=%d, want 1 each", creators, keyRows(t, pool, key), alertRows(t, pool, p.ID, model.AlertStatusOpen))
	}
}

func TestPostgres_StockAlert_ConcurrentResolvesAgreeOnOneResolvedAt(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	opened := mustOpen(t, db, stockedProduct(t, db, pool, 1).ID, 1)
	const callers = 10
	got := make([]model.StockAlert, callers)
	errs := make([]error, callers)

	race(callers, func(i int) {
		got[i], errs[i] = db.StockAlerts().Resolve(ctxT(), opened.ID, time.Now().Add(time.Duration(i)*time.Hour))
	})

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if got[i].Status != model.AlertStatusResolved || !got[i].ResolvedAt.Equal(*got[0].ResolvedAt) {
			t.Errorf("caller %d saw %+v, caller 0 saw %+v", i, got[i], got[0])
		}
	}
}
