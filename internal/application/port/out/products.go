package out

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

var (
	ErrNotFound = errors.New("not found")

	// ErrIdempotencyKeyReused is returned by CreateOnce when the key was
	// already spent on a different resource type (idempotency_key is one
	// table for every resource in products_schema).
	ErrIdempotencyKeyReused = errors.New("idempotency key already used for a different resource type")
)

type ProductFilter struct {
	Page, Limit int
	CategoryID  *string
	Active      *bool
	Name        *string // partial, case-insensitive
	StockAtMost *int
}

type ProductRepository interface {
	CreateOnce(ctx context.Context, key string, p model.Product) (id string, created bool, err error)
	FindByID(ctx context.Context, id string) (model.Product, error) // ErrNotFound if absent
	List(ctx context.Context, f ProductFilter) (items []model.Product, total int, err error)
	Update(ctx context.Context, p model.Product) error // full row replace, used by both update and deactivate
}
