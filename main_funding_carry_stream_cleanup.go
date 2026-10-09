package main

import (
	"errors"
	"fmt"

	"quantmesh/strategy"
)

type runtimeStopCleanupRetryError struct{ cause error }

func (e *runtimeStopCleanupRetryError) Error() string {
	return fmt.Sprintf("funding_carry stopped runtime requires order stream cleanup retry: %v", e.cause)
}
func (e *runtimeStopCleanupRetryError) Unwrap() error { return e.cause }
func isRetryableRuntimeStopCleanup(err error) bool {
	var pending *runtimeStopCleanupRetryError
	return errors.As(err, &pending)
}

type fundingCarryStreamStop struct {
	name string
	stop func() error
	done bool
}

// Owned by the runtime stopMu. Financial work is never called here. A shared
// final-verification marker belongs to that verifier and must not be cleared.
type fundingCarryStreamCleanup struct {
	rt             *SymbolRuntime
	ownershipGuard func() error
	streams        []fundingCarryStreamStop
	reason         *string
	shared         bool
	completed      bool
	err            error
}

func (c *fundingCarryStreamCleanup) guard() error {
	if c.rt == nil || c.ownershipGuard == nil || c.rt.shutdownCloseUnverified.Load() != c.reason {
		return fmt.Errorf("stream cleanup no longer owns the retained failure")
	}
	return c.ownershipGuard()
}

func (c *fundingCarryStreamCleanup) run(first bool, financialErr error, sharedReason *string) error {
	if c.rt == nil || c.ownershipGuard == nil {
		return fmt.Errorf("stream cleanup requires runtime and ownership guard")
	}
	if c.completed {
		return nil
	}
	eligible := financialErr == nil || strategy.IsFundingCarryFinalVerificationPending(financialErr)
	if !first {
		if !eligible {
			return nil
		} // Unknown financial outcomes are cached, not replayable.
		if err := c.guard(); err != nil {
			return errors.Join(c.err, err)
		}
	} else {
		c.reason, c.shared = sharedReason, sharedReason != nil
	}
	var failures []error
	for i := range c.streams {
		s := &c.streams[i]
		if s.done {
			continue
		}
		if !first {
			if err := c.guard(); err != nil {
				return errors.Join(c.err, err)
			}
		}
		if err := s.stop(); err != nil {
			failures = append(failures, fmt.Errorf("stop %s order stream: %w", s.name, err))
		} else {
			s.done = true
		}
		if !first {
			if err := c.guard(); err != nil {
				return errors.Join(errors.Join(failures...), err)
			}
		}
	}
	c.err = errors.Join(failures...)
	if !eligible {
		return c.err
	} // Initial cleanup still runs even on a generic financial failure.
	if c.err != nil {
		if c.reason == nil {
			if financialErr != nil {
				return errors.Join(c.err, fmt.Errorf("financial verification marker was not retained"))
			}
			reason := c.err.Error()
			if !c.rt.shutdownCloseUnverified.CompareAndSwap(nil, &reason) {
				return errors.Join(c.err, fmt.Errorf("other stop failure superseded stream cleanup"))
			}
			c.reason = &reason
		}
		if err := c.guard(); err != nil {
			return errors.Join(c.err, err)
		}
		return &runtimeStopCleanupRetryError{cause: errors.Join(financialErr, c.err)}
	}
	if err := c.guard(); err != nil {
		return err
	}
	if c.reason != nil && !c.shared {
		if !c.rt.shutdownCloseUnverified.CompareAndSwap(c.reason, nil) {
			return fmt.Errorf("stream cleanup failure changed before completion")
		}
	}
	c.completed = true
	c.reason = nil
	return nil
}
