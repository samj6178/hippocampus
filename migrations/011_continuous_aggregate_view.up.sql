-- Continuous aggregate: automatic session-level summaries from episodic memory.
-- NOTE: must run outside transaction block (TimescaleDB requirement).
-- @notx

CREATE MATERIALIZED VIEW IF NOT EXISTS episodic_session_summary
WITH (timescaledb.continuous) AS
SELECT
    session_id,
    project_id,
    agent_id,
    time_bucket('1 hour', time) AS bucket,
    COUNT(*) AS episode_count,
    AVG(importance) AS avg_importance,
    MAX(importance) AS max_importance,
    SUM(token_count) AS total_tokens,
    MIN(time) AS session_start,
    MAX(time) AS session_end
FROM episodic_memory
GROUP BY session_id, project_id, agent_id, time_bucket('1 hour', time);

SELECT add_continuous_aggregate_policy('episodic_session_summary',
    start_offset    => INTERVAL '2 days',
    end_offset      => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour'
);
