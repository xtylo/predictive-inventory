-- +goose Up

-- Current stock per SKU. The CHECK constraint is the last line of defense:
-- even if application code has a bug, PostgreSQL refuses negative stock.
CREATE TABLE inventory (
    sku        TEXT        PRIMARY KEY,
    stock      INTEGER     NOT NULL CHECK (stock >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per sale event. This history feeds the demand forecast.
CREATE TABLE sales (
    id              BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Client-supplied key: a retried request with the same key must not
    -- decrement stock twice. UNIQUE makes PostgreSQL enforce it.
    idempotency_key TEXT        NOT NULL UNIQUE,
    sku             TEXT        NOT NULL REFERENCES inventory (sku),
    quantity        INTEGER     NOT NULL CHECK (quantity > 0),
    sold_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Supports "sales for SKU X in the last N days", the forecast's main query.
CREATE INDEX sales_sku_sold_at_idx ON sales (sku, sold_at DESC);

-- +goose Down
DROP TABLE sales;
DROP TABLE inventory;
