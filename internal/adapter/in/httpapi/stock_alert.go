package httpapi

import (
	"net/http"
	"slices"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

const (
	permAlertsRead  = "stock-alerts:read"
	permAlertsWrite = "stock-alerts:write"
)

// ─── Stock alerts ──────────────────────────────────────────────────────

// alertRequest is CreateStockAlertRequest.
type alertRequest struct {
	ProductID      *string `json:"productId"`
	StockAtOpening *int    `json:"stockAtOpening"`
}

type alertResponse struct {
	AlertID        string     `json:"alertId"`
	ProductID      string     `json:"productId"`
	Status         string     `json:"status"`
	StockAtOpening int        `json:"stockAtOpening"`
	OpenedAt       time.Time  `json:"openedAt"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}

func toAlertResponse(a model.StockAlert) alertResponse {
	return alertResponse{a.ID, a.ProductID, a.Status, a.StockAtOpening, a.OpenedAt, a.ResolvedAt}
}

type alertHandlers struct{ uc in.StockAlertUseCases }

// canReadAlerts: ADMIN, INVENTORY, or a token holding stock-alerts:read.
func canReadAlerts(c tokenClaims) bool {
	return slices.Contains(c.Roles, "ADMIN") || slices.Contains(c.Roles, "INVENTORY") ||
		slices.Contains(c.Permissions, permAlertsRead)
}

// canWriteAlerts: only a token holding stock-alerts:write, i.e. the
// worker's. It checks the permission, not a role (security-rules.md).
func canWriteAlerts(c tokenClaims) bool { return slices.Contains(c.Permissions, permAlertsWrite) }

func forbid(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusForbidden, "FORBIDDEN", "The token is not allowed to perform this operation")
}

func (h alertHandlers) list(w http.ResponseWriter, r *http.Request) {
	if !canReadAlerts(claimsFrom(r)) {
		forbid(w, r)
		return
	}
	q := newQuery(r, "status")
	page, limit := q.page()
	status := q.optString("status")
	if status != nil && *status != model.AlertStatusOpen && *status != model.AlertStatusResolved {
		q.fail("status", "must be OPEN or RESOLVED")
		status = nil
	}
	if len(q.details) > 0 {
		writeValidationError(w, r, q.details)
		return
	}

	res, err := h.uc.ListStockAlerts(r.Context(), in.ListStockAlertsQuery{Page: page, Limit: limit, Status: status})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	data := make([]alertResponse, 0, len(res.Items))
	for _, a := range res.Items {
		data = append(data, toAlertResponse(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": newPageMeta(page, limit, res.Total)})
}

func (h alertHandlers) open(w http.ResponseWriter, r *http.Request) {
	if !canWriteAlerts(claimsFrom(r)) {
		forbid(w, r)
		return
	}
	key, details := requireIdempotencyKey(r)
	var req alertRequest
	var productID string
	if bodyDetails := decodeBody(r, &req); bodyDetails != nil {
		details = append(details, bodyDetails...)
	} else {
		if req.ProductID == nil {
			details = append(details, errorDetail{Field: "productId", Message: "required"})
		} else if id, ok := canonicalUUID(*req.ProductID); !ok {
			details = append(details, errorDetail{Field: "productId", Message: "must be a UUID"})
		} else {
			productID = id
		}
		if req.StockAtOpening == nil || *req.StockAtOpening < 0 || *req.StockAtOpening > maxInt32 {
			details = append(details, errorDetail{Field: "stockAtOpening", Message: "required, an integer from 0 to 2147483647"})
		}
	}
	if len(details) > 0 {
		writeValidationError(w, r, details)
		return
	}

	res, err := h.uc.OpenStockAlert(r.Context(), in.OpenStockAlertCommand{
		IdempotencyKey: key, ProductID: productID, StockAtOpening: *req.StockAtOpening,
	})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	if !res.Created {
		writeJSON(w, http.StatusOK, toAlertResponse(res.Alert))
		return
	}
	// The contract defines no GET for a single alert, so, as for stock
	// adjustments, Location names the product, which is fetchable.
	w.Header().Set("Location", "/api/v1/products/"+res.Alert.ProductID)
	writeJSON(w, http.StatusCreated, toAlertResponse(res.Alert))
}

func (h alertHandlers) resolve(w http.ResponseWriter, r *http.Request) {
	if !canWriteAlerts(claimsFrom(r)) {
		forbid(w, r)
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrStockAlertNotFound)
		return
	}
	a, err := h.uc.ResolveStockAlert(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAlertResponse(a))
}
