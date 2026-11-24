-- +goose Up
UPDATE documents
SET reg_number = 'НПО' || substring(reg_number FROM 4)
WHERE organization_code = 'ORG-01' AND reg_number LIKE 'ОРГ-%';

-- +goose Down
UPDATE documents
SET reg_number = 'ОРГ' || substring(reg_number FROM 4)
WHERE organization_code = 'ORG-01' AND reg_number LIKE 'НПО-%';
