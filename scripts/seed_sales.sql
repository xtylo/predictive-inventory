-- Development seed: 90 days of synthetic sales history for SKU-0 .. SKU-9,
-- so the forecast has patterns to find. Run scripts/seed.sql first.
--
-- Each SKU has a base daily rate, a slow upward trend, a weekend peak and
-- some noise. Safe to run repeatedly: existing rows are skipped.
-- Stock is not decremented: this is history, not live sales.
WITH skus AS (
    SELECT 'SKU-' || n AS sku, 5 + n * 2 AS base
    FROM generate_series(0, 9) AS n
), days AS (
    SELECT (current_date - g) AS day, g AS age
    FROM generate_series(1, 90) AS g
)
INSERT INTO sales (idempotency_key, sku, quantity, sold_at)
SELECT 'seed-' || s.sku || '-' || d.day,
       s.sku,
       GREATEST(1, round(
             s.base
           * (1 + 0.005 * (90 - d.age))                                          -- trend
           * CASE WHEN extract(isodow FROM d.day) IN (6, 7) THEN 1.6 ELSE 1 END  -- weekends
           + (random() * 4 - 2)                                                  -- noise
       ))::int,
       d.day + time '12:00'
FROM skus s
CROSS JOIN days d
ON CONFLICT (idempotency_key) DO NOTHING;
