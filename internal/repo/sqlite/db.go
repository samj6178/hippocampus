package sqlite

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/hippocampus-mcp/hippocampus/internal/pkg/contenthash"
)

// ContentHash delegates to the shared contenthash package.
// Kept as a convenience alias for repo-internal use.
func ContentHash(content string) string {
	return contenthash.Of(content)
}

// schemaSQL contains the complete database schema for Hippocampus MOS.
// FTS5 virtual tables are managed via manual inserts/deletes in repo methods
// rather than triggers, because FTS5 external content with TEXT rowid requires
// careful coordination that is easier to handle explicitly.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS projects (
  id           TEXT PRIMARY KEY,
  slug         TEXT UNIQUE NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  description  TEXT NOT NULL DEFAULT '',
  root_path    TEXT NOT NULL DEFAULT '',
  is_active    INTEGER NOT NULL DEFAULT 1,
  metadata     TEXT NOT NULL DEFAULT '{}',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS episodic_memory (
  id            TEXT PRIMARY KEY,
  time          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  project_id    TEXT REFERENCES projects(id),
  agent_id      TEXT NOT NULL DEFAULT '',
  session_id    TEXT NOT NULL DEFAULT '',
  content       TEXT NOT NULL DEFAULT '',
  summary       TEXT NOT NULL DEFAULT '',
  embedding     BLOB,
  importance    REAL NOT NULL DEFAULT 0.5,
  confidence    REAL NOT NULL DEFAULT 0.5,
  access_count  INTEGER NOT NULL DEFAULT 0,
  last_accessed TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  token_count   INTEGER NOT NULL DEFAULT 0,
  tags          TEXT NOT NULL DEFAULT '[]',
  metadata      TEXT NOT NULL DEFAULT '{}',
  consolidated  INTEGER NOT NULL DEFAULT 0,
  content_hash  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_episodic_project        ON episodic_memory(project_id);
CREATE INDEX IF NOT EXISTS idx_episodic_time           ON episodic_memory(time);
CREATE INDEX IF NOT EXISTS idx_episodic_unconsolidated ON episodic_memory(consolidated) WHERE consolidated = 0;
CREATE INDEX IF NOT EXISTS idx_episodic_content_hash   ON episodic_memory(project_id, content_hash) WHERE content_hash != '';

CREATE TABLE IF NOT EXISTS semantic_memory (
  id              TEXT PRIMARY KEY,
  project_id      TEXT REFERENCES projects(id),
  content         TEXT NOT NULL DEFAULT '',
  summary         TEXT NOT NULL DEFAULT '',
  embedding       BLOB,
  importance      REAL NOT NULL DEFAULT 0.5,
  confidence      REAL NOT NULL DEFAULT 0.5,
  access_count    INTEGER NOT NULL DEFAULT 0,
  last_accessed   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  token_count     INTEGER NOT NULL DEFAULT 0,
  tags            TEXT NOT NULL DEFAULT '[]',
  metadata        TEXT NOT NULL DEFAULT '{}',
  entity_type     TEXT NOT NULL DEFAULT '',
  source_episodes TEXT NOT NULL DEFAULT '[]',
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_semantic_project    ON semantic_memory(project_id);
CREATE INDEX IF NOT EXISTS idx_semantic_entity     ON semantic_memory(entity_type);
CREATE INDEX IF NOT EXISTS idx_semantic_importance ON semantic_memory(importance DESC);

CREATE TABLE IF NOT EXISTS procedural_memory (
  id            TEXT PRIMARY KEY,
  project_id    TEXT REFERENCES projects(id),
  task_type     TEXT NOT NULL DEFAULT '',
  content       TEXT NOT NULL DEFAULT '',
  embedding     BLOB,
  importance    REAL NOT NULL DEFAULT 0.5,
  confidence    REAL NOT NULL DEFAULT 0.5,
  success_count INTEGER NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_used     TEXT,
  token_count   INTEGER NOT NULL DEFAULT 0,
  tags          TEXT NOT NULL DEFAULT '[]',
  metadata      TEXT NOT NULL DEFAULT '{}',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_procedural_project ON procedural_memory(project_id);
CREATE INDEX IF NOT EXISTS idx_procedural_task    ON procedural_memory(task_type);

CREATE TABLE IF NOT EXISTS causal_links (
  id               TEXT PRIMARY KEY,
  cause_id         TEXT NOT NULL,
  effect_id        TEXT NOT NULL,
  relation_type    TEXT NOT NULL DEFAULT 'caused',
  confidence       REAL NOT NULL DEFAULT 0.5,
  evidence         TEXT NOT NULL DEFAULT '[]',
  counter_evidence TEXT NOT NULL DEFAULT '[]',
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_causal_cause  ON causal_links(cause_id);
CREATE INDEX IF NOT EXISTS idx_causal_effect ON causal_links(effect_id);

CREATE TABLE IF NOT EXISTS predictions (
  id               TEXT PRIMARY KEY,
  project_id       TEXT REFERENCES projects(id),
  agent_id         TEXT NOT NULL DEFAULT '',
  domain           TEXT NOT NULL DEFAULT '',
  action           TEXT NOT NULL DEFAULT '',
  expected_outcome TEXT NOT NULL DEFAULT '',
  actual_outcome   TEXT,
  confidence       REAL NOT NULL DEFAULT 0.5,
  prediction_error REAL,
  embedding        BLOB,
  resolved         INTEGER NOT NULL DEFAULT 0,
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  resolved_at      TEXT
);
CREATE INDEX IF NOT EXISTS idx_predictions_unresolved ON predictions(resolved) WHERE resolved = 0;
CREATE INDEX IF NOT EXISTS idx_predictions_domain     ON predictions(domain, agent_id);

CREATE TABLE IF NOT EXISTS metacognitive_log (
  id                  TEXT PRIMARY KEY,
  agent_id            TEXT NOT NULL DEFAULT '',
  domain              TEXT NOT NULL DEFAULT '',
  predicted_confidence REAL NOT NULL,
  actual_outcome      REAL NOT NULL,
  calibration_error   REAL NOT NULL,
  context             TEXT NOT NULL DEFAULT '',
  created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_metacognitive_domain ON metacognitive_log(domain, agent_id);

CREATE TABLE IF NOT EXISTS emotional_tags (
  id         TEXT PRIMARY KEY,
  memory_id  TEXT NOT NULL,
  valence    TEXT NOT NULL DEFAULT 'neutral',
  intensity  REAL NOT NULL DEFAULT 0.5,
  source     TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_emotional_memory   ON emotional_tags(memory_id);
CREATE INDEX IF NOT EXISTS idx_emotional_priority ON emotional_tags(valence, intensity DESC);

CREATE TABLE IF NOT EXISTS knowledge_triples (
  id             TEXT PRIMARY KEY,
  project_id     TEXT REFERENCES projects(id),
  subject        TEXT NOT NULL,
  predicate      TEXT NOT NULL,
  object         TEXT NOT NULL,
  valid_from     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  valid_to       TEXT,
  confidence     REAL NOT NULL DEFAULT 1.0,
  source_session TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_triples_subject  ON knowledge_triples(project_id, subject);
CREATE INDEX IF NOT EXISTS idx_triples_object   ON knowledge_triples(project_id, object);
CREATE INDEX IF NOT EXISTS idx_triples_temporal  ON knowledge_triples(valid_from, valid_to);

CREATE VIRTUAL TABLE IF NOT EXISTS episodic_fts   USING fts5(content, content_rowid='rowid');
CREATE VIRTUAL TABLE IF NOT EXISTS semantic_fts   USING fts5(content, content_rowid='rowid');
CREATE VIRTUAL TABLE IF NOT EXISTS procedural_fts USING fts5(content, content_rowid='rowid');

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
`

// NewDB opens a SQLite database at the given path and applies performance pragmas.
// The caller is responsible for closing the returned *sql.DB.
func NewDB(path string) (*sql.DB, error) {
	// Pragmas are passed as query parameters in the DSN.
	// _cache_size is negative to indicate KB units (64 MB = 65536 KB ≈ -64000).
	dsn := path +
		"?_journal_mode=WAL" +
		"&_busy_timeout=5000" +
		"&_foreign_keys=ON" +
		"&_cache_size=-64000" +
		"&_synchronous=NORMAL"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite performs best with a single writer connection.
	// WAL mode allows one writer + multiple concurrent readers.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}

// RunMigrations applies the embedded schema to db if it has not been applied yet.
// It checks schema_version to avoid re-applying an already initialised database.
func RunMigrations(db *sql.DB) error {
	// Check if schema_version exists and already has a version recorded.
	// If the table doesn't exist yet, the query will fail — that's the signal
	// to run the full schema.
	var version int
	err := db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	if err != nil {
		version = 0
	}

	if version == 0 {
		// Fresh database: apply the full schema.
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration tx: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck

		if _, err := tx.Exec(schemaSQL); err != nil {
			return fmt.Errorf("apply schema: %w", err)
		}

		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (3)`); err != nil {
			return fmt.Errorf("record schema version: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration: %w", err)
		}
		return nil
	}

	// Incremental migrations for existing databases.
	if version < 2 {
		if err := migrateV1toV2(db); err != nil {
			return fmt.Errorf("migrate v1->v2: %w", err)
		}
	}

	if version < 3 {
		if err := migrateV2toV3(db); err != nil {
			return fmt.Errorf("migrate v2->v3: %w", err)
		}
	}

	// Self-healing: backfill any rows with empty content_hash (covers partial
	// migration failures and rows inserted by older code).
	if err := backfillContentHashes(db); err != nil {
		return fmt.Errorf("backfill content hashes: %w", err)
	}

	return nil
}

// migrateV1toV2 adds content_hash column for persistent dedup.
// Backfills hashes for existing rows so the index is immediately useful.
func migrateV1toV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin v2 tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Add column (idempotent: ignore error if column already exists).
	_, _ = tx.Exec(`ALTER TABLE episodic_memory ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''`)

	// Create index for dedup lookups.
	_, _ = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_episodic_content_hash ON episodic_memory(project_id, content_hash) WHERE content_hash != ''`)

	// Bump schema version.
	if _, err := tx.Exec(`UPDATE schema_version SET version = 2`); err != nil {
		return fmt.Errorf("update schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit v2: %w", err)
	}

	return nil
}

// migrateV2toV3 adds the knowledge_triples table for temporal knowledge graph.
func migrateV2toV3(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin v3 tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, _ = tx.Exec(`CREATE TABLE IF NOT EXISTS knowledge_triples (
		id             TEXT PRIMARY KEY,
		project_id     TEXT REFERENCES projects(id),
		subject        TEXT NOT NULL,
		predicate      TEXT NOT NULL,
		object         TEXT NOT NULL,
		valid_from     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		valid_to       TEXT,
		confidence     REAL NOT NULL DEFAULT 1.0,
		source_session TEXT NOT NULL DEFAULT '',
		created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`)
	_, _ = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_triples_subject ON knowledge_triples(project_id, subject)`)
	_, _ = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_triples_object ON knowledge_triples(project_id, object)`)
	_, _ = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_triples_temporal ON knowledge_triples(valid_from, valid_to)`)

	if _, err := tx.Exec(`UPDATE schema_version SET version = 3`); err != nil {
		return fmt.Errorf("update schema version: %w", err)
	}

	return tx.Commit()
}

// backfillContentHashes fills empty content_hash values for any episodic memories.
// Runs at every startup — cheap (index hit on empty set when nothing to backfill)
// and self-healing (covers partial migration failures and rows from older code).
func backfillContentHashes(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, content FROM episodic_memory WHERE content_hash = '' LIMIT 10000`)
	if err != nil {
		return fmt.Errorf("backfill query: %w", err)
	}
	defer rows.Close()

	type row struct {
		id, content string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.content); err != nil {
			return fmt.Errorf("backfill scan: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("backfill rows: %w", err)
	}

	for _, r := range pending {
		hash := ContentHash(r.content)
		if _, err := db.Exec(`UPDATE episodic_memory SET content_hash = ? WHERE id = ?`, hash, r.id); err != nil {
			return fmt.Errorf("backfill update id=%s: %w", r.id, err)
		}
	}

	return nil
}
