-- +goose Up
-- Trilha de auditoria das mudanças de verdict (uma linha por PATCH).
-- Sem ON DELETE CASCADE: apagar o incidente não pode apagar a auditoria.
CREATE TABLE incident_audit (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    incident_id uuid NOT NULL REFERENCES incidents (id),
    old_verdict varchar(32) NOT NULL,
    new_verdict varchar(32) NOT NULL,
    changed_by  text NOT NULL,
    changed_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX incident_audit_incident_idx ON incident_audit (incident_id, changed_at);

-- +goose Down
DROP TABLE incident_audit;
