package persistence

import (
	"context"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// The stock side of the in-memory store. It enforces what the Postgres
// adapter enforces under lock — availability, all-or-nothing across
// lines, the price read at call time, replays that change nothing — so
// the HTTP-tier tests exercise the real rules. The single mutex stands in
// for the row locks.

func (m *Memory) StockAdjustments() out.StockAdjustmentRepository { return memoryAdjustments{m} }
func (m *Memory) StockReservations() out.StockReservationRepository {
	return memoryReservations{m}
}

type memoryAdjustments struct{ m *Memory }

func (r memoryAdjustments) CreateOnce(_ context.Context, key string, a model.StockAdjustment) (model.StockAdjustment, int, bool, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if id, replay, err := r.m.claimKey(key, resourceAdjustment); err != nil || replay {
		if err != nil {
			return model.StockAdjustment{}, 0, false, err
		}
		orig := r.m.adjustments[id]
		return orig, *orig.StockAfter, false, nil // what it left, not the stock now
	}
	row, ok := r.m.products[a.ProductID]
	if !ok {
		return model.StockAdjustment{}, 0, false, out.ErrNotFound
	}
	if row.v.Stock+a.Delta < 0 {
		return model.StockAdjustment{}, 0, false, out.ErrInsufficientStock
	}
	row.v.Stock += a.Delta
	r.m.products[a.ProductID] = row
	a.AdjustedAt = time.Now().UTC()
	after := row.v.Stock
	a.StockAfter = &after
	r.m.adjustments[a.ID] = a
	r.m.keys[key] = memKey{resourceAdjustment, a.ID}
	return a, row.v.Stock, true, nil
}

type memoryReservations struct{ m *Memory }

func (r memoryReservations) CreateOnce(_ context.Context, key string, res model.StockReservation) (model.StockReservation, bool, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if id, replay, err := r.m.claimKey(key, resourceReservation); err != nil || replay {
		if err != nil {
			return model.StockReservation{}, false, err
		}
		return r.m.reservations[id], false, nil
	}

	// Every line is checked, and priced, before any product is touched:
	// the in-memory equivalent of rolling everything back.
	priced := make([]model.ReservationLine, len(res.Lines))
	for i, l := range res.Lines {
		row, ok := r.m.products[l.ProductID]
		switch {
		case !ok || !row.v.Active:
			return model.StockReservation{}, false, &out.LineError{Index: i, ProductID: l.ProductID, Err: out.ErrProductUnavailable}
		case row.v.Stock < l.Quantity:
			return model.StockReservation{}, false, &out.LineError{Index: i, ProductID: l.ProductID, Err: out.ErrInsufficientStock}
		}
		l.UnitPriceCents = row.v.PriceCents
		priced[i] = l
	}
	for _, l := range priced {
		row := r.m.products[l.ProductID]
		row.v.Stock -= l.Quantity
		r.m.products[l.ProductID] = row
	}
	res.Lines = priced
	res.CreatedAt = time.Now().UTC()
	r.m.reservations[res.ID] = res
	r.m.keys[key] = memKey{resourceReservation, res.ID}
	return res, true, nil
}

func (r memoryReservations) FindByID(_ context.Context, id string) (model.StockReservation, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	res, ok := r.m.reservations[id]
	if !ok {
		return model.StockReservation{}, out.ErrNotFound
	}
	return res, nil
}

func (r memoryReservations) Release(_ context.Context, id string) (model.StockReservation, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	res, ok := r.m.reservations[id]
	if !ok {
		return model.StockReservation{}, out.ErrNotFound
	}
	if res.Status == model.StatusReleased {
		return res, nil
	}
	for _, l := range res.Lines {
		row := r.m.products[l.ProductID]
		row.v.Stock += l.Quantity
		r.m.products[l.ProductID] = row
	}
	now := time.Now().UTC()
	res.Status, res.ReleasedAt = model.StatusReleased, &now
	r.m.reservations[id] = res
	return res, nil
}
