package httpapi

import (
	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"net/http"
	"slices"
	"time"
)

func canReadPriceHistory(c tokenClaims) bool {
	return hasAnyRole(c, "ADMIN", "INVENTORY") && !hasAnyRole(c, "SERVICE") && slices.Contains(c.Permissions, permProductsRead)
}

type priceHistoryResponse struct {
	HistoryID     string    `json:"historyId"`
	ProductID     string    `json:"productId"`
	Sequence      int64     `json:"sequence"`
	OldPriceCents int64     `json:"oldPriceCents"`
	NewPriceCents int64     `json:"newPriceCents"`
	ChangedBy     string    `json:"changedBy"`
	ChangedAt     time.Time `json:"changedAt"`
}

func (h productHandlers) history(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrProductNotFound)
		return
	}
	q := newQuery(r)
	page, limit := q.page()
	if len(q.details) > 0 {
		writeValidationError(w, r, q.details)
		return
	}
	result, err := h.uc.ListPriceHistory(r.Context(), in.ListPriceHistoryQuery{ProductID: id, Page: page, Limit: limit})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	data := make([]priceHistoryResponse, 0, len(result.Items))
	for _, v := range result.Items {
		data = append(data, priceHistoryResponse{v.ID, v.ProductID, v.Sequence, v.OldPriceCents, v.NewPriceCents, v.ChangedBy, v.ChangedAt.UTC()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": newPageMeta(page, limit, result.Total)})
}
