package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// StockAlertService validates what needs no I/O and leaves "does this
// product already have an OPEN alert" to the repository: that answer is
// concurrent state, owned by the partial unique index, and a check here
// would only be a stale read of it.
type StockAlertService struct {
	alerts out.StockAlertRepository
	newID  func() string
	now    func() time.Time
}

var _ in.StockAlertUseCases = (*StockAlertService)(nil)

func NewStockAlertService(alerts out.StockAlertRepository, newID func() string, now func() time.Time) *StockAlertService {
	return &StockAlertService{alerts: alerts, newID: newID, now: now}
}

func (s *StockAlertService) OpenStockAlert(ctx context.Context, cmd in.OpenStockAlertCommand) (in.OpenStockAlertResult, error) {
	a, err := model.OpenStockAlert(s.newID(), cmd.ProductID, cmd.StockAtOpening)
	if err != nil {
		return in.OpenStockAlertResult{}, err
	}

	stored, created, err := s.alerts.OpenOnce(ctx, cmd.IdempotencyKey, a)
	switch {
	case errors.Is(err, out.ErrIdempotencyKeyReused):
		return in.OpenStockAlertResult{}, in.ErrIdempotencyKeyReused
	case err != nil:
		return in.OpenStockAlertResult{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return in.OpenStockAlertResult{Alert: stored, Created: created}, nil
}

// ResolveStockAlert is idempotent by status: the repository returns an
// already-resolved alert untouched.
func (s *StockAlertService) ResolveStockAlert(ctx context.Context, id string) (model.StockAlert, error) {
	a, err := s.alerts.Resolve(ctx, id, s.now().UTC())
	if err != nil {
		return model.StockAlert{}, notFoundAs(err, in.ErrStockAlertNotFound)
	}
	return a, nil
}

func (s *StockAlertService) ListStockAlerts(ctx context.Context, q in.ListStockAlertsQuery) (in.ListStockAlertsResult, error) {
	items, total, err := s.alerts.List(ctx, q.Page, q.Limit, q.Status)
	if err != nil {
		return in.ListStockAlertsResult{}, err
	}
	return in.ListStockAlertsResult{Items: items, Total: total}, nil
}
