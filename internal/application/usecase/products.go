package usecase

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type ProductService struct {
	products   out.ProductRepository
	categories out.CategoryRepository
	newID      func() string
}

var _ in.ProductUseCases = (*ProductService)(nil)

// NewProductService takes the id generator so ids are always made here,
// in Go — product_id has no DEFAULT in products_schema.
func NewProductService(products out.ProductRepository, categories out.CategoryRepository, newID func() string) *ProductService {
	return &ProductService{products: products, categories: categories, newID: newID}
}

func (s *ProductService) CreateProduct(ctx context.Context, cmd in.CreateProductCommand) (in.CreateProductResult, error) {
	p, err := model.NewProduct(s.newID(), cmd.Name, cmd.PriceCents, cmd.CategoryID)
	if err != nil {
		return in.CreateProductResult{}, err
	}
	if err := requireActiveCategory(ctx, s.categories, cmd.CategoryID); err != nil {
		return in.CreateProductResult{}, err
	}

	id, created, err := s.products.CreateOnce(ctx, cmd.IdempotencyKey, p)
	if errors.Is(err, out.ErrIdempotencyKeyReused) {
		return in.CreateProductResult{}, in.ErrIdempotencyKeyReused
	}
	if err != nil {
		return in.CreateProductResult{}, err
	}
	if created {
		return in.CreateProductResult{Product: p, Created: true}, nil
	}

	original, err := s.find(ctx, id)
	if err != nil {
		return in.CreateProductResult{}, err
	}
	return in.CreateProductResult{Product: original, Created: false}, nil
}

func (s *ProductService) ListProducts(ctx context.Context, q in.ListProductsQuery) (in.ListProductsResult, error) {
	items, total, err := s.products.List(ctx, out.ProductFilter{
		Page:        q.Page,
		Limit:       q.Limit,
		CategoryID:  q.CategoryID,
		Active:      q.Active,
		Name:        q.Name,
		StockAtMost: q.StockAtMost,
	})
	if err != nil {
		return in.ListProductsResult{}, err
	}
	return in.ListProductsResult{Items: items, Total: total}, nil
}

func (s *ProductService) GetProduct(ctx context.Context, id string) (in.GetProductResult, error) {
	p, err := s.find(ctx, id)
	if err != nil {
		return in.GetProductResult{}, err
	}
	return in.GetProductResult{Product: p}, nil
}

func (s *ProductService) UpdateProduct(ctx context.Context, id string, cmd in.UpdateProductCommand) (in.UpdateProductResult, error) {
	p, err := s.find(ctx, id)
	if err != nil {
		return in.UpdateProductResult{}, err
	}
	if err := p.UpdateDetails(cmd.Name, cmd.PriceCents, cmd.CategoryID); err != nil {
		return in.UpdateProductResult{}, err
	}
	if err := requireActiveCategory(ctx, s.categories, cmd.CategoryID); err != nil {
		return in.UpdateProductResult{}, err
	}
	if err := s.products.Update(ctx, p); err != nil {
		return in.UpdateProductResult{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return in.UpdateProductResult{Product: p}, nil
}

// DeactivateProduct is idempotent: an already-inactive product is
// returned as it is, with no write.
func (s *ProductService) DeactivateProduct(ctx context.Context, id string) (in.DeactivateProductResult, error) {
	p, err := s.find(ctx, id)
	if err != nil {
		return in.DeactivateProductResult{}, err
	}
	if !p.Active {
		return in.DeactivateProductResult{Product: p}, nil
	}
	p.Deactivate()
	if err := s.products.Update(ctx, p); err != nil {
		return in.DeactivateProductResult{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return in.DeactivateProductResult{Product: p}, nil
}

func (s *ProductService) find(ctx context.Context, id string) (model.Product, error) {
	p, err := s.products.FindByID(ctx, id)
	if err != nil {
		return model.Product{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return p, nil
}

// requireActiveCategory is the product-side category rule: a category
// that is missing OR inactive is "not found" (404) — unlike the
// category-side name rule, which is a business-rule violation (422).
func requireActiveCategory(ctx context.Context, categories out.CategoryRepository, id string) error {
	c, err := categories.FindByID(ctx, id)
	if err != nil {
		return notFoundAs(err, in.ErrCategoryNotFound)
	}
	if !c.Active {
		return in.ErrCategoryNotFound
	}
	return nil
}

// notFoundAs swaps the repository's generic ErrNotFound for the use
// case's specific one, and passes every other failure through untouched.
func notFoundAs(err, notFound error) error {
	if errors.Is(err, out.ErrNotFound) {
		return notFound
	}
	return err
}
