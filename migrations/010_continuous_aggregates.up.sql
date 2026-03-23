-- Helper function: compute decay-adjusted importance at query time.
-- Uses exponential decay without full-table UPDATE (computed on read).
-- half_life_seconds defaults to 7 days = 604800 seconds.
CREATE OR REPLACE FUNCTION decayed_importance(
    base_importance FLOAT,
    last_accessed TIMESTAMPTZ,
    half_life_seconds FLOAT DEFAULT 604800
) RETURNS FLOAT AS $$
BEGIN
    RETURN base_importance * EXP(
        -0.693147 * EXTRACT(EPOCH FROM (NOW() - last_accessed)) / half_life_seconds
    );
END;
$$ LANGUAGE plpgsql IMMUTABLE;
