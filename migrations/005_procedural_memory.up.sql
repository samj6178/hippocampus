-- Procedural memory: versioned action patterns with success tracking.
-- Maps to basal ganglia / cerebellum. Stores tool-use patterns,
-- successful reasoning chains, deploy sequences.

CREATE TABLE procedural_memory (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id   UUID REFERENCES projects(id) ON DELETE SET NULL,
    task_type    TEXT NOT NULL,
    description  TEXT NOT NULL,
    steps        JSONB NOT NULL DEFAULT '[]',
    embedding    vector(768) NOT NULL,
    importance   FLOAT NOT NULL DEFAULT 0.5,
    confidence   FLOAT NOT NULL DEFAULT 1.0,
    success_count INT NOT NULL DEFAULT 0,
    failure_count INT NOT NULL DEFAULT 0,
    success_rate FLOAT GENERATED ALWAYS AS (
        CASE WHEN (success_count + failure_count) = 0 THEN 0
             ELSE success_count::float / (success_count + failure_count)
        END
    ) STORED,
    access_count  INT NOT NULL DEFAULT 0,
    last_accessed TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used    TIMESTAMPTZ,
    token_count  INT NOT NULL DEFAULT 0,
    version      INT NOT NULL DEFAULT 1,
    tags         TEXT[] NOT NULL DEFAULT '{}',
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_procedural_project ON procedural_memory (project_id);
CREATE INDEX idx_procedural_task_type ON procedural_memory (task_type);
CREATE INDEX idx_procedural_success ON procedural_memory (success_rate DESC)
    WHERE (success_count + failure_count) > 0;

CREATE INDEX idx_procedural_embedding ON procedural_memory
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);
