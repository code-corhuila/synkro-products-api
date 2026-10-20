package usecase

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/in"
	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type CategoryService struct {
	categories out.CategoryRepository
	newID      func() string
}

var _ in.CategoryUseCases = (*CategoryService)(nil)

func NewCategoryService(categories out.CategoryRepository, newID func() string) *CategoryService {
	return &CategoryService{categories: categories, newID: newID}
}

// CreateCategory leaves the active-name rule to CreateOnce instead of a
// FindActiveByName pre-check: the key has to be resolved first, or a
// retry of a successful create would collide with the category it made
// and answer 422 instead of replaying. CreateOnce enforces the name rule
// in the same transaction as the insert, so it also closes the
// check-then-insert race.
func (s *CategoryService) CreateCategory(ctx context.Context, cmd in.CreateCategoryCommand) (in.CreateCategoryResult, error) {
	c, err := model.NewCategory(s.newID(), cmd.Name)
	if err != nil {
		return in.CreateCategoryResult{}, err
	}

	id, created, err := s.categories.CreateOnce(ctx, cmd.IdempotencyKey, c)
	switch {
	case errors.Is(err, out.ErrDuplicateActiveName):
		return in.CreateCategoryResult{}, in.ErrCategoryNameTaken
	case errors.Is(err, out.ErrIdempotencyKeyReused):
		return in.CreateCategoryResult{}, in.ErrIdempotencyKeyReused
	case err != nil:
		return in.CreateCategoryResult{}, err
	}
	if created {
		return in.CreateCategoryResult{Category: c, Created: true}, nil
	}

	original, err := s.find(ctx, id)
	if err != nil {
		return in.CreateCategoryResult{}, err
	}
	return in.CreateCategoryResult{Category: original, Created: false}, nil
}

func (s *CategoryService) ListCategories(ctx context.Context, q in.ListCategoriesQuery) (in.ListCategoriesResult, error) {
	items, total, err := s.categories.List(ctx, q.Page, q.Limit, q.Active)
	if err != nil {
		return in.ListCategoriesResult{}, err
	}
	return in.ListCategoriesResult{Items: items, Total: total}, nil
}

func (s *CategoryService) RenameCategory(ctx context.Context, id string, cmd in.RenameCategoryCommand) (in.RenameCategoryResult, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return in.RenameCategoryResult{}, err
	}
	if err := c.Rename(cmd.Name); err != nil {
		return in.RenameCategoryResult{}, err
	}

	// uq_category_name_active only covers active rows: an inactive
	// category can never collide, and an active one never collides with
	// itself.
	if c.Active {
		holder, found, err := s.categories.FindActiveByName(ctx, c.Name)
		if err != nil {
			return in.RenameCategoryResult{}, err
		}
		if found && holder.ID != c.ID {
			return in.RenameCategoryResult{}, in.ErrCategoryNameTaken
		}
	}

	if err := s.categories.Update(ctx, c); err != nil {
		if errors.Is(err, out.ErrDuplicateActiveName) {
			return in.RenameCategoryResult{}, in.ErrCategoryNameTaken
		}
		return in.RenameCategoryResult{}, notFoundAs(err, in.ErrCategoryNotFound)
	}
	return in.RenameCategoryResult{Category: c}, nil
}

// DeactivateCategory is idempotent: an already-inactive category is
// returned as it is, with no write and no product check.
func (s *CategoryService) DeactivateCategory(ctx context.Context, id string) (in.DeactivateCategoryResult, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return in.DeactivateCategoryResult{}, err
	}
	if !c.Active {
		return in.DeactivateCategoryResult{Category: c}, nil
	}

	busy, err := s.categories.HasActiveProducts(ctx, c.ID)
	if err != nil {
		return in.DeactivateCategoryResult{}, err
	}
	if busy {
		return in.DeactivateCategoryResult{}, in.ErrCategoryHasActiveProducts
	}

	c.Deactivate()
	if err := s.categories.Update(ctx, c); err != nil {
		return in.DeactivateCategoryResult{}, notFoundAs(err, in.ErrCategoryNotFound)
	}
	return in.DeactivateCategoryResult{Category: c}, nil
}

func (s *CategoryService) find(ctx context.Context, id string) (model.Category, error) {
	c, err := s.categories.FindByID(ctx, id)
	if err != nil {
		return model.Category{}, notFoundAs(err, in.ErrCategoryNotFound)
	}
	return c, nil
}
