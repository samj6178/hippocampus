-- Predictions: the Predictive Memory Engine log.
-- Before each task, PREDICT stores expected outcome.
-- After task, SURPRISE compares and computes prediction error.
-- Only the delta (surprise) is encoded — Shannon-optimal learning.

CREATE TABLE predictions (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_description TEXT NOT NULL,
    task_embedding   vector(768) NOT NULL,
    predicted_outcome TEXT NOT NULL,
    actual_outcome   TEXT,
    prediction_error FLOAT,
    domain           TEXT NOT NULL DEFAULT 'general',
    agent_id         TEXT NOT NULL,
    project_id       UUID REFERENCES projects(id) ON DELETE SET NULL,
    confidence       FLOAT NOT NULL DEFAULT 0.5,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at      TIMESTAMPTZ
);

CREATE INDEX idx_predictions_unresolved ON predictions (agent_id, created_at DESC)
    WHERE resolved_at IS NULL;
CREATE INDEX idx_predictions_domain ON predictions (domain, agent_id);
CREATE INDEX idx_predictions_project ON predictions (project_id, created_at DESC);

CREATE INDEX idx_predictions_embedding ON predictions
    USING hnsw (task_embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 128);
