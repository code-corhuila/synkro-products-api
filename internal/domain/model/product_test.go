package model

import (
	"errors"
	"testing"
)

func TestNewProduct_StartsActiveWithZeroStock(t *testing.T) {
	p, err := NewProduct("p-1", "Logitech MX Master 3S", 45990000, "c-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Stock != 0 {
		t.Errorf("expected stock 0, got %d", p.Stock)
	}
	if !p.Active {
		t.Error("expected a new product to be active")
	}
	if p.ID != "p-1" || p.Name != "Logitech MX Master 3S" || p.PriceCents != 45990000 || p.CategoryID != "c-1" {
		t.Errorf("fields not set as given: %+v", p)
	}
}

func TestNewProduct_RejectsEmptyName(t *testing.T) {
	_, err := NewProduct("p-1", "", 1000, "c-1")
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestNewProduct_RejectsNonPositivePrice(t *testing.T) {
	for _, price := range []int64{0, -1} {
		_, err := NewProduct("p-1", "Mouse", price, "c-1")
		if !errors.Is(err, ErrPriceNotPositive) {
			t.Errorf("price %d: expected ErrPriceNotPositive, got %v", price, err)
		}
	}
}

func TestUpdateDetails_ChangesCatalogDataButNeverStock(t *testing.T) {
	p, _ := NewProduct("p-1", "Mouse", 1000, "c-1")
	p.Stock = 7 // as if adjusted elsewhere

	if err := p.UpdateDetails("Keyboard", 2500, "c-2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "Keyboard" || p.PriceCents != 2500 || p.CategoryID != "c-2" {
		t.Errorf("details not updated: %+v", p)
	}
	if p.Stock != 7 {
		t.Errorf("UpdateDetails must not touch stock, got %d", p.Stock)
	}
}

func TestUpdateDetails_RejectsInvalidValuesAndLeavesProductUnchanged(t *testing.T) {
	p, _ := NewProduct("p-1", "Mouse", 1000, "c-1")
	before := p

	if err := p.UpdateDetails("", 2500, "c-2"); !errors.Is(err, ErrNameRequired) {
		t.Errorf("expected ErrNameRequired, got %v", err)
	}
	if err := p.UpdateDetails("Keyboard", 0, "c-2"); !errors.Is(err, ErrPriceNotPositive) {
		t.Errorf("expected ErrPriceNotPositive, got %v", err)
	}
	if p != before {
		t.Errorf("a rejected update changed the product: %+v", p)
	}
}

func TestProductDeactivate_IsIdempotent(t *testing.T) {
	p, _ := NewProduct("p-1", "Mouse", 1000, "c-1")
	p.Deactivate()
	p.Deactivate()
	if p.Active {
		t.Error("expected product to be inactive")
	}
}
