-- +goose Up
ALTER TABLE document_types
    ADD COLUMN IF NOT EXISTS stream_kind text NOT NULL DEFAULT 'внутренний';

UPDATE document_types SET stream_kind = 'входящий' WHERE code = 'входящий';
UPDATE document_types SET stream_kind = 'исходящий' WHERE code = 'исходящий';

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS body text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS addressee_user_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS counterparty_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS counterparty_inn text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS service_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS due_date date,
    ADD COLUMN IF NOT EXISTS amount numeric(16, 2);

CREATE INDEX IF NOT EXISTS documents_stream_idx ON documents (organization_code, type_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS documents_stream_idx;
ALTER TABLE documents
    DROP COLUMN IF EXISTS amount,
    DROP COLUMN IF EXISTS due_date,
    DROP COLUMN IF EXISTS service_name,
    DROP COLUMN IF EXISTS counterparty_inn,
    DROP COLUMN IF EXISTS counterparty_name,
    DROP COLUMN IF EXISTS addressee_user_id,
    DROP COLUMN IF EXISTS body;
ALTER TABLE document_types
    DROP COLUMN IF EXISTS stream_kind;
