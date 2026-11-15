package persistence

import (
	"context"
	"errors"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
	"github.com/jackc/pgx/v5"
)

type memoryProductUpdates struct{ m *Memory }
type postgresProductUpdates struct{ db *Postgres }

func (m *Memory) ProductUpdates() out.ProductUpdateUnit       { return memoryProductUpdates{m} }
func (m *Memory) PriceHistory() out.PriceHistoryRepository    { return memoryProductUpdates{m} }
func (db *Postgres) ProductUpdates() out.ProductUpdateUnit    { return postgresProductUpdates{db} }
func (db *Postgres) PriceHistory() out.PriceHistoryRepository { return postgresProductUpdates{db} }

func validatePriceDecision(current, next model.Product, h *model.PriceChange) error {
	if next.ID != current.ID {
		return errors.New("catalog update cannot change product identity")
	}
	if current.PriceCents == next.PriceCents {
		if h != nil {
			return model.ErrPriceChangeNoop
		}
		return nil
	}
	if h == nil {
		return errors.New("price update requires history")
	}
	if h.ID == "" || h.ProductID != current.ID || h.OldPriceCents != current.PriceCents || h.NewPriceCents != next.PriceCents {
		return errors.New("price history does not match the update")
	}
	_, err := model.NewPriceChange(h.ID, h.ProductID, h.OldPriceCents, h.NewPriceCents, h.ChangedBy, h.ChangedAt)
	return err
}

func (r memoryProductUpdates) Apply(_ context.Context, id string, decide out.ProductUpdateDecision) (model.Product, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	row, ok := r.m.products[id]
	if !ok {
		return model.Product{}, out.ErrNotFound
	}
	next, h, err := decide(row.v)
	if err != nil {
		return model.Product{}, err
	}
	next.Stock = row.v.Stock
	if err := validatePriceDecision(row.v, next, h); err != nil {
		return model.Product{}, err
	}
	if h != nil {
		for _, events := range r.m.priceHistory {
			for _, existing := range events {
				if existing.ID == h.ID {
					return model.Product{}, errors.New("duplicate price history id")
				}
			}
		}
		h.Sequence = int64(len(r.m.priceHistory[id]) + 1)
		h.ChangedAt = h.ChangedAt.UTC()
		r.m.priceHistory[id] = append(r.m.priceHistory[id], *h)
	}
	row.v = next
	r.m.products[id] = row
	return next, nil
}

func (r memoryProductUpdates) ListByProduct(_ context.Context, id string, page, limit int) ([]model.PriceChange, int, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if _, ok := r.m.products[id]; !ok {
		return nil, 0, out.ErrNotFound
	}
	all := r.m.priceHistory[id]
	total := len(all)
	items := []model.PriceChange{}
	start := offset(page, limit)
	if start < 0 || limit < 1 {
		return nil, 0, errors.New("invalid history page")
	}
	for i := total - 1 - start; i >= 0 && len(items) < limit; i-- {
		items = append(items, all[i])
	}
	return items, total, nil
}

func (r postgresProductUpdates) Apply(ctx context.Context, id string, decide out.ProductUpdateDecision) (model.Product, error) {
	if !validUUID(id) {
		return model.Product{}, out.ErrNotFound
	}
	tx, err := r.db.pool.Begin(ctx)
	if err != nil {
		return model.Product{}, err
	}
	defer tx.Rollback(ctx)
	current, err := scanProduct(tx.QueryRow(ctx, `SELECT `+productColumns+` FROM products_schema.product WHERE product_id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Product{}, out.ErrNotFound
	}
	if err != nil {
		return model.Product{}, err
	}
	next, h, err := decide(current)
	if err != nil {
		return model.Product{}, err
	}
	next.Stock = current.Stock
	if err := validatePriceDecision(current, next, h); err != nil {
		return model.Product{}, err
	}
	if next != current {
		_, err = tx.Exec(ctx, `UPDATE products_schema.product SET name=$2,price_cents=$3,category_id=$4,active=$5 WHERE product_id=$1`, id, next.Name, next.PriceCents, next.CategoryID, next.Active)
		if err != nil {
			return model.Product{}, err
		}
	}
	if h != nil {
		_, err = tx.Exec(ctx, `INSERT INTO products_schema.product_price_history
   (history_id,product_id,sequence_no,old_price_cents,new_price_cents,changed_by,changed_at)
   SELECT $1,$2,coalesce(max(sequence_no),0)+1,$3,$4,$5,$6
   FROM products_schema.product_price_history WHERE product_id=$2`, h.ID, id, h.OldPriceCents, h.NewPriceCents, h.ChangedBy, h.ChangedAt.UTC())
		if err != nil {
			return model.Product{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Product{}, err
	}
	return next, nil
}

func (r postgresProductUpdates) ListByProduct(ctx context.Context, id string, page, limit int) ([]model.PriceChange, int, error) {
	if !validUUID(id) {
		return nil, 0, out.ErrNotFound
	}
	if page < 1 || limit < 1 || limit > 100 {
		return nil, 0, errors.New("invalid history page")
	}
	tx, err := r.db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products_schema.product WHERE product_id=$1)`, id).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, out.ErrNotFound
	}
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM products_schema.product_price_history WHERE product_id=$1`, id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT history_id::text,product_id::text,sequence_no,old_price_cents,new_price_cents,changed_by::text,changed_at
  FROM products_schema.product_price_history WHERE product_id=$1 ORDER BY sequence_no DESC LIMIT $2 OFFSET $3`, id, limit, offset(page, limit))
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.PriceChange, error) {
		var h model.PriceChange
		err := row.Scan(&h.ID, &h.ProductID, &h.Sequence, &h.OldPriceCents, &h.NewPriceCents, &h.ChangedBy, &h.ChangedAt)
		h.ChangedAt = h.ChangedAt.UTC()
		return h, err
	})
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []model.PriceChange{}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
