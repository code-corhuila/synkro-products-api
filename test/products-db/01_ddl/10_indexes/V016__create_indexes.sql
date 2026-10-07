-- A plain index, separate from the partial unique one above — that one
-- only covers OPEN rows, so it doesn't substitute for a general
-- FK-lookup index across every row.
CREATE INDEX idx_stock_alert_product_id ON products_schema.stock_alert (product_id);
