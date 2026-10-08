package usecase

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// StockService owns the two stock-changing operations. It validates what
// needs no I/O (the domain's invariants) and leaves every check that
// depends on concurrent state — is there enough stock, what is the price
// now — to the repositories, which make it atomically under lock. Reading
// the product here first would only be a stale answer to a question the
// adapter has to ask again.
type StockService struct {
	adjustments  out.StockAdjustmentRepository
	reservations out.StockReservationRepository
	newID        func() string
}

var _ in.StockUseCases = (*StockService)(nil)

func NewStockService(adjustments out.StockAdjustmentRepository, reservations out.StockReservationRepository, newID func() string) *StockService {
	return &StockService{adjustments: adjustments, reservations: reservations, newID: newID}
}

func (s *StockService) CreateStockAdjustment(ctx context.Context, cmd in.CreateStockAdjustmentCommand) (in.CreateStockAdjustmentResult, error) {
	a, err := model.NewStockAdjustment(s.newID(), cmd.ProductID, cmd.Delta, cmd.Reason, cmd.AdjustedBy)
	if err != nil {
		return in.CreateStockAdjustmentResult{}, err
	}

	stored, currentStock, created, err := s.adjustments.CreateOnce(ctx, cmd.IdempotencyKey, a)
	switch {
	case errors.Is(err, out.ErrInsufficientStock):
		return in.CreateStockAdjustmentResult{}, in.ErrInsufficientStock
	case errors.Is(err, out.ErrIdempotencyKeyReused):
		return in.CreateStockAdjustmentResult{}, in.ErrIdempotencyKeyReused
	case err != nil:
		return in.CreateStockAdjustmentResult{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return in.CreateStockAdjustmentResult{Adjustment: stored, CurrentStock: currentStock, Created: created}, nil
}

func (s *StockService) CreateStockReservation(ctx context.Context, cmd in.CreateStockReservationCommand) (in.CreateStockReservationResult, error) {
	lines := make([]model.ReservationLine, len(cmd.Lines))
	for i, l := range cmd.Lines {
		lines[i] = model.ReservationLine{LineID: s.newID(), ProductID: l.ProductID, Quantity: l.Quantity}
	}
	r, err := model.NewStockReservation(s.newID(), lines) // runs model.ValidateLines
	if err != nil {
		return in.CreateStockReservationResult{}, err
	}

	stored, created, err := s.reservations.CreateOnce(ctx, cmd.IdempotencyKey, r)
	if err != nil {
		return in.CreateStockReservationResult{}, mapReservationError(err)
	}
	return in.CreateStockReservationResult{Reservation: stored, Created: created}, nil
}

func (s *StockService) GetStockReservation(ctx context.Context, id string) (model.StockReservation, error) {
	r, err := s.reservations.FindByID(ctx, id)
	if err != nil {
		return model.StockReservation{}, notFoundAs(err, in.ErrReservationNotFound)
	}
	return r, nil
}

// ReleaseStockReservation is idempotent: the repository returns an
// already-released reservation untouched.
func (s *StockService) ReleaseStockReservation(ctx context.Context, id string) (model.StockReservation, error) {
	r, err := s.reservations.Release(ctx, id)
	if err != nil {
		return model.StockReservation{}, notFoundAs(err, in.ErrReservationNotFound)
	}
	return r, nil
}

func mapReservationError(err error) error {
	if errors.Is(err, out.ErrIdempotencyKeyReused) {
		return in.ErrIdempotencyKeyReused
	}
	var le *out.LineError
	if !errors.As(err, &le) {
		return err
	}
	cause := in.ErrInsufficientStock
	if errors.Is(le.Err, out.ErrProductUnavailable) {
		cause = in.ErrProductUnavailable
	}
	return &in.ReservationLineError{Index: le.Index, ProductID: le.ProductID, Err: cause}
}
