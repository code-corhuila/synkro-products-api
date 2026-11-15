//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/adapter/out/persistence"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Real HTTP server and real PostgreSQL, as products_app: the author is read
// from products_schema.stock_adjustment itself, not from a response or a
// test double. Like the other real-database tests of this package, a missing
// TEST_DATABASE_URL is a failure, never a skip.

type pgAuthorAPI struct {
	*testAPI
	pool *pgxpool.Pool
}

func newPostgresAuthorAPI(t *testing.T) *pgAuthorAPI {
	t.Helper()
	pool := openHealthPool(t)
	db := persistence.NewPostgres(pool)
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	verifier, err := NewRS256Verifier(publicKeyPEM(testKey), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(
		verifier,
		usecase.NewProductService(db.Products(), db.Categories(), newID),
		usecase.NewCategoryService(db.Categories(), newID),
		usecase.NewStockService(db.StockAdjustments(), db.StockReservations(), newID),
		usecase.NewStockAlertService(db.StockAlerts(), newID, time.Now),
		usecase.NewHealthService(db, time.Second),
	)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &pgAuthorAPI{testAPI: &testAPI{t: t, srv: srv}, pool: pool}
}

// product stores a product with the given stock through the real adapter.
func (a *pgAuthorAPI) product(stock int) model.Product {
	a.t.Helper()
	ctx := context.Background()
	db := persistence.NewPostgres(a.pool)
	cat, err := model.NewCategory(uuid.Must(uuid.NewV7()).String(), "Cat "+uuid.NewString())
	if err != nil {
		a.t.Fatal(err)
	}
	if _, _, err := db.Categories().CreateOnce(ctx, "author-cat-"+uuid.NewString(), cat); err != nil {
		a.t.Fatalf("creating category: %v", err)
	}
	p, err := model.NewProduct(uuid.Must(uuid.NewV7()).String(), "Mouse "+uuid.NewString()[:8], 10_000, cat.ID)
	if err != nil {
		a.t.Fatal(err)
	}
	if _, _, err := db.Products().CreateOnce(ctx, "author-prod-"+uuid.NewString(), p); err != nil {
		a.t.Fatalf("creating product: %v", err)
	}
	if _, err := a.pool.Exec(ctx, `UPDATE products_schema.product SET stock = $2 WHERE product_id = $1`, p.ID, stock); err != nil {
		a.t.Fatalf("setting stock: %v", err)
	}
	p.Stock = stock
	return p
}

func (a *pgAuthorAPI) count(query string, args ...any) int {
	a.t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		a.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (a *pgAuthorAPI) stockOf(productID string) int {
	return a.count(`SELECT stock FROM products_schema.product WHERE product_id = $1`, productID)
}

func (a *pgAuthorAPI) adjust(token, productID, key string, body map[string]any) apiResponse {
	return a.do("POST", "/api/v1/products/"+productID+"/stock-adjustments", body,
		append(as(token), "Idempotency-Key", key)...)
}

func TestStockAdjustmentAuthorPostgres_StoredAuthorIsTheVerifiedSubNotTheBodyAdjustedBy(t *testing.T) {
	api := newPostgresAuthorAPI(t)
	p := api.product(10)
	key := "author-it-" + uuid.NewString()

	r := api.adjust(adjustmentAuthorInventoryToken(), p.ID, key, map[string]any{"delta": -3, "reason": "Damaged", "adjustedBy": forgedAuthorSub})

	if r.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", r.status, r.body)
	}
	var created adjustmentJSON
	r.decode(t, &created)

	var author string
	var delta, stockAfter int
	err := api.pool.QueryRow(context.Background(), `
		SELECT adjusted_by::text, delta, stock_after
		FROM products_schema.stock_adjustment WHERE adjustment_id = $1`, created.AdjustmentID).Scan(&author, &delta, &stockAfter)
	if err != nil {
		t.Fatalf("reading the stored adjustment: %v", err)
	}
	if author != inventoryUserSub {
		t.Errorf("stored adjusted_by %q, want the verified sub %q (forged body value %q must be ignored)", author, inventoryUserSub, forgedAuthorSub)
	}
	if delta != -3 || stockAfter != 7 {
		t.Errorf("stored delta %d / stock_after %d, want -3 / 7", delta, stockAfter)
	}
	if n := api.count(`SELECT count(*) FROM products_schema.stock_adjustment WHERE adjusted_by = $1 AND product_id = $2`, forgedAuthorSub, p.ID); n != 0 {
		t.Errorf("%d rows were stored under the forged author, want 0", n)
	}
}

func TestStockAdjustmentAuthorPostgres_AnInvalidSignatureCarryingAnotherUsersSubLeavesNoRowsAndStockAndKeyUnchanged(t *testing.T) {
	api := newPostgresAuthorAPI(t)
	p := api.product(10)
	key := "author-it-" + uuid.NewString()
	claims := validClaims(forgedAuthorSub, []string{"INVENTORY"}, []string{"products:read", "products:write"})
	forgeries := []struct{ name, token string }{
		{"signed with another RSA key", signedWith(otherKey, map[string]any{"alg": "RS256", "typ": "JWT"}, claims)},
		{"tampered signature", flippedSignature(rs256Token(claims))},
		{"alg none", unsignedToken(claims, "")},
	}

	for _, f := range forgeries {
		r := api.adjust(f.token, p.ID, key, map[string]any{"delta": 5, "reason": "Restock", "adjustedBy": forgedAuthorSub})
		if r.status != http.StatusUnauthorized {
			t.Errorf("%s: answered %d, want 401: %s", f.name, r.status, r.body)
		}
	}

	if n := api.count(`SELECT count(*) FROM products_schema.stock_adjustment WHERE product_id = $1`, p.ID); n != 0 {
		t.Errorf("%d adjustment rows exist for the product, want 0", n)
	}
	if n := api.count(`SELECT count(*) FROM products_schema.stock_adjustment WHERE adjusted_by = $1`, forgedAuthorSub); n != 0 {
		t.Errorf("%d adjustment rows exist under the other user's sub, want 0", n)
	}
	if n := api.count(`SELECT count(*) FROM products_schema.idempotency_key WHERE idempotency_key = $1`, key); n != 0 {
		t.Errorf("the idempotency key was stored %d times, want 0", n)
	}
	if got := api.stockOf(p.ID); got != 10 {
		t.Errorf("stock %d, want 10 (unchanged)", got)
	}
}
