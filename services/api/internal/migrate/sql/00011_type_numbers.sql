-- +goose Up
CREATE TABLE IF NOT EXISTS type_number_counters (
    organization_code text NOT NULL,
    type_code text NOT NULL,
    year int NOT NULL,
    last_value int NOT NULL,
    PRIMARY KEY (organization_code, type_code, year)
);

WITH ranked AS (
    SELECT d.id,
           t.code AS type_code,
           d.organization_code,
           EXTRACT(YEAR FROM d.created_at)::int AS y,
           ROW_NUMBER() OVER (
               PARTITION BY d.organization_code, t.code, EXTRACT(YEAR FROM d.created_at)
               ORDER BY d.created_at, d.id
           ) AS n
    FROM documents d
    JOIN document_types t ON t.id = d.type_id
    WHERE d.reg_number IS NULL OR d.reg_number = ''
)
UPDATE documents d
SET reg_number = CASE r.type_code
        WHEN 'входящий' THEN 'ВХ'
        WHEN 'исходящий' THEN 'ИСХ'
        WHEN 'служебная_записка' THEN 'СЗ'
        WHEN 'приказ' THEN 'ПР'
        WHEN 'распоряжение' THEN 'РСП'
        WHEN 'поручение' THEN 'ПОР'
        WHEN 'протокол' THEN 'ПРТ'
        WHEN 'заявка' THEN 'ЗВ'
        WHEN 'договор' THEN 'ДГ'
        WHEN 'допсоглашение' THEN 'ДС'
        WHEN 'лна' THEN 'ЛНА'
        WHEN 'тз' THEN 'ТЗ'
        WHEN 'ткп' THEN 'ТКП'
        WHEN 'счет' THEN 'СЧ'
        WHEN 'акт' THEN 'АКТ'
        ELSE 'ДОК'
    END || '-' || r.y || '-' || r.n,
    reg_year = r.y,
    reg_date = COALESCE(d.reg_date, d.created_at)
FROM ranked r
WHERE d.id = r.id;

INSERT INTO type_number_counters (organization_code, type_code, year, last_value)
SELECT d.organization_code, t.code, d.reg_year, MAX(split_part(d.reg_number, '-', 3)::int)
FROM documents d
JOIN document_types t ON t.id = d.type_id
WHERE d.reg_number IS NOT NULL AND d.reg_year IS NOT NULL AND split_part(d.reg_number, '-', 3) ~ '^[0-9]+$'
GROUP BY d.organization_code, t.code, d.reg_year
ON CONFLICT (organization_code, type_code, year)
DO UPDATE SET last_value = GREATEST(type_number_counters.last_value, EXCLUDED.last_value);

-- +goose Down
DROP TABLE IF EXISTS type_number_counters;
