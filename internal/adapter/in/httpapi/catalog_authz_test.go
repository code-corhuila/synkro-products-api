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
