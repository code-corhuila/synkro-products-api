package persistence

import (
	"context"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// The stock-alert side of the in-memory store. It enforces what the
// partial unique index enforces in Postgres: at most one OPEN alert per
// product, whatever the Idempotency-Key.

func (m *Memory) StockAlerts() out.StockAlertRepository { return memoryAlerts{m} }

type memoryAlerts struct{ m *Memory }

func (r memoryAlerts) OpenOnce(_ context.Context, key string, a model.StockAlert) (model.StockAlert, bool, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	// An exact retransmission first: the original, as it is now.
	if id, replay, err := r.m.claimKey(key, resourceAlert); err != nil || replay {
		if err != nil {
			return model.StockAlert{}, false, err
		}
		return r.m.alerts[id].v, false, nil
	}
	prod, ok := r.m.products[a.ProductID]
	if !ok {
		return model.StockAlert{}, false, out.ErrNotFound
	}
	if !prod.v.Active {
		return model.StockAlert{}, false, out.ErrProductInactive
	}
	// Then the business rule: an OPEN alert for the product wins, and the
	// new key is spent on it.
	for _, row := range r.m.alerts {
		if row.v.ProductID == a.ProductID && row.v.Status == model.AlertStatusOpen {
			r.m.keys[key] = memKey{resourceAlert, row.v.ID}
			return row.v, false, nil
		}
	}
	a.OpenedAt = time.Now().UTC()
	r.m.seq++
	r.m.alerts[a.ID] = memRow[model.StockAlert]{a, r.m.seq}
	r.m.keys[key] = memKey{resourceAlert, a.ID}
	return a, true, nil
}

func (r memoryAlerts) Resolve(_ context.Context, id string, at time.Time) (model.StockAlert, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	row, ok := r.m.alerts[id]
	if !ok {
		return model.StockAlert{}, out.ErrNotFound
	}
	row.v = row.v.Resolve(at)
	r.m.alerts[id] = row
	return row.v, nil
}

func (r memoryAlerts) List(_ context.Context, pageNum, limit int, status *string) ([]model.StockAlert, int, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	var rows []memRow[model.StockAlert]
	for _, row := range r.m.alerts {
		if status == nil || row.v.Status == *status {
			rows = append(rows, row)
		}
	}
	items, total := page(rows, pageNum, limit)
	return items, total, nil
}
