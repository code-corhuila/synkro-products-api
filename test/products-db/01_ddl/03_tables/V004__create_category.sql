CREATE TABLE products_schema.category (
    category_id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true
);

-- Partial unique index: name must be unique among ACTIVE categories only.
-- A deactivated category's name can be reused by a new one.
CREATE UNIQUE INDEX uq_category_name_active ON products_schema.category (name) WHERE active = true;
