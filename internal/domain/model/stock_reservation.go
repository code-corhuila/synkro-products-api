package model

import (
	"errors"
	"time"
)

var (
	ErrNoLines             = errors.New("a reservation needs at least one line")
	ErrDuplicateProduct    = errors.New("a product may appear at most once per reservation")
	ErrQuantityNotPositive = errors.New("quantity must be greater than 0")
)

const (
	StatusReserved = "RESERVED"
	StatusReleased = "RELEASED"
)

type ReservationLine struct {
	LineID         string
	ProductID      string
	Quantity       int
	UnitPriceCents int64 // set by the adapter once the price is actually read, not here
}

// ValidateLines checks only what needs no database access — availability
// is the adapter's job, inside its transaction.
func ValidateLines(lines []ReservationLine) error {
	if len(lines) == 0 {
		return ErrNoLines
	}
	seen := make(map[string]bool, len(lines))
	for _, l := range lines {
		if l.Quantity <= 0 {
			return ErrQuantityNotPositive
		}
		if seen[l.ProductID] {
			return ErrDuplicateProduct
		}
		seen[l.ProductID] = true
	}
	return nil
}

type StockReservation struct {
	ID         string
	Status     string // StatusReserved, StatusReleased
	CreatedAt  time.Time
	ReleasedAt *time.Time // non-nil exactly when Status is StatusReleased
	Lines      []ReservationLine
}

// NewStockReservation starts a RESERVED reservation. Lines carry no price
// yet; the store freezes it from the product row it locks.
func NewStockReservation(id string, lines []ReservationLine) (StockReservation, error) {
	if err := ValidateLines(lines); err != nil {
		return StockReservation{}, err
	}
	return StockReservation{ID: id, Status: StatusReserved, Lines: lines}, nil
}
