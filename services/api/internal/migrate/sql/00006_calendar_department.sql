-- +goose Up
ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS addressee_department_code text NOT NULL DEFAULT '';

CREATE TABLE calendar_events (
    id uuid PRIMARY KEY,
    organization_code text NOT NULL,
    owner_user_id text NOT NULL,
    scope text NOT NULL,
    title text NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (scope IN ('personal', 'shared')),
    CHECK (ends_at >= starts_at)
);

CREATE INDEX calendar_events_org_idx
    ON calendar_events (organization_code, starts_at);

-- +goose Down
DROP TABLE IF EXISTS calendar_events;
ALTER TABLE documents DROP COLUMN IF EXISTS addressee_department_code;
