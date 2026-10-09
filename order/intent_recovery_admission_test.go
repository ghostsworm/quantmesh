package order

import (
	"encoding/json"
	"testing"
)

func TestLoadedIntentRecoveryRequiresCompleteOwnerValidatedJournal(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "loaded_unknown", true: "corrupt_owner"}[corrupt], func(t *testing.T) {
			journal := &memoryIntentJournal{}
			scope := journalScope()
			key, err := scope.Key()
			if err != nil {
				t.Fatal(err)
			}
			p := persistedIntent{Version: 1, Scope: scope, Opening: true, Request: OrderRequest{Symbol: scope.Symbol, Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "prepared-recovery", StrategyName: "dca"}}
			if corrupt {
				p.Scope.Account = "another-owner"
			}
			payload, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := journal.SaveExecutionIntent(t.Context(), key, p.Request.ClientOrderID, 0, payload); err != nil {
				t.Fatal(err)
			}
			oe, _, _ := newOwnedTestExecutor()
			if oe.LoadedIntentRecoveryRequired(&loadedIntentRecoveryRequiredError{}) {
				t.Fatal("unloaded journal admitted by error type alone")
			}
			err = oe.ConfigureIntentJournal(t.Context(), journal, scope)
			if err == nil || oe.LoadedIntentRecoveryRequired(err) == corrupt || !oe.IsOpeningPaused() {
				t.Fatalf("recovery admission did not distinguish complete owner validation: %v", err)
			}
		})
	}
}
