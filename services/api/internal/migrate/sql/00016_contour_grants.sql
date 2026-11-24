-- +goose Up
CREATE TABLE contour_grants (
    id uuid PRIMARY KEY,
    user_id text NOT NULL,
    organization_code text NOT NULL,
    business_domain text NOT NULL CHECK (business_domain IN ('общий', 'продажи', 'закупки', 'производство', 'кб', 'качество')),
    access_level text NOT NULL CHECK (access_level IN ('view', 'cards', 'lead', 'manage')),
    granted_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, organization_code, business_domain)
);

CREATE INDEX contour_grants_org_domain_idx ON contour_grants (organization_code, business_domain);
CREATE INDEX contour_grants_user_idx ON contour_grants (user_id, organization_code);

-- +goose Down
DROP TABLE IF EXISTS contour_grants;
