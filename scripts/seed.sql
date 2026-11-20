-- Development seed: SKU-0 .. SKU-99 with 1000 units each.
-- Safe to run repeatedly: existing SKUs are left untouched.
INSERT INTO inventory (sku, stock)
SELECT 'SKU-' || n, 1000
FROM generate_series(0, 99) AS n
ON CONFLICT (sku) DO NOTHING;
