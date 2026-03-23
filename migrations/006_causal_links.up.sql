-- Causal links: directed relationships between memories across tiers.
-- Enables reasoning about interventions: "if I change X, what happens to Y?"
-- Forms a causal graph overlaying the knowledge graph.

CREATE TABLE causal_links (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cause_id            UUID NOT NULL,
    cause_tier          TEXT NOT NULL,
    effect_id           UUID NOT NULL,
    effect_tier         TEXT NOT NULL,
    relation_type       TEXT NOT NULL,
    confidence          FLOAT NOT NULL DEFAULT 0.5,
    evidence_episodes   UUID[] NOT NULL DEFAULT '{}',
    counter_evidence    UUID[] NOT NULL DEFAULT '{}',
    boundary_conditions TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_relation CHECK (
        relation_type IN ('caused', 'prevented', 'enabled', 'degraded', 'required')
    ),
    CONSTRAINT valid_tier_cause CHECK (
        cause_tier IN ('episodic', 'semantic', 'procedural', 'causal')
    ),
    CONSTRAINT valid_tier_effect CHECK (
        effect_tier IN ('episodic', 'semantic', 'procedural', 'causal')
    )
);

CREATE INDEX idx_causal_cause ON causal_links (cause_id);
CREATE INDEX idx_causal_effect ON causal_links (effect_id);
CREATE INDEX idx_causal_relation ON causal_links (relation_type);
CREATE INDEX idx_causal_confidence ON causal_links (confidence DESC);
