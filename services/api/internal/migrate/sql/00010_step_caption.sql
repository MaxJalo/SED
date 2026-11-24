-- +goose Up
ALTER TABLE route_steps
    ADD COLUMN IF NOT EXISTS caption text NOT NULL DEFAULT '';

WITH ranked AS (
    SELECT id, route_id, order_no,
           ROW_NUMBER() OVER (PARTITION BY route_id, order_no ORDER BY id) AS rn
    FROM route_steps
    WHERE route_id IN (
        'a0000001-0000-4000-8000-000000000009',
        'a0000001-0000-4000-8000-000000000010',
        'a0000001-0000-4000-8000-000000000011',
        'a0000001-0000-4000-8000-000000000004',
        'a0000001-0000-4000-8000-000000000005'
    )
)
UPDATE route_steps rs
SET caption = CASE
    WHEN r.route_id IN ('a0000001-0000-4000-8000-000000000009', 'a0000001-0000-4000-8000-000000000010') AND r.order_no = 1 THEN
        CASE r.rn WHEN 1 THEN 'Предмет' WHEN 2 THEN 'Юрист' WHEN 3 THEN 'Финансы' ELSE '' END
    WHEN r.route_id = 'a0000001-0000-4000-8000-000000000011' AND r.order_no = 2 THEN
        CASE r.rn WHEN 1 THEN 'Профильное подразделение' WHEN 2 THEN 'Юрист' ELSE '' END
    WHEN r.route_id IN ('a0000001-0000-4000-8000-000000000004', 'a0000001-0000-4000-8000-000000000005') AND r.order_no = 2 THEN
        'Заинтересованное подразделение'
    WHEN r.route_id IN ('a0000001-0000-4000-8000-000000000004', 'a0000001-0000-4000-8000-000000000005') AND r.order_no = 3 THEN
        'Юрист'
    ELSE rs.caption
END
FROM ranked r
WHERE rs.id = r.id;

-- +goose Down
ALTER TABLE route_steps
    DROP COLUMN IF EXISTS caption;
