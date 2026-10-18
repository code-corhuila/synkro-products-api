ALTER TABLE products_schema.stock_adjustment
    ADD COLUMN stock_after INTEGER;

ALTER TABLE products_schema.stock_adjustment
    ADD CONSTRAINT ck_stock_adjustment_stock_after_non_negative
    CHECK (stock_after IS NULL OR stock_after >= 0);
