-- +goose Up
CREATE INDEX IF NOT EXISTS documents_author_org_idx
    ON documents (organization_code, author_user_id);
CREATE INDEX IF NOT EXISTS documents_addressee_org_idx
    ON documents (organization_code, addressee_user_id);
CREATE INDEX IF NOT EXISTS step_tasks_document_state_idx
    ON step_tasks (document_id, state);
CREATE INDEX IF NOT EXISTS step_tasks_state_due_idx
    ON step_tasks (state, plan_due_at)
    WHERE plan_due_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS step_tasks_state_due_idx;
DROP INDEX IF EXISTS step_tasks_document_state_idx;
DROP INDEX IF EXISTS documents_addressee_org_idx;
DROP INDEX IF EXISTS documents_author_org_idx;
