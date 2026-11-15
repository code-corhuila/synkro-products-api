-- History is evidence: a product that has history can be neither deleted nor
-- re-keyed. RESTRICT, not the default NO ACTION, so the rule holds even in a
-- deferred context and is stated explicitly.
ALTER TABLE products_schema.product_price_history
    ADD CONSTRAINT fk_product_price_history_product
    FOREIGN KEY (product_id) REFERENCES products_schema.product (product_id)
    ON DELETE RESTRICT ON UPDATE RESTRICT;
