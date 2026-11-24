-- +goose Up
ALTER TABLE calendar_events
    ADD COLUMN IF NOT EXISTS place text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS organizer_user_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS protocol_document_id uuid REFERENCES documents (id);

UPDATE calendar_events SET organizer_user_id = owner_user_id WHERE organizer_user_id = '';

CREATE TABLE work_grids (
    owner_kind text NOT NULL, -- 'document' | 'meeting'
    owner_id uuid NOT NULL,
    columns jsonb NOT NULL DEFAULT '[]'::jsonb,
    rows jsonb NOT NULL DEFAULT '[]'::jsonb,
    updated_by text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_kind, owner_id),
    CHECK (owner_kind IN ('document', 'meeting'))
);

-- +goose Down
DROP TABLE IF EXISTS work_grids;
ALTER TABLE calendar_events
    DROP COLUMN IF EXISTS protocol_document_id,
    DROP COLUMN IF EXISTS organizer_user_id,
    DROP COLUMN IF EXISTS place;
