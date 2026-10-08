package model

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrDeltaZero      = errors.New("delta must not be zero")
	ErrReasonRequired = errors.New("reason is required")
)

// StockAdjustment is a manual correction of one product's stock. Whether
// stock + Delta stays >= 0 depends on concurrent state, so it is not
// checked here: the persistence adapter does it under lock.
type StockAdjustment struct {
	ID         string
	ProductID  string
	Delta      int
	Reason     string
	AdjustedBy string
	AdjustedAt time.Time // set by the store when the row is written
}

func NewStockAdjustment(id, productID string, delta int, reason, adjustedBy string) (StockAdjustment, error) {
	if delta == 0 {
		return StockAdjustment{}, ErrDeltaZero
	}
	// Blank counts as empty: ck_stock_adjustment_reason_not_empty is
	// char_length(trim(reason)) > 0.
	if strings.TrimSpace(reason) == "" {
		return StockAdjustment{}, ErrReasonRequired
	}
	return StockAdjustment{ID: id, ProductID: productID, Delta: delta, Reason: reason, AdjustedBy: adjustedBy}, nil
}
