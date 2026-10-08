package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type adjustmentJSON struct {
	AdjustmentID string `json:"adjustmentId"`
	ProductID    string `json:"productId"`
	Delta        int    `json:"delta"`
	Reason       string `json:"reason"`
	AdjustedBy   string `json:"adjustedBy"`
	AdjustedAt   string `json:"adjustedAt"`
	CurrentStock int    `json:"currentStock"`
}

type reservationLineJSON struct {
	LineID         string `json:"lineId"`
	ProductID      string `json:"productId"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unitPriceCents"`
}

type reservationJSON struct {
	ReservationID string                `json:"reservationId"`
	Status        string                `json:"status"`
	CreatedAt     string                `json:"createdAt"`
	ReleasedAt    string                `json:"releasedAt"`
	Lines         []reservationLineJSON `json:"lines"`
}

// stocked creates a product through the API and sets its stock directly
// in the store (stock has no other write path than the endpoints under
// test).
func (a *testAPI) stocked(name string, price int64, stock int) productJSON {
	a.t.Helper()
	p := a.createProduct(name, price, a.createCategory("Cat "+name).CategoryID)
	a.mem.SeedProduct(model.Product{ID: p.ProductID, Name: p.Name, PriceCents: p.PriceCents, Stock: stock, CategoryID: p.CategoryID, Active: true})
	return p
}

func (a *testAPI) stockOf(productID string) int {
	a.t.Helper()
	r := a.do("GET", "/api/v1/products/"+productID, nil)
	var p productJSON
	r.decode(a.t, &p)
	return p.Stock
}

func (a *testAPI) adjust(productID string, body any, key string) apiResponse {
	return a.do("POST", "/api/v1/products/"+productID+"/stock-adjustments", body, "Idempotency-Key", key)
}

func (a *testAPI) reserve(lines []map[string]any, key string) apiResponse {
	return a.do("POST", "/api/v1/stock-reservations", map[string]any{"lines": lines}, "Idempotency-Key", key)
}

func ln(productID string, qty int) map[string]any {
	return map[string]any{"productId": productID, "quantity": qty}
}

// ─── POST /products/{id}/stock-adjustments ─────────────────────────────

func TestCreateStockAdjustment_Returns201WithLocationAndTheContractShape(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)

	// adjustedBy in the body must be ignored: it comes from the token.
	r := api.adjust(p.ProductID, map[string]any{"delta": -3, "reason": "Damaged", "adjustedBy": "someone-else"}, "adjust-key-1")
	if r.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", r.status, r.body)
	}
	var a adjustmentJSON
	r.decode(t, &a)
	if a.AdjustmentID == "" || a.ProductID != p.ProductID || a.Delta != -3 || a.Reason != "Damaged" || a.CurrentStock != 7 || a.AdjustedAt == "" {
		t.Errorf("unexpected body: %+v", a)
	}
	if a.AdjustedBy != testSub {
		t.Errorf("adjustedBy %q, want the token's sub %q", a.AdjustedBy, testSub)
	}
	if loc := r.header.Get("Location"); !strings.HasPrefix(loc, "/api/v1/products/"+p.ProductID+"/stock-adjustments/") || !strings.HasSuffix(loc, a.AdjustmentID) {
		t.Errorf("unexpected Location %q", loc)
	}
	if got := api.stockOf(p.ProductID); got != 7 {
		t.Errorf("product stock %d, want 7", got)
	}
}

func TestCreateStockAdjustment_RepeatedKeyReturns200AndDoesNotApplyTwice(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	body := map[string]any{"delta": 5, "reason": "Restock"}

	first := api.adjust(p.ProductID, body, "same-key-123")
	again := api.adjust(p.ProductID, body, "same-key-123")
	if first.status != http.StatusCreated || again.status != http.StatusOK {
		t.Fatalf("expected 201 then 200, got %d then %d: %s", first.status, again.status, again.body)
	}
	var a1, a2 adjustmentJSON
	first.decode(t, &a1)
	again.decode(t, &a2)
	if a1.AdjustmentID != a2.AdjustmentID {
		t.Errorf("replay returned adjustment %s, want %s", a2.AdjustmentID, a1.AdjustmentID)
	}
	if again.header.Get("Location") != "" {
		t.Error("a replay is not a creation: no Location expected")
	}
	if got := api.stockOf(p.ProductID); got != 15 {
		t.Errorf("stock %d, want 15 (applied once)", got)
	}
}

func TestCreateStockAdjustment_BelowZeroIs422AndChangesNothing(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)

	r := api.adjust(p.ProductID, map[string]any{"delta": -3, "reason": "Lost"}, "adjust-key-1")
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", r.status, r.body)
	}
	if e := r.errorBody(t); e.Error != "BUSINESS_RULE_VIOLATION" || e.TraceID == "" {
		t.Errorf("unexpected error body: %+v", e)
	}
	if got := api.stockOf(p.ProductID); got != 2 {
		t.Errorf("stock %d, want 2", got)
	}
}

func TestCreateStockAdjustment_InvalidBodyIs400(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	long := strings.Repeat("x", 256)

	tests := []struct {
		name  string
		body  any
		field string
	}{
		{"zero delta", map[string]any{"delta": 0, "reason": "x"}, "delta"},
		{"missing delta", map[string]any{"reason": "x"}, "delta"},
		{"delta not an integer", map[string]any{"delta": "three", "reason": "x"}, "delta"},
		{"missing reason", map[string]any{"delta": 1}, "reason"},
		{"empty reason", map[string]any{"delta": 1, "reason": ""}, "reason"},
		{"blank reason", map[string]any{"delta": 1, "reason": "   "}, "reason"},
		{"reason over 255", map[string]any{"delta": 1, "reason": long}, "reason"},
		{"not json", "{nope", "body"},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := api.adjust(p.ProductID, tc.body, "bad-body-key-"+string(rune('a'+i)))
			if r.status != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", r.status, r.body)
			}
			e := r.errorBody(t)
			if e.Error != "VALIDATION_ERROR" || len(e.Details) == 0 || e.Details[0].Field != tc.field {
				t.Errorf("unexpected error body: %+v", e)
			}
		})
	}
	if got := api.stockOf(p.ProductID); got != 10 {
		t.Errorf("stock %d, want 10: no invalid request may change it", got)
	}
}

func TestCreateStockAdjustment_RequiresAnIdempotencyKey(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	r := api.do("POST", "/api/v1/products/"+p.ProductID+"/stock-adjustments", map[string]any{"delta": 1, "reason": "x"})
	if r.status != http.StatusBadRequest || r.errorBody(t).Details[0].Field != "Idempotency-Key" {
		t.Fatalf("expected 400 naming Idempotency-Key, got %d: %s", r.status, r.body)
	}
}

func TestCreateStockAdjustment_UnknownOrMalformedProductIs404(t *testing.T) {
	api := newTestAPI(t)
	for _, id := range []string{"0192a000-0000-7000-8000-00000000dead", "not-a-uuid"} {
		r := api.adjust(id, map[string]any{"delta": 1, "reason": "x"}, "adjust-key-1")
		if r.status != http.StatusNotFound || r.errorBody(t).Error != "NOT_FOUND" {
			t.Errorf("%s: expected 404 NOT_FOUND, got %d: %s", id, r.status, r.body)
		}
	}
}

func TestCreateStockAdjustment_TokenWithoutAUUIDSubIs401(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	r := api.do("POST", "/api/v1/products/"+p.ProductID+"/stock-adjustments", map[string]any{"delta": 1, "reason": "x"},
		"Idempotency-Key", "adjust-key-1", "Authorization", "Bearer "+tokenWithSub("not-a-uuid"))
	if r.status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", r.status, r.body)
	}
	if got := api.stockOf(p.ProductID); got != 10 {
		t.Errorf("stock %d, want 10", got)
	}
}

func TestCreateStockAdjustment_Returns401WithNoToken(t *testing.T) {
	api := newTestAPI(t)
	resp, err := http.Post(api.srv.URL+"/api/v1/products/0192a000-0000-7000-8000-00000000dead/stock-adjustments", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ─── POST /stock-reservations ──────────────────────────────────────────

func TestCreateStockReservation_Returns201WithLocationAndFrozenPrices(t *testing.T) {
	api := newTestAPI(t)
	p1 := api.stocked("Mouse", 1500, 10)
	p2 := api.stocked("Keyboard", 700, 5)

	r := api.reserve([]map[string]any{ln(p1.ProductID, 3), ln(p2.ProductID, 5)}, "reserve-key-1")
	if r.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", r.status, r.body)
	}
	var res reservationJSON
	r.decode(t, &res)
	if res.ReservationID == "" || res.Status != "RESERVED" || res.CreatedAt == "" || res.ReleasedAt != "" || len(res.Lines) != 2 {
		t.Fatalf("unexpected body: %+v", res)
	}
	if l := res.Lines[0]; l.LineID == "" || l.ProductID != p1.ProductID || l.Quantity != 3 || l.UnitPriceCents != 1500 {
		t.Errorf("line 0: %+v", l)
	}
	if l := res.Lines[1]; l.ProductID != p2.ProductID || l.Quantity != 5 || l.UnitPriceCents != 700 {
		t.Errorf("line 1: %+v", l)
	}
	if loc := r.header.Get("Location"); loc != "/api/v1/stock-reservations/"+res.ReservationID {
		t.Errorf("unexpected Location %q", loc)
	}
	if api.stockOf(p1.ProductID) != 7 || api.stockOf(p2.ProductID) != 0 {
		t.Errorf("stock p1=%d p2=%d, want 7 and 0", api.stockOf(p1.ProductID), api.stockOf(p2.ProductID))
	}
}

func TestCreateStockReservation_RepeatedKeyReturns200AndReservesOnce(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	lines := []map[string]any{ln(p.ProductID, 4)}

	first := api.reserve(lines, "same-key-123")
	again := api.reserve(lines, "same-key-123")
	if first.status != http.StatusCreated || again.status != http.StatusOK {
		t.Fatalf("expected 201 then 200, got %d then %d: %s", first.status, again.status, again.body)
	}
	var r1, r2 reservationJSON
	first.decode(t, &r1)
	again.decode(t, &r2)
	if r1.ReservationID != r2.ReservationID || len(r2.Lines) != 1 || r2.Lines[0].LineID != r1.Lines[0].LineID {
		t.Errorf("replay returned %+v, want %+v", r2, r1)
	}
	if again.header.Get("Location") != "" {
		t.Error("a replay is not a creation: no Location expected")
	}
	if got := api.stockOf(p.ProductID); got != 6 {
		t.Errorf("stock %d, want 6 (reserved once)", got)
	}
}

func TestCreateStockReservation_OneUnaffordableLineIs422AndNothingIsReserved(t *testing.T) {
	api := newTestAPI(t)
	ok := api.stocked("Mouse", 100, 10)
	short := api.stocked("Keyboard", 200, 3)

	r := api.reserve([]map[string]any{ln(ok.ProductID, 2), ln(short.ProductID, 9)}, "reserve-key-1")
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", r.status, r.body)
	}
	e := r.errorBody(t)
	if e.Error != "BUSINESS_RULE_VIOLATION" || len(e.Details) != 1 || e.Details[0].Field != "lines[1].quantity" || !strings.Contains(e.Details[0].Message, short.ProductID) {
		t.Errorf("unexpected error body: %+v", e)
	}
	if api.stockOf(ok.ProductID) != 10 || api.stockOf(short.ProductID) != 3 {
		t.Errorf("stock ok=%d short=%d, want 10 and 3: nothing may be reserved", api.stockOf(ok.ProductID), api.stockOf(short.ProductID))
	}
}

func TestCreateStockReservation_InactiveOrUnknownProductIs422(t *testing.T) {
	api := newTestAPI(t)
	ok := api.stocked("Mouse", 100, 10)
	off := api.stocked("Old", 100, 10)
	if r := api.do("DELETE", "/api/v1/products/"+off.ProductID, nil); r.status != http.StatusOK {
		t.Fatalf("deactivating: %d %s", r.status, r.body)
	}

	for name, bad := range map[string]string{"inactive": off.ProductID, "unknown": "0192a000-0000-7000-8000-00000000dead"} {
		r := api.reserve([]map[string]any{ln(ok.ProductID, 1), ln(bad, 1)}, "reserve-"+name)
		if r.status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: expected 422, got %d: %s", name, r.status, r.body)
		}
		e := r.errorBody(t)
		if e.Error != "BUSINESS_RULE_VIOLATION" || e.Details[0].Field != "lines[1].productId" {
			t.Errorf("%s: unexpected error body: %+v", name, e)
		}
	}
	if got := api.stockOf(ok.ProductID); got != 10 {
		t.Errorf("stock %d, want 10", got)
	}
}

func TestCreateStockReservation_InvalidBodyIs400(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)

	tests := []struct {
		name  string
		body  any
		field string
	}{
		{"missing lines", map[string]any{}, "lines"},
		{"empty lines", map[string]any{"lines": []any{}}, "lines"},
		{"zero quantity", map[string]any{"lines": []any{ln(p.ProductID, 0)}}, "lines[0].quantity"},
		{"negative quantity", map[string]any{"lines": []any{ln(p.ProductID, -1)}}, "lines[0].quantity"},
		{"missing quantity", map[string]any{"lines": []any{map[string]any{"productId": p.ProductID}}}, "lines[0].quantity"},
		{"bad productId", map[string]any{"lines": []any{ln("nope", 1)}}, "lines[0].productId"},
		{"missing productId", map[string]any{"lines": []any{map[string]any{"quantity": 1}}}, "lines[0].productId"},
		{"duplicate product", map[string]any{"lines": []any{ln(p.ProductID, 1), ln(p.ProductID, 2)}}, "lines[1].productId"},
		{"not json", "{nope", "body"},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := api.do("POST", "/api/v1/stock-reservations", tc.body, "Idempotency-Key", "bad-body-key-"+string(rune('a'+i)))
			if r.status != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", r.status, r.body)
			}
			e := r.errorBody(t)
			if e.Error != "VALIDATION_ERROR" || len(e.Details) == 0 || e.Details[0].Field != tc.field {
				t.Errorf("unexpected error body: %+v", e)
			}
		})
	}
	if got := api.stockOf(p.ProductID); got != 10 {
		t.Errorf("stock %d, want 10", got)
	}
}

func TestCreateStockReservation_RequiresAnIdempotencyKey(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	r := api.do("POST", "/api/v1/stock-reservations", map[string]any{"lines": []any{ln(p.ProductID, 1)}})
	if r.status != http.StatusBadRequest || r.errorBody(t).Details[0].Field != "Idempotency-Key" {
		t.Fatalf("expected 400 naming Idempotency-Key, got %d: %s", r.status, r.body)
	}
}

func TestCreateStockReservation_KeySpentOnAnotherResourceTypeIs422(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	if r := api.adjust(p.ProductID, map[string]any{"delta": 1, "reason": "x"}, "shared-key-1"); r.status != http.StatusCreated {
		t.Fatalf("adjustment: %d %s", r.status, r.body)
	}
	r := api.reserve([]map[string]any{ln(p.ProductID, 1)}, "shared-key-1")
	if r.status != http.StatusUnprocessableEntity || r.errorBody(t).Error != "BUSINESS_RULE_VIOLATION" {
		t.Fatalf("expected 422, got %d: %s", r.status, r.body)
	}
}

// ─── GET /stock-reservations/{id} ──────────────────────────────────────

func TestGetStockReservation_ReturnsItAndItsFrozenPriceSurvivesAPriceChange(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 10)
	var created reservationJSON
	api.reserve([]map[string]any{ln(p.ProductID, 2)}, "reserve-key-1").decode(t, &created)

	// Raise the price through the real endpoint.
	upd := api.do("PUT", "/api/v1/products/"+p.ProductID, map[string]any{"name": "Mouse", "priceCents": 999, "categoryId": p.CategoryID})
	if upd.status != http.StatusOK {
		t.Fatalf("updating the price: %d %s", upd.status, upd.body)
	}

	r := api.do("GET", "/api/v1/stock-reservations/"+created.ReservationID, nil)
	if r.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.status, r.body)
	}
	var got reservationJSON
	r.decode(t, &got)
	if got.ReservationID != created.ReservationID || got.Status != "RESERVED" || got.Lines[0].UnitPriceCents != 100 {
		t.Errorf("got %+v, want the original with unitPriceCents 100", got)
	}
}

func TestStockReservation_UnknownOrMalformedIDIs404(t *testing.T) {
	api := newTestAPI(t)
	for _, id := range []string{"0192a000-0000-7000-8000-00000000dead", "not-a-uuid"} {
		for _, req := range [][2]string{{"GET", "/api/v1/stock-reservations/" + id}, {"POST", "/api/v1/stock-reservations/" + id + "/release"}} {
			r := api.do(req[0], req[1], nil)
			if r.status != http.StatusNotFound || r.errorBody(t).Error != "NOT_FOUND" {
				t.Errorf("%s %s: expected 404 NOT_FOUND, got %d: %s", req[0], req[1], r.status, r.body)
			}
		}
	}
}

// ─── POST /stock-reservations/{id}/release ─────────────────────────────

func TestReleaseStockReservation_RestoresStockAndIsIdempotent(t *testing.T) {
	api := newTestAPI(t)
	p1 := api.stocked("Mouse", 100, 10)
	p2 := api.stocked("Keyboard", 200, 4)
	var created reservationJSON
	api.reserve([]map[string]any{ln(p1.ProductID, 3), ln(p2.ProductID, 4)}, "reserve-key-1").decode(t, &created)
	path := "/api/v1/stock-reservations/" + created.ReservationID + "/release"

	first := api.do("POST", path, nil)
	if first.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", first.status, first.body)
	}
	var rel reservationJSON
	first.decode(t, &rel)
	if rel.Status != "RELEASED" || rel.ReleasedAt == "" || len(rel.Lines) != 2 {
		t.Errorf("unexpected body: %+v", rel)
	}
	if api.stockOf(p1.ProductID) != 10 || api.stockOf(p2.ProductID) != 4 {
		t.Fatalf("after release p1=%d p2=%d, want 10 and 4", api.stockOf(p1.ProductID), api.stockOf(p2.ProductID))
	}

	second := api.do("POST", path, nil)
	if second.status != http.StatusOK {
		t.Fatalf("second release: expected 200, got %d: %s", second.status, second.body)
	}
	var again reservationJSON
	second.decode(t, &again)
	if again.Status != "RELEASED" || again.ReleasedAt != rel.ReleasedAt {
		t.Errorf("second release changed the resource: %+v vs %+v", again, rel)
	}
	if api.stockOf(p1.ProductID) != 10 || api.stockOf(p2.ProductID) != 4 {
		t.Errorf("a second release restored stock again: p1=%d p2=%d", api.stockOf(p1.ProductID), api.stockOf(p2.ProductID))
	}
}
