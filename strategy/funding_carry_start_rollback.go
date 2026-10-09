package strategy

import (
	"context"
	"time"
)

// Only the exact verified remaining-accounting result may roll back producers
// without pretending financial Stop succeeded. Other errors retain Stop's
// financial-state checks, including bare borrowed-debt receipt recovery.
func (s *FundingCarryStrategy) rollbackFailedStartup(startupErr error) error {
	for err := startupErr; err != nil; {
		if _, ok := err.(*FundingCarryReconciliationRequiredError); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.QuiesceContext(ctx); err != nil {
				return err
			}
			s.mu.RLock()
			gate := s.openingGate
			s.mu.RUnlock()
			return s.VerifyRemainingReconciliation(ctx, gate)
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = wrapped.Unwrap()
	}
	return s.Stop()
}
