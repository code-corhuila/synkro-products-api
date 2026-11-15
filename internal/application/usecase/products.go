package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type ProductService struct {
	products   out.ProductRepository
	categories out.CategoryRepository
	newID      func() string
	updates    out.ProductUpdateUnit
	history    out.PriceHistoryRepository
	now        func() time.Time
}

var _ in.ProductUseCases = (*ProductService)(nil)

// NewProductService takes the id generator so ids are always made here,
// in Go — product_id has no DEFAULT in products_schema.
func NewProductService(products out.ProductRepository, categories out.CategoryRepository, newID func() string, updates out.ProductUpdateUnit, history out.PriceHistoryRepository, now func() time.Time) *ProductService {
	return &ProductService{products: products, categories: categories, newID: newID, updates: updates, history: history, now: now}
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

	updated, err := s.updates.Apply(ctx, id, func(current model.Product) (model.Product, *model.PriceChange, error) {
		next := current
		if err := next.UpdateDetails(cmd.Name, cmd.PriceCents, cmd.CategoryID); err != nil {
			return current, nil, err
		}
		if current.PriceCents == next.PriceCents {
			return next, nil, nil
		}
		h, err := model.NewPriceChange(s.newID(), id, current.PriceCents, next.PriceCents, cmd.ChangedBy, s.now())
		return next, &h, err
	})
	if err != nil {
		return in.UpdateProductResult{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return in.UpdateProductResult{Product: updated}, nil
}

// DeactivateProduct is idempotent: an already-inactive product is
// returned as it is, with no write.
func (s *ProductService) DeactivateProduct(ctx context.Context, id string) (in.DeactivateProductResult, error) {
	p, err := s.updates.Apply(ctx, id, func(current model.Product) (model.Product, *model.PriceChange, error) {
		current.Deactivate()
		return current, nil, nil
	})
	if err != nil {
		return in.DeactivateProductResult{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return in.DeactivateProductResult{Product: p}, nil
}

func (s *ProductService) ListPriceHistory(ctx context.Context, q in.ListPriceHistoryQuery) (in.ListPriceHistoryResult, error) {
	items, total, err := s.history.ListByProduct(ctx, q.ProductID, q.Page, q.Limit)
	if err != nil {
		return in.ListPriceHistoryResult{}, notFoundAs(err, in.ErrProductNotFound)
	}
	return in.ListPriceHistoryResult{Items: items, Total: total}, nil
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
