package model

import "errors"

var (
	ErrNameRequired     = errors.New("name is required")
	ErrPriceNotPositive = errors.New("price must be greater than 0")
)

type Product struct {
	ID         string
	Name       string
	PriceCents int64
	Stock      int
	CategoryID string
	Active     bool
}

// NewProduct starts a product with stock 0 — stock only ever changes
// through a reservation or an adjustment (HU-PRO-09), never here.
func NewProduct(id, name string, priceCents int64, categoryID string) (Product, error) {
	if name == "" {
		return Product{}, ErrNameRequired
	}
	if priceCents <= 0 {
		return Product{}, ErrPriceNotPositive
	}
	return Product{ID: id, Name: name, PriceCents: priceCents, CategoryID: categoryID, Active: true}, nil
}

// UpdateDetails never touches Stock — the contract's UpdateProductRequest
// has no stock field at all.
func (p *Product) UpdateDetails(name string, priceCents int64, categoryID string) error {
	if name == "" {
		return ErrNameRequired
	}
	if priceCents <= 0 {
		return ErrPriceNotPositive
	}
	p.Name, p.PriceCents, p.CategoryID = name, priceCents, categoryID
	return nil
}

func (p *Product) Deactivate() { p.Active = false }
