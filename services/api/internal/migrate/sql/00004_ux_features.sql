-- +goose Up
CREATE TABLE document_templates (
    id uuid PRIMARY KEY,
    type_code text NOT NULL REFERENCES document_types (code),
    name text NOT NULL,
    default_title text NOT NULL,
    body_template text NOT NULL,
    default_service_name text NOT NULL DEFAULT ''
);

INSERT INTO document_templates (id, type_code, name, default_title, body_template, default_service_name) VALUES
    ('d0000001-0000-4000-8000-000000000001', 'служебная_записка', 'Запрос оборудования / доступа', 'Служебная записка о предоставлении ресурсов', 'Прошу согласовать и обеспечить предоставление оборудования/доступа для выполнения производственной задачи в срок до ...', ''),
    ('d0000001-0000-4000-8000-000000000002', 'служебная_записка', 'Согласование командировки / выезда', 'Служебная записка о служебной поездке', 'Прошу согласовать направление сотрудника на объект для проведения пусконаладочных работ.', ''),
    ('d0000001-0000-4000-8000-000000000003', 'заявка', 'Заявка в ИТ-поддержку', 'Заявка на настройку рабочего места / ПО', 'Прошу настроить доступ к рабочим сервисам и проверить оборудование на рабочем месте.', 'ИТ-поддержка'),
    ('d0000001-0000-4000-8000-000000000004', 'заявка', 'Заявка в АХО', 'Заявка на хозяйственное обслуживание', 'Прошу выполнить ремонт / замену комплектующих в кабинете.', 'АХО и эксплуатация'),
    ('d0000001-0000-4000-8000-000000000005', 'исходящий', 'Ответное письмо контрагенту', 'Ответ на обращение контрагента', 'В ответ на Ваше обращение сообщаем следующее: ...', ''),
    ('d0000001-0000-4000-8000-000000000006', 'договор', 'Типовой договор поставки / услуг', 'Договор поставки оборудования', 'Предмет договора: поставка и пусконаладка оборудования согласно спецификации. Срок исполнения и порядок расчётов указаны в разделе 3.', ''),
    ('d0000001-0000-4000-8000-000000000007', 'приказ', 'Приказ по основной деятельности', 'Приказ о назначении ответственных', 'В целях обеспечения выполнения производственного плана приказываю назначить ответственными следующих сотрудников:', '');

CREATE TABLE document_links (
    id uuid PRIMARY KEY,
    from_document_id uuid NOT NULL REFERENCES documents (id),
    to_document_id uuid NOT NULL REFERENCES documents (id),
    link_type text NOT NULL,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (from_document_id <> to_document_id),
    UNIQUE (from_document_id, to_document_id, link_type)
);

CREATE TABLE deputies (
    id uuid PRIMARY KEY,
    organization_code text NOT NULL,
    user_id text NOT NULL,
    deputy_user_id text NOT NULL,
    valid_from date NOT NULL,
    valid_to date NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (user_id <> deputy_user_id)
);

CREATE TABLE comments_reviews (
    id uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id),
    entry_kind text NOT NULL, -- 'comment' | 'review'
    author_user_id text NOT NULL,
    target_user_id text NOT NULL DEFAULT '',
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE paper_movements (
    id uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id),
    holder_user_id text NOT NULL,
    moved_at timestamptz NOT NULL DEFAULT now(),
    note text NOT NULL DEFAULT '',
    recorded_by text NOT NULL
);

CREATE TABLE case_nomenclature (
    id uuid PRIMARY KEY,
    organization_code text NOT NULL,
    code text NOT NULL,
    title text NOT NULL,
    year int NOT NULL,
    retention_years int NOT NULL,
    storage text NOT NULL,
    UNIQUE (organization_code, code, year)
);

INSERT INTO case_nomenclature (id, organization_code, code, title, year, retention_years, storage) VALUES
    ('e0000001-0000-4000-8000-000000000001', 'ORG-01', '01-05', 'Входящая и исходящая переписка', 2026, 5, 'Шкаф 1, полка 2'),
    ('e0000001-0000-4000-8000-000000000002', 'ORG-01', '02-10', 'Приказы, распоряжения и служебные записки', 2026, 10, 'Шкаф 2, полка 1'),
    ('e0000001-0000-4000-8000-000000000003', 'ORG-01', '03-01', 'Договоры с контрагентами и заказчиками', 2026, 10, 'Сейф 1, полка 1'),
    ('e0000001-0000-4000-8000-000000000004', 'ORG-02', '01-01', 'Общее делопроизводство ORG-02', 2026, 5, 'электронно');

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS case_id uuid REFERENCES case_nomenclature (id);

CREATE TABLE tags (
    id uuid PRIMARY KEY,
    organization_code text NOT NULL,
    scope text NOT NULL, -- 'org' | 'dept'
    department_code text NOT NULL DEFAULT '',
    name text NOT NULL
);

INSERT INTO tags (id, organization_code, scope, department_code, name) VALUES
    ('f0000001-0000-4000-8000-000000000001', 'ORG-01', 'org', '', 'Срочно в работу'),
    ('f0000001-0000-4000-8000-000000000002', 'ORG-01', 'org', '', 'Контроль руководства'),
    ('f0000001-0000-4000-8000-000000000003', 'ORG-01', 'dept', 'TECH', 'Технический отдел (локальный)'),
    ('f0000001-0000-4000-8000-000000000004', 'ORG-01', 'dept', 'SALES', 'Отдел продаж (локальный)');

CREATE TABLE document_tags (
    document_id uuid NOT NULL REFERENCES documents (id),
    tag_id uuid NOT NULL REFERENCES tags (id),
    added_by text NOT NULL,
    PRIMARY KEY (document_id, tag_id)
);

ALTER TABLE step_tasks
    ADD COLUMN IF NOT EXISTS deputy_for_user_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delegated_from_user_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS remark_addressed boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE step_tasks
    DROP COLUMN IF EXISTS remark_addressed,
    DROP COLUMN IF EXISTS delegated_from_user_id,
    DROP COLUMN IF EXISTS deputy_for_user_id;
DROP TABLE IF EXISTS document_tags;
DROP TABLE IF EXISTS tags;
ALTER TABLE documents DROP COLUMN IF EXISTS case_id;
DROP TABLE IF EXISTS case_nomenclature;
DROP TABLE IF EXISTS paper_movements;
DROP TABLE IF EXISTS comments_reviews;
DROP TABLE IF EXISTS deputies;
DROP TABLE IF EXISTS document_links;
DROP TABLE IF EXISTS document_templates;
