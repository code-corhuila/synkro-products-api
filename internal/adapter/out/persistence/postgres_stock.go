package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// The stock side of the Postgres adapter. Every check that depends on
// concurrent state — enough stock, the product being active, the price —
// is made inside the transaction that changes the stock, on rows locked
// with SELECT … FOR UPDATE, and so re-verified at that moment: under
// READ COMMITTED a locker that had to wait re-reads the row's committed
// latest version once it gets the lock. Nothing a caller read earlier is
// trusted.
//
// Lock order, to keep concurrent transactions from deadlocking: the
// idempotency key (or the reservation row, on release) first, then the
// product rows, always in ascending product_id order. A reservation locks
// all of its products in one ordered statement, so two reservations over
// overlapping products can only queue behind one another, never wait on
// each other in a cycle.

func (db *Postgres) StockAdjustments() out.StockAdjustmentRepository { return pgAdjustments{db} }
func (db *Postgres) StockReservations() out.StockReservationRepository {
	return pgReservations{db}
}

// queryer is what both the pool and a transaction offer.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ─── Adjustments ───────────────────────────────────────────────────────

type pgAdjustments struct{ db *Postgres }

func (r pgAdjustments) CreateOnce(ctx context.Context, key string, a model.StockAdjustment) (model.StockAdjustment, int, bool, error) {
	if !validUUID(a.ProductID) {
		return model.StockAdjustment{}, 0, false, out.ErrNotFound
	}

	var current int
	id, created, err := r.db.createOnce(ctx, key, resourceAdjustment, a.ID, func(tx pgx.Tx) error {
		var stock int
		err := tx.QueryRow(ctx,
			`SELECT stock FROM products_schema.product WHERE product_id = $1 FOR UPDATE`, a.ProductID).Scan(&stock)
		if errors.Is(err, pgx.ErrNoRows) {
			return out.ErrNotFound
		}
		if err != nil {
			return err
		}
		if stock+a.Delta < 0 {
			return out.ErrInsufficientStock
		}
		if err := tx.QueryRow(ctx,
			`UPDATE products_schema.product SET stock = stock + $2 WHERE product_id = $1 RETURNING stock`,
			a.ProductID, a.Delta).Scan(&current); err != nil {
			return err
		}
		// The stock this adjustment leaves is kept with it, so a replay can
		// report it however much the product moves afterwards.
		after := current
		a.StockAfter = &after
		return tx.QueryRow(ctx, `
			INSERT INTO products_schema.stock_adjustment (adjustment_id, product_id, delta, reason, adjusted_by, stock_after)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING adjusted_at`,
			a.ID, a.ProductID, a.Delta, a.Reason, a.AdjustedBy, after).Scan(&a.AdjustedAt)
	})
	if err != nil {
		return model.StockAdjustment{}, 0, false, err
	}
	if created {
		return a, current, true, nil
	}

	// A replay returns the stored adjustment and the stock it left
	// (stock_after), however much the product has moved since.
	var orig model.StockAdjustment
	var productStock int
	err = r.db.pool.QueryRow(ctx, `
		SELECT a.adjustment_id::text, a.product_id::text, a.delta, a.reason, a.adjusted_by::text, a.adjusted_at, a.stock_after, p.stock
		FROM products_schema.stock_adjustment a
		JOIN products_schema.product p ON p.product_id = a.product_id
		WHERE a.adjustment_id = $1`, id).
		Scan(&orig.ID, &orig.ProductID, &orig.Delta, &orig.Reason, &orig.AdjustedBy, &orig.AdjustedAt, &orig.StockAfter, &productStock)
	if err != nil {
		return model.StockAdjustment{}, 0, false, fmt.Errorf("reading the original adjustment: %w", err)
	}
	if orig.StockAfter != nil {
		return orig, *orig.StockAfter, false, nil
	}
	// Backward compatibility ONLY: a row written before migration V017 has no
	// stock_after, and the stock it left cannot be reconstructed. The
	// product's stock now is the closest answer; every row written since
	// V017 takes the branch above.
	return orig, productStock, false, nil
}

// ─── Reservations ──────────────────────────────────────────────────────

type pgReservations struct{ db *Postgres }

type lockedProduct struct {
	price  int64
	stock  int
	active bool
}

func (r pgReservations) CreateOnce(ctx context.Context, key string, res model.StockReservation) (model.StockReservation, bool, error) {
	id, created, err := r.db.createOnce(ctx, key, resourceReservation, res.ID, func(tx pgx.Tx) error {
		products, err := lockProducts(ctx, tx, lineProductIDs(res.Lines))
		if err != nil {
			return err
		}

		// Check and price every line before changing any: the first
		// failure (by line order) rolls back the whole transaction,
		// including the idempotency key.
		priced := make([]model.ReservationLine, len(res.Lines))
		for i, l := range res.Lines {
			p, ok := products[strings.ToLower(l.ProductID)]
			switch {
			case !ok || !p.active:
				return &out.LineError{Index: i, ProductID: l.ProductID, Err: out.ErrProductUnavailable}
			case p.stock < l.Quantity:
				return &out.LineError{Index: i, ProductID: l.ProductID, Err: out.ErrInsufficientStock}
			}
			l.UnitPriceCents = p.price // read under this transaction's lock
			priced[i] = l
		}

		for _, l := range priced {
			if _, err := tx.Exec(ctx,
				`UPDATE products_schema.product SET stock = stock - $2 WHERE product_id = $1`,
				l.ProductID, l.Quantity); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO products_schema.stock_reservation (reservation_id, status)
			VALUES ($1, $2)
			RETURNING created_at`, res.ID, model.StatusReserved).Scan(&res.CreatedAt); err != nil {
			return err
		}
		for _, l := range priced {
			if _, err := tx.Exec(ctx, `
				INSERT INTO products_schema.stock_reservation_line (line_id, reservation_id, product_id, quantity, unit_price_cents)
				VALUES ($1, $2, $3, $4, $5)`,
				l.LineID, res.ID, l.ProductID, l.Quantity, l.UnitPriceCents); err != nil {
				return err
			}
		}
		res.Lines = priced
		return nil
	})
	if err != nil {
		return model.StockReservation{}, false, err
	}
	if created {
		return res, true, nil
	}
	orig, err := r.FindByID(ctx, id)
	if err != nil {
		return model.StockReservation{}, false, fmt.Errorf("reading the original reservation: %w", err)
	}
	return orig, false, nil
}

func (r pgReservations) FindByID(ctx context.Context, id string) (model.StockReservation, error) {
	if !validUUID(id) {
		return model.StockReservation{}, out.ErrNotFound
	}
	return loadReservation(ctx, r.db.pool, id)
}

func (r pgReservations) Release(ctx context.Context, id string) (model.StockReservation, error) {
	if !validUUID(id) {
		return model.StockReservation{}, out.ErrNotFound
	}
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return model.StockReservation{}, err
	}
	defer tx.Rollback(ctx) // no-op after Commit

	// Lock the reservation first: a concurrent release of the same
	// reservation waits here, then sees RELEASED and restores nothing.
	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM products_schema.stock_reservation WHERE reservation_id = $1 FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.StockReservation{}, out.ErrNotFound
	}
	if err != nil {
		return model.StockReservation{}, err
	}

	if status != model.StatusReleased {
		rows, err := tx.Query(ctx,
			`SELECT product_id::text FROM products_schema.stock_reservation_line WHERE reservation_id = $1`, id)
		if err != nil {
			return model.StockReservation{}, err
		}
		productIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return model.StockReservation{}, err
		}
		if _, err := lockProducts(ctx, tx, productIDs); err != nil {
			return model.StockReservation{}, err
		}
		// A product appears at most once per reservation, so this adds
		// each line's quantity back exactly once.
		if _, err := tx.Exec(ctx, `
			UPDATE products_schema.product p
			SET stock = p.stock + l.quantity
			FROM products_schema.stock_reservation_line l
			WHERE l.reservation_id = $1 AND p.product_id = l.product_id`, id); err != nil {
			return model.StockReservation{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE products_schema.stock_reservation
			SET status = $2, released_at = now()
			WHERE reservation_id = $1`, id, model.StatusReleased); err != nil {
			return model.StockReservation{}, err
		}
	}

	res, err := loadReservation(ctx, tx, id)
	if err != nil {
		return model.StockReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.StockReservation{}, err
	}
	return res, nil
}

// lineProductIDs returns the product ids that can name a row; a malformed
// id is left out and so comes back as "not found" for its line.
func lineProductIDs(lines []model.ReservationLine) []string {
	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		if validUUID(l.ProductID) {
			ids = append(ids, l.ProductID)
		}
	}
	return ids
}

// lockProducts locks the given product rows FOR UPDATE in ascending
// product_id order (the sort happens before the locking) and returns
// their state as of the lock, keyed by lowercase id. Ids with no row are
// simply absent.
func lockProducts(ctx context.Context, tx pgx.Tx, ids []string) (map[string]lockedProduct, error) {
	rows, err := tx.Query(ctx, `
		SELECT product_id::text, price_cents, stock, active
		FROM products_schema.product
		WHERE product_id = ANY($1::uuid[])
		ORDER BY product_id
		FOR UPDATE`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]lockedProduct, len(ids))
	for rows.Next() {
		var id string
		var p lockedProduct
		if err := rows.Scan(&id, &p.price, &p.stock, &p.active); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}

// loadReservation reads a reservation and its lines. Lines come back in
// line_id order: the ids are UUIDv7 generated in line order, and the
// table has no position column.
func loadReservation(ctx context.Context, q queryer, id string) (model.StockReservation, error) {
	var res model.StockReservation
	var releasedAt *time.Time
	err := q.QueryRow(ctx, `
		SELECT reservation_id::text, status, created_at, released_at
		FROM products_schema.stock_reservation
		WHERE reservation_id = $1`, id).Scan(&res.ID, &res.Status, &res.CreatedAt, &releasedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.StockReservation{}, out.ErrNotFound
	}
	if err != nil {
		return model.StockReservation{}, err
	}
	res.ReleasedAt = releasedAt

	rows, err := q.Query(ctx, `
		SELECT line_id::text, product_id::text, quantity, unit_price_cents
		FROM products_schema.stock_reservation_line
		WHERE reservation_id = $1
		ORDER BY line_id`, id)
	if err != nil {
		return model.StockReservation{}, err
	}
	res.Lines, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.ReservationLine, error) {
		var l model.ReservationLine
		err := row.Scan(&l.LineID, &l.ProductID, &l.Quantity, &l.UnitPriceCents)
		return l, err
	})
	if err != nil {
		return model.StockReservation{}, err
	}
	return res, nil
}
