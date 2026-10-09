package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/code-corhuila/synkro-products-api/internal/adapter/out/persistence"
	"github.com/code-corhuila/synkro-products-api/internal/application/usecase"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// A JWT-shaped token: requireAuth only checks the shape. Its sub is a UUID
// because the stock-adjustment endpoint records it as adjustedBy.
const testSub = "0192a000-0000-7000-8000-0000000000a1"

var testToken = tokenWithSub(testSub)

func tokenWithSub(sub string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(`{"sub":"`+sub+`"}`)) + ".c2ln"
}

type testAPI struct {
	t   *testing.T
	srv *httptest.Server
	mem *persistence.Memory
	n   int
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	mem := persistence.NewMemory()
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	router := NewRouter(
		usecase.NewProductService(mem.Products(), mem.Categories(), newID),
		usecase.NewCategoryService(mem.Categories(), newID),
		usecase.NewStockService(mem.StockAdjustments(), mem.StockReservations(), newID),
		usecase.NewStockAlertService(mem.StockAlerts(), newID, time.Now),
	)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &testAPI{t: t, srv: srv, mem: mem}
}

type apiResponse struct {
	status int
	header http.Header
	body   []byte
}

func (r apiResponse) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decoding %q: %v", r.body, err)
	}
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"details"`
	TraceID string `json:"traceId"`
}

func (r apiResponse) errorBody(t *testing.T) errorBody {
	t.Helper()
	var e errorBody
	r.decode(t, &e)
	return e
}

func (a *testAPI) do(method, path string, body any, headers ...string) apiResponse {
	a.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, a.srv.URL+path, rd)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return apiResponse{status: resp.StatusCode, header: resp.Header, body: raw}
}

func (a *testAPI) key() string {
	a.n++
	return fmt.Sprintf("test-key-%04d", a.n)
}

type categoryJSON struct {
	CategoryID string `json:"categoryId"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
}

type productJSON struct {
	ProductID  string `json:"productId"`
	Name       string `json:"name"`
	PriceCents int64  `json:"priceCents"`
	Stock      int    `json:"stock"`
	CategoryID string `json:"categoryId"`
	Active     bool   `json:"active"`
}

type pageMeta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"totalPages"`
}

func (a *testAPI) createCategory(name string) categoryJSON {
	a.t.Helper()
	r := a.do("POST", "/api/v1/products/categories", map[string]any{"name": name}, "Idempotency-Key", a.key())
	if r.status != http.StatusCreated {
		a.t.Fatalf("creating category %q: %d %s", name, r.status, r.body)
	}
	var c categoryJSON
	r.decode(a.t, &c)
	return c
}

func (a *testAPI) createProduct(name string, price int64, categoryID string) productJSON {
	a.t.Helper()
	r := a.do("POST", "/api/v1/products", map[string]any{"name": name, "priceCents": price, "categoryId": categoryID}, "Idempotency-Key", a.key())
	if r.status != http.StatusCreated {
		a.t.Fatalf("creating product %q: %d %s", name, r.status, r.body)
	}
	var p productJSON
	r.decode(a.t, &p)
	return p
}

// ─── Products ──────────────────────────────────────────────────────────

func TestCreateProduct_Returns201WithLocationAndStockZero(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")

	r := api.do("POST", "/api/v1/products", map[string]any{"name": "Mouse", "priceCents": 4599_00, "categoryId": cat.CategoryID}, "Idempotency-Key", "create-mouse-1")
	if r.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", r.status, r.body)
	}
	var p productJSON
	r.decode(t, &p)
	if p.ProductID == "" || p.Name != "Mouse" || p.PriceCents != 4599_00 || p.Stock != 0 || p.CategoryID != cat.CategoryID || !p.Active {
		t.Errorf("unexpected body: %+v", p)
	}
	if loc := r.header.Get("Location"); loc != "/api/v1/products/"+p.ProductID {
		t.Errorf("unexpected Location %q", loc)
	}
	if r.header.Get("X-Correlation-Id") == "" {
		t.Error("expected X-Correlation-Id on the response")
	}
}

func TestCreateProduct_RepeatedKeyReturns200WithTheSameProduct(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	body := map[string]any{"name": "Mouse", "priceCents": 100, "categoryId": cat.CategoryID}

	first := api.do("POST", "/api/v1/products", body, "Idempotency-Key", "same-key-123")
	again := api.do("POST", "/api/v1/products", body, "Idempotency-Key", "same-key-123")
	if first.status != http.StatusCreated || again.status != http.StatusOK {
		t.Fatalf("expected 201 then 200, got %d then %d", first.status, again.status)
	}
	var p1, p2 productJSON
	first.decode(t, &p1)
	again.decode(t, &p2)
	if p1 != p2 {
		t.Errorf("replay returned %+v, want %+v", p2, p1)
	}
	if again.header.Get("Location") != "" {
		t.Error("a replay is not a creation: no Location expected")
	}

	var list struct{ Meta pageMeta }
	api.do("GET", "/api/v1/products", nil).decode(t, &list)
	if list.Meta.Total != 1 {
		t.Errorf("expected exactly 1 product, got %d", list.Meta.Total)
	}
}

// The first of the two 404-vs-422 distinctions: a bad category on a
// product is NOT_FOUND (404), never BUSINESS_RULE_VIOLATION.
func TestCreateProduct_InactiveOrUnknownCategoryAnswers404(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Old Stuff")
	if r := api.do("DELETE", "/api/v1/products/categories/"+cat.CategoryID, nil); r.status != http.StatusOK {
		t.Fatalf("setup: deactivating category: %d %s", r.status, r.body)
	}

	for name, catID := range map[string]string{"inactive": cat.CategoryID, "unknown": uuid.NewString()} {
		r := api.do("POST", "/api/v1/products", map[string]any{"name": "Mouse", "priceCents": 100, "categoryId": catID}, "Idempotency-Key", api.key())
		if r.status != http.StatusNotFound {
			t.Errorf("%s category: expected 404, got %d: %s", name, r.status, r.body)
			continue
		}
		if e := r.errorBody(t); e.Error != "NOT_FOUND" {
			t.Errorf("%s category: expected NOT_FOUND, got %q", name, e.Error)
		}
	}
}

func TestCreateProduct_MissingIdempotencyKeyAnswers400(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	body := map[string]any{"name": "Mouse", "priceCents": 100, "categoryId": cat.CategoryID}

	for _, key := range []string{"", "short", strings.Repeat("k", 129)} {
		var r apiResponse
		if key == "" {
			r = api.do("POST", "/api/v1/products", body)
		} else {
			r = api.do("POST", "/api/v1/products", body, "Idempotency-Key", key)
		}
		if r.status != http.StatusBadRequest {
			t.Errorf("key %q: expected 400, got %d", key, r.status)
			continue
		}
		e := r.errorBody(t)
		if e.Error != "VALIDATION_ERROR" || len(e.Details) != 1 || e.Details[0].Field != "Idempotency-Key" {
			t.Errorf("key %q: unexpected error body %+v", key, e)
		}
	}
}

func TestCreateProduct_OneDetailPerInvalidField(t *testing.T) {
	api := newTestAPI(t)
	r := api.do("POST", "/api/v1/products", map[string]any{"name": "", "priceCents": 0, "categoryId": "not-a-uuid"},
		"Idempotency-Key", api.key(), "X-Correlation-Id", "corr-abc-123")
	if r.status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", r.status, r.body)
	}
	e := r.errorBody(t)
	if e.Error != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %q", e.Error)
	}
	fields := map[string]bool{}
	for _, d := range e.Details {
		fields[d.Field] = true
	}
	if len(e.Details) != 3 || !fields["name"] || !fields["priceCents"] || !fields["categoryId"] {
		t.Errorf("expected one detail each for name, priceCents, categoryId; got %+v", e.Details)
	}
	if e.TraceID != "corr-abc-123" || r.header.Get("X-Correlation-Id") != "corr-abc-123" {
		t.Errorf("expected the caller's correlation id to be reused, got traceId=%q header=%q", e.TraceID, r.header.Get("X-Correlation-Id"))
	}
}

func TestCreateProduct_MissingFieldsAndOverlongNameAre400(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")

	r := api.do("POST", "/api/v1/products", map[string]any{}, "Idempotency-Key", api.key())
	if e := r.errorBody(t); r.status != http.StatusBadRequest || len(e.Details) != 3 {
		t.Errorf("empty body: expected 400 with 3 details, got %d %+v", r.status, e)
	}

	r = api.do("POST", "/api/v1/products", map[string]any{"name": strings.Repeat("x", 151), "priceCents": 1, "categoryId": cat.CategoryID}, "Idempotency-Key", api.key())
	if e := r.errorBody(t); r.status != http.StatusBadRequest || len(e.Details) != 1 || e.Details[0].Field != "name" {
		t.Errorf("151-char name: expected 400 on name, got %d %+v", r.status, e)
	}
}

func TestCreateProduct_MalformedJSONAnswers400WithTheEnvelope(t *testing.T) {
	api := newTestAPI(t)
	for _, body := range []string{"{not json", `{"name": "x", "priceCents": "lots", "categoryId": "x"}`} {
		r := api.do("POST", "/api/v1/products", body, "Idempotency-Key", api.key())
		if r.status != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", body, r.status)
			continue
		}
		if e := r.errorBody(t); e.Error != "VALIDATION_ERROR" || e.TraceID == "" {
			t.Errorf("%s: unexpected envelope %+v", body, e)
		}
	}
}

func TestGetProduct_Returns200AndUnknownIs404(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	p := api.createProduct("Mouse", 100, cat.CategoryID)

	r := api.do("GET", "/api/v1/products/"+p.ProductID, nil)
	var got productJSON
	r.decode(t, &got)
	if r.status != http.StatusOK || got != p {
		t.Errorf("expected 200 with %+v, got %d %+v", p, r.status, got)
	}

	for _, id := range []string{uuid.NewString(), "not-a-uuid"} {
		r = api.do("GET", "/api/v1/products/"+id, nil)
		if e := r.errorBody(t); r.status != http.StatusNotFound || e.Error != "NOT_FOUND" {
			t.Errorf("id %s: expected 404 NOT_FOUND, got %d %+v", id, r.status, e)
		}
	}
}

func TestUpdateProduct_Returns200AndNeverChangesStock(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	other := api.createCategory("Audio")
	p := api.createProduct("Mouse", 100, cat.CategoryID)
	api.mem.SeedProduct(model.Product{ID: p.ProductID, Name: p.Name, PriceCents: p.PriceCents, Stock: 12, CategoryID: p.CategoryID, Active: true})

	r := api.do("PUT", "/api/v1/products/"+p.ProductID, map[string]any{"name": "Headset", "priceCents": 250, "categoryId": other.CategoryID, "stock": 999})
	if r.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.status, r.body)
	}
	var got productJSON
	r.decode(t, &got)
	want := productJSON{ProductID: p.ProductID, Name: "Headset", PriceCents: 250, Stock: 12, CategoryID: other.CategoryID, Active: true}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUpdateProduct_InactiveCategoryAnswers404(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	old := api.createCategory("Old Stuff")
	api.do("DELETE", "/api/v1/products/categories/"+old.CategoryID, nil)
	p := api.createProduct("Mouse", 100, cat.CategoryID)

	r := api.do("PUT", "/api/v1/products/"+p.ProductID, map[string]any{"name": "Mouse", "priceCents": 100, "categoryId": old.CategoryID})
	if e := r.errorBody(t); r.status != http.StatusNotFound || e.Error != "NOT_FOUND" {
		t.Errorf("expected 404 NOT_FOUND, got %d %+v", r.status, e)
	}
}

func TestUpdateProduct_UnknownProductIs404AndBadBodyIs400(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	p := api.createProduct("Mouse", 100, cat.CategoryID)

	r := api.do("PUT", "/api/v1/products/"+uuid.NewString(), map[string]any{"name": "X", "priceCents": 1, "categoryId": cat.CategoryID})
	if r.status != http.StatusNotFound {
		t.Errorf("unknown product: expected 404, got %d", r.status)
	}
	r = api.do("PUT", "/api/v1/products/"+p.ProductID, map[string]any{"name": "X", "priceCents": -1, "categoryId": cat.CategoryID})
	if e := r.errorBody(t); r.status != http.StatusBadRequest || len(e.Details) != 1 || e.Details[0].Field != "priceCents" {
		t.Errorf("negative price: expected 400 on priceCents, got %d %+v", r.status, e)
	}
}

func TestDeactivateProduct_Returns200WithTheBodyEvenWhenAlreadyInactive(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	p := api.createProduct("Mouse", 100, cat.CategoryID)

	for i := 1; i <= 2; i++ {
		r := api.do("DELETE", "/api/v1/products/"+p.ProductID, nil)
		if r.status != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d: %s", i, r.status, r.body)
		}
		var got productJSON
		r.decode(t, &got)
		if got.ProductID != p.ProductID || got.Active {
			t.Errorf("call %d: expected the inactive product, got %+v", i, got)
		}
	}
	if r := api.do("DELETE", "/api/v1/products/"+uuid.NewString(), nil); r.status != http.StatusNotFound {
		t.Errorf("unknown product: expected 404, got %d", r.status)
	}
}

func TestListProducts_FiltersPaginatesAndOrdersNewestFirst(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	audio := api.createCategory("Audio")
	api.createProduct("Logitech Mouse", 100, cat.CategoryID)
	pad := api.createProduct("Mouse Pad", 100, cat.CategoryID)
	headset := api.createProduct("Headset", 100, audio.CategoryID)
	api.mem.SeedProduct(model.Product{ID: headset.ProductID, Name: headset.Name, PriceCents: 100, Stock: 50, CategoryID: audio.CategoryID, Active: true})
	api.do("DELETE", "/api/v1/products/"+pad.ProductID, nil)

	type listBody struct {
		Data []productJSON `json:"data"`
		Meta pageMeta      `json:"meta"`
	}
	ids := func(l listBody) []string {
		var out []string
		for _, p := range l.Data {
			out = append(out, p.Name)
		}
		return out
	}
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"Headset", "Mouse Pad", "Logitech Mouse"}},
		{"?name=MOUSE", []string{"Mouse Pad", "Logitech Mouse"}},
		{"?categoryId=" + audio.CategoryID, []string{"Headset"}},
		{"?active=false", []string{"Mouse Pad"}},
		{"?stockAtMost=0", []string{"Mouse Pad", "Logitech Mouse"}},
		{"?limit=1&page=2", []string{"Mouse Pad"}},
	}
	for _, c := range cases {
		r := api.do("GET", "/api/v1/products"+c.query, nil)
		if r.status != http.StatusOK {
			t.Errorf("%s: expected 200, got %d: %s", c.query, r.status, r.body)
			continue
		}
		var l listBody
		r.decode(t, &l)
		if fmt.Sprint(ids(l)) != fmt.Sprint(c.want) {
			t.Errorf("%s: got %v, want %v", c.query, ids(l), c.want)
		}
	}

	var l listBody
	api.do("GET", "/api/v1/products?limit=2", nil).decode(t, &l)
	if l.Meta != (pageMeta{Page: 1, Limit: 2, Total: 3, TotalPages: 2}) {
		t.Errorf("unexpected meta %+v", l.Meta)
	}
}

func TestListProducts_EmptyListIsAnArrayNotNull(t *testing.T) {
	api := newTestAPI(t)
	r := api.do("GET", "/api/v1/products", nil)
	if !strings.Contains(string(r.body), `"data":[]`) {
		t.Errorf("expected an empty data array, got %s", r.body)
	}
}

func TestListProducts_BadQueryParametersAnswer400(t *testing.T) {
	api := newTestAPI(t)
	for _, q := range []string{"?limit=101", "?limit=0", "?page=0", "?page=x", "?active=maybe", "?stockAtMost=-1", "?categoryId=nope", "?color=red"} {
		r := api.do("GET", "/api/v1/products"+q, nil)
		if e := r.errorBody(t); r.status != http.StatusBadRequest || e.Error != "VALIDATION_ERROR" {
			t.Errorf("%s: expected 400 VALIDATION_ERROR, got %d %+v", q, r.status, e)
		}
	}
}

// ─── Categories ────────────────────────────────────────────────────────

func TestCreateCategory_Returns201WithLocationThen200OnReplay(t *testing.T) {
	api := newTestAPI(t)
	first := api.do("POST", "/api/v1/products/categories", map[string]any{"name": "Peripherals"}, "Idempotency-Key", "cat-key-0001")
	if first.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", first.status, first.body)
	}
	var c categoryJSON
	first.decode(t, &c)
	if c.CategoryID == "" || c.Name != "Peripherals" || !c.Active {
		t.Errorf("unexpected body %+v", c)
	}
	if loc := first.header.Get("Location"); loc != "/api/v1/products/categories/"+c.CategoryID {
		t.Errorf("unexpected Location %q", loc)
	}

	// The replay carries a name that is now taken — by this very
	// category. It must still be a 200 replay, not a 422.
	again := api.do("POST", "/api/v1/products/categories", map[string]any{"name": "Peripherals"}, "Idempotency-Key", "cat-key-0001")
	var c2 categoryJSON
	again.decode(t, &c2)
	if again.status != http.StatusOK || c2 != c {
		t.Errorf("replay: expected 200 with %+v, got %d %+v", c, again.status, c2)
	}
}

// The second of the two 404-vs-422 distinctions: a name collision is a
// BUSINESS_RULE_VIOLATION (422), never NOT_FOUND.
func TestCreateCategory_DuplicateActiveNameAnswers422(t *testing.T) {
	api := newTestAPI(t)
	api.createCategory("Peripherals")

	r := api.do("POST", "/api/v1/products/categories", map[string]any{"name": "Peripherals"}, "Idempotency-Key", api.key())
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", r.status, r.body)
	}
	if e := r.errorBody(t); e.Error != "BUSINESS_RULE_VIOLATION" {
		t.Errorf("expected BUSINESS_RULE_VIOLATION, got %q", e.Error)
	}
}

func TestCreateCategory_NameOfADeactivatedCategoryCanBeReused(t *testing.T) {
	api := newTestAPI(t)
	old := api.createCategory("Peripherals")
	api.do("DELETE", "/api/v1/products/categories/"+old.CategoryID, nil)

	r := api.do("POST", "/api/v1/products/categories", map[string]any{"name": "Peripherals"}, "Idempotency-Key", api.key())
	if r.status != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", r.status, r.body)
	}
}

func TestCreateCategory_KeyAlreadyUsedForAProductAnswers422(t *testing.T) {
	api := newTestAPI(t)
	cat := api.createCategory("Peripherals")
	api.do("POST", "/api/v1/products", map[string]any{"name": "Mouse", "priceCents": 1, "categoryId": cat.CategoryID}, "Idempotency-Key", "shared-key-01")

	r := api.do("POST", "/api/v1/products/categories", map[string]any{"name": "Audio"}, "Idempotency-Key", "shared-key-01")
	if e := r.errorBody(t); r.status != http.StatusUnprocessableEntity || e.Error != "BUSINESS_RULE_VIOLATION" {
		t.Errorf("expected 422 BUSINESS_RULE_VIOLATION, got %d %+v", r.status, e)
	}
}

func TestCreateCategory_InvalidNameAnswers400(t *testing.T) {
	api := newTestAPI(t)
	for _, body := range []any{map[string]any{}, map[string]any{"name": ""}, map[string]any{"name": strings.Repeat("x", 101)}} {
		r := api.do("POST", "/api/v1/products/categories", body, "Idempotency-Key", api.key())
		if e := r.errorBody(t); r.status != http.StatusBadRequest || len(e.Details) != 1 || e.Details[0].Field != "name" {
			t.Errorf("%v: expected 400 on name, got %d %+v", body, r.status, e)
		}
	}
}

func TestListCategories_IsNotSwallowedByTheProductIDRoute(t *testing.T) {
	api := newTestAPI(t)
	a := api.createCategory("Peripherals")
	b := api.createCategory("Audio")
	api.do("DELETE", "/api/v1/products/categories/"+a.CategoryID, nil)

	var l struct {
		Data []categoryJSON `json:"data"`
		Meta pageMeta       `json:"meta"`
	}
	r := api.do("GET", "/api/v1/products/categories", nil)
	if r.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.status, r.body)
	}
	r.decode(t, &l)
	if len(l.Data) != 2 || l.Data[0].CategoryID != b.CategoryID || l.Meta.Total != 2 {
		t.Errorf("expected both categories newest first, got %+v", l)
	}

	api.do("GET", "/api/v1/products/categories?active=true", nil).decode(t, &l)
	if len(l.Data) != 1 || l.Data[0].CategoryID != b.CategoryID {
		t.Errorf("active=true: expected only %s, got %+v", b.Name, l.Data)
	}

	if r := api.do("GET", "/api/v1/products/categories?limit=500", nil); r.status != http.StatusBadRequest {
		t.Errorf("limit=500: expected 400, got %d", r.status)
	}
}

func TestRenameCategory_Returns200AndAllowsItsOwnName(t *testing.T) {
	api := newTestAPI(t)
	c := api.createCategory("Peripherals")

	for _, name := range []string{"Peripherals", "Accessories"} {
		r := api.do("PUT", "/api/v1/products/categories/"+c.CategoryID, map[string]any{"name": name})
		var got categoryJSON
		r.decode(t, &got)
		if r.status != http.StatusOK || got.Name != name || got.CategoryID != c.CategoryID {
			t.Errorf("rename to %q: expected 200, got %d %+v", name, r.status, got)
		}
	}
}

func TestRenameCategory_ToAnotherActiveNameAnswers422AndUnknownIs404(t *testing.T) {
	api := newTestAPI(t)
	api.createCategory("Peripherals")
	audio := api.createCategory("Audio")

	r := api.do("PUT", "/api/v1/products/categories/"+audio.CategoryID, map[string]any{"name": "Peripherals"})
	if e := r.errorBody(t); r.status != http.StatusUnprocessableEntity || e.Error != "BUSINESS_RULE_VIOLATION" {
		t.Errorf("expected 422 BUSINESS_RULE_VIOLATION, got %d %+v", r.status, e)
	}
	r = api.do("PUT", "/api/v1/products/categories/"+uuid.NewString(), map[string]any{"name": "X"})
	if e := r.errorBody(t); r.status != http.StatusNotFound || e.Error != "NOT_FOUND" {
		t.Errorf("expected 404 NOT_FOUND, got %d %+v", r.status, e)
	}
}

func TestDeactivateCategory_WithActiveProductsAnswers422(t *testing.T) {
	api := newTestAPI(t)
	c := api.createCategory("Peripherals")
	p := api.createProduct("Mouse", 100, c.CategoryID)

	r := api.do("DELETE", "/api/v1/products/categories/"+c.CategoryID, nil)
	if e := r.errorBody(t); r.status != http.StatusUnprocessableEntity || e.Error != "BUSINESS_RULE_VIOLATION" {
		t.Fatalf("expected 422 BUSINESS_RULE_VIOLATION, got %d %+v", r.status, e)
	}

	// once the product is inactive, the category can go — twice
	api.do("DELETE", "/api/v1/products/"+p.ProductID, nil)
	for i := 1; i <= 2; i++ {
		r = api.do("DELETE", "/api/v1/products/categories/"+c.CategoryID, nil)
		var got categoryJSON
		r.decode(t, &got)
		if r.status != http.StatusOK || got.Active || got.CategoryID != c.CategoryID {
			t.Errorf("call %d: expected 200 with the inactive category, got %d %+v", i, r.status, got)
		}
	}
}

// ─── Envelope ──────────────────────────────────────────────────────────

func TestUnknownRouteUnderTheAPIAnswers404WithTheEnvelope(t *testing.T) {
	api := newTestAPI(t)
	r := api.do("GET", "/api/v1/nothing-here", nil, "X-Correlation-Id", "corr-404")
	e := r.errorBody(t)
	if r.status != http.StatusNotFound || e.Error != "NOT_FOUND" || e.TraceID != "corr-404" {
		t.Errorf("expected 404 NOT_FOUND envelope with traceId corr-404, got %d %+v", r.status, e)
	}
}
