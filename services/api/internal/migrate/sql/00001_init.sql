-- +goose Up
CREATE TABLE document_types (
    id uuid PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL
);

CREATE TABLE registration_journals (
    id uuid PRIMARY KEY,
    organization_code text NOT NULL,
    stream_kind text NOT NULL,
    name text NOT NULL,
    prefix text NOT NULL,
    UNIQUE (organization_code, stream_kind)
);

CREATE TABLE documents (
    id uuid PRIMARY KEY,
    type_id uuid NOT NULL REFERENCES document_types (id),
    journal_id uuid REFERENCES registration_journals (id),
    organization_code text NOT NULL,
    status text NOT NULL,
    importance text NOT NULL DEFAULT 'обычная',
    title text NOT NULL,
    reg_number text,
    reg_date timestamptz,
    reg_year int,
    current_version_id uuid,
    author_user_id text NOT NULL,
    customer_contract boolean NOT NULL DEFAULT false,
    onec_sync_status text NOT NULL DEFAULT 'не_требуется',
    route_started boolean NOT NULL DEFAULT false,
    route_instance_id uuid,
    external_order_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX documents_reg_unique
    ON documents (organization_code, journal_id, reg_year, reg_number)
    WHERE reg_number IS NOT NULL;

CREATE INDEX documents_org_created_idx
    ON documents (organization_code, created_at DESC, id DESC);

CREATE TABLE document_versions (
    id uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id),
    number int NOT NULL,
    content_hash text,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_id, number)
);

CREATE TABLE files (
    id uuid PRIMARY KEY,
    version_id uuid NOT NULL REFERENCES document_versions (id),
    document_id uuid NOT NULL REFERENCES documents (id),
    name text NOT NULL,
    size_bytes bigint NOT NULL,
    sha256 text NOT NULL,
    shard_volume int NOT NULL DEFAULT 0,
    storage_key text NOT NULL,
    uploaded_by text NOT NULL,
    uploaded_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE routes (
    id uuid PRIMARY KEY,
    type_id uuid REFERENCES document_types (id),
    document_id uuid REFERENCES documents (id),
    template_id uuid,
    is_template boolean NOT NULL,
    status text NOT NULL
);

CREATE TABLE route_steps (
    id uuid PRIMARY KEY,
    route_id uuid NOT NULL REFERENCES routes (id),
    order_no int NOT NULL,
    parallel_group int,
    join_mode text NOT NULL DEFAULT 'all',
    stop_on_reject boolean NOT NULL DEFAULT true,
    allow_early_sign boolean NOT NULL DEFAULT false,
    per_file_approval boolean NOT NULL DEFAULT false,
    is_mandatory boolean NOT NULL DEFAULT false,
    is_order_locked boolean NOT NULL DEFAULT false,
    kind text NOT NULL,
    is_blocking boolean NOT NULL DEFAULT true,
    sla_enabled boolean NOT NULL DEFAULT true,
    due_days int,
    condition_rule text NOT NULL DEFAULT '',
    assignee_role text,
    assignee_user_id text
);

CREATE TABLE step_tasks (
    id uuid PRIMARY KEY,
    step_id uuid NOT NULL REFERENCES route_steps (id),
    document_id uuid NOT NULL REFERENCES documents (id),
    version_id uuid,
    assignee_user_id text NOT NULL,
    state text NOT NULL,
    started_at timestamptz,
    plan_due_at timestamptz,
    decided_at timestamptz,
    comment text,
    orchestrator_task_id text
);

CREATE INDEX step_tasks_assignee_idx
    ON step_tasks (assignee_user_id, state, started_at DESC, id DESC);

CREATE TABLE audit_log (
    id uuid PRIMARY KEY,
    document_id uuid,
    user_id text NOT NULL,
    at timestamptz NOT NULL DEFAULT now(),
    action text NOT NULL,
    from_state text,
    to_state text,
    reason text,
    payload text
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_log_forbid() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_log_no_update
    BEFORE UPDATE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_forbid();

CREATE TRIGGER audit_log_no_delete
    BEFORE DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_forbid();

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents (id),
    event_type text NOT NULL,
    idempotency_key text NOT NULL UNIQUE,
    status text NOT NULL,
    attempts int NOT NULL DEFAULT 0,
    next_retry_at timestamptz,
    payload text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_keys (
    user_id text NOT NULL,
    idem_key text NOT NULL,
    body_hash text NOT NULL,
    response_code int NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, idem_key)
);

CREATE TABLE journal_counters (
    journal_id uuid NOT NULL REFERENCES registration_journals (id),
    year int NOT NULL,
    last_value int NOT NULL,
    PRIMARY KEY (journal_id, year)
);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE journal_counters;
DROP TABLE idempotency_keys;
DROP TABLE outbox_events;
DROP TABLE step_tasks;
DROP TABLE route_steps;
DROP TABLE routes;
DROP TABLE files;
DROP TABLE document_versions;
DROP TABLE documents;
DROP TABLE registration_journals;
DROP TABLE document_types;
DROP FUNCTION audit_log_forbid();
