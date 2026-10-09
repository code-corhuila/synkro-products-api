package httpapi

import (
	"net/http"
	"slices"
)

const (
	permProductsRead  = "products:read"
	permProductsWrite = "products:write"
)

// guard answers 403 before the handler runs, so no validation or lookup
// can leak anything to a caller that is not allowed.
func guard(allowed func(tokenClaims) bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowed(claimsFrom(r)) {
			forbid(w, r)
			return
		}
		next(w, r)
	}
}

func hasAnyRole(c tokenClaims, roles ...string) bool {
	return slices.ContainsFunc(roles, func(role string) bool { return slices.Contains(c.Roles, role) })
}

// canReadCatalog: ADMIN, SALESPERSON, INVENTORY, or a token holding
// products:read (the worker's). Applies to products and categories.
func canReadCatalog(c tokenClaims) bool {
	return slices.Contains(c.Permissions, permProductsRead) || hasAnyRole(c, "ADMIN", "SALESPERSON", "INVENTORY")
}

// canWriteCatalog: ADMIN, INVENTORY, or a token holding products:write.
// SALESPERSON does not. Applies to products, categories and stock
// adjustments.
func canWriteCatalog(c tokenClaims) bool {
	return slices.Contains(c.Permissions, permProductsWrite) || hasAnyRole(c, "ADMIN", "INVENTORY")
}
