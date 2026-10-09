package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type stockAlertJSON struct {
	AlertID        string `json:"alertId"`
	ProductID      string `json:"productId"`
	Status         string `json:"status"`
	StockAtOpening int    `json:"stockAtOpening"`
	OpenedAt       string `json:"openedAt"`
	ResolvedAt     string `json:"resolvedAt"`
}

type alertPageJSON struct {
	Data []stockAlertJSON `json:"data"`
	Meta pageMeta         `json:"meta"`
}

// tokenWith builds a JWT-shaped token carrying the claims the alert
// endpoints authorize on (the signature is not verified, see requireAuth).
func tokenWith(roles, permissions []string) string {
	return tokenWithClaims(testSub, roles, permissions)
}

func tokenWithClaims(sub string, roles, permissions []string) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload, _ := json.Marshal(map[string]any{"sub": sub, "roles": roles, "permissions": permissions})
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + ".c2ln"
}

var (
	// synkro-worker's service token: products:read, stock-alerts:read, stock-alerts:write.
	workerToken      = tokenWith([]string{"SERVICE"}, []string{"products:read", "stock-alerts:read", "stock-alerts:write"})
	adminToken       = tokenWith([]string{"ADMIN"}, []string{"products:read", "products:write"})
	inventoryToken   = tokenWith([]string{"INVENTORY"}, []string{"products:read", "products:write"})
	salespersonToken = tokenWith([]string{"SALESPERSON"}, []string{"products:read"})
	// A service token that may only read alerts, as another service's would.
	readOnlyServiceToken = tokenWith([]string{"SERVICE"}, []string{"stock-alerts:read"})
)

func as(token string) []string { return []string{"Authorization", "Bearer " + token} }

func (a *testAPI) openAlert(token string, body any, key string) apiResponse {
	h := append(as(token), "Idempotency-Key", key)
	return a.do("POST", "/api/v1/stock-alerts", body, h...)
}

func alertBody(productID string, stock int) map[string]any {
	return map[string]any{"productId": productID, "stockAtOpening": stock}
}

// opened opens an alert as the worker and returns it.
func (a *testAPI) opened(productID string, stock int) stockAlertJSON {
	a.t.Helper()
	r := a.openAlert(workerToken, alertBody(productID, stock), a.key())
	if r.status != http.StatusCreated {
		a.t.Fatalf("opening an alert: %d %s", r.status, r.body)
	}
	var al stockAlertJSON
	r.decode(a.t, &al)
	return al
}

func (a *testAPI) resolveAlert(token, id string) apiResponse {
	return a.do("POST", "/api/v1/stock-alerts/"+id+"/resolve", nil, as(token)...)
}

func (a *testAPI) listAlerts(token, query string) apiResponse {
	return a.do("GET", "/api/v1/stock-alerts"+query, nil, as(token)...)
}

// ─── POST /stock-alerts ────────────────────────────────────────────────

func TestOpenStockAlert_Returns201WithLocationAndTheContractShape(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)

	r := api.openAlert(workerToken, alertBody(p.ProductID, 2), "low-stock:"+p.ProductID+":2026-10-09")

	if r.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", r.status, r.body)
	}
	var al stockAlertJSON
	r.decode(t, &al)
	if al.AlertID == "" || al.ProductID != p.ProductID || al.Status != "OPEN" || al.StockAtOpening != 2 || al.OpenedAt == "" {
		t.Errorf("unexpected body: %+v", al)
	}
	if strings.Contains(string(r.body), "resolvedAt") {
		t.Errorf("resolvedAt is present only when RESOLVED: %s", r.body)
	}
	if _, err := time.Parse(time.RFC3339, al.OpenedAt); err != nil {
		t.Errorf("openedAt %q is not a date-time: %v", al.OpenedAt, err)
	}
	// The contract defines no GET for a single alert, so, as for stock
	// adjustments, Location names the product the alert is about.
	if loc := r.header.Get("Location"); loc != "/api/v1/products/"+p.ProductID {
		t.Errorf("unexpected Location %q", loc)
	}
}

func TestOpenStockAlert_ASameKeyRetransmissionAnswers200WithTheSameAlert(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)
	key := "low-stock:" + p.ProductID + ":2026-10-09"
	first := api.openAlert(workerToken, alertBody(p.ProductID, 2), key)

	again := api.openAlert(workerToken, alertBody(p.ProductID, 2), key)

	if again.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", again.status, again.body)
	}
	var a, b stockAlertJSON
	first.decode(t, &a)
	again.decode(t, &b)
	if a != b {
		t.Errorf("replay changed the alert: %+v vs %+v", a, b)
	}
	if again.header.Get("Location") != "" {
		t.Errorf("a 200 creates nothing, so it has no Location, got %q", again.header.Get("Location"))
	}
}

// The worker runs daily, so its key changes every day. The product still
// gets a single OPEN alert: that is the database's rule, not the key's.
func TestOpenStockAlert_ADifferentKeyForAProductWithAnOpenAlertAnswers200WithThatAlert(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)
	first := api.openAlert(workerToken, alertBody(p.ProductID, 2), "low-stock:"+p.ProductID+":2026-10-08")

	next := api.openAlert(workerToken, alertBody(p.ProductID, 1), "low-stock:"+p.ProductID+":2026-10-09")

	if next.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", next.status, next.body)
	}
	var a, b stockAlertJSON
	first.decode(t, &a)
	next.decode(t, &b)
	if a != b {
		t.Errorf("expected the already-open alert %+v, got %+v", a, b)
	}
	var page alertPageJSON
	api.listAlerts(adminToken, "?status=OPEN").decode(t, &page)
	if page.Meta.Total != 1 {
		t.Errorf("expected a single OPEN alert, got %d", page.Meta.Total)
	}
}

func TestOpenStockAlert_AfterResolutionTheNextOneAnswers201AsANewAlert(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)
	first := api.opened(p.ProductID, 2)
	if r := api.resolveAlert(workerToken, first.AlertID); r.status != http.StatusOK {
		t.Fatalf("resolve: %d %s", r.status, r.body)
	}

	r := api.openAlert(workerToken, alertBody(p.ProductID, 1), api.key())

	if r.status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", r.status, r.body)
	}
	var next stockAlertJSON
	r.decode(t, &next)
	if next.AlertID == first.AlertID || next.Status != "OPEN" || next.StockAtOpening != 1 {
		t.Errorf("expected a new OPEN alert, got %+v", next)
	}
}

func TestOpenStockAlert_Returns403ToEveryCallerButTheWorkersToken(t *testing.T) {
	cases := map[string]string{
		"ADMIN":                   adminToken,
		"INVENTORY":               inventoryToken,
		"SALESPERSON":             salespersonToken,
		"service, read-only":      readOnlyServiceToken,
		"no roles or permissions": testToken,
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			api := newTestAPI(t)
			p := api.stocked("Mouse", 100, 2)

			r := api.openAlert(token, alertBody(p.ProductID, 2), api.key())

			if r.status != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", r.status, r.body)
			}
			if e := r.errorBody(t); e.Error != "FORBIDDEN" || e.TraceID == "" {
				t.Errorf("unexpected error envelope: %+v", e)
			}
			var page alertPageJSON
			api.listAlerts(adminToken, "").decode(t, &page)
			if page.Meta.Total != 0 {
				t.Errorf("a forbidden call must create nothing, found %d alerts", page.Meta.Total)
			}
		})
	}
}

func TestOpenStockAlert_ChecksThePermissionBeforeValidatingTheRequest(t *testing.T) {
	api := newTestAPI(t)

	r := api.do("POST", "/api/v1/stock-alerts", "not json", as(adminToken)...)

	if r.status != http.StatusForbidden {
		t.Errorf("expected 403 before any validation, got %d: %s", r.status, r.body)
	}
}

func TestOpenStockAlert_Returns404ForAnUnknownProduct(t *testing.T) {
	api := newTestAPI(t)

	r := api.openAlert(workerToken, alertBody("0192a000-0000-7000-8000-00000000dead", 1), api.key())

	if r.status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", r.status, r.body)
	}
	if e := r.errorBody(t); e.Error != "NOT_FOUND" {
		t.Errorf("unexpected error: %+v", e)
	}
}

func TestOpenStockAlert_Returns400ForAnInvalidRequest(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)
	cases := map[string]struct {
		body  any
		field string
	}{
		"missing productId":       {map[string]any{"stockAtOpening": 1}, "productId"},
		"productId not a UUID":    {alertBody("nope", 1), "productId"},
		"missing stockAtOpening":  {map[string]any{"productId": p.ProductID}, "stockAtOpening"},
		"negative stockAtOpening": {alertBody(p.ProductID, -1), "stockAtOpening"},
		"stockAtOpening too big":  {alertBody(p.ProductID, 1<<31), "stockAtOpening"},
		"stockAtOpening a string": {map[string]any{"productId": p.ProductID, "stockAtOpening": "2"}, "stockAtOpening"},
		"body not JSON":           {"not json", "body"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := api.openAlert(workerToken, c.body, api.key())

			if r.status != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", r.status, r.body)
			}
			e := r.errorBody(t)
			if e.Error != "VALIDATION_ERROR" || len(e.Details) == 0 || e.Details[0].Field != c.field {
				t.Errorf("expected a VALIDATION_ERROR naming %q, got %+v", c.field, e)
			}
		})
	}
}

func TestOpenStockAlert_AcceptsZeroStockAtOpening(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 0)

	r := api.openAlert(workerToken, alertBody(p.ProductID, 0), api.key())

	if r.status != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", r.status, r.body)
	}
}

func TestOpenStockAlert_RequiresAnIdempotencyKey(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)

	r := api.do("POST", "/api/v1/stock-alerts", alertBody(p.ProductID, 2), as(workerToken)...)

	if r.status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", r.status, r.body)
	}
	if e := r.errorBody(t); len(e.Details) == 0 || e.Details[0].Field != "Idempotency-Key" {
		t.Errorf("expected a detail naming Idempotency-Key, got %+v", e)
	}
}

func TestOpenStockAlert_Returns422WhenTheKeyWasSpentOnAnotherKindOfResource(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)
	key := api.key()
	if r := api.do("POST", "/api/v1/products/categories", map[string]any{"name": "Spent"}, "Idempotency-Key", key); r.status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.status, r.body)
	}

	r := api.openAlert(workerToken, alertBody(p.ProductID, 2), key)

	if r.status != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", r.status, r.body)
	}
}

// ─── POST /stock-alerts/{id}/resolve ───────────────────────────────────

func TestResolveStockAlert_Returns200WithTheAlertResolved(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)
	al := api.opened(p.ProductID, 2)

	// No Idempotency-Key: it is idempotent by status, not by key.
	r := api.resolveAlert(workerToken, al.AlertID)

	if r.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.status, r.body)
	}
	var got stockAlertJSON
	r.decode(t, &got)
	if got.AlertID != al.AlertID || got.ProductID != p.ProductID || got.Status != "RESOLVED" || got.StockAtOpening != 2 || got.OpenedAt != al.OpenedAt {
		t.Errorf("unexpected body: %+v", got)
	}
	if _, err := time.Parse(time.RFC3339, got.ResolvedAt); err != nil {
		t.Errorf("resolvedAt %q is not a date-time: %v", got.ResolvedAt, err)
	}
}

func TestResolveStockAlert_ResolvingAgainAnswers200AndLeavesResolvedAtAlone(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 2)
	al := api.opened(p.ProductID, 2)
	var first stockAlertJSON
	api.resolveAlert(workerToken, al.AlertID).decode(t, &first)
	time.Sleep(10 * time.Millisecond) // a moved resolvedAt would now differ

	r := api.resolveAlert(workerToken, al.AlertID)

	if r.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.status, r.body)
	}
	var again stockAlertJSON
	r.decode(t, &again)
	if again != first {
		t.Errorf("resolving twice changed the alert: %+v vs %+v", first, again)
	}
}

func TestResolveStockAlert_Returns404ForAnUnknownOrMalformedId(t *testing.T) {
	api := newTestAPI(t)
	for _, id := range []string{"0192a000-0000-7000-8000-00000000dead", "not-a-uuid"} {
		r := api.resolveAlert(workerToken, id)

		if r.status != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d: %s", id, r.status, r.body)
		}
	}
}

func TestResolveStockAlert_Returns403ToEveryCallerButTheWorkersToken(t *testing.T) {
	cases := map[string]string{
		"ADMIN":                   adminToken,
		"INVENTORY":               inventoryToken,
		"SALESPERSON":             salespersonToken,
		"service, read-only":      readOnlyServiceToken,
		"no roles or permissions": testToken,
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			api := newTestAPI(t)
			p := api.stocked("Mouse", 100, 2)
			al := api.opened(p.ProductID, 2)

			r := api.resolveAlert(token, al.AlertID)

			if r.status != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", r.status, r.body)
			}
			var page alertPageJSON
			api.listAlerts(adminToken, "?status=OPEN").decode(t, &page)
			if page.Meta.Total != 1 {
				t.Errorf("a forbidden call must resolve nothing, OPEN alerts: %d", page.Meta.Total)
			}
		})
	}
}

// ─── GET /stock-alerts ─────────────────────────────────────────────────

func TestListStockAlerts_AllowsAdminInventoryAndTheWorkersReadPermission(t *testing.T) {
	for name, token := range map[string]string{"ADMIN": adminToken, "INVENTORY": inventoryToken, "worker": workerToken, "service, read-only": readOnlyServiceToken} {
		api := newTestAPI(t)

		r := api.listAlerts(token, "")

		if r.status != http.StatusOK {
			t.Errorf("%s: expected 200, got %d: %s", name, r.status, r.body)
		}
	}
}

func TestListStockAlerts_Returns403ToSalespersonAndToTokensWithoutTheRole(t *testing.T) {
	writeOnlyService := tokenWith([]string{"SERVICE"}, []string{"stock-alerts:write"})
	for name, token := range map[string]string{"SALESPERSON": salespersonToken, "no roles or permissions": testToken, "service, write-only": writeOnlyService} {
		api := newTestAPI(t)

		r := api.listAlerts(token, "")

		if r.status != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d: %s", name, r.status, r.body)
		}
	}
}

func TestListStockAlerts_ReturnsNewestFirstWithPageMeta(t *testing.T) {
	api := newTestAPI(t)
	var ids []string
	for _, name := range []string{"A", "B", "C"} {
		ids = append(ids, api.opened(api.stocked(name, 100, 1).ProductID, 1).AlertID)
	}

	r := api.listAlerts(adminToken, "?limit=2")

	var page alertPageJSON
	r.decode(t, &page)
	if len(page.Data) != 2 || page.Data[0].AlertID != ids[2] || page.Data[1].AlertID != ids[1] {
		t.Errorf("expected the two newest alerts first, got %+v", page.Data)
	}
	if page.Meta != (pageMeta{Page: 1, Limit: 2, Total: 3, TotalPages: 2}) {
		t.Errorf("unexpected meta: %+v", page.Meta)
	}
}

func TestListStockAlerts_FiltersByStatus(t *testing.T) {
	api := newTestAPI(t)
	a := api.opened(api.stocked("A", 100, 1).ProductID, 1)
	b := api.opened(api.stocked("B", 100, 1).ProductID, 1)
	api.resolveAlert(workerToken, a.AlertID)

	var open, resolved alertPageJSON
	api.listAlerts(inventoryToken, "?status=OPEN").decode(t, &open)
	api.listAlerts(inventoryToken, "?status=RESOLVED").decode(t, &resolved)

	if len(open.Data) != 1 || open.Data[0].AlertID != b.AlertID {
		t.Errorf("unexpected OPEN list: %+v", open.Data)
	}
	if len(resolved.Data) != 1 || resolved.Data[0].AlertID != a.AlertID || resolved.Data[0].ResolvedAt == "" {
		t.Errorf("unexpected RESOLVED list: %+v", resolved.Data)
	}
}

func TestListStockAlerts_Returns400ForABadStatusOrAnUnknownFilter(t *testing.T) {
	api := newTestAPI(t)
	for _, q := range []string{"?status=CLOSED", "?status=open", "?productId=x", "?page=0"} {
		r := api.listAlerts(adminToken, q)

		if r.status != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", q, r.status, r.body)
		}
	}
}

func TestListStockAlerts_AnEmptyListIsAnEmptyArray(t *testing.T) {
	api := newTestAPI(t)

	r := api.listAlerts(adminToken, "")

	if !strings.Contains(string(r.body), `"data":[]`) {
		t.Errorf("expected data to be [], got %s", r.body)
	}
}

func TestOpenStockAlert_Returns422ForAnInactiveProduct(t *testing.T) {
	api := newTestAPI(t)
	p := api.inactiveProduct("Mouse")

	r := api.openAlert(workerToken, alertBody(p.ProductID, 1), api.key())

	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", r.status, r.body)
	}
	e := r.errorBody(t)
	if e.Error != "BUSINESS_RULE_VIOLATION" || e.TraceID == "" || len(e.Details) == 0 || e.Details[0].Field != "productId" {
		t.Errorf("unexpected error envelope: %+v", e)
	}
	var page alertPageJSON
	api.listAlerts(adminToken, "").decode(t, &page)
	if page.Meta.Total != 0 {
		t.Errorf("no alert may be created, found %d", page.Meta.Total)
	}
}

// An alert opened while the product was active keeps working afterwards.
func TestStockAlerts_AnOpenAlertOfAProductDeactivatedLaterIsUntouched(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 1)
	al := api.opened(p.ProductID, 1)
	api.do("DELETE", "/api/v1/products/"+p.ProductID, nil)

	var page alertPageJSON
	api.listAlerts(adminToken, "?status=OPEN").decode(t, &page)
	resolved := api.resolveAlert(workerToken, al.AlertID)

	if len(page.Data) != 1 || page.Data[0].AlertID != al.AlertID {
		t.Errorf("the open alert must still be listed, got %+v", page.Data)
	}
	if resolved.status != http.StatusOK {
		t.Errorf("resolve: expected 200, got %d: %s", resolved.status, resolved.body)
	}
}
