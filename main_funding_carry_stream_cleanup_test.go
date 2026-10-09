package main

import (
	"errors"
	"testing"
)

func TestFundingCarryStreamCleanupRetainsOtherFailuresAndOwnership(t *testing.T) {
	for _, mode := range []string{"recovered", "changed_same_text", "changed_during", "lost_before", "lost_during", "generic_financial", "partial_cleanup"} {
		t.Run(mode, func(t *testing.T) {
			rt := &SymbolRuntime{}
			lost, recovered := false, false
			calls := [3]int{}
			c := &fundingCarryStreamCleanup{rt: rt, ownershipGuard: func() error {
				if lost {
					return errors.New("fixture owner lost")
				}
				return nil
			}}
			for i := 0; i < 3; i++ {
				c.streams = append(c.streams, fundingCarryStreamStop{name: []string{"futures", "spot", "margin"}[i], stop: func() error {
					calls[i]++
					if recovered && i == 1 {
						if mode == "changed_during" {
							rt.markShutdownCloseUnverified("other failure")
						}
						if mode == "lost_during" {
							lost = true
						}
					}
					if !recovered && (i == 1 || (mode == "partial_cleanup" && i == 0)) {
						return errConstructorStreamCleanup
					}
					return nil
				}})
			}
			var financial error
			if mode == "generic_financial" {
				financial = errors.New("fixture financial UNKNOWN")
			}
			first := c.run(true, financial, nil)
			if first == nil || isRetryableRuntimeStopCleanup(first) == (mode == "generic_financial") {
				t.Fatal("initial cleanup eligibility incorrect", first)
			}
			original := rt.shutdownCloseUnverified.Load()
			if mode == "generic_financial" {
				if err := c.run(false, financial, nil); err != nil || calls != [3]int{1, 1, 1} {
					t.Fatal("generic financial fault retried cleanup")
				}
				return
			}
			if mode == "changed_same_text" {
				rt.markShutdownCloseUnverified(*original)
			}
			if mode == "lost_before" {
				lost = true
			}
			recovered = true
			err := c.run(false, nil, nil)
			if mode == "recovered" || mode == "partial_cleanup" {
				want := [3]int{1, 2, 1}
				if mode == "partial_cleanup" {
					want[0] = 2
				}
				if err != nil || rt.shutdownCloseUnverified.Load() != nil || calls != want {
					t.Fatal("cleanup did not finish only failed legs", err, calls)
				}
				if err := c.run(false, nil, nil); err != nil || calls != want {
					t.Fatal("completed cleanup replayed legs", err)
				}
				return
			}
			if err == nil || isRetryableRuntimeStopCleanup(err) || rt.shutdownCloseUnverified.Load() == nil {
				t.Fatal("other failure or ownership loss was hidden", err)
			}
			if (mode == "changed_same_text" || mode == "lost_before") && calls != [3]int{1, 1, 1} {
				t.Fatal("invalid prerequisite performed another cleanup")
			}
			before := rt.shutdownCloseUnverified.Load()
			rt.retainShutdownCloseFailure("controller wrapper error")
			if rt.shutdownCloseUnverified.Load() != before {
				t.Fatal("controller overwrote independently recorded failure")
			}
		})
	}
}
