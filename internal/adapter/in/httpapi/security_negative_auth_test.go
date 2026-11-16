package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/code-corhuila/synkro-products-api/internal/adapter/out/persistence"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Negative authentication regression tests for the price update and the
// stock adjustment. They run in the dedicated security-negative-auth CI job
// (-run '^TestSecurityNegativeAuth_'), so keep that prefix on every test
// here. Each test goes through real HTTP and then looks at the repository:
// a 401 or 403 is only proven when nothing was written.

// countingAdjustments wraps the real adjustment repository and counts the
// calls that reach it. A rejected request must leave the count at zero: the
// adjustment, the stock change and the idempotency key are all written inside
// that one call.
type countingAdjustments struct {
	out.StockAdjustmentRepository
	calls atomic.Int32
}

func (c *countingAdjustments) CreateOnce(ctx context.Context, key string, a model.StockAdjustment) (model.StockAdjustment, int, bool, error) {
	c.calls.Add(1)
	return c.StockAdjustmentRepository.CreateOnce(ctx, key, a)
}

type negativeAuthAPI struct {
	*testAPI
	adjustments *countingAdjustments
}

func newNegativeAuthAPI(t *testing.T) *negativeAuthAPI {
	t.Helper()
	mem := persistence.NewMemory()
	counting := &countingAdjustments{StockAdjustmentRepository: mem.StockAdjustments()}
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	verifier, err := NewRS256Verifier(publicKeyPEM(testKey), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(
		verifier,
		usecase.NewProductService(mem.Products(), mem.Categories(), newID, mem.ProductUpdates(), mem.PriceHistory(), time.Now),
		usecase.NewCategoryService(mem.Categories(), newID),
		usecase.NewStockService(counting, mem.StockReservations(), newID),
		usecase.NewStockAlertService(mem.StockAlerts(), newID, time.Now),
		healthyFixture{},
	)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &negativeAuthAPI{testAPI: &testAPI{t: t, srv: srv, mem: mem}, adjustments: counting}
}

// seeded creates a product through the API and fixes its price and stock in
// the store, so the "unchanged" assertions compare against known values.
func (a *negativeAuthAPI) seeded(name string, price int64, stock int) model.Product {
	a.t.Helper()
	p := a.createProduct(name, price, a.createCategory("Cat "+name).CategoryID)
	seeded := model.Product{ID: p.ProductID, Name: p.Name, PriceCents: price, Stock: stock, CategoryID: p.CategoryID, Active: true}
	a.mem.SeedProduct(seeded)
	return seeded
}

func (a *negativeAuthAPI) stored(id string) model.Product {
	a.t.Helper()
	p, err := a.mem.Products().FindByID(context.Background(), id)
	if err != nil {
		a.t.Fatalf("reading product %s: %v", id, err)
	}
	return p
}

func (a *negativeAuthAPI) putPrice(p model.Product, newPrice int64, token string) apiResponse {
	return a.do("PUT", "/api/v1/products/"+p.ID,
		map[string]any{"name": p.Name, "priceCents": newPrice, "categoryId": p.CategoryID},
		as(token)...)
}

func (a *negativeAuthAPI) adjustStock(p model.Product, delta int, key, token string) apiResponse {
	return a.do("POST", "/api/v1/products/"+p.ID+"/stock-adjustments",
		map[string]any{"delta": delta, "reason": "Security regression"},
		append(as(token), "Idempotency-Key", key)...)
}

// editedAdminPayload is the forgery under test: the header and signature of
// a genuine low-privilege token, with the payload replaced by ADMIN claims.
func editedAdminPayload() string {
	genuine := rs256Token(validClaims(testSub, []string{"SALESPERSON"}, []string{"products:read"}))
	return tamperedPayload(genuine, validClaims(testSub, []string{"ADMIN"}, []string{"products:read", "products:write"}))
}

func TestSecurityNegativeAuth_PutPriceWithAForgedAdminTokenIs401AndThePriceIsUnchanged(t *testing.T) {
	adminClaims := validClaims(testSub, []string{"ADMIN"}, []string{"products:read", "products:write"})
	genuineAdmin := rs256Token(adminClaims)
	now := time.Now()

	forgeries := []struct{ name, token string }{
		{"edited ADMIN payload on a SALESPERSON signature", editedAdminPayload()},
		{"edited ADMIN payload on the ADMIN signature of other claims", tamperedPayload(genuineAdmin, adminClaims.with("sub", "0192a000-0000-7000-8000-0000000000ff"))},
		{"tampered signature", flippedSignature(genuineAdmin)},
		{"alg none with an empty signature", unsignedToken(adminClaims, "")},
		{"alg none with a garbage signature", unsignedToken(adminClaims, "c2ln")},
		{"signed with another RSA key", signedWith(otherKey, map[string]any{"alg": "RS256", "typ": "JWT"}, adminClaims)},
		{"HS256 keyed with the public key", hs256Token(publicKeyPEM(testKey), adminClaims)},
		{"RS384 header", signedWith(testKey, map[string]any{"alg": "RS384", "typ": "JWT"}, adminClaims)},
		{"header without alg", signedWith(testKey, map[string]any{"typ": "JWT"}, adminClaims)},
		{"expired beyond the skew", rs256Token(adminClaims.with("exp", now.Add(-time.Hour).Unix()))},
		{"exp missing", rs256Token(adminClaims.without("exp"))},
	}

	api := newNegativeAuthAPI(t)
	for _, f := range forgeries {
		p := api.seeded("Mouse "+f.name, 10_000, 7)

		r := api.putPrice(p, 1, f.token)

		if r.status != http.StatusUnauthorized {
			t.Errorf("%s: PUT answered %d, want 401: %s", f.name, r.status, r.body)
			continue
		}
		if e := r.errorBody(t); e.Error != "UNAUTHORIZED" {
			t.Errorf("%s: error code %q, want UNAUTHORIZED", f.name, e.Error)
		}
		if got := api.stored(p.ID); got != p {
			t.Errorf("%s: product changed after a rejected PUT: got %+v, want %+v", f.name, got, p)
		}
	}

	// Control: the same request with the genuine token succeeds, so every
	// 401 above can only come from the credential.
	p := api.seeded("Control mouse", 10_000, 7)
	if r := api.putPrice(p, 1, genuineAdmin); r.status != http.StatusOK {
		t.Fatalf("control: genuine ADMIN PUT answered %d, want 200: %s", r.status, r.body)
	}
	if got := api.stored(p.ID); got.PriceCents != 1 || got.Stock != 7 {
		t.Errorf("control: stored %+v, want price 1 and stock 7", got)
	}
}

func TestSecurityNegativeAuth_PutPriceWithoutWritePermissionIs403AndThePriceIsUnchanged(t *testing.T) {
	api := newNegativeAuthAPI(t)
	p := api.seeded("Mouse", 10_000, 7)

	salesperson := rs256Token(validClaims(testSub, []string{"SALESPERSON"}, []string{"products:read"}))
	r := api.putPrice(p, 1, salesperson)

	if r.status != http.StatusForbidden {
		t.Fatalf("SALESPERSON PUT answered %d, want 403: %s", r.status, r.body)
	}
	if got := api.stored(p.ID); got != p {
		t.Errorf("product changed after a 403: got %+v, want %+v", got, p)
	}
}

func TestSecurityNegativeAuth_SalespersonStockAdjustmentIs403AndNothingIsPersisted(t *testing.T) {
	api := newNegativeAuthAPI(t)
	p := api.seeded("Mouse", 10_000, 7)
	const key = "negative-auth-key-1"

	salesperson := rs256Token(validClaims(testSub, []string{"SALESPERSON"}, []string{"products:read"}))
	r := api.adjustStock(p, 5, key, salesperson)

	if r.status != http.StatusForbidden {
		t.Fatalf("SALESPERSON adjustment answered %d, want 403: %s", r.status, r.body)
	}
	if e := r.errorBody(t); e.Error != "FORBIDDEN" {
		t.Errorf("error code %q, want FORBIDDEN", e.Error)
	}
	if n := api.adjustments.calls.Load(); n != 0 {
		t.Errorf("the adjustment repository was called %d times, want 0", n)
	}
	if got := api.stored(p.ID); got != p {
		t.Errorf("product changed after a 403: got %+v, want %+v", got, p)
	}

	// The idempotency key was not claimed: an authorized caller can still
	// use it, and the adjustment is applied exactly once.
	inventory := rs256Token(validClaims(testSub, []string{"INVENTORY"}, []string{"products:read", "products:write"}))
	again := api.adjustStock(p, 5, key, inventory)
	if again.status != http.StatusCreated {
		t.Fatalf("the same key with INVENTORY answered %d, want 201 (key must be unclaimed): %s", again.status, again.body)
	}
	if got := api.stored(p.ID); got.Stock != 12 {
		t.Errorf("stock %d after the authorized adjustment, want 12", got.Stock)
	}
	if n := api.adjustments.calls.Load(); n != 1 {
		t.Errorf("the adjustment repository was called %d times, want 1", n)
	}
}

func TestSecurityNegativeAuth_ForgedTokenStockAdjustmentIs401AndNothingIsPersisted(t *testing.T) {
	adminClaims := validClaims(testSub, []string{"ADMIN"}, []string{"products:read", "products:write"})
	forgeries := []struct{ name, token string }{
		{"edited ADMIN payload", editedAdminPayload()},
		{"alg none", unsignedToken(adminClaims, "")},
		{"other RSA key", signedWith(otherKey, map[string]any{"alg": "RS256", "typ": "JWT"}, adminClaims)},
		{"expired", rs256Token(adminClaims.with("exp", time.Now().Add(-time.Hour).Unix()))},
		{"HS256 with public key", hs256Token(publicKeyPEM(testKey), adminClaims)},
		{"tampered signature", flippedSignature(rs256Token(adminClaims))},
		{"empty bearer token", ""},
	}

	api := newNegativeAuthAPI(t)
	p := api.seeded("Mouse", 10_000, 7)
	for _, f := range forgeries {
		r := api.adjustStock(p, 5, "forged-key-"+uuid.NewString()[:8], f.token)
		if r.status != http.StatusUnauthorized {
			t.Errorf("%s: adjustment answered %d, want 401: %s", f.name, r.status, r.body)
		}
	}
	if n := api.adjustments.calls.Load(); n != 0 {
		t.Errorf("the adjustment repository was called %d times, want 0", n)
	}
	if got := api.stored(p.ID); got != p {
		t.Errorf("product changed after rejected adjustments: got %+v, want %+v", got, p)
	}
}
