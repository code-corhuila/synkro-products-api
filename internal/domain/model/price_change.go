package model

import (
	"errors"
	"time"
)

var (
	ErrChangedByRequired  = errors.New("the author of a price change is required")
	ErrPriceChangeNoop    = errors.New("a price change needs two different prices")
	ErrPriceChangeInvalid = errors.New("a price change needs positive prices")
)

// PriceChange is one entry of a product's price history: an immutable fact.
// Sequence is the position among the product's changes (1, 2, 3...); the
// store assigns it under the product's lock, so the order never depends on
// clocks.
type PriceChange struct {
	ID            string
	ProductID     string
	Sequence      int64
	OldPriceCents int64
	NewPriceCents int64
	ChangedBy     string
	ChangedAt     time.Time
}

// NewPriceChange records moving a product's price from oldCents to newCents.
// changedBy is the verified identity of the caller. The instant is kept in
// UTC. Sequence stays zero until the store assigns it.
func NewPriceChange(id, productID string, oldCents, newCents int64, changedBy string, at time.Time) (PriceChange, error) {
	if changedBy == "" {
		return PriceChange{}, ErrChangedByRequired
	}
	if oldCents <= 0 || newCents <= 0 {
		return PriceChange{}, ErrPriceChangeInvalid
	}
	if oldCents == newCents {
		return PriceChange{}, ErrPriceChangeNoop
	}
	return PriceChange{
		ID: id, ProductID: productID, OldPriceCents: oldCents, NewPriceCents: newCents,
		ChangedBy: changedBy, ChangedAt: at.UTC(),
	}, nil
}
