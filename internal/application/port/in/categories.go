package in

import (
	"context"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type CreateCategoryCommand struct {
	IdempotencyKey string
	Name           string
}

type CreateCategoryResult struct {
	Category model.Category
	Created  bool // false on an idempotent replay
}

type ListCategoriesQuery struct {
	Page, Limit int
	Active      *bool
}

type ListCategoriesResult struct {
	Items []model.Category
	Total int
}

type RenameCategoryCommand struct {
	Name string
}

type RenameCategoryResult struct {
	Category model.Category
}

type DeactivateCategoryResult struct {
	Category model.Category
}

type CategoryUseCases interface {
	CreateCategory(ctx context.Context, cmd CreateCategoryCommand) (CreateCategoryResult, error)
	ListCategories(ctx context.Context, q ListCategoriesQuery) (ListCategoriesResult, error)
	RenameCategory(ctx context.Context, id string, cmd RenameCategoryCommand) (RenameCategoryResult, error)
	DeactivateCategory(ctx context.Context, id string) (DeactivateCategoryResult, error)
}
