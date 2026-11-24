-- +goose Up
INSERT INTO document_subtypes (id, type_code, code, name, business_domain, default_title, body_template, default_importance, default_service_name)
VALUES (
    '10000001-0000-4000-8000-000000000013',
    'приказ',
    'приказ_лс',
    'Приказ по личному составу',
    'общий',
    'Приказ по личному составу',
    'В соответствии с трудовым законодательством приказываю:',
    'обычная',
    ''
)
ON CONFLICT (code) DO NOTHING;

CREATE TEMP TABLE reg_rank AS
WITH base AS (
    SELECT d.id,
           d.organization_code,
           d.created_at,
           CASE
               WHEN d.organization_code ILIKE '%нпо%' OR d.organization_code ILIKE '%турбулент%' THEN 'НПО'
               ELSE 'ОРГ'
           END AS org_mark,
           CASE
               WHEN t.code = 'входящий' THEN 'ВХ'
               WHEN t.code = 'исходящий' THEN 'ИСХ'
               WHEN t.code = 'служебная_записка' THEN 'СЗ'
               WHEN t.code = 'приказ' AND (d.subtype_code = 'приказ_лс' OR d.subtype_code ILIKE '%лич%') THEN 'ЛС'
               WHEN t.code = 'приказ' THEN 'П'
               WHEN t.code = 'распоряжение' THEN 'Р'
               WHEN t.code = 'протокол' THEN 'ПР'
               WHEN t.code = 'поручение' THEN 'ПОР'
               WHEN t.code = 'заявка' THEN 'ЗВ'
               WHEN t.code = 'договор' THEN 'ДГ'
               WHEN t.code = 'допсоглашение' THEN 'ДС'
               WHEN t.code = 'лна' THEN 'ЛНА'
               WHEN t.code = 'тз' THEN 'ТЗ'
               WHEN t.code = 'ткп' THEN 'ТКП'
               WHEN t.code = 'счет' THEN 'СЧ'
               WHEN t.code = 'акт' THEN 'АКТ'
               ELSE 'ДОК'
           END AS mark,
           CASE
               WHEN t.code IN ('входящий', 'исходящий', 'служебная_записка', 'поручение', 'заявка', 'договор', 'счет') THEN 4
               ELSE 3
           END AS width,
           EXTRACT(YEAR FROM d.created_at)::int AS y
    FROM documents d
    JOIN document_types t ON t.id = d.type_id
)
SELECT id, organization_code, org_mark, mark, width, y,
       ROW_NUMBER() OVER (
           PARTITION BY organization_code, mark, y
           ORDER BY created_at, id
       ) AS n
FROM base;

UPDATE documents d
SET reg_number = r.org_mark || '-' || r.mark || '-' || lpad(r.n::text, r.width, '0') || '/' || lpad((r.y % 100)::text, 2, '0'),
    reg_year = r.y
FROM reg_rank r
WHERE d.id = r.id;

DELETE FROM type_number_counters;

INSERT INTO type_number_counters (organization_code, type_code, year, last_value)
SELECT organization_code, mark, y, MAX(n)
FROM reg_rank
GROUP BY organization_code, mark, y;

DROP TABLE reg_rank;

-- +goose Down
DELETE FROM document_subtypes WHERE code = 'приказ_лс';
