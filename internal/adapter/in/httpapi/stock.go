package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

const (
	reasonMax = 255
	// stock, delta and quantity are INTEGER columns: anything wider would
	// reach the database as an overflow error, i.e. a 500.
	maxInt32 = 1<<31 - 1
)

// ─── Stock adjustments ─────────────────────────────────────────────────

// adjustmentRequest is CreateStockAdjustmentRequest. adjustedBy is not a
// field: it comes from the token's sub, and any such key in the body is
// ignored.
type adjustmentRequest struct {
	Delta  *int    `json:"delta"`
	Reason *string `json:"reason"`
}

type adjustmentResponse struct {
	AdjustmentID string    `json:"adjustmentId"`
	ProductID    string    `json:"productId"`
	Delta        int       `json:"delta"`
	Reason       string    `json:"reason"`
	AdjustedBy   string    `json:"adjustedBy"`
	AdjustedAt   time.Time `json:"adjustedAt"`
	CurrentStock int       `json:"currentStock"`
}

type stockHandlers struct{ uc in.StockUseCases }

func (h stockHandlers) createAdjustment(w http.ResponseWriter, r *http.Request) {
	productID, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrProductNotFound)
		return
	}
	adjustedBy, ok := subjectFrom(r)
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "the token must carry a UUID subject")
		return
	}

	key, details := requireIdempotencyKey(r)
	var req adjustmentRequest
	if bodyDetails := decodeBody(r, &req); bodyDetails != nil {
		details = append(details, bodyDetails...)
	} else {
		if req.Delta == nil || *req.Delta == 0 || *req.Delta > maxInt32 || *req.Delta < -maxInt32 {
			details = append(details, errorDetail{Field: "delta", Message: "required, a non-zero 32-bit integer"})
		}
		switch {
		case req.Reason == nil || strings.TrimSpace(*req.Reason) == "":
			details = append(details, errorDetail{Field: "reason", Message: "required"})
		case utf8.RuneCountInString(*req.Reason) > reasonMax:
			details = append(details, errorDetail{Field: "reason", Message: fmt.Sprintf("at most %d characters", reasonMax)})
		}
	}
	if len(details) > 0 {
		writeValidationError(w, r, details)
		return
	}

	res, err := h.uc.CreateStockAdjustment(r.Context(), in.CreateStockAdjustmentCommand{
		IdempotencyKey: key, ProductID: productID, Delta: *req.Delta, Reason: *req.Reason, AdjustedBy: adjustedBy,
	})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	a := res.Adjustment
	body := adjustmentResponse{a.ID, a.ProductID, a.Delta, a.Reason, a.AdjustedBy, a.AdjustedAt, res.CurrentStock}
	if !res.Created {
		writeJSON(w, http.StatusOK, body)
		return
	}
	// The contract defines no GET for a single adjustment (a team decision, not
	// an oversight), so Location names the affected resource, which is fetchable.
	w.Header().Set("Location", "/api/v1/products/"+a.ProductID)
	writeJSON(w, http.StatusCreated, body)
}

// ─── Stock reservations ────────────────────────────────────────────────

type reservationRequest struct {
	Lines *[]reservationLineRequest `json:"lines"`
}

type reservationLineRequest struct {
	ProductID *string `json:"productId"`
	Quantity  *int    `json:"quantity"`
}

type reservationLineResponse struct {
	LineID         string `json:"lineId"`
	ProductID      string `json:"productId"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unitPriceCents"`
}

type reservationResponse struct {
	ReservationID string                    `json:"reservationId"`
	Status        string                    `json:"status"`
	CreatedAt     time.Time                 `json:"createdAt"`
	ReleasedAt    *time.Time                `json:"releasedAt,omitempty"`
	Lines         []reservationLineResponse `json:"lines"`
}

func toReservationResponse(r model.StockReservation) reservationResponse {
	lines := make([]reservationLineResponse, 0, len(r.Lines))
	for _, l := range r.Lines {
		lines = append(lines, reservationLineResponse{l.LineID, l.ProductID, l.Quantity, l.UnitPriceCents})
	}
	return reservationResponse{r.ID, r.Status, r.CreatedAt, r.ReleasedAt, lines}
}

// readLines validates the request lines, one detail per bad field.
func readLines(r *http.Request) (lines []in.ReservationLineCommand, details []errorDetail) {
	var req reservationRequest
	if details := decodeBody(r, &req); details != nil {
		return nil, details
	}
	if req.Lines == nil || len(*req.Lines) == 0 {
		return nil, []errorDetail{{Field: "lines", Message: "required, at least one line"}}
	}
	seen := map[string]bool{}
	for i, l := range *req.Lines {
		field := func(name string) string { return fmt.Sprintf("lines[%d].%s", i, name) }
		var id string
		if l.ProductID == nil {
			details = append(details, errorDetail{Field: field("productId"), Message: "required"})
		} else if cid, ok := canonicalUUID(*l.ProductID); !ok {
			details = append(details, errorDetail{Field: field("productId"), Message: "must be a UUID"})
		} else if seen[cid] {
			details = append(details, errorDetail{Field: field("productId"), Message: "a product may appear at most once"})
		} else {
			id = cid
			seen[cid] = true
		}
		if l.Quantity == nil || *l.Quantity < 1 || *l.Quantity > maxInt32 {
			details = append(details, errorDetail{Field: field("quantity"), Message: "required, an integer from 1 to 2147483647"})
			continue
		}
		lines = append(lines, in.ReservationLineCommand{ProductID: id, Quantity: *l.Quantity})
	}
	if details != nil {
		return nil, details
	}
	return lines, nil
}

func (h stockHandlers) createReservation(w http.ResponseWriter, r *http.Request) {
	if !slices.Contains(claimsFrom(r).Permissions, permStockReserve) {
		forbid(w, r)
		return
	}
	key, details := requireIdempotencyKey(r)
	lines, bodyDetails := readLines(r)
	if details = append(details, bodyDetails...); len(details) > 0 {
		writeValidationError(w, r, details)
		return
	}

	res, err := h.uc.CreateStockReservation(r.Context(), in.CreateStockReservationCommand{IdempotencyKey: key, Lines: lines})
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	if !res.Created {
		writeJSON(w, http.StatusOK, toReservationResponse(res.Reservation))
		return
	}
	w.Header().Set("Location", "/api/v1/stock-reservations/"+res.Reservation.ID)
	writeJSON(w, http.StatusCreated, toReservationResponse(res.Reservation))
}

// getReservation only requires a well-formed token, as before: the contract
// says "the workflow's service token" but names no permission for this read.
func (h stockHandlers) getReservation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrReservationNotFound)
		return
	}
	res, err := h.uc.GetStockReservation(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toReservationResponse(res))
}

func (h stockHandlers) releaseReservation(w http.ResponseWriter, r *http.Request) {
	if !slices.Contains(claimsFrom(r).Permissions, permStockRelease) {
		forbid(w, r)
		return
	}
	id, ok := pathID(r)
	if !ok {
		writeUseCaseError(w, r, in.ErrReservationNotFound)
		return
	}
	res, err := h.uc.ReleaseStockReservation(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toReservationResponse(res))
}

// Held only by synkro-workflow's service token (authentication.md). The
// permission is checked, not a role (security-rules.md). GET of a single
// reservation is deliberately not checked: see the note on getReservation.
const (
	permStockReserve = "stock:reserve"
	permStockRelease = "stock:release"
)
