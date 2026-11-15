-- The table inherited SELECT, INSERT, UPDATE for products_writer (V003) and
-- SELECT for products_reader (V020) as a default privilege. An audit table is
-- append-only: take back everything but SELECT and INSERT from the writer, and
-- keep the reader at SELECT. Only this table is touched; the defaults stay.
REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER
  ON TABLE products_schema.product_price_history FROM products_writer;
GRANT SELECT, INSERT
  ON TABLE products_schema.product_price_history TO products_writer;

REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER
  ON TABLE products_schema.product_price_history FROM products_reader;
GRANT SELECT
  ON TABLE products_schema.product_price_history TO products_reader;
