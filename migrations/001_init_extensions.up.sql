-- Hippocampus MOS: Enable required PostgreSQL extensions
-- TimescaleDB for time-series hypertables + compression + continuous aggregates
-- pgvector for embedding similarity search (HNSW index)

CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
