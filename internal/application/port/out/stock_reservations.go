package out

import (
	"context"
	"fmt"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// LineError names the reservation line that made CreateOnce fail. Err is
// ErrInsufficientStock or ErrProductUnavailable.
type LineError struct {
	Index     int
	ProductID string
	Err       error
}

func (e *LineError) Error() string {
	return fmt.Sprintf("line %d (product %s): %v", e.Index, e.ProductID, e.Err)
}
func (e *LineError) Unwrap() error { return e.Err }

type StockReservationRepository interface {
	// Atomically, under lock: for every line, checks the product exists,
	// is active and has enough stock, decrements it, and freezes
	// UnitPriceCents from the product's price read in THIS transaction —
	// never a price read earlier by the caller. All lines succeed or none
	// do. Inserts the reservation, its lines and the idempotency_key row
	// together. Returns a *LineError (naming the first failing line) if
	// any line fails. On a repeated key returns the original, stored
	// reservation with created=false and changes nothing.
	CreateOnce(ctx context.Context, key string, r model.StockReservation) (reservation model.StockReservation, created bool, err error)
	FindByID(ctx context.Context, id string) (model.StockReservation, error) // ErrNotFound if absent
	// Atomically: restores every line's stock, sets status=RELEASED.
	// No-op (returns the current state, no error, no stock change) if
	// already RELEASED. ErrNotFound if absent.
	Release(ctx context.Context, id string) (model.StockReservation, error)
}
