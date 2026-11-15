-- Enforce the existing one-product-per-reservation aggregate invariant.
-- No duplicate row is deleted or rewritten by this migration.
SET lock_timeout = '5s';

DO $$
BEGIN
  IF EXISTS (
    SELECT reservation_id, product_id
    FROM products_schema.stock_reservation_line
    GROUP BY reservation_id, product_id
    HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'duplicate reservation/product pairs in stock_reservation_line; stop and agree their treatment before retrying'
      USING ERRCODE = '23505';
  END IF;

  IF to_regclass('products_schema.uq_stock_reservation_line_reservation_product') IS NOT NULL
     AND NOT EXISTS (
       SELECT 1 FROM pg_index i
       JOIN pg_class idx ON idx.oid = i.indexrelid
       JOIN pg_am am ON am.oid = idx.relam
       WHERE i.indexrelid = to_regclass('products_schema.uq_stock_reservation_line_reservation_product')
         AND i.indrelid = 'products_schema.stock_reservation_line'::regclass
         AND i.indisvalid AND i.indisready AND i.indisunique
         AND i.indnkeyatts = 2 AND i.indnatts = 2
         AND i.indpred IS NULL AND i.indexprs IS NULL AND am.amname = 'btree'
         AND pg_get_indexdef(i.indexrelid, 1, true) = 'reservation_id'
         AND pg_get_indexdef(i.indexrelid, 2, true) = 'product_id'
     ) THEN
    RAISE EXCEPTION 'invalid or unexpected uq_stock_reservation_line_reservation_product; inspect it and agree cleanup before retrying; U018 is available for its owned index';
  END IF;
END $$;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_stock_reservation_line_reservation_product
  ON products_schema.stock_reservation_line (reservation_id, product_id);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conrelid = 'products_schema.stock_reservation_line'::regclass
      AND conname = 'uq_stock_reservation_line_reservation_product'
  ) THEN
    ALTER TABLE products_schema.stock_reservation_line
      ADD CONSTRAINT uq_stock_reservation_line_reservation_product
      UNIQUE USING INDEX uq_stock_reservation_line_reservation_product;
  ELSIF NOT EXISTS (
    SELECT 1 FROM pg_constraint c
    WHERE c.conrelid = 'products_schema.stock_reservation_line'::regclass
      AND c.conname = 'uq_stock_reservation_line_reservation_product'
      AND c.contype = 'u' AND NOT c.condeferrable
      AND c.conindid = 'products_schema.uq_stock_reservation_line_reservation_product'::regclass
  ) THEN
    RAISE EXCEPTION 'unexpected uq_stock_reservation_line_reservation_product constraint; inspect before retrying';
  END IF;
END $$;

RESET lock_timeout;
