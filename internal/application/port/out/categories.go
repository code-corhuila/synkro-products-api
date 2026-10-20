package out

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// ErrDuplicateActiveName is returned by CreateOnce and Update when the
// write would give two active categories the same name — the rule
// uq_category_name_active enforces in the database.
var ErrDuplicateActiveName = errors.New("an active category already has this name")

type CategoryRepository interface {
	// CreateOnce resolves the key before anything else: a repeated key
	// returns the original id with created=false even if the name is now
	// taken (by that very category). Only a new key can fail with
	// ErrDuplicateActiveName.
	CreateOnce(ctx context.Context, key string, c model.Category) (id string, created bool, err error)
	FindByID(ctx context.Context, id string) (model.Category, error)                 // ErrNotFound if absent
	FindActiveByName(ctx context.Context, name string) (model.Category, bool, error) // the active-only uniqueness check
	List(ctx context.Context, page, limit int, active *bool) (items []model.Category, total int, err error)
	Update(ctx context.Context, c model.Category) error
	HasActiveProducts(ctx context.Context, categoryID string) (bool, error) // gates deactivation
}
