package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

func (db *Postgres) StockAlerts() out.StockAlertRepository { return pgAlerts{db} }

type pgAlerts struct{ db *Postgres }

const (
	// openAttempts bounds the insert-or-find loop in OpenOnce: it only
	// repeats when the OPEN alert it conflicted with is resolved before it
	// can be read, which takes a resolve landing in that exact window.
	openAttempts = 3
)

const alertColumns = `alert_id::text, product_id::text, status, stock_at_opening, opened_at, resolved_at`

func scanAlert(row pgx.Row) (model.StockAlert, error) {
	var a model.StockAlert
	err := row.Scan(&a.ID, &a.ProductID, &a.Status, &a.StockAtOpening, &a.OpenedAt, &a.ResolvedAt)
	return a, err
}

// OpenOnce uses two mechanisms, deliberately kept apart:
//
//  1. createOnce claims the idempotency_key row first. A repeated key
//     returns the alert that key produced and writes nothing.
//  2. The INSERT names the partial unique index as its arbiter
//     (ON CONFLICT (product_id) WHERE status = 'OPEN'). The index, not
//     any check in this code, decides whether the product already has an
//     OPEN alert; if so nothing is inserted, that alert is read back and
//     the new key is pointed at it. Under concurrency a second inserter
//     waits on the first one's index entry, then takes the DO NOTHING
//     branch, so there is no check-then-insert race to lose.
func (r pgAlerts) OpenOnce(ctx context.Context, key string, a model.StockAlert) (model.StockAlert, bool, error) {
	if !validUUID(a.ProductID) {
		return model.StockAlert{}, false, out.ErrNotFound
	}

	resultID := a.ID
	id, claimed, err := r.db.createOnce(ctx, key, resourceAlert, a.ID, func(tx pgx.Tx) error {
		for range openAttempts {
			// The INSERT goes first and the partial unique index decides: a
			// product that already has an OPEN alert yields no row, whatever
			// its state. The SELECT ... FROM product also yields no row for a
			// missing or inactive product, so only a NEW alert needs an active
			// product.
			var openedAt time.Time
			err := tx.QueryRow(ctx, `
				INSERT INTO products_schema.stock_alert (alert_id, product_id, status, stock_at_opening)
				SELECT $1::uuid, p.product_id, $3::text, $4::int
				FROM products_schema.product p
				WHERE p.product_id = $2::uuid AND p.active
				ON CONFLICT (product_id) WHERE status = 'OPEN' DO NOTHING
				RETURNING opened_at`,
				a.ID, a.ProductID, model.AlertStatusOpen, a.StockAtOpening).Scan(&openedAt)
			if err == nil {
				resultID = a.ID
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}

			// Nothing was inserted. If the product has an OPEN alert, that is
			// the answer, active or not.
			var existing string
			err = tx.QueryRow(ctx, `
				SELECT alert_id::text FROM products_schema.stock_alert
				WHERE product_id = $1 AND status = $2`, a.ProductID, model.AlertStatusOpen).Scan(&existing)
			if err == nil {
				resultID = existing
				_, err = tx.Exec(ctx,
					`UPDATE products_schema.idempotency_key SET resource_id = $2 WHERE idempotency_key = $1`, key, existing)
				return err
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}

			// No OPEN alert either: the product is missing or inactive, or its
			// alert was resolved between the two statements (try again).
			var active bool
			err = tx.QueryRow(ctx,
				`SELECT active FROM products_schema.product WHERE product_id = $1`, a.ProductID).Scan(&active)
			if errors.Is(err, pgx.ErrNoRows) {
				return out.ErrNotFound
			}
			if err != nil {
				return err
			}
			if !active {
				return out.ErrProductInactive
			}
		}
		return fmt.Errorf("no OPEN alert could be inserted or found for product %s", a.ProductID)
	})
	if err != nil {
		return model.StockAlert{}, false, err
	}
	if !claimed {
		// A repeated key: the alert it produced, as it is now.
		orig, err := loadAlert(ctx, r.db.pool, id)
		if err != nil {
			return model.StockAlert{}, false, fmt.Errorf("reading the original alert: %w", err)
		}
		return orig, false, nil
	}
	got, err := loadAlert(ctx, r.db.pool, resultID)
	if err != nil {
		return model.StockAlert{}, false, fmt.Errorf("reading the alert: %w", err)
	}
	return got, resultID == a.ID, nil
}

func (r pgAlerts) Resolve(ctx context.Context, id string, at time.Time) (model.StockAlert, error) {
	if !validUUID(id) {
		return model.StockAlert{}, out.ErrNotFound
	}
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return model.StockAlert{}, err
	}
	defer tx.Rollback(ctx) // no-op after Commit

	// Lock the row first: a concurrent resolve waits here, then sees
	// RESOLVED and leaves resolved_at alone.
	current, err := scanAlert(tx.QueryRow(ctx,
		`SELECT `+alertColumns+` FROM products_schema.stock_alert WHERE alert_id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.StockAlert{}, out.ErrNotFound
	}
	if err != nil {
		return model.StockAlert{}, err
	}

	result := current
	if resolved := current.Resolve(at); resolved.Status != current.Status {
		result, err = scanAlert(tx.QueryRow(ctx, `
			UPDATE products_schema.stock_alert
			SET status = $2, resolved_at = $3
			WHERE alert_id = $1
			RETURNING `+alertColumns, id, resolved.Status, resolved.ResolvedAt))
		if err != nil {
			return model.StockAlert{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.StockAlert{}, err
	}
	return result, nil
}

// List orders by alert_id DESC: ids are UUIDv7, so that is creation
// order, newest first (the table has opened_at, but ids are unique and
// the other lists order the same way).
func (r pgAlerts) List(ctx context.Context, page, limit int, status *string) ([]model.StockAlert, int, error) {
	var w where
	if status != nil {
		w.add("status = $%d", *status)
	}

	var total int
	if err := r.db.pool.QueryRow(ctx, `SELECT count(*) FROM products_schema.stock_alert`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args := append(w.args, limit, offset(page, limit))
	rows, err := r.db.pool.Query(ctx, fmt.Sprintf(
		`SELECT %s FROM products_schema.stock_alert%s ORDER BY alert_id DESC LIMIT $%d OFFSET $%d`,
		alertColumns, w.sql(), len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.StockAlert, error) { return scanAlert(row) })
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []model.StockAlert{}
	}
	return items, total, nil
}

func loadAlert(ctx context.Context, q queryer, id string) (model.StockAlert, error) {
	a, err := scanAlert(q.QueryRow(ctx,
		`SELECT `+alertColumns+` FROM products_schema.stock_alert WHERE alert_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.StockAlert{}, out.ErrNotFound
	}
	return a, err
}
