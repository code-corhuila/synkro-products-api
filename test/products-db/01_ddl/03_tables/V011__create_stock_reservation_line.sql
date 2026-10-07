CREATE TABLE products_schema.stock_reservation_line (
    line_id UUID PRIMARY KEY,
    reservation_id UUID NOT NULL,
    product_id UUID NOT NULL,
    quantity INTEGER NOT NULL
        CONSTRAINT ck_stock_reservation_line_quantity_positive CHECK (quantity > 0),
    unit_price_cents BIGINT NOT NULL
        CONSTRAINT ck_stock_reservation_line_price_positive CHECK (unit_price_cents > 0)
);
