CREATE TABLE products_schema.idempotency_key (
    idempotency_key TEXT PRIMARY KEY
        CONSTRAINT ck_idempotency_key_length CHECK (char_length(idempotency_key) BETWEEN 8 AND 128),
    resource_type TEXT NOT NULL
        CONSTRAINT ck_idempotency_key_resource_type CHECK (resource_type IN ('PRODUCT','CATEGORY','STOCK_ADJUSTMENT','STOCK_RESERVATION','STOCK_ALERT')),
    resource_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
