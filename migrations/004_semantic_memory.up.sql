-- Semantic memory: knowledge graph nodes. Maps to neocortex.
-- Near-permanent storage of generalized facts, concepts, patterns.
-- NULL project_id = global knowledge (available across all projects).

CREATE TABLE semantic_memory (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id     UUID REFERENCES projects(id) ON DELETE SET NULL,
    entity_type    TEXT NOT NULL DEFAULT 'fact',
    content        TEXT NOT NULL,
    summary        TEXT,
    embedding      vector(768) NOT NULL,
    importance     FLOAT NOT NULL DEFAULT 0.5,
    confidence     FLOAT NOT NULL DEFAULT 1.0,
    source_episodes UUID[] NOT NULL DEFAULT '{}',
    access_count   INT NOT NULL DEFAULT 0,
    last_accessed  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    token_count    INT NOT NULL DEFAULT 0,
    tags           TEXT[] NOT NULL DEFAULT '{}',
    metadata       JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_semantic_project ON semantic_memory (project_id);
CREATE INDEX idx_semantic_global ON semantic_memory (importance DESC)
    WHERE project_id IS NULL;
CREATE INDEX idx_semantic_entity_type ON semantic_memory (entity_type);

CREATE INDEX idx_semantic_embedding ON semantic_memory
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);

-- Semantic edges: knowledge graph relations between semantic nodes.
CREATE TABLE semantic_edges (
    id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    source_id UUID NOT NULL REFERENCES semantic_memory(id) ON DELETE CASCADE,
    target_id UUID NOT NULL REFERENCES semantic_memory(id) ON DELETE CASCADE,
    relation  TEXT NOT NULL,
    weight    FLOAT NOT NULL DEFAULT 1.0,
    metadata  JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (source_id, target_id, relation)
);

CREATE INDEX idx_semantic_edges_source ON semantic_edges (source_id);
CREATE INDEX idx_semantic_edges_target ON semantic_edges (target_id);
CREATE INDEX idx_semantic_edges_relation ON semantic_edges (relation);
