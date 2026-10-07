package in

import (
	"context"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type CreateProductCommand struct {
	IdempotencyKey string
	Name           string
	PriceCents     int64
	CategoryID     string
}

type CreateProductResult struct {
	Product model.Product
	Created bool // false on an idempotent replay
}

type ListProductsQuery struct {
	Page, Limit int
	CategoryID  *string
	Active      *bool
	Name        *string
	StockAtMost *int
}

type ListProductsResult struct {
	Items []model.Product
	Total int
}

type GetProductResult struct {
	Product model.Product
}

type UpdateProductCommand struct {
	Name       string
	PriceCents int64
	CategoryID string
}

type UpdateProductResult struct {
	Product model.Product
}

type DeactivateProductResult struct {
	Product model.Product
}

type ProductUseCases interface {
	CreateProduct(ctx context.Context, cmd CreateProductCommand) (CreateProductResult, error)
	ListProducts(ctx context.Context, q ListProductsQuery) (ListProductsResult, error)
	GetProduct(ctx context.Context, id string) (GetProductResult, error)
	UpdateProduct(ctx context.Context, id string, cmd UpdateProductCommand) (UpdateProductResult, error)
	DeactivateProduct(ctx context.Context, id string) (DeactivateProductResult, error)
}
