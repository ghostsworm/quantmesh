package strategy

import (
	"context"
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestSpotShortBorrowHistoryCannotBeAssignedToCompetingIntent(t *testing.T) {
	for _, phase := range []string{"prepared", "borrowed"} {
		t.Run(phase, func(t *testing.T) {
			started := time.Now().Add(-time.Second).UnixMilli()
			venue := &spotShortClientOrderLookupExchange{borrowRows: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.5, Status: "CONFIRMED", Timestamp: started + 1}}, borrowTotal: 1}
			s := newSpotShortForTest(&signalTestExecutor{}, venue, &mockMarginExchange{})
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			intent := spotShortPendingBorrow{Amount: 0.5, Phase: "prepared", CreatedAtUnixMilli: started}
			other := intent
			other.Phase = phase
			if phase == "borrowed" {
				other.BorrowTransferID = 81
			}
			s.pendingBorrow["target"] = intent
			s.pendingBorrow["other"] = other
			s.mu.Lock()
			err := s.persistRuntimeStateLocked()
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			original := store.payload
			restarted := newSpotShortForTest(&signalTestExecutor{}, venue, &mockMarginExchange{})
			restarted.SetRuntimeStateStore(store)
			restarted.mu.Lock()
			err = restarted.restoreRuntimeStateLocked()
			restarted.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			s = restarted
			if _, err := s.reconcilePendingBorrowTransfer(context.Background(), "target", intent); err == nil {
				t.Fatal("one borrow transfer was attributed to competing intents")
			}
			if s.pendingBorrow["target"] != intent || s.pendingBorrow["other"] != other || store.payload != original {
				t.Fatal("ambiguous transfer changed memory or durable state")
			}
		})
	}
}

func TestSpotShortBorrowHistoryRecoversWithoutCompetingIntent(t *testing.T) {
	started := time.Now().Add(-time.Second).UnixMilli()
	for name, other := range map[string]spotShortPendingBorrow{
		"known_unsubmitted":        {Amount: 0.5, Phase: "unsubmitted", CreatedAtUnixMilli: started},
		"different_amount":         {Amount: 0.25, Phase: "prepared", CreatedAtUnixMilli: started},
		"later_submission":         {Amount: 0.5, Phase: "prepared", CreatedAtUnixMilli: started + 2},
		"other_confirmed_transfer": {Amount: 0.5, Phase: "borrowed", BorrowTransferID: 82, CreatedAtUnixMilli: started},
	} {
		t.Run(name, func(t *testing.T) {
			venue := &spotShortClientOrderLookupExchange{borrowRows: []exchange.MarginBorrowRecord{{TransferID: 81, Asset: "BTC", Amount: 0.5, Status: "CONFIRMED", Timestamp: started + 1}}, borrowTotal: 1}
			s := newSpotShortForTest(&signalTestExecutor{}, venue, &mockMarginExchange{})
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			intent := spotShortPendingBorrow{Amount: 0.5, Phase: "prepared", CreatedAtUnixMilli: started}
			s.pendingBorrow["target"] = intent
			s.pendingBorrow["other"] = other
			got, err := s.reconcilePendingBorrowTransfer(context.Background(), "target", intent)
			if err != nil || got.Phase != "borrowed" || got.BorrowTransferID != 81 || s.pendingBorrow["target"] != got || s.pendingBorrow["other"] != other {
				t.Fatalf("unambiguous transfer failed recovery: got=%+v err=%v", got, err)
			}
			restored := newSpotShortForTest(&signalTestExecutor{}, venue, &mockMarginExchange{})
			restored.SetRuntimeStateStore(store)
			restored.mu.Lock()
			err = restored.restoreRuntimeStateLocked()
			restored.mu.Unlock()
			if err != nil || restored.pendingBorrow["target"] != got || restored.pendingBorrow["other"] != other {
				t.Fatalf("durable recovery lost evidence: %v", err)
			}
		})
	}
}
