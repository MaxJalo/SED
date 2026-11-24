-- +goose Up
-- Базовые маршруты концепций юридического лица и группы компаний (ред. 03).
-- Шаги только из примитивов, которые движок умеет завершать.
-- Регистрация номера остаётся действием канцелярии в карточке.
-- Межкорпоративный односторонний документ — исходящий у стороны А и входящий у стороны Б.
-- Двусторонний документ — договор: у каждой стороны свой подписант в своей карточке.

DELETE FROM route_steps rs
WHERE rs.route_id IN (
    SELECT id FROM routes
    WHERE is_template = true
      AND id IN (
        'a0000001-0000-4000-8000-000000000001',
        'a0000001-0000-4000-8000-000000000002',
        'a0000001-0000-4000-8000-000000000003',
        'a0000001-0000-4000-8000-000000000004',
        'a0000001-0000-4000-8000-000000000005',
        'a0000001-0000-4000-8000-000000000006',
        'a0000001-0000-4000-8000-000000000007',
        'a0000001-0000-4000-8000-000000000008',
        'a0000001-0000-4000-8000-000000000009',
        'a0000001-0000-4000-8000-000000000010',
        'a0000001-0000-4000-8000-000000000011',
        'a0000001-0000-4000-8000-000000000012',
        'a0000001-0000-4000-8000-000000000013',
        'a0000001-0000-4000-8000-000000000014',
        'a0000001-0000-4000-8000-000000000015'
      )
)
AND NOT EXISTS (SELECT 1 FROM step_tasks t WHERE t.step_id = rs.id);

INSERT INTO route_steps (
    id, route_id, order_no, parallel_group, kind, is_blocking, sla_enabled, due_days, condition_rule, assignee_role
) VALUES
    -- входящий: регистратор → резолюция → поручение или к сведению
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 1, NULL, 'review', true, true, 1, '', 'clerk'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 2, NULL, 'resolution', true, true, 1, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 3, 3, 'execution', true, true, 10, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 3, 3, 'acknowledgment', false, false, 3, '', 'observer'),
    -- исходящий: исполнитель → руководитель → профильная проверка → подписант → отправка
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000002', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000002', 2, NULL, 'approval', true, true, 2, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000002', 3, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000002', 4, NULL, 'review', true, true, 1, '', 'clerk'),
    -- служебная записка: профильная проверка → адресат → поручение
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000003', 1, NULL, 'approval', true, true, 2, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000003', 2, NULL, 'resolution', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000003', 3, NULL, 'execution', true, true, 5, '', 'executor'),
    -- приказ: инициатор → руководитель → подразделения → юрист → регистратор → подписант → ознакомление → исполнение
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 2, 2, 'approval', true, true, 2, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 3, NULL, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 4, NULL, 'review', true, true, 2, '', 'clerk'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 5, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 6, NULL, 'acknowledgment', true, true, 3, '', 'observer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 7, NULL, 'execution', true, true, 5, '', 'executor'),
    -- распоряжение: тот же маршрут, что у приказа
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 2, 2, 'approval', true, true, 2, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 3, NULL, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 4, NULL, 'review', true, true, 2, '', 'clerk'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 5, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 6, NULL, 'acknowledgment', true, true, 3, '', 'observer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 7, NULL, 'execution', true, true, 5, '', 'executor'),
    -- поручение: исполнение, приёмку делает автор через подтверждение результата
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000006', 1, NULL, 'execution', true, true, 5, '', 'executor'),
    -- протокол: председатель → доведение → поручения
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000007', 1, NULL, 'signing', true, true, 1, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000007', 2, NULL, 'acknowledgment', true, true, 1, '', 'observer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000007', 3, NULL, 'execution', true, true, 5, '', 'executor'),
    -- заявка: короткий внутренний маршрут, в концепциях отдельного класса нет
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000008', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000008', 2, NULL, 'execution', true, true, 3, '', 'executor'),
    -- договор: предмет, юрист и финансы параллельно → лимит → подписант → исполнение
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 2, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 3, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 4, NULL, 'execution', true, true, 10, '', 'executor'),
    -- допсоглашение: тот же цикл, что у договора
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 2, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 3, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000010', 4, NULL, 'execution', true, true, 10, '', 'executor'),
    -- ЛНА: владелец → профильные и юрист параллельно → кадры → утверждение → ознакомление
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 1, NULL, 'approval', true, true, 5, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 2, 2, 'approval', true, true, 5, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 2, 2, 'approval', true, true, 5, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 3, NULL, 'approval', true, true, 5, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 4, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000011', 5, NULL, 'acknowledgment', true, true, 3, '', 'observer'),
    -- ТЗ и проектные: инициатор проекта → проектные роли → утверждение → приёмка
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000012', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000012', 2, NULL, 'approval', true, true, 5, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000012', 3, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000012', 4, NULL, 'execution', true, true, 5, '', 'executor'),
    -- ТКП идёт как исходящий
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000013', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000013', 2, NULL, 'approval', true, true, 2, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000013', 3, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000013', 4, NULL, 'review', true, true, 1, '', 'clerk'),
    -- бухгалтерский первичный: проверка реквизитов → отражение
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000014', 1, NULL, 'review', true, true, 2, '', 'clerk'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000014', 2, NULL, 'execution', true, true, 2, '', 'executor'),
    -- акт: как двусторонний документ группы, одна сторона в карточке
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000015', 1, NULL, 'approval', true, true, 2, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000015', 2, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000015', 3, NULL, 'execution', true, true, 5, '', 'executor');

CREATE TABLE role_grants (
    id uuid PRIMARY KEY,
    user_id text NOT NULL,
    role text NOT NULL,
    organization_code text NOT NULL,
    granted_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, role, organization_code)
);

-- +goose Down
DELETE FROM route_steps WHERE route_id IN (
    'a0000001-0000-4000-8000-000000000001',
    'a0000001-0000-4000-8000-000000000002',
    'a0000001-0000-4000-8000-000000000003',
    'a0000001-0000-4000-8000-000000000004',
    'a0000001-0000-4000-8000-000000000005',
    'a0000001-0000-4000-8000-000000000006',
    'a0000001-0000-4000-8000-000000000007',
    'a0000001-0000-4000-8000-000000000008',
    'a0000001-0000-4000-8000-000000000009',
    'a0000001-0000-4000-8000-000000000010',
    'a0000001-0000-4000-8000-000000000011',
    'a0000001-0000-4000-8000-000000000012',
    'a0000001-0000-4000-8000-000000000013',
    'a0000001-0000-4000-8000-000000000014',
    'a0000001-0000-4000-8000-000000000015'
);
DROP TABLE role_grants;
