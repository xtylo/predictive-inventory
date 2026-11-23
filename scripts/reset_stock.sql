-- Load-test reset: put every seeded SKU back to 1000 units, so repeated
-- runs start from the same state. Sales history is kept.
UPDATE inventory
SET stock = 1000, updated_at = now()
WHERE sku LIKE 'SKU-%';
