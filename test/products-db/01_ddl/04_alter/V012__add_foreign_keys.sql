ALTER TABLE products_schema.stock_adjustment
    ADD CONSTRAINT fk_stock_adjustment_product
    FOREIGN KEY (product_id) REFERENCES products_schema.product (product_id);

ALTER TABLE products_schema.stock_reservation_line
    ADD CONSTRAINT fk_stock_reservation_line_reservation
    FOREIGN KEY (reservation_id) REFERENCES products_schema.stock_reservation (reservation_id);

ALTER TABLE products_schema.stock_reservation_line
    ADD CONSTRAINT fk_stock_reservation_line_product
    FOREIGN KEY (product_id) REFERENCES products_schema.product (product_id);
