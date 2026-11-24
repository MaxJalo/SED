-- +goose Up
INSERT INTO document_types (id, code, name) VALUES
    ('b0000001-0000-4000-8000-000000000001', 'входящий', 'Входящий документ'),
    ('b0000001-0000-4000-8000-000000000002', 'исходящий', 'Исходящее письмо'),
    ('b0000001-0000-4000-8000-000000000003', 'служебная_записка', 'Служебная записка'),
    ('b0000001-0000-4000-8000-000000000004', 'приказ', 'Приказ'),
    ('b0000001-0000-4000-8000-000000000005', 'распоряжение', 'Распоряжение'),
    ('b0000001-0000-4000-8000-000000000006', 'поручение', 'Поручение сервиса'),
    ('b0000001-0000-4000-8000-000000000007', 'протокол', 'Протокол совещания'),
    ('b0000001-0000-4000-8000-000000000008', 'заявка', 'Заявка в службу'),
    ('b0000001-0000-4000-8000-000000000009', 'договор', 'Договор');

INSERT INTO registration_journals (id, organization_code, stream_kind, name, prefix) VALUES
    ('c0000001-0000-4000-8000-000000000011', 'ORG-01', 'входящий', 'Входящие ORG-01', 'ВХ'),
    ('c0000001-0000-4000-8000-000000000012', 'ORG-01', 'исходящий', 'Исходящие ORG-01', 'ИСХ'),
    ('c0000001-0000-4000-8000-000000000013', 'ORG-01', 'внутренний', 'Внутренние ORG-01', 'ВН'),
    ('c0000001-0000-4000-8000-000000000021', 'ORG-02', 'входящий', 'Входящие ORG-02', 'ВХ'),
    ('c0000001-0000-4000-8000-000000000022', 'ORG-02', 'исходящий', 'Исходящие ORG-02', 'ИСХ'),
    ('c0000001-0000-4000-8000-000000000023', 'ORG-02', 'внутренний', 'Внутренние ORG-02', 'ВН');

INSERT INTO routes (id, type_id, is_template, status) VALUES
    ('a0000001-0000-4000-8000-000000000001', 'b0000001-0000-4000-8000-000000000001', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000002', 'b0000001-0000-4000-8000-000000000002', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000003', 'b0000001-0000-4000-8000-000000000003', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000004', 'b0000001-0000-4000-8000-000000000004', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000005', 'b0000001-0000-4000-8000-000000000005', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000006', 'b0000001-0000-4000-8000-000000000006', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000007', 'b0000001-0000-4000-8000-000000000007', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000008', 'b0000001-0000-4000-8000-000000000008', true, 'действует'),
    ('a0000001-0000-4000-8000-000000000009', 'b0000001-0000-4000-8000-000000000009', true, 'действует');

INSERT INTO route_steps (
    id, route_id, order_no, parallel_group, kind, is_blocking, sla_enabled, due_days, condition_rule, assignee_role
) VALUES
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 1, NULL, 'resolution', true, true, 1, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 2, NULL, 'review', true, true, 1, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 3, 3, 'acknowledgment', false, false, 0, '', 'observer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000001', 3, 3, 'execution', true, true, 10, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000002', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000002', 2, NULL, 'approval', true, true, 2, 'linked_contract', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000002', 3, NULL, 'signing', true, true, 2, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000003', 1, NULL, 'approval', true, true, 2, 'addressee_above', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000003', 2, NULL, 'execution', true, true, 5, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 2, NULL, 'signing', true, true, 3, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 3, 3, 'acknowledgment', false, false, 0, '', 'observer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000004', 3, 3, 'execution', true, true, 5, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 1, NULL, 'approval', true, true, 2, 'executors_outside', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 2, NULL, 'signing', true, true, 1, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000005', 3, NULL, 'execution', true, true, 5, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000006', 1, NULL, 'execution', true, true, 5, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000007', 1, NULL, 'signing', true, true, 1, '', 'signer'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000007', 2, NULL, 'execution', true, true, 5, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000008', 1, NULL, 'approval', true, true, 2, '', 'manager'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000008', 2, NULL, 'execution', true, true, 3, '', 'executor'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 1, 1, 'approval', true, true, 3, '', 'approver'),
    (gen_random_uuid(), 'a0000001-0000-4000-8000-000000000009', 2, NULL, 'signing', true, true, 3, '', 'signer');

-- +goose Down
DELETE FROM route_steps;
DELETE FROM routes;
DELETE FROM registration_journals;
DELETE FROM document_types;
