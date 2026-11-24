-- +goose Up
ALTER TABLE calendar_events
    ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'event';

ALTER TABLE calendar_events
    DROP CONSTRAINT IF EXISTS calendar_events_kind_check;

ALTER TABLE calendar_events
    ADD CONSTRAINT calendar_events_kind_check CHECK (kind IN ('event', 'meeting'));

-- +goose Down
ALTER TABLE calendar_events DROP CONSTRAINT IF EXISTS calendar_events_kind_check;
ALTER TABLE calendar_events DROP COLUMN IF EXISTS kind;
