-- +goose Up
INSERT INTO document_types (id, code, name, stream_kind) VALUES
    ('b0000001-0000-4000-8000-000000000010', 'допсоглашение', 'Дополнительное соглашение', 'внутренний'),
    ('b0000001-0000-4000-8000-000000000011', 'лна', 'Внутренний документ (ЛНА)', 'внутренний'),
    ('b0000001-0000-4000-8000-000000000012', 'тз', 'Техническое задание (ТЗ)', 'внутренний'),
    ('b0000001-0000-4000-8000-000000000013', 'ткп', 'Технико-коммерческое предложение (ТКП)', 'исходящий'),
    ('b0000001-0000-4000-8000-000000000014', 'счет', 'Счёт на оплату', 'входящий'),
    ('b0000001-0000-4000-8000-000000000015', 'акт', 'Акт / Рекламация', 'внутренний')
ON CONFLICT (code) DO NOTHING;

INSERT INTO routes (id, type_id, is_template, status) VALUES
    ('a0000001-0000-4000-8000-000000000010', 'b0000001-0000-4000-8000-000000000010', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000011', 'b0000001-0000-4000-8000-000000000011', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000012', 'b0000001-0000-4000-8000-000000000012', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000013', 'b0000001-0000-4000-8000-000000000013', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000014', 'b0000001-0000-4000-8000-000000000014', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000015', 'b0000001-0000-4000-8000-000000000015', true, 'действует')
ON CONFLICT (id) DO NOTHING;

INSERT INTO route_steps (
    id, route_id, order_no, parallel_group, kind, is_blocking, is_mandatory, is_order_locked, sla_enabled, due_days, condition_rule, assignee_role
) VALUES
    -- допсоглашение
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 1, 1, 'approval', true, true, false, true, 2, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 2, NULL, 'signing', true, true, true, true, 2, '', 'signer'),
    -- лна
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 1, 1, 'approval', true, false, false, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 2, NULL, 'signing', true, true, true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 3, NULL, 'acknowledgment', false, false, false, false, 0, '', 'observer'),
    -- тз
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000012', 1, NULL, 'approval', true, false, false, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000012', 2, NULL, 'signing', true, true, true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000012', 3, NULL, 'execution', true, false, false, true, 5, '', 'executor'),
    -- ткп
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000013', 1, NULL, 'approval', true, false, false, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000013', 2, NULL, 'signing', true, true, true, true, 1, '', 'signer'),
    -- счет
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000014', 1, NULL, 'approval', true, true, false, true, 1, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000014', 2, NULL, 'execution', true, false, false, true, 2, '', 'executor'),
    -- акт
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000015', 1, NULL, 'approval', true, false, false, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000015', 2, NULL, 'signing', true, true, true, true, 2, '', 'signer');

CREATE TABLE document_subtypes (
    id uuid PRIMARY KEY,
    type_code text NOT NULL REFERENCES document_types (code),
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    business_domain text NOT NULL DEFAULT 'общий', -- продажи | закупки | производство | кб | качество | общий
    default_title text NOT NULL,
    body_template text NOT NULL,
    default_importance text NOT NULL DEFAULT 'обычная',
    default_service_name text NOT NULL DEFAULT ''
);

INSERT INTO document_subtypes (id, type_code, code, name, business_domain, default_title, body_template, default_importance, default_service_name) VALUES
    ('10000001-0000-4000-8000-000000000001', 'договор', 'договор_покупатель', 'Договор с покупателем (продажа)', 'продажи', 'Договор поставки продукции заказчику', 'Предмет договора: поставка серийной/заказной продукции согласно спецификации №1.', 'обычная', ''),
    ('10000001-0000-4000-8000-000000000002', 'договор', 'договор_поставка_тмц', 'Договор закупки / поставки ТМЦ', 'закупки', 'Договор закупки комплектующих и материалов', 'Предмет договора: закупка ТМЦ для обеспечения производственной программы.', 'обычная', ''),
    ('10000001-0000-4000-8000-000000000003', 'ткп', 'ткп_заказчику', 'ТКП заказчику', 'продажи', 'Технико-коммерческое предложение на поставку', 'Предлагаем рассмотреть технико-коммерческое предложение со сроком поставки и условиями оплаты.', 'повышенная', ''),
    ('10000001-0000-4000-8000-000000000004', 'тз', 'тз_кб', 'ТЗ на разработку / КД', 'кб', 'Техническое задание на проектирование узла', 'Исходные требования, габариты, нормативы приёмки и состав конструкторской документации.', 'обычная', ''),
    ('10000001-0000-4000-8000-000000000005', 'акт', 'акт_несоответствия', 'Акт несоответствия / рекламация', 'качество', 'Акт о выявленном несоответствии качества', 'Описание отклонения, партия, причина, корректирующие мероприятия и ответственные.', 'немедленная', ''),
    ('10000001-0000-4000-8000-000000000006', 'акт', 'акт_выполненных_работ', 'Акт выполненных работ / сдачи-приёмки', 'продажи', 'Акт сдачи-приёмки выполненных работ', 'Работы выполнены в полном объёме, стороны претензий не имеют.', 'обычная', ''),
    ('10000001-0000-4000-8000-000000000007', 'распоряжение', 'распоряжение_производство', 'Производственное распоряжение', 'производство', 'Распоряжение по производственному участку', 'В целях выполнения сменно-суточного задания организовать выпуск партии изделий в срок.', 'повышенная', ''),
    ('10000001-0000-4000-8000-000000000008', 'приказ', 'приказ_основной', 'Приказ по основной деятельности', 'общий', 'Приказ по основной деятельности', 'В целях повышения операционной эффективности приказываю:', 'обычная', ''),
    ('10000001-0000-4000-8000-000000000009', 'заявка', 'заявка_ит', 'Заявка в ИТ-поддержку', 'общий', 'Заявка на настройку доступа / оборудования', 'Прошу предоставить доступ и настроить рабочее место.', 'обычная', 'ИТ-поддержка'),
    ('10000001-0000-4000-8000-000000000010', 'заявка', 'заявка_закупка', 'Заявка на закупку ТМЦ', 'закупки', 'Заявка на закупку материалов для подразделения', 'Прошу организовать закупку номенклатуры согласно перечню.', 'обычная', 'Отдел закупок'),
    ('10000001-0000-4000-8000-000000000011', 'счет', 'счет_поставщика', 'Входящий счёт поставщика', 'закупки', 'Счёт на оплату от поставщика', 'Счёт на оплату по договору поставки.', 'повышенная', ''),
    ('10000001-0000-4000-8000-000000000012', 'лна', 'лна_регламент', 'Регламент / Положение (ЛНА)', 'общий', 'Положение о внутреннем регламенте', 'Настоящее положение определяет порядок взаимодействия подразделений.', 'обычная', '');

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS subtype_code text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS business_domain text NOT NULL DEFAULT 'общий',
    ADD COLUMN IF NOT EXISTS completed_by_author_at timestamptz;

CREATE INDEX IF NOT EXISTS documents_domain_idx ON documents (organization_code, business_domain, created_at DESC, id DESC);

ALTER TABLE step_tasks
    ADD COLUMN IF NOT EXISTS accepted_at timestamptz;

CREATE TABLE user_bookmarks (
    user_id text NOT NULL,
    document_id uuid NOT NULL REFERENCES documents (id),
    note_text text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, document_id)
);

CREATE TABLE document_grants (
    id uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id),
    user_id text NOT NULL,
    granted_by text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX document_grants_lookup_idx ON document_grants (document_id, user_id, expires_at);

-- +goose Down
DROP INDEX IF EXISTS document_grants_lookup_idx;
DROP TABLE IF EXISTS document_grants;
DROP TABLE IF EXISTS user_bookmarks;
ALTER TABLE step_tasks DROP COLUMN IF EXISTS accepted_at;
DROP INDEX IF EXISTS documents_domain_idx;
ALTER TABLE documents
    DROP COLUMN IF EXISTS completed_by_author_at,
    DROP COLUMN IF EXISTS business_domain,
    DROP COLUMN IF EXISTS subtype_code;
DROP TABLE IF EXISTS document_subtypes;
DELETE FROM route_steps WHERE route_id IN (
    'a0000001-0000-4000-8000-000000000010',
    'a0000001-0000-4000-8000-000000000011',
    'a0000001-0000-4000-8000-000000000012',
    'a0000001-0000-4000-8000-000000000013',
    'a0000001-0000-4000-8000-000000000014',
    'a0000001-0000-4000-8000-000000000015'
);
DELETE FROM routes WHERE id IN (
    'a0000001-0000-4000-8000-000000000010',
    'a0000001-0000-4000-8000-000000000011',
    'a0000001-0000-4000-8000-000000000012',
    'a0000001-0000-4000-8000-000000000013',
    'a0000001-0000-4000-8000-000000000014',
    'a0000001-0000-4000-8000-000000000015'
);
DELETE FROM document_types WHERE code IN ('допсоглашение', 'лна', 'тз', 'ткп', 'счет', 'акт');
