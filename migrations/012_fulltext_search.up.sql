-- Add full-text search via expression-based GIN indexes.
-- Compatible with TimescaleDB hypertables (no generated columns needed).
-- Queries use to_tsvector('english', content) inline — the index accelerates them.

CREATE INDEX IF NOT EXISTS idx_episodic_tsv ON episodic_memory USING gin(to_tsvector('english', coalesce(content, '')));
CREATE INDEX IF NOT EXISTS idx_semantic_tsv ON semantic_memory USING gin(to_tsvector('english', coalesce(content, '')));
CREATE INDEX IF NOT EXISTS idx_procedural_tsv ON procedural_memory USING gin(to_tsvector('english', coalesce(description, '')));
