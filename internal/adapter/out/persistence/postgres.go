package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/code-corhuila/synkro-products-api/internal/application/port/out"
	"github.com/code-corhuila/synkro-products-api/internal/domain/model"
)

// Postgres implements both repositories against products_schema as
// synkro-products-db creates it. Every statement names the schema, so
// nothing depends on the connection's search_path, and nothing here
// needs more than V003's grants (SELECT, INSERT, UPDATE).
type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (db *Postgres) Products() out.ProductRepository    { return pgProducts{db} }
func (db *Postgres) Categories() out.CategoryRepository { return pgCategories{db} }

const (
	pgUniqueViolation    = "23505"
	uqCategoryNameActive = "uq_category_name_active"
)

func isDuplicateActiveName(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == uqCategoryNameActive
}

// validUUID guards every id that reaches a uuid column: a malformed id
// cannot name a row, and sent as-is it would fail with 22P02 instead.
// Only the hyphenated form: uuid.Parse also takes "urn:uuid:…", which
// PostgreSQL does not.
func validUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	_, err := uuid.Parse(id)
	return err == nil
}

// createOnce inserts the idempotency_key row and the resource in one
// transaction. The key goes first, with ON CONFLICT DO NOTHING: a
// concurrent request holding the same key makes this INSERT wait for it
// to finish, then either take over the key (the other rolled back) or
// see it as a replay (the other committed). A replay writes nothing and
// returns the original id; a failed resource insert rolls the key back
// with it.
func (db *Postgres) createOnce(ctx context.Context, key, resourceType, id string, insert func(pgx.Tx) error) (string, bool, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx) // no-op after Commit

	tag, err := tx.Exec(ctx, `
		INSERT INTO products_schema.idempotency_key (idempotency_key, resource_type, resource_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (idempotency_key) DO NOTHING`, key, resourceType, id)
	if err != nil {
		return "", false, fmt.Errorf("claiming idempotency key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var storedType, storedID string
		err := tx.QueryRow(ctx, `
			SELECT resource_type, resource_id::text
			FROM products_schema.idempotency_key
			WHERE idempotency_key = $1`, key).Scan(&storedType, &storedID)
		if err != nil {
			return "", false, fmt.Errorf("reading idempotency key: %w", err)
		}
		if storedType != resourceType {
			return "", false, out.ErrIdempotencyKeyReused
		}
		return storedID, false, nil
	}

	if err := insert(tx); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// offset turns a 1-based page into an OFFSET.
func offset(page, limit int) int { return (page - 1) * limit }

// likeContains builds an ILIKE pattern that matches s literally: without
// escaping, a search for "50%" would also match "500".
func likeContains(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// where accumulates "column op $n" conditions with their arguments.
type where struct {
	conds []string
	args  []any
}

func (w *where) add(cond string, arg any) {
	w.args = append(w.args, arg)
	w.conds = append(w.conds, fmt.Sprintf(cond, len(w.args)))
}

func (w *where) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

// ─── Products ──────────────────────────────────────────────────────────

type pgProducts struct{ db *Postgres }

const productColumns = `product_id::text, name, price_cents, stock, category_id::text, active`

func scanProduct(row pgx.Row) (model.Product, error) {
	var p model.Product
	err := row.Scan(&p.ID, &p.Name, &p.PriceCents, &p.Stock, &p.CategoryID, &p.Active)
	return p, err
}

func (r pgProducts) CreateOnce(ctx context.Context, key string, p model.Product) (string, bool, error) {
	return r.db.createOnce(ctx, key, resourceProduct, p.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO products_schema.product (product_id, name, price_cents, stock, category_id, active)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			p.ID, p.Name, p.PriceCents, p.Stock, p.CategoryID, p.Active)
		return err
	})
}

func (r pgProducts) FindByID(ctx context.Context, id string) (model.Product, error) {
	if !validUUID(id) {
		return model.Product{}, out.ErrNotFound
	}
	p, err := scanProduct(r.db.pool.QueryRow(ctx,
		`SELECT `+productColumns+` FROM products_schema.product WHERE product_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Product{}, out.ErrNotFound
	}
	return p, err
}

// List orders by product_id DESC: ids are UUIDv7, so that is creation
// order, newest first (products_schema has no created_at column).
func (r pgProducts) List(ctx context.Context, f out.ProductFilter) ([]model.Product, int, error) {
	var w where
	if f.CategoryID != nil {
		if !validUUID(*f.CategoryID) {
			return []model.Product{}, 0, nil
		}
		w.add("category_id = $%d", *f.CategoryID)
	}
	if f.Active != nil {
		w.add("active = $%d", *f.Active)
	}
	if f.Name != nil {
		w.add(`name ILIKE $%d ESCAPE '\'`, likeContains(*f.Name))
	}
	if f.StockAtMost != nil {
		w.add("stock <= $%d", *f.StockAtMost)
	}

	// A separate COUNT, not count(*) OVER (): a page past the end has no
	// rows to carry the window total.
	var total int
	if err := r.db.pool.QueryRow(ctx, `SELECT count(*) FROM products_schema.product`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args := append(w.args, f.Limit, offset(f.Page, f.Limit))
	rows, err := r.db.pool.Query(ctx, fmt.Sprintf(
		`SELECT %s FROM products_schema.product%s ORDER BY product_id DESC LIMIT $%d OFFSET $%d`,
		productColumns, w.sql(), len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Product, error) { return scanProduct(row) })
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []model.Product{}
	}
	return items, total, nil
}

// Update writes every catalog column but never stock — see the port.
func (r pgProducts) Update(ctx context.Context, p model.Product) error {
	if !validUUID(p.ID) {
		return out.ErrNotFound
	}
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE products_schema.product
		SET name = $2, price_cents = $3, category_id = $4, active = $5
		WHERE product_id = $1`,
		p.ID, p.Name, p.PriceCents, p.CategoryID, p.Active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return out.ErrNotFound
	}
	return nil
}

// ─── Categories ────────────────────────────────────────────────────────

type pgCategories struct{ db *Postgres }

const categoryColumns = `category_id::text, name, active`

func scanCategory(row pgx.Row) (model.Category, error) {
	var c model.Category
	err := row.Scan(&c.ID, &c.Name, &c.Active)
	return c, err
}

func (r pgCategories) CreateOnce(ctx context.Context, key string, c model.Category) (string, bool, error) {
	id, created, err := r.db.createOnce(ctx, key, resourceCategory, c.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO products_schema.category (category_id, name, active)
			VALUES ($1, $2, $3)`, c.ID, c.Name, c.Active)
		return err
	})
	if isDuplicateActiveName(err) {
		return "", false, out.ErrDuplicateActiveName
	}
	return id, created, err
}

func (r pgCategories) FindByID(ctx context.Context, id string) (model.Category, error) {
	if !validUUID(id) {
		return model.Category{}, out.ErrNotFound
	}
	c, err := scanCategory(r.db.pool.QueryRow(ctx,
		`SELECT `+categoryColumns+` FROM products_schema.category WHERE category_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Category{}, out.ErrNotFound
	}
	return c, err
}

// FindActiveByName compares with plain "=" on the raw name — the same
// equality uq_category_name_active (name) WHERE active = true enforces.
// No lower(), no trim, no ILIKE: any of them would make the application
// reject names the database accepts.
func (r pgCategories) FindActiveByName(ctx context.Context, name string) (model.Category, bool, error) {
	c, err := scanCategory(r.db.pool.QueryRow(ctx,
		`SELECT `+categoryColumns+` FROM products_schema.category WHERE name = $1 AND active = true`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Category{}, false, nil
	}
	if err != nil {
		return model.Category{}, false, err
	}
	return c, true, nil
}

func (r pgCategories) List(ctx context.Context, page, limit int, active *bool) ([]model.Category, int, error) {
	var w where
	if active != nil {
		w.add("active = $%d", *active)
	}

	var total int
	if err := r.db.pool.QueryRow(ctx, `SELECT count(*) FROM products_schema.category`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args := append(w.args, limit, offset(page, limit))
	rows, err := r.db.pool.Query(ctx, fmt.Sprintf(
		`SELECT %s FROM products_schema.category%s ORDER BY category_id DESC LIMIT $%d OFFSET $%d`,
		categoryColumns, w.sql(), len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Category, error) { return scanCategory(row) })
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []model.Category{}
	}
	return items, total, nil
}

func (r pgCategories) Update(ctx context.Context, c model.Category) error {
	if !validUUID(c.ID) {
		return out.ErrNotFound
	}
	tag, err := r.db.pool.Exec(ctx, `
		UPDATE products_schema.category
		SET name = $2, active = $3
		WHERE category_id = $1`, c.ID, c.Name, c.Active)
	if isDuplicateActiveName(err) {
		return out.ErrDuplicateActiveName
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return out.ErrNotFound
	}
	return nil
}

func (r pgCategories) HasActiveProducts(ctx context.Context, categoryID string) (bool, error) {
	if !validUUID(categoryID) {
		return false, nil
	}
	var busy bool
	err := r.db.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM products_schema.product
			WHERE category_id = $1 AND active = true
		)`, categoryID).Scan(&busy)
	return busy, err
}
