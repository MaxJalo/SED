-- +goose Up
-- Перевод таблицы выданных прав role_grants с должностных ролей на чистые права доступа: read, write, edit.
INSERT INTO role_grants (id, user_id, role, organization_code, granted_by, created_at)
SELECT gen_random_uuid(), user_id,
       CASE
           WHEN role IN ('observer', 'controller') THEN 'read'
           WHEN role IN ('author', 'executor') THEN 'write'
           WHEN role IN ('clerk', 'manager') THEN 'edit'
       END,
       organization_code, granted_by, created_at
FROM role_grants
WHERE role IN ('observer', 'controller', 'author', 'executor', 'clerk', 'manager')
ON CONFLICT (user_id, role, organization_code) DO NOTHING;

DELETE FROM role_grants
WHERE role NOT IN ('read', 'write', 'edit');

-- +goose Down
-- Откат оставляет только валидные записи role_grants без обратного угадывания должностных ролей.
SELECT 1;
