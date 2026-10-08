package usecase

import (
	"context"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Unlike the catalog fakes in fakes_test.go, these ENFORCE the rules the
// real adapter enforces under lock: availability, all-or-nothing across
// lines, the price frozen from the product as it is at call time, and
// replays that re-check and re-apply nothing. A fake that only recorded
// calls would let a use-case bug (say, reading the price early, or
// reserving line by line) through. Both stock fakes share one
// fakeProducts, the way both tables share products_schema.product.

type fakeStockAdjustments struct {
	products    *fakeProducts
	byID        map[string]model.StockAdjustment
	keys        map[string]string
	createCalls int
	err         error
}

func newFakeStockAdjustments(ps *fakeProducts) *fakeStockAdjustments {
	return &fakeStockAdjustments{products: ps, byID: map[string]model.StockAdjustment{}, keys: map[string]string{}}
}

func (f *fakeStockAdjustments) CreateOnce(_ context.Context, key string, a model.StockAdjustment) (model.StockAdjustment, int, bool, error) {
	f.createCalls++
	if f.err != nil {
		return model.StockAdjustment{}, 0, false, f.err
	}
	if id, ok := f.keys[key]; ok {
		return f.byID[id], f.products.byID[f.byID[id].ProductID].Stock, false, nil
	}
	p, ok := f.products.byID[a.ProductID]
	if !ok {
		return model.StockAdjustment{}, 0, false, out.ErrNotFound
	}
	if p.Stock+a.Delta < 0 {
		return model.StockAdjustment{}, 0, false, out.ErrInsufficientStock
	}
	p.Stock += a.Delta
	f.products.byID[p.ID] = p
	a.AdjustedAt = time.Now()
	f.byID[a.ID] = a
	f.keys[key] = a.ID
	return a, p.Stock, true, nil
}

type fakeStockReservations struct {
	products    *fakeProducts
	byID        map[string]model.StockReservation
	keys        map[string]string
	createCalls int
	err         error
}

func newFakeStockReservations(ps *fakeProducts) *fakeStockReservations {
	return &fakeStockReservations{products: ps, byID: map[string]model.StockReservation{}, keys: map[string]string{}}
}

func (f *fakeStockReservations) CreateOnce(_ context.Context, key string, r model.StockReservation) (model.StockReservation, bool, error) {
	f.createCalls++
	if f.err != nil {
		return model.StockReservation{}, false, f.err
	}
	if id, ok := f.keys[key]; ok {
		return f.byID[id], false, nil
	}
	// Check every line against the products before changing any of them:
	// the in-memory equivalent of "roll back everything".
	priced := make([]model.ReservationLine, len(r.Lines))
	for i, l := range r.Lines {
		p, ok := f.products.byID[l.ProductID]
		switch {
		case !ok || !p.Active:
			return model.StockReservation{}, false, &out.LineError{Index: i, ProductID: l.ProductID, Err: out.ErrProductUnavailable}
		case p.Stock < l.Quantity:
			return model.StockReservation{}, false, &out.LineError{Index: i, ProductID: l.ProductID, Err: out.ErrInsufficientStock}
		}
		l.UnitPriceCents = p.PriceCents // read now, not earlier
		priced[i] = l
	}
	for _, l := range priced {
		p := f.products.byID[l.ProductID]
		p.Stock -= l.Quantity
		f.products.byID[p.ID] = p
	}
	r.Lines = priced
	r.CreatedAt = time.Now()
	f.byID[r.ID] = r
	f.keys[key] = r.ID
	return r, true, nil
}

func (f *fakeStockReservations) FindByID(_ context.Context, id string) (model.StockReservation, error) {
	if f.err != nil {
		return model.StockReservation{}, f.err
	}
	r, ok := f.byID[id]
	if !ok {
		return model.StockReservation{}, out.ErrNotFound
	}
	return r, nil
}

func (f *fakeStockReservations) Release(_ context.Context, id string) (model.StockReservation, error) {
	if f.err != nil {
		return model.StockReservation{}, f.err
	}
	r, ok := f.byID[id]
	if !ok {
		return model.StockReservation{}, out.ErrNotFound
	}
	if r.Status == model.StatusReleased {
		return r, nil
	}
	for _, l := range r.Lines {
		p := f.products.byID[l.ProductID]
		p.Stock += l.Quantity
		f.products.byID[p.ID] = p
	}
	now := time.Now()
	r.Status, r.ReleasedAt = model.StatusReleased, &now
	f.byID[id] = r
	return r, nil
}
