package model

import (
	"errors"
	"time"
)

var ErrStockAtOpeningNegative = errors.New("stockAtOpening must not be negative")

const (
	AlertStatusOpen     = "OPEN"
	AlertStatusResolved = "RESOLVED"
)

// StockAlert is a low-stock alert for one product. "At most one OPEN alert
// per product" depends on concurrent state, so it is not checked here: the
// database's partial unique index enforces it and the persistence adapter
// relies on that.
type StockAlert struct {
	ID             string
	ProductID      string
	Status         string // AlertStatusOpen, AlertStatusResolved
	StockAtOpening int
	OpenedAt       time.Time  // set by the store when the row is written
	ResolvedAt     *time.Time // non-nil exactly when Status is AlertStatusResolved
}

func OpenStockAlert(id, productID string, stockAtOpening int) (StockAlert, error) {
	if stockAtOpening < 0 {
		return StockAlert{}, ErrStockAtOpeningNegative
	}
	return StockAlert{ID: id, ProductID: productID, Status: AlertStatusOpen, StockAtOpening: stockAtOpening}, nil
}

// Resolve returns the alert as RESOLVED at the given time. RESOLVED is
// final: an already-resolved alert is returned untouched, resolvedAt
// included, so resolving twice is not an error.
func (a StockAlert) Resolve(at time.Time) StockAlert {
	if a.Status == AlertStatusResolved {
		return a
	}
	a.Status = AlertStatusResolved
	a.ResolvedAt = &at
	return a
}
