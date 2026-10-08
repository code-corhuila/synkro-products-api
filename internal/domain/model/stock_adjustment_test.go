package model

import (
	"errors"
	"testing"
)

func TestNewStockAdjustment_AcceptsPositiveAndNegativeDeltas(t *testing.T) {
	for _, delta := range []int{5, -3} {
		a, err := NewStockAdjustment("adj-1", "prod-1", delta, "Damaged in shipping", "user-1")
		if err != nil {
			t.Fatalf("delta %d: unexpected error: %v", delta, err)
		}
		want := StockAdjustment{ID: "adj-1", ProductID: "prod-1", Delta: delta, Reason: "Damaged in shipping", AdjustedBy: "user-1"}
		if a != want {
			t.Errorf("got %+v, want %+v", a, want)
		}
	}
}

func TestNewStockAdjustment_RejectsAZeroDelta(t *testing.T) {
	if _, err := NewStockAdjustment("adj-1", "prod-1", 0, "why", "user-1"); !errors.Is(err, ErrDeltaZero) {
		t.Fatalf("expected ErrDeltaZero, got %v", err)
	}
}

// ck_stock_adjustment_reason_not_empty is char_length(trim(reason)) > 0:
// a blank reason that the domain let through would reach the database as
// a CHECK violation, i.e. a 500 instead of a 400.
func TestNewStockAdjustment_RejectsAnEmptyOrBlankReason(t *testing.T) {
	for _, reason := range []string{"", " ", " \t\n "} {
		if _, err := NewStockAdjustment("adj-1", "prod-1", 1, reason, "user-1"); !errors.Is(err, ErrReasonRequired) {
			t.Errorf("reason %q: expected ErrReasonRequired, got %v", reason, err)
		}
	}
}
