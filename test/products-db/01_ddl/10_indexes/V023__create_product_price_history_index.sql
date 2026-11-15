-- Serves GET /products/{id}/price-history: newest first, paginated. The index
-- behind uq_product_price_history_product_sequence is ascending; this one
-- states the access path of the read model and survives a change to that
-- constraint.
CREATE INDEX idx_product_price_history_product_sequence
    ON products_schema.product_price_history (product_id, sequence_no DESC);
