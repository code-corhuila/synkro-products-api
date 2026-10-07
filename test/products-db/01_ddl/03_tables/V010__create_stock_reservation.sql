CREATE TABLE products_schema.stock_reservation (
    reservation_id UUID PRIMARY KEY,
    status TEXT NOT NULL
        CONSTRAINT ck_stock_reservation_status CHECK (status IN ('RESERVED', 'RELEASED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at TIMESTAMPTZ,
    -- released_at is NULL exactly when RESERVED, NOT NULL exactly when RELEASED.
    -- Written as two implications so a status outside the set violates only
    -- ck_stock_reservation_status; together they accept the same rows as
    -- (RESERVED AND NULL) OR (RELEASED AND NOT NULL).
    CONSTRAINT ck_stock_reservation_released_at_consistency CHECK (
        (status <> 'RESERVED' OR released_at IS NULL) AND
        (status <> 'RELEASED' OR released_at IS NOT NULL)
    )
);
