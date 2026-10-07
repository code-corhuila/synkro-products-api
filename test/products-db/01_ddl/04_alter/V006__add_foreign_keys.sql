ALTER TABLE products_schema.product
    ADD CONSTRAINT fk_product_category
    FOREIGN KEY (category_id) REFERENCES products_schema.category (category_id);
