package model

import (
	"errors"
	"testing"
)

func TestNewCategory_StartsActive(t *testing.T) {
	c, err := NewCategory("c-1", "Peripherals")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.Active || c.ID != "c-1" || c.Name != "Peripherals" {
		t.Errorf("unexpected category: %+v", c)
	}
}

func TestNewCategory_RejectsEmptyName(t *testing.T) {
	_, err := NewCategory("c-1", "")
	if !errors.Is(err, ErrCategoryNameRequired) {
		t.Fatalf("expected ErrCategoryNameRequired, got %v", err)
	}
}

func TestRename_ChangesName(t *testing.T) {
	c, _ := NewCategory("c-1", "Peripherals")
	if err := c.Rename("Accessories"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Name != "Accessories" {
		t.Errorf("expected Accessories, got %q", c.Name)
	}
}

func TestRename_RejectsEmptyNameAndKeepsTheOldOne(t *testing.T) {
	c, _ := NewCategory("c-1", "Peripherals")
	if err := c.Rename(""); !errors.Is(err, ErrCategoryNameRequired) {
		t.Fatalf("expected ErrCategoryNameRequired, got %v", err)
	}
	if c.Name != "Peripherals" {
		t.Errorf("a rejected rename changed the name to %q", c.Name)
	}
}

func TestCategoryDeactivate_IsIdempotent(t *testing.T) {
	c, _ := NewCategory("c-1", "Peripherals")
	c.Deactivate()
	c.Deactivate()
	if c.Active {
		t.Error("expected category to be inactive")
	}
}
