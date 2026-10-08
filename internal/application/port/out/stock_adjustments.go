package out

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

var (
	// ErrInsufficientStock: a stock + delta (or stock - quantity) check
	// that has to be made under lock, so it cannot live in the domain.
	ErrInsufficientStock = errors.New("insufficient stock")
	// ErrProductUnavailable: a reservation line names a product that is
	// inactive or does not exist.
	ErrProductUnavailable = errors.New("product is inactive or does not exist")
)

type StockAdjustmentRepository interface {
	// Atomically, under lock: checks product.stock + a.Delta >= 0, applies
	// the delta, inserts the adjustment and its idempotency_key row. On a
	// repeated key, returns the original with created=false and does not
	// re-check or re-apply anything (currentStock is then the product's
	// stock now: the stock "right after" the original is not stored).
	// ErrInsufficientStock if the check fails; ErrNotFound if the product
	// does not exist. The adapter fills AdjustedAt.
	CreateOnce(ctx context.Context, key string, a model.StockAdjustment) (adjustment model.StockAdjustment, currentStock int, created bool, err error)
}
