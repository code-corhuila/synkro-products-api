-- One row per real price change of a product; append-only (see V024).
-- Events of a product are ordered by sequence_no, assigned under the product
-- row lock by the writer, never by changed_at: timestamps can tie or invert
-- relative to commit order.
CREATE TABLE products_schema.product_price_history (
    history_id UUID NOT NULL
        CONSTRAINT pk_product_price_history PRIMARY KEY,
    product_id UUID NOT NULL,
    sequence_no BIGINT NOT NULL
        CONSTRAINT chk_product_price_history_sequence_positive CHECK (sequence_no >= 1),
    old_price_cents BIGINT NOT NULL
        CONSTRAINT chk_product_price_history_old_price_positive CHECK (old_price_cents > 0),
    new_price_cents BIGINT NOT NULL
        CONSTRAINT chk_product_price_history_new_price_positive CHECK (new_price_cents > 0),
    -- A user in Auth: cross-domain, no foreign key by design (as
    -- stock_adjustment.adjusted_by).
    changed_by UUID NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT chk_product_price_history_price_changed CHECK (old_price_cents <> new_price_cents),
    CONSTRAINT uq_product_price_history_product_sequence UNIQUE (product_id, sequence_no)
);
