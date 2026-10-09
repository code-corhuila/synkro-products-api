package out

import (
	"context"
	"errors"
	"time"

	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

type StockAlertRepository interface {
	// Two distinct idempotency mechanisms, in this order:
	//
	//  1. The idempotency_key row makes an exact retransmission (same key)
	//     return the alert that key produced, created=false, writing
	//     nothing.
	//  2. The partial unique index on stock_alert (product_id) WHERE
	//     status = 'OPEN' is the business rule and the source of truth for
	//     "already exists": whatever the key, if the product already has an
	//     OPEN alert, that alert is returned with created=false and no
	//     second one is inserted. The new key is then spent on it.
	//
	// Otherwise inserts the alert and the key together. ErrNotFound if the
	// product does not exist; ErrIdempotencyKeyReused if the key belongs to
	// another resource type. The adapter fills OpenedAt.
	OpenOnce(ctx context.Context, key string, a model.StockAlert) (alert model.StockAlert, created bool, err error)
	// Atomically, under lock: applies model.StockAlert.Resolve(at). An
	// already-RESOLVED alert is returned untouched (resolvedAt included).
	// ErrNotFound if absent.
	Resolve(ctx context.Context, id string, at time.Time) (model.StockAlert, error)
	// Newest first; status nil means every status.
	List(ctx context.Context, page, limit int, status *string) (items []model.StockAlert, total int, err error)
}

// ErrProductInactive: OpenOnce found the product, but it is inactive.
var ErrProductInactive = errors.New("product is inactive")
