CREATE TABLE products_schema.product (
    product_id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    price_cents BIGINT NOT NULL CONSTRAINT ck_product_price_positive CHECK (price_cents > 0),
    stock INTEGER NOT NULL DEFAULT 0 CONSTRAINT ck_product_stock_non_negative CHECK (stock >= 0),
    category_id UUID NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true
);
