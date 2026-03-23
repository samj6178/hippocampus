package domain

import "errors"

var (
	ErrNotFound           = errors.New("not found")
	ErrMemoryNotFound     = errors.New("memory not found")
	ErrProjectNotFound    = errors.New("project not found")
	ErrPredictionNotFound = errors.New("prediction not found")
	ErrGateRejected       = errors.New("experience rejected by thalamic gate: insufficient novelty/relevance")
	ErrBudgetExceeded     = errors.New("token budget exceeded")
	ErrEmbeddingFailed    = errors.New("embedding generation failed")
	ErrAlreadyResolved    = errors.New("prediction already resolved")
	ErrInvalidTier        = errors.New("invalid memory tier")
	ErrEmptyContent       = errors.New("content cannot be empty")
	ErrProjectSlugTaken   = errors.New("project slug already in use")
)
