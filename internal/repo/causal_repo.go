package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type CausalRepo struct {
	pool *pgxpool.Pool
}

func NewCausalRepo(pool *pgxpool.Pool) *CausalRepo {
	return &CausalRepo{pool: pool}
}

func (r *CausalRepo) Insert(ctx context.Context, link *domain.CausalLink) error {
	evidence := link.Evidence
	if evidence == nil {
		evidence = []uuid.UUID{}
	}
	counter := link.CounterEvidence
	if counter == nil {
		counter = []uuid.UUID{}
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO causal_links (id, cause_id, cause_tier, effect_id, effect_tier,
			relation_type, confidence, evidence_episodes, counter_evidence,
			boundary_conditions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		link.ID, link.CauseID, link.CauseTier, link.EffectID, link.EffectTier,
		link.Relation, link.Confidence, evidence, counter,
		link.BoundaryConditions,
	)
	if err != nil {
		return fmt.Errorf("causal insert: %w", err)
	}
	return nil
}

func (r *CausalRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.CausalLink, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, cause_id, cause_tier, effect_id, effect_tier,
			relation_type, confidence, evidence_episodes, counter_evidence,
			boundary_conditions, created_at, updated_at
		FROM causal_links
		WHERE id = $1`, id)

	return scanCausal(row)
}

func (r *CausalRepo) GetCauses(ctx context.Context, effectID uuid.UUID) ([]*domain.CausalLink, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, cause_id, cause_tier, effect_id, effect_tier,
			relation_type, confidence, evidence_episodes, counter_evidence,
			boundary_conditions, created_at, updated_at
		FROM causal_links
		WHERE effect_id = $1
		ORDER BY confidence DESC`, effectID)
	if err != nil {
		return nil, fmt.Errorf("get causes: %w", err)
	}
	defer rows.Close()

	return collectCausal(rows)
}

func (r *CausalRepo) GetEffects(ctx context.Context, causeID uuid.UUID) ([]*domain.CausalLink, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, cause_id, cause_tier, effect_id, effect_tier,
			relation_type, confidence, evidence_episodes, counter_evidence,
			boundary_conditions, created_at, updated_at
		FROM causal_links
		WHERE cause_id = $1
		ORDER BY confidence DESC`, causeID)
	if err != nil {
		return nil, fmt.Errorf("get effects: %w", err)
	}
	defer rows.Close()

	return collectCausal(rows)
}

func (r *CausalRepo) AddEvidence(ctx context.Context, id uuid.UUID, episodeID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE causal_links
		SET evidence_episodes = array_append(evidence_episodes, $2),
			confidence = LEAST(confidence + 0.05, 1.0),
			updated_at = NOW()
		WHERE id = $1`, id, episodeID)
	if err != nil {
		return fmt.Errorf("add evidence: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *CausalRepo) AddCounterEvidence(ctx context.Context, id uuid.UUID, episodeID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE causal_links
		SET counter_evidence = array_append(counter_evidence, $2),
			confidence = GREATEST(confidence - 0.1, 0.0),
			updated_at = NOW()
		WHERE id = $1`, id, episodeID)
	if err != nil {
		return fmt.Errorf("add counter evidence: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *CausalRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM causal_links WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("causal delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanCausal(row pgx.Row) (*domain.CausalLink, error) {
	var c domain.CausalLink
	err := row.Scan(
		&c.ID, &c.CauseID, &c.CauseTier, &c.EffectID, &c.EffectTier,
		&c.Relation, &c.Confidence, &c.Evidence, &c.CounterEvidence,
		&c.BoundaryConditions, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan causal: %w", err)
	}
	return &c, nil
}

func collectCausal(rows pgx.Rows) ([]*domain.CausalLink, error) {
	var result []*domain.CausalLink
	for rows.Next() {
		var c domain.CausalLink
		err := rows.Scan(
			&c.ID, &c.CauseID, &c.CauseTier, &c.EffectID, &c.EffectTier,
			&c.Relation, &c.Confidence, &c.Evidence, &c.CounterEvidence,
			&c.BoundaryConditions, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan causal: %w", err)
		}
		result = append(result, &c)
	}
	return result, rows.Err()
}
