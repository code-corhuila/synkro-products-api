package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Tier 2 integration tests (testing-strategy.md). They run against the
// database TEST_DATABASE_URL points to, with products_schema created by
// synkro-products-db's migrations, and are skipped without it. In CI they
// connect as products_app, so they also prove the adapter needs nothing
// beyond V003's grants (SELECT, INSERT, UPDATE — never DELETE). That is
// why no test cleans up: every test uses its own unique names and keys.

func openDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func newID() string         { return uuid.Must(uuid.NewV7()).String() }
func uniq() string          { return uuid.NewString()[:8] }
func newKey() string        { return "it-" + uuid.NewString() }
func ctxT() context.Context { return context.Background() }

func mustCategory(t *testing.T, repo out.CategoryRepository, name string) model.Category {
	t.Helper()
	c, _ := model.NewCategory(newID(), name)
	if _, created, err := repo.CreateOnce(ctxT(), newKey(), c); err != nil || !created {
		t.Fatalf("creating category %q: created=%v err=%v", name, created, err)
	}
	return c
}

func mustProduct(t *testing.T, repo out.ProductRepository, name string, categoryID string) model.Product {
	t.Helper()
	p, _ := model.NewProduct(newID(), name, 4599_00, categoryID)
	if _, created, err := repo.CreateOnce(ctxT(), newKey(), p); err != nil || !created {
		t.Fatalf("creating product %q: created=%v err=%v", name, created, err)
	}
	return p
}

func setStock(t *testing.T, pool *pgxpool.Pool, productID string, stock int) {
	t.Helper()
	if _, err := pool.Exec(ctxT(), `UPDATE products_schema.product SET stock = $2 WHERE product_id = $1`, productID, stock); err != nil {
		t.Fatalf("setting stock: %v", err)
	}
}

func keyRows(t *testing.T, pool *pgxpool.Pool, key string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctxT(), `SELECT count(*) FROM products_schema.idempotency_key WHERE idempotency_key = $1`, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ─── Products ──────────────────────────────────────────────────────────

func TestPostgres_ProductRoundTrip(t *testing.T) {
	db := NewPostgres(openDB(t))
	cat := mustCategory(t, db.Categories(), "Peripherals "+uniq())
	p := mustProduct(t, db.Products(), "Mouse "+uniq(), cat.ID)

	got, err := db.Products().FindByID(ctxT(), p.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got != p || got.Stock != 0 || !got.Active {
		t.Errorf("got %+v, want %+v", got, p)
	}
}

func TestPostgres_CreateOnce_ReturnsTheOriginalProductWhenTheKeyIsRepeated(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	cat := mustCategory(t, db.Categories(), "Peripherals "+uniq())
	key := newKey()

	first, _ := model.NewProduct(newID(), "Mouse "+uniq(), 100, cat.ID)
	id, created, err := db.Products().CreateOnce(ctxT(), key, first)
	if err != nil || !created || id != first.ID {
		t.Fatalf("first: id=%s created=%v err=%v", id, created, err)
	}

	second, _ := model.NewProduct(newID(), "Other "+uniq(), 200, cat.ID)
	id, created, err = db.Products().CreateOnce(ctxT(), key, second)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created || id != first.ID {
		t.Errorf("replay: expected the original id %s with created=false, got %s created=%v", first.ID, id, created)
	}
	if _, err := db.Products().FindByID(ctxT(), second.ID); !errors.Is(err, out.ErrNotFound) {
		t.Errorf("the replay must not insert a second product, FindByID: %v", err)
	}
	if n := keyRows(t, pool, key); n != 1 {
		t.Errorf("expected 1 idempotency_key row, got %d", n)
	}
}

// A failed insert must take its idempotency_key row with it: the key is
// still free afterwards.
func TestPostgres_CreateOnce_RollsBackTheKeyWhenTheInsertFails(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	key := newKey()

	orphan, _ := model.NewProduct(newID(), "Orphan "+uniq(), 100, uuid.NewString()) // fk_product_category
	if _, _, err := db.Products().CreateOnce(ctxT(), key, orphan); err == nil {
		t.Fatal("expected the foreign key to reject a product with no category")
	}
	if n := keyRows(t, pool, key); n != 0 {
		t.Fatalf("the failed insert left %d idempotency_key row(s) behind", n)
	}

	cat := mustCategory(t, db.Categories(), "Peripherals "+uniq())
	good, _ := model.NewProduct(newID(), "Mouse "+uniq(), 100, cat.ID)
	id, created, err := db.Products().CreateOnce(ctxT(), key, good)
	if err != nil || !created || id != good.ID {
		t.Errorf("the key should be free again: id=%s created=%v err=%v", id, created, err)
	}
}

func TestPostgres_CreateOnce_ConcurrentRequestsWithOneKeyCreateExactlyOneProduct(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	cat := mustCategory(t, db.Categories(), "Peripherals "+uniq())
	key := newKey()

	const n = 8
	var wg sync.WaitGroup
	ids := make([]string, n)
	createdCount := make([]bool, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, _ := model.NewProduct(newID(), "Racer "+uniq(), 100, cat.ID)
			ids[i], createdCount[i], errs[i] = db.Products().CreateOnce(ctxT(), key, p)
		}()
	}
	wg.Wait()

	created := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if createdCount[i] {
			created++
		}
		if ids[i] != ids[0] {
			t.Errorf("request %d got id %s, request 0 got %s", i, ids[i], ids[0])
		}
	}
	if created != 1 {
		t.Errorf("expected exactly 1 creation, got %d", created)
	}
}

func TestPostgres_CreateOnce_KeyUsedForAnotherResourceTypeIsRejected(t *testing.T) {
	db := NewPostgres(openDB(t))
	key := newKey()
	c, _ := model.NewCategory(newID(), "Peripherals "+uniq())
	if _, _, err := db.Categories().CreateOnce(ctxT(), key, c); err != nil {
		t.Fatal(err)
	}
	p, _ := model.NewProduct(newID(), "Mouse "+uniq(), 100, c.ID)
	if _, _, err := db.Products().CreateOnce(ctxT(), key, p); !errors.Is(err, out.ErrIdempotencyKeyReused) {
		t.Fatalf("expected ErrIdempotencyKeyReused, got %v", err)
	}
}

func TestPostgres_FindByID_UnknownOrMalformedIDIsNotFound(t *testing.T) {
	db := NewPostgres(openDB(t))
	for _, id := range []string{uuid.NewString(), "not-a-uuid"} {
		if _, err := db.Products().FindByID(ctxT(), id); !errors.Is(err, out.ErrNotFound) {
			t.Errorf("product %q: expected ErrNotFound, got %v", id, err)
		}
		if _, err := db.Categories().FindByID(ctxT(), id); !errors.Is(err, out.ErrNotFound) {
			t.Errorf("category %q: expected ErrNotFound, got %v", id, err)
		}
	}
}

func TestPostgres_ProductUpdate_ReplacesCatalogColumnsButNeverStock(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	cat := mustCategory(t, db.Categories(), "Peripherals "+uniq())
	other := mustCategory(t, db.Categories(), "Audio "+uniq())
	p := mustProduct(t, db.Products(), "Mouse "+uniq(), cat.ID)

	// a reservation lands after the product was read (p.Stock is still 0)
	setStock(t, pool, p.ID, 7)

	p.Name, p.PriceCents, p.CategoryID = "Headset "+uniq(), 999, other.ID
	p.Deactivate()
	if err := db.Products().Update(ctxT(), p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := db.Products().FindByID(ctxT(), p.ID)
	want := p
	want.Stock = 7
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPostgres_ProductUpdate_UnknownIsNotFound(t *testing.T) {
	db := NewPostgres(openDB(t))
	p := model.Product{ID: uuid.NewString(), Name: "Ghost", PriceCents: 1, CategoryID: uuid.NewString(), Active: true}
	if err := db.Products().Update(ctxT(), p); !errors.Is(err, out.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgres_ProductList_FiltersPagesAndOrdersNewestFirst(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	tag := uniq()
	cat := mustCategory(t, db.Categories(), "Peripherals "+tag)
	a := mustProduct(t, db.Products(), "Logitech MOUSE "+tag, cat.ID)
	b := mustProduct(t, db.Products(), "50% Off Pad "+tag, cat.ID)
	c := mustProduct(t, db.Products(), "500 Off Pad "+tag, cat.ID)
	setStock(t, pool, c.ID, 40)
	b.Deactivate()
	if err := db.Products().Update(ctxT(), b); err != nil {
		t.Fatal(err)
	}

	str := func(s string) *string { return &s }
	boolean := func(v bool) *bool { return &v }
	num := func(n int) *int { return &n }
	names := func(ps []model.Product) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.ID)
		}
		return out
	}
	cases := []struct {
		name  string
		f     out.ProductFilter
		want  []string
		total int
	}{
		{"by category, newest first", out.ProductFilter{CategoryID: &cat.ID}, []string{c.ID, b.ID, a.ID}, 3},
		{"name is partial and case-insensitive", out.ProductFilter{CategoryID: &cat.ID, Name: str("mouse")}, []string{a.ID}, 1},
		{"LIKE wildcards in name are literal", out.ProductFilter{CategoryID: &cat.ID, Name: str("50%")}, []string{b.ID}, 1},
		{"active", out.ProductFilter{CategoryID: &cat.ID, Active: boolean(false)}, []string{b.ID}, 1},
		{"stockAtMost", out.ProductFilter{CategoryID: &cat.ID, StockAtMost: num(0)}, []string{b.ID, a.ID}, 2},
		{"second page keeps the full total", out.ProductFilter{CategoryID: &cat.ID, Page: 2, Limit: 2}, []string{a.ID}, 3},
		{"page past the end keeps the full total", out.ProductFilter{CategoryID: &cat.ID, Page: 9, Limit: 2}, nil, 3},
	}
	for _, tc := range cases {
		if tc.f.Page == 0 {
			tc.f.Page, tc.f.Limit = 1, 20
		}
		items, total, err := db.Products().List(ctxT(), tc.f)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := names(items); fmt.Sprint(got) != fmt.Sprint(tc.want) || total != tc.total {
			t.Errorf("%s: got %v (total %d), want %v (total %d)", tc.name, got, total, tc.want, tc.total)
		}
	}
}

// ─── Categories ────────────────────────────────────────────────────────

func TestPostgres_CategoryRoundTripAndReplay(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	key := newKey()
	c, _ := model.NewCategory(newID(), "Peripherals "+uniq())

	if _, created, err := db.Categories().CreateOnce(ctxT(), key, c); err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	// The replay carries the same name, now held by that same category:
	// the key must win over the name rule.
	again, _ := model.NewCategory(newID(), c.Name)
	id, created, err := db.Categories().CreateOnce(ctxT(), key, again)
	if err != nil || created || id != c.ID {
		t.Fatalf("replay: id=%s created=%v err=%v, want %s", id, created, err, c.ID)
	}
	got, err := db.Categories().FindByID(ctxT(), c.ID)
	if err != nil || got != c {
		t.Errorf("FindByID: got %+v, %v", got, err)
	}
}

func TestPostgres_CategoryCreateOnce_DuplicateActiveNameIsReportedAndReleasesTheKey(t *testing.T) {
	pool := openDB(t)
	db := NewPostgres(pool)
	name := "Peripherals " + uniq()
	mustCategory(t, db.Categories(), name)

	key := newKey()
	dup, _ := model.NewCategory(newID(), name)
	if _, _, err := db.Categories().CreateOnce(ctxT(), key, dup); !errors.Is(err, out.ErrDuplicateActiveName) {
		t.Fatalf("expected ErrDuplicateActiveName, got %v", err)
	}
	if n := keyRows(t, pool, key); n != 0 {
		t.Fatalf("the rejected insert left %d idempotency_key row(s) behind", n)
	}
	fresh, _ := model.NewCategory(newID(), name+" II")
	if _, created, err := db.Categories().CreateOnce(ctxT(), key, fresh); err != nil || !created {
		t.Errorf("the key should be free again: created=%v err=%v", created, err)
	}
}

// FindActiveByName must use exactly the equality uq_category_name_active
// uses: case-sensitive, active rows only.
func TestPostgres_FindActiveByName_AgreesWithTheUniqueIndex(t *testing.T) {
	db := NewPostgres(openDB(t))
	tag := uniq()
	c := mustCategory(t, db.Categories(), "Peripherals "+tag)

	if got, found, err := db.Categories().FindActiveByName(ctxT(), c.Name); err != nil || !found || got != c {
		t.Errorf("exact name: got %+v found=%v err=%v", got, found, err)
	}
	lower := "peripherals " + tag
	if _, found, err := db.Categories().FindActiveByName(ctxT(), lower); err != nil || found {
		t.Errorf("different case: expected not found, got found=%v err=%v", found, err)
	}
	// ...and the index agrees that the lowercase name is not a duplicate
	other, _ := model.NewCategory(newID(), lower)
	if _, created, err := db.Categories().CreateOnce(ctxT(), newKey(), other); err != nil || !created {
		t.Errorf("the index should accept a name differing only in case: created=%v err=%v", created, err)
	}

	c.Deactivate()
	if err := db.Categories().Update(ctxT(), c); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := db.Categories().FindActiveByName(ctxT(), c.Name); found {
		t.Error("an inactive category must not be found")
	}
}

func TestPostgres_CategoryUpdate_RenameCollisionIsReportedAndDeactivationFreesTheName(t *testing.T) {
	db := NewPostgres(openDB(t))
	tag := uniq()
	a := mustCategory(t, db.Categories(), "Peripherals "+tag)
	b := mustCategory(t, db.Categories(), "Audio "+tag)

	b.Name = a.Name
	if err := db.Categories().Update(ctxT(), b); !errors.Is(err, out.ErrDuplicateActiveName) {
		t.Fatalf("expected ErrDuplicateActiveName, got %v", err)
	}

	a.Deactivate()
	if err := db.Categories().Update(ctxT(), a); err != nil {
		t.Fatal(err)
	}
	if err := db.Categories().Update(ctxT(), b); err != nil {
		t.Errorf("the name should be free after deactivation, got %v", err)
	}
	if err := db.Categories().Update(ctxT(), model.Category{ID: uuid.NewString(), Name: "Ghost", Active: true}); !errors.Is(err, out.ErrNotFound) {
		t.Errorf("unknown category: expected ErrNotFound, got %v", err)
	}
}

func TestPostgres_HasActiveProducts(t *testing.T) {
	db := NewPostgres(openDB(t))
	cat := mustCategory(t, db.Categories(), "Peripherals "+uniq())

	if busy, err := db.Categories().HasActiveProducts(ctxT(), cat.ID); err != nil || busy {
		t.Fatalf("empty category: busy=%v err=%v", busy, err)
	}
	p := mustProduct(t, db.Products(), "Mouse "+uniq(), cat.ID)
	if busy, _ := db.Categories().HasActiveProducts(ctxT(), cat.ID); !busy {
		t.Error("expected an active product to be found")
	}
	p.Deactivate()
	if err := db.Products().Update(ctxT(), p); err != nil {
		t.Fatal(err)
	}
	if busy, _ := db.Categories().HasActiveProducts(ctxT(), cat.ID); busy {
		t.Error("an inactive product must not count")
	}
}

func TestPostgres_CategoryList_FiltersAndPagesNewestFirst(t *testing.T) {
	db := NewPostgres(openDB(t))
	tag := uniq()
	a := mustCategory(t, db.Categories(), "A "+tag)
	b := mustCategory(t, db.Categories(), "B "+tag)
	c := mustCategory(t, db.Categories(), "C "+tag)
	b.Deactivate()
	if err := db.Categories().Update(ctxT(), b); err != nil {
		t.Fatal(err)
	}

	items, total, err := db.Categories().List(ctxT(), 1, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0] != c || items[1] != b || items[2] != a || total < 3 {
		t.Errorf("newest first: got %+v (total %d)", items, total)
	}

	inactive := false
	items, totalInactive, err := db.Categories().List(ctxT(), 1, 1, &inactive)
	if err != nil || len(items) != 1 || items[0] != b {
		t.Errorf("active=false: got %+v, %v", items, err)
	}
	if totalInactive >= total {
		t.Errorf("the active filter should shrink the total: %d vs %d", totalInactive, total)
	}
}
