package usecase

import (
	"context"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Like the stock fakes, this one ENFORCES what the real adapter enforces:
// a product has at most one OPEN alert (the partial unique index), and
// that rule is independent of the Idempotency-Key, which only collapses an
// exact retransmission. A fake that just recorded calls would let a use
// case that treated the key as the dedup mechanism pass.
type fakeStockAlerts struct {
	products  *fakeProducts
	byID      map[string]model.StockAlert
	order     []string // insertion order, standing in for UUIDv7 ordering
	keys      map[string]string
	openCalls int
	err       error
}

func newFakeStockAlerts(ps *fakeProducts) *fakeStockAlerts {
	return &fakeStockAlerts{products: ps, byID: map[string]model.StockAlert{}, keys: map[string]string{}}
}

func (f *fakeStockAlerts) openFor(productID string) (model.StockAlert, bool) {
	for _, a := range f.byID {
		if a.ProductID == productID && a.Status == model.AlertStatusOpen {
			return a, true
		}
	}
	return model.StockAlert{}, false
}

func (f *fakeStockAlerts) count(productID, status string) int {
	n := 0
	for _, a := range f.byID {
		if a.ProductID == productID && a.Status == status {
			n++
		}
	}
	return n
}

func (f *fakeStockAlerts) OpenOnce(_ context.Context, key string, a model.StockAlert) (model.StockAlert, bool, error) {
	f.openCalls++
	if f.err != nil {
		return model.StockAlert{}, false, f.err
	}
	// 1. An exact retransmission: same key, original alert, nothing else.
	if id, ok := f.keys[key]; ok {
		return f.byID[id], false, nil
	}
	p, ok := f.products.byID[a.ProductID]
	if !ok {
		return model.StockAlert{}, false, out.ErrNotFound
	}
	if !p.Active {
		return model.StockAlert{}, false, out.ErrProductInactive
	}
	// 2. The business rule: one OPEN alert per product, whatever the key.
	// A new key for it is spent on the existing alert.
	if existing, ok := f.openFor(a.ProductID); ok {
		f.keys[key] = existing.ID
		return existing, false, nil
	}
	a.OpenedAt = time.Now()
	f.byID[a.ID] = a
	f.order = append(f.order, a.ID)
	f.keys[key] = a.ID
	return a, true, nil
}

func (f *fakeStockAlerts) Resolve(_ context.Context, id string, at time.Time) (model.StockAlert, error) {
	if f.err != nil {
		return model.StockAlert{}, f.err
	}
	a, ok := f.byID[id]
	if !ok {
		return model.StockAlert{}, out.ErrNotFound
	}
	a = a.Resolve(at)
	f.byID[id] = a
	return a, nil
}

func (f *fakeStockAlerts) List(_ context.Context, page, limit int, status *string) ([]model.StockAlert, int, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	var all []model.StockAlert
	for i := len(f.order) - 1; i >= 0; i-- { // newest first
		a := f.byID[f.order[i]]
		if status == nil || a.Status == *status {
			all = append(all, a)
		}
	}
	total := len(all)
	start := (page - 1) * limit
	items := []model.StockAlert{}
	for i := start; i < total && i < start+limit; i++ {
		items = append(items, all[i])
	}
	return items, total, nil
}
