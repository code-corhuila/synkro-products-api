package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// errorResponse is the only error envelope of the system (_shared.yaml
// Error). It never carries a driver message or a stack trace.
type errorResponse struct {
	Error   string        `json:"error"`
	Message string        `json:"message"`
	Details []errorDetail `json:"details,omitempty"`
	TraceID string        `json:"traceId"`
}

type errorDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details ...errorDetail) {
	writeJSON(w, status, errorResponse{Error: code, Message: message, Details: details, TraceID: traceIDFrom(r)})
}

func writeValidationError(w http.ResponseWriter, r *http.Request, details []errorDetail) {
	writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "The request has invalid fields", details...)
}

func writeNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
}

// writeUseCaseError maps every error a use case can return to exactly one
// status. Note the two "category" rules land on different codes on
// purpose: a product's missing/inactive category is 404, a category's
// name collision is 422.
func writeUseCaseError(w http.ResponseWriter, r *http.Request, err error) {
	var lineErr *in.ReservationLineError
	switch {
	case errors.As(err, &lineErr):
		writeLineRuleViolation(w, r, lineErr)
	case errors.Is(err, in.ErrInsufficientStock): // a stock adjustment: no line involved
		writeError(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION",
			"The adjustment would take stock below 0",
			errorDetail{Field: "delta", Message: "exceeds the current stock"})
	case errors.Is(err, in.ErrReservationNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Stock reservation not found")

	case errors.Is(err, in.ErrProductInactive):
		writeError(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION",
			"An alert cannot be opened for an inactive product",
			errorDetail{Field: "productId", Message: "the product is inactive"})
	case errors.Is(err, in.ErrStockAlertNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Stock alert not found")

	case errors.Is(err, in.ErrProductNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Product not found")
	case errors.Is(err, in.ErrCategoryNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Category not found or not active")

	case errors.Is(err, in.ErrCategoryNameTaken):
		writeError(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION",
			"An active category with this name already exists",
			errorDetail{Field: "name", Message: "already used by an active category"})
	case errors.Is(err, in.ErrCategoryHasActiveProducts):
		writeError(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION",
			"The category has active products assigned to it")
	case errors.Is(err, in.ErrIdempotencyKeyReused):
		writeError(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION",
			"The Idempotency-Key was already used for a different resource",
			errorDetail{Field: "Idempotency-Key", Message: "already used for a different resource"})

	// The domain's own invariants; the request validation normally
	// catches these first.
	case errors.Is(err, model.ErrNameRequired), errors.Is(err, model.ErrCategoryNameRequired):
		writeValidationError(w, r, []errorDetail{{Field: "name", Message: "required"}})
	case errors.Is(err, model.ErrPriceNotPositive):
		writeValidationError(w, r, []errorDetail{{Field: "priceCents", Message: "must be greater than 0"}})
	case errors.Is(err, model.ErrDeltaZero):
		writeValidationError(w, r, []errorDetail{{Field: "delta", Message: "must not be zero"}})
	case errors.Is(err, model.ErrStockAtOpeningNegative):
		writeValidationError(w, r, []errorDetail{{Field: "stockAtOpening", Message: "must not be negative"}})
	case errors.Is(err, model.ErrReasonRequired):
		writeValidationError(w, r, []errorDetail{{Field: "reason", Message: "required"}})
	case errors.Is(err, model.ErrNoLines), errors.Is(err, model.ErrDuplicateProduct), errors.Is(err, model.ErrQuantityNotPositive):
		writeValidationError(w, r, []errorDetail{{Field: "lines", Message: err.Error()}})

	default:
		slog.ErrorContext(r.Context(), "unexpected error", "error", err, "traceId", traceIDFrom(r),
			"method", r.Method, "path", r.URL.Path)
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected error")
	}
}

// writeLineRuleViolation answers a reservation that was refused because of
// one line: 422, with a detail that points at that line.
func writeLineRuleViolation(w http.ResponseWriter, r *http.Request, e *in.ReservationLineError) {
	if errors.Is(e.Err, in.ErrProductUnavailable) {
		writeError(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION",
			"A product is inactive or does not exist",
			errorDetail{Field: fmt.Sprintf("lines[%d].productId", e.Index), Message: fmt.Sprintf("product %s is inactive or does not exist", e.ProductID)})
		return
	}
	writeError(w, r, http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION",
		"Requested quantity exceeds available stock",
		errorDetail{Field: fmt.Sprintf("lines[%d].quantity", e.Index), Message: fmt.Sprintf("exceeds available stock for product %s", e.ProductID)})
}
