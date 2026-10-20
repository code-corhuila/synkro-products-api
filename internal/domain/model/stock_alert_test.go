package model

import (
	"errors"
	"testing"
	"time"
)

func TestOpenStockAlert_StartsOpenWithTheStockItWasOpenedAt(t *testing.T) {
	a, err := OpenStockAlert("a1", "p1", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ID != "a1" || a.ProductID != "p1" || a.StockAtOpening != 3 {
		t.Errorf("unexpected alert: %+v", a)
	}
	if a.Status != AlertStatusOpen {
		t.Errorf("status %q, want %q", a.Status, AlertStatusOpen)
	}
	if a.ResolvedAt != nil {
		t.Errorf("an open alert has no resolvedAt, got %v", a.ResolvedAt)
	}
}

func TestOpenStockAlert_AcceptsZeroStock(t *testing.T) {
	if _, err := OpenStockAlert("a1", "p1", 0); err != nil {
		t.Errorf("a product out of stock is the clearest low-stock case, got %v", err)
	}
}

func TestOpenStockAlert_RejectsNegativeStock(t *testing.T) {
	if _, err := OpenStockAlert("a1", "p1", -1); !errors.Is(err, ErrStockAtOpeningNegative) {
		t.Errorf("expected ErrStockAtOpeningNegative, got %v", err)
	}
}

func TestStockAlert_ResolveSetsStatusAndResolvedAt(t *testing.T) {
	open, _ := OpenStockAlert("a1", "p1", 3)
	at := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	resolved := open.Resolve(at)

	if resolved.Status != AlertStatusResolved {
		t.Errorf("status %q, want %q", resolved.Status, AlertStatusResolved)
	}
	if resolved.ResolvedAt == nil || !resolved.ResolvedAt.Equal(at) {
		t.Errorf("resolvedAt %v, want %v", resolved.ResolvedAt, at)
	}
	if resolved.ID != open.ID || resolved.ProductID != open.ProductID || resolved.StockAtOpening != open.StockAtOpening {
		t.Errorf("resolving must change only status and resolvedAt: %+v -> %+v", open, resolved)
	}
	if open.Status != AlertStatusOpen || open.ResolvedAt != nil {
		t.Errorf("Resolve must not mutate its receiver: %+v", open)
	}
}

// RESOLVED is final: resolving again changes nothing, resolvedAt included.
func TestStockAlert_ResolveOnAResolvedAlertChangesNothing(t *testing.T) {
	open, _ := OpenStockAlert("a1", "p1", 3)
	first := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	resolved := open.Resolve(first)

	again := resolved.Resolve(first.Add(24 * time.Hour))

	if again.Status != AlertStatusResolved {
		t.Errorf("status %q, want %q", again.Status, AlertStatusResolved)
	}
	if again.ResolvedAt == nil || !again.ResolvedAt.Equal(first) {
		t.Errorf("resolvedAt moved to %v, want it to stay %v", again.ResolvedAt, first)
	}
}
