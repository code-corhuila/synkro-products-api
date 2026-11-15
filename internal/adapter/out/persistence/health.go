package persistence

import "context"

// Check requires access to the own schema/table without scanning business rows.
// SELECT 1 alone would incorrectly succeed after schema loss or revoked access.
func (db *Postgres) Check(ctx context.Context) error {
	_, err := db.pool.Exec(ctx, `SELECT 1 FROM products_schema.product LIMIT 0`)
	return err
}
