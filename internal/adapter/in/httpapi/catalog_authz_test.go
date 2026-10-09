package httpapi

import (
	"net/http"
	"testing"
)

// Authorization of products, categories and stock adjustments
// (authentication.md, RBAC): products:read for ADMIN, SALESPERSON,
// INVENTORY and the worker's token; products:write for ADMIN and INVENTORY.

type catalogRoute struct {
	name, method, path string
	body               any
}

func catalogWriteRoutes(productID, categoryID string) []catalogRoute {
	return []catalogRoute{
		{"POST /products", "POST", "/api/v1/products", map[string]any{"name": "X", "priceCents": 100, "categoryId": categoryID}},
		{"PUT /products/{id}", "PUT", "/api/v1/products/" + productID, map[string]any{"name": "X", "priceCents": 100, "categoryId": categoryID}},
		{"DELETE /products/{id}", "DELETE", "/api/v1/products/" + productID, nil},
		{"POST /products/{id}/stock-adjustments", "POST", "/api/v1/products/" + productID + "/stock-adjustments", map[string]any{"delta": 1, "reason": "x"}},
		{"POST /products/categories", "POST", "/api/v1/products/categories", map[string]any{"name": "Fresh"}},
		{"PUT /products/categories/{id}", "PUT", "/api/v1/products/categories/" + categoryID, map[string]any{"name": "Renamed"}},
		{"DELETE /products/categories/{id}", "DELETE", "/api/v1/products/categories/" + categoryID, nil},
	}
}

func catalogReadRoutes(productID string) []catalogRoute {
	return []catalogRoute{
		{"GET /products", "GET", "/api/v1/products", nil},
		{"GET /products/{id}", "GET", "/api/v1/products/" + productID, nil},
		{"GET /products/categories", "GET", "/api/v1/products/categories", nil},
	}
}

func (a *testAPI) call(rt catalogRoute, token string) apiResponse {
	h := as(token)
	if rt.method == "POST" {
		h = append(h, "Idempotency-Key", a.key())
	}
	return a.do(rt.method, rt.path, rt.body, h...)
}

func TestCatalogWrites_Return403WithoutProductsWrite(t *testing.T) {
	denied := map[string]string{
		"SALESPERSON (products:read only)": salespersonToken,
		"the worker's token":               workerToken,
		"the workflow's token":             workflowToken,
		"no roles or permissions":          testToken,
	}
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 5)
	for _, rt := range catalogWriteRoutes(p.ProductID, p.CategoryID) {
		for who, token := range denied {
			t.Run(rt.name+" / "+who, func(t *testing.T) {
				r := api.call(rt, token)
				if r.status != http.StatusForbidden {
					t.Fatalf("expected 403, got %d: %s", r.status, r.body)
				}
				if e := r.errorBody(t); e.Error != "FORBIDDEN" || e.TraceID == "" {
					t.Errorf("unexpected envelope: %+v", e)
				}
			})
		}
	}
}

func TestCatalogWrites_ForbiddenCallsChangeNothing(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 5)
	var before productJSON
	api.do("GET", "/api/v1/products/"+p.ProductID, nil, as(adminToken)...).decode(t, &before)
	for _, rt := range catalogWriteRoutes(p.ProductID, p.CategoryID) {
		api.call(rt, salespersonToken)
	}

	var got productJSON
	api.do("GET", "/api/v1/products/"+p.ProductID, nil, as(adminToken)...).decode(t, &got)
	if got != before {
		t.Errorf("product changed by forbidden calls: %+v -> %+v", before, got)
	}
	var list struct {
		Meta pageMeta `json:"meta"`
	}
	api.do("GET", "/api/v1/products", nil, as(adminToken)...).decode(t, &list)
	if list.Meta.Total != 1 {
		t.Errorf("a forbidden POST created a product: total %d", list.Meta.Total)
	}
}

func TestCatalogWrites_ArePermittedToAdminAndInventoryOrAProductsWritePermission(t *testing.T) {
	allowed := map[string]string{
		"ADMIN":                adminToken,
		"INVENTORY":            inventoryToken,
		"products:write alone": tokenWith(nil, []string{"products:write"}),
	}
	for who, token := range allowed {
		api := newTestAPI(t)
		p := api.stocked("Mouse", 100, 5)
		for _, rt := range catalogWriteRoutes(p.ProductID, p.CategoryID) {
			if r := api.call(rt, token); r.status == http.StatusForbidden || r.status >= 500 {
				t.Errorf("%s / %s: got %d: %s", who, rt.name, r.status, r.body)
			}
		}
	}
}

func TestCatalogWrites_CheckThePermissionBeforeValidatingOrLookingUp(t *testing.T) {
	api := newTestAPI(t)
	for _, rt := range []catalogRoute{
		{"bad body", "POST", "/api/v1/products", "not json"},
		{"unknown id", "PUT", "/api/v1/products/not-a-uuid", nil},
		{"unknown category", "DELETE", "/api/v1/products/categories/not-a-uuid", nil},
	} {
		if r := api.call(rt, salespersonToken); r.status != http.StatusForbidden {
			t.Errorf("%s: expected 403 first, got %d: %s", rt.name, r.status, r.body)
		}
	}
}

func TestCatalogReads_Return403WithoutProductsRead(t *testing.T) {
	denied := map[string]string{
		"the workflow's token":    workflowToken,
		"no roles or permissions": testToken,
	}
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 5)
	for _, rt := range catalogReadRoutes(p.ProductID) {
		for who, token := range denied {
			t.Run(rt.name+" / "+who, func(t *testing.T) {
				if r := api.call(rt, token); r.status != http.StatusForbidden {
					t.Fatalf("expected 403, got %d: %s", r.status, r.body)
				}
			})
		}
	}
}

func TestCatalogReads_ArePermittedToAllThreeRolesAndTheWorkersToken(t *testing.T) {
	allowed := map[string]string{
		"ADMIN": adminToken, "SALESPERSON": salespersonToken, "INVENTORY": inventoryToken, "the worker": workerToken,
		"products:read alone": tokenWith(nil, []string{"products:read"}),
	}
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 5)
	for _, rt := range catalogReadRoutes(p.ProductID) {
		for who, token := range allowed {
			if r := api.call(rt, token); r.status != http.StatusOK {
				t.Errorf("%s / %s: expected 200, got %d: %s", who, rt.name, r.status, r.body)
			}
		}
	}
}

// ─── GET /products/{id} and inactive products ──────────────────────────

// inactiveProduct creates a product and deactivates it, as the default
// (ADMIN) caller.
func (a *testAPI) inactiveProduct(name string) productJSON {
	a.t.Helper()
	p := a.stocked(name, 100, 5)
	if r := a.do("DELETE", "/api/v1/products/"+p.ProductID, nil); r.status != http.StatusOK {
		a.t.Fatalf("deactivating: %d %s", r.status, r.body)
	}
	return p
}

// The contract: "Returns the product if it exists and is active (or if the
// caller is ADMIN/INVENTORY, including inactive)". For anyone else an
// inactive product is indistinguishable from one that does not exist.
func TestGetProduct_AnInactiveProductIs404ForACallerWithoutProductsWrite(t *testing.T) {
	callers := map[string]string{
		"SALESPERSON":        salespersonToken,
		"the worker's token": workerToken,
		"products:read only": tokenWith(nil, []string{"products:read"}),
	}
	for who, token := range callers {
		t.Run(who, func(t *testing.T) {
			api := newTestAPI(t)
			p := api.inactiveProduct("Mouse")

			r := api.do("GET", "/api/v1/products/"+p.ProductID, nil, as(token)...)
			missing := api.do("GET", "/api/v1/products/0192a000-0000-7000-8000-00000000dead", nil, as(token)...)

			if r.status != http.StatusNotFound || r.errorBody(t).Error != "NOT_FOUND" {
				t.Fatalf("expected 404 NOT_FOUND, got %d: %s", r.status, r.body)
			}
			if got, want := r.errorBody(t).Message, missing.errorBody(t).Message; got != want {
				t.Errorf("an inactive product must look like a missing one: %q vs %q", got, want)
			}
		})
	}
}

func TestGetProduct_AnInactiveProductIsStillVisibleToAdminInventoryAndProductsWrite(t *testing.T) {
	callers := map[string]string{
		"ADMIN":                 adminToken,
		"INVENTORY":             inventoryToken,
		"products:read + write": tokenWith(nil, []string{"products:read", "products:write"}),
	}
	for who, token := range callers {
		t.Run(who, func(t *testing.T) {
			api := newTestAPI(t)
			p := api.inactiveProduct("Mouse")

			r := api.do("GET", "/api/v1/products/"+p.ProductID, nil, as(token)...)

			var got productJSON
			r.decode(t, &got)
			if r.status != http.StatusOK || got.ProductID != p.ProductID || got.Active {
				t.Errorf("expected 200 with the inactive product, got %d: %s", r.status, r.body)
			}
		})
	}
}

func TestGetProduct_AnActiveProductIsVisibleToEveryReader(t *testing.T) {
	api := newTestAPI(t)
	p := api.stocked("Mouse", 100, 5)
	for who, token := range map[string]string{"SALESPERSON": salespersonToken, "the worker's token": workerToken} {
		if r := api.do("GET", "/api/v1/products/"+p.ProductID, nil, as(token)...); r.status != http.StatusOK {
			t.Errorf("%s: expected 200, got %d: %s", who, r.status, r.body)
		}
	}
}

// ─── GET /products and the active filter ───────────────────────────────

type productPageJSON struct {
	Data []productJSON `json:"data"`
	Meta pageMeta      `json:"meta"`
}

// listFixture has one active and one inactive product.
func listFixture(t *testing.T) (api *testAPI, active, inactive productJSON) {
	api = newTestAPI(t)
	active = api.stocked("Active", 100, 5)
	inactive = api.inactiveProduct("Inactive")
	return api, active, inactive
}

func listedIDs(t *testing.T, r apiResponse) []string {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.status, r.body)
	}
	var page productPageJSON
	r.decode(t, &page)
	ids := []string{}
	for _, p := range page.Data {
		ids = append(ids, p.ProductID)
	}
	return ids
}

func TestListProducts_CallersWithoutProductsWriteNeverSeeInactiveProducts(t *testing.T) {
	callers := map[string]string{
		"SALESPERSON":        salespersonToken,
		"the worker's token": workerToken,
		"products:read only": tokenWith(nil, []string{"products:read"}),
	}
	queries := map[string]string{"active=false": "?active=false", "no active filter": "", "active=true": "?active=true"}
	for who, token := range callers {
		for qname, q := range queries {
			t.Run(who+" / "+qname, func(t *testing.T) {
				api, active, _ := listFixture(t)

				r := api.do("GET", "/api/v1/products"+q, nil, as(token)...)

				ids := listedIDs(t, r)
				var page productPageJSON
				r.decode(t, &page)
				if len(ids) != 1 || ids[0] != active.ProductID || page.Meta.Total != 1 {
					t.Errorf("expected only the active product (total 1), got %v, meta %+v", ids, page.Meta)
				}
			})
		}
	}
}

func TestListProducts_AdminAndInventoryHonourTheActiveFilterAsGiven(t *testing.T) {
	for who, token := range map[string]string{"ADMIN": adminToken, "INVENTORY": inventoryToken, "products:read + write": tokenWith(nil, []string{"products:read", "products:write"})} {
		t.Run(who, func(t *testing.T) {
			api, active, inactive := listFixture(t)

			onlyInactive := listedIDs(t, api.do("GET", "/api/v1/products?active=false", nil, as(token)...))
			onlyActive := listedIDs(t, api.do("GET", "/api/v1/products?active=true", nil, as(token)...))
			all := listedIDs(t, api.do("GET", "/api/v1/products", nil, as(token)...))

			if len(onlyInactive) != 1 || onlyInactive[0] != inactive.ProductID {
				t.Errorf("active=false: expected only the inactive product, got %v", onlyInactive)
			}
			if len(onlyActive) != 1 || onlyActive[0] != active.ProductID {
				t.Errorf("active=true: expected only the active product, got %v", onlyActive)
			}
			if len(all) != 2 {
				t.Errorf("no filter: expected both products, got %v", all)
			}
		})
	}
}
