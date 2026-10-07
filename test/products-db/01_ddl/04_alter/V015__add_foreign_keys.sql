ALTER TABLE products_schema.stock_alert
    ADD CONSTRAINT fk_stock_alert_product
    FOREIGN KEY (product_id) REFERENCES products_schema.product (product_id);
