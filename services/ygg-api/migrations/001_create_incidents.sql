-- +goose Up
-- Incidentes criados a partir do TriageResult do muninn (ADR-002, ADR-003).
-- id é sempre gerado aqui; o cliente nunca escolhe.
CREATE TABLE incidents (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NormalizedEvent de origem (ADR-005). UNIQUE garante a idempotência do
    -- POST /api/v1/incidents com entrega at-least-once.
    event_id           uuid NOT NULL UNIQUE,
    title              varchar(256) NOT NULL,
    severity           varchar(16) NOT NULL
        CHECK (severity IN ('unknown', 'info', 'low', 'medium', 'high', 'critical')),
    source             varchar(64) NOT NULL,
    category           varchar(64) NOT NULL,
    engine             varchar(16) NOT NULL CHECK (engine IN ('llm', 'heuristic')),
    model              varchar(128) NOT NULL,
    confidence         double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    rationale          text NOT NULL DEFAULT '',
    recommended_action text NOT NULL DEFAULT '',
    iocs               jsonb NOT NULL DEFAULT '[]',
    raw_event          jsonb NOT NULL DEFAULT '{}',
    verdict            varchar(32) NOT NULL
        CHECK (verdict IN ('open', 'true_positive', 'false_positive', 'review')),
    status             varchar(16) NOT NULL DEFAULT 'open',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    verdict_changed_by text,
    verdict_changed_at timestamptz
);

CREATE INDEX incidents_created_at_idx ON incidents (created_at DESC);

-- +goose Down
DROP TABLE incidents;
