package daily

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const reasonSourceChanged = "source_changed"

// Service coordinates persisted evidence with deterministic evaluation. It is
// read-only with respect to exchanges: no method submits trading or transfer
// requests and none derives withdrawal authorization.
type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Reconcile loads one immutable input snapshot, evaluates it and publishes the
// result with a full-digest compare-and-set in the repository transaction.
func (s *Service) Reconcile(ctx context.Context, query Query, now time.Time) (Result, error) {
	if err := validateServiceQuery(query); err != nil {
		return Result{}, err
	}
	if s == nil || s.repository == nil {
		return Result{}, fmt.Errorf("%w: daily equity repository is required", ErrInvalid)
	}
	inputs, err := s.repository.LoadDailyEquityInputs(ctx, query)
	if err != nil {
		return Result{}, fmt.Errorf("load daily equity inputs: %w", err)
	}
	if err := matchQuery(query, inputs.Evidence); err != nil {
		return Result{}, err
	}
	result, err := Evaluate(inputs, now)
	if err != nil {
		return Result{}, err
	}
	committed, err := s.repository.CommitDailyEquityResult(ctx, query, result.InputDigest, result)
	if err != nil {
		return Result{}, fmt.Errorf("commit daily equity result: %w", err)
	}
	return committed, nil
}

// Read returns a persisted result only while its full input digest is still
// current. A changed input snapshot makes the stored report explicitly
// incomplete; the historical report itself is never overwritten here.
func (s *Service) Read(ctx context.Context, query Query, now time.Time) (Result, error) {
	if err := validateServiceQuery(query); err != nil {
		return Result{}, err
	}
	if s == nil || s.repository == nil {
		return Result{}, fmt.Errorf("%w: daily equity repository is required", ErrInvalid)
	}
	persisted, err := s.repository.LoadDailyEquityResult(ctx, query)
	if err != nil {
		return Result{}, fmt.Errorf("load daily equity result: %w", err)
	}
	inputs, err := s.repository.LoadDailyEquityInputs(ctx, query)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return staleResult(persisted), nil
		}
		return Result{}, fmt.Errorf("load daily equity inputs: %w", err)
	}
	if err := matchQuery(query, inputs.Evidence); err != nil {
		return Result{}, err
	}
	digest, err := InputDigest(inputs)
	if err != nil {
		return Result{}, err
	}
	if digest != persisted.InputDigest {
		return staleResult(persisted), nil
	}
	_ = now
	return persisted, nil
}

func staleResult(persisted Result) Result {
	out := persisted
	out.EquityReconciliationComplete = false
	reasons := append([]string(nil), persisted.Reasons...)
	found := false
	for _, reason := range reasons {
		if reason == reasonSourceChanged {
			found = true
			break
		}
	}
	if !found {
		reasons = append(reasons, reasonSourceChanged)
	}
	out.Reasons = reasons
	return out
}

func validateServiceQuery(query Query) error {
	if query.Exchange == "" || query.AccountScope == "" {
		return fmt.Errorf("%w: exchange and accountScope are required", ErrInvalid)
	}
	if _, _, err := DayBounds(query.Date); err != nil {
		return err
	}
	return nil
}

func matchQuery(query Query, evidence Evidence) error {
	switch {
	case evidence.Scope.Exchange != query.Exchange:
		return fmt.Errorf("%w: evidence exchange does not match query", ErrInvalid)
	case evidence.Scope.AccountScope != query.AccountScope:
		return fmt.Errorf("%w: evidence account scope does not match query", ErrInvalid)
	case evidence.Date != query.Date:
		return fmt.Errorf("%w: evidence date does not match query", ErrInvalid)
	}
	return nil
}
