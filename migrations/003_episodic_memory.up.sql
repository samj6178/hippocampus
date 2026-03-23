-- Episodic memory: time-partitioned hypertable for experience storage.
-- Maps to hippocampus in neuroscience — fast writing, temporal ordering.
-- Chunk interval 1 week balances query performance and compression.

CREATE TABLE episodic_memory (
    id           UUID NOT NULL DEFAULT uuid_generate_v4(),
    time         TIMESTAMPTZ NOT NULL,
    project_id   UUID REFERENCES projects(id) ON DELETE SET NULL,
    agent_id     TEXT NOT NULL,
    session_id   UUID NOT NULL,
    content      TEXT NOT NULL,
    summary      TEXT,
    embedding    vector(768) NOT NULL,
    importance   FLOAT NOT NULL DEFAULT 0.5,
    confidence   FLOAT NOT NULL DEFAULT 1.0,
    access_count INT NOT NULL DEFAULT 0,
    last_accessed TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    token_count  INT NOT NULL DEFAULT 0,
    tags         TEXT[] NOT NULL DEFAULT '{}',
    metadata     JSONB NOT NULL DEFAULT '{}',
    consolidated BOOLEAN NOT NULL DEFAULT false,

    PRIMARY KEY (id, time)
);

SELECT create_hypertable('episodic_memory', 'time',
    chunk_time_interval => INTERVAL '1 week'
);

CREATE INDEX idx_episodic_project ON episodic_memory (project_id, time DESC);
CREATE INDEX idx_episodic_session ON episodic_memory (session_id, time DESC);
CREATE INDEX idx_episodic_agent ON episodic_memory (agent_id, time DESC);
CREATE INDEX idx_episodic_unconsolidated ON episodic_memory (consolidated, time DESC)
    WHERE consolidated = false;

-- HNSW vector index for similarity search
-- m=16, ef_construction=200 balances recall quality and build time for 1536-dim
CREATE INDEX idx_episodic_embedding ON episodic_memory
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);

-- Compression policy: compress chunks older than 7 days
ALTER TABLE episodic_memory SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'project_id',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('episodic_memory', INTERVAL '7 days');

-- Retention: drop raw episodic data older than 90 days
-- (consolidated summaries survive in semantic_memory)
SELECT add_retention_policy('episodic_memory', INTERVAL '90 days');
