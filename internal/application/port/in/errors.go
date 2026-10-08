package in

import "errors"

// Errors every use case in this package may return besides the domain's
// own validation errors (model.Err*). The HTTP adapter maps each one to
// exactly one status code; see httpapi.writeUseCaseError.
var (
	ErrProductNotFound  = errors.New("product not found")
	ErrCategoryNotFound = errors.New("category not found or not active")

	ErrCategoryNameTaken         = errors.New("an active category with this name already exists")
	ErrCategoryHasActiveProducts = errors.New("the category has active products assigned to it")
	ErrIdempotencyKeyReused      = errors.New("the Idempotency-Key was already used for a different resource")
)

// Stock errors. ErrInsufficientStock and ErrProductUnavailable are the
// 422 BUSINESS_RULE_VIOLATION cases; a reservation reports them wrapped
// in a *ReservationLineError that names the line.
var (
	ErrReservationNotFound = errors.New("stock reservation not found")
	ErrInsufficientStock   = errors.New("insufficient stock")
	ErrProductUnavailable  = errors.New("product is inactive or does not exist")
)

type ReservationLineError struct {
	Index     int
	ProductID string
	Err       error // ErrInsufficientStock or ErrProductUnavailable
}

func (e *ReservationLineError) Error() string { return e.Err.Error() }
func (e *ReservationLineError) Unwrap() error { return e.Err }
