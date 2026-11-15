package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/code-corhuila/synkro-products-api/internal/adapter/out/persistence"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// The author of a stock adjustment is the verified sub of the token, never a
// value the client sends. These tests go through real HTTP with a valid body
// and then read the stored adjustment back from the repository.

const (
	// inventoryUserSub is the verified identity: the sub of the INVENTORY
	// token. forgedAuthorSub is another user's id, the one an attacker puts in
	// the body (adjustedBy) or in a token it cannot sign.
	inventoryUserSub = "0192a000-0000-7000-8000-0000000000b2"
	forgedAuthorSub  = "0192a000-0000-7000-8000-0000000000c3"
)

// recordingAdjustments wraps the real adjustment repository and remembers
// every call that reaches it, so a rejected request can be proven to have
// written nothing: the adjustment, the stock change and the idempotency key
// are all written inside that one call.
type recordingAdjustments struct {
	out.StockAdjustmentRepository
	mu       sync.Mutex
	attempts []model.StockAdjustment
}

func (r *recordingAdjustments) CreateOnce(ctx context.Context, key string, a model.StockAdjustment) (model.StockAdjustment, int, bool, error) {
	r.mu.Lock()
	r.attempts = append(r.attempts, a)
	r.mu.Unlock()
	return r.StockAdjustmentRepository.CreateOnce(ctx, key, a)
}

func (r *recordingAdjustments) attemptCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.attempts)
}

type authorAPI struct {
	*testAPI
	adjustments *recordingAdjustments
}

func newAuthorAPI(t *testing.T) *authorAPI {
	t.Helper()
	mem := persistence.NewMemory()
	recording := &recordingAdjustments{StockAdjustmentRepository: mem.StockAdjustments()}
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	verifier, err := NewRS256Verifier(publicKeyPEM(testKey), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(
		verifier,
		usecase.NewProductService(mem.Products(), mem.Categories(), newID),
		usecase.NewCategoryService(mem.Categories(), newID),
		usecase.NewStockService(recording, mem.StockReservations(), newID),
		usecase.NewStockAlertService(mem.StockAlerts(), newID, time.Now),
		healthyFixture{},
	)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &authorAPI{testAPI: &testAPI{t: t, srv: srv, mem: mem}, adjustments: recording}
}

func (a *authorAPI) stockedProduct(stock int) model.Product {
	a.t.Helper()
	created := a.createProduct("Mouse "+uuid.NewString()[:8], 10_000, a.createCategory("Cat "+uuid.NewString()[:8]).CategoryID)
	p := model.Product{ID: created.ProductID, Name: created.Name, PriceCents: 10_000, Stock: stock, CategoryID: created.CategoryID, Active: true}
	a.mem.SeedProduct(p)
	return p
}

func (a *authorAPI) stock(productID string) int {
	a.t.Helper()
	p, err := a.mem.Products().FindByID(context.Background(), productID)
	if err != nil {
		a.t.Fatalf("reading product %s: %v", productID, err)
	}
	return p.Stock
}

func (a *authorAPI) adjustAs(token, productID, key string, body map[string]any) apiResponse {
	return a.do("POST", "/api/v1/products/"+productID+"/stock-adjustments", body,
		append(as(token), "Idempotency-Key", key)...)
}

// storedAdjustment reads the adjustment saved under key back from the
// repository. A repeated key returns the stored original and applies
// nothing, so this does not change what it inspects.
func (a *authorAPI) storedAdjustment(productID, key string) model.StockAdjustment {
	a.t.Helper()
	probe := model.StockAdjustment{ID: uuid.NewString(), ProductID: productID, Delta: 1, Reason: "probe", AdjustedBy: forgedAuthorSub}
	stored, _, created, err := a.mem.StockAdjustments().CreateOnce(context.Background(), key, probe)
	if err != nil {
		a.t.Fatalf("reading adjustment under key %s: %v", key, err)
	}
	if created {
		a.t.Fatalf("no adjustment was stored under key %s", key)
	}
	return stored
}

func adjustmentAuthorInventoryToken() string {
	return tokenWithClaims(inventoryUserSub, []string{"INVENTORY"}, []string{"products:read", "products:write"})
}

func TestStockAdjustmentAuthor_IsTheVerifiedSubNotTheBodyAdjustedBy(t *testing.T) {
	api := newAuthorAPI(t)
	p := api.stockedProduct(10)
	const key = "author-key-0001"

	r := api.adjustAs(adjustmentAuthorInventoryToken(), p.ID, key, map[string]any{
		"delta": -3, "reason": "Damaged", "adjustedBy": forgedAuthorSub,
	})

	if r.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", r.status, r.body)
	}
	var created adjustmentJSON
	r.decode(t, &created)
	if created.AdjustedBy != inventoryUserSub {
		t.Errorf("response adjustedBy %q, want the verified sub %q", created.AdjustedBy, inventoryUserSub)
	}
	stored := api.storedAdjustment(p.ID, key)
	if stored.AdjustedBy != inventoryUserSub {
		t.Errorf("stored author %q, want the verified sub %q (the forged %q must be ignored)", stored.AdjustedBy, inventoryUserSub, forgedAuthorSub)
	}
	if stored.ID != created.AdjustmentID || stored.Delta != -3 || stored.Reason != "Damaged" {
		t.Errorf("stored adjustment %+v does not match the response %+v", stored, created)
	}
	if got := api.stock(p.ID); got != 7 {
		t.Errorf("stock %d, want 7", got)
	}
}

func TestStockAdjustmentAuthor_EachCallerIsRecordedUnderTheirOwnSub(t *testing.T) {
	api := newAuthorAPI(t)
	p := api.stockedProduct(10)
	admin := tokenWithClaims(testSub, []string{"ADMIN"}, []string{"products:read", "products:write"})

	byInventory := api.adjustAs(adjustmentAuthorInventoryToken(), p.ID, "author-key-inv", map[string]any{"delta": 1, "reason": "Restock", "adjustedBy": testSub})
	byAdmin := api.adjustAs(admin, p.ID, "author-key-adm", map[string]any{"delta": 1, "reason": "Restock", "adjustedBy": inventoryUserSub})

	if byInventory.status != http.StatusCreated || byAdmin.status != http.StatusCreated {
		t.Fatalf("expected 201 and 201, got %d and %d", byInventory.status, byAdmin.status)
	}
	if got := api.storedAdjustment(p.ID, "author-key-inv").AdjustedBy; got != inventoryUserSub {
		t.Errorf("INVENTORY adjustment stored under %q, want %q", got, inventoryUserSub)
	}
	if got := api.storedAdjustment(p.ID, "author-key-adm").AdjustedBy; got != testSub {
		t.Errorf("ADMIN adjustment stored under %q, want %q", got, testSub)
	}
}

func TestStockAdjustmentAuthor_AnInvalidSignatureCarryingAnotherUsersSubIs401AndWritesNothing(t *testing.T) {
	claims := validClaims(forgedAuthorSub, []string{"INVENTORY"}, []string{"products:read", "products:write"})
	forgeries := []struct{ name, token string }{
		{"signed with another RSA key", signedWith(otherKey, map[string]any{"alg": "RS256", "typ": "JWT"}, claims)},
		{"tampered signature", flippedSignature(rs256Token(claims))},
		{"alg none", unsignedToken(claims, "")},
		{"payload edited to the other user's sub", tamperedPayload(adjustmentAuthorInventoryToken(), claims)},
		{"expired", rs256Token(claims.with("exp", time.Now().Add(-time.Hour).Unix()))},
	}

	api := newAuthorAPI(t)
	p := api.stockedProduct(10)
	const key = "author-key-forged"
	for _, f := range forgeries {
		r := api.adjustAs(f.token, p.ID, key, map[string]any{"delta": 5, "reason": "Restock", "adjustedBy": forgedAuthorSub})
		if r.status != http.StatusUnauthorized {
			t.Errorf("%s: answered %d, want 401: %s", f.name, r.status, r.body)
		}
	}

	if n := api.adjustments.attemptCount(); n != 0 {
		t.Errorf("the adjustment repository was called %d times, want 0", n)
	}
	if got := api.stock(p.ID); got != 10 {
		t.Errorf("stock %d, want 10 (unchanged)", got)
	}

	// The key is still free: the real INVENTORY user can use it, creating
	// the first and only adjustment, authored by that user.
	ok := api.adjustAs(adjustmentAuthorInventoryToken(), p.ID, key, map[string]any{"delta": 5, "reason": "Restock"})
	if ok.status != http.StatusCreated {
		t.Fatalf("the same key with a genuine token answered %d, want 201 (key must be unclaimed): %s", ok.status, ok.body)
	}
	if got := api.storedAdjustment(p.ID, key).AdjustedBy; got != inventoryUserSub {
		t.Errorf("stored author %q, want %q", got, inventoryUserSub)
	}
	if got := api.stock(p.ID); got != 15 {
		t.Errorf("stock %d, want 15", got)
	}
}
