CREATE TABLE products_schema.stock_adjustment (
    adjustment_id UUID PRIMARY KEY,
    product_id UUID NOT NULL,
    delta INTEGER NOT NULL
        CONSTRAINT ck_stock_adjustment_delta_nonzero CHECK (delta <> 0),
    reason TEXT NOT NULL
        CONSTRAINT ck_stock_adjustment_reason_not_empty CHECK (char_length(trim(reason)) > 0),
    adjusted_by UUID NOT NULL,
    adjusted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
