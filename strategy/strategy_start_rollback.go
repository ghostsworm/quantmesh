package strategy

import (
	"errors"
	"fmt"
)

// StrategyStartupRollbackError reports a failed startup whose cleanup is also
// unverified. It must not be treated as an ordinary stopped startup failure.
type StrategyStartupRollbackError struct {
	Startup  error
	Rollback error
}

func (e *StrategyStartupRollbackError) Error() string {
	return fmt.Sprintf("strategy startup rollback unverified: %v", errors.Join(e.Startup, e.Rollback))
}
func (e *StrategyStartupRollbackError) Unwrap() []error { return []error{e.Startup, e.Rollback} }

func stopFailedStrategyStartup(s Strategy, startupErr error) error {
	if carry, ok := s.(*FundingCarryStrategy); ok {
		return carry.rollbackFailedStartup(startupErr)
	}
	return s.Stop()
}
