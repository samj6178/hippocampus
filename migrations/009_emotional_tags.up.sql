-- Emotional tags: formal affective signals on memory items.
-- Maps to amygdala — non-anthropomorphic signals that affect
-- consolidation priority and decay rate.

CREATE TABLE emotional_tags (
    memory_id   UUID NOT NULL,
    memory_tier TEXT NOT NULL,
    valence     TEXT NOT NULL,
    intensity   FLOAT NOT NULL DEFAULT 0.5,
    signals     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (memory_id, valence),

    CONSTRAINT valid_valence CHECK (
        valence IN ('success', 'frustration', 'surprise', 'novelty', 'danger')
    ),
    CONSTRAINT valid_memory_tier CHECK (
        memory_tier IN ('episodic', 'semantic', 'procedural')
    ),
    CONSTRAINT valid_intensity CHECK (
        intensity >= 0.0 AND intensity <= 1.0
    )
);

CREATE INDEX idx_emotional_memory ON emotional_tags (memory_id);
CREATE INDEX idx_emotional_valence ON emotional_tags (valence);
CREATE INDEX idx_emotional_high_priority ON emotional_tags (intensity DESC)
    WHERE valence IN ('danger', 'surprise', 'frustration');
