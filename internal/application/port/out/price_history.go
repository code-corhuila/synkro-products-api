package out

import (
	"context"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// ProductUpdateDecision runs inside the unit of work, on the product exactly
// as it is stored while the unit holds its lock. It returns the product to
// store and, when the price changes, the history entry to append with it.
// Returning an error abandons the unit: nothing is written.
type ProductUpdateDecision func(current model.Product) (next model.Product, priceChange *model.PriceChange, err error)

// ProductUpdateUnit is the unit of work of a catalog update: lock the
// product, decide on the locked state, write the catalog columns and append
// the price-history entry, all or nothing.
//
// Because decide sees the locked row, a concurrent stock change is never
// overwritten (stock is never written here) and a concurrent price change is
// never undone by a write based on a stale read, as a deactivation would be.
type ProductUpdateUnit interface {
	// Apply returns the product as stored afterwards, with the locked stock.
	// Name, price, category and active are written only when next differs
	// from the current product. The entry's Sequence is assigned here: one
	// more than the product's latest, under the lock. A failure to append
	// the entry (a duplicate id, say) rolls the catalog update back.
	// ErrNotFound if the product does not exist.
	Apply(ctx context.Context, id string, decide ProductUpdateDecision) (model.Product, error)
}

type PriceHistoryRepository interface {
	// ListByProduct returns the product's price changes newest first (highest
	// Sequence first) with the total count, from one consistent snapshot.
	// ErrNotFound if the product does not exist.
	ListByProduct(ctx context.Context, productID string, page, limit int) (items []model.PriceChange, total int, err error)
}
