-- Meta-cognitive log: tracks prediction-outcome pairs for confidence calibration.
-- Maps to anterior cingulate cortex — error monitoring and self-assessment.
-- Enables: "in domain X, my confidence is systematically 15% too high."

CREATE TABLE metacognitive_log (
    id                   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    domain               TEXT NOT NULL,
    predicted_confidence FLOAT NOT NULL,
    actual_accuracy      FLOAT,
    strategy_used        TEXT,
    strategy_outcome     TEXT,
    agent_id             TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_metacog_domain ON metacognitive_log (domain, agent_id);
CREATE INDEX idx_metacog_agent ON metacognitive_log (agent_id, created_at DESC);

-- Materialized view: per-domain calibration summary
-- Refreshed by consolidation engine after each batch of resolved predictions
CREATE MATERIALIZED VIEW IF NOT EXISTS metacognitive_calibration AS
SELECT
    domain,
    agent_id,
    COUNT(*) AS sample_count,
    AVG(predicted_confidence) AS avg_predicted_confidence,
    AVG(actual_accuracy) AS avg_actual_accuracy,
    AVG(predicted_confidence) - AVG(actual_accuracy) AS calibration_offset
FROM metacognitive_log
WHERE actual_accuracy IS NOT NULL
GROUP BY domain, agent_id;

CREATE UNIQUE INDEX idx_metacog_cal_domain_agent
    ON metacognitive_calibration (domain, agent_id);
