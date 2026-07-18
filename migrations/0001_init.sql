-- Schema for the demo Order Service's own business database (SQLite).
-- Applied automatically at startup by internal/adapters/sqlite/repository.go
-- (kept here too as the human-readable reference copy).
CREATE TABLE IF NOT EXISTS orders (
    id            TEXT PRIMARY KEY,
    customer_name TEXT NOT NULL,
    item          TEXT NOT NULL,
    quantity      INTEGER NOT NULL,
    amount_cents  INTEGER NOT NULL,
    status        TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
