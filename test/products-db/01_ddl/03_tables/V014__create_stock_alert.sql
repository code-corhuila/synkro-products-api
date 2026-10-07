CREATE TABLE products_schema.stock_alert (
    alert_id UUID PRIMARY KEY,
    product_id UUID NOT NULL,
    status TEXT NOT NULL
        CONSTRAINT ck_stock_alert_status CHECK (status IN ('OPEN', 'RESOLVED')),
    stock_at_opening INTEGER NOT NULL
        CONSTRAINT ck_stock_alert_stock_at_opening_non_negative CHECK (stock_at_opening >= 0),
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT ck_stock_alert_resolved_at_consistency CHECK (
        (status <> 'OPEN' OR resolved_at IS NULL) AND
        (status <> 'RESOLVED' OR resolved_at IS NOT NULL)
    )
);

-- At most one OPEN alert per product — a domain rule enforced by the
-- database, not merely a performance index. Kept in this migration, same
-- reasoning as HU-PRO-05's active-only unique category name.
CREATE UNIQUE INDEX uq_stock_alert_product_open ON products_schema.stock_alert (product_id) WHERE status = 'OPEN';
