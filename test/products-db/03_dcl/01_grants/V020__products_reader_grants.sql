-- Explicitly reviewed business tables; the Flyway ledger is administrative.
GRANT USAGE ON SCHEMA products_schema TO products_reader;
GRANT SELECT ON TABLE
  products_schema.category,
  products_schema.product,
  products_schema.idempotency_key,
  products_schema.stock_adjustment,
  products_schema.stock_reservation,
  products_schema.stock_reservation_line,
  products_schema.stock_alert
TO products_reader;

-- Defaults belong to the effective migration creator, not a hardcoded login.
ALTER DEFAULT PRIVILEGES IN SCHEMA products_schema
  GRANT SELECT ON TABLES TO products_reader;
